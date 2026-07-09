package database

import (
	"time"
)

type User struct {
	ID        uint       `json:"id" gorm:"primarykey"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-" gorm:"index"`

	UserID       string     `json:"user_id" gorm:"uniqueIndex;size:64;not null" json:"user_id"`
	Email        string     `json:"email" gorm:"uniqueIndex;size:100" json:"email"`
	Phone        string     `json:"phone" gorm:"index;size:11" json:"phone"`
	Nickname     string     `json:"nickname" gorm:"size:50" json:"nickname"`
	Avatar       string     `json:"avatar" gorm:"size:255" json:"avatar"`
	Password     string     `json:"-" gorm:"size:255" json:"-"`
	MemberLevel  string     `json:"member_level" gorm:"size:20;default:free" json:"member_level"`
	MemberExpire *time.Time `json:"member_expire" json:"member_expire"`

	// 3rd Party Login
	WechatOpenID string `json:"wechat_openid" gorm:"index;size:64" json:"wechat_openid"`
	AppleUserID  string `json:"apple_user_id" gorm:"index;size:64" json:"apple_user_id"`

	// Minor Protection & Compliance
	Age            int        `json:"age" gorm:"default:0" json:"age"`
	IsMinor        bool       `json:"is_minor" gorm:"default:false" json:"is_minor"`
	DailyUsageTime int64      `json:"daily_usage_time" gorm:"default:0" json:"daily_usage_time"` // in seconds
	LastLoginAt    *time.Time `json:"last_login_at" json:"last_login_at"`
	LoginIP        string     `json:"login_ip" gorm:"size:45" json:"login_ip"`

	// 语言和地区设置
	Language string `json:"language" gorm:"size:10;default:en" json:"language"` // en, zh-Hans, zh-Hant, ko, ja
	Region   string `json:"region" gorm:"size:10;default:US" json:"region"`     // US, TW, HK, SG, MY, AU, IN, KR, JP, CN

	// 性别偏好 (male/female/unknown)
	Gender string `json:"gender" gorm:"size:10;default:unknown" json:"gender"`

	// 统计
	ChatDays  int `json:"chat_days" gorm:"default:0" json:"chat_days"`
	CharCount int `json:"char_count" gorm:"default:0" json:"char_count"`
	MsgCount  int `json:"msg_count" gorm:"default:0" json:"msg_count"`
}

func (User) TableName() string {
	return "users"
}
