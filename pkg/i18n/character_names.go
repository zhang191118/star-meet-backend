package i18n

// characterNameTranslations maps Chinese character names to their localized equivalents.
// Keys are the canonical (Chinese) names stored in the database.
// Supported languages: zh-Hans, zh-Hant, en, ko, ja.
var characterNameTranslations = map[string]map[string]string{
	// ========== 女性角色 ==========
	"小软": {
		"zh-Hans": "小软",
		"zh-Hant": "小軟",
		"en":      "Mia",
		"ko":      "미아",
		"ja":      "ミア",
	},
	"红袖": {
		"zh-Hans": "红袖",
		"zh-Hant": "紅袖",
		"en":      "Scarlet",
		"ko":      "스칼렛",
		"ja":      "スカーレット",
	},
	"御薇": {
		"zh-Hans": "御薇",
		"zh-Hant": "御薇",
		"en":      "Vivian",
		"ko":      "비비안",
		"ja":      "ビビアン",
	},
	"娇娇": {
		"zh-Hans": "娇娇",
		"zh-Hant": "嬌嬌",
		"en":      "Jiaojiao",
		"ko":      "쭈쭈",
		"ja":      "ジョジョ",
	},
	"甜甜": {
		"zh-Hans": "甜甜",
		"zh-Hant": "甜甜",
		"en":      "Sunny",
		"ko":      "티티",
		"ja":      "ティティ",
	},
	"知知": {
		"zh-Hans": "知知",
		"zh-Hant": "知知",
		"en":      "Iris",
		"ko":      "지지",
		"ja":      "チチ",
	},
	"念念": {
		"zh-Hans": "念念",
		"zh-Hant": "念念",
		"en":      "Mio",
		"ko":      "념념",
		"ja":      "ネネ",
	},

	// ========== 男性角色 ==========
	"季夏": {
		"zh-Hans": "季夏",
		"zh-Hant": "季夏",
		"en":      "Leo",
		"ko":      "계하",
		"ja":      "リク",
	},
	"江寒": {
		"zh-Hans": "江寒",
		"zh-Hant": "江寒",
		"en":      "Jiang",
		"ko":      "강한",
		"ja":      "コウ",
	},
	"旭阳": {
		"zh-Hans": "旭阳",
		"zh-Hant": "旭陽",
		"en":      "Asahi",
		"ko":      "욱양",
		"ja":      "アサヒ",
	},
	"沉渊": {
		"zh-Hans": "沉渊",
		"zh-Hant": "沉淵",
		"en":      "Arthur",
		"ko":      "침연",
		"ja":      "シンエン",
	},
	"寒川": {
		"zh-Hans": "寒川",
		"zh-Hant": "寒川",
		"en":      "Kazuki",
		"ko":      "한천",
		"ja":      "カワカワ",
	},
	"烈风": {
		"zh-Hans": "烈风",
		"zh-Hant": "烈風",
		"en":      "Rex",
		"ko":      "렬풍",
		"ja":      "レツプウ",
	},
	"温言": {
		"zh-Hans": "温言",
		"zh-Hant": "溫言",
		"en":      "Len",
		"ko":      "온언",
		"ja":      "オンゲン",
	},

	// ========== 特殊角色 ==========
	"阿黄": {
		"zh-Hans": "阿黄",
		"zh-Hant": "阿黃",
		"en":      "Buddy",
		"ko":      "아황",
		"ja":      "アコ",
	},
	"星宝": {
		"zh-Hans": "星宝",
		"zh-Hant": "星寶",
		"en":      "Starla",
		"ko":      "별보",
		"ja":      "ホシ",
	},
}

// TranslateCharacterName returns the localized character name for the given language.
// If no translation exists for the name or language, the original name is returned unchanged.
func TranslateCharacterName(name, lang string) string {
	if name == "" {
		return name
	}

	translations, ok := characterNameTranslations[name]
	if !ok {
		return name
	}

	if localized, ok := translations[lang]; ok && localized != "" {
		return localized
	}

	// Fallback: zh-Hans -> original
	if localized, ok := translations["zh-Hans"]; ok && localized != "" {
		return localized
	}

	return name
}

// NormalizeLang maps various language code formats to the canonical keys used in translation tables.
func NormalizeLang(lang string) string {
	switch lang {
	case "", "en", "EN":
		return "en"
	case "zh-Hans", "zh-hans", "zh-CN", "zh-cn", "zh":
		return "zh-Hans"
	case "zh-Hant", "zh-hant", "zh-TW", "zh-tw", "zh-HK", "zh-hk":
		return "zh-Hant"
	case "ko", "KO":
		return "ko"
	case "ja", "JA":
		return "ja"
	default:
		return "en"
	}
}
