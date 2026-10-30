package database

import (
	"time"

	"gorm.io/gorm"
)

// CharacterMoment 角色动态（朋友圈式内容，由每日脚本生成）
type CharacterMoment struct {
	ID        uint       `json:"id" gorm:"primarykey"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	CharacterID string `json:"character_id" gorm:"size:64;not null;index:idx_moments_char_day,priority:1"`
	Content     string `json:"content" gorm:"type:text;not null"`
	ImageURLs   string `json:"image_urls" gorm:"type:json"` // JSON数组，OSS URL，0~3张
	TopicTags   string `json:"topic_tags" gorm:"size:255;default:''"` // 逗号分隔
	LikeCount   int    `json:"like_count" gorm:"default:0"`
	Status      int    `json:"status" gorm:"index:idx_moments_feed,priority:1"` // 1=正常 0=下架（脚本显式写入）

	// PublishDay 用于"每角色每天一条"的唯一约束，格式 YYYY-MM-DD
	PublishDay  string    `json:"publish_day" gorm:"size:10;not null;index:idx_moments_char_day,unique,priority:2"`
	PublishedAt time.Time `json:"published_at" gorm:"index:idx_moments_feed,sort:desc,priority:2"`
}

func (CharacterMoment) TableName() string {
	return "character_moments"
}
