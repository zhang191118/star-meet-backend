package v1_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

func TestCurrencyAppleVerifyAcceptsExactTransactionWithoutReceipt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupCurrencyMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "apple-modern-user", Nickname: "Apple用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.RechargePackage{
		PackageID: "star_60",
		Name:      "60星星",
		Currency:  database.CurrencyStar,
		Amount:    60,
		Price:     6,
		IsActive:  true,
	}).Error; err != nil {
		t.Fatalf("create package: %v", err)
	}

	api := &apiv1.CurrencyAPI{CurrencyService: service.NewCurrencyService(db)}
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/currency")
	group.Use(middleware.Auth())
	group.POST("/apple/verify", api.VerifyAppleReceipt)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/currency/apple/verify",
		bytes.NewReader([]byte(`{"package_id":"star_60","transaction_id":"not-numeric"}`)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "apple-modern-user"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode == "validation_failed" {
		t.Fatalf("transaction-only request must pass receipt binding validation, body=%s", recorder.Body.String())
	}
	if resp.ErrorCode != "invalid_receipt" {
		t.Fatalf("expected service-level transaction validation, got %q body=%s", resp.ErrorCode, recorder.Body.String())
	}
}
