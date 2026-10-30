package i18n

// categoryAliases maps localized category names (from any language) to canonical
// discover tropes or legacy relationship values. Discover chips now filter by
// tags; old relationship aliases stay so previous clients do not hard-break.
var categoryAliases = map[string]string{
	// "全部" / "All" - handled separately (no filter)

	// 伙伴 (partner/lover) - the main romantic partner category
	"伙伴":   "伙伴",
	"恋人":   "伙伴", // iOS "恋人" maps to backend "伙伴"
	"Lover":  "伙伴",
	"Partner": "伙伴",
	"戀人":   "伙伴",

	// 朋友
	"朋友":    "朋友",
	"Friend": "朋友",

	// 导师
	"导师":    "导师",
	"導師":    "导师",
	"Mentor": "导师",

	// 虚拟偶像
	"虚拟偶像":       "虚拟偶像",
	"虛擬偶像":       "虚拟偶像",
	"Virtual Idol": "虚拟偶像",
	"Idol":         "虚拟偶像",

	// 萌宠
	"萌宠":  "萌宠",
	"萌寵":  "萌宠",
	"Pet":  "萌宠",

	// 二次元
	"二次元":  "二次元",
	"Anime": "二次元",

	// 灵魂伴侣
	"灵魂伴侣":  "灵魂伴侣",
	"靈魂伴侶":  "灵魂伴侣",
	"Soulmate": "灵魂伴侣",

	// 知己
	"知己":    "知己",
	"Bosom":  "知己",
	"Confidant": "知己",

	// 青梅竹马
	"青梅竹马":  "青梅竹马",
	"青梅竹馬":  "青梅竹马",
	"Childhood": "青梅竹马",

	// 医生
	"医生":    "医生",
	"醫生":    "医生",
	"Doctor": "医生",

	// 律师
	"律师":    "律师",
	"律師":    "律师",
	"Lawyer": "律师",

	// 教师
	"教师":    "教师",
	"教師":    "教师",
	"Teacher": "教师",

	// 警察
	"警察":    "警察",
	"Police": "警察",

	// 咖啡师
	"咖啡师":    "咖啡师",
	"咖啡師":    "咖啡师",
	"Barista": "咖啡师",

	// 学长/助教
	"学长/助教":  "学长/助教",
	"學長/助教":  "学长/助教",
	"Senpai":  "学长/助教",
}

// NormalizeCategory maps a localized category name to a canonical discover trope
// or legacy relationship value. Unmapped input is returned as-is.
func NormalizeCategory(category string) string {
	if category == "" {
		return ""
	}

	// Check direct alias
	if canonical, ok := categoryAliases[category]; ok {
		return canonical
	}

	// Return as-is for unmapped values (custom relationships)
	return category
}
