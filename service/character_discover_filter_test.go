package service

import (
	"encoding/json"
	"errors"
	"testing"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"

	"gorm.io/gorm"
)

func TestDiscoverCategoryFiltersByTagsNotRelationship(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	service := &CharacterService{DB: db}

	characters := []database.Character{
		{
			CharacterID:     "disc-protective",
			UserID:          "system",
			Name:            "保护欲角色",
			Gender:          "male",
			Relationship:    "伙伴",
			IsPublic:        1,
			ChatCount:       30,
			TagsJSON:        `{"Roles":[],"Personality":["保护欲"],"Story":[],"Origin":["原创"]}`,
			Personality:     `["保护欲"]`,
			PersonalityTags: `["保护欲"]`,
		},
		{
			CharacterID:     "disc-cold",
			UserID:          "system",
			Name:            "高冷角色",
			Gender:          "male",
			Relationship:    "伙伴",
			IsPublic:        1,
			ChatCount:       20,
			TagsJSON:        `{"Roles":[],"Personality":["高冷"],"Story":[],"Origin":["原创"]}`,
			Personality:     `["高冷"]`,
			PersonalityTags: `["高冷"]`,
		},
		{
			CharacterID:     "disc-ceo",
			UserID:          "system",
			Name:            "霸总角色",
			Gender:          "male",
			Relationship:    "伙伴",
			IsPublic:        1,
			ChatCount:       10,
			TagsJSON:        `{"Roles":["总裁"],"Personality":["强势"],"Story":[],"Origin":["原创"]}`,
			Personality:     `["强势"]`,
			PersonalityTags: `["总裁","强势"]`,
		},
		{
			CharacterID:     "disc-fantasy",
			UserID:          "system",
			Name:            "非人角色",
			Gender:          "male",
			Relationship:    "伙伴",
			IsPublic:        1,
			ChatCount:       8,
			TagsJSON:        `{"Roles":["狗狗"],"Personality":["忠诚"],"Story":["奇幻"],"Origin":["原创"]}`,
			Personality:     `["忠诚"]`,
			PersonalityTags: `["狗狗","忠诚","奇幻"]`,
		},
		{
			CharacterID:  "disc-partner-only",
			UserID:       "system",
			Name:         "仅伙伴角色",
			Gender:       "female",
			Relationship: "伙伴",
			IsPublic:     1,
			ChatCount:    5,
		},
		{
			CharacterID:  "disc-friend-only",
			UserID:       "system",
			Name:         "仅朋友女角色",
			Gender:       "female",
			Relationship: "朋友",
			IsPublic:     1,
			ChatCount:    4,
		},
		{
			CharacterID:  "disc-friend-male",
			UserID:       "system",
			Name:         "朋友男角色",
			Gender:       "male",
			Relationship: "朋友",
			IsPublic:     1,
			ChatCount:    4,
		},
		{
			CharacterID:  "disc-private",
			UserID:       "user-1",
			Name:         "私有角色",
			Gender:       "male",
			Relationship: "伙伴",
			IsPublic:     0,
			TagsJSON:     `{"Personality":["保护欲"]}`,
		},
	}
	for _, character := range characters {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create %s: %v", character.CharacterID, err)
		}
	}

	assertIDs := func(t *testing.T, category string, want ...string) {
		t.Helper()
		got, total, err := service.GetPublicList(request.GetPublicCharactersReq{
			Category: category,
			Page:     1,
			PageSize: 20,
		}, "zh-Hans")
		if err != nil {
			t.Fatalf("category %s: %v", category, err)
		}
		if int(total) != len(want) {
			t.Fatalf("category %s total=%d want %d list=%+v", category, total, len(want), got)
		}
		ids := make([]string, 0, len(got))
		for _, item := range got {
			ids = append(ids, item.CharacterID)
		}
		if len(ids) != len(want) {
			t.Fatalf("category %s ids=%v want %v", category, ids, want)
		}
		wantSet := map[string]bool{}
		for _, id := range want {
			wantSet[id] = true
		}
		for _, id := range ids {
			if !wantSet[id] {
				t.Fatalf("category %s unexpected id %s in %v", category, id, ids)
			}
		}
	}

	assertIDs(t, "保护欲", "disc-protective")
	assertIDs(t, "Protective", "disc-protective")
	assertIDs(t, "高冷", "disc-cold")
	assertIDs(t, "Cold", "disc-cold")
	assertIDs(t, "霸总", "disc-ceo")
	assertIDs(t, "Billionaire", "disc-ceo")
	assertIDs(t, "非人", "disc-fantasy")
	assertIDs(t, "Nonhuman", "disc-fantasy")
	assertIDs(t, "伙伴", "disc-protective", "disc-cold", "disc-ceo", "disc-fantasy")
	assertIDs(t, "恋人", "disc-protective", "disc-cold", "disc-ceo", "disc-fantasy")
	assertIDs(t, "朋友", "disc-friend-male")

	searched, total, err := service.SearchCharacter(request.SearchCharacterReq{
		Category: "保护欲",
		Page:     1,
		PageSize: 20,
	}, "en")
	if err != nil {
		t.Fatalf("search protective: %v", err)
	}
	if total != 1 || len(searched) != 1 || searched[0].CharacterID != "disc-protective" {
		t.Fatalf("expected search to use tag filters, got total=%d list=%+v", total, searched)
	}
}

func TestGetDiscoverCategoriesCountsPublicTags(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	service := &CharacterService{DB: db}
	if err := db.Create(&database.Character{
		CharacterID: "disc-count-protective",
		UserID:      "system",
		Name:        "计数保护欲",
		Gender:      "male",
		IsPublic:    1,
		TagsJSON:    `{"Personality":["保护欲"]}`,
	}).Error; err != nil {
		t.Fatalf("create count character: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "disc-count-private",
		UserID:      "user-1",
		Name:        "私有保护欲",
		Gender:      "male",
		IsPublic:    0,
		TagsJSON:    `{"Personality":["保护欲"]}`,
	}).Error; err != nil {
		t.Fatalf("create private character: %v", err)
	}

	categories := service.GetDiscoverCategories("en")
	if len(categories) != 8 {
		t.Fatalf("expected 8 discover categories, got %s", mustJSON(t, categories))
	}
	found := false
	for _, item := range categories {
		if item["id"] != "保护欲" {
			continue
		}
		found = true
		if item["name"] != "Protective" {
			t.Fatalf("expected English name Protective, got %#v", item)
		}
		if item["count"] != int64(1) && item["count"] != 1 {
			t.Fatalf("expected public-only count 1, got %#v", item)
		}
	}
	if !found {
		t.Fatalf("missing 保护欲 category: %s", mustJSON(t, categories))
	}
}

func TestPublicMarketplaceHidesFemaleCharacters(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	service := &CharacterService{DB: db}

	femaleID := "public-female-hidden"
	maleID := "public-male-visible"
	for _, character := range []database.Character{
		{CharacterID: femaleID, UserID: "system", Name: "公开女角色", Gender: "female", Relationship: "伙伴", IsPublic: 1, ChatCount: 99},
		{CharacterID: maleID, UserID: "system", Name: "公开男角色", Gender: "male", Relationship: "伙伴", IsPublic: 1, ChatCount: 10},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create %s: %v", character.CharacterID, err)
		}
	}

	assertOnlyMale := func(t *testing.T, source string, items []response.PublicCharacterResp, total int64) {
		t.Helper()
		if total != 1 || len(items) != 1 || items[0].CharacterID != maleID {
			t.Fatalf("%s leaked female public character: total=%d list=%+v", source, total, items)
		}
	}

	list, total, err := service.GetPublicList(request.GetPublicCharactersReq{Page: 1, PageSize: 20}, "zh-Hans")
	if err != nil {
		t.Fatalf("get public list: %v", err)
	}
	assertOnlyMale(t, "public list", list, total)

	searched, searchTotal, err := service.SearchCharacter(request.SearchCharacterReq{Page: 1, PageSize: 20}, "zh-Hans")
	if err != nil {
		t.Fatalf("search characters: %v", err)
	}
	assertOnlyMale(t, "search", searched, searchTotal)

	recommended, recTotal, err := service.GetRecommend(request.GetRecommendReq{Page: 1, PageSize: 20}, "zh-Hans")
	if err != nil {
		t.Fatalf("recommend characters: %v", err)
	}
	assertOnlyMale(t, "recommend", recommended, recTotal)

	if _, err := service.GetCharacterDetail("", femaleID, "zh-Hans"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected female public detail to be hidden, got %v", err)
	}
	if _, err := service.GetCharacterDetail("", maleID, "zh-Hans"); err != nil {
		t.Fatalf("expected male public detail, got %v", err)
	}
	if err := service.EncounterCharacter("hide-female-user", femaleID, "zh-Hans"); !errors.Is(err, ErrCharacterUnavailable) {
		t.Fatalf("expected female public encounter to fail, got %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}
