package service

import (
	"fmt"
	"testing"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAccessControlTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.Session{}, &database.Message{}, &database.Memory{}); err != nil {
		t.Fatalf("migrate tables: %v", err)
	}

	return db
}

func TestDeleteSessionRejectsCrossUserAccess(t *testing.T) {
	db := setupAccessControlTestDB(t)
	service := &ChatService{DB: db}

	if err := db.Create(&database.User{UserID: "owner", Email: "owner@example.com"}).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := db.Create(&database.User{UserID: "intruder", Email: "intruder@example.com"}).Error; err != nil {
		t.Fatalf("create intruder: %v", err)
	}

	session := database.Session{SessionID: "session-1", UserID: "owner", CharacterID: "char-1"}
	message := database.Message{MessageID: "msg-1", SessionID: "session-1", CharacterID: "char-1", UserID: "owner", Role: "user", Content: "hi"}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	if err := service.DeleteSession("session-1", "intruder"); err == nil {
		t.Fatal("expected deleting another user's session to fail")
	}

	var sessionCount int64
	if err := db.Model(&database.Session{}).Where("session_id = ?", "session-1").Count(&sessionCount).Error; err != nil {
		t.Fatalf("count session: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("expected session to remain, count=%d", sessionCount)
	}
}

func TestGetMessageByIDRejectsCrossUserAccess(t *testing.T) {
	db := setupAccessControlTestDB(t)
	service := &ChatService{DB: db}

	message := database.Message{MessageID: "msg-2", SessionID: "session-2", CharacterID: "char-1", UserID: "owner", Role: "assistant", Content: "hello"}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	if _, err := service.GetMessageByID("msg-2", "intruder"); err == nil {
		t.Fatal("expected reading another user's message status to fail")
	}
}

func TestUpdateMemoryRejectsCrossUserAccess(t *testing.T) {
	db := setupAccessControlTestDB(t)
	service := &MemoryService{DB: db}

	memory := database.Memory{MemoryID: "memory-1", UserID: "owner", CharacterID: "char-1", Content: "old"}
	if err := db.Create(&memory).Error; err != nil {
		t.Fatalf("create memory: %v", err)
	}

	if err := service.UpdateMemory("memory-1", "intruder", "new"); err == nil {
		t.Fatal("expected updating another user's memory to fail")
	}

	var stored database.Memory
	if err := db.Where("memory_id = ?", "memory-1").First(&stored).Error; err != nil {
		t.Fatalf("reload memory: %v", err)
	}
	if stored.Content != "old" {
		t.Fatalf("expected memory content unchanged, got %q", stored.Content)
	}
}

func TestDeleteMemoryRejectsCrossUserAccess(t *testing.T) {
	db := setupAccessControlTestDB(t)
	service := &MemoryService{DB: db}

	memory := database.Memory{MemoryID: "memory-2", UserID: "owner", CharacterID: "char-1", Content: "keep"}
	if err := db.Create(&memory).Error; err != nil {
		t.Fatalf("create memory: %v", err)
	}

	if err := service.DeleteMemory("memory-2", "intruder"); err == nil {
		t.Fatal("expected deleting another user's memory to fail")
	}

	var count int64
	if err := db.Model(&database.Memory{}).Where("memory_id = ?", "memory-2").Count(&count).Error; err != nil {
		t.Fatalf("count memory: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected memory to remain, count=%d", count)
	}
}
