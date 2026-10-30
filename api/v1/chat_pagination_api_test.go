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
)

func TestGetHistoryIncludesPaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	now := time.Now()
	if err := db.Create(&database.User{UserID: "history-page-user", Nickname: "历史分页用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "history-page-char",
		UserID:       "system_admin",
		Name:         "历史分页角色",
		Gender:       "female",
		Relationship: "伙伴",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "history-page-session",
		UserID:      "history-page-user",
		CharacterID: "history-page-char",
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := 0; i < 101; i++ {
		if err := db.Create(&database.Message{
			MessageID:   fmt.Sprintf("history-page-msg-%03d", i),
			SessionID:   "history-page-session",
			CharacterID: "history-page-char",
			UserID:      "history-page-user",
			Role:        "user",
			Content:     fmt.Sprintf("message %d", i),
			MessageType: "text",
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.GET("/history", chatAPI.GetHistory)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/history?character_id=history-page-char&page=1&page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "history-page-user"))

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
}

func TestGetHistorySupportsBeforeIDCursorMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	now := time.Now()
	if err := db.Create(&database.User{UserID: "history-cursor-user", Nickname: "历史游标用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "history-cursor-char",
		UserID:       "system_admin",
		Name:         "历史游标角色",
		Gender:       "female",
		Relationship: "伙伴",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "history-cursor-session",
		UserID:      "history-cursor-user",
		CharacterID: "history-cursor-char",
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := db.Create(&database.Message{
			MessageID:   fmt.Sprintf("history-cursor-msg-%03d", i),
			SessionID:   "history-cursor-session",
			CharacterID: "history-cursor-char",
			UserID:      "history-cursor-user",
			Role:        "user",
			Content:     fmt.Sprintf("message %d", i),
			MessageType: "text",
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.GET("/history", chatAPI.GetHistory)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/history?character_id=history-cursor-char&before_id=history-cursor-msg-003&page_size=2", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "history-cursor-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	data := decodePaginationData(t, recorder)
	if data["before_id"] != "history-cursor-msg-003" {
		t.Fatalf("expected before_id echo, got %#v", data["before_id"])
	}
	if data["next_before_id"] != "history-cursor-msg-001" {
		t.Fatalf("expected next_before_id from last item, got %#v", data["next_before_id"])
	}
	if data["total"] != float64(3) {
		t.Fatalf("expected remaining total 3, got %#v", data["total"])
	}

	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 2 {
		t.Fatalf("expected 2 list items, got %#v", data["list"])
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first item object, got %#v", list[0])
	}
	if first["message_id"] != "history-cursor-msg-002" {
		t.Fatalf("expected newest older message first, got %#v", first["message_id"])
	}
}

func TestGetHistoryRejectsMissingCharacterIDWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "history-missing-char-user", Nickname: "历史缺参用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.GET("/history", chatAPI.GetHistory)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/history", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "history-missing-char-user"))
	req.Header.Set("X-Request-ID", "chat-history-missing-character-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing character_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-history-missing-character-req" {
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
		t.Fatalf("expected reason detail, got %#v", details["reason"])
	}
}

func TestGetSessionsIncludesPaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	now := time.Now()
	if err := db.Create(&database.User{UserID: "sessions-page-user", Nickname: "会话分页用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	for i := 0; i < 101; i++ {
		characterID := fmt.Sprintf("sessions-page-char-%03d", i)
		if err := db.Create(&database.Character{
			CharacterID:  characterID,
			UserID:       "system_admin",
			Name:         fmt.Sprintf("会话角色 %d", i),
			Gender:       "female",
			Relationship: "伙伴",
			IsPublic:     1,
		}).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
		lastAt := now.Add(time.Duration(i) * time.Second)
		if err := db.Create(&database.Session{
			SessionID:     fmt.Sprintf("sessions-page-%03d", i),
			UserID:        "sessions-page-user",
			CharacterID:   characterID,
			LastMessage:   "hello",
			LastMessageAt: &lastAt,
		}).Error; err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.GET("/sessions", chatAPI.GetSessions)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/sessions?page=1&page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "sessions-page-user"))

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
}

func decodePaginationData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()

	resp := decodeResponse(t, recorder)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %#v", resp.Data)
	}
	return data
}
