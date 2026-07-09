package database

import (
	"time"
)

type Message struct {
	ID        uint       `json:"id" gorm:"primarykey;index:idx_messages_session_created_id,sort:desc,priority:3;index:idx_messages_user_character_created_id,sort:desc,priority:4"`
	CreatedAt time.Time  `json:"created_at" gorm:"index:idx_messages_session_created_id,sort:desc,priority:2;index:idx_messages_user_character_created_id,sort:desc,priority:3"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	MessageID   string `json:"message_id" gorm:"uniqueIndex;size:64;not null" json:"message_id"`
	SessionID   string `json:"session_id" gorm:"index;index:idx_messages_session_created_id,priority:1;size:64;not null" json:"session_id"`
	CharacterID string `json:"character_id" gorm:"index;index:idx_messages_user_character_created_id,priority:2;size:64;not null" json:"character_id"`
	UserID      string `json:"user_id" gorm:"index;index:idx_messages_user_character_created_id,priority:1;size:64;not null" json:"user_id"`

	Role        string `json:"role" gorm:"size:10;not null" json:"role"` // user/assistant
	Content     string `json:"content" gorm:"type:text;not null" json:"content"`
	MessageType string `json:"message_type" gorm:"size:10;default:text" json:"message_type"` // text/voice/image

	// 语音
	VoiceURL      string  `json:"voice_url" gorm:"size:255" json:"voice_url"`
	VoiceDuration float64 `json:"voice_duration" json:"voice_duration"` // 秒

	// 情绪分析
	UserEmotion  string `json:"user_emotion" gorm:"size:20" json:"user_emotion"`
	ReplyEmotion string `json:"reply_emotion" gorm:"size:20" json:"reply_emotion"`

	// 已读状态
	IsRead bool       `json:"is_read" gorm:"default:false" json:"is_read"`
	ReadAt *time.Time `json:"read_at,omitempty" gorm:"index"`

	// 撤回状态
	IsRecalled   bool       `json:"is_recalled" gorm:"default:false" json:"is_recalled"`
	RecalledAt   *time.Time `json:"recalled_at,omitempty"`
	RecallReason string     `json:"recall_reason" gorm:"size:255" json:"recall_reason"`

	// 引用消息
	QuotedMessageID string `json:"quoted_message_id" gorm:"size:64;index" json:"quoted_message_id"`
}

func (Message) TableName() string {
	return "messages"
}
