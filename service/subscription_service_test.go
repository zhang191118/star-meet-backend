package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
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

	if err := db.AutoMigrate(&database.User{}, &database.Plan{}, &database.Subscription{}, &database.AppleTransactionOwner{}); err != nil {
		t.Fatalf("migrate subscription tables: %v", err)
	}

	return db
}

func futureAppleExpiry() string {
	return fmt.Sprintf("%d", time.Now().Add(24*time.Hour).UnixMilli())
}

func TestGetPlansReturnsThePublicWeeklyAndMonthlyPlans(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	if err := db.Create(&database.Plan{
		PlanID:         "yearly",
		Name:           "Legacy yearly",
		Level:          "yearly",
		AppleProductID: "com.guoliang.starmeet.yearly",
		Price:          268,
		Currency:       "CNY",
		Duration:       365,
	}).Error; err != nil {
		t.Fatalf("create legacy plan: %v", err)
	}
	service := NewSubscriptionService(db)

	plans, err := service.GetPlans()
	if err != nil {
		t.Fatalf("get plans: %v", err)
	}

	if len(plans) != 2 {
		t.Fatalf("expected two public plans, got %d", len(plans))
	}
	// Weekly is listed first because the paywall selects it by default.
	if plans[0].PlanID != "weekly" || plans[0].Level != "weekly" || plans[0].AppleProductID != "com.guoliang.starmeet.weekly" || plans[0].Price != 90 || plans[0].Currency != "TWD" || plans[0].Duration != 7 {
		t.Fatalf("unexpected weekly plan: %+v", plans[0])
	}
	if plans[1].PlanID != "monthly" || plans[1].Name != "StarMeet VIP" || plans[1].Price != 170 || plans[1].Currency != "TWD" {
		t.Fatalf("unexpected monthly plan: %+v", plans[1])
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
						ProductID:             "com.guoliang.starmeet.monthly",
						OriginalTransactionID: "txn-sandbox",
						TransactionID:         "txn-sandbox-1",
						ExpiresDateMs:         futureAppleExpiry(),
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
					ProductID:             "com.guoliang.starmeet.yearly",
					OriginalTransactionID: "txn-mismatch",
					TransactionID:         "txn-mismatch-1",
					ExpiresDateMs:         futureAppleExpiry(),
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

func TestVerifyAppleReceiptSelectsLatestTransactionForRequestedProduct(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Production",
			LatestReceiptInfo: []appleReceiptItem{
				{
					ProductID:             "com.guoliang.starmeet.monthly",
					OriginalTransactionID: "monthly-original",
					TransactionID:         "monthly-old",
					ExpiresDateMs:         futureAppleExpiry(),
				},
				{
					ProductID:             "com.guoliang.starmeet.yearly",
					OriginalTransactionID: "yearly-original",
					TransactionID:         "yearly-new",
					ExpiresDateMs:         futureAppleExpiry(),
				},
			},
		}, nil
	}

	verification, err := service.VerifyAppleReceipt("dGVzdA==", "monthly", false)
	if err != nil {
		t.Fatalf("verify receipt: %v", err)
	}
	if verification.OriginalTransactionID != "monthly-original" {
		t.Fatalf("expected monthly transaction, got %q", verification.OriginalTransactionID)
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
					ProductID:     "com.guoliang.starmeet.monthly",
					TransactionID: "txn-no-original",
					ExpiresDateMs: futureAppleExpiry(),
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

func TestVerifyAppleReceiptAcceptsWeeklyTrialExpiry(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)

	trialEnd := time.Now().Add(3 * 24 * time.Hour)

	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			LatestReceiptInfo: []appleReceiptItem{
				{
					ProductID:             "com.guoliang.starmeet.weekly",
					OriginalTransactionID: "txn-weekly-trial",
					TransactionID:         "txn-weekly-trial-1",
					ExpiresDateMs:         fmt.Sprintf("%d", trialEnd.UnixMilli()),
				},
			},
		}, nil
	}

	verification, err := service.VerifyAppleReceipt("dGVzdA==", "weekly", true)
	if err != nil {
		t.Fatalf("verify weekly trial receipt: %v", err)
	}
	if verification.OriginalTransactionID != "txn-weekly-trial" {
		t.Fatalf("unexpected transaction id: %q", verification.OriginalTransactionID)
	}
	if diff := verification.ExpiresAt.Sub(trialEnd); diff < -time.Second || diff > time.Second {
		t.Fatalf("expected expiry to match trial end, got %v want %v", verification.ExpiresAt, trialEnd)
	}

	user := database.User{UserID: "user-weekly-trial", Nickname: "周卡试用用户", MemberLevel: "free"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	subscription, err := service.CreateSubscription("user-weekly-trial", "weekly", verification.Environment, verification.OriginalTransactionID, verification.ExpiresAt)
	if err != nil {
		t.Fatalf("create weekly subscription: %v", err)
	}
	if subscription.PlanID != "weekly" || subscription.Status != "active" {
		t.Fatalf("unexpected subscription: %+v", subscription)
	}

	var storedUser database.User
	if err := db.Where("user_id = ?", "user-weekly-trial").First(&storedUser).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if storedUser.MemberLevel != "weekly" {
		t.Fatalf("expected member level weekly, got %q", storedUser.MemberLevel)
	}
	if storedUser.MemberExpire == nil {
		t.Fatal("expected member expire to be set")
	}
	if diff := storedUser.MemberExpire.Sub(trialEnd); diff < -time.Second || diff > time.Second {
		t.Fatalf("expected member expire at trial end, got %v want %v", storedUser.MemberExpire, trialEnd)
	}
}

func createSubscriptionTestUser(t *testing.T, db *gorm.DB, userID string) {
	t.Helper()
	if err := db.Create(&database.User{UserID: userID, Email: userID + "@example.test"}).Error; err != nil {
		t.Fatalf("create user %s: %v", userID, err)
	}
}

func TestCreateSubscriptionRejectsTransactionOwnedByAnotherUser(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	createSubscriptionTestUser(t, db, "owner")
	createSubscriptionTestUser(t, db, "other")
	expiry := time.Now().Add(30 * 24 * time.Hour)

	if _, err := service.CreateSubscription("owner", "monthly", "Sandbox", "shared-original", expiry); err != nil {
		t.Fatalf("create owner subscription: %v", err)
	}
	_, err := service.CreateSubscription("other", "monthly", "Sandbox", "shared-original", expiry)
	if !errors.Is(err, ErrAppleTransactionConflict) {
		t.Fatalf("expected ownership conflict, got %v", err)
	}

	var otherSubscriptions int64
	if err := db.Model(&database.Subscription{}).Where("user_id = ?", "other").Count(&otherSubscriptions).Error; err != nil {
		t.Fatal(err)
	}
	if otherSubscriptions != 0 {
		t.Fatalf("conflicting claim created %d subscriptions", otherSubscriptions)
	}
	var other database.User
	if err := db.Where("user_id = ?", "other").First(&other).Error; err != nil {
		t.Fatal(err)
	}
	if other.MemberLevel != "free" || other.MemberExpire != nil {
		t.Fatalf("conflicting claim changed entitlement: %+v", other)
	}
}

func TestCreateSubscriptionInheritsUniqueHistoricalOwner(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	createSubscriptionTestUser(t, db, "legacy-owner")
	createSubscriptionTestUser(t, db, "legacy-other")
	expiry := time.Now().Add(20 * 24 * time.Hour)
	if err := db.Create(&database.Subscription{
		UserID:                     "legacy-owner",
		PlanID:                     "monthly",
		EndTime:                    expiry,
		Status:                     "active",
		Environment:                "Sandbox",
		AppleOriginalTransactionID: "historical-original",
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := service.CreateSubscription("legacy-owner", "monthly", "Sandbox", "historical-original", expiry); err != nil {
		t.Fatalf("unique historical owner could not inherit: %v", err)
	}
	var owner database.AppleTransactionOwner
	if err := db.Where("environment = ? AND original_transaction_id = ?", "Sandbox", "historical-original").First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if owner.UserID != "legacy-owner" {
		t.Fatalf("unexpected inherited owner: %+v", owner)
	}

	if _, err := service.CreateSubscription("legacy-other", "monthly", "Sandbox", "historical-original", expiry); !errors.Is(err, ErrAppleTransactionConflict) {
		t.Fatalf("other user claimed an inherited transaction: %v", err)
	}
}

func TestCreateSubscriptionRejectsAmbiguousHistoricalTransaction(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	createSubscriptionTestUser(t, db, "ambig-a")
	createSubscriptionTestUser(t, db, "ambig-b")
	createSubscriptionTestUser(t, db, "ambig-c")
	expiry := time.Now().Add(20 * 24 * time.Hour)
	for index, userID := range []string{"ambig-a", "ambig-b"} {
		environment := "Sandbox"
		if index == 1 {
			// Older rows may predate the environment column; they still belong
			// to the same original transaction for ownership purposes.
			environment = ""
		}
		if err := db.Create(&database.Subscription{
			UserID:                     userID,
			PlanID:                     "monthly",
			EndTime:                    expiry,
			Status:                     "active",
			Environment:                environment,
			AppleOriginalTransactionID: "ambiguous-original",
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	for _, userID := range []string{"ambig-a", "ambig-c"} {
		if _, err := service.CreateSubscription(userID, "monthly", "Sandbox", "ambiguous-original", expiry); !errors.Is(err, ErrAppleTransactionConflict) {
			t.Fatalf("%s received ambiguous ownership: %v", userID, err)
		}
	}
	var owners int64
	if err := db.Model(&database.AppleTransactionOwner{}).
		Where("environment = ? AND original_transaction_id = ?", "Sandbox", "ambiguous-original").
		Count(&owners).Error; err != nil {
		t.Fatal(err)
	}
	if owners != 0 {
		t.Fatalf("ambiguous transaction was assigned to %d owners", owners)
	}
}

func TestCreateSubscriptionAllowsSameOwnerPlanChangeWithoutResettingExpiry(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	createSubscriptionTestUser(t, db, "upgrade-owner")
	firstExpiry := time.Now().Add(10 * 24 * time.Hour)
	upgradeExpiry := time.Now().Add(60 * 24 * time.Hour)

	first, err := service.CreateSubscription("upgrade-owner", "monthly", "Sandbox", "upgrade-original", firstExpiry)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := service.CreateSubscription("upgrade-owner", "yearly", "Sandbox", "upgrade-original", upgradeExpiry)
	if err != nil {
		t.Fatalf("same owner could not change plan: %v", err)
	}
	if upgraded.ID == first.ID || upgraded.PlanID != "yearly" {
		t.Fatalf("unexpected upgraded subscription: %+v", upgraded)
	}
	if diff := upgraded.EndTime.Sub(upgradeExpiry); diff < -time.Second || diff > time.Second {
		t.Fatalf("upgrade did not keep the verified expiry: got %v want %v", upgraded.EndTime, upgradeExpiry)
	}

	repeated, err := service.CreateSubscription("upgrade-owner", "yearly", "Sandbox", "upgrade-original", upgradeExpiry)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != upgraded.ID {
		t.Fatalf("repeated upgrade created a new subscription: %d and %d", upgraded.ID, repeated.ID)
	}
	if _, err := service.CreateSubscription("upgrade-owner", "monthly", "Sandbox", "upgrade-original", firstExpiry); !errors.Is(err, ErrAppleSubscriptionSuperseded) {
		t.Fatalf("expected replaying the superseded plan to fail with stale purchase, got %v", err)
	}
	var owner database.User
	if err := db.Where("user_id = ?", "upgrade-owner").First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if owner.MemberLevel != "yearly" || owner.MemberExpire == nil || !owner.MemberExpire.Equal(upgradeExpiry) {
		t.Fatalf("replayed superseded receipt reset entitlement: %+v", owner)
	}
	active, err := service.GetActiveSubscription("upgrade-owner")
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != upgraded.ID {
		t.Fatalf("replayed superseded receipt replaced the active plan: %+v", active)
	}
	var subscriptions int64
	if err := db.Model(&database.Subscription{}).Where("user_id = ?", "upgrade-owner").Count(&subscriptions).Error; err != nil {
		t.Fatal(err)
	}
	if subscriptions != 2 {
		t.Fatalf("expected one replaced and one active subscription, got %d", subscriptions)
	}
}

func TestCreateSubscriptionClaimsTransactionOnceUnderConcurrentRequests(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	service := NewSubscriptionService(db)
	users := []string{"race-a", "race-b", "race-c"}
	for _, userID := range users {
		createSubscriptionTestUser(t, db, userID)
	}
	expiry := time.Now().Add(30 * 24 * time.Hour)

	var wg sync.WaitGroup
	results := make([]error, len(users))
	for index, userID := range users {
		wg.Add(1)
		go func(index int, userID string) {
			defer wg.Done()
			_, results[index] = service.CreateSubscription(userID, "monthly", "Sandbox", "race-original", expiry)
		}(index, userID)
	}
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrAppleTransactionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent result: %v", result)
		}
	}
	if successes != 1 || conflicts != len(users)-1 {
		t.Fatalf("expected one winner and %d conflicts, got %d successes and %d conflicts", len(users)-1, successes, conflicts)
	}
	var owners int64
	if err := db.Model(&database.AppleTransactionOwner{}).
		Where("environment = ? AND original_transaction_id = ?", "Sandbox", "race-original").
		Count(&owners).Error; err != nil {
		t.Fatal(err)
	}
	if owners != 1 {
		t.Fatalf("expected exactly one ownership row, got %d", owners)
	}
}

func TestVerificationForJWSTransactionRejectsMissingOriginalAndRevocation(t *testing.T) {
	plan := &database.Plan{Level: "monthly", AppleProductID: "com.guoliang.starmeet.monthly"}
	now := time.Now()
	payload := &jwsTransactionPayload{
		OriginalTransactionID: "signed-original",
		Environment:           "Sandbox",
		Type:                  "Auto-Renewable Subscription",
		ExpiresDate:           now.Add(time.Hour).UnixMilli(),
		AppAccountToken:       "00000000-0000-4000-8000-000000000001",
	}
	verification, err := verificationForJWSTransaction(payload, plan, now)
	if err != nil {
		t.Fatalf("valid JWS payload failed: %v", err)
	}
	if verification.AppAccountToken != payload.AppAccountToken {
		t.Fatalf("app account token was not carried: %+v", verification)
	}

	payload.OriginalTransactionID = ""
	if _, err := verificationForJWSTransaction(payload, plan, now); err == nil {
		t.Fatal("expected empty original transaction id to fail")
	}

	payload.OriginalTransactionID = "signed-original"
	payload.RevocationDate = now.UnixMilli()
	if _, err := verificationForJWSTransaction(payload, plan, now); !errors.Is(err, ErrAppleReceiptRevoked) {
		t.Fatalf("expected revoked transaction to fail, got %v", err)
	}

	payload.RevocationDate = 0
	payload.Type = "Consumable"
	if _, err := verificationForJWSTransaction(payload, plan, now); err == nil {
		t.Fatal("expected consumable transaction to fail")
	}

	payload.Type = "Auto-Renewable Subscription"
	payload.Environment = "Xcode"
	if _, err := verificationForJWSTransaction(payload, plan, now); err == nil {
		t.Fatal("expected Xcode environment to fail")
	}
}

func TestVerifyAppleReceiptCarriesAppAccountToken(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Sandbox",
			LatestReceiptInfo: []appleReceiptItem{{
				ProductID:             "com.guoliang.starmeet.monthly",
				OriginalTransactionID: "token-original",
				ExpiresDateMs:         futureAppleExpiry(),
				AppAccountToken:       "00000000-0000-4000-8000-000000000001",
			}},
		}, nil
	}

	verification, err := service.VerifyAppleReceipt("dGVzdA==", "monthly", true)
	if err != nil {
		t.Fatal(err)
	}
	if verification.AppAccountToken != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("app account token was not extracted: %+v", verification)
	}
}

func TestVerifyAppleReceiptRejectsXcodeEnvironment(t *testing.T) {
	db := setupSubscriptionServiceTestDB(t)
	service := NewSubscriptionService(db)
	original := appleVerifyReceiptRequestFunc
	defer func() { appleVerifyReceiptRequestFunc = original }()
	appleVerifyReceiptRequestFunc = func(url string, payload map[string]interface{}) (*appleVerifyReceiptResponse, error) {
		return &appleVerifyReceiptResponse{
			Status:      0,
			Environment: "Xcode",
			LatestReceiptInfo: []appleReceiptItem{{
				ProductID:             "com.guoliang.starmeet.monthly",
				OriginalTransactionID: "xcode-original",
				ExpiresDateMs:         futureAppleExpiry(),
			}},
		}, nil
	}

	if _, err := service.VerifyAppleReceipt("dGVzdA==", "monthly", true); err == nil {
		t.Fatal("expected Xcode receipt environment to fail")
	}
}
