package database

import (
	"time"
)

type Character struct {
	ID        uint       `json:"id" gorm:"primarykey;index:idx_characters_public_rank,sort:desc,priority:4;index:idx_characters_public_relationship_rank,sort:desc,priority:5;index:idx_characters_public_gender_rank,sort:desc,priority:5;index:idx_characters_user_public_rank,sort:desc,priority:5"`
	CreatedAt time.Time  `json:"created_at" gorm:"index:idx_characters_public_rank,sort:desc,priority:3;index:idx_characters_public_relationship_rank,sort:desc,priority:4;index:idx_characters_public_gender_rank,sort:desc,priority:4;index:idx_characters_user_created,sort:desc,priority:2"`
	UpdatedAt time.Time  `json:"updated_at" gorm:"index:idx_characters_user_public_rank,sort:desc,priority:4"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	CharacterID  string `json:"character_id" gorm:"uniqueIndex;size:64;not null" json:"character_id"`
	UserID       string `json:"user_id" gorm:"index;index:idx_characters_user_created,priority:1;index:idx_characters_user_public_rank,priority:1;size:64;not null" json:"user_id"`
	Name         string `json:"name" gorm:"size:50;not null" json:"name"`
	Gender       string `json:"gender" gorm:"size:10;default:female;index:idx_characters_public_gender_rank,priority:2" json:"gender"`
	Age          int    `json:"age" gorm:"default:22" json:"age"`
	Personality  string `json:"personality" gorm:"type:text" json:"personality"` // JSON数组
	Occupation   string `json:"occupation" gorm:"size:50" json:"occupation"`
	Background   string `json:"background" gorm:"type:text" json:"background"`
	VoiceID      string `json:"voice_id" gorm:"size:32" json:"voice_id"`
	Relationship string `json:"relationship" gorm:"size:20;default:恋人;index:idx_characters_public_relationship_rank,priority:2" json:"relationship"`
	Style        string `json:"style" gorm:"size:20;default:撒娇型" json:"style"`
	Avatar       string `json:"avatar" gorm:"size:255" json:"avatar"`

	// 系统Prompt（创建时生成，锁定后不变）
	SystemPrompt string `json:"system_prompt" gorm:"type:text" json:"system_prompt"`

	// 工作与生活（新增）
	WorkSchedule string `json:"work_schedule" gorm:"type:text" json:"work_schedule"` // 工作日程 JSON
	DailyLife    string `json:"daily_life" gorm:"type:text" json:"daily_life"`       // 日常生活描述

	// 公开状态: 0=私有, 1=系统公共角色(发现页可见), 2=用户私有角色(仅我的角色可见)
	IsPublic  int `json:"is_public" gorm:"default:0;index:idx_characters_public_rank,priority:1;index:idx_characters_public_relationship_rank,priority:1;index:idx_characters_public_gender_rank,priority:1;index:idx_characters_user_public_rank,priority:2" json:"is_public"`
	Affection int `json:"affection" gorm:"default:0" json:"affection"`
	ChatCount int `json:"chat_count" gorm:"default:0;index:idx_characters_public_rank,sort:desc,priority:2;index:idx_characters_public_relationship_rank,sort:desc,priority:3;index:idx_characters_public_gender_rank,sort:desc,priority:3;index:idx_characters_user_public_rank,sort:desc,priority:3" json:"chat_count"`
	ViewCount int `json:"view_count" gorm:"default:0" json:"view_count"`
	LikeCount int `json:"like_count" gorm:"default:0" json:"like_count"`

	// Mem0相关
	Mem0AgentID string `json:"mem0_agent_id" gorm:"size:64" json:"mem0_agent_id"`

	// 角色定制扩展
	SpeechTone      string `json:"speech_tone" gorm:"size:50"`
	Hobbies         string `json:"hobbies" gorm:"type:text"`
	Catchphrases    string `json:"catchphrases" gorm:"type:text"`
	Strengths       string `json:"strengths" gorm:"size:100"`
	Weaknesses      string `json:"weaknesses" gorm:"size:100"`
	InteractionMode string `json:"interaction_mode" gorm:"size:20;default:active"`
	Appearance      string `json:"appearance" gorm:"type:text"`
	Birthday        string `json:"birthday" gorm:"size:20"`
	Constellation   string `json:"constellation" gorm:"size:20"`
	PersonalityTags string `json:"personality_tags" gorm:"type:text"`
}

func (Character) TableName() string {
	return "characters"
}
