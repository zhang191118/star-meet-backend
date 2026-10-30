package database

import "time"

type UserSettings struct {
	ID        uint       `json:"id" gorm:"primarykey"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	UserID string `json:"user_id" gorm:"uniqueIndex;size:64;not null"`

	RelationshipMode string `json:"relationship_mode" gorm:"size:20;default:romantic"`
	ResponseStyle    string `json:"response_style" gorm:"size:20;default:balanced"`
	ResponseLength   string `json:"response_length" gorm:"size:20;default:medium"`

	MemoryEnabled          bool   `json:"memory_enabled" gorm:"default:true"`
	VoiceAutoPlay          bool   `json:"voice_auto_play" gorm:"default:false"`
	PushEnabled            bool   `json:"push_enabled" gorm:"default:true"`
	ProactiveEnable        bool   `json:"proactive_enable" gorm:"default:true"`
	ProactiveQuietStart    string `json:"proactive_quiet_start" gorm:"size:5;default:22:30"`
	ProactiveQuietEnd      string `json:"proactive_quiet_end" gorm:"size:5;default:08:00"`
	ProactiveDailyLimit    int    `json:"proactive_daily_limit" gorm:"default:1"`
	ProactiveOnlyWhenAway  bool   `json:"proactive_only_when_away" gorm:"default:true"`
	ProactivePreviewMode   string `json:"proactive_preview_mode" gorm:"size:16;default:name_only"`
	PromiseReminderEnabled bool   `json:"promise_reminder_enabled" gorm:"default:true"`
	PrivacyMode            bool   `json:"privacy_mode" gorm:"default:false"`
}

func (UserSettings) TableName() string {
	return "user_settings"
}
