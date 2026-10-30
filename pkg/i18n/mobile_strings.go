package i18n

// mobileStrings holds localized UI strings for the mobile home/dashboard API.
var mobileStrings = map[string]map[string]string{
	// 推荐区域标题
	"mobile_for_you_title": {
		"zh-Hans": "为你推荐",
		"zh-Hant": "為你推薦",
		"en":      "For You",
		"ko":      "추천",
		"ja":      "おすすめ",
	},
	// 推荐理由 - 近期热门
	"mobile_reason_hot": {
		"zh-Hans": "近期很受欢迎",
		"zh-Hant": "近期很受歡迎",
		"en":      "Popular right now",
		"ko":      "지금 인기 있는",
		"ja":      "今人気です",
	},
	// 推荐理由 - 新角色
	"mobile_reason_new": {
		"zh-Hans": "适合开启第一次聊天",
		"zh-Hant": "適合開啟第一次聊天",
		"en":      "Great for a first chat",
		"ko":      "첫 대화에 좋아요",
		"ja":      "初めての会話にぴったり",
	},
	// Banner - 今日陪伴
	"mobile_banner_daily_title": {
		"zh-Hans": "今日陪伴已开启",
		"zh-Hant": "今日陪伴已開啟",
		"en":      "Daily companion ready",
		"ko":      "오늘의 동반자가 준비되었습니다",
		"ja":      "今日のパートナーが準備できました",
	},
	// Banner - 副标题
	"mobile_banner_daily_subtitle": {
		"zh-Hans": "和喜欢的角色聊几句，亲密度会慢慢升温。",
		"zh-Hant": "和喜歡的角色聊幾句，親密度會慢慢升溫。",
		"en":      "Chat with your favorite character and grow closer.",
		"ko":      "좋아하는 캐릭터와 대화하며 친밀도를 높여보세요.",
		"ja":      "お気に入りのキャラクターと話して、親密度を深めましょう。",
	},
}

// TranslateMobileString returns the localized mobile UI string for the given key and language.
func TranslateMobileString(key, lang string) string {
	translations, ok := mobileStrings[key]
	if !ok {
		return key
	}
	if localized, ok := translations[lang]; ok && localized != "" {
		return localized
	}
	if localized, ok := translations["zh-Hans"]; ok && localized != "" {
		return localized
	}
	return key
}
