package v1_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupCurrencyMetadataTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.UserCurrency{}, &database.CurrencyTransaction{}, &database.RechargePackage{}); err != nil {
		t.Fatalf("migrate currency metadata tables: %v", err)
	}

	return db
}

func TestCurrencyHandlersReturnUnauthorizedWhenUserContextMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCurrencyMetadataTestDB(t)
	api := &apiv1.CurrencyAPI{CurrencyService: service.NewCurrencyService(db)}
	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/api/v1/currency/balance", api.GetBalance)
	router.GET("/api/v1/currency/transactions", api.GetTransactions)
	router.POST("/api/v1/currency/recharge", api.Recharge)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "balance", method: http.MethodGet, path: "/api/v1/currency/balance"},
		{name: "transactions", method: http.MethodGet, path: "/api/v1/currency/transactions"},
		{name: "recharge", method: http.MethodPost, path: "/api/v1/currency/recharge", body: `{"package_id":"star_60"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", "currency-missing-user-context-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d body=%s", recorder.Code, recorder.Body.String())
			}
			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "unauthorized" {
				t.Fatalf("expected unauthorized error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "currency-missing-user-context-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
		})
	}
}

func TestCurrencyBalanceIncludesResponseMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCurrencyMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "currency-user", Nickname: "资产用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.UserCurrency{
		UserID:   "currency-user",
		Currency: database.CurrencyStar,
		Balance:  66,
	}).Error; err != nil {
		t.Fatalf("create currency: %v", err)
	}

	api := &apiv1.CurrencyAPI{CurrencyService: service.NewCurrencyService(db)}
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/currency")
	group.Use(middleware.Auth())
	group.GET("/balance", api.GetBalance)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/currency/balance", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "currency-user"))
	req.Header.Set("X-Request-ID", "currency-balance-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "currency-balance-req" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime <= 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
}

func TestCurrencyTransactionsIncludesMobilePaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCurrencyMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "currency-page-user", Nickname: "交易用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now()
	for i := 0; i < 101; i++ {
		if err := db.Create(&database.CurrencyTransaction{
			UserID:      "currency-page-user",
			Currency:    database.CurrencyStar,
			Amount:      int64(i + 1),
			Type:        database.TransactionSystem,
			RelatedID:   fmt.Sprintf("currency-tx-%03d", i),
			Description: fmt.Sprintf("transaction %d", i),
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatalf("create transaction %d: %v", i, err)
		}
	}

	api := &apiv1.CurrencyAPI{CurrencyService: service.NewCurrencyService(db)}
	router := gin.New()
	group := router.Group("/api/v1/currency")
	group.Use(middleware.Auth())
	group.GET("/transactions", api.GetTransactions)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/currency/transactions?page=1&page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "currency-page-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	data := decodePaginationData(t, recorder)
	if data["page"] != float64(1) {
		t.Fatalf("expected page 1, got %#v", data["page"])
	}
	if data["page_size"] != float64(100) {
		t.Fatalf("expected clamped page_size 100, got %#v", data["page_size"])
	}
	if data["has_more"] != true {
		t.Fatalf("expected has_more true, got %#v", data["has_more"])
	}
	if data["total"] != float64(101) {
		t.Fatalf("expected total 101, got %#v", data["total"])
	}
	list, ok := data["list"].([]interface{})
	if !ok {
		t.Fatalf("expected list array, got %#v", data["list"])
	}
	if len(list) != 100 {
		t.Fatalf("expected 100 transactions after clamp, got %d", len(list))
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected transaction object, got %#v", list[0])
	}
	if _, ok := first["id"].(string); !ok {
		t.Fatalf("expected transaction id to be string for iOS decoding, got %#v", first["id"])
	}
}

func TestCurrencyTransactionsWithoutPaginationKeepsLegacyArray(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCurrencyMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "currency-legacy-user", Nickname: "旧交易用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.CurrencyTransaction{
		UserID:      "currency-legacy-user",
		Currency:    database.CurrencyStar,
		Amount:      12,
		Type:        database.TransactionSystem,
		RelatedID:   "legacy-tx",
		Description: "legacy transaction",
		CreatedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatalf("create transaction: %v", err)
	}

	api := &apiv1.CurrencyAPI{CurrencyService: service.NewCurrencyService(db)}
	router := gin.New()
	group := router.Group("/api/v1/currency")
	group.Use(middleware.Auth())
	group.GET("/transactions", api.GetTransactions)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/currency/transactions", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "currency-legacy-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	list, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected legacy array data, got %#v", resp.Data)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(list))
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected transaction object, got %#v", list[0])
	}
	if _, ok := first["id"].(string); !ok {
		t.Fatalf("expected transaction id to be string for iOS decoding, got %#v", first["id"])
	}
}

func TestCurrencyPackagesUseStringIDsForIOSDecoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCurrencyMetadataTestDB(t)
	if err := db.Create(&database.RechargePackage{
		PackageID: "moon_300",
		Name:      "300月亮",
		Currency:  database.CurrencyMoon,
		Amount:    300,
		Price:     30.0,
		IsActive:  true,
		SortOrder: 1,
	}).Error; err != nil {
		t.Fatalf("create package: %v", err)
	}

	api := &apiv1.CurrencyAPI{CurrencyService: service.NewCurrencyService(db)}
	router := gin.New()
	router.GET("/api/v1/currency/packages", api.GetPackages)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/currency/packages", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	list, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected packages array data, got %#v", resp.Data)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 package, got %d", len(list))
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected package object, got %#v", list[0])
	}
	if id, ok := first["id"].(string); !ok || id != "moon_300" {
		t.Fatalf("expected package id to be string for iOS decoding, got %#v", first["id"])
	}
}
