package i18n

// personalityTranslations maps canonical personality tags (stored in
// characters.personality) to localized display names.
var personalityTranslations = map[string]map[string]string{
	"保护欲":   {"zh-Hans": "保护欲", "zh-Hant": "保護欲", "en": "Protective", "ko": "보호욕", "ja": "保護欲"},
	"高冷":    {"zh-Hans": "高冷", "zh-Hant": "高冷", "en": "Cold", "ko": "쿨", "ja": "クール"},
	"体贴":    {"zh-Hans": "体贴", "zh-Hant": "體貼", "en": "Caring", "ko": "다정", "ja": "気が利く"},
	"强势":    {"zh-Hans": "强势", "zh-Hant": "強勢", "en": "Dominant", "ko": "강세", "ja": "支配的"},
	"敏感":    {"zh-Hans": "敏感", "zh-Hant": "敏感", "en": "Sensitive", "ko": "예민", "ja": "繊細"},
	"脆弱":    {"zh-Hans": "脆弱", "zh-Hant": "脆弱", "en": "Vulnerable", "ko": "여린", "ja": "か弱い"},
	"非人":    {"zh-Hans": "非人", "zh-Hant": "非人", "en": "Nonhuman", "ko": "비인간", "ja": "非人間"},
	"自信":    {"zh-Hans": "自信", "zh-Hant": "自信", "en": "Confident", "ko": "자신감 넘치는", "ja": "自信家"},
	"有创造力":  {"zh-Hans": "有创造力", "zh-Hant": "有創造力", "en": "Creative", "ko": "창의적인", "ja": "創造的"},
	"占有欲":   {"zh-Hans": "占有欲", "zh-Hant": "佔有慾", "en": "Possessive", "ko": "소유욕", "ja": "独占欲"},
	"忠诚":    {"zh-Hans": "忠诚", "zh-Hant": "忠誠", "en": "Loyal", "ko": "충직한", "ja": "誠実"},
	"活力":    {"zh-Hans": "活力", "zh-Hant": "活力", "en": "Energetic", "ko": "활기찬", "ja": "元気"},
	"魅力":    {"zh-Hans": "魅力", "zh-Hant": "魅力", "en": "Charming", "ko": "매력적인", "ja": "魅力的"},
	"专一":    {"zh-Hans": "专一", "zh-Hant": "專一", "en": "Faithful", "ko": "일편단심", "ja": "一途"},
	"阳光开朗":  {"zh-Hans": "阳光开朗", "zh-Hant": "陽光開朗", "en": "Cheerful", "ko": "밝은", "ja": "明るい"},
	"治愈":    {"zh-Hans": "治愈", "zh-Hant": "治癒", "en": "Healing", "ko": "힐링", "ja": "癒し系"},
	"暖心":    {"zh-Hans": "暖心", "zh-Hant": "暖心", "en": "Sweet", "ko": "달콤한", "ja": "心和む"},
	"真诚善良":  {"zh-Hans": "真诚善良", "zh-Hant": "真誠善良", "en": "Sincere", "ko": "진솔한", "ja": "誠実"},
	"平易近人":  {"zh-Hans": "平易近人", "zh-Hant": "平易近人", "en": "Approachable", "ko": "친근한", "ja": "親しみやすい"},
	"有才华":   {"zh-Hans": "有才华", "zh-Hant": "有才華", "en": "Talented", "ko": "재능 있는", "ja": "才能がある"},
	"深情":    {"zh-Hans": "深情", "zh-Hant": "深情", "en": "Loving", "ko": "다정한", "ja": "愛情深い"},
	"忧郁":    {"zh-Hans": "忧郁", "zh-Hant": "憂鬱", "en": "Melancholic", "ko": "우울한", "ja": "憂鬱"},
	"偏执":    {"zh-Hans": "偏执", "zh-Hant": "偏執", "en": "Obsessive", "ko": "집착", "ja": "偏執"},
	"執":     {"zh-Hans": "执着", "zh-Hant": "執著", "en": "Stubborn", "ko": "고집스러운", "ja": "執着"},
	"外向":    {"zh-Hans": "外向", "zh-Hant": "外向", "en": "Outgoing", "ko": "외향적인", "ja": "外向的"},
	"肌肉":    {"zh-Hans": "肌肉", "zh-Hant": "肌肉", "en": "Muscular", "ko": "근육질", "ja": "マッチョ"},
	"壞男孩":   {"zh-Hans": "坏男孩", "zh-Hant": "壞男孩", "en": "Bad Boy", "ko": "나쁜 남자", "ja": "バッドボーイ"},
	"温柔":    {"zh-Hans": "温柔", "zh-Hant": "溫柔", "en": "Gentle", "ko": "온화한", "ja": "優しい"},
	"耐心":    {"zh-Hans": "耐心", "zh-Hant": "耐心", "en": "Patient", "ko": "참을성 있는", "ja": "忍耐強い"},
	"共情力强":  {"zh-Hans": "共情力强", "zh-Hant": "共情力強", "en": "Empathetic", "ko": "공감 능력이 좋은", "ja": "共感力が高い"},
	"细腻":    {"zh-Hans": "细腻", "zh-Hant": "細膩", "en": "Thoughtful", "ko": "섬세한", "ja": "細やか"},
	"宠溺":    {"zh-Hans": "宠溺", "zh-Hant": "寵溺", "en": "Doting", "ko": "애정 표현이 많은", "ja": "溺愛"},
	"护短":    {"zh-Hans": "护短", "zh-Hant": "護短", "en": "Fiercely Loyal", "ko": "내 편", "ja": "身内に甘い"},
	"内敛":    {"zh-Hans": "内敛", "zh-Hant": "內斂", "en": "Reserved", "ko": "차분한", "ja": "控えめ"},
	"慢热":    {"zh-Hans": "慢热", "zh-Hant": "慢熱", "en": "Slow to Warm Up", "ko": "느긋한", "ja": "人見知り"},
	"外冷内热":  {"zh-Hans": "外冷内热", "zh-Hant": "外冷內熱", "en": "Cold Front, Warm Heart", "ko": "츤데레", "ja": "ツンデレ"},
	"成熟稳重":  {"zh-Hans": "成熟稳重", "zh-Hant": "成熟穩重", "en": "Mature", "ko": "성숙한", "ja": "大人びた"},
	"温柔包容":  {"zh-Hans": "温柔包容", "zh-Hant": "溫柔包容", "en": "Nurturing", "ko": "포용력 있는", "ja": "包容力がある"},
	"有担当":   {"zh-Hans": "有担当", "zh-Hant": "有擔當", "en": "Responsible", "ko": "책임감 있는", "ja": "頼りがいがある"},
	"情绪稳定":  {"zh-Hans": "情绪稳定", "zh-Hant": "情緒穩定", "en": "Even-tempered", "ko": "감정 기복이 없는", "ja": "情緒が安定"},
	"可靠":    {"zh-Hans": "可靠", "zh-Hant": "可靠", "en": "Reliable", "ko": "믿음직한", "ja": "頼りになる"},
	"活泼纯情":  {"zh-Hans": "活泼纯情", "zh-Hant": "活潑純情", "en": "Playful & Pure", "ko": "발랄하고 순수한", "ja": "元気で純粋"},
	"粘人":    {"zh-Hans": "粘人", "zh-Hant": "黏人", "en": "Clingy", "ko": "애교 많은", "ja": "甘えん坊"},
	"少年感":   {"zh-Hans": "少年感", "zh-Hant": "少年感", "en": "Youthful", "ko": "소년미 있는", "ja": "少年らしい"},
	"活泼":    {"zh-Hans": "活泼", "zh-Hant": "活潑", "en": "Lively", "ko": "활발한", "ja": "活発"},
	"贪吃":    {"zh-Hans": "贪吃", "zh-Hant": "貪吃", "en": "Foodie", "ko": "먹보", "ja": "食いしん坊"},
	"温暖":    {"zh-Hans": "温暖", "zh-Hant": "溫暖", "en": "Warm", "ko": "따뜻한", "ja": "温かい"},
}

// TranslatePersonalityTag returns the localized display name for a personality
// tag. Unknown tags are returned unchanged so new character data never breaks.
func TranslatePersonalityTag(tag, lang string) string {
	translations, ok := personalityTranslations[tag]
	if !ok {
		return tag
	}
	if localized, ok := translations[lang]; ok && localized != "" {
		return localized
	}
	if localized, ok := translations["zh-Hans"]; ok && localized != "" {
		return localized
	}
	return tag
}
