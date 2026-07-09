package response

// CharacterConfigResp 角色配置响应
type CharacterConfigResp struct {
	CharacterID    string   `json:"character_id"`
	SpeechTone     string   `json:"speech_tone"`
	Personality    []string `json:"personality"`
	Hobbies        []string `json:"hobbies"`
	Catchphrases   []string `json:"catchphrases"`
	Strengths      string   `json:"strengths"`
	Weaknesses     string   `json:"weaknesses"`
	InteractionMode string `json:"interaction_mode"`
	Appearance     string   `json:"appearance"`
	Birthday       string   `json:"birthday"`
	Constellation  string   `json:"constellation"`
	PersonalityTags []string `json:"personality_tags"`
}
