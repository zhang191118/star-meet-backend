package v1_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func setupMobileAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(
		&database.User{},
		&database.UserSettings{},
		&database.Character{},
		&database.Session{},
		&database.UserCurrency{},
		&database.Subscription{},
	); err != nil {
		t.Fatalf("migrate mobile api tables: %v", err)
	}

	return db
}

func TestGetMobileHomeReturnsFirstScreenPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	now := time.Now()
	if err := db.Create(&database.User{UserID: "mobile-api-user", Nickname: "首屏用户", MemberLevel: "free"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "mobile-api-char",
		UserID:       "system_admin",
		Name:         "首屏角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{
		SessionID:     "mobile-api-session",
		UserID:        "mobile-api-user",
		CharacterID:   "mobile-api-char",
		LastMessage:   "欢迎回来",
		LastMessageAt: &now,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/mobile")
	group.Use(middleware.Auth())
	group.GET("/home", api.GetHome)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mobile/home", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "mobile-api-user"))
	req.Header.Set("X-Request-ID", "mobile-home-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "mobile-home-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	for _, key := range []string{"user", "settings", "membership", "wallet", "recent_sessions", "recommend_sections", "banners", "server_time"} {
		if _, exists := data[key]; !exists {
			t.Fatalf("expected mobile home key %q in %#v", key, data)
		}
	}
}

func TestGetMobileHomeDegradesWalletWhenWalletLoadFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	if err := db.Create(&database.User{UserID: "mobile-api-wallet-error-user", Nickname: "钱包异常用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "mobile-api-wallet-error-char",
		UserID:       "system_admin",
		Name:         "钱包异常推荐角色",
		Gender:       "female",
		Relationship: "朋友",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Migrator().DropTable(&database.UserCurrency{}); err != nil {
		t.Fatalf("drop wallet table: %v", err)
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/mobile")
	group.Use(middleware.Auth())
	group.GET("/home", api.GetHome)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mobile/home", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "mobile-api-wallet-error-user"))
	req.Header.Set("X-Request-ID", "mobile-home-wallet-error-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 with degraded wallet, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "mobile-home-wallet-error-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	for _, key := range []string{"user", "settings", "membership", "wallet", "recent_sessions", "recommend_sections", "banners", "server_time"} {
		if _, exists := data[key]; !exists {
			t.Fatalf("expected degraded mobile home to keep key %q in %#v", key, data)
		}
	}
	if data["degraded"] != true {
		t.Fatalf("expected degraded=true, got %#v", data["degraded"])
	}
	unavailableSections, ok := data["unavailable_sections"].([]interface{})
	if !ok || len(unavailableSections) != 1 || unavailableSections[0] != "wallet" {
		t.Fatalf("expected unavailable_sections [wallet], got %#v", data["unavailable_sections"])
	}
	wallet, ok := data["wallet"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fallback wallet object, got %#v", data["wallet"])
	}
	if wallet["star"] != float64(0) || wallet["moon"] != float64(0) || wallet["sun"] != float64(0) {
		t.Fatalf("expected zero fallback wallet, got %#v", wallet)
	}
}

func TestMobileHandlersReturnUnauthorizedWhenUserContextMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/api/v1/mobile/home", api.GetHome)
	router.GET("/api/v1/me/dashboard", api.GetDashboard)
	router.POST("/api/v1/mobile/batch", api.PostBatch)

	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{name: "home", method: http.MethodGet, path: "/api/v1/mobile/home"},
		{name: "dashboard", method: http.MethodGet, path: "/api/v1/me/dashboard"},
		{name: "batch", method: http.MethodPost, path: "/api/v1/mobile/batch", body: []byte(`{"include":["wallet"]}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", "mobile-missing-user-context-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d body=%s", recorder.Code, recorder.Body.String())
			}

			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "unauthorized" {
				t.Fatalf("expected unauthorized error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "mobile-missing-user-context-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
		})
	}
}

func TestGetMeDashboardReturnsAccountPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	if err := db.Create(&database.User{
		UserID:      "dashboard-user",
		Nickname:    "面板用户",
		MemberLevel: "yearly",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.UserCurrency{
		UserID:   "dashboard-user",
		Currency: database.CurrencyStar,
		Balance:  88,
	}).Error; err != nil {
		t.Fatalf("create currency: %v", err)
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/me")
	group.Use(middleware.Auth())
	group.GET("/dashboard", api.GetDashboard)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "dashboard-user"))
	req.Header.Set("X-Request-ID", "me-dashboard-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "me-dashboard-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	for _, key := range []string{"profile", "settings", "subscription", "wallet", "daily_limits", "server_time"} {
		if _, exists := data[key]; !exists {
			t.Fatalf("expected dashboard key %q in %#v", key, data)
		}
	}
}

func TestGetMeDashboardDegradesWalletWhenWalletLoadFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	if err := db.Create(&database.User{
		UserID:      "dashboard-wallet-error-user",
		Nickname:    "面板钱包异常用户",
		MemberLevel: "monthly",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Migrator().DropTable(&database.UserCurrency{}); err != nil {
		t.Fatalf("drop wallet table: %v", err)
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/me")
	group.Use(middleware.Auth())
	group.GET("/dashboard", api.GetDashboard)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "dashboard-wallet-error-user"))
	req.Header.Set("X-Request-ID", "me-dashboard-wallet-error-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 with degraded dashboard wallet, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "me-dashboard-wallet-error-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	for _, key := range []string{"profile", "settings", "subscription", "wallet", "daily_limits", "server_time"} {
		if _, exists := data[key]; !exists {
			t.Fatalf("expected degraded dashboard to keep key %q in %#v", key, data)
		}
	}
	if data["degraded"] != true {
		t.Fatalf("expected degraded=true, got %#v", data["degraded"])
	}
	unavailableSections, ok := data["unavailable_sections"].([]interface{})
	if !ok || len(unavailableSections) != 1 || unavailableSections[0] != "wallet" {
		t.Fatalf("expected unavailable_sections [wallet], got %#v", data["unavailable_sections"])
	}
	wallet, ok := data["wallet"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fallback wallet object, got %#v", data["wallet"])
	}
	if wallet["star"] != float64(0) || wallet["moon"] != float64(0) || wallet["sun"] != float64(0) {
		t.Fatalf("expected zero fallback wallet, got %#v", wallet)
	}
}

func TestGetMeDashboardReturnsUserNotFoundWhenProfileMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/me")
	group.Use(middleware.Auth())
	group.GET("/dashboard", api.GetDashboard)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "missing-dashboard-user"))
	req.Header.Set("X-Request-ID", "me-dashboard-missing-user-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing dashboard user, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "user_not_found" {
		t.Fatalf("expected user_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "me-dashboard-missing-user-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
}

func TestPostMobileBatchReturnsSafeAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	if err := db.Create(&database.User{UserID: "batch-user", Nickname: "批量用户", MemberLevel: "monthly"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.UserCurrency{
		UserID:   "batch-user",
		Currency: database.CurrencyMoon,
		Balance:  66,
	}).Error; err != nil {
		t.Fatalf("create currency: %v", err)
	}
	for _, character := range []database.Character{
		{
			CharacterID:  "batch-char-a",
			UserID:       "system_admin",
			Name:         "批量角色A",
			Gender:       "female",
			Relationship: "恋人",
			Personality:  `["温柔","主动"]`,
			SystemPrompt: "secret prompt",
			Mem0AgentID:  "secret-agent",
			IsPublic:     1,
			ChatCount:    9,
		},
		{
			CharacterID:  "batch-char-b",
			UserID:       "system_admin",
			Name:         "批量角色B",
			Gender:       "female",
			Relationship: "朋友",
			IsPublic:     1,
			ChatCount:    3,
		},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %s: %v", character.CharacterID, err)
		}
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/mobile")
	group.Use(middleware.Auth())
	group.POST("/batch", api.PostBatch)

	body := map[string]interface{}{
		"character_ids": []string{"batch-char-a", "batch-char-a", "missing-char", "batch-char-b"},
		"include":       []string{"characters", "wallet", "membership"},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/batch", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "batch-user"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "mobile-batch-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "mobile-batch-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	if _, exists := data["server_time"]; !exists {
		t.Fatalf("expected server_time in batch response: %#v", data)
	}
	if _, exists := data["wallet"]; !exists {
		t.Fatalf("expected wallet in batch response: %#v", data)
	}
	if _, exists := data["membership"]; !exists {
		t.Fatalf("expected membership in batch response: %#v", data)
	}

	characters, ok := data["characters"].([]interface{})
	if !ok || len(characters) != 2 {
		t.Fatalf("expected 2 safe character cards, got %#v", data["characters"])
	}
	first, ok := characters[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first character object, got %#v", characters[0])
	}
	if first["character_id"] != "batch-char-a" {
		t.Fatalf("expected deduped order to keep batch-char-a first, got %#v", first["character_id"])
	}
	if _, exists := first["system_prompt"]; exists {
		t.Fatalf("batch response leaked system_prompt: %#v", first)
	}
	if _, exists := first["mem0_agent_id"]; exists {
		t.Fatalf("batch response leaked mem0_agent_id: %#v", first)
	}
}

func TestPostMobileBatchDegradesWalletWhenWalletLoadFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	if err := db.Create(&database.User{UserID: "batch-wallet-error-user", Nickname: "批量钱包异常用户", MemberLevel: "monthly"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, character := range []database.Character{
		{
			CharacterID:  "batch-wallet-error-char-a",
			UserID:       "system_admin",
			Name:         "批量钱包异常角色A",
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     1,
		},
		{
			CharacterID:  "batch-wallet-error-char-b",
			UserID:       "system_admin",
			Name:         "批量钱包异常角色B",
			Gender:       "male",
			Relationship: "朋友",
			IsPublic:     1,
		},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %s: %v", character.CharacterID, err)
		}
	}
	if err := db.Migrator().DropTable(&database.UserCurrency{}); err != nil {
		t.Fatalf("drop wallet table: %v", err)
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/mobile")
	group.Use(middleware.Auth())
	group.POST("/batch", api.PostBatch)

	payload, err := json.Marshal(map[string]interface{}{
		"character_ids": []string{"batch-wallet-error-char-a", "batch-wallet-error-char-b"},
		"include":       []string{"characters", "wallet", "membership"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/batch", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "batch-wallet-error-user"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "mobile-batch-wallet-error-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 with degraded batch wallet, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "mobile-batch-wallet-error-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	characters, ok := data["characters"].([]interface{})
	if !ok || len(characters) != 2 {
		t.Fatalf("expected characters to survive wallet degradation, got %#v", data["characters"])
	}
	if _, ok := data["membership"].(map[string]interface{}); !ok {
		t.Fatalf("expected membership to survive wallet degradation, got %#v", data["membership"])
	}
	if data["degraded"] != true {
		t.Fatalf("expected degraded=true, got %#v", data["degraded"])
	}
	unavailableSections, ok := data["unavailable_sections"].([]interface{})
	if !ok || len(unavailableSections) != 1 || unavailableSections[0] != "wallet" {
		t.Fatalf("expected unavailable_sections [wallet], got %#v", data["unavailable_sections"])
	}
	wallet, ok := data["wallet"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fallback wallet object, got %#v", data["wallet"])
	}
	if wallet["star"] != float64(0) || wallet["moon"] != float64(0) || wallet["sun"] != float64(0) {
		t.Fatalf("expected zero fallback wallet, got %#v", wallet)
	}
}

func TestPostMobileBatchReturnsUserNotFoundWhenAccountMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/mobile")
	group.Use(middleware.Auth())
	group.POST("/batch", api.PostBatch)

	payload, err := json.Marshal(map[string]interface{}{
		"include": []string{"wallet"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/batch", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "missing-batch-user"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "mobile-batch-missing-user-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing batch user, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "user_not_found" {
		t.Fatalf("expected user_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "mobile-batch-missing-user-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
}

func TestPostMobileBatchRejectsUnknownIncludeKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	if err := db.Create(&database.User{UserID: "batch-invalid-include-user", Nickname: "批量参数用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/mobile")
	group.Use(middleware.Auth())
	group.POST("/batch", api.PostBatch)

	payload, err := json.Marshal(map[string]interface{}{
		"character_ids": []string{"any-char"},
		"include":       []string{"wallet", "foo", "bar"},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/batch", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "batch-invalid-include-user"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "mobile-batch-invalid-include-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for invalid include, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	invalidInclude, ok := details["invalid_include"].([]interface{})
	if !ok {
		t.Fatalf("expected invalid_include array, got %#v", details["invalid_include"])
	}
	if len(invalidInclude) != 2 || invalidInclude[0] != "foo" || invalidInclude[1] != "bar" {
		t.Fatalf("expected invalid include keys foo/bar, got %#v", invalidInclude)
	}
	allowedInclude, ok := details["allowed_include"].([]interface{})
	if !ok {
		t.Fatalf("expected allowed_include array, got %#v", details["allowed_include"])
	}
	if len(allowedInclude) != 3 || allowedInclude[0] != "characters" || allowedInclude[1] != "membership" || allowedInclude[2] != "wallet" {
		t.Fatalf("expected allowed include list, got %#v", allowedInclude)
	}
}

func TestPostMobileBatchRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMobileAPITestDB(t)
	if err := db.Create(&database.User{UserID: "batch-malformed-json-user", Nickname: "批量 JSON 用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	api := apiv1.NewMobileAPI(service.NewMobileService(db))
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/mobile")
	group.Use(middleware.Auth())
	group.POST("/batch", api.PostBatch)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mobile/batch", bytes.NewReader([]byte(`{"include":["wallet"`)))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "batch-malformed-json-user"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "mobile-batch-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "mobile-batch-malformed-json-req" {
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
