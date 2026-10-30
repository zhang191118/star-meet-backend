package database

import "time"

// Notification 站内通知
type Notification struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_notifications_user_created,sort:desc,priority:2"`

	UserID           string `json:"user_id" gorm:"index;index:idx_notifications_user_created,priority:1;size:64;not null"`
	Category         string `json:"category" gorm:"size:20;not null"` // system, character, interaction, proactive
	Title            string `json:"title" gorm:"size:100;not null"`
	Preview          string `json:"preview" gorm:"size:255"`
	CharacterID      string `json:"character_id,omitempty" gorm:"size:64;index"`
	ProactiveEventID string `json:"-" gorm:"size:64;index"`
	DeepLink         string `json:"deep_link,omitempty" gorm:"size:255"`
	IsRead           bool   `json:"is_read" gorm:"default:false"`
}

func (Notification) TableName() string {
	return "notifications"
}

// PushDevice stores one APNs token and its current account ownership.
type PushDevice struct {
	ID          uint      `json:"id" gorm:"primarykey"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	UserID      string    `json:"user_id" gorm:"index;size:64;not null"`
	Token       string    `json:"-" gorm:"uniqueIndex;size:128;not null"`
	Platform    string    `json:"platform" gorm:"size:16;not null"`
	Environment string    `json:"environment" gorm:"size:16;not null"`
	IsActive    bool      `json:"is_active" gorm:"default:true;index"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

func (PushDevice) TableName() string {
	return "push_devices"
}

// UserAffection 用户与角色的亲密度（服务端权威数据）
type UserAffection struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	UserID      string `json:"user_id" gorm:"uniqueIndex:idx_user_affection_user_character,priority:1;size:64;not null"`
	CharacterID string `json:"character_id" gorm:"uniqueIndex:idx_user_affection_user_character,priority:2;size:64;not null"`
	Exp         int    `json:"exp" gorm:"default:0"`
	Level       int    `json:"level" gorm:"default:1"`
}

func (UserAffection) TableName() string {
	return "user_affections"
}
