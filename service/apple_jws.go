package service

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"star-meet-backend/model/database"
)

//go:embed apple_root_g3.cer
var appleRootG3DER []byte

var appleRootG3 *x509.Certificate

func init() {
	cert, err := x509.ParseCertificate(appleRootG3DER)
	if err != nil {
		log.Printf("[apple-jws] 解析内嵌 Apple Root CA 失败: %v", err)
		return
	}
	appleRootG3 = cert
}

type jwsHeader struct {
	Algorithm string   `json:"alg"`
	X5c       []string `json:"x5c"`
}

var appleJWSTransactionVerifier = verifyAppleJWSTransaction
var appleJWSRenewalInfoVerifier = verifyAppleJWSRenewalInfo

// jwsTransactionPayload 对应 Apple JWSTransaction 的关键字段
type jwsTransactionPayload struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	Environment           string `json:"environment"`
	SignedDate            int64  `json:"signedDate"`
	Type                  string `json:"type"`
	ExpiresDate           int64  `json:"expiresDate"`
	RevocationDate        int64  `json:"revocationDate"`
	AppAccountToken       string `json:"appAccountToken"`
}

type jwsRenewalInfo struct {
	OriginalTransactionID  string `json:"originalTransactionId"`
	ProductID              string `json:"productId"`
	Environment            string `json:"environment"`
	SignedDate             int64  `json:"signedDate"`
	GracePeriodExpiresDate int64  `json:"gracePeriodExpiresDate"`
	IsInBillingRetryPeriod bool   `json:"isInBillingRetryPeriod"`
	AppAccountToken        string `json:"appAccountToken"`
}

// verifyAppleJWSTransaction 本地验证 App Store JWS 交易（Apple 官方离线验签方案）：
// x5c 证书链必须终止于内嵌的 Apple Root CA - G3，ES256 签名覆盖 header.payload。
func verifyAppleJWSTransaction(signed string) (*jwsTransactionPayload, error) {
	payloadJSON, err := verifyAndDecodeAppleJWS(signed)
	if err != nil {
		return nil, err
	}

	var payload jwsTransactionPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("解析交易数据失败: %w", err)
	}
	return &payload, nil
}

func verifyAppleJWSRenewalInfo(signed string) (*jwsRenewalInfo, error) {
	payloadJSON, err := verifyAndDecodeAppleJWS(signed)
	if err != nil {
		return nil, err
	}

	var payload jwsRenewalInfo
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("解析续订信息失败: %w", err)
	}
	return &payload, nil
}

func verifyAndDecodeAppleJWS(signed string) ([]byte, error) {
	parts := strings.Split(signed, ".")
	if len(parts) != 3 {
		return nil, errors.New("JWS 格式错误")
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("解码 JWS header 失败: %w", err)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("解码 JWS payload 失败: %w", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("解码 JWS 签名失败: %w", err)
	}

	var header jwsHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("解析 JWS header 失败: %w", err)
	}
	if header.Algorithm != "ES256" {
		return nil, fmt.Errorf("不支持的签名算法: %s", header.Algorithm)
	}
	if len(header.X5c) < 2 {
		return nil, errors.New("JWS 缺少证书链")
	}

	certs := make([]*x509.Certificate, 0, len(header.X5c))
	for _, raw := range header.X5c {
		der, decErr := base64.StdEncoding.DecodeString(raw)
		if decErr != nil {
			return nil, fmt.Errorf("解码证书失败: %w", decErr)
		}
		cert, parseErr := x509.ParseCertificate(der)
		if parseErr != nil {
			return nil, fmt.Errorf("解析证书失败: %w", parseErr)
		}
		certs = append(certs, cert)
	}
	leaf := certs[0]

	// 链尾必须是指纹校验过的 Apple Root CA - G3（防止伪造证书链）
	if appleRootG3 == nil {
		return nil, errors.New("Apple Root CA 未初始化")
	}
	last := certs[len(certs)-1]
	if !last.Equal(appleRootG3) {
		return nil, errors.New("证书链根证书不是 Apple Root CA - G3")
	}

	// ES256 签名验证：叶子证书公钥签过 header.payload
	pubKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("叶子证书不是 EC 密钥")
	}
	if len(signature) != 64 {
		return nil, errors.New("JWS 签名长度错误")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(pubKey, digest[:], r, s) {
		return nil, errors.New("JWS 签名验证失败")
	}

	var timing struct {
		SignedDate int64 `json:"signedDate"`
	}
	if err := json.Unmarshal(payloadJSON, &timing); err != nil {
		return nil, fmt.Errorf("解析 JWS 载荷失败: %w", err)
	}

	// 以交易签名时间为基准验证证书链有效期（签名证书寿命短，用当前时间会误判过期）
	signedAt := time.UnixMilli(timing.SignedDate)
	if timing.SignedDate <= 0 {
		signedAt = time.Now()
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1 : len(certs)-1] {
		intermediates.AddCert(cert)
	}
	roots := x509.NewCertPool()
	roots.AddCert(appleRootG3)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Intermediates: intermediates,
		Roots:         roots,
		CurrentTime:   signedAt,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, fmt.Errorf("证书链验证失败: %w", err)
	}

	return payloadJSON, nil
}

// verifyAppleJWSIdentity verifies that a signed transaction belongs to the
// requested plan and originates from an Apple Store environment. Expiry and
// revocation deliberately remain an online decision: this payload identifies
// the transaction, while ConfirmApplePurchase obtains the current entitlement
// state from Apple.
func (s *SubscriptionService) verifyAppleJWSIdentity(signedJWS, planID string) (*jwsTransactionPayload, *database.Plan, error) {
	plan, err := s.GetPlan(planID)
	if err != nil {
		return nil, nil, err
	}

	payload, err := appleJWSTransactionVerifier(signedJWS)
	if err != nil {
		return nil, nil, err
	}
	if payload == nil {
		return nil, nil, errors.New("交易数据为空")
	}

	if payload.BundleID != appleBundleID() {
		return nil, nil, fmt.Errorf("%w: 交易 bundleId 不匹配: %s", ErrAppleJWSRejected, payload.BundleID)
	}
	if payload.ProductID != plan.AppleProductID {
		return nil, nil, fmt.Errorf("%w: 交易商品不匹配: %s", ErrAppleJWSRejected, payload.ProductID)
	}
	if payload.SignedDate > time.Now().Add(5*time.Minute).UnixMilli() {
		return nil, nil, fmt.Errorf("%w: 交易签名时间异常", ErrAppleJWSRejected)
	}
	if strings.TrimSpace(payload.OriginalTransactionID) == "" {
		return nil, nil, fmt.Errorf("%w: 交易缺少原始交易号", ErrAppleJWSRejected)
	}
	if strings.EqualFold(payload.Type, "consumable") {
		return nil, nil, fmt.Errorf("%w: 交易类型不是订阅", ErrAppleJWSRejected)
	}
	if _, err := normalizeAppleEnvironment(payload.Environment); err != nil {
		return nil, nil, fmt.Errorf("%w: 未知交易环境: %s", ErrAppleJWSRejected, payload.Environment)
	}

	return payload, plan, nil
}

// VerifyViaJWS validates a signed transaction without contacting Apple. It is
// useful for diagnostics, but it must not be used as the authority for granting
// or renewing an entitlement. Use ConfirmApplePurchase for purchases.
func (s *SubscriptionService) VerifyViaJWS(signedJWS, planID string) (*AppleReceiptVerification, error) {
	payload, plan, err := s.verifyAppleJWSIdentity(signedJWS, planID)
	if err != nil {
		return nil, err
	}
	verification, err := verificationForJWSTransaction(payload, plan, time.Now())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAppleJWSRejected, err)
	}
	return verification, nil
}

func verificationForJWSTransaction(payload *jwsTransactionPayload, plan *database.Plan, now time.Time) (*AppleReceiptVerification, error) {
	if payload == nil {
		return nil, errors.New("交易数据为空")
	}
	originalTransactionID := strings.TrimSpace(payload.OriginalTransactionID)
	if originalTransactionID == "" {
		return nil, errors.New("交易缺少原始交易号")
	}
	if payload.RevocationDate > 0 {
		return nil, ErrAppleReceiptRevoked
	}
	if strings.EqualFold(payload.Type, "consumable") {
		return nil, errors.New("交易类型不是订阅")
	}
	environment, err := normalizeAppleEnvironment(payload.Environment)
	if err != nil {
		return nil, fmt.Errorf("未知交易环境: %s", payload.Environment)
	}

	if plan.Level != "lifetime" {
		if payload.ExpiresDate <= 0 {
			return nil, errors.New("订阅交易缺少到期时间")
		}
		expiresAt := time.UnixMilli(payload.ExpiresDate)
		if !expiresAt.After(now) {
			return nil, errors.New("Apple 交易已过期")
		}
		return &AppleReceiptVerification{
			Environment:           environment,
			OriginalTransactionID: originalTransactionID,
			ExpiresAt:             expiresAt,
			AppAccountToken:       strings.TrimSpace(payload.AppAccountToken),
		}, nil
	}

	return &AppleReceiptVerification{
		Environment:           environment,
		OriginalTransactionID: originalTransactionID,
		AppAccountToken:       strings.TrimSpace(payload.AppAccountToken),
	}, nil
}
