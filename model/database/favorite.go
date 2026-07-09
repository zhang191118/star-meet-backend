package database

import (
	"time"
)

type Favorite struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	UserID      string `json:"user_id" gorm:"index;index:idx_favorites_user_character,priority:1;size:64;not null" json:"user_id"`
	CharacterID string `json:"character_id" gorm:"index;index:idx_favorites_user_character,priority:2;size:64;not null" json:"character_id"`

	// 公开角色的收藏
	PublicCharID string `json:"public_char_id" gorm:"size:32" json:"public_char_id"`
}

func (Favorite) TableName() string {
	return "favorites"
}
