package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"star-meet-backend/core/minimax"
	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type chatRoundTripFunc func(*http.Request) (*http.Response, error)

func (f chatRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func setupChatStreamMetadataTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(
		&database.User{},
		&database.UserSettings{},
		&database.Character{},
		&database.Session{},
		&database.Message{},
		&database.Block{},
	); err != nil {
		t.Fatalf("migrate chat tables: %v", err)
	}

	return db
}

func TestStreamMessageMetaIncludesClientAndSessionIDs(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "stream-user", Nickname: "流式用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "stream-char",
		UserID:       "system_admin",
		Name:         "流式角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}

	service := &ChatService{DB: db}
	events, errs := service.StreamMessage("stream-user", "stream-char", "你好", "text", "client-user-msg", "client-reply-msg", "")

	event := <-events
	if event.Type != "meta" || event.Meta == nil {
		t.Fatalf("expected first event to be meta, got %+v", event)
	}
	if event.Meta.SessionID == "" {
		t.Fatalf("expected session id in meta, got %+v", event.Meta)
	}
	if event.Meta.UserMessageID != "client-user-msg" {
		t.Fatalf("expected user message id in meta, got %+v", event.Meta)
	}
	if event.Meta.ReplyMessageID != "client-reply-msg" {
		t.Fatalf("expected reply message id in meta, got %+v", event.Meta)
	}
	if event.Meta.CreatedAt <= 0 {
		t.Fatalf("expected created_at in meta, got %+v", event.Meta)
	}

	if err := <-errs; !errors.Is(err, ErrAIServiceUnavailable) {
		t.Fatalf("expected unavailable stream client to report AI unavailable, got %v", err)
	}

	var assistantCount int64
	if err := db.Model(&database.Message{}).Where("message_id = ? AND role = ?", "client-reply-msg", "assistant").Count(&assistantCount).Error; err != nil {
		t.Fatalf("count assistant replies: %v", err)
	}
	if assistantCount != 0 {
		t.Fatalf("expected unavailable AI to avoid saving a template reply, got %d assistant messages", assistantCount)
	}
}

func TestSendMessageDoesNotPersistTemplateWhenAIUnavailable(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "send-unavailable-user", Nickname: "发送用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "send-unavailable-char",
		UserID:       "system_admin",
		Name:         "发送角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}

	service := &ChatService{DB: db}
	_, err := service.SendMessageWithIDs(
		"send-unavailable-user",
		"send-unavailable-char",
		"你好",
		"text",
		"send-unavailable-user-message",
		"send-unavailable-reply-message",
	)
	if !errors.Is(err, ErrAIServiceUnavailable) {
		t.Fatalf("expected AI unavailable error, got %v", err)
	}

	var assistantCount int64
	if err := db.Model(&database.Message{}).Where("role = ?", "assistant").Count(&assistantCount).Error; err != nil {
		t.Fatalf("count assistant replies: %v", err)
	}
	if assistantCount != 0 {
		t.Fatalf("expected no assistant template reply, got %d messages", assistantCount)
	}
}

func TestStreamMessageReusesCompletedClientMessage(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "retry-user", Nickname: "重试用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "retry-char",
		UserID:       "system_admin",
		Name:         "重试角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	session := database.Session{SessionID: "retry-session", UserID: "retry-user", CharacterID: "retry-char"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "retry-user-msg",
		SessionID:   "retry-session",
		CharacterID: "retry-char",
		UserID:      "retry-user",
		Role:        "user",
		Content:     "重复请求",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatalf("create existing user message: %v", err)
	}
	if err := db.Create(&database.Message{
		MessageID:    "retry-reply-msg",
		SessionID:    "retry-session",
		CharacterID:  "retry-char",
		UserID:       "retry-user",
		Role:         "assistant",
		Content:      "已经回复过了",
		MessageType:  "text",
		ReplyEmotion: "neutral",
	}).Error; err != nil {
		t.Fatalf("create existing reply message: %v", err)
	}

	service := &ChatService{DB: db}
	events, errs := service.StreamMessage("retry-user", "retry-char", "重复请求", "text", "retry-user-msg", "retry-reply-msg", "")

	event := <-events
	if event.Type != "meta" || event.Meta == nil {
		t.Fatalf("expected metadata for reused reply, got %+v", event)
	}
	if event.Meta.SessionID != "retry-session" || event.Meta.UserMessageID != "retry-user-msg" || event.Meta.ReplyMessageID != "retry-reply-msg" {
		t.Fatalf("unexpected reuse metadata: %+v", event.Meta)
	}
	if replay := <-events; replay.Type != "content" || replay.Content != "已经回复过了" {
		t.Fatalf("retry must replay the committed reply, got %+v", replay)
	}
	if err := <-errs; err != nil {
		t.Fatalf("expected completed retry to avoid AI generation error, got %v", err)
	}

	var count int64
	if err := db.Model(&database.Message{}).Where("user_id = ? AND character_id = ?", "retry-user", "retry-char").Count(&count).Error; err != nil {
		t.Fatalf("count retry messages: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected retry to keep two existing messages, got %d", count)
	}
}

func TestStreamMessageReportsAssistantSaveError(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.User{UserID: "save-error-user", Nickname: "保存错误用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "save-error-char",
		UserID:       "system_admin",
		Name:         "保存错误角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "conflicting-reply-id",
		SessionID:   "other-session",
		CharacterID: "save-error-char",
		UserID:      "save-error-user",
		Role:        "user",
		Content:     "占用回复ID",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatalf("create conflicting message: %v", err)
	}

	client := minimax.NewClient("fake-key")
	client.Client = &http.Client{
		Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := "data: {\"choices\":[{\"delta\":{\"content\":\"保存失败回复\"}}]}\n\ndata: [DONE]\n\n"
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	service := &ChatService{DB: db, MiniMaxClient: client}
	events, errs := service.StreamMessage("save-error-user", "save-error-char", "触发保存错误", "text", "save-error-user-msg", "conflicting-reply-id", "")

	if event := <-events; event.Type != "meta" {
		t.Fatalf("expected first event to be meta, got %+v", event)
	}
	if event := <-events; event.Type != "content" || event.Content != "保存失败回复" {
		t.Fatalf("expected streamed content before save error, got %+v", event)
	}
	if err := <-errs; err == nil {
		t.Fatal("expected assistant save error to be reported")
	}
}
