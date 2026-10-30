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

	// ===== 导演层 AI 模块字段 (P0) =====
	DirectorState     string `json:"director_state" gorm:"type:text"`
	ContextSummary    string `json:"context_summary" gorm:"type:text"`
	PlotState         string `json:"plot_state" gorm:"size:32;default:opening"`
	PlotBeat          int    `json:"plot_beat" gorm:"default:0"`
	PlotThreads       string `json:"plot_threads" gorm:"type:text"`
	CharacterEmotion  string `json:"character_emotion" gorm:"size:32;default:平静"`
	EmotionIntensity  int    `json:"emotion_intensity" gorm:"default:1"`
	RelationshipStage string `json:"relationship_stage" gorm:"size:20;default:初識"`
	AffectionScore    int    `json:"affection_score" gorm:"default:0"`
	MessageCount      int    `json:"message_count" gorm:"default:0"`
}

func (Session) TableName() string {
	return "sessions"
}
