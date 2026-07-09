package service

import (
	"fmt"
	"testing"
	"time"

	"star-meet-backend/model/database"
)

func TestChatHistoryPageSizeIsClamped(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	now := time.Now()

	if err := db.Create(&database.User{UserID: "page-user", Nickname: "分页用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "page-char",
		UserID:       "system_admin",
		Name:         "分页角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{SessionID: "page-session", UserID: "page-user", CharacterID: "page-char"}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 101; i++ {
		message := database.Message{
			MessageID:   fmt.Sprintf("page-msg-%03d", i),
			SessionID:   "page-session",
			CharacterID: "page-char",
			UserID:      "page-user",
			Role:        "user",
			Content:     fmt.Sprintf("message %d", i),
			MessageType: "text",
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}

	service := &ChatService{DB: db}
	messages, total, err := service.GetHistory("page-user", "page-char", 1, 1000)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if total != 101 {
		t.Fatalf("expected total 101, got %d", total)
	}
	if len(messages) != 100 {
		t.Fatalf("expected page size to clamp to 100, got %d", len(messages))
	}
}

func TestChatHistorySupportsBeforeIDCursor(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	now := time.Now()

	if err := db.Create(&database.User{UserID: "cursor-user", Nickname: "游标用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "cursor-char",
		UserID:       "system_admin",
		Name:         "游标角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{SessionID: "cursor-session", UserID: "cursor-user", CharacterID: "cursor-char"}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 5; i++ {
		message := database.Message{
			MessageID:   fmt.Sprintf("cursor-msg-%03d", i),
			SessionID:   "cursor-session",
			CharacterID: "cursor-char",
			UserID:      "cursor-user",
			Role:        "user",
			Content:     fmt.Sprintf("cursor message %d", i),
			MessageType: "text",
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}

	service := &ChatService{DB: db}
	messages, total, err := service.GetHistoryBefore("cursor-user", "cursor-char", "cursor-msg-003", 1, 2)
	if err != nil {
		t.Fatalf("get history before cursor: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total remaining 3, got %d", total)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 cursor messages, got %d", len(messages))
	}
	if messages[0].MessageID != "cursor-msg-002" || messages[1].MessageID != "cursor-msg-001" {
		t.Fatalf("expected newest older messages, got %#v", []string{messages[0].MessageID, messages[1].MessageID})
	}
}

func TestChatHistoryUsesStableIDTieBreakerForSameTimestamp(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	sameTime := time.Now()

	if err := db.Create(&database.User{UserID: "history-stable-user", Nickname: "历史稳定用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "history-stable-char",
		UserID:       "system_admin",
		Name:         "历史稳定角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{SessionID: "history-stable-session", UserID: "history-stable-user", CharacterID: "history-stable-char"}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	for i := 0; i < 3; i++ {
		message := database.Message{
			MessageID:   fmt.Sprintf("history-stable-msg-%03d", i),
			SessionID:   "history-stable-session",
			CharacterID: "history-stable-char",
			UserID:      "history-stable-user",
			Role:        "user",
			Content:     fmt.Sprintf("stable message %d", i),
			MessageType: "text",
			CreatedAt:   sameTime,
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}

	service := &ChatService{DB: db}
	messages, total, err := service.GetHistory("history-stable-user", "history-stable-char", 1, 3)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if messages[0].MessageID != "history-stable-msg-002" {
		t.Fatalf("expected newest inserted tied message first, got %#v", messages[0].MessageID)
	}
}

func TestChatSessionsPageSizeIsClamped(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	now := time.Now()

	if err := db.Create(&database.User{UserID: "session-page-user", Nickname: "会话分页用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	for i := 0; i < 101; i++ {
		characterID := fmt.Sprintf("session-page-char-%03d", i)
		if err := db.Create(&database.Character{
			CharacterID:  characterID,
			UserID:       "system_admin",
			Name:         fmt.Sprintf("会话角色 %d", i),
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     1,
		}).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
		lastAt := now.Add(time.Duration(i) * time.Second)
		if err := db.Create(&database.Session{
			SessionID:     fmt.Sprintf("session-page-%03d", i),
			UserID:        "session-page-user",
			CharacterID:   characterID,
			LastMessage:   "hello",
			LastMessageAt: &lastAt,
		}).Error; err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	service := &ChatService{DB: db}
	sessions, total, err := service.GetSessions("session-page-user", 1, 1000)
	if err != nil {
		t.Fatalf("get sessions: %v", err)
	}
	if total != 101 {
		t.Fatalf("expected total 101, got %d", total)
	}
	if len(sessions) != 100 {
		t.Fatalf("expected page size to clamp to 100, got %d", len(sessions))
	}
}

func TestChatSessionsUseStableIDTieBreakerForSameRank(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	lastAt := time.Now()

	if err := db.Create(&database.User{UserID: "session-stable-user", Nickname: "会话稳定用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	for i := 0; i < 3; i++ {
		characterID := fmt.Sprintf("session-stable-char-%03d", i)
		if err := db.Create(&database.Character{
			CharacterID:  characterID,
			UserID:       "system_admin",
			Name:         fmt.Sprintf("会话稳定角色 %d", i),
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     1,
		}).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
		if err := db.Create(&database.Session{
			SessionID:     fmt.Sprintf("session-stable-%03d", i),
			UserID:        "session-stable-user",
			CharacterID:   characterID,
			LastMessage:   "hello",
			LastMessageAt: &lastAt,
			IsPinned:      true,
		}).Error; err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}

	service := &ChatService{DB: db}
	sessions, total, err := service.GetSessions("session-stable-user", 1, 3)
	if err != nil {
		t.Fatalf("get sessions: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != "session-stable-002" {
		t.Fatalf("expected newest inserted tied session first, got %#v", sessions[0].SessionID)
	}
}
