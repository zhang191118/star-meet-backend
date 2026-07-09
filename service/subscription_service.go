package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"star-meet-backend/model/database"

	"github.com/spf13/viper"
	"gorm.io/gorm"
)

var ErrInvalidReceiptFormat = errors.New("invalid receipt format")

type SubscriptionService struct {
	DB *gorm.DB
}

type AppleReceiptVerification struct {
	Environment           string
	OriginalTransactionID string
}

var appleVerifyReceiptRequestFunc = verifyAppleReceiptRequest

func NewSubscriptionService(db *gorm.DB) *SubscriptionService {
	return &SubscriptionService{DB: db}
}

func (s *SubscriptionService) GetPlans() ([]database.Plan, error) {
	var plans []database.Plan
	if err := s.DB.Order("price ASC").Find(&plans).Error; err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return s.defaultPlans(), nil
	}
	return plans, nil
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

func (s *SubscriptionService) CreateSubscription(userID, planID string, environment string, appleOriginalTransactionID string) (*database.Subscription, error) {
	plan, err := s.GetPlan(planID)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()
	endTime := startTime.AddDate(0, 0, plan.Duration)

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
		if err := tx.Where("user_id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		if appleOriginalTransactionID != "" {
			var existing database.Subscription
			err := tx.Where(
				"user_id = ? AND plan_id = ? AND apple_original_transaction_id = ? AND environment = ? AND status = ?",
				userID,
				planID,
				appleOriginalTransactionID,
				environment,
				"active",
			).First(&existing).Error
			if err == nil {
				subscription = &existing
				return tx.Model(&database.User{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
					"member_level":  plan.Level,
					"member_expire": existing.EndTime,
				}).Error
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
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
	if sharedSecret := appleSharedSecret(); sharedSecret != "" {
		payload["password"] = sharedSecret
	}

	verifyURL := appleProductionVerifyURL
	if isSandbox {
		verifyURL = appleSandboxVerifyURL
	}

	resp, err := appleVerifyReceiptRequestFunc(verifyURL, payload)
	if err != nil {
		return nil, err
	}

	switch resp.Status {
	case 21007:
		resp, err = appleVerifyReceiptRequestFunc(appleSandboxVerifyURL, payload)
	case 21008:
		resp, err = appleVerifyReceiptRequestFunc(appleProductionVerifyURL, payload)
	}
	if err != nil {
		return nil, err
	}
	if resp.Status != 0 {
		return nil, fmt.Errorf("Apple 验证失败，status=%d", resp.Status)
	}

	item, err := resp.latestTransaction()
	if err != nil {
		return nil, err
	}
	if item.ProductID != plan.AppleProductID {
		return nil, fmt.Errorf("receipt 对应商品 %s 与套餐 %s 不匹配", item.ProductID, plan.AppleProductID)
	}
	if item.OriginalTransactionID == "" {
		return nil, fmt.Errorf("receipt 缺少原始交易号")
	}

	environment := resp.Environment
	if environment == "" {
		if isSandbox {
			environment = "Sandbox"
		} else {
			environment = "Production"
		}
	}

	return &AppleReceiptVerification{
		Environment:           environment,
		OriginalTransactionID: item.OriginalTransactionID,
	}, nil
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
	InApp []appleReceiptItem `json:"in_app"`
}

type appleReceiptItem struct {
	ProductID             string `json:"product_id"`
	OriginalTransactionID string `json:"original_transaction_id"`
	TransactionID         string `json:"transaction_id"`
	ExpiresDateMs         string `json:"expires_date_ms"`
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
	candidates := make([]appleReceiptItem, 0, len(r.LatestReceiptInfo)+len(r.Receipt.InApp)+1)
	candidates = append(candidates, r.LatestReceiptInfo...)
	candidates = append(candidates, r.Receipt.InApp...)
	if r.LatestExpiredReceiptInfo.OriginalTransactionID != "" {
		candidates = append(candidates, r.LatestExpiredReceiptInfo)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("receipt 中没有可用的交易记录")
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
		{
			PlanID:         "monthly",
			Name:           "月卡",
			Description:    "开通VIP享无限特权",
			Level:          "monthly",
			AppleProductID: "com.starmeet.monthly",
			Price:          30,
			Currency:       "CNY",
			Duration:       30,
		},
		{
			PlanID:         "quarterly",
			Name:           "季卡",
			Description:    "季卡优惠限时特惠",
			Level:          "quarterly",
			AppleProductID: "com.starmeet.quarterly",
			Price:          78,
			Currency:       "CNY",
			Duration:       90,
		},
		{
			PlanID:         "yearly",
			Name:           "年卡",
			Description:    "年卡超值全年最低价",
			Level:          "yearly",
			AppleProductID: "com.starmeet.yearly",
			Price:          268,
			Currency:       "CNY",
			Duration:       365,
		},
		{
			PlanID:         "lifetime",
			Name:           "终身会员",
			Description:    "一次开通长期有效",
			Level:          "lifetime",
			AppleProductID: "com.starmeet.lifetime",
			Price:          998,
			Currency:       "CNY",
			Duration:       36500,
		},
	}
}
