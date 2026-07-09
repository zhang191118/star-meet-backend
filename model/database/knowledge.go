package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Knowledge 知识库表
type Knowledge struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	KnowledgeID string    `gorm:"uniqueIndex;size:64" json:"knowledge_id"`
	Category    string    `gorm:"size:32;index;index:idx_knowledge_category_active,priority:1;index:idx_knowledge_category_sub_active,priority:1" json:"category"` // 分类: emotion(情感话术), scene(场景), compliance(合规)
	SubCategory string    `gorm:"size:32;index:idx_knowledge_category_sub_active,priority:2" json:"sub_category"`                                                  // 子分类: happy, sad, angry, cute, etc.
	Content     string    `gorm:"type:text" json:"content"`                                                                                                        // 话术内容
	Keywords    string    `gorm:"size:500" json:"keywords"`                                                                                                        // 关键词，用于匹配
	Emotion     string    `gorm:"size:32" json:"emotion"`                                                                                                          // 关联情绪
	IsActive    bool      `gorm:"default:true;index:idx_knowledge_category_active,priority:2;index:idx_knowledge_category_sub_active,priority:3" json:"is_active"`
}

// 自动迁移
func (Knowledge) TableName() string {
	return "knowledge"
}

func (k *Knowledge) BeforeCreate(tx *gorm.DB) error {
	if k.KnowledgeID == "" {
		k.KnowledgeID = uuid.New().String()
	}
	return nil
}
