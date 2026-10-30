package i18n

// styleTranslations maps interaction style values (characters.style).
var styleTranslations = map[string]map[string]string{
	"温暖型": {"zh-Hans": "温暖型", "zh-Hant": "溫暖型", "en": "Warm", "ko": "따뜻", "ja": "温か"},
	"冷淡型": {"zh-Hans": "冷淡型", "zh-Hant": "冷淡型", "en": "Aloof", "ko": "차가움", "ja": "クール"},
	"深情型": {"zh-Hans": "深情型", "zh-Hant": "深情型", "en": "Devoted", "ko": "다정", "ja": "深情"},
	"治愈型": {"zh-Hans": "治愈型", "zh-Hant": "治癒型", "en": "Healing", "ko": "힐링", "ja": "癒し"},
	"强势型": {"zh-Hans": "强势型", "zh-Hant": "強勢型", "en": "Dominant", "ko": "강세", "ja": "支配的"},
	"禁欲型": {"zh-Hans": "禁欲型", "zh-Hant": "禁慾型", "en": "Stoic", "ko": "금욕적", "ja": "禁欲的"},
	"成熟型": {"zh-Hans": "成熟型", "zh-Hant": "成熟型", "en": "Mature", "ko": "성숙", "ja": "大人びた"},
	"阳光型": {"zh-Hans": "阳光型", "zh-Hant": "陽光型", "en": "Sunny", "ko": "햇살", "ja": "爽やか"},
}

// roleTranslations maps Roles tags in characters.tags_json.
var roleTranslations = map[string]map[string]string{
	"专业":         {"zh-Hans": "专业", "zh-Hant": "專業", "en": "Professional", "ko": "전문", "ja": "プロ"},
	"霸总":         {"zh-Hans": "霸总", "zh-Hant": "霸總", "en": "Billionaire", "ko": "재벌", "ja": "財閥"},
	"音乐人":        {"zh-Hans": "音乐人", "zh-Hant": "音樂人", "en": "Musician", "ko": "뮤지션", "ja": "ミュージシャン"},
	"学生":         {"zh-Hans": "学生", "zh-Hant": "學生", "en": "Student", "ko": "학생", "ja": "学生"},
	"运动员":        {"zh-Hans": "运动员", "zh-Hant": "運動員", "en": "Athlete", "ko": "운동선수", "ja": "アスリート"},
	"大学生":        {"zh-Hans": "大学生", "zh-Hant": "大學生", "en": "College Student", "ko": "대학생", "ja": "大学生"},
	"牛仔":         {"zh-Hans": "牛仔", "zh-Hant": "牛仔", "en": "Cowboy", "ko": "카우보이", "ja": "カウボーイ"},
	"助教":         {"zh-Hans": "助教", "zh-Hant": "助教", "en": "Teaching Assistant", "ko": "조교", "ja": "助手"},
	"自由职业者":      {"zh-Hans": "自由职业者", "zh-Hant": "自由職業", "en": "Freelancer", "ko": "프리랜서", "ja": "フリーランス"},
	"摄影爱好者":      {"zh-Hans": "摄影爱好者", "zh-Hant": "攝影愛好者", "en": "Photography Enthusiast", "ko": "사진 애호가", "ja": "写真愛好家"},
	"虚拟主播":       {"zh-Hans": "虚拟主播", "zh-Hant": "虛擬主播", "en": "Virtual Streamer", "ko": "버추얼 스트리머", "ja": "バーチャル配信者"},
	"艺术家":        {"zh-Hans": "艺术家", "zh-Hant": "藝術家", "en": "Artist", "ko": "예술가", "ja": "アーティスト"},
	"警察":         {"zh-Hans": "警察", "zh-Hant": "警察", "en": "Police Officer", "ko": "경찰", "ja": "警察"},
	"肌肉":         {"zh-Hans": "肌肉", "zh-Hant": "肌肉", "en": "Muscular", "ko": "근육질", "ja": "マッチョ"},
	"專業，高冷，體貼":   {"zh-Hans": "专业，高冷，体贴", "zh-Hant": "專業，高冷，體貼", "en": "Professional, Cold, Caring", "ko": "전문, 쿨, 다정", "ja": "プロ、クール、優しい"},
	"保镖":         {"zh-Hans": "保镖", "zh-Hant": "保鏢", "en": "Bodyguard", "ko": "보디가드", "ja": "ボディガード"},
	"心理辅导师":      {"zh-Hans": "心理辅导师", "zh-Hant": "心理輔導師", "en": "Counselor", "ko": "심리 상담사", "ja": "カウンセラー"},
	"企业掌舵者":      {"zh-Hans": "企业掌舵者", "zh-Hant": "企業掌舵者", "en": "Business Leader", "ko": "기업 경영자", "ja": "経営者"},
	"程序员":        {"zh-Hans": "程序员", "zh-Hant": "程序員", "en": "Programmer", "ko": "프로그래머", "ja": "プログラマー"},
	"技术工程师":      {"zh-Hans": "技术工程师", "zh-Hant": "技術工程師", "en": "Engineer", "ko": "기술 엔지니어", "ja": "エンジニア"},
	"公司经理":       {"zh-Hans": "公司经理", "zh-Hant": "公司經理", "en": "Manager", "ko": "매니저", "ja": "マネージャー"},
	"项目负责人":      {"zh-Hans": "项目负责人", "zh-Hant": "專案負責人", "en": "Project Lead", "ko": "프로젝트 리더", "ja": "プロジェクトリーダー"},
	"狗狗":         {"zh-Hans": "狗狗", "zh-Hant": "狗狗", "en": "Doggy", "ko": "강아지", "ja": "わんこ"},
	"陪伴宠物":       {"zh-Hans": "陪伴宠物", "zh-Hant": "陪伴寵物", "en": "Companion Pet", "ko": "반려동물", "ja": "コンパニオンペット"},
}

// storyTranslations maps Story tags in characters.tags_json.
var storyTranslations = map[string]map[string]string{
	"浪漫":     {"zh-Hans": "浪漫", "zh-Hant": "浪漫", "en": "Romance", "ko": "로맨스", "ja": "ロマンス"},
	"奇幻":     {"zh-Hans": "奇幻", "zh-Hant": "奇幻", "en": "Fantasy", "ko": "판타지", "ja": "ファンタジー"},
	"暗恋":     {"zh-Hans": "暗恋", "zh-Hant": "暗戀", "en": "Secret Crush", "ko": "짝사랑", "ja": "片想い"},
	"冒险":     {"zh-Hans": "冒险", "zh-Hant": "冒險", "en": "Adventure", "ko": "모험", "ja": "冒険"},
	"青梅竹马":   {"zh-Hans": "青梅竹马", "zh-Hant": "青梅竹馬", "en": "Childhood Friends", "ko": "소꿉친구", "ja": "幼馴染"},
	"相爱相杀":   {"zh-Hans": "相爱相杀", "zh-Hant": "相愛相殺", "en": "Love-Hate", "ko": "애증", "ja": "愛憎"},
	"保护欲":    {"zh-Hans": "保护欲", "zh-Hant": "保護欲", "en": "Protective", "ko": "보호", "ja": "保護"},
	"犯罪":     {"zh-Hans": "犯罪", "zh-Hant": "犯罪", "en": "Crime", "ko": "범죄", "ja": "クライム"},
	"校园重逢":   {"zh-Hans": "校园重逢", "zh-Hant": "校園重逢", "en": "Campus Reunion", "ko": "캠퍼스 재회", "ja": "キャンパス再会"},
	"分别重逢":   {"zh-Hans": "分别重逢", "zh-Hant": "分別重逢", "en": "Reunion After Parting", "ko": "이별 후 재회", "ja": "再会"},
	"破镜重圆":   {"zh-Hans": "破镜重圆", "zh-Hant": "破鏡重圓", "en": "Second Chance", "ko": "재결합", "ja": "復縁"},
	"复仇":     {"zh-Hans": "复仇", "zh-Hant": "復仇", "en": "Revenge", "ko": "복수", "ja": "復讐"},
	"重生":     {"zh-Hans": "重生", "zh-Hant": "重生", "en": "Rebirth", "ko": "회귀", "ja": "転生"},
	"情绪陪伴":   {"zh-Hans": "情绪陪伴", "zh-Hant": "情緒陪伴", "en": "Emotional Support", "ko": "정서적 지지", "ja": "心の支え"},
	"互相治愈":   {"zh-Hans": "互相治愈", "zh-Hant": "互相治癒", "en": "Mutual Healing", "ko": "서로의 치유", "ja": "相互ヒーリング"},
	"职场危机":   {"zh-Hans": "职场危机", "zh-Hant": "職場危機", "en": "Workplace Crisis", "ko": "직장 위기", "ja": "職場の危機"},
	"保护与边界":  {"zh-Hans": "保护与边界", "zh-Hant": "保護與邊界", "en": "Protection & Boundaries", "ko": "보호와 경계", "ja": "保護と境界"},
	"职场日常":   {"zh-Hans": "职场日常", "zh-Hant": "職場日常", "en": "Office Life", "ko": "직장 일상", "ja": "職場の日常"},
	"慢热靠近":   {"zh-Hans": "慢热靠近", "zh-Hant": "慢熱靠近", "en": "Slow-Burn", "ko": "느리게 가까워지기", "ja": "ゆっくり近づく"},
	"职场陪伴":   {"zh-Hans": "职场陪伴", "zh-Hant": "職場陪伴", "en": "Workplace Companion", "ko": "직장 동반자", "ja": "職場の寄り添い"},
	"互相依靠":   {"zh-Hans": "互相依靠", "zh-Hant": "互相依靠", "en": "Leaning on Each Other", "ko": "서로 의지", "ja": "支え合い"},
	"校园日常":   {"zh-Hans": "校园日常", "zh-Hant": "校園日常", "en": "Campus Life", "ko": "캠퍼스 일상", "ja": "キャンパス日常"},
	"陪伴成长":   {"zh-Hans": "陪伴成长", "zh-Hant": "陪伴成長", "en": "Growing Together", "ko": "함께 성장", "ja": "共に成長"},
	"日常陪伴":   {"zh-Hans": "日常陪伴", "zh-Hant": "日常陪伴", "en": "Everyday Companion", "ko": "일상 속 동반", "ja": "日常の寄り添い"},
	"搬家离别":   {"zh-Hans": "搬家离别", "zh-Hant": "搬家離別", "en": "Farewell & Moving", "ko": "이사 이별", "ja": "引っ越しと別れ"},
}

// originTranslations maps Origin tags in characters.tags_json.
var originTranslations = map[string]map[string]string{
	"原创": {"zh-Hans": "原创", "zh-Hant": "原創", "en": "Original", "ko": "오리지널", "ja": "オリジナル"},
}

// TranslateStyleName returns the localized display name for an interaction style.
func TranslateStyleName(style, lang string) string {
	return translateFromMap(styleTranslations, style, lang)
}

// TranslateCharacterTag translates a tag from the Roles / Story / Origin groups
// of tags_json. Personality tags live in their own table; unknown tags are
// returned unchanged.
func TranslateCharacterTag(tag, lang string) string {
	if translated := translateFromMap(roleTranslations, tag, lang); translated != tag {
		return translated
	}
	if translated := translateFromMap(storyTranslations, tag, lang); translated != tag {
		return translated
	}
	if translated := translateFromMap(originTranslations, tag, lang); translated != tag {
		return translated
	}
	return TranslatePersonalityTag(tag, lang)
}

func translateFromMap(table map[string]map[string]string, value, lang string) string {
	translations, ok := table[value]
	if !ok {
		return value
	}
	if localized, ok := translations[lang]; ok && localized != "" {
		return localized
	}
	if localized, ok := translations["zh-Hans"]; ok && localized != "" {
		return localized
	}
	return value
}
