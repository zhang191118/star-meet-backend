package database

import "time"

type Report struct {
	ID          uint      `json:"id" gorm:"primarykey"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	ReporterID  string `json:"reporter_id" gorm:"index;size:64;not null"`
	TargetType  string `json:"target_type" gorm:"size:20;not null"`
	TargetID    string `json:"target_id" gorm:"size:128;not null"`
	Reason      string `json:"reason" gorm:"size:50;not null"`
	Description string `json:"description" gorm:"size:500"`
	Status      string `json:"status" gorm:"size:20;default:pending"`
}

func (Report) TableName() string {
	return "reports"
}

type Block struct {
	ID         uint      `json:"id" gorm:"primarykey"`
	CreatedAt  time.Time `json:"created_at"`
	BlockerID  string `json:"blocker_id" gorm:"uniqueIndex:idx_block_pair;size:64;not null"`
	BlockedID  string `json:"blocked_id" gorm:"uniqueIndex:idx_block_pair;size:64;not null"`
}

func (Block) TableName() string {
	return "blocks"
}