package response

type UserSettingsResp struct {
	UserID string `json:"user_id"`

	RelationshipMode string `json:"relationship_mode"`
	ResponseStyle    string `json:"response_style"`
	ResponseLength   string `json:"response_length"`

	MemoryEnabled          bool   `json:"memory_enabled"`
	VoiceAutoPlay          bool   `json:"voice_auto_play"`
	PushEnabled            bool   `json:"push_enabled"`
	ProactiveEnable        bool   `json:"proactive_enable"`
	ProactiveQuietStart    string `json:"proactive_quiet_start"`
	ProactiveQuietEnd      string `json:"proactive_quiet_end"`
	ProactiveDailyLimit    int    `json:"proactive_daily_limit"`
	ProactiveOnlyWhenAway  bool   `json:"proactive_only_when_away"`
	ProactivePreviewMode   string `json:"proactive_preview_mode"`
	PromiseReminderEnabled bool   `json:"promise_reminder_enabled"`
	PrivacyMode            bool   `json:"privacy_mode"`

	UpdatedAt int64 `json:"updated_at"`
}
