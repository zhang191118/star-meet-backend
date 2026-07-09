package request

type UpdateUserSettingsReq struct {
	RelationshipMode string `json:"relationship_mode"`
	ResponseStyle    string `json:"response_style"`
	ResponseLength   string `json:"response_length"`

	MemoryEnabled   *bool `json:"memory_enabled"`
	VoiceAutoPlay   *bool `json:"voice_auto_play"`
	PushEnabled     *bool `json:"push_enabled"`
	ProactiveEnable *bool `json:"proactive_enable"`
	PrivacyMode     *bool `json:"privacy_mode"`
}
