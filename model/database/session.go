package database

import (
	"time"
)

type Session struct {
	ID        uint       `json:"id" gorm:"primarykey;index:idx_sessions_user_pinned_last_message,sort:desc,priority:4"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	SessionID   string `json:"session_id" gorm:"uniqueIndex;size:64;not null" json:"session_id"`
	UserID      string `json:"user_id" gorm:"index;index:idx_sessions_user_character,priority:1;index:idx_sessions_user_pinned_last_message,priority:1;size:64;not null" json:"user_id"`
	CharacterID string `json:"character_id" gorm:"index;index:idx_sessions_user_character,priority:2;size:64;not null" json:"character_id"`

	LastMessage   string     `json:"last_message" gorm:"type:text" json:"last_message"`
	LastMessageAt *time.Time `json:"last_message_at" gorm:"index:idx_sessions_user_pinned_last_message,sort:desc,priority:3" json:"last_message_at"`
	UnreadCount   int        `json:"unread_count" gorm:"default:0" json:"unread_count"`
	IsPinned      bool       `json:"is_pinned" gorm:"default:false;index:idx_sessions_user_pinned_last_message,sort:desc,priority:2" json:"is_pinned"`
}

func (Session) TableName() string {
	return "sessions"
}
