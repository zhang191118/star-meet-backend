package database

import "time"

// Feedback is a private support ticket. Public submissions receive only a receipt.
type Feedback struct {
	ID         string    `json:"feedback_id" gorm:"primaryKey;size:64"`
	UserID     string    `json:"user_id,omitempty" gorm:"size:64;index"`
	Category   string    `json:"category" gorm:"size:20;not null"`
	Content    string    `json:"content" gorm:"type:text;not null"`
	Contact    string    `json:"contact,omitempty" gorm:"size:200"`
	AppVersion string    `json:"app_version" gorm:"size:32"`
	Platform   string    `json:"platform" gorm:"size:20"`
	Device     string    `json:"device" gorm:"size:100"`
	RequestID  string    `json:"request_id,omitempty" gorm:"size:100"`
	Status     string    `json:"status" gorm:"size:20;index;default:received"`
	CreatedAt  time.Time `json:"created_at" gorm:"index"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Feedback) TableName() string { return "feedback" }

// ProductEvent contains bounded metadata only. Server event names cannot be submitted by clients.
type ProductEvent struct {
	ID             string    `json:"event_id" gorm:"primaryKey;size:64"`
	Name           string    `json:"name" gorm:"size:40;index:idx_product_event_name_time,priority:1"`
	InstallationID string    `json:"installation_id,omitempty" gorm:"size:64;index"`
	UserID         string    `json:"user_id,omitempty" gorm:"size:64;index:idx_product_event_user_time,priority:1"`
	UserMessageID  string    `json:"-" gorm:"size:64;index"`
	CharacterID    string    `json:"character_id,omitempty" gorm:"size:100"`
	Properties     string    `json:"-" gorm:"type:text"`
	Internal       bool      `json:"internal" gorm:"default:false"`
	OccurredAt     time.Time `json:"occurred_at" gorm:"index:idx_product_event_name_time,priority:2;index:idx_product_event_user_time,priority:2"`
	CreatedAt      time.Time `json:"created_at"`
}

func (ProductEvent) TableName() string { return "product_events" }

// MemoryIndexJob is an outbox row coalesced by memory ID. Leases survive newer revisions.
type MemoryIndexJob struct {
	MemoryID           string     `json:"memory_id" gorm:"primaryKey;size:64"`
	UserID             string     `json:"-" gorm:"size:64;index"`
	CharacterID        string     `json:"-" gorm:"size:100"`
	Revision           uint64     `json:"revision"`
	CleanupCharacterID string     `json:"-" gorm:"size:100"`
	Attempts           int        `json:"attempts"`
	NextAttemptAt      time.Time  `json:"-" gorm:"index"`
	LeaseToken         string     `json:"-" gorm:"size:64"`
	LeaseUntil         *time.Time `json:"-" gorm:"index"`
	LastError          string     `json:"-" gorm:"size:64"`
	CreatedAt          time.Time  `json:"-"`
	UpdatedAt          time.Time  `json:"-"`
}

func (MemoryIndexJob) TableName() string { return "memory_index_jobs" }
