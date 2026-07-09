package v1_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

func setupCurrencyRechargeIdempotencyRouter(t *testing.T) (*gin.Engine, *service.CurrencyService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := setupCurrencyMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "recharge-user", Nickname: "充值用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, pkg := range []database.RechargePackage{
		{PackageID: "star_60", Name: "60星星", Currency: database.CurrencyStar, Amount: 60, Price: 6.0, IsActive: true, SortOrder: 1},
		{PackageID: "star_300", Name: "300星星", Currency: database.CurrencyStar, Amount: 300, Price: 30.0, IsActive: true, SortOrder: 2},
		{PackageID: "moon_300", Name: "300月亮", Currency: database.CurrencyMoon, Amount: 300, Price: 30.0, IsActive: true, SortOrder: 3},
	} {
		if err := db.Create(&pkg).Error; err != nil {
			t.Fatalf("create package %s: %v", pkg.PackageID, err)
		}
	}

	currencyService := service.NewCurrencyService(db)
	api := &apiv1.CurrencyAPI{CurrencyService: currencyService}
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/currency")
	group.Use(middleware.Auth())
	group.POST("/recharge", api.Recharge)
	return router, currencyService
}

func setupCurrencyRechargeEmptyPackageRouter(t *testing.T) (*gin.Engine, *service.CurrencyService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := setupCurrencyMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "recharge-user", Nickname: "充值用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	currencyService := service.NewCurrencyService(db)
	api := &apiv1.CurrencyAPI{CurrencyService: currencyService}
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/currency")
	group.Use(middleware.Auth())
	group.GET("/packages", api.GetPackages)
	group.POST("/recharge", api.Recharge)
	return router, currencyService
}

func postRecharge(t *testing.T, router *gin.Engine, payload map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/currency/recharge", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "recharge-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestRechargePackageListSeedsDefaultsUsedByRecharge(t *testing.T) {
	router, currencyService := setupCurrencyRechargeEmptyPackageRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/currency/packages", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "recharge-user"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected packages status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	list, ok := resp.Data.([]interface{})
	if !ok || len(list) == 0 {
		t.Fatalf("expected seeded package list, got %#v", resp.Data)
	}
	first, ok := list[0].(map[string]interface{})
	if !ok || first["package_id"] != "star_60" {
		t.Fatalf("expected first default package star_60, got %#v", list[0])
	}

	var packageCount int64
	if err := currencyService.DB.Model(&database.RechargePackage{}).Count(&packageCount).Error; err != nil {
		t.Fatalf("count recharge packages: %v", err)
	}
	if packageCount == 0 {
		t.Fatal("expected default recharge packages to be persisted")
	}

	recharge := postRecharge(t, router, map[string]string{"package_id": "star_60"})
	if recharge.Code != http.StatusOK {
		t.Fatalf("expected recharge with seeded package to succeed, got %d body=%s", recharge.Code, recharge.Body.String())
	}
	balance, err := currencyService.GetUserBalance("recharge-user")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance[database.CurrencyStar] != 60 {
		t.Fatalf("expected seeded star_60 recharge to add 60 stars, got %d", balance[database.CurrencyStar])
	}
}

func TestRechargeReusesClientOrderIDOnRetry(t *testing.T) {
	router, currencyService := setupCurrencyRechargeIdempotencyRouter(t)

	payload := map[string]string{
		"package_id":      "star_60",
		"client_order_id": "ios-recharge-order-1",
	}
	for i := 0; i < 2; i++ {
		recorder := postRecharge(t, router, payload)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d expected status 200, got %d body=%s", i+1, recorder.Code, recorder.Body.String())
		}
		resp := decodeResponse(t, recorder)
		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("request %d expected response data object, got %#v", i+1, resp.Data)
		}
		if _, ok := data["transaction_id"].(string); !ok {
			t.Fatalf("request %d expected transaction_id string, got %#v", i+1, data["transaction_id"])
		}
	}

	balance, err := currencyService.GetUserBalance("recharge-user")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance[database.CurrencyStar] != 60 {
		t.Fatalf("expected retry to keep balance at 60, got %d", balance[database.CurrencyStar])
	}

	var txCount int64
	if err := currencyService.DB.Model(&database.CurrencyTransaction{}).
		Where("user_id = ? AND client_order_id = ?", "recharge-user", "ios-recharge-order-1").
		Count(&txCount).Error; err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if txCount != 1 {
		t.Fatalf("expected one transaction for retried client_order_id, got %d", txCount)
	}
}

func TestRechargeRejectsClientOrderIDPackageMismatch(t *testing.T) {
	router, currencyService := setupCurrencyRechargeIdempotencyRouter(t)

	first := postRecharge(t, router, map[string]string{
		"package_id":      "star_60",
		"client_order_id": "ios-recharge-order-conflict",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first request expected status 200, got %d body=%s", first.Code, first.Body.String())
	}

	second := postRecharge(t, router, map[string]string{
		"package_id":      "star_300",
		"client_order_id": "ios-recharge-order-conflict",
	})
	if second.Code != http.StatusConflict {
		t.Fatalf("second request expected status 409, got %d body=%s", second.Code, second.Body.String())
	}
	resp := decodeResponse(t, second)
	if resp.ErrorCode != "currency_idempotency_conflict" {
		t.Fatalf("expected currency_idempotency_conflict error_code, got %q", resp.ErrorCode)
	}

	balance, err := currencyService.GetUserBalance("recharge-user")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance[database.CurrencyStar] != 60 {
		t.Fatalf("expected conflict not to add balance, got %d", balance[database.CurrencyStar])
	}
}

func TestRechargeRejectsUnavailablePackageWithStableErrorCode(t *testing.T) {
	router, currencyService := setupCurrencyRechargeIdempotencyRouter(t)

	recorder := postRecharge(t, router, map[string]string{
		"package_id":      "missing_package",
		"client_order_id": "ios-recharge-missing-package",
	})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for unavailable recharge package, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "recharge_package_unavailable" {
		t.Fatalf("expected recharge_package_unavailable error_code, got %q", resp.ErrorCode)
	}

	balance, err := currencyService.GetUserBalance("recharge-user")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance[database.CurrencyStar] != 0 || balance[database.CurrencyMoon] != 0 || balance[database.CurrencySun] != 0 {
		t.Fatalf("expected unavailable package not to change balance, got %+v", balance)
	}
}

func TestRechargeRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	router, _ := setupCurrencyRechargeIdempotencyRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/currency/recharge", bytes.NewReader([]byte(`{"package_id":"star_60"`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "recharge-user"))
	req.Header.Set("X-Request-ID", "currency-recharge-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed recharge JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "currency-recharge-malformed-json-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "body" {
		t.Fatalf("expected body field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected string reason detail, got %#v", details["reason"])
	}
}

func TestRechargeRejectsMissingPackageIDWithValidationError(t *testing.T) {
	router, _ := setupCurrencyRechargeIdempotencyRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/currency/recharge", bytes.NewReader([]byte(`{"client_order_id":"ios-missing-package"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "recharge-user"))
	req.Header.Set("X-Request-ID", "currency-recharge-missing-package-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing package_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "currency-recharge-missing-package-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "package_id" {
		t.Fatalf("expected package_id field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected string reason detail, got %#v", details["reason"])
	}
}

func TestRechargeWithoutClientOrderIDKeepsLegacyRepeatBehavior(t *testing.T) {
	router, currencyService := setupCurrencyRechargeIdempotencyRouter(t)

	payload := map[string]string{"package_id": "star_60"}
	for i := 0; i < 2; i++ {
		recorder := postRecharge(t, router, payload)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d expected status 200, got %d body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	balance, err := currencyService.GetUserBalance("recharge-user")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance[database.CurrencyStar] != 120 {
		t.Fatalf("expected legacy repeat recharge balance 120, got %d", balance[database.CurrencyStar])
	}
}

func TestRechargeAllowsSameUserToHoldMultipleCurrencyTypes(t *testing.T) {
	router, currencyService := setupCurrencyRechargeIdempotencyRouter(t)

	for _, packageID := range []string{"star_60", "moon_300"} {
		recorder := postRecharge(t, router, map[string]string{"package_id": packageID})
		if recorder.Code != http.StatusOK {
			t.Fatalf("recharge %s expected status 200, got %d body=%s", packageID, recorder.Code, recorder.Body.String())
		}
	}

	balance, err := currencyService.GetUserBalance("recharge-user")
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance[database.CurrencyStar] != 60 {
		t.Fatalf("expected star balance 60, got %d", balance[database.CurrencyStar])
	}
	if balance[database.CurrencyMoon] != 300 {
		t.Fatalf("expected moon balance 300, got %d", balance[database.CurrencyMoon])
	}
	if balance[database.CurrencySun] != 0 {
		t.Fatalf("expected sun balance 0, got %d", balance[database.CurrencySun])
	}
}
