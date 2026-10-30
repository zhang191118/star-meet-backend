package i18n

// DiscoverCategory is a crushie-style trope used by the discover page.
// These IDs are stored as simplified Chinese tags and accepted as category filters.
type DiscoverCategory struct {
	ID   string
	Icon string
}

// discoverTagAliases maps canonical discover tropes to stored-tag match terms.
// Matching is substring-based against tags_json, personality_tags, and personality
// so Traditional / English leftovers still hit after a partial migration.
var discoverTagAliases = map[string][]string{
	"保护欲": {"保护欲", "保护欲强", "Protective", "保護慾", "保護欲", "护短", "守护系", "守護系", "暖男系"},
	"高冷":  {"高冷", "Cold", "冷酷", "冷漠", "冷淡疏离", "冷淡疏離", "清冷话少", "高冷独立", "冰山系", "酷哥系"},
	"体贴":  {"体贴", "体贴温柔", "温柔体贴", "Caring", "體貼", "溫柔體貼", "关怀", "關懷", "暖男系", "贴心派", "貼心派"},
	"强势":  {"强势", "强势霸道", "Dominant", "強勢", "強勢霸道", "霸道", "霸道系", "霸气派", "霸氣派"},
	"脆弱":  {"脆弱", "脆弱敏感", "Vulnerable", "没安全感", "沒安全感", "缺乏安全感", "破碎感", "玻璃心"},
	"霸总":  {"霸总", "霸總", "总裁", "總裁", "亿万富翁", "億萬富翁", "Billionaire", "CEO", "公司总裁", "企业掌舵者", "霸总系", "霸總系", "霸气总裁", "霸氣總裁"},
	"运动员": {"运动员", "運動員", "Athlete", "Athletic", "Hockey", "篮球爱好者", "运动系", "運動系", "阳光运动系", "陽光運動系"},
	"非人":  {"非人", "非人类", "非人類", "NonHuman", "Nonhuman", "Ghost", "奇幻", "幻想", "Fantasy", "狗狗", "宠物", "寵物", "陪伴宠物", "奇幻系"},
}

func init() {
	for id, aliases := range discoverTagAliases {
		categoryAliases[id] = id
		for _, alias := range aliases {
			if _, exists := categoryAliases[alias]; exists {
				continue
			}
			categoryAliases[alias] = id
		}
	}
	// The iOS client sends localized chip labels, so register every display
	// translation against the same canonical ID as well.
	for id, translations := range categoryNameTranslations {
		if _, ok := discoverTagAliases[id]; !ok {
			continue
		}
		for _, localized := range translations {
			if localized != "" {
				categoryAliases[localized] = id
			}
		}
	}
}

// DiscoverCategories returns the discover-page chips, excluding "全部".
func DiscoverCategories() []DiscoverCategory {
	return []DiscoverCategory{
		{ID: "保护欲", Icon: "shield.lefthalf.filled"},
		{ID: "高冷", Icon: "snowflake"},
		{ID: "体贴", Icon: "heart.fill"},
		{ID: "强势", Icon: "crown.fill"},
		{ID: "脆弱", Icon: "leaf.fill"},
		{ID: "霸总", Icon: "briefcase.fill"},
		{ID: "运动员", Icon: "figure.run"},
		{ID: "非人", Icon: "sparkles"},
	}
}

// DiscoverTagAliases returns LIKE-match terms for a localized or canonical category.
// An empty result means the caller should fall back to relationship filtering.
func DiscoverTagAliases(category string) []string {
	canonical := NormalizeCategory(category)
	if aliases, ok := discoverTagAliases[canonical]; ok {
		return aliases
	}
	return nil
}
