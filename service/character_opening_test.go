package service

import (
	"testing"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
)

func TestPublicCharactersKeepCardIdentityWithoutOpeningFields(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	character := database.Character{
		CharacterID:    "scene-public-char",
		UserID:         "system_admin",
		Name:           "场景角色",
		Gender:         "male",
		Age:            24,
		Occupation:     "咖啡师",
		Personality:    `["温柔","慢热"]`,
		Relationship:   "伙伴",
		Narrative:      "这是角色梗概。",
		FirstSituation: "雨夜的咖啡店里，你推门而入。",
		FirstWords:     "你终于来了。",
		IsPublic:       1,
	}
	if err := db.Create(&character).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}

	service := &CharacterService{DB: db}
	characters, _, err := service.GetPublicList(request.GetPublicCharactersReq{Page: 1, PageSize: 10}, "zh-Hans")
	if err != nil {
		t.Fatalf("get public characters: %v", err)
	}
	if len(characters) != 1 {
		t.Fatalf("expected one public character, got %d", len(characters))
	}
	got := characters[0]
	if got.Age != 24 || got.Occupation != "咖啡师" {
		t.Fatalf("expected scene card identity fields, got %+v", got)
	}
	if got.FirstSituation != "" || got.FirstWords != "" {
		t.Fatalf("expected opening fields to stay out of the public list, got %+v", got)
	}
}

func TestEncounteredCharacterSeedsItsOpeningSceneOnFirstChat(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	userID := "encounter-opening-user"
	publicID := "encounter-opening-public"
	if err := db.Create(&database.User{UserID: userID, Nickname: "邂逅用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:    publicID,
		UserID:         "system_admin",
		Name:           "邂逅角色",
		Gender:         "male",
		Relationship:   "伙伴",
		FirstSituation: "书店打烊前，你在旧书里发现了她留下的纸条。",
		FirstWords:     "原来捡到纸条的人是你。",
		IsPublic:       1,
	}).Error; err != nil {
		t.Fatalf("create public character: %v", err)
	}

	characterService := &CharacterService{DB: db}
	if err := characterService.EncounterCharacter(userID, publicID, "zh-Hans"); err != nil {
		t.Fatalf("encounter character: %v", err)
	}

	privateID := publicID + "_" + userID[:8]
	chatService := &ChatService{DB: db}
	messages, total, err := chatService.GetHistory(userID, privateID, 1, 20)
	if err != nil {
		t.Fatalf("get first private chat history: %v", err)
	}
	if total != 2 || len(messages) != 2 {
		t.Fatalf("expected scene and opening words after encounter, total=%d messages=%d", total, len(messages))
	}
	if messages[0].Content != "原来捡到纸条的人是你。" || messages[1].MessageType != "narrative" {
		t.Fatalf("unexpected encountered opening timeline: %+v", messages)
	}
}

func TestPublicListOmitsHeavyStoryFields(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	character := database.Character{
		CharacterID:    "slim-public-char",
		UserID:         "system_admin",
		Name:           "轻量角色",
		Gender:         "male",
		Age:            23,
		Occupation:     "插画师",
		Personality:    "[\"温柔\"]",
		Relationship:   "伙伴",
		Narrative:      "这是不应该出现在发现页列表里的长故事。",
		BasicSetting:   "这是不应该出现在发现页列表里的人设履历。",
		Example:        "这是不应该出现在发现页列表里的说话风格。",
		SystemPrompt:   "这是不应该被列表查询读出的系统提示词。",
		FirstSituation: "黄昏的画室里，你推门进来。",
		FirstWords:     "你来得刚好。",
		IsPublic:       1,
	}
	if err := db.Create(&character).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}

	service := &CharacterService{DB: db}
	characters, _, err := service.GetPublicList(request.GetPublicCharactersReq{Page: 1, PageSize: 10}, "zh-Hans")
	if err != nil {
		t.Fatalf("get public characters: %v", err)
	}
	if len(characters) != 1 {
		t.Fatalf("expected one public character, got %d", len(characters))
	}
	got := characters[0]
	if got.Narrative != "" || got.BasicSetting != "" || got.Example != "" {
		t.Fatalf("expected heavy story fields to be omitted from public list, got %+v", got)
	}
	if got.FirstSituation != "" || got.FirstWords != "" {
		t.Fatalf("expected opening fields to stay out of the public list, got %+v", got)
	}
	if got.Occupation != "插画师" || got.Age != 23 {
		t.Fatalf("expected card identity fields to remain in public list, got %+v", got)
	}
}
