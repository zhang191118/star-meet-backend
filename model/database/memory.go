package database

import (
	"gorm.io/gorm"
	"time"
)

type Memory struct {
	ID        uint           `json:"id" gorm:"primarykey;index:idx_memories_user_character_created_id,sort:desc,priority:4;index:idx_memories_user_character_type_created_id,sort:desc,priority:5"`
	CreatedAt time.Time      `json:"created_at" gorm:"index:idx_memories_user_character_created_id,sort:desc,priority:3;index:idx_memories_user_character_type_created_id,sort:desc,priority:4"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	MemoryID    string `json:"memory_id" gorm:"uniqueIndex;size:64;not null" json:"memory_id"`
	UserID      string `json:"user_id" gorm:"index;index:idx_memories_user_character_created_id,priority:1;index:idx_memories_user_character_type_created_id,priority:1;size:64;not null" json:"user_id"`
	CharacterID string `json:"character_id" gorm:"index;index:idx_memories_user_character_created_id,priority:2;index:idx_memories_user_character_type_created_id,priority:2;size:64;not null" json:"character_id"`

	Content    string `json:"content" gorm:"type:text;not null" json:"content"`
	MemoryType string `json:"memory_type" gorm:"size:20;default:fact;index:idx_memories_user_character_type_created_id,priority:3" json:"memory_type"` // preference/emotion/event/relationship/fact/promise/boundary
	Source     string `json:"source" gorm:"size:10;default:auto" json:"source"`                                                                        // manual/auto

	Revision        uint64 `json:"revision" gorm:"default:1;not null"`
	IndexedRevision uint64 `json:"-" gorm:"default:0;not null"`

	// Mem0相关
	Mem0ID     string `json:"mem0_id" gorm:"size:64" json:"mem0_id"`
	Importance int    `json:"importance" gorm:"default:3" json:"importance"` // 1-5
}

func (Memory) TableName() string {
	return "memories"
}
