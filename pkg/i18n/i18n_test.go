package i18n

import "testing"

func TestTranslateCharacterName(t *testing.T) {
	tests := []struct {
		name string
		lang string
		want string
	}{
		{"季夏", "en", "Leo"},
		{"季夏", "zh-Hans", "季夏"},
		{"季夏", "zh-Hant", "季夏"},
		{"季夏", "ko", "계하"},
		{"季夏", "ja", "リク"},
		{"小软", "en", "Mia"},
		{"红袖", "en", "Scarlet"},
		{"阿黄", "en", "Buddy"},
		{"星宝", "en", "Starla"},
		{"unknown", "en", "unknown"},
		{"", "en", ""},
	}
	for _, tt := range tests {
		got := TranslateCharacterName(tt.name, tt.lang)
		if got != tt.want {
			t.Errorf("TranslateCharacterName(%q, %q) = %q, want %q", tt.name, tt.lang, got, tt.want)
		}
	}
}

func TestNormalizeCategory(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"恋人", "伙伴"},
		{"Lover", "伙伴"},
		{"戀人", "伙伴"},
		{"朋友", "朋友"},
		{"Friend", "朋友"},
		{"导师", "导师"},
		{"Mentor", "导师"},
		{"萌宠", "萌宠"},
		{"Pet", "萌宠"},
		{"二次元", "二次元"},
		{"Anime", "二次元"},
		{"Protective", "保护欲"},
		{"保护欲", "保护欲"},
		{"保護慾", "保护欲"},
		{"守護系", "保护欲"},
		{"Cold", "高冷"},
		{"冰山系", "高冷"},
		{"Billionaire", "霸总"},
		{"霸总", "霸总"},
		{"霸總系", "霸总"},
		{"Nonhuman", "非人"},
		{"Athlete", "运动员"},
		{"custom_relationship", "custom_relationship"},
	}
	for _, tt := range tests {
		got := NormalizeCategory(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeCategory(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTranslateCategoryName(t *testing.T) {
	tests := []struct {
		id   string
		lang string
		want string
	}{
		{"伙伴", "en", "Partner"},
		{"伙伴", "zh-Hans", "伙伴"},
		{"伙伴", "zh-Hant", "夥伴"},
		{"朋友", "en", "Friend"},
		{"萌宠", "en", "Pet"},
		{"灵魂伴侣", "en", "Soulmate"},
		{"保护欲", "en", "Protective"},
		{"保护欲", "zh-Hans", "守护系"},
		{"保护欲", "zh-Hant", "守護系"},
		{"霸总", "en", "Billionaire"},
		{"非人", "en", "Nonhuman"},
		{"unknown", "en", "unknown"},
	}
	for _, tt := range tests {
		got := TranslateCategoryName(tt.id, tt.lang)
		if got != tt.want {
			t.Errorf("TranslateCategoryName(%q, %q) = %q, want %q", tt.id, tt.lang, got, tt.want)
		}
	}
}

func TestTranslatePersonalityTag(t *testing.T) {
	tests := []struct {
		tag  string
		lang string
		want string
	}{
		{"阳光开朗", "en", "Cheerful"},
		{"阳光开朗", "zh-Hant", "陽光開朗"},
		{"深情", "en", "Loving"},
		{"忧郁", "ja", "憂鬱"},
		{"外冷内热", "ko", "츤데레"},
		{"壞男孩", "zh-Hans", "坏男孩"},
		{"執", "zh-Hant", "執著"},
		{"保护欲", "en", "Protective"},
		{"没有这个词", "en", "没有这个词"},
		{"", "en", ""},
	}
	for _, tt := range tests {
		got := TranslatePersonalityTag(tt.tag, tt.lang)
		if got != tt.want {
			t.Errorf("TranslatePersonalityTag(%q, %q) = %q, want %q", tt.tag, tt.lang, got, tt.want)
		}
	}
}

func TestDiscoverTagAliases(t *testing.T) {
	if got := DiscoverTagAliases("Protective"); len(got) == 0 {
		t.Fatalf("expected Protective to map to discover tag aliases")
	}
	if got := DiscoverTagAliases("保护欲"); got[0] != "保护欲" {
		t.Fatalf("expected canonical 保护欲 aliases, got %#v", got)
	}
	if got := DiscoverTagAliases("恋人"); len(got) != 0 {
		t.Fatalf("expected legacy 恋人 to fall back to relationship filter, got %#v", got)
	}
	if got := DiscoverTagAliases("Pet"); len(got) != 0 {
		t.Fatalf("expected Pet to stay a relationship alias, got %#v", got)
	}
	ids := DiscoverCategories()
	if len(ids) != 8 {
		t.Fatalf("expected 8 discover tropes, got %#v", ids)
	}
}
