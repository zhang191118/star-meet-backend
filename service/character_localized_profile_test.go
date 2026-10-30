package service

import (
	"testing"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
)

func TestLocalizedProfileForUsesMarketSpecificContent(t *testing.T) {
	char := database.Character{
		Name:             "原始名称",
		Narrative:        "旧故事",
		BasicSetting:     "旧介绍",
		LocalizedProfile: `{"en":{"name":"Alex Morgan","narrative":"US story","basic_setting":"US profile"},"zh-Hant":{"name":"林予安","narrative":"台北故事","basic_setting":"台灣介紹"},"zh-Hans":{"name":"林予安","narrative":"上海故事","basic_setting":"大陆介绍"}}`,
	}

	tests := []struct {
		lang, name, narrative, setting string
	}{
		{"en", "Alex Morgan", "US story", "US profile"},
		{"zh-Hant", "林予安", "台北故事", "台灣介紹"},
		{"zh-Hans", "林予安", "上海故事", "大陆介绍"},
		// The three stored variants are also the fallback for currently
		// unsupported locales, with English being the neutral default.
		{"ja", "Alex Morgan", "US story", "US profile"},
	}
	for _, tt := range tests {
		got := localizedProfileFor(char, tt.lang)
		if got.Name != tt.name || got.Narrative != tt.narrative || got.BasicSetting != tt.setting {
			t.Fatalf("localizedProfileFor(%q) = %#v, want name=%q narrative=%q setting=%q", tt.lang, got, tt.name, tt.narrative, tt.setting)
		}
	}
}

func TestLocalizedProfileForFallsBackToLegacyFields(t *testing.T) {
	char := database.Character{Name: "季夏", Narrative: "旧故事", BasicSetting: "旧介绍"}
	got := localizedProfileFor(char, "en")
	if got.Name != "Leo" || got.Narrative != "旧故事" || got.BasicSetting != "旧介绍" {
		t.Fatalf("unexpected legacy fallback: %#v", got)
	}
}

func TestLocalizedProfileForUsesLocalizedDetails(t *testing.T) {
	char := database.Character{
		Name:           "原始名称",
		Background:     "舊背景",
		Example:        "舊風格",
		FirstSituation: "舊開場",
		FirstWords:     "舊首句",
		LocalizedProfile: `{"en":{"name":"Alex Morgan","background":"US background","example":"US style","first_situation":"US opening","first_words":"US hello"},` +
			`"zh-Hant":{"name":"林予安","background":"台北背景","example":"台北風格","first_situation":"台北開場","first_words":"台北首句"},` +
			`"zh-Hans":{"name":"林予安","background":"上海背景","example":"上海风格","first_situation":"上海开场","first_words":"上海首句"}}`,
	}

	tests := []struct {
		lang, background, example, firstSituation, firstWords string
	}{
		{"en", "US background", "US style", "US opening", "US hello"},
		{"zh-Hant", "台北背景", "台北風格", "台北開場", "台北首句"},
		{"zh-Hans", "上海背景", "上海风格", "上海开场", "上海首句"},
	}
	for _, tt := range tests {
		got := localizedProfileFor(char, tt.lang)
		if got.Background != tt.background || got.Example != tt.example ||
			got.FirstSituation != tt.firstSituation || got.FirstWords != tt.firstWords {
			t.Fatalf("localizedProfileFor(%q) = %#v, want background=%q example=%q first_situation=%q first_words=%q",
				tt.lang, got, tt.background, tt.example, tt.firstSituation, tt.firstWords)
		}
	}
}

func TestLocalizedProfileForDetailsFallBackToLegacyColumns(t *testing.T) {
	// 详情字段翻译落库前，localized_profile 只有原有三个键，应回退到数据库原字段。
	char := database.Character{
		Name:             "原始名称",
		Background:       "舊背景",
		Example:          "舊風格",
		FirstSituation:   "舊開場",
		FirstWords:       "舊首句",
		LocalizedProfile: `{"en":{"name":"Alex Morgan","narrative":"US story","basic_setting":"US profile"}}`,
	}
	got := localizedProfileFor(char, "en")
	if got.Name != "Alex Morgan" || got.Narrative != "US story" {
		t.Fatalf("unexpected localized values: %#v", got)
	}
	if got.Background != "舊背景" || got.Example != "舊風格" || got.FirstSituation != "舊開場" || got.FirstWords != "舊首句" {
		t.Fatalf("expected legacy column fallback for details, got %#v", got)
	}
}

func TestPublicCharacterRespUsesLocalizedDetails(t *testing.T) {
	service := &CharacterService{}
	char := database.Character{
		CharacterID:    "detail-public-char",
		Name:           "原始名称",
		Example:        "舊風格",
		FirstSituation: "舊開場",
		FirstWords:     "舊首句",
		LocalizedProfile: `{"en":{"name":"Alex Morgan","example":"US style","first_situation":"US opening","first_words":"US hello"},` +
			`"zh-Hant":{"name":"林予安"},"zh-Hans":{"name":"林予安"}}`,
	}

	got := service.toPublicCharacterResp(char, "en")
	if got.Example != "US style" || got.FirstSituation != "US opening" || got.FirstWords != "US hello" {
		t.Fatalf("expected localized details in public resp, got %#v", got)
	}

	// 未提供详情翻译的语言回退到数据库原字段。
	fallback := service.toPublicCharacterResp(char, "zh-Hans")
	if fallback.Example != "舊風格" || fallback.FirstSituation != "舊開場" || fallback.FirstWords != "舊首句" {
		t.Fatalf("expected legacy fallback for zh-Hans, got %#v", fallback)
	}
}

func TestPublicListSelectsLocalizedProfile(t *testing.T) {
	db := setupCharacterPaginationServiceTestDB(t)
	char := database.Character{
		CharacterID:      "localized-public-char",
		UserID:           "system",
		Name:             "原始名称",
		Gender:           "male",
		IsPublic:         1,
		LocalizedProfile: `{"en":{"name":"Alex Morgan"},"zh-Hant":{"name":"林予安"},"zh-Hans":{"name":"林予安"}}`,
	}
	if err := db.Create(&char).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	service := &CharacterService{DB: db}
	characters, _, err := service.GetPublicList(request.GetPublicCharactersReq{Page: 1, PageSize: 10}, "en")
	if err != nil {
		t.Fatalf("get public list: %v", err)
	}
	if len(characters) != 1 || characters[0].Name != "Alex Morgan" {
		t.Fatalf("expected localized card name, got %#v", characters)
	}
}
