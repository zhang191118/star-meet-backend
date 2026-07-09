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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupChatMessageIdempotencyAPITestDB(t *testing.T) *gorm.DB {
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
		&database.Message{},
		&database.Memory{},
	); err != nil {
		t.Fatalf("migrate chat idempotency tables: %v", err)
	}

	return db
}

func TestChatHandlersReturnUnauthorizedWhenUserContextMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.POST("/stream", chatAPI.StreamMessage)
	group.POST("/message", chatAPI.SendMessage)
	group.GET("/history", chatAPI.GetHistory)
	group.GET("/sessions", chatAPI.GetSessions)
	group.DELETE("/session/:session_id", chatAPI.DeleteSession)
	group.PUT("/session/:session_id", chatAPI.UpdateSession)
	group.DELETE("/session", chatAPI.DeleteSessionByCharacter)
	group.DELETE("/clear-history", chatAPI.ClearChatHistory)
	group.POST("/message/read", chatAPI.MarkMessageRead)
	group.DELETE("/message/:message_id", chatAPI.RecallMessage)
	group.GET("/message/read", chatAPI.GetMessageReadStatus)

	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{name: "stream", method: http.MethodPost, path: "/api/v1/chat/stream", body: []byte(`{"character_id":"char-1","message":"hello"}`)},
		{name: "send", method: http.MethodPost, path: "/api/v1/chat/message", body: []byte(`{"character_id":"char-1","message":"hello"}`)},
		{name: "history", method: http.MethodGet, path: "/api/v1/chat/history?character_id=char-1"},
		{name: "sessions", method: http.MethodGet, path: "/api/v1/chat/sessions"},
		{name: "delete_session", method: http.MethodDelete, path: "/api/v1/chat/session/session-1"},
		{name: "update_session", method: http.MethodPut, path: "/api/v1/chat/session/session-1", body: []byte(`{"is_pinned":true}`)},
		{name: "delete_session_by_character", method: http.MethodDelete, path: "/api/v1/chat/session?character_id=char-1"},
		{name: "clear_history", method: http.MethodDelete, path: "/api/v1/chat/clear-history?character_id=char-1"},
		{name: "mark_read", method: http.MethodPost, path: "/api/v1/chat/message/read", body: []byte(`{"message_id":"msg-1","character_id":"char-1"}`)},
		{name: "recall", method: http.MethodDelete, path: "/api/v1/chat/message/msg-1"},
		{name: "read_status", method: http.MethodGet, path: "/api/v1/chat/message/read?message_id=msg-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", "chat-missing-user-context-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d body=%s", recorder.Code, recorder.Body.String())
			}
			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "unauthorized" {
				t.Fatalf("expected unauthorized error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "chat-missing-user-context-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
		})
	}
}

func TestSendMessageReusesClientMessageIDsOnRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-retry-user", Nickname: "重试用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "chat-retry-char",
		UserID:       "system_admin",
		Name:         "重试角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/message", chatAPI.SendMessage)

	payload := map[string]interface{}{
		"character_id":     "chat-retry-char",
		"message":          "这条消息会重试",
		"message_type":     "text",
		"user_message_id":  "api-user-msg-1",
		"reply_message_id": "api-reply-msg-1",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-retry-user"))

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d expected status 200, got %d body=%s", i+1, recorder.Code, recorder.Body.String())
		}

		resp := decodeResponse(t, recorder)
		body, ok := resp.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("request %d expected object data, got %#v", i+1, resp.Data)
		}
		if body["message_id"] != "api-reply-msg-1" {
			t.Fatalf("request %d expected reused reply id, got %#v", i+1, body["message_id"])
		}
	}

	var userMessageCount int64
	if err := db.Model(&database.Message{}).Where("message_id = ? AND role = ?", "api-user-msg-1", "user").Count(&userMessageCount).Error; err != nil {
		t.Fatalf("count user messages: %v", err)
	}
	if userMessageCount != 1 {
		t.Fatalf("expected one user message after retry, got %d", userMessageCount)
	}

	var replyMessageCount int64
	if err := db.Model(&database.Message{}).Where("message_id = ? AND role = ?", "api-reply-msg-1", "assistant").Count(&replyMessageCount).Error; err != nil {
		t.Fatalf("count reply messages: %v", err)
	}
	if replyMessageCount != 1 {
		t.Fatalf("expected one reply message after retry, got %d", replyMessageCount)
	}
}

func TestSendMessageRejectsUnavailableCharacterWithoutWritingChatData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-missing-character-user", Nickname: "缺角色用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/message", chatAPI.SendMessage)

	payload := []byte(`{"character_id":"missing-chat-character","message":"你好","user_message_id":"missing-char-user-msg","reply_message_id":"missing-char-reply-msg"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-missing-character-user"))
	req.Header.Set("X-Request-ID", "chat-missing-character-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing character, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "character_unavailable" {
		t.Fatalf("expected character_unavailable error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-missing-character-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}

	for _, table := range []struct {
		name  string
		model interface{}
	}{
		{name: "sessions", model: &database.Session{}},
		{name: "messages", model: &database.Message{}},
		{name: "memories", model: &database.Memory{}},
	} {
		var count int64
		if err := db.Model(table.model).Where("user_id = ?", "chat-missing-character-user").Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table.name, err)
		}
		if count != 0 {
			t.Fatalf("expected missing character request not to create %s, got %d", table.name, count)
		}
	}
}

func TestSendMessageRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-malformed-user", Nickname: "坏 JSON 用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/message", chatAPI.SendMessage)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", bytes.NewReader([]byte(`{"character_id":"chat-char","message"`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-malformed-user"))
	req.Header.Set("X-Request-ID", "chat-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-malformed-json-req" {
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
		t.Fatalf("expected reason detail, got %#v", details["reason"])
	}
}

func TestSendMessageRejectsMissingRequiredFieldsWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-validation-user", Nickname: "发送校验用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/message", chatAPI.SendMessage)

	tests := []struct {
		name  string
		body  string
		field string
	}{
		{name: "missing_character", body: `{"message":"hello"}`, field: "character_id"},
		{name: "missing_message", body: `{"character_id":"chat-validation-char"}`, field: "message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-validation-user"))
			req.Header.Set("X-Request-ID", "chat-send-validation-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected status 422 for missing %s, got %d body=%s", tt.field, recorder.Code, recorder.Body.String())
			}

			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "validation_failed" {
				t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "chat-send-validation-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
			details, ok := resp.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("expected details object, got %#v", resp.Details)
			}
			if details["field"] != tt.field {
				t.Fatalf("expected field detail %q, got %#v", tt.field, details["field"])
			}
			if _, ok := details["reason"].(string); !ok {
				t.Fatalf("expected reason detail, got %#v", details["reason"])
			}
		})
	}
}

func TestStreamMessageRejectsMissingRequiredFieldsWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-stream-validation-user", Nickname: "流式校验用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/stream", chatAPI.StreamMessage)

	tests := []struct {
		name  string
		body  string
		field string
	}{
		{name: "missing_character", body: `{"message":"hello"}`, field: "character_id"},
		{name: "missing_message", body: `{"character_id":"chat-stream-validation-char"}`, field: "message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/stream", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-stream-validation-user"))
			req.Header.Set("X-Request-ID", "chat-stream-validation-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected status 422 for missing %s, got %d body=%s", tt.field, recorder.Code, recorder.Body.String())
			}

			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "validation_failed" {
				t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "chat-stream-validation-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
			details, ok := resp.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("expected details object, got %#v", resp.Details)
			}
			if details["field"] != tt.field {
				t.Fatalf("expected field detail %q, got %#v", tt.field, details["field"])
			}
			if _, ok := details["reason"].(string); !ok {
				t.Fatalf("expected reason detail, got %#v", details["reason"])
			}
		})
	}
}

func TestMarkMessageReadRejectsMissingMessageIDWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-read-user", Nickname: "已读用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/message/read", chatAPI.MarkMessageRead)

	payload := []byte(`{"character_id":"chat-read-char"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message/read", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-read-user"))
	req.Header.Set("X-Request-ID", "chat-read-missing-message-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing message_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-read-missing-message-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "message_id" {
		t.Fatalf("expected message_id field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected reason detail, got %#v", details["reason"])
	}
}

func TestMarkMessageReadReturnsStableNotFoundForMissingMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-read-missing-target-user", Nickname: "已读目标用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/message/read", chatAPI.MarkMessageRead)

	payload := []byte(`{"message_id":"missing-message-id","character_id":"chat-read-char"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/message/read", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-read-missing-target-user"))
	req.Header.Set("X-Request-ID", "chat-read-message-not-found-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing message, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "message_not_found" {
		t.Fatalf("expected message_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-read-message-not-found-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "message_id" {
		t.Fatalf("expected message_id field detail, got %#v", details["field"])
	}
}

func TestGetMessageReadStatusRejectsMissingMessageIDWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-read-status-user", Nickname: "已读状态用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.GET("/message/read", chatAPI.GetMessageReadStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/message/read", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-read-status-user"))
	req.Header.Set("X-Request-ID", "chat-read-status-missing-message-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing message_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-read-status-missing-message-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "message_id" {
		t.Fatalf("expected message_id field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected reason detail, got %#v", details["reason"])
	}
}

func TestGetMessageReadStatusReturnsZeroReadAtForUnreadMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-unread-status-user", Nickname: "未读状态用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "chat-unread-status-msg",
		SessionID:   "chat-unread-status-session",
		CharacterID: "chat-unread-status-char",
		UserID:      "chat-unread-status-user",
		Role:        "assistant",
		Content:     "hello",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.GET("/message/read", chatAPI.GetMessageReadStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/message/read?message_id=chat-unread-status-msg", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-unread-status-user"))
	req.Header.Set("X-Request-ID", "chat-unread-status-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 for unread message, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", resp.Data)
	}
	if data["is_read"] != false {
		t.Fatalf("expected unread status, got %#v", data["is_read"])
	}
	if data["read_at"] != float64(0) {
		t.Fatalf("expected read_at 0 for unread message, got %#v", data["read_at"])
	}
}

func TestUpdateSessionRequiresAtLeastOneMutableField(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-update-session-user", Nickname: "会话更新用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "chat-update-session-char",
		UserID:       "system_admin",
		Name:         "会话更新角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "chat-update-session-id",
		UserID:      "chat-update-session-user",
		CharacterID: "chat-update-session-char",
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.PUT("/session/:session_id", chatAPI.UpdateSession)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/chat/session/chat-update-session-id", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-update-session-user"))
	req.Header.Set("X-Request-ID", "chat-update-session-empty-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing mutable fields, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-update-session-empty-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "is_pinned" {
		t.Fatalf("expected is_pinned field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected reason detail, got %#v", details["reason"])
	}
}

func TestUpdateSessionRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-update-malformed-user", Nickname: "会话坏 JSON 用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.PUT("/session/:session_id", chatAPI.UpdateSession)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/chat/session/chat-update-any", bytes.NewReader([]byte(`{"is_pinned"`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-update-malformed-user"))
	req.Header.Set("X-Request-ID", "chat-update-session-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-update-session-malformed-json-req" {
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
		t.Fatalf("expected reason detail, got %#v", details["reason"])
	}
}

func TestUpdateSessionReturnsStableNotFoundForMissingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-update-missing-session-user", Nickname: "会话不存在用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.PUT("/session/:session_id", chatAPI.UpdateSession)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/chat/session/missing-session-id", bytes.NewReader([]byte(`{"is_pinned":true}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-update-missing-session-user"))
	req.Header.Set("X-Request-ID", "chat-update-session-not-found-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing session, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "session_not_found" {
		t.Fatalf("expected session_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-update-session-not-found-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "session_id" {
		t.Fatalf("expected session_id field detail, got %#v", details["field"])
	}
}

func TestDeleteSessionReturnsStableNotFoundForMissingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-delete-missing-session-user", Nickname: "删除会话用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.DELETE("/session/:session_id", chatAPI.DeleteSession)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/chat/session/missing-session-id", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-delete-missing-session-user"))
	req.Header.Set("X-Request-ID", "chat-delete-session-not-found-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing session, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "session_not_found" {
		t.Fatalf("expected session_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-delete-session-not-found-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "session_id" {
		t.Fatalf("expected session_id field detail, got %#v", details["field"])
	}
}

func TestRecallMessageRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-recall-user", Nickname: "撤回用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "chat-recall-msg",
		SessionID:   "chat-recall-session",
		CharacterID: "chat-recall-char",
		UserID:      "chat-recall-user",
		Role:        "user",
		Content:     "bad json should not recall me",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.DELETE("/message/:message_id", chatAPI.RecallMessage)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/chat/message/chat-recall-msg", bytes.NewReader([]byte(`{"reason"`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-recall-user"))
	req.Header.Set("X-Request-ID", "chat-recall-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-recall-malformed-json-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "body" {
		t.Fatalf("expected body field detail, got %#v", details["field"])
	}

	var message database.Message
	if err := db.Where("message_id = ?", "chat-recall-msg").First(&message).Error; err != nil {
		t.Fatalf("reload message: %v", err)
	}
	if message.IsRecalled {
		t.Fatalf("expected malformed JSON not to recall message")
	}
	if message.Content != "bad json should not recall me" {
		t.Fatalf("expected original content to remain, got %q", message.Content)
	}
}

func TestRecallMessageReturnsStableNotFoundForMissingMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-recall-missing-target-user", Nickname: "撤回目标用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.DELETE("/message/:message_id", chatAPI.RecallMessage)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/chat/message/missing-message-id", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-recall-missing-target-user"))
	req.Header.Set("X-Request-ID", "chat-recall-message-not-found-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing message, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "message_not_found" {
		t.Fatalf("expected message_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "chat-recall-message-not-found-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "message_id" {
		t.Fatalf("expected message_id field detail, got %#v", details["field"])
	}
}

func TestChatCharacterQueryActionsRejectMissingCharacterIDWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-query-user", Nickname: "查询参数用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.DELETE("/session", chatAPI.DeleteSessionByCharacter)
	group.DELETE("/clear-history", chatAPI.ClearChatHistory)

	tests := []struct {
		name string
		path string
	}{
		{name: "delete_session", path: "/api/v1/chat/session"},
		{name: "clear_history", path: "/api/v1/chat/clear-history"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-query-user"))
			req.Header.Set("X-Request-ID", "chat-query-missing-character-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected status 422 for missing character_id, got %d body=%s", recorder.Code, recorder.Body.String())
			}

			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "validation_failed" {
				t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "chat-query-missing-character-"+tt.name {
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
		})
	}
}

func TestChatCharacterQueryActionsReturnStableNotFoundForMissingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupChatMessageIdempotencyAPITestDB(t)
	if err := db.Create(&database.User{UserID: "chat-query-not-found-user", Nickname: "查询不存在用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.DELETE("/session", chatAPI.DeleteSessionByCharacter)
	group.DELETE("/clear-history", chatAPI.ClearChatHistory)

	tests := []struct {
		name string
		path string
	}{
		{name: "delete_session", path: "/api/v1/chat/session?character_id=missing-character-session"},
		{name: "clear_history", path: "/api/v1/chat/clear-history?character_id=missing-character-session"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "chat-query-not-found-user"))
			req.Header.Set("X-Request-ID", "chat-query-session-not-found-"+tt.name)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status 404 for missing session, got %d body=%s", recorder.Code, recorder.Body.String())
			}

			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != "session_not_found" {
				t.Fatalf("expected session_not_found error_code, got %q", resp.ErrorCode)
			}
			if resp.RequestID != "chat-query-session-not-found-"+tt.name {
				t.Fatalf("expected request id in response, got %q", resp.RequestID)
			}
			details, ok := resp.Details.(map[string]interface{})
			if !ok {
				t.Fatalf("expected details object, got %#v", resp.Details)
			}
			if details["field"] != "session_id" {
				t.Fatalf("expected session_id field detail, got %#v", details["field"])
			}
		})
	}
}
