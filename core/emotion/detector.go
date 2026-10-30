package emotion

import (
	"star-meet-backend/pkg/i18n"
	"strings"
)

// 情绪类型
const (
	EmotionHappy    = "happy"    // 开心
	EmotionSad      = "sad"      // 难过
	EmotionAngry    = "angry"    // 生气
	EmotionFear     = "fear"     // 害怕/担心
	EmotionLove     = "love"     // 爱意
	EmotionLonely   = "lonely"   // 孤独
	EmotionNeutral  = "neutral"  // 中性
	EmotionExcited  = "excited"  // 兴奋
	EmotionConfused = "confused" // 困惑
	EmotionBored    = "bored"    // 无聊
)

// 回复情绪映射 - 根据用户情绪生成回复情绪
var ReplyEmotionMap = map[string]string{
	EmotionHappy:    "happy",   // 用户开心 → 回复开心
	EmotionSad:      "comfort", // 用户难过 → 安慰
	EmotionAngry:    "apology", // 用户生气 → 道歉/安抚
	EmotionLove:     "love",    // 用户示爱 → 回应爱意
	EmotionLonely:   "comfort", // 用户孤独 → 陪伴
	EmotionNeutral:  "neutral", // 用户中性 → 轻松聊天
	EmotionExcited:  "excited", // 用户兴奋 → 一起兴奋
	EmotionConfused: "help",    // 用户困惑 → 解释
	EmotionBored:    "play",    // 用户无聊 → 找乐子
}

// 情绪关键词库 - 大幅扩充
var emotionKeywords = map[string][]string{
	EmotionHappy: {
		// 中文
		"开心", "高兴", "快乐", "棒", "太棒了", "好开心", "哈哈", "笑", "爽", "爽歪歪",
		"完美", "优秀", "喜欢", "爱了", "超开心", "飞起", "嗨", "兴奋", "激动",
		"满足", "幸福", "惬意", "舒畅", "愉快", "欢快", "欣喜", "雀跃",
		// 英文
		"happy", "joy", "glad", "excited", "awesome", "great", "wonderful", "amazing",
		"perfect", "love it", "so good", "yay", "woohoo",
	},
	EmotionSad: {
		// 中文
		"难过", "伤心", "哭", "委屈", "郁闷", "心情不好", "失落", "悲伤", "沮丧",
		"郁闷", "压抑", "痛苦", "难受", "想哭", "泪", "心碎", "难过", "悲伤",
		"惆怅", "伤感", "无奈", "绝望", "沮丧", "郁闷", "低沉",
		// 英文
		"sad", "cry", "unhappy", "depressed", "heartbroken", "misserable", "upset",
		"grief", "sorrow", "melancholy", "down",
	},
	EmotionAngry: {
		// 中文
		"生气", "愤怒", "不爽", "讨厌", "气", "烦", "凭什么", "滚", "去你的",
		"可恶", "讨厌", "烦人", "恼火", "大火", "气死了", "气炸", "怒",
		"不满", "怨恨", "愤恨", "抓狂", "暴躁",
		// 英文
		"angry", "mad", "furious", "annoyed", "irritated", "upset", "hate",
		"stupid", "damn", "hell", "rage",
	},
	EmotionLove: {
		// 中文
		"爱", "喜欢", "想你", "爱你", "心动的", "害羞", "心动", "暗恋",
		"甜蜜", "温暖", "幸福", "依赖", "离不开", "最喜欢", "超爱",
		"表白", "情人节", "七夕", "么么哒", "亲亲", "抱抱", "举高高",
		// 英文
		"love", "miss you", "adore", "crush", "fond of", "fall in love",
		"kiss", "hug", "miss", "thinking of you", "love you",
	},
	EmotionLonely: {
		// 中文
		"孤独", "寂寞", "一个人", "无聊", "没人陪", "空虚", "冷清",
		"孤单", "独处", "无所事事", "闷", "闲", "发呆", "没意思",
		"想找人", "想聊天", "睡不着", "失眠", "夜深人静",
		// 英文
		"lonely", "bored", "alone", "idle", "nothing to do", "feeling empty",
		"no one", "by myself", "sleepless",
	},
	EmotionFear: {
		// 中文
		"害怕", "担心", "恐惧", "紧张", "焦虑", "不安", "忐忑", "慌",
		"怕", "不敢", "惊恐", "瑟瑟发抖", "胆战心惊", "忧心",
		"考试", "面试", "结果", "未知", "失眠", "焦虑",
		// 英文
		"scared", "worried", "anxious", "nervous", "fear", "afraid", "terrified",
		"panic", "stress", "panic",
	},
	EmotionExcited: {
		// 中文
		"超赞", "太酷了", "牛逼", "厉害", "666", "牛", "炸裂", "燃",
		"激动", "热血", "沸腾", "爆", "秀", "大神", "偶像", "崇拜",
		"成功了", "中了", "赢了", "拿到", "通过", "入选",
		// 英文
		"awesome", "cool", " epic", "legendary", "insane", "crazy", "wild",
		"won", "got it", "passed", "selected",
	},
	EmotionConfused: {
		// 中文
		"不懂", "不明白", "困惑", "迷茫", "疑惑", "疑问", "为什么",
		"怎麼", "怎么办", "怎么整", "搞不懂", "晕", "蒙", "傻",
		"不理解", "想不通", "难懂", "复杂", "头晕",
		// 英文
		"confused", "don't understand", "puzzled", "confusing", "why", "how",
		"what", "lost", "don't know",
	},
	EmotionBored: {
		// 中文
		"无聊", "没意思", "闲得慌", "无所事事", "发呆", "闲",
		"没劲", "腻", "烦", "腻歪", "厌倦", "没兴趣", "困",
		"好闲", "太闲", "没事干", "打发时间",
		// 英文
		"boring", "bored", "nothing to do", "idle", "kill time", "waste time",
	},
}

// Detect 情绪识别
func Detect(text string) string {
	if text == "" {
		return EmotionNeutral
	}

	text = i18n.NormalizeDetectionText(text)
	// Strip explicit negations before counting overlapping keyword fragments.
	for _, phrase := range []string{"不是不开心", "没有不开心", "not unhappy"} {
		text = strings.ReplaceAll(text, phrase, "")
	}
	for _, phrase := range []string{"不开心", "不快乐", "不高兴", "not happy", "unhappy"} {
		if strings.Contains(text, phrase) {
			return EmotionSad
		}
	}
	for _, phrase := range []string{"不喜欢", "不爱", "没有想你", "不想你", "不孤独", "不寂寞", "不无聊", "不生气", "不愤怒", "不害怕", "不担心", "not lonely", "not angry", "not scared", "don't like", "do not like", "don't love"} {
		text = strings.ReplaceAll(text, phrase, "")
	}

	emotionScores := map[string]int{
		EmotionHappy:    0,
		EmotionSad:      0,
		EmotionAngry:    0,
		EmotionLove:     0,
		EmotionLonely:   0,
		EmotionFear:     0,
		EmotionExcited:  0,
		EmotionConfused: 0,
		EmotionBored:    0,
	}

	for emotion, keywords := range emotionKeywords {
		for _, kw := range keywords {
			if contains(text, toLower(kw)) {
				emotionScores[emotion]++
			}
		}
	}

	// 找出最高分的情绪
	maxScore := 0
	maxEmotion := EmotionNeutral

	for _, emotion := range []string{EmotionSad, EmotionFear, EmotionAngry, EmotionLonely, EmotionConfused, EmotionBored, EmotionLove, EmotionHappy, EmotionExcited} {
		score := emotionScores[emotion]
		if score > maxScore {
			maxScore = score
			maxEmotion = emotion
		}
	}

	// 至少需要匹配1个关键词
	if maxScore == 0 {
		return EmotionNeutral
	}

	return maxEmotion
}

// GetReplyEmotion 获取回复情绪
func GetReplyEmotion(userEmotion string) string {
	if v, ok := ReplyEmotionMap[userEmotion]; ok {
		return v
	}
	return EmotionNeutral
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
