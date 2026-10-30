package v1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAccessControlAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.Character{}, &database.Message{}, &database.Favorite{}, &database.Memory{}, &database.Block{}); err != nil {
		t.Fatalf("migrate tables: %v", err)
	}

	return db
}

func makeTestToken(t *testing.T, userID string) string {
	t.Helper()

	claims := middleware.TokenClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("star-meet-secret-key-change-in-production"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	return tokenString
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) response.Response {
	t.Helper()

	var resp response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return resp
}

func TestPublicCharacterDetailDoesNotExposeInternalFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	characterAPI := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})

	if err := db.Create(&database.Character{
		CharacterID:  "public-detail-char",
		UserID:       "system_admin",
		Name:         "公开详情角色",
		Gender:       "male",
		IsPublic:     1,
		SystemPrompt: "internal prompt",
		Mem0AgentID:  "mem0-internal-id",
	}).Error; err != nil {
		t.Fatalf("create public character: %v", err)
	}

	router := gin.New()
	group := router.Group("/api/v1/character")
	group.GET("/:character_id", characterAPI.GetDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/public-detail-char", nil)
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

	for _, field := range []string{"user_id", "system_prompt", "mem0_agent_id"} {
		if _, exists := data[field]; exists {
			t.Fatalf("public character detail leaked %s: %#v", field, data)
		}
	}
}

func TestPublicCharacterDetailRejectsPrivateCharacters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	characterAPI := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})

	if err := db.Create(&database.Character{
		CharacterID:  "private-detail-char",
		UserID:       "owner",
		Name:         "私有详情角色",
		Gender:       "female",
		IsPublic:     2,
		SystemPrompt: "private internal prompt",
		Mem0AgentID:  "private-mem0-id",
	}).Error; err != nil {
		t.Fatalf("create private character: %v", err)
	}

	router := gin.New()
	group := router.Group("/api/v1/character")
	group.GET("/:character_id", characterAPI.GetDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/private-detail-char", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected response code 404, got %d", resp.Code)
	}
	if resp.Data != nil {
		t.Fatalf("expected no private detail data, got %#v", resp.Data)
	}
}

func TestPublicCharacterDetailRejectsDraftCharacters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	characterAPI := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})

	if err := db.Create(&database.Character{
		CharacterID: "draft-detail-char",
		UserID:      "owner",
		Name:        "草稿详情角色",
		Gender:      "female",
		IsPublic:    0,
	}).Error; err != nil {
		t.Fatalf("create draft character: %v", err)
	}

	router := gin.New()
	group := router.Group("/api/v1/character")
	group.GET("/:character_id", characterAPI.GetDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/draft-detail-char", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	resp := decodeResponse(t, recorder)
	if recorder.Code != http.StatusNotFound || resp.Code != http.StatusNotFound {
		t.Fatalf("expected status/code 404, got status=%d code=%d body=%s", recorder.Code, resp.Code, recorder.Body.String())
	}
	if resp.Data != nil {
		t.Fatalf("expected no draft detail data, got %#v", resp.Data)
	}
	if resp.ErrorCode != "not_found" {
		t.Fatalf("expected not_found error_code, got %q", resp.ErrorCode)
	}
}

func TestPublicCharacterDetailReturnsNotFoundForMissingCharacter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	characterAPI := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})

	router := gin.New()
	group := router.Group("/api/v1/character")
	group.GET("/:character_id", characterAPI.GetDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/missing-detail-char", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	resp := decodeResponse(t, recorder)
	if recorder.Code != http.StatusNotFound || resp.Code != http.StatusNotFound {
		t.Fatalf("expected status/code 404, got status=%d code=%d body=%s", recorder.Code, resp.Code, recorder.Body.String())
	}
	if resp.Data != nil {
		t.Fatalf("expected no missing detail data, got %#v", resp.Data)
	}
	if resp.ErrorCode != "not_found" {
		t.Fatalf("expected not_found error_code, got %q", resp.ErrorCode)
	}
}

func TestGetMyCharactersIgnoresQueryUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	characterAPI := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})

	if err := db.Create(&database.Character{
		CharacterID: "owner-char",
		UserID:      "owner",
		Name:        "Owner",
		Gender:      "female",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatalf("create owner character: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "other-char",
		UserID:      "other",
		Name:        "Other",
		Gender:      "male",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatalf("create other character: %v", err)
	}

	router := gin.New()
	api := router.Group("/api/v1")
	authCharacter := api.Group("/character")
	authCharacter.Use(middleware.Auth())
	authCharacter.GET("/my", characterAPI.GetMyCharacters)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/character/my?user_id=other&page=1&page_size=20", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))

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

	if total, ok := data["total"].(float64); !ok || total != 1 {
		t.Fatalf("expected total=1, got %#v", data["total"])
	}

	list, ok := data["list"].([]interface{})
	if !ok {
		t.Fatalf("expected list array, got %#v", data["list"])
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 character, got %d", len(list))
	}

	item, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected character object, got %#v", list[0])
	}
	if item["character_id"] != "owner-char" {
		t.Fatalf("expected owner character, got %#v", item["character_id"])
	}
}

func TestUpdateCharacterRejectsCrossUserAccessWithStableNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	characterAPI := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})

	if err := db.Create(&database.Character{
		CharacterID: "other-update-char",
		UserID:      "other",
		Name:        "Other Before",
		Gender:      "female",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatalf("create other character: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestID())
	api := router.Group("/api/v1")
	authCharacter := api.Group("/character")
	authCharacter.Use(middleware.Auth())
	authCharacter.PUT("/:character_id", characterAPI.Update)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/character/other-update-char", bytes.NewReader([]byte(`{"name":"Owner Rewrite"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))
	req.Header.Set("X-Request-ID", "character-update-cross-user-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "character_not_found" {
		t.Fatalf("expected character_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "character-update-cross-user-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "character_id" || details["reason"] != "not_found_or_not_owned" {
		t.Fatalf("unexpected details: %#v", details)
	}

	var character database.Character
	if err := db.Where("character_id = ?", "other-update-char").First(&character).Error; err != nil {
		t.Fatalf("reload character: %v", err)
	}
	if character.Name != "Other Before" {
		t.Fatalf("expected cross-user update not to mutate character, got %q", character.Name)
	}
}

func TestDeleteCharacterReturnsStableNotFoundForMissingOrCrossUserCharacter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	characterAPI := apiv1.NewCharacterAPI(&service.CharacterService{DB: db})

	if err := db.Create(&database.Character{
		CharacterID: "other-delete-char",
		UserID:      "other",
		Name:        "Other Delete",
		Gender:      "female",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatalf("create other character: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestID())
	api := router.Group("/api/v1")
	authCharacter := api.Group("/character")
	authCharacter.Use(middleware.Auth())
	authCharacter.DELETE("/:character_id", characterAPI.Delete)

	tests := []struct {
		name        string
		characterID string
	}{
		{name: "cross_user", characterID: "other-delete-char"},
		{name: "missing", characterID: "missing-delete-char"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/character/"+tt.characterID, nil)
			req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))
			req.Header.Set("X-Request-ID", "character-delete-not-found-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status 404, got %d body=%s", recorder.Code, recorder.Body.String())
			}
			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "character_not_found" {
				t.Fatalf("expected character_not_found error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "character-delete-not-found-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
			details, ok := resp.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("expected details object, got %#v", resp.Details)
			}
			if details["field"] != "character_id" || details["reason"] != "not_found_or_not_owned" {
				t.Fatalf("unexpected details: %#v", details)
			}
		})
	}

	var count int64
	if err := db.Model(&database.Character{}).Where("character_id = ?", "other-delete-char").Count(&count).Error; err != nil {
		t.Fatalf("count other character: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected cross-user delete not to remove character, got count %d", count)
	}
}

func TestUpdateMemoryReturnsStableNotFoundForMissingOrCrossUserMemory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	memoryAPI := apiv1.NewMemoryAPI(&service.MemoryService{DB: db})

	if err := db.Create(&database.Memory{
		MemoryID:    "other-memory-update",
		UserID:      "other",
		CharacterID: "char-1",
		Content:     "keep me",
	}).Error; err != nil {
		t.Fatalf("create other memory: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestID())
	api := router.Group("/api/v1")
	authMemory := api.Group("/memory")
	authMemory.Use(middleware.Auth())
	authMemory.PUT("/:memory_id", memoryAPI.Update)

	tests := []struct {
		name     string
		memoryID string
	}{
		{name: "cross_user", memoryID: "other-memory-update"},
		{name: "missing", memoryID: "missing-memory-update"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/memory/"+tt.memoryID, bytes.NewReader([]byte(`{"content":"new"}`)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))
			req.Header.Set("X-Request-ID", "memory-update-not-found-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status 404, got %d body=%s", recorder.Code, recorder.Body.String())
			}
			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "memory_not_found" {
				t.Fatalf("expected memory_not_found error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "memory-update-not-found-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
			details, ok := resp.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("expected details object, got %#v", resp.Details)
			}
			if details["field"] != "memory_id" || details["reason"] != "not_found_or_not_owned" {
				t.Fatalf("unexpected details: %#v", details)
			}
		})
	}

	var memory database.Memory
	if err := db.Where("memory_id = ?", "other-memory-update").First(&memory).Error; err != nil {
		t.Fatalf("reload other memory: %v", err)
	}
	if memory.Content != "keep me" {
		t.Fatalf("expected cross-user update not to mutate memory, got %q", memory.Content)
	}
}

func TestDeleteMemoryReturnsStableNotFoundForMissingOrCrossUserMemory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	memoryAPI := apiv1.NewMemoryAPI(&service.MemoryService{DB: db})

	if err := db.Create(&database.Memory{
		MemoryID:    "other-memory-delete",
		UserID:      "other",
		CharacterID: "char-1",
		Content:     "keep me",
	}).Error; err != nil {
		t.Fatalf("create other memory: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestID())
	api := router.Group("/api/v1")
	authMemory := api.Group("/memory")
	authMemory.Use(middleware.Auth())
	authMemory.DELETE("/:memory_id", memoryAPI.Delete)

	tests := []struct {
		name     string
		memoryID string
	}{
		{name: "cross_user", memoryID: "other-memory-delete"},
		{name: "missing", memoryID: "missing-memory-delete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/memory/"+tt.memoryID, nil)
			req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))
			req.Header.Set("X-Request-ID", "memory-delete-not-found-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status 404, got %d body=%s", recorder.Code, recorder.Body.String())
			}
			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "memory_not_found" {
				t.Fatalf("expected memory_not_found error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "memory-delete-not-found-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
			details, ok := resp.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("expected details object, got %#v", resp.Details)
			}
			if details["field"] != "memory_id" || details["reason"] != "not_found_or_not_owned" {
				t.Fatalf("unexpected details: %#v", details)
			}
		})
	}

	var count int64
	if err := db.Model(&database.Memory{}).Where("memory_id = ?", "other-memory-delete").Count(&count).Error; err != nil {
		t.Fatalf("count other memory: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected cross-user delete not to remove memory, got count %d", count)
	}
}

func TestGetMessageReadStatusRejectsCrossUserAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})

	if err := db.Create(&database.Message{
		MessageID:   "other-msg",
		SessionID:   "session-1",
		CharacterID: "char-1",
		UserID:      "other",
		Role:        "assistant",
		Content:     "secret",
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	router := gin.New()
	api := router.Group("/api/v1")
	authChat := api.Group("/chat")
	authChat.Use(middleware.Auth())
	authChat.GET("/message/read", chatAPI.GetMessageReadStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/message/read?message_id=other-msg", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "owner"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "message_not_found" {
		t.Fatalf("expected message_not_found error_code, got %q", resp.ErrorCode)
	}
}
