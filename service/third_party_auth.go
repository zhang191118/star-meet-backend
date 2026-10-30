package service

import (
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const appleJWKSURL = "https://appleid.apple.com/auth/keys"

const appleIssuer = "https://appleid.apple.com"

// 哨兵错误：区分 Apple 公钥获取/校验失败的原因，便于日志定位与映射合适的状态码
var (
	errAppleKeyFetch    = errors.New("Apple JWKS fetch failed")
	errAppleKeyNotFound = errors.New("Apple signing key not found")
)

type appleJWK struct {
	KID string `json:"kid"`
	Kty string `json:"kty"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type appleJWKSet struct {
	Keys []appleJWK `json:"keys"`
}

var appleKeyCache struct {
	sync.RWMutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

func exchangeWechatCode(code string) (string, error) {
	appid := strings.TrimSpace(os.Getenv("WECHAT_APP_ID"))
	secret := strings.TrimSpace(os.Getenv("WECHAT_APP_SECRET"))
	if appid == "" || secret == "" {
		return "", ErrThirdPartyUnavailable
	}

	endpoint := "https://api.weixin.qq.com/sns/jscode2session?appid=" + url.QueryEscape(appid) +
		"&secret=" + url.QueryEscape(secret) + "&js_code=" + url.QueryEscape(code) + "&grant_type=authorization_code"
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return "", fmt.Errorf("wechat code exchange failed: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		OpenID  string `json:"openid"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("wechat response invalid: %w", err)
	}
	if resp.StatusCode/100 != 2 || payload.ErrCode != 0 || payload.OpenID == "" {
		return "", fmt.Errorf("wechat code rejected: %s", payload.ErrMsg)
	}
	return payload.OpenID, nil
}

func validateAppleIdentityToken(identityToken, claimedUserID, expectedNonce string) (string, error) {
	clientID := strings.TrimSpace(os.Getenv("APPLE_CLIENT_ID"))
	if clientID == "" {
		clientID = strings.TrimSpace(os.Getenv("APPLE_BUNDLE_ID"))
	}
	if clientID == "" {
		return "", ErrThirdPartyUnavailable
	}
	if strings.TrimSpace(identityToken) == "" || strings.TrimSpace(claimedUserID) == "" {
		return "", errors.New("invalid Apple identity token")
	}

	parser := jwt.NewParser(jwt.WithIssuer(appleIssuer), jwt.WithAudience(clientID))
	var seenKid string
	keyfunc := func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("unsupported Apple signing method: %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		seenKid = kid
		return appleSigningKey(kid)
	}

	claims := jwt.MapClaims{}
	token, err := parser.ParseWithClaims(identityToken, claims, keyfunc)

	// 签名校验失败或找不到签名密钥：可能恰逢 Apple 轮换签名密钥而本地缓存未更新，
	// 强制刷新 JWKS 后重试一次（重试可覆盖最多 6 小时的缓存窗口）
	if err != nil && (errors.Is(err, jwt.ErrTokenSignatureInvalid) || errors.Is(err, errAppleKeyNotFound)) {
		appleInvalidateKeyCache()
		claims = jwt.MapClaims{}
		seenKid = ""
		token, err = parser.ParseWithClaims(identityToken, claims, keyfunc)
	}

	// 连不上 Apple 公钥服务属于服务暂时不可用（503，客户端提示稍后重试），与无效凭证区分开
	if errors.Is(err, errAppleKeyFetch) {
		log.Printf("[apple-auth] JWKS fetch failed: %v", err)
		return "", ErrThirdPartyUnavailable
	}

	if err != nil || token == nil || !token.Valid {
		log.Printf("[apple-auth] token validation failed failure=%s error=%v kid=%s iss=%s aud=%v exp=%v token_sub=%s claimed_sub=%s",
			appleTokenFailureClass(err), appleErrorString(err), seenKid,
			appleClaimString(claims, "iss"), claims["aud"], claims["exp"],
			appleClaimString(claims, "sub"), claimedUserID)
		return "", errors.New("invalid Apple identity token")
	}

	// Nonce 校验：客户端随机串原文的 SHA-256 必须等于 token 内 nonce 声明（Apple 官方
	// 推荐做法），防止系统缓存的过期/旧 token 被重放；旧版客户端不传 nonce 时跳过以兼容
	if strings.TrimSpace(expectedNonce) != "" {
		tokenNonce, _ := claims["nonce"].(string)
		if tokenNonce != hexSHA256(expectedNonce) {
			log.Printf("[apple-auth] nonce mismatch kid=%s", seenKid)
			return "", errors.New("Apple identity token nonce mismatch")
		}
	}

	subject, _ := claims["sub"].(string)
	if subject == "" || subject != claimedUserID {
		log.Printf("[apple-auth] subject mismatch kid=%s token_sub=%s claimed_sub=%s", seenKid, subject, claimedUserID)
		return "", errors.New("Apple identity does not match user")
	}
	return subject, nil
}

// appleTokenFailureClass 把 JWT 校验失败原因归类，便于日志快速定位（如审核被拒排查）
func appleTokenFailureClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, jwt.ErrTokenExpired):
		return "expired"
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return "issuer_mismatch"
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return "audience_mismatch"
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "signature_invalid"
	case errors.Is(err, jwt.ErrTokenMalformed):
		return "malformed"
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return "not_valid_yet"
	case errors.Is(err, errAppleKeyNotFound):
		return "kid_not_found"
	case errors.Is(err, errAppleKeyFetch):
		return "jwks_fetch_failed"
	default:
		return "other"
	}
}

func appleErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func appleClaimString(claims jwt.MapClaims, key string) string {
	s, _ := claims[key].(string)
	return s
}

func hexSHA256(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

const lineJWKSURL = "https://access.line.me/oauth2/v2.1/certs"

var lineKeyCache struct {
	sync.RWMutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
}

// LineConfigResolver 由 main 启动时注入，返回（频道 ID，频道 Secret），
// 配置存数据库 runtime_config 表（改库即时生效）；解析不到时回退环境变量。
var LineConfigResolver func() (string, string)

func lineChannelID() string {
	if LineConfigResolver != nil {
		id, _ := LineConfigResolver()
		if strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	return strings.TrimSpace(os.Getenv("LINE_CHANNEL_ID"))
}

// LineIdentity LINE ID Token 验证结果
type LineIdentity struct {
	UserID  string
	Name    string
	Picture string
}

// validateLineIDToken 校验客户端 LINE SDK 返回的 ID Token（JWT）：
// 签名（LINE JWKS）、签发者、受众（频道 ID）、有效期，返回 LINE 用户身份。
func validateLineIDToken(idToken, claimedUserID string) (*LineIdentity, error) {
	channelID := lineChannelID()
	if channelID == "" {
		return nil, ErrThirdPartyUnavailable
	}
	if strings.TrimSpace(idToken) == "" || strings.TrimSpace(claimedUserID) == "" {
		return nil, errors.New("invalid LINE id token")
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithIssuer("https://access.line.me"), jwt.WithAudience(channelID))
	token, err := parser.ParseWithClaims(idToken, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("unsupported LINE signing method")
		}
		kid, _ := token.Header["kid"].(string)
		return lineSigningKey(kid)
	})
	if err != nil || token == nil || !token.Valid {
		return nil, errors.New("invalid LINE id token")
	}
	subject, _ := claims["sub"].(string)
	if subject == "" || subject != claimedUserID {
		return nil, errors.New("LINE identity does not match user")
	}
	name, _ := claims["name"].(string)
	picture, _ := claims["picture"].(string)
	return &LineIdentity{UserID: subject, Name: name, Picture: picture}, nil
}

func lineSigningKey(kid string) (*rsa.PublicKey, error) {
	lineKeyCache.RLock()
	if key, ok := lineKeyCache.keys[kid]; ok && time.Now().Before(lineKeyCache.expires) {
		lineKeyCache.RUnlock()
		return key, nil
	}
	lineKeyCache.RUnlock()

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(lineJWKSURL)
	if err != nil {
		return nil, fmt.Errorf("LINE key fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("LINE key fetch returned %d", resp.StatusCode)
	}
	var set appleJWKSet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("LINE key response invalid: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, item := range set.Keys {
		if item.KID == "" || item.Kty != "RSA" || item.Use != "sig" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(item.N)
		e, errE := base64.RawURLEncoding.DecodeString(item.E)
		if errN != nil || errE != nil || len(e) == 0 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		if exponent == 0 {
			continue
		}
		keys[item.KID] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
	}
	key, ok := keys[kid]
	if !ok {
		return nil, errors.New("LINE signing key not found")
	}
	lineKeyCache.Lock()
	lineKeyCache.keys = keys
	lineKeyCache.expires = time.Now().Add(6 * time.Hour)
	lineKeyCache.Unlock()
	return key, nil
}

func appleSigningKey(kid string) (*rsa.PublicKey, error) {
	appleKeyCache.RLock()
	if key, ok := appleKeyCache.keys[kid]; ok && time.Now().Before(appleKeyCache.expires) {
		appleKeyCache.RUnlock()
		return key, nil
	}
	appleKeyCache.RUnlock()

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(appleJWKSURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAppleKeyFetch, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%w: status %d", errAppleKeyFetch, resp.StatusCode)
	}
	var set appleJWKSet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("%w: %v", errAppleKeyFetch, err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, item := range set.Keys {
		if item.KID == "" || item.Kty != "RSA" || item.Use != "sig" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(item.N)
		e, errE := base64.RawURLEncoding.DecodeString(item.E)
		if errN != nil || errE != nil || len(e) == 0 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		if exponent == 0 {
			continue
		}
		keys[item.KID] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
	}
	key, ok := keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: kid=%s", errAppleKeyNotFound, kid)
	}
	appleKeyCache.Lock()
	appleKeyCache.keys = keys
	appleKeyCache.expires = time.Now().Add(6 * time.Hour)
	appleKeyCache.Unlock()
	return key, nil
}

// appleInvalidateKeyCache 使 JWKS 缓存失效，强制下一次取密钥时向 Apple 重新拉取，
// 用于 Apple 轮换签名密钥导致签名校验失败时的自愈重试
func appleInvalidateKeyCache() {
	appleKeyCache.Lock()
	appleKeyCache.expires = time.Time{}
	appleKeyCache.Unlock()
}
