package database

import "time"

// Music 背景音乐表
type Music struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Title    string `json:"title" gorm:"size:100;not null"`
	Artist   string `json:"artist" gorm:"size:100"`
	URL      string `json:"url" gorm:"size:500;not null;uniqueIndex"`
	Duration int    `json:"duration" gorm:"default:0"` // 时长（秒）
	Sort     int    `json:"sort" gorm:"default:0"`     // 排序
	Enabled  bool   `json:"enabled" gorm:"default:true"`
}

func (Music) TableName() string {
	return "musics"
}
