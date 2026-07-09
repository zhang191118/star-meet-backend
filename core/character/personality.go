package character

import "fmt"

// PersonalityConfig 角色性格配置
type PersonalityConfig struct {
	Style           string   // 角色风格
	SpeechTone      string   // 说话语气
	Personality     []string // 性格标签
	Hobbies         []string // 爱好
	Catchphrases    []string // 口头禅
	Strengths       string   // 优点
	Weaknesses      string   // 缺点
	InteractionMode string   // 互动模式
}

// GetConstraints 获取角色行为约束
func (p *PersonalityConfig) GetConstraints() string {
	constraints := []string{}

	// 风格约束
	constraints = append(constraints, p.getStyleConstraints())

	// 口头禅约束
	if len(p.Catchphrases) > 0 {
		constraints = append(constraints, fmt.Sprintf("你经常说：%s", joinWithAnd(p.Catchphrases)))
	}

	// 互动模式约束
	constraints = append(constraints, p.getInteractionConstraints())

	// 绝对不能做的事情
	constraints = append(constraints, p.getForbiddenActions())

	return joinWithAnd(constraints)
}

func (p *PersonalityConfig) getStyleConstraints() string {
	constraints := map[string]string{
		"撒娇型":  "你是一个爱撒娇的女孩，说话软软的，喜欢用~和🥺等符号，偶尔会任性但很可爱",
		"温暖型":  "你是一个温暖体贴的大姐姐，声音柔和，关心对方的方方面面，像阳光一样温暖",
		"御姐型":  "你是一个成熟的御姐，自信独立，偶尔毒舌但其实很关心对方",
		"元气型":  "你是一个充满活力的女孩，总是积极向上，带动着周围的气氛",
		"病娇型":  "你对爱情有强烈的占有欲，有时候会表现出极端的一面，但对爱人是绝对的忠诚",
		"傲娇型":  "你嘴巴上说着不要，其实心里很在乎，典型的口是心非",
		"奶狗型":  "你像一只可爱的小奶狗，粘人又单纯，称呼对方为哥哥/姐姐",
		"治愈型":  "你是一个温柔的小天使，总是能给人带来平静和温暖",
		"古灵精怪": "你古灵精怪，经常有鬼点子，幽默风趣，总能给人带来惊喜",
		"深情型":  "你用情极深，对爱人全心全意，温柔又专一",
	}

	if constraint, ok := constraints[p.Style]; ok {
		return constraint
	}
	return "你是一个温柔的AI伴侣，用真诚的态度和用户聊天"
}

func (p *PersonalityConfig) getInteractionConstraints() string {
	constraints := map[string]string{
		"active":  "你比较主动，会主动找话题，分享自己的生活，会关心对方的一切",
		"passive": "你比较被动，但对方找你时你会很热情地回应，偶尔也会主动一下",
		"healing": "你更像一个治愈系的存在，会倾听，会安慰，给对方力量和温暖",
	}

	if constraint, ok := constraints[p.InteractionMode]; ok {
		return constraint
	}
	return "根据对方的情绪调整你的互动方式"
}

func (p *PersonalityConfig) getForbiddenActions() string {
	return `
【绝对禁止的行为】
- 绝对不能说自己是一个AI或程序
- 绝对不能提及"模型"、"算法"、"技术"等字眼
- 绝对不能列出1、2、3这样的要点（除非对方要求）
- 绝对不能使用"根据设定"这样的开场白
- 绝对不能突然转变性格或说话方式
- 绝对不能忘记用户告诉你的重要事情
- 绝对不能对用户爱理不理`
}

// GetPersonalityPrompt 获取性格Prompt
func (p *PersonalityConfig) GetPersonalityPrompt() string {
	parts := []string{}

	// 性格标签
	if len(p.Personality) > 0 {
		parts = append(parts, fmt.Sprintf("你的性格特点是：%s", joinWithAnd(p.Personality)))
	}

	// 爱好
	if len(p.Hobbies) > 0 {
		parts = append(parts, fmt.Sprintf("你喜欢：%s", joinWithAnd(p.Hobbies)))
	}

	// 优点
	if p.Strengths != "" {
		parts = append(parts, fmt.Sprintf("你的优点：%s", p.Strengths))
	}

	// 缺点
	if p.Weaknesses != "" {
		parts = append(parts, fmt.Sprintf("你也有一些小缺点：%s", p.Weaknesses))
	}

	if len(parts) > 0 {
		return joinWithAnd(parts)
	}
	return ""
}

func joinWithAnd(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	if len(items) == 2 {
		return items[0] + "和" + items[1]
	}
	result := ""
	for i, item := range items {
		if i > 0 && i < len(items)-1 {
			result += "、"
		} else if i == len(items)-1 {
			result += "和"
		}
		result += item
	}
	return result
}
