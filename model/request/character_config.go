package request

// CharacterConfigReq 角色配置请求
type CharacterConfigReq struct {
	SpeechTone      string   `json:"speech_tone" form:"speech_tone"`             // 说话语气
	Personality     []string `json:"personality" form:"personality"`             // 性格标签
	Hobbies         []string `json:"hobbies" form:"hobbies"`                     // 爱好
	Catchphrases    []string `json:"catchphrases" form:"catchphrases"`            // 口头禅
	Strengths       string   `json:"strengths" form:"strengths"`                  // 优点
	Weaknesses      string   `json:"weaknesses" form:"weaknesses"`                 // 缺点
	InteractionMode string   `json:"interaction_mode" form:"interaction_mode"`     // 互动模式
	Appearance      string   `json:"appearance" form:"appearance"`                  // 外貌描述
	Birthday        string   `json:"birthday" form:"birthday"`                   // 生日
	Constellation   string   `json:"constellation" form:"constellation"`         // 星座
	PersonalityTags []string `json:"personality_tags" form:"personality_tags"`   // 性格标签
}
