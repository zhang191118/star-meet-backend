package v1_test

import (
	"bytes"
	"fmt"
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

func setupGachaPaginationAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.UserGachaRecord{}); err != nil {
		t.Fatalf("migrate gacha api tables: %v", err)
	}
	return db
}

func seedGachaHistory(t *testing.T, db *gorm.DB, userID string, count int) {
	t.Helper()

	if err := db.Create(&database.User{UserID: userID, Nickname: "抽卡用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now()
	for i := 0; i < count; i++ {
		if err := db.Create(&database.UserGachaRecord{
			UserID:      userID,
			PoolID:      "starter",
			CharacterID: fmt.Sprintf("gacha-char-%03d", i),
			Rarity:      database.GachaR,
			Count:       i + 1,
			Cost:        160,
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatalf("create gacha record %d: %v", i, err)
		}
	}
}

func setupGachaHistoryRouter(db *gorm.DB) *gin.Engine {
	api := &apiv1.GachaAPI{GachaService: service.NewGachaService(db)}
	router := gin.New()
	group := router.Group("/api/v1/gacha")
	group.Use(middleware.Auth())
	group.GET("/history", api.GetHistory)
	return router
}

func TestGetGachaPoolsReturnsEmptyArrayWhenDatabaseIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupGachaPaginationAPITestDB(t)
	if err := db.AutoMigrate(&database.GachaPool{}); err != nil {
		t.Fatalf("migrate gacha pools: %v", err)
	}

	api := &apiv1.GachaAPI{GachaService: service.NewGachaService(db)}
	router := gin.New()
	router.GET("/api/v1/gacha/pools", api.GetPools)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gacha/pools", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	list, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected gacha pools array, got %#v", resp.Data)
	}
	if len(list) != 0 {
		t.Fatalf("expected no pools when database is empty, got %d pools", len(list))
	}
}

func TestGetGachaCharactersRequiresAndUsesAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupGachaPaginationAPITestDB(t)
	if err := db.AutoMigrate(&database.UserCharacter{}); err != nil {
		t.Fatalf("migrate user characters: %v", err)
	}
	if err := db.Create(&database.User{UserID: "gacha-owner", Nickname: "拥有者"}).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := db.Create(&database.UserCharacter{
		UserID:      "gacha-owner",
		CharacterID: "owned-char",
		ObtainType:  "gacha",
		StarLevel:   4,
	}).Error; err != nil {
		t.Fatalf("create owned character: %v", err)
	}

	api := &apiv1.GachaAPI{GachaService: service.NewGachaService(db)}
	router := gin.New()
	group := router.Group("/api/v1/gacha")
	group.Use(middleware.Auth())
	group.GET("/characters", api.GetCharacters)

	missingAuthReq := httptest.NewRequest(http.MethodGet, "/api/v1/gacha/characters", nil)
	missingAuthRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingAuthRecorder, missingAuthReq)
	if missingAuthRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing token status 401, got %d body=%s", missingAuthRecorder.Code, missingAuthRecorder.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gacha/characters", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "gacha-owner"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected authenticated status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	list, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected user characters array, got %#v", resp.Data)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 owned character, got %d", len(list))
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first owned character object, got %#v", list[0])
	}
	if first["character_id"] != "owned-char" {
		t.Fatalf("expected owned character, got %#v", first["character_id"])
	}
}

func TestToggleFavoriteRejectsMissingCharacterIDWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupGachaPaginationAPITestDB(t)
	if err := db.AutoMigrate(&database.UserCharacter{}); err != nil {
		t.Fatalf("migrate user characters: %v", err)
	}
	if err := db.Create(&database.User{UserID: "favorite-user", Nickname: "收藏用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	api := &apiv1.GachaAPI{GachaService: service.NewGachaService(db)}
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/gacha")
	group.Use(middleware.Auth())
	group.POST("/favorite", api.ToggleFavorite)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/gacha/favorite", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "favorite-user"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "gacha-favorite-missing-character-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing character_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "gacha-favorite-missing-character-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "character_id" {
		t.Fatalf("expected character_id field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected string reason detail, got %#v", details["reason"])
	}
}

func TestToggleFavoriteReturnsStableNotFoundForMissingCharacter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupGachaPaginationAPITestDB(t)
	if err := db.AutoMigrate(&database.UserCharacter{}); err != nil {
		t.Fatalf("migrate user characters: %v", err)
	}
	if err := db.Create(&database.User{UserID: "favorite-missing-user", Nickname: "收藏用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	api := &apiv1.GachaAPI{GachaService: service.NewGachaService(db)}
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/gacha")
	group.Use(middleware.Auth())
	group.POST("/favorite", api.ToggleFavorite)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/gacha/favorite", bytes.NewReader([]byte(`{"character_id":"not-owned-char"}`)))
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "favorite-missing-user"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "gacha-favorite-character-not-found-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing owned character, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "user_character_not_found" {
		t.Fatalf("expected user_character_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "gacha-favorite-character-not-found-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "character_id" || details["reason"] != "not_found_or_not_owned" {
		t.Fatalf("unexpected details: %#v", details)
	}
}

func TestGetGachaHistoryIncludesMobilePaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupGachaPaginationAPITestDB(t)
	seedGachaHistory(t, db, "gacha-page-user", 101)
	router := setupGachaHistoryRouter(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gacha/history?page=1&page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "gacha-page-user"))

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
		t.Fatalf("expected 100 gacha records after clamp, got %d", len(list))
	}
}

func TestGetGachaHistoryWithoutPaginationKeepsLegacyArray(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupGachaPaginationAPITestDB(t)
	seedGachaHistory(t, db, "gacha-legacy-user", 1)
	router := setupGachaHistoryRouter(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gacha/history", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "gacha-legacy-user"))

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
		t.Fatalf("expected 1 gacha record, got %d", len(list))
	}
}

func TestGetGachaHistoryPageOnlyKeepsLegacyArray(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupGachaPaginationAPITestDB(t)
	seedGachaHistory(t, db, "gacha-page-only-user", 1)
	router := setupGachaHistoryRouter(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gacha/history?page=2", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "gacha-page-only-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if _, ok := resp.Data.([]interface{}); !ok {
		t.Fatalf("expected page-only request to keep legacy array data, got %#v", resp.Data)
	}
}
