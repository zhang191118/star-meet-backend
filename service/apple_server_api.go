package service

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

// App Store Server API（Apple 官方推荐的交易验证方式，替代 verifyReceipt + 共享密钥）
const (
	appStoreAPIProduction = "https://api.storekit.itunes.apple.com"
	appStoreAPISandbox    = "https://api.storekit-sandbox.itunes.apple.com"
)

var appleServerAPITokenFunc = appleServerAPIToken
var appleServerAPICredentialsConfiguredFunc = appleServerAPICredentialsConfigured

func appleIAPIssuerID() string {
	if v := viper.GetString("apple.issuer_id"); v != "" {
		return v
	}
	return os.Getenv("APPLE_IAP_ISSUER_ID")
}

func appleIAPKeyID() string {
	if v := viper.GetString("apple.key_id"); v != "" {
		return v
	}
	return os.Getenv("APPLE_IAP_KEY_ID")
}

func appleIAPKeyPath() string {
	if v := viper.GetString("apple.private_key_path"); v != "" {
		return v
	}
	return os.Getenv("APPLE_IAP_KEY_PATH")
}

func loadAppleIAPKey() (*ecdsa.PrivateKey, error) {
	path := appleIAPKeyPath()
	if path == "" {
		return nil, errors.New("APPLE_IAP_KEY_PATH 未配置")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 App Store Server API 密钥失败: %w", err)
	}

	// .p8 可能是裸 PKCS#8，也可能带 PEM 包裹
	if block, _ := pem.Decode(data); block != nil {
		data = block.Bytes
	}
	parsed, err := x509.ParsePKCS8PrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("解析 App Store Server API 密钥失败: %w", err)
	}
	ecKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("App Store Server API 密钥不是 EC 密钥")
	}
	return ecKey, nil
}

func appleServerAPIToken() (string, error) {
	issuer := appleIAPIssuerID()
	keyID := appleIAPKeyID()
	if issuer == "" || keyID == "" {
		return "", errors.New("APPLE_IAP_ISSUER_ID / APPLE_IAP_KEY_ID 未配置")
	}
	key, err := loadAppleIAPKey()
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": issuer,
		"iat": now.Unix(),
		"exp": now.Add(50 * time.Minute).Unix(),
		"aud": "appstoreconnect-v1",
		"bid": appleBundleID(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID
	token.Header["typ"] = "JWT"
	return token.SignedString(key)
}

func appleServerAPICredentialsConfigured() bool {
	return strings.TrimSpace(appleIAPIssuerID()) != "" &&
		strings.TrimSpace(appleIAPKeyID()) != "" &&
		strings.TrimSpace(appleIAPKeyPath()) != ""
}

func appleBundleID() string {
	if v := viper.GetString("apple.bundle_id"); v != "" {
		return v
	}
	if v := os.Getenv("APPLE_BUNDLE_ID"); v != "" {
		return v
	}
	return "com.guoliang.starmeet"
}

type subscriptionStatusesResponse struct {
	Environment string                    `json:"environment"`
	BundleID    string                    `json:"bundleId"`
	Data        []subscriptionStatusGroup `json:"data"`
}

type subscriptionStatusGroup struct {
	SubscriptionGroupIdentifier string                        `json:"subscriptionGroupIdentifier"`
	LastTransactions            []subscriptionLastTransaction `json:"lastTransactions"`
}

type subscriptionLastTransaction struct {
	OriginalTransactionID string `json:"originalTransactionId"`
	TransactionID         string `json:"transactionId"`
	Status                int    `json:"status"`
	SignedRenewalInfo     string `json:"signedRenewalInfo"`
	SignedTransactionInfo string `json:"signedTransactionInfo"`
}

func fetchSubscriptionStatuses(baseURL, token, originalTransactionID string) (*subscriptionStatusesResponse, int, error) {
	endpoint := fmt.Sprintf("%s/inApps/v1/subscriptions/%s", baseURL, url.PathEscape(originalTransactionID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("App Store Server API HTTP %d: %s", resp.StatusCode, string(body))
	}
	var parsed subscriptionStatusesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, resp.StatusCode, err
	}
	return &parsed, resp.StatusCode, nil
}

// fetchSubscriptionStatusesWithRetry：沙盒新交易存在传播延迟（404），等待后重试
func fetchSubscriptionStatusesWithRetry(baseURL, token, originalTransactionID string) (*subscriptionStatusesResponse, error) {
	waits := []time.Duration{0, 6 * time.Second, 15 * time.Second}
	var lastErr error
	for _, wait := range waits {
		if wait > 0 {
			log.Printf("[apple-debug] server-api 404，%v 后重试", wait)
			time.Sleep(wait)
		}
		statuses, statusCode, err := fetchSubscriptionStatuses(baseURL, token, originalTransactionID)
		if err == nil {
			return statuses, nil
		}
		lastErr = err
		if statusCode != http.StatusNotFound {
			return nil, err
		}
	}
	return nil, lastErr
}

// VerifyViaServerAPI 用原始交易号通过 App Store Server API 验证订阅。
// 先查生产环境，仅在生产返回 404 时切沙盒；认证和其他失败都直接关闭授予。
func (s *SubscriptionService) VerifyViaServerAPI(originalTransactionID, planID string) (*AppleReceiptVerification, error) {
	originalTransactionID = strings.TrimSpace(originalTransactionID)
	if originalTransactionID == "" {
		return nil, errors.New("缺少原始交易号")
	}
	token, err := appleServerAPITokenFunc()
	if err != nil {
		return nil, err
	}

	plan, err := s.GetPlan(planID)
	if err != nil {
		return nil, err
	}

	expectedEnvironment := "Production"
	statuses, statusCode, err := fetchSubscriptionStatuses(appStoreAPIProduction, token, originalTransactionID)
	if err != nil && statusCode == http.StatusNotFound {
		log.Printf("[apple-debug] server-api production 查询失败(%d)，切换沙盒", statusCode)
		statuses, err = fetchSubscriptionStatusesWithRetry(appStoreAPISandbox, token, originalTransactionID)
		expectedEnvironment = "Sandbox"
	}
	if err != nil {
		return nil, err
	}

	verification, err := verificationFromSubscriptionStatuses(
		statuses,
		expectedEnvironment,
		originalTransactionID,
		plan.AppleProductID,
		time.Now(),
	)
	if err != nil {
		return nil, err
	}
	log.Printf("[apple-debug] server-api 验证成功 product=%s env=%s expires=%s",
		plan.AppleProductID, verification.Environment, verification.ExpiresAt.Format(time.RFC3339))
	return verification, nil
}

func verificationFromSubscriptionStatuses(
	statuses *subscriptionStatusesResponse,
	expectedEnvironment string,
	originalTransactionID string,
	productID string,
	now time.Time,
) (*AppleReceiptVerification, error) {
	originalTransactionID = strings.TrimSpace(originalTransactionID)
	productID = strings.TrimSpace(productID)
	if statuses == nil {
		return nil, errors.New("App Store 返回空响应")
	}
	responseEnvironment, err := normalizeAppleEnvironment(statuses.Environment)
	if err != nil {
		return nil, fmt.Errorf("App Store 返回未知环境: %s", statuses.Environment)
	}
	expectedEnvironment, err = normalizeAppleEnvironment(expectedEnvironment)
	if err != nil {
		return nil, err
	}
	if responseEnvironment != expectedEnvironment {
		return nil, fmt.Errorf("App Store 返回环境不一致: got %s want %s", responseEnvironment, expectedEnvironment)
	}
	if strings.TrimSpace(statuses.BundleID) != appleBundleID() {
		return nil, fmt.Errorf("App Store 返回 bundleId 不匹配: %s", statuses.BundleID)
	}
	if len(statuses.Data) == 0 {
		return nil, errors.New("App Store 未返回任何订阅状态")
	}

	var selected *AppleReceiptVerification
	revoked := false
	observedStatuses := make(map[int]bool)
	for _, group := range statuses.Data {
		for _, last := range group.LastTransactions {
			lastOriginalTransactionID := strings.TrimSpace(last.OriginalTransactionID)
			if lastOriginalTransactionID != "" && lastOriginalTransactionID != originalTransactionID {
				// This endpoint returns every subscription group for the
				// customer. Unrelated groups must not block or satisfy the
				// transaction being restored.
				continue
			}
			if strings.TrimSpace(last.SignedTransactionInfo) == "" {
				return nil, errors.New("App Store 未返回 signedTransactionInfo")
			}

			txn, err := appleJWSTransactionVerifier(last.SignedTransactionInfo)
			if err != nil {
				return nil, fmt.Errorf("验证 signedTransactionInfo 失败: %w", err)
			}
			if txn == nil {
				return nil, errors.New("App Store 返回空 signedTransactionInfo")
			}
			if strings.TrimSpace(txn.OriginalTransactionID) != originalTransactionID {
				continue
			}
			if txn.ProductID != productID {
				continue
			}
			if txn.BundleID != appleBundleID() {
				return nil, fmt.Errorf("App Store 返回的交易 bundleId 不匹配: %s", txn.BundleID)
			}
			txnEnvironment, err := normalizeAppleEnvironment(txn.Environment)
			if err != nil || txnEnvironment != responseEnvironment {
				return nil, fmt.Errorf("App Store 返回的交易环境不一致: %s", txn.Environment)
			}
			if strings.EqualFold(txn.Type, "consumable") {
				return nil, errors.New("App Store 返回了非订阅交易")
			}

			observedStatuses[last.Status] = true
			if last.Status == 5 {
				revoked = true
				continue
			}
			if last.Status != 1 && last.Status != 4 {
				continue
			}
			if txn.RevocationDate > 0 {
				revoked = true
				continue
			}

			expiresAt := time.UnixMilli(txn.ExpiresDate)
			appAccountToken := strings.TrimSpace(txn.AppAccountToken)
			if last.Status == 4 {
				if strings.TrimSpace(last.SignedRenewalInfo) == "" {
					return nil, errors.New("宽限期缺少 signedRenewalInfo")
				}
				renewal, err := appleJWSRenewalInfoVerifier(last.SignedRenewalInfo)
				if err != nil {
					return nil, fmt.Errorf("验证 signedRenewalInfo 失败: %w", err)
				}
				if renewal == nil {
					return nil, errors.New("App Store 返回空 signedRenewalInfo")
				}
				if renewal.OriginalTransactionID != "" && strings.TrimSpace(renewal.OriginalTransactionID) != originalTransactionID {
					return nil, errors.New("续订信息与原交易号不一致")
				}
				if renewal.ProductID != "" && renewal.ProductID != productID {
					return nil, errors.New("续订信息与商品不一致")
				}
				if renewal.Environment != "" {
					renewalEnvironment, normalizeErr := normalizeAppleEnvironment(renewal.Environment)
					if normalizeErr != nil || renewalEnvironment != responseEnvironment {
						return nil, fmt.Errorf("续订信息环境不一致: %s", renewal.Environment)
					}
				}
				expiresAt = time.UnixMilli(renewal.GracePeriodExpiresDate)
				if appAccountToken == "" {
					appAccountToken = strings.TrimSpace(renewal.AppAccountToken)
				}
			}
			if txn.ExpiresDate <= 0 && last.Status == 1 {
				continue
			}
			if !expiresAt.After(now) {
				continue
			}

			candidate := &AppleReceiptVerification{
				Environment:           responseEnvironment,
				OriginalTransactionID: originalTransactionID,
				ExpiresAt:             expiresAt,
				AppAccountToken:       appAccountToken,
			}
			if selected == nil || candidate.ExpiresAt.After(selected.ExpiresAt) {
				selected = candidate
			}
		}
	}

	if selected != nil {
		return selected, nil
	}
	if revoked {
		return nil, ErrAppleReceiptRevoked
	}
	statusCodes := make([]int, 0, len(observedStatuses))
	for status := range observedStatuses {
		statusCodes = append(statusCodes, status)
	}
	sort.Ints(statusCodes)
	statusList := make([]string, 0, len(statusCodes))
	for _, status := range statusCodes {
		statusList = append(statusList, fmt.Sprintf("%d", status))
	}
	if len(statusList) == 0 {
		return nil, fmt.Errorf("未找到商品 %s 的订阅交易", productID)
	}
	return nil, fmt.Errorf("商品 %s 没有可授予的订阅状态: %s", productID, strings.Join(statusList, ","))
}
