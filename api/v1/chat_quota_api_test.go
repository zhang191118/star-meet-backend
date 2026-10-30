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
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupChatQuotaAPITest(t *testing.T, limit int) (*gorm.DB, *gin.Engine, *service.RuntimeConfigService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

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
		&database.Block{},
		&database.RuntimeConfig{},
	); err != nil {
		t.Fatalf("migrate quota api tables: %v", err)
	}

	runtimeConfig := &service.RuntimeConfigService{DB: db}
	if err := runtimeConfig.SeedDefaults(service.ChatQuotaRuntimeDefaults()); err != nil {
		t.Fatalf("seed quota defaults: %v", err)
	}
	if _, err := runtimeConfig.Upsert(service.RuntimeConfigChatQuotaFreeDailyMessages, fmt.Sprintf("%d", limit), false, ""); err != nil {
		t.Fatalf("upsert daily messages: %v", err)
	}

	chatAPI := apiv1.NewChatAPI(&service.ChatService{
		DB:                 db,
		OpenAICompatClient: configuredChatTestAIClient(),
		RuntimeConfig:      runtimeConfig,
	})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/chat")
	group.Use(middleware.Auth())
	group.POST("/message", chatAPI.SendMessage)
	group.POST("/stream", chatAPI.StreamMessage)
	return db, router, runtimeConfig
}

func createChatQuotaAPIUser(t *testing.T, db *gorm.DB, userID, email, level string, expire *time.Time) {
	t.Helper()
	if err := db.Create(&database.User{
		UserID:       userID,
		Email:        email,
		Nickname:     userID,
		MemberLevel:  level,
		MemberExpire: expire,
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func createChatQuotaAPICharacter(t *testing.T, db *gorm.DB, characterID string) {
	t.Helper()
	if err := db.Create(&database.Character{
		CharacterID:  characterID,
		UserID:       "system_admin",
		Name:         "额度接口角色",
		Gender:       "female",
		Relationship: "伙伴",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
}

func seedChatQuotaAPIUserMessages(t *testing.T, db *gorm.DB, userID, characterID string, count int) {
	t.Helper()
	now := time.Now()
	for i := 0; i < count; i++ {
		if err := db.Create(&database.Message{
			MessageID:   fmt.Sprintf("%s-seed-%d", userID, i),
			SessionID:   userID + "-session",
			CharacterID: characterID,
			UserID:      userID,
			Role:        "user",
			Content:     fmt.Sprintf("seed-%d", i),
			MessageType: "text",
			CreatedAt:   now,
		}).Error; err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
}

func postChatJSON(t *testing.T, router *gin.Engine, path, userID, requestID string, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, userID))
	req.Header.Set("X-Request-ID", requestID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func assertQuotaForbidden(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "chat_quota_exceeded" {
		t.Fatalf("expected chat_quota_exceeded, got %q body=%s", resp.ErrorCode, recorder.Body.String())
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["membership_required"] != true {
		t.Fatalf("expected membership_required=true, got %#v", details["membership_required"])
	}
}

func TestSendMessageReturns403WhenFreeQuotaExceeded(t *testing.T) {
	db, router, _ := setupChatQuotaAPITest(t, 2)
	createChatQuotaAPIUser(t, db, "quota-send-user", "quota-send-user@example.com", "free", nil)
	createChatQuotaAPICharacter(t, db, "quota-send-char")
	seedChatQuotaAPIUserMessages(t, db, "quota-send-user", "quota-send-char", 2)

	recorder := postChatJSON(t, router, "/api/v1/chat/message", "quota-send-user", "quota-send-req", map[string]interface{}{
		"character_id": "quota-send-char",
		"message":      "第3条应该被拦截",
	})
	assertQuotaForbidden(t, recorder)

	var count int64
	if err := db.Model(&database.Message{}).Where("user_id = ? AND role = ?", "quota-send-user", "user").Count(&count).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 2 {
		t.Fatalf("blocked send should not write a new user message, got %d", count)
	}
}

func TestStreamMessageReturnsJSON403WhenFreeQuotaExceeded(t *testing.T) {
	db, router, _ := setupChatQuotaAPITest(t, 2)
	createChatQuotaAPIUser(t, db, "quota-stream-user", "quota-stream-user@example.com", "free", nil)
	createChatQuotaAPICharacter(t, db, "quota-stream-char")
	seedChatQuotaAPIUserMessages(t, db, "quota-stream-user", "quota-stream-char", 2)

	recorder := postChatJSON(t, router, "/api/v1/chat/stream", "quota-stream-user", "quota-stream-req", map[string]interface{}{
		"character_id": "quota-stream-char",
		"message":      "流式第3条应该被拦截",
	})
	assertQuotaForbidden(t, recorder)
	if contentType := recorder.Header().Get("Content-Type"); contentType == "text/event-stream" {
		t.Fatalf("quota error should not start SSE, content-type=%s", contentType)
	}
}

func TestSendMessageAllowsTwentiethAndBlocksTwentyFirst(t *testing.T) {
	db, router, _ := setupChatQuotaAPITest(t, 20)
	createChatQuotaAPIUser(t, db, "quota-20-user", "quota-20-user@example.com", "free", nil)
	createChatQuotaAPICharacter(t, db, "quota-20-char")
	seedChatQuotaAPIUserMessages(t, db, "quota-20-user", "quota-20-char", 19)

	okRecorder := postChatJSON(t, router, "/api/v1/chat/message", "quota-20-user", "quota-20-ok", map[string]interface{}{
		"character_id":     "quota-20-char",
		"message":          "第20条应该通过",
		"user_message_id":  "quota-20-user-msg",
		"reply_message_id": "quota-20-reply-msg",
	})
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("20th message should succeed, got %d body=%s", okRecorder.Code, okRecorder.Body.String())
	}

	blocked := postChatJSON(t, router, "/api/v1/chat/message", "quota-20-user", "quota-20-block", map[string]interface{}{
		"character_id": "quota-20-char",
		"message":      "第21条应该失败",
	})
	assertQuotaForbidden(t, blocked)
}

func TestSendMessageAllowsIdempotentRetryAtQuotaLimit(t *testing.T) {
	db, router, _ := setupChatQuotaAPITest(t, 1)
	createChatQuotaAPIUser(t, db, "quota-retry-user", "quota-retry-user@example.com", "free", nil)
	createChatQuotaAPICharacter(t, db, "quota-retry-char")

	payload := map[string]interface{}{
		"character_id":     "quota-retry-char",
		"message":          "幂等重试",
		"user_message_id":  "quota-retry-user-msg",
		"reply_message_id": "quota-retry-reply-msg",
	}
	first := postChatJSON(t, router, "/api/v1/chat/message", "quota-retry-user", "quota-retry-1", payload)
	if first.Code != http.StatusOK {
		t.Fatalf("first send should succeed, got %d body=%s", first.Code, first.Body.String())
	}
	second := postChatJSON(t, router, "/api/v1/chat/message", "quota-retry-user", "quota-retry-2", payload)
	if second.Code != http.StatusOK {
		t.Fatalf("idempotent retry should succeed at quota limit, got %d body=%s", second.Code, second.Body.String())
	}
}

func TestSendMessageAllowsUnlimitedActiveMember(t *testing.T) {
	db, router, _ := setupChatQuotaAPITest(t, 1)
	expire := time.Now().Add(24 * time.Hour)
	createChatQuotaAPIUser(t, db, "quota-vip-user", "quota-vip-user@example.com", "monthly", &expire)
	createChatQuotaAPICharacter(t, db, "quota-vip-char")
	seedChatQuotaAPIUserMessages(t, db, "quota-vip-user", "quota-vip-char", 3)

	recorder := postChatJSON(t, router, "/api/v1/chat/message", "quota-vip-user", "quota-vip-req", map[string]interface{}{
		"character_id": "quota-vip-char",
		"message":      "会员不受每日条数限制",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("active member should send unlimited, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSendMessageBlocksExpiredMemberWithFreeQuota(t *testing.T) {
	db, router, _ := setupChatQuotaAPITest(t, 1)
	expire := time.Now().Add(-time.Hour)
	createChatQuotaAPIUser(t, db, "quota-expired-user", "quota-expired-user@example.com", "monthly", &expire)
	createChatQuotaAPICharacter(t, db, "quota-expired-char")
	seedChatQuotaAPIUserMessages(t, db, "quota-expired-user", "quota-expired-char", 1)

	recorder := postChatJSON(t, router, "/api/v1/chat/message", "quota-expired-user", "quota-expired-req", map[string]interface{}{
		"character_id": "quota-expired-char",
		"message":      "过期会员应按免费拦截",
	})
	assertQuotaForbidden(t, recorder)
}
