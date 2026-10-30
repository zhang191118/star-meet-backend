package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"star-meet-backend/model/database"

	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidReceiptFormat = errors.New("invalid receipt format")

var (
	// ErrAppleTransactionConflict deliberately omits the owning account so API
	// responses cannot be used to discover another user's ID.
	ErrAppleTransactionConflict = errors.New("apple transaction is already linked to another account")
	ErrAppleReceiptRevoked      = errors.New("apple transaction has been revoked")
	// ErrAppleSubscriptionSuperseded prevents an old plan's receipt from
	// becoming the active response after the same transaction was upgraded.
	ErrAppleSubscriptionSuperseded = errors.New("apple subscription was superseded by a newer purchase")
	// ErrAppleJWSRejected marks a signature-valid transaction that failed
	// semantic validation. Callers must not retry it through a weaker path.
	ErrAppleJWSRejected = errors.New("apple signed transaction rejected")
)

type SubscriptionService struct {
	DB *gorm.DB
}

type AppleReceiptVerification struct {
	Environment           string
	OriginalTransactionID string
	ExpiresAt             time.Time
	AppAccountToken       string
}

var appleVerifyReceiptRequestFunc = verifyAppleReceiptRequest

const (
	publicWeeklyPlanID  = "weekly"
	publicMonthlyPlanID = "monthly"
)

func NewSubscriptionService(db *gorm.DB) *SubscriptionService {
	return &SubscriptionService{DB: db}
}

func (s *SubscriptionService) GetPlans() ([]database.Plan, error) {
	// Keep the launch catalog independent of legacy rows that may still exist
	// for restore and receipt validation. StoreKit remains the price authority.
	// Weekly is listed first because the paywall selects it by default.
	return []database.Plan{s.weeklyPlan(), s.monthlyPlan()}, nil
}

func (s *SubscriptionService) GetPlan(planID string) (*database.Plan, error) {
	var plan database.Plan
	if err := s.DB.Where("plan_id = ?", planID).First(&plan).Error; err != nil {
		for _, fallback := range s.defaultPlans() {
			if fallback.PlanID == planID {
				plan = fallback
				return &plan, nil
			}
		}
		return nil, err
	}
	return &plan, nil
}

func (s *SubscriptionService) CreateSubscription(userID, planID string, environment string, appleOriginalTransactionID string, receiptExpiry ...time.Time) (*database.Subscription, error) {
	userID = strings.TrimSpace(userID)
	appleOriginalTransactionID = strings.TrimSpace(appleOriginalTransactionID)
	environment, err := normalizeAppleEnvironment(environment)
	if err != nil {
		return nil, err
	}
	if appleOriginalTransactionID == "" {
		return nil, fmt.Errorf("缺少原始交易号")
	}

	plan, err := s.GetPlan(planID)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()
	endTime := startTime.AddDate(0, 0, plan.Duration)
	if len(receiptExpiry) > 0 && !receiptExpiry[0].IsZero() {
		endTime = receiptExpiry[0]
	}
	if !endTime.After(startTime) {
		return nil, fmt.Errorf("Apple 交易已过期")
	}

	subscription := &database.Subscription{
		UserID:                     userID,
		PlanID:                     planID,
		StartTime:                  startTime,
		EndTime:                    endTime,
		Status:                     "active",
		Environment:                environment,
		AutoRenewStatus:            true,
		AppleOriginalTransactionID: appleOriginalTransactionID,
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var user database.User
		// Serializing on the user row keeps a concurrent account deletion from
		// removing the account between entitlement creation and commit.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		if err := claimAppleTransactionOwner(tx, userID, environment, appleOriginalTransactionID); err != nil {
			return err
		}

		var existing database.Subscription
		err := tx.Where(
			"user_id = ? AND plan_id = ? AND apple_original_transaction_id = ? AND (LOWER(environment) = ? OR environment = '' OR environment IS NULL)",
			userID,
			planID,
			appleOriginalTransactionID,
			strings.ToLower(environment),
		).Order("end_time DESC, id DESC").First(&existing).Error
		if err == nil {
			subscription = &existing
			if existing.Status == "replaced" {
				// Replaying an older receipt after an upgrade must not
				// resurrect or downgrade the newer entitlement.
				return ErrAppleSubscriptionSuperseded
			}
			if existing.EndTime.Before(endTime) {
				existing.EndTime = endTime
			}
			existing.Status = "active"
			existing.AutoRenewStatus = true
			existing.CancelReason = ""
			existing.Environment = environment
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
			return tx.Model(&database.User{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
				"member_level":  plan.Level,
				"member_expire": existing.EndTime,
			}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err := tx.Model(&database.Subscription{}).
			Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", time.Now()).
			Updates(map[string]interface{}{
				"status":            "replaced",
				"auto_renew_status": false,
				"cancel_reason":     "superseded_by_new_purchase",
			}).Error; err != nil {
			return err
		}

		if err := tx.Create(subscription).Error; err != nil {
			return err
		}

		if err := tx.Model(&database.User{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
			"member_level":  plan.Level,
			"member_expire": endTime,
		}).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return subscription, nil
}

func normalizeAppleEnvironment(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sandbox":
		return "Sandbox", nil
	case "production":
		return "Production", nil
	default:
		return "", fmt.Errorf("未知 Apple 交易环境")
	}
}

// AppleAccountTokenMatchesUser compares the StoreKit appAccountToken carried by
// a signed transaction with the authenticated user's UUID.
func AppleAccountTokenMatchesUser(token, userID string) bool {
	token = strings.TrimSpace(token)
	userID = strings.TrimSpace(userID)
	return token != "" && userID != "" && strings.EqualFold(token, userID)
}

// claimAppleTransactionOwner assigns an original transaction to one account.
// The unique index is the final authority: after an insert conflict we read the
// committed row back inside the same transaction instead of trusting our earlier
// lookup.
func claimAppleTransactionOwner(tx *gorm.DB, userID, environment, originalTransactionID string) error {
	var owner database.AppleTransactionOwner
	err := tx.Where("environment = ? AND original_transaction_id = ?", environment, originalTransactionID).
		First(&owner).Error
	if err == nil {
		if owner.UserID != userID {
			return ErrAppleTransactionConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	// Deployments that predate the ownership table already contain subscription
	// rows. Inherit a transaction only when every historical row agrees on one
	// owner; ambiguous rows require manual review instead of an arbitrary pick.
	var historicalOwners []string
	if err := tx.Model(&database.Subscription{}).
		Where(
			"apple_original_transaction_id = ? AND (LOWER(environment) = ? OR environment = '' OR environment IS NULL)",
			originalTransactionID,
			strings.ToLower(environment),
		).
		Distinct().
		Pluck("user_id", &historicalOwners).Error; err != nil {
		return err
	}
	if len(historicalOwners) > 1 {
		return ErrAppleTransactionConflict
	}
	if len(historicalOwners) == 1 && historicalOwners[0] != userID {
		return ErrAppleTransactionConflict
	}

	candidate := database.AppleTransactionOwner{
		Environment:           environment,
		OriginalTransactionID: originalTransactionID,
		UserID:                userID,
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&candidate).Error; err != nil {
		return err
	}

	owner = database.AppleTransactionOwner{}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("environment = ? AND original_transaction_id = ?", environment, originalTransactionID).
		First(&owner).Error; err != nil {
		return err
	}
	if owner.UserID != userID {
		return ErrAppleTransactionConflict
	}
	return nil
}

func (s *SubscriptionService) GetActiveSubscription(userID string) (*database.Subscription, error) {
	var sub database.Subscription
	if err := s.DB.Preload("Plan").
		Where("user_id = ? AND status = ? AND end_time > ?", userID, "active", time.Now()).
		Order("end_time DESC").
		First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *SubscriptionService) CancelSubscription(userID string) error {
	result := s.DB.Model(&database.Subscription{}).
		Where("user_id = ? AND status = ?", userID, "active").
		Update("auto_renew_status", false)

	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (s *SubscriptionService) VerifyAppleReceipt(receipt, planID string, isSandbox bool) (*AppleReceiptVerification, error) {
	if _, err := base64.StdEncoding.DecodeString(receipt); err != nil {
		return nil, ErrInvalidReceiptFormat
	}

	plan, err := s.GetPlan(planID)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"receipt-data":             receipt,
		"exclude-old-transactions": true,
	}
	passwordSet := false
	if sharedSecret := appleSharedSecret(); sharedSecret != "" {
		payload["password"] = sharedSecret
		passwordSet = true
	}

	verifyURL := appleProductionVerifyURL
	if isSandbox {
		verifyURL = appleSandboxVerifyURL
	}

	log.Printf("[apple-debug] verify start url=%s passwordSet=%v receiptLen=%d planID=%s isSandbox=%v",
		verifyURL, passwordSet, len(receipt), planID, isSandbox)

	resp, err := appleVerifyReceiptRequestFunc(verifyURL, payload)
	if err != nil {
		log.Printf("[apple-debug] verify request error: %v", err)
		return nil, err
	}

	log.Printf("[apple-debug] verify response status=%d env=%s", resp.Status, resp.Environment)

	switch resp.Status {
	case 21007:
		resp, err = appleVerifyReceiptRequestFunc(appleSandboxVerifyURL, payload)
	case 21008:
		resp, err = appleVerifyReceiptRequestFunc(appleProductionVerifyURL, payload)
	}
	if err != nil {
		return nil, err
	}
	log.Printf("[apple-debug] verify final status=%d env=%s", resp.Status, resp.Environment)
	if resp.Status != 0 {
		return nil, fmt.Errorf("Apple 验证失败，status=%d", resp.Status)
	}
	environment, err := receiptEnvironment(resp.Environment, isSandbox)
	if err != nil {
		return nil, err
	}

	item, err := resp.latestTransactionForProduct(plan.AppleProductID)
	if err != nil {
		return nil, err
	}
	if item.OriginalTransactionID == "" {
		return nil, fmt.Errorf("receipt 缺少原始交易号")
	}
	revoked, err := appleTransactionRevoked(item.RevocationDateMs)
	if err != nil {
		return nil, fmt.Errorf("receipt 撤销时间无效")
	}
	cancelled, err := appleTransactionRevoked(item.CancellationDateMs)
	if err != nil {
		return nil, fmt.Errorf("receipt 取消时间无效")
	}
	if revoked || cancelled ||
		strings.TrimSpace(item.RevocationDate) != "" ||
		strings.TrimSpace(item.CancellationDate) != "" {
		return nil, ErrAppleReceiptRevoked
	}
	if item.ExpiresDateMs != "" {
		expiresAt, parseErr := parseAppleMilliseconds(item.ExpiresDateMs)
		if parseErr != nil {
			return nil, fmt.Errorf("receipt 过期时间无效")
		}
		if !expiresAt.After(time.Now()) {
			return nil, fmt.Errorf("Apple 交易已过期")
		}
		verificationExpiry := expiresAt
		return &AppleReceiptVerification{
			Environment:           environment,
			OriginalTransactionID: item.OriginalTransactionID,
			ExpiresAt:             verificationExpiry,
			AppAccountToken:       item.AppAccountToken,
		}, nil
	}
	if plan.Level != "lifetime" {
		return nil, fmt.Errorf("订阅收据缺少有效期")
	}

	return &AppleReceiptVerification{
		Environment:           environment,
		OriginalTransactionID: item.OriginalTransactionID,
		AppAccountToken:       item.AppAccountToken,
	}, nil
}

func receiptEnvironment(value string, isSandbox bool) (string, error) {
	if strings.TrimSpace(value) != "" {
		return normalizeAppleEnvironment(value)
	}
	if isSandbox {
		return "Sandbox", nil
	}
	return "Production", nil
}

func appleTransactionRevoked(value string) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return false, nil
	}
	revokedAt, err := parseAppleMilliseconds(value)
	if err != nil {
		return false, err
	}
	return revokedAt.UnixMilli() > 0, nil
}

func parseAppleMilliseconds(value string) (time.Time, error) {
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds <= 0 {
		return time.Time{}, err
	}
	return time.UnixMilli(milliseconds), nil
}

const (
	appleProductionVerifyURL = "https://buy.itunes.apple.com/verifyReceipt"
	appleSandboxVerifyURL    = "https://sandbox.itunes.apple.com/verifyReceipt"
)

type appleVerifyReceiptResponse struct {
	Status                   int                 `json:"status"`
	Environment              string              `json:"environment"`
	LatestReceiptInfo        []appleReceiptItem  `json:"latest_receipt_info"`
	LatestExpiredReceiptInfo appleReceiptItem    `json:"latest_expired_receipt_info"`
	Receipt                  appleReceiptPayload `json:"receipt"`
}

type appleReceiptPayload struct {
	BundleID string             `json:"bundle_id"`
	InApp    []appleReceiptItem `json:"in_app"`
}

type appleReceiptItem struct {
	ProductID             string `json:"product_id"`
	OriginalTransactionID string `json:"original_transaction_id"`
	TransactionID         string `json:"transaction_id"`
	ExpiresDateMs         string `json:"expires_date_ms"`
	RevocationDate        string `json:"revocation_date"`
	RevocationDateMs      string `json:"revocation_date_ms"`
	CancellationDate      string `json:"cancellation_date"`
	CancellationDateMs    string `json:"cancellation_date_ms"`
	AppAccountToken       string `json:"app_account_token"`
}

func verifyAppleReceiptRequest(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed appleVerifyReceiptResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}

func (r appleVerifyReceiptResponse) latestTransaction() (*appleReceiptItem, error) {
	return r.latestTransactionForProduct("")
}

func (r appleVerifyReceiptResponse) latestTransactionForProduct(productID string) (*appleReceiptItem, error) {
	candidates := make([]appleReceiptItem, 0, len(r.LatestReceiptInfo)+len(r.Receipt.InApp)+1)
	candidates = append(candidates, r.LatestReceiptInfo...)
	candidates = append(candidates, r.Receipt.InApp...)
	if r.LatestExpiredReceiptInfo.OriginalTransactionID != "" {
		candidates = append(candidates, r.LatestExpiredReceiptInfo)
	}
	if productID != "" {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if candidate.ProductID == productID {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	if len(candidates) == 0 {
		if productID == "" {
			return nil, fmt.Errorf("receipt 中没有可用的交易记录")
		}
		return nil, fmt.Errorf("receipt 商品不匹配，缺少商品 %s 的交易记录", productID)
	}

	latest := candidates[0]
	for _, item := range candidates[1:] {
		if item.ExpiresDateMs > latest.ExpiresDateMs {
			latest = item
			continue
		}
		if item.ExpiresDateMs == latest.ExpiresDateMs && item.TransactionID > latest.TransactionID {
			latest = item
		}
	}

	return &latest, nil
}

func appleSharedSecret() string {
	if secret := viper.GetString("apple.shared_secret"); secret != "" {
		return secret
	}
	return os.Getenv("APPLE_SHARED_SECRET")
}

func (s *SubscriptionService) defaultPlans() []database.Plan {
	return []database.Plan{
		s.weeklyPlan(),
		s.monthlyPlan(),
		{
			PlanID:         "quarterly",
			Name:           "季卡",
			Description:    "季卡优惠限时特惠",
			Level:          "quarterly",
			AppleProductID: "com.guoliang.starmeet.quarterly",
			Price:          78,
			Currency:       "CNY",
			Duration:       90,
		},
		{
			PlanID:         "yearly",
			Name:           "年卡",
			Description:    "年卡超值全年最低价",
			Level:          "yearly",
			AppleProductID: "com.guoliang.starmeet.yearly",
			Price:          268,
			Currency:       "CNY",
			Duration:       365,
		},
		{
			PlanID:         "lifetime",
			Name:           "终身会员",
			Description:    "一次开通长期有效",
			Level:          "lifetime",
			AppleProductID: "com.guoliang.starmeet.lifetime",
			Price:          998,
			Currency:       "CNY",
			Duration:       36500,
		},
	}
}

func (s *SubscriptionService) weeklyPlan() database.Plan {
	return database.Plan{
		PlanID:         publicWeeklyPlanID,
		Name:           "StarMeet VIP",
		Description:    "无限聊天，解锁全部官方角色，优先体验后续陪伴功能",
		Level:          "weekly",
		AppleProductID: "com.guoliang.starmeet.weekly",
		Price:          90,
		Currency:       "TWD",
		Duration:       7,
	}
}

func (s *SubscriptionService) monthlyPlan() database.Plan {
	return database.Plan{
		PlanID:         publicMonthlyPlanID,
		Name:           "StarMeet VIP",
		Description:    "无限聊天，解锁全部官方角色，优先体验后续陪伴功能",
		Level:          "monthly",
		AppleProductID: "com.guoliang.starmeet.monthly",
		Price:          170,
		Currency:       "TWD",
		Duration:       30,
	}
}
