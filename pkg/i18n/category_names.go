package i18n

// categoryNameTranslations maps canonical category IDs to localized display names.
var categoryNameTranslations = map[string]map[string]string{
	"保护欲": {
		"zh-Hans": "守护系",
		"zh-Hant": "守護系",
		"en":      "Protective",
		"ko":      "보호욕",
		"ja":      "保護欲",
	},
	"高冷": {
		"zh-Hans": "冰山系",
		"zh-Hant": "冰山系",
		"en":      "Cold",
		"ko":      "쿨",
		"ja":      "クール",
	},
	"体贴": {
		"zh-Hans": "暖男系",
		"zh-Hant": "暖男系",
		"en":      "Caring",
		"ko":      "다정",
		"ja":      "優しい",
	},
	"强势": {
		"zh-Hans": "霸道系",
		"zh-Hant": "霸道系",
		"en":      "Dominant",
		"ko":      "강세",
		"ja":      "支配的",
	},
	"脆弱": {
		"zh-Hans": "破碎感",
		"zh-Hant": "破碎感",
		"en":      "Vulnerable",
		"ko":      "여린",
		"ja":      "繊細",
	},
	"霸总": {
		"zh-Hans": "霸总系",
		"zh-Hant": "霸總系",
		"en":      "Billionaire",
		"ko":      "재벌",
		"ja":      "財閥",
	},
	"运动员": {
		"zh-Hans": "运动系",
		"zh-Hant": "運動系",
		"en":      "Athlete",
		"ko":      "운동선수",
		"ja":      "アスリート",
	},
	"非人": {
		"zh-Hans": "奇幻系",
		"zh-Hant": "奇幻系",
		"en":      "Nonhuman",
		"ko":      "비인간",
		"ja":      "非人",
	},
	"伙伴": {
		"zh-Hans": "伙伴",
		"zh-Hant": "夥伴",
		"en":      "Partner",
		"ko":      "파트너",
		"ja":      "パートナー",
	},
	// 历史数据中的关系值仍是"恋人"，展示层统一显示为"伙伴"
	"恋人": {
		"zh-Hans": "伙伴",
		"zh-Hant": "夥伴",
		"en":      "Partner",
		"ko":      "파트너",
		"ja":      "パートナー",
	},
	"戀人": {
		"zh-Hans": "伙伴",
		"zh-Hant": "夥伴",
		"en":      "Partner",
		"ko":      "파트너",
		"ja":      "パートナー",
	},
	"朋友": {
		"zh-Hans": "朋友",
		"zh-Hant": "朋友",
		"en":      "Friend",
		"ko":      "친구",
		"ja":      "友達",
	},
	"导师": {
		"zh-Hans": "导师",
		"zh-Hant": "導師",
		"en":      "Mentor",
		"ko":      "멘토",
		"ja":      "メンター",
	},
	"虚拟偶像": {
		"zh-Hans": "虚拟偶像",
		"zh-Hant": "虛擬偶像",
		"en":      "Virtual Idol",
		"ko":      "가상 아이돌",
		"ja":      "バーチャルアイドル",
	},
	"萌宠": {
		"zh-Hans": "萌宠",
		"zh-Hant": "萌寵",
		"en":      "Pet",
		"ko":      "반려동물",
		"ja":      "ペット",
	},
	"二次元": {
		"zh-Hans": "二次元",
		"zh-Hant": "二次元",
		"en":      "Anime",
		"ko":      "애니메이션",
		"ja":      "二次元",
	},
	"医生": {
		"zh-Hans": "医生",
		"zh-Hant": "醫生",
		"en":      "Doctor",
		"ko":      "의사",
		"ja":      "医者",
	},
	"律师": {
		"zh-Hans": "律师",
		"zh-Hant": "律師",
		"en":      "Lawyer",
		"ko":      "변호사",
		"ja":      "弁護士",
	},
	"教师": {
		"zh-Hans": "教师",
		"zh-Hant": "教師",
		"en":      "Teacher",
		"ko":      "교사",
		"ja":      "教師",
	},
	"警察": {
		"zh-Hans": "警察",
		"zh-Hant": "警察",
		"en":      "Police",
		"ko":      "경찰",
		"ja":      "警察",
	},
	"咖啡师": {
		"zh-Hans": "咖啡师",
		"zh-Hant": "咖啡師",
		"en":      "Barista",
		"ko":      "바리스타",
		"ja":      "バリスタ",
	},
	"灵魂伴侣": {
		"zh-Hans": "灵魂伴侣",
		"zh-Hant": "靈魂伴侶",
		"en":      "Soulmate",
		"ko":      "소울메이트",
		"ja":      "ソウルメイト",
	},
	"知己": {
		"zh-Hans": "知己",
		"zh-Hant": "知己",
		"en":      "Confidant",
		"ko":      "지기",
		"ja":      "理解者",
	},
	"青梅竹马": {
		"zh-Hans": "青梅竹马",
		"zh-Hant": "青梅竹馬",
		"en":      "Childhood Friend",
		"ko":      "소꿉친구",
		"ja":      "幼馴染",
	},
	"学长/助教": {
		"zh-Hans": "学长/助教",
		"zh-Hant": "學長/助教",
		"en":      "Senpai",
		"ko":      "선배",
		"ja":      "先輩",
	},
}

// TranslateCategoryName returns the localized display name for a category ID.
func TranslateCategoryName(id, lang string) string {
	translations, ok := categoryNameTranslations[id]
	if !ok {
		return id
	}
	if localized, ok := translations[lang]; ok && localized != "" {
		return localized
	}
	if localized, ok := translations["zh-Hans"]; ok && localized != "" {
		return localized
	}
	return id
}
