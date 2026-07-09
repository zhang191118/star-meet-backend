package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSubscriptionServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.Plan{}, &database.Subscription{}); err != nil {
		t.Fatalf("migrate subscription tables: %v", err)
	}

	return db
}

func TestGetPlansReturnsFallbackWhenDatabaseIsEmpty(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	plans, err := service.GetPlans()
	if err != nil {
		t.Fatalf("get plans: %v", err)
	}

	if len(plans) != 4 {
		t.Fatalf("expected 4 fallback plans, got %d", len(plans))
	}
	if plans[0].PlanID != "monthly" || plans[1].PlanID != "quarterly" || plans[2].PlanID != "yearly" || plans[3].PlanID != "lifetime" {
		t.Fatalf("unexpected fallback plans: %+v", plans)
	}
}

func TestCreateSubscriptionUsesFallbackPlanAndUpdatesUser(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	user := database.User{
		UserID:      "user-sub-1",
		Nickname:    "订阅用户",
		MemberLevel: "free",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	before := time.Now()
	subscription, err := service.CreateSubscription("user-sub-1", "quarterly", "Sandbox", "mock-original-txn")
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if subscription.PlanID != "quarterly" || subscription.Status != "active" {
		t.Fatalf("unexpected subscription: %+v", subscription)
	}

	var storedUser database.User
	if err := db.Where("user_id = ?", "user-sub-1").First(&storedUser).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}

	if storedUser.MemberLevel != "quarterly" {
		t.Fatalf("expected member level quarterly, got %q", storedUser.MemberLevel)
	}
	if storedUser.MemberExpire == nil {
		t.Fatal("expected member expire to be set")
	}
	if storedUser.MemberExpire.Before(before.Add(89 * 24 * time.Hour)) {
		t.Fatalf("expected quarterly expiration to be about 90 days later, got %v", storedUser.MemberExpire)
	}
}

func TestCreateSubscriptionRequiresExistingUser(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	if _, err := service.CreateSubscription("missing-user", "monthly", "Sandbox", "txn-missing-user"); err == nil {
		t.Fatal("expected missing user to fail subscription creation")
	}
}

func TestCreateSubscriptionIsIdempotentForSameTransaction(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	user := database.User{
		UserID:      "user-sub-2",
		Nickname:    "幂等订阅用户",
		MemberLevel: "free",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	first, err := service.CreateSubscription("user-sub-2", "monthly", "Sandbox", "duplicate-txn")
	if err != nil {
		t.Fatalf("create first subscription: %v", err)
	}

	second, err := service.CreateSubscription("user-sub-2", "monthly", "Sandbox", "duplicate-txn")
	if err != nil {
		t.Fatalf("create duplicate subscription: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected duplicate transaction to reuse subscription, got %d and %d", first.ID, second.ID)
	}

	var count int64
	if err := db.Model(&database.Subscription{}).Where("user_id = ?", "user-sub-2").Count(&count).Error; err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one subscription record, got %d", count)
	}
}

func TestCreateSubscriptionReplacesPreviousActiveSubscription(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	user := database.User{
		UserID:      "user-sub-3",
		Nickname:    "升级订阅用户",
		MemberLevel: "free",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	first, err := service.CreateSubscription("user-sub-3", "monthly", "Sandbox", "txn-first")
	if err != nil {
		t.Fatalf("create first subscription: %v", err)
	}
	second, err := service.CreateSubscription("user-sub-3", "yearly", "Sandbox", "txn-second")
	if err != nil {
		t.Fatalf("create second subscription: %v", err)
	}

	if first.ID == second.ID {
		t.Fatal("expected upgraded purchase to create a new subscription record")
	}

	var previous database.Subscription
	if err := db.First(&previous, first.ID).Error; err != nil {
		t.Fatalf("reload previous subscription: %v", err)
	}
	if previous.Status != "replaced" || previous.AutoRenewStatus {
		t.Fatalf("expected previous subscription to be replaced, got status=%q autoRenew=%v", previous.Status, previous.AutoRenewStatus)
	}

	active, err := service.GetActiveSubscription("user-sub-3")
	if err != nil {
		t.Fatalf("get active subscription: %v", err)
	}
	if active.ID != second.ID || active.PlanID != "yearly" {
		t.Fatalf("expected yearly subscription to be active, got %+v", active)
	}
}

func TestVerifyAppleReceiptRejectsInvalidBase64(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		t.Fatal("verify request should not be called for invalid base64")
		return nil, nil
	}

	if _, err := service.VerifyAppleReceipt("not-base64", "monthly", true); err == nil {
		t.Fatal("expected invalid base64 receipt to fail")
	}
}

func TestVerifyAppleReceiptRetriesSandboxOn21007(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	receipt := "dGVzdC1yZWNlaXB0"
	var calledURLs []string

	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		calledURLs = append(calledURLs, url)
		switch len(calledURLs) {
		case 1:
			if url != appleProductionVerifyURL {
				t.Fatalf("expected first call to production, got %s", url)
			}
			return &appleVerifyReceiptResponse{Status: 21007}, nil
		case 2:
			if url != appleSandboxVerifyURL {
				t.Fatalf("expected retry to sandbox, got %s", url)
			}
			return &appleVerifyReceiptResponse{
				Status:      0,
				Environment: "Sandbox",
				LatestReceiptInfo: []appleReceiptItem{
					{
						ProductID:             "com.starmeet.monthly",
						OriginalTransactionID: "txn-sandbox",
						TransactionID:         "txn-sandbox-1",
						ExpiresDateMs:         "100",
					},
				},
			}, nil
		default:
			t.Fatalf("unexpected extra verify call to %s", url)
			return nil, nil
		}
	}

	verification, err := service.VerifyAppleReceipt(receipt, "monthly", false)
	if err != nil {
		t.Fatalf("verify apple receipt: %v", err)
	}

	if verification.Environment != "Sandbox" || verification.OriginalTransactionID != "txn-sandbox" {
		t.Fatalf("unexpected verification result: %+v", verification)
	}
	if len(calledURLs) != 2 {
		t.Fatalf("expected 2 verify calls, got %d", len(calledURLs))
	}
}

func TestVerifyAppleReceiptRejectsMismatchedProduct(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status: 0,
			LatestReceiptInfo: []appleReceiptItem{
				{
					ProductID:             "com.starmeet.yearly",
					OriginalTransactionID: "txn-mismatch",
					TransactionID:         "txn-mismatch-1",
					ExpiresDateMs:         "100",
				},
			},
		}, nil
	}

	_, err := service.VerifyAppleReceipt("dGVzdA==", "monthly", true)
	if err == nil {
		t.Fatal("expected mismatched product to fail")
	}
	if !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestVerifyAppleReceiptRejectsAppleFailureStatus(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{Status: 21010}, nil
	}

	_, err := service.VerifyAppleReceipt("dGVzdA==", "monthly", true)
	if err == nil {
		t.Fatal("expected Apple failure status to fail")
	}
	if !strings.Contains(err.Error(), "status=21010") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestVerifyAppleReceiptRejectsMissingOriginalTransactionID(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status: 0,
			LatestReceiptInfo: []appleReceiptItem{
				{
					ProductID:     "com.starmeet.monthly",
					TransactionID: "txn-no-original",
					ExpiresDateMs: "100",
				},
			},
		}, nil
	}

	_, err := service.VerifyAppleReceipt("dGVzdA==", "monthly", true)
	if err == nil {
		t.Fatal("expected missing original transaction id to fail")
	}
	if !strings.Contains(err.Error(), "缺少原始交易号") {
		t.Fatalf("expected missing original transaction id error, got %v", err)
	}
}
