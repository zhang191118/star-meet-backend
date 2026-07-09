package v1_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func setupSubscriptionAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.Plan{}, &database.Subscription{}); err != nil {
		t.Fatalf("migrate tables: %v", err)
	}

	return db
}

func TestSubscriptionPlansIncludeResponseMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupSubscriptionAPITestDB(t)
	api := &apiv1.SubscriptionAPI{SubscriptionService: service.NewSubscriptionService(db)}

	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/api/v1/subscription/plans", api.GetPlans)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscription/plans", nil)
	req.Header.Set("X-Request-ID", "subscription-plans-req")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("expected response code 200, got %d", resp.Code)
	}
	if resp.RequestID != "subscription-plans-req" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime == 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
}

func TestVerifyAppleReceiptCreatesSubscriptionForJWTUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupSubscriptionAPITestDB(t)
	if err := db.Create(&database.User{UserID: "owner", Email: "owner-sub@example.com"}).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := db.Create(&database.User{UserID: "other", Email: "other-sub@example.com"}).Error; err != nil {
		t.Fatalf("create other: %v", err)
	}

	api := &apiv1.SubscriptionAPI{SubscriptionService: service.NewSubscriptionService(db)}

	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"status":0,"environment":"Sandbox","latest_receipt_info":[{"product_id":"com.starmeet.monthly","original_transaction_id":"txn-owner","transaction_id":"txn-owner-1","expires_date_ms":"100"}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	router := gin.New()
	group := router.Group("/api/v1/subscription")
	group.Use(middleware.Auth())
	group.POST("/apple/verify", api.VerifyAppleReceipt)

	payload := map[string]interface{}{
		"receipt":    "dGVzdA==",
		"plan_id":    "monthly",
		"is_sandbox": true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/apple/verify", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var count int64
	if err := db.Model(&database.Subscription{}).Where("user_id = ?", "owner").Count(&count).Error; err != nil {
		t.Fatalf("count owner subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one subscription for owner, got %d", count)
	}

	if err := db.Model(&database.Subscription{}).Where("user_id = ?", "other").Count(&count).Error; err != nil {
		t.Fatalf("count other subscriptions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no subscription for other user, got %d", count)
	}
}

func TestVerifyAppleReceiptRequiresAuthBeforeAppleRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupSubscriptionAPITestDB(t)
	api := &apiv1.SubscriptionAPI{SubscriptionService: service.NewSubscriptionService(db)}

	appleCalls := 0
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		appleCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":0}`)),
			Request:    req,
		}, nil
	})

	router := gin.New()
	router.Use(middleware.RequestID())
	router.POST("/api/v1/subscription/apple/verify", api.VerifyAppleReceipt)

	payload := map[string]interface{}{
		"receipt":    "dGVzdA==",
		"plan_id":    "monthly",
		"is_sandbox": true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/apple/verify", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "subscription-apple-auth-first-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if appleCalls != 0 {
		t.Fatalf("expected unauthenticated request to skip Apple verification, got %d calls", appleCalls)
	}

	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ErrorCode != "unauthorized" {
		t.Fatalf("expected unauthorized error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "subscription-apple-auth-first-req" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
}

func TestVerifyAppleReceiptRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupSubscriptionAPITestDB(t)
	api := &apiv1.SubscriptionAPI{SubscriptionService: service.NewSubscriptionService(db)}

	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/subscription")
	group.Use(middleware.Auth())
	group.POST("/apple/verify", api.VerifyAppleReceipt)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/apple/verify", bytes.NewReader([]byte(`{"receipt"`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "subscription-malformed-user"))
	req.Header.Set("X-Request-ID", "subscription-apple-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "subscription-apple-malformed-json-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "body" {
		t.Fatalf("expected body field detail, got %#v", details["field"])
	}
}

func TestVerifyAppleReceiptRejectsMissingRequiredFieldsWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupSubscriptionAPITestDB(t)
	api := &apiv1.SubscriptionAPI{SubscriptionService: service.NewSubscriptionService(db)}

	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/subscription")
	group.Use(middleware.Auth())
	group.POST("/apple/verify", api.VerifyAppleReceipt)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/apple/verify", bytes.NewReader([]byte(`{"receipt":"dGVzdA=="}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "subscription-validation-user"))
	req.Header.Set("X-Request-ID", "subscription-apple-validation-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing plan_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "subscription-apple-validation-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "plan_id" {
		t.Fatalf("expected plan_id field detail, got %#v", details["field"])
	}
}

func TestVerifyAppleReceiptRejectsInvalidBase64WithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupSubscriptionAPITestDB(t)
	api := &apiv1.SubscriptionAPI{SubscriptionService: service.NewSubscriptionService(db)}

	appleCalls := 0
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		appleCalls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"status":0}`)),
			Request:    req,
		}, nil
	})

	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/subscription")
	group.Use(middleware.Auth())
	group.POST("/apple/verify", api.VerifyAppleReceipt)

	payload := map[string]interface{}{
		"receipt":    "not-base64",
		"plan_id":    "monthly",
		"is_sandbox": true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/apple/verify", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "subscription-invalid-receipt-user"))
	req.Header.Set("X-Request-ID", "subscription-apple-invalid-base64-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for invalid receipt format, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if appleCalls != 0 {
		t.Fatalf("expected invalid receipt format to skip Apple verification, got %d calls", appleCalls)
	}

	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "subscription-apple-invalid-base64-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "receipt" {
		t.Fatalf("expected receipt field detail, got %#v", details["field"])
	}
}

func TestVerifyAppleReceiptReturnsBadRequestOnValidationFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupSubscriptionAPITestDB(t)
	if err := db.Create(&database.User{UserID: "owner", Email: "owner-sub-fail@example.com"}).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	api := &apiv1.SubscriptionAPI{SubscriptionService: service.NewSubscriptionService(db)}

	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"status":21010}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	router := gin.New()
	group := router.Group("/api/v1/subscription")
	group.Use(middleware.Auth())
	group.POST("/apple/verify", api.VerifyAppleReceipt)

	payload := map[string]interface{}{
		"receipt":    "dGVzdA==",
		"plan_id":    "monthly",
		"is_sandbox": true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/apple/verify", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected response code 400, got %d", resp.Code)
	}
	if resp.Message != "Invalid receipt: Apple 验证失败，status=21010" {
		t.Fatalf("unexpected error message: %s", resp.Message)
	}

	var count int64
	if err := db.Model(&database.Subscription{}).Count(&count).Error; err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no subscriptions to be created, got %d", count)
	}
}
