package database

import "time"

// ProactiveEvent is a server-side candidate for a context-driven proactive message.
// It is separate from notifications so candidates can expire or be cancelled
// without creating a user-visible notification.
type ProactiveEvent struct {
	ID                 uint       `json:"id" gorm:"primarykey"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	EventID            string     `json:"event_id" gorm:"uniqueIndex;size:64;not null"`
	UserID             string     `json:"user_id" gorm:"index;size:64;not null"`
	CharacterID        string     `json:"character_id" gorm:"index;size:64;not null"`
	EventType          string     `json:"event_type" gorm:"size:32;not null"`
	SourceMemoryID     string     `json:"source_memory_id" gorm:"size:64"`
	TriggerAt          time.Time  `json:"trigger_at" gorm:"index"`
	ExpiresAt          *time.Time `json:"expires_at"`
	DedupeKey          string     `json:"dedupe_key" gorm:"uniqueIndex;size:160;not null"`
	Reason             string     `json:"reason" gorm:"size:255"`
	Status             string     `json:"status" gorm:"size:16;default:pending;index"`
	ProcessingAt       *time.Time `json:"-" gorm:"index"`
	AssistantMessageID string     `json:"assistant_message_id,omitempty" gorm:"size:64;index"`
	GeneratedAt        *time.Time `json:"-"`
	AttemptCount       int        `json:"-" gorm:"default:0"`
	LastError          string     `json:"-" gorm:"size:500"`
	SentAt             *time.Time `json:"sent_at"`
}

func (ProactiveEvent) TableName() string { return "proactive_events" }

type UserCharacterNotificationSetting struct {
	ID          uint       `json:"id" gorm:"primarykey"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	UserID      string     `json:"user_id" gorm:"uniqueIndex:idx_user_character_notification_settings,priority:1;size:64;not null"`
	CharacterID string     `json:"character_id" gorm:"uniqueIndex:idx_user_character_notification_settings,priority:2;size:64;not null"`
	Enabled     bool       `json:"enabled" gorm:"default:true"`
	DailyLimit  int        `json:"daily_limit" gorm:"default:1"`
	MutedUntil  *time.Time `json:"muted_until"`
}

func (UserCharacterNotificationSetting) TableName() string {
	return "user_character_notification_settings"
}
