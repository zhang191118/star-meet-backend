package v1_test

import (
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

func setupCharacterPaginationAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.Character{}, &database.Favorite{}, &database.Block{}); err != nil {
		t.Fatalf("migrate character table: %v", err)
	}
	return db
}

func TestEncounterReturnsStableNotFoundForUnavailableCharacter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	if err := db.Create(&database.User{UserID: "encounter-user", Nickname: "邂逅用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.Use(middleware.Auth())
	group.POST("/:character_id/encounter", api.Encounter)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/character/missing-public-char/encounter", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "encounter-user"))
	req.Header.Set("X-Request-ID", "character-encounter-unavailable-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "character_unavailable" {
		t.Fatalf("expected character_unavailable error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "character-encounter-unavailable-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "character_id" || details["reason"] != "not_public_or_missing" {
		t.Fatalf("unexpected details: %#v", details)
	}
}

func TestEncounterReturnsConflictForAlreadyEncounteredCharacter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	userID := "encounter-dupe-user"
	if err := db.Create(&database.User{UserID: userID, Nickname: "重复邂逅用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "encounter-dupe-char",
		UserID:       "system_admin",
		Name:         "公开角色",
		Gender:       "male",
		Relationship: "朋友",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create public character: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "encounter-dupe-char_" + userID[:8],
		UserID:       userID,
		Name:         "已有角色",
		Gender:       "female",
		Relationship: "朋友",
		IsPublic:     2,
	}).Error; err != nil {
		t.Fatalf("create existing private character: %v", err)
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.Use(middleware.Auth())
	group.POST("/:character_id/encounter", api.Encounter)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/character/encounter-dupe-char/encounter", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, userID))
	req.Header.Set("X-Request-ID", "character-encounter-duplicate-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "character_already_encountered" {
		t.Fatalf("expected character_already_encountered error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "character-encounter-duplicate-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "character_id" || details["reason"] != "already_encountered" {
		t.Fatalf("unexpected details: %#v", details)
	}
}

func TestGetPublicCharactersSupportsBeforeIDCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	now := time.Now()
	for i := 0; i < 5; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("public-cursor-char-%03d", i),
			UserID:       "system_admin",
			Name:         fmt.Sprintf("公开游标角色 %d", i),
			Gender:       "male",
			Relationship: "伙伴",
			IsPublic:     1,
			ChatCount:    100 - i,
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.GET("/public", api.GetPublicList)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/public?before_id=public-cursor-char-001&page_size=2", nil)
	req.Header.Set("X-Request-ID", "character-public-cursor")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "character-public-cursor" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	if data["before_id"] != "public-cursor-char-001" {
		t.Fatalf("expected before_id echo, got %#v", data["before_id"])
	}
	if data["next_before_id"] != "public-cursor-char-003" {
		t.Fatalf("expected next_before_id from last item, got %#v", data["next_before_id"])
	}
	if data["page_size"] != float64(2) {
		t.Fatalf("expected page_size 2, got %#v", data["page_size"])
	}
	if data["has_more"] != true {
		t.Fatalf("expected has_more true, got %#v", data["has_more"])
	}

	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2 list items, got %#v", data["list"])
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first item object, got %#v", list[0])
	}
	if first["character_id"] != "public-cursor-char-002" {
		t.Fatalf("expected next older character first, got %#v", first["character_id"])
	}
}

func TestSearchCharactersIncludesMobilePaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	for i := 0; i < 3; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("search-meta-char-%03d", i),
			UserID:       "system_admin",
			Name:         fmt.Sprintf("分页搜索角色 %d", i),
			Gender:       "male",
			Relationship: "伙伴",
			IsPublic:     1,
			ChatCount:    30 - i,
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.GET("/search", api.Search)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/search?keyword=%E5%88%86%E9%A1%B5&page=1&page_size=2", nil)
	req.Header.Set("X-Request-ID", "character-search-pagination")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "character-search-pagination" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	if data["page"] != float64(1) {
		t.Fatalf("expected page 1, got %#v", data["page"])
	}
	if data["page_size"] != float64(2) {
		t.Fatalf("expected page_size 2, got %#v", data["page_size"])
	}
	if data["has_more"] != true {
		t.Fatalf("expected has_more true, got %#v", data["has_more"])
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2 list items, got %#v", data["list"])
	}
}

func TestSearchCharactersUsesStableTieBreakerOrdering(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	baseTime := time.Now()
	for i := 0; i < 3; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("search-stable-char-%03d", i),
			UserID:       "system_admin",
			Name:         fmt.Sprintf("稳定搜索角色 %d", i),
			Gender:       "male",
			Relationship: "伙伴",
			IsPublic:     1,
			ChatCount:    99,
			CreatedAt:    baseTime.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.GET("/search", api.Search)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/search?keyword=%E7%A8%B3%E5%AE%9A&page=1&page_size=3", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 3 {
		t.Fatalf("expected 3 list items, got %#v", data["list"])
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first item object, got %#v", list[0])
	}
	if first["character_id"] != "search-stable-char-002" {
		t.Fatalf("expected newest tied search character first, got %#v", first["character_id"])
	}
}

func TestRecommendCharactersIncludesMobilePaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	for i := 0; i < 5; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("recommend-meta-char-%03d", i),
			UserID:       "system_admin",
			Name:         fmt.Sprintf("推荐分页角色 %d", i),
			Gender:       "male",
			Relationship: "伙伴",
			IsPublic:     1,
			ChatCount:    50 - i,
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.GET("/recommend", api.GetRecommend)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/recommend?page=2&page_size=2", nil)
	req.Header.Set("X-Request-ID", "character-recommend-pagination")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "character-recommend-pagination" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	if data["page"] != float64(2) {
		t.Fatalf("expected page 2, got %#v", data["page"])
	}
	if data["page_size"] != float64(2) {
		t.Fatalf("expected page_size 2, got %#v", data["page_size"])
	}
	if data["has_more"] != true {
		t.Fatalf("expected has_more true, got %#v", data["has_more"])
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2 list items, got %#v", data["list"])
	}
}

func TestRecommendCharactersUsesStableTieBreakerOrdering(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	baseTime := time.Now()
	for i := 0; i < 3; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("recommend-stable-char-%03d", i),
			UserID:       "system_admin",
			Name:         fmt.Sprintf("稳定推荐角色 %d", i),
			Gender:       "male",
			Relationship: "伙伴",
			IsPublic:     1,
			ChatCount:    88,
			CreatedAt:    baseTime.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.GET("/recommend", api.GetRecommend)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/recommend?page=1&page_size=3", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 3 {
		t.Fatalf("expected 3 list items, got %#v", data["list"])
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first item object, got %#v", list[0])
	}
	if first["character_id"] != "recommend-stable-char-002" {
		t.Fatalf("expected newest tied recommended character first, got %#v", first["character_id"])
	}
}

func TestGetCharacterListUsesAuthenticatedUserAndIncludesPaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	for i := 0; i < 4; i++ {
		character := database.Character{
			CharacterID: fmt.Sprintf("owner-char-%03d", i),
			UserID:      "token-user",
			Name:        fmt.Sprintf("我的角色 %d", i),
			IsPublic:    2,
			ChatCount:   10 - i,
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create own character %d: %v", i, err)
		}
	}
	if err := db.Create(&database.Character{
		CharacterID: "spoofed-user-char",
		UserID:      "query-user",
		Name:        "不应该返回的角色",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatalf("create spoofed character: %v", err)
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.Use(middleware.Auth())
	group.GET("/list", api.GetList)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/list?user_id=query-user&page=1&page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "token-user"))
	req.Header.Set("X-Request-ID", "character-list-token-user")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "character-list-token-user" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	if data["page"] != float64(1) {
		t.Fatalf("expected page 1, got %#v", data["page"])
	}
	if data["page_size"] != float64(100) {
		t.Fatalf("expected page_size clamp to 100, got %#v", data["page_size"])
	}
	if data["has_more"] != false {
		t.Fatalf("expected has_more false, got %#v", data["has_more"])
	}
	if data["total"] != float64(4) {
		t.Fatalf("expected token user's total 4, got %#v", data["total"])
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 4 {
		t.Fatalf("expected 4 token user items, got %#v", data["list"])
	}
	for _, item := range list {
		character, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("expected character object, got %#v", item)
		}
		if character["user_id"] != "token-user" {
			t.Fatalf("expected only token user characters, got user_id %#v", character["user_id"])
		}
	}
}

func TestGetCharacterListAcceptsTokenOnlyRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.Use(middleware.Auth())
	group.GET("/list", api.GetList)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/list?page=1&page_size=20", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "empty-token-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected token-only request to succeed, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	list, ok := data["list"].([]interface{})
	if !ok {
		t.Fatalf("expected empty list array, got %#v", data["list"])
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %#v", list)
	}
}

func TestGetMyCharactersIncludesMobilePaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupCharacterPaginationAPITestDB(t)
	for i := 0; i < 3; i++ {
		character := database.Character{
			CharacterID: fmt.Sprintf("my-meta-char-%03d", i),
			UserID:      "my-token-user",
			Name:        fmt.Sprintf("我的私有角色 %d", i),
			Gender:      "female",
			IsPublic:    2,
			ChatCount:   10 - i,
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create my character %d: %v", i, err)
		}
	}

	api := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/character")
	group.Use(middleware.Auth())
	group.GET("/my", api.GetMyCharacters)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/my?page=1&page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "my-token-user"))
	req.Header.Set("X-Request-ID", "character-my-pagination")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "character-my-pagination" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	if data["page"] != float64(1) {
		t.Fatalf("expected page 1, got %#v", data["page"])
	}
	if data["page_size"] != float64(100) {
		t.Fatalf("expected page_size clamp to 100, got %#v", data["page_size"])
	}
	if data["has_more"] != false {
		t.Fatalf("expected has_more false, got %#v", data["has_more"])
	}
	if data["total"] != float64(3) {
		t.Fatalf("expected total 3, got %#v", data["total"])
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 3 {
		t.Fatalf("expected 3 list items, got %#v", data["list"])
	}
}
