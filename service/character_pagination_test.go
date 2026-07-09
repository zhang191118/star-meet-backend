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

func setupCharacterPaginationServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Character{}); err != nil {
		t.Fatalf("migrate character table: %v", err)
	}
	return db
}

func TestPublicCharactersBeforeIDUsesStableSort(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	now := time.Now()

	for i := 0; i < 5; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("stable-public-char-%03d", i),
			UserID:       "system_admin",
			Name:         fmt.Sprintf("稳定公开角色 %d", i),
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     1,
			ChatCount:    100,
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
	}

	service := &CharacterService{DB: db}
	characters, total, err := service.GetPublicList(request.GetPublicCharactersReq{
		BeforeID: "stable-public-char-003",
		Page:     1,
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("get public list before cursor: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected 3 remaining older characters, got %d", total)
	}
	if len(characters) != 2 {
		t.Fatalf("expected 2 characters, got %d", len(characters))
	}
	if characters[0].CharacterID != "stable-public-char-002" || characters[1].CharacterID != "stable-public-char-001" {
		t.Fatalf("expected stable created_at-desc cursor order, got %+v", characters)
	}
}

func TestMyCharactersUsesStableIDTieBreakerForSameRank(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	sameTime := time.Now()

	for i := 0; i < 3; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("stable-my-char-%03d", i),
			UserID:       "stable-my-user",
			Name:         fmt.Sprintf("稳定我的角色 %d", i),
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     2,
			ChatCount:    88,
			UpdatedAt:    sameTime,
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create my character %d: %v", i, err)
		}
	}

	service := &CharacterService{DB: db}
	characters, total, err := service.GetMyCharacters("stable-my-user", 1, 3)
	if err != nil {
		t.Fatalf("get my characters: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(characters) != 3 {
		t.Fatalf("expected 3 characters, got %d", len(characters))
	}
	if characters[0].CharacterID != "stable-my-char-002" {
		t.Fatalf("expected newest inserted tied my character first, got %#v", characters[0].CharacterID)
	}
}

func TestMyCharactersIncludesUserCreatedPrivateCharacters(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)

	for _, character := range []database.Character{
		{
			CharacterID:  "created-private-char",
			UserID:       "my-contract-user",
			Name:         "用户创建角色",
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     0,
			ChatCount:    20,
		},
		{
			CharacterID:  "encounter-private-char",
			UserID:       "my-contract-user",
			Name:         "邂逅角色",
			Gender:       "female",
			Relationship: "朋友",
			IsPublic:     2,
			ChatCount:    10,
		},
		{
			CharacterID:  "public-system-char",
			UserID:       "system_admin",
			Name:         "系统公开角色",
			Gender:       "female",
			Relationship: "朋友",
			IsPublic:     1,
			ChatCount:    30,
		},
		{
			CharacterID:  "other-private-char",
			UserID:       "other-user",
			Name:         "他人私有角色",
			Gender:       "female",
			Relationship: "朋友",
			IsPublic:     2,
			ChatCount:    40,
		},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %s: %v", character.CharacterID, err)
		}
	}

	service := &CharacterService{DB: db}
	characters, total, err := service.GetMyCharacters("my-contract-user", 1, 20)
	if err != nil {
		t.Fatalf("get my characters: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected two owned private characters, got total %d list %+v", total, characters)
	}
	if len(characters) != 2 {
		t.Fatalf("expected two owned private characters in list, got %+v", characters)
	}
	if characters[0].CharacterID != "created-private-char" || characters[1].CharacterID != "encounter-private-char" {
		t.Fatalf("expected user-created and encountered private characters only, got %+v", characters)
	}
}
