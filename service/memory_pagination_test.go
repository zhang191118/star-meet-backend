package service

import (
	"fmt"
	"testing"
	"time"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMemoryPaginationServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Memory{}); err != nil {
		t.Fatalf("migrate memory table: %v", err)
	}
	return db
}

func TestMemoryListUsesStableIDTieBreakerForSameTimestamp(t *testing.T) {
	db := setupMemoryPaginationServiceTestDB(t)
	sameTime := time.Now()

	for i := 0; i < 3; i++ {
		memory := database.Memory{
			MemoryID:    fmt.Sprintf("memory-stable-%03d", i),
			UserID:      "memory-stable-user",
			CharacterID: "memory-stable-char",
			Content:     fmt.Sprintf("stable memory %d", i),
			MemoryType:  "fact",
			Source:      "manual",
			CreatedAt:   sameTime,
		}
		if err := db.Create(&memory).Error; err != nil {
			t.Fatalf("create memory %d: %v", i, err)
		}
	}

	service := &MemoryService{DB: db}
	memories, total, err := service.GetMemoryList(request.GetMemoryListReq{
		UserID:      "memory-stable-user",
		CharacterID: "memory-stable-char",
		Page:        1,
		PageSize:    3,
	})
	if err != nil {
		t.Fatalf("get memory list: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(memories) != 3 {
		t.Fatalf("expected 3 memories, got %d", len(memories))
	}
	if memories[0].MemoryID != "memory-stable-002" {
		t.Fatalf("expected newest inserted tied memory first, got %#v", memories[0].MemoryID)
	}
}
