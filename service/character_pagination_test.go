package service

import (
	"fmt"
	"strings"
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
	if err := db.AutoMigrate(&database.Character{}, &database.Block{}); err != nil {
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
			Gender:       "male",
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
	}, "en")
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
	characters, total, err := service.GetMyCharacters("stable-my-user", 1, 3, "en")
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
	characters, total, err := service.GetMyCharacters("my-contract-user", 1, 20, "en")
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

func TestPublicListOmitsLongFormStoryFields(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	character := database.Character{
		CharacterID:     "slim-public-char",
		UserID:          "system_admin",
		Name:            "瘦身公开角色",
		Gender:          "male",
		Relationship:    "恋人",
		Avatar:          "https://example.com/avatar.png",
		BannerImage:     "https://example.com/banner.png",
		IsPublic:        1,
		ChatCount:       12,
		Background:      strings.Repeat("背景", 200),
		SystemPrompt:    strings.Repeat("提示", 200),
		Narrative:       strings.Repeat("故事", 200),
		BasicSetting:    strings.Repeat("设定", 200),
		Example:         strings.Repeat("示例", 80),
		FirstSituation:  strings.Repeat("开场", 80),
		FirstWords:      "你好呀",
		Comment:         "钩子句",
		TagsJSON:        `["tag"]`,
		PersonalityTags: "温柔",
		BgImage:         "https://example.com/bg.png",
	}
	if err := db.Create(&character).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}

	service := &CharacterService{DB: db}
	characters, total, err := service.GetPublicList(request.GetPublicCharactersReq{
		Page:     1,
		PageSize: 20,
	}, "zh-Hans")
	if err != nil {
		t.Fatalf("get public list: %v", err)
	}
	if total != 1 || len(characters) != 1 {
		t.Fatalf("expected 1 public character, got total=%d list=%+v", total, characters)
	}
	item := characters[0]
	// BgImage 是发现页竖版卡片的底图，属于卡片字段，需随列表返回
	if item.CharacterID != "slim-public-char" || item.Avatar == "" || item.BannerImage == "" || item.BgImage == "" {
		t.Fatalf("expected card fields to remain, got %+v", item)
	}
	if item.Narrative != "" || item.BasicSetting != "" || item.Example != "" || item.FirstSituation != "" || item.FirstWords != "" || item.Comment != "" || item.TagsJSON != "" || item.PersonalityTags != "" {
		t.Fatalf("expected story fields to stay on detail endpoint, got %+v", item)
	}
}
