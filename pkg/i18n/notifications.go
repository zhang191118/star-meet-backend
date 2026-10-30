package i18n

// notificationMessages holds localized notification titles and preview templates.
var notificationMessages = map[string]map[string]string{
	// 亲密度升级通知标题
	"notif_affection_title": {
		"zh-Hans": "亲密度升级",
		"zh-Hant": "親密度升級",
		"en":      "Affection Level Up",
		"ko":      "친밀도 레벨 업",
		"ja":      "親密度レベルアップ",
	},
	// 亲密度升级通知内容模板: %s = 角色名, %d = 等级
	"notif_affection_preview": {
		"zh-Hans": "你与%s的亲密度达到了 Lv.%d！",
		"zh-Hant": "你與%s的親密度達到了 Lv.%d！",
		"en":      "Your affection with %s reached Lv.%d!",
		"ko":      "%s와(과)의 친밀도가 Lv.%d에 도달했습니다!",
		"ja":      "%sとの親密度が Lv.%d に到達しました！",
	},
	// 新角色降临通知标题
	"notif_new_char_title": {
		"zh-Hans": "新角色降临",
		"zh-Hant": "新角色降臨",
		"en":      "New Character",
		"ko":      "새 캐릭터",
		"ja":      "新しいキャラクター",
	},
	// 邂逅新角色通知内容模板: %s = 角色名
	"notif_encounter_preview": {
		"zh-Hans": "你邂逅了%s，快去和TA聊聊吧！",
		"zh-Hant": "你邂逅了%s，快去和TA聊聊吧！",
		"en":      "You met %s! Go chat with them now!",
		"ko":      "%s를(을) 만났어요! 지금 대화를 시작해 보세요!",
		"ja":      "%sと出会いました！今すぐ話しかけてみましょう！",
	},
	// 抽卡获得新角色通知内容模板: %s = 稀有度, %s = 角色名
	"notif_gacha_preview": {
		"zh-Hans": "恭喜获得 %s 角色%s，快去和TA聊聊吧！",
		"zh-Hant": "恭喜獲得 %s 角色%s，快去和TA聊聊吧！",
		"en":      "You got a %s character %s! Go chat with them now!",
		"ko":      "%s 캐릭터 %s를(을) 획득했습니다! 지금 대화를 시작해 보세요!",
		"ja":      "%sキャラクター %s を獲得しました！今すぐ話しかけてみましょう！",
	},
	// 欢迎通知标题
	"notif_welcome_title": {
		"zh-Hans": "欢迎来到星遇",
		"zh-Hant": "歡迎來到星遇",
		"en":      "Welcome to StarMeet",
		"ko":      "StarMeet에 오신 것을 환영합니다",
		"ja":      "StarMeetへようこそ",
	},
	// 欢迎通知内容
	"notif_welcome_preview": {
		"zh-Hans": "这里有属于你的AI伙伴，开始第一段对话吧！",
		"zh-Hant": "這裡有屬於你的AI夥伴，開始第一段對話吧！",
		"en":      "Your AI companion is here. Start your first conversation!",
		"ko":      "당신의 AI 파트너가 여기 있습니다. 첫 대화를 시작해 보세요!",
		"ja":      "あなたのAIパートナーがここにいます。最初の会話を始めましょう！",
	},
	// 默认角色名（当角色名为空时的占位符）
	"notif_default_name": {
		"zh-Hans": "TA",
		"zh-Hant": "TA",
		"en":      "them",
		"ko":      "그",
		"ja":      "相手",
	},
	"notif_proactive_title": {
		"zh-Hans": "来自%s的消息",
		"zh-Hant": "來自%s的訊息",
		"en":      "A message from %s",
		"ko":      "%s님의 메시지",
		"ja":      "%sからのメッセージ",
	},
	"notif_proactive_preview": {
		"zh-Hans": "%s想起你了，去看看TA留下的话吧。",
		"zh-Hant": "%s想起你了，去看看TA留下的話吧。",
		"en":      "%s is thinking of you. See what they left for you.",
		"ko":      "%s님이 당신을 생각하고 있어요. 남긴 말을 확인해 보세요.",
		"ja":      "%sがあなたを想っています。メッセージを確認しましょう。",
	},
	"notif_proactive_generic": {
		"zh-Hans": "你的陪伴角色有一条新消息。",
		"zh-Hant": "你的陪伴角色有一則新訊息。",
		"en":      "Your companion left you a new message.",
		"ko":      "당신의 동반자가 새 메시지를 남겼어요.",
		"ja":      "あなたのパートナーから新しいメッセージがあります。",
	},
	"notif_proactive_missed_reason": {
		"zh-Hans": "好一阵子没聊天了，来和我说说你最近过得怎么样吧。",
		"zh-Hant": "好一陣子沒聊天了，來和我說說你最近過得怎麼樣吧。",
		"en":      "It has been a while. Come tell me how you've been lately.",
		"ko":      "한동안 대화하지 못했네요. 요즘 어떻게 지냈는지 들려주세요.",
		"ja":      "しばらく話していませんね。最近どうしていたか聞かせてください。",
	},
}

// TranslateNotification returns the localized notification message for the given key and language.
// If the key is not found, it returns the key itself.
func TranslateNotification(key, lang string) string {
	translations, ok := notificationMessages[key]
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
