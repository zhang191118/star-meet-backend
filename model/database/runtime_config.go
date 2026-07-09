package database

import "time"

// RuntimeConfig stores deploy-time settings that should not be hard-coded in
// source files. Sensitive values are redacted by management APIs by default.
type RuntimeConfig struct {
	ID        uint       `json:"id" gorm:"primarykey"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	Key         string `json:"key" gorm:"column:config_key;uniqueIndex;size:128;not null"`
	Value       string `json:"value" gorm:"type:text"`
	Sensitive   bool   `json:"sensitive" gorm:"default:true"`
	Description string `json:"description" gorm:"size:255"`
}

func (RuntimeConfig) TableName() string {
	return "runtime_configs"
}
