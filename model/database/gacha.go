package database

import (
	"time"
)

type GachaRarity string

const (
	GachaSSR GachaRarity = "SSR" // 限定角色 1%
	GachaSR  GachaRarity = "SR"  // 高级角色 4%
	GachaR   GachaRarity = "R"   // 普通角色 15%
	GachaN   GachaRarity = "N"   // 基础角色 80%
)

type GachaPool struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	PoolID     string     `json:"pool_id" gorm:"uniqueIndex;size:50;not null"`
	Name       string     `json:"name" gorm:"size:100;not null"`
	Type       string     `json:"type" gorm:"size:20;not null"` // limited, permanent, event
	StartTime  *time.Time `json:"start_time"`
	EndTime    *time.Time `json:"end_time"`
	IsActive   bool       `json:"is_active" gorm:"default:true;index"`
	SSRRate    float64    `json:"ssr_rate" gorm:"default:0.01"`  // 1%
	SRRate     float64    `json:"sr_rate" gorm:"default:0.04"`   // 4%
	RRate      float64    `json:"r_rate" gorm:"default:0.15"`    // 15%
	NGuarantee int        `json:"n_guarantee" gorm:"default:80"` // SSR保底80抽
	Price      int        `json:"price" gorm:"default:160"`      // 单抽价格(月亮)
}

func (GachaPool) TableName() string {
	return "gacha_pools"
}

type GachaCharacter struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`

	CharacterID string      `json:"character_id" gorm:"size:64;not null"`
	PoolID      string      `json:"pool_id" gorm:"index;index:idx_gacha_characters_pool_rarity,priority:1;size:50;not null"`
	Rarity      GachaRarity `json:"rarity" gorm:"size:10;not null;index:idx_gacha_characters_pool_rarity,priority:2"`
	Weight      int         `json:"weight" gorm:"default:1"` // 权重
	IsLimited   bool        `json:"is_limited" gorm:"default:false"`
}

func (GachaCharacter) TableName() string {
	return "gacha_characters"
}

type UserGachaRecord struct {
	ID        uint      `json:"id" gorm:"primarykey;index:idx_user_gacha_records_user_created,sort:desc,priority:3;index:idx_user_gacha_records_client_draw,priority:3"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_user_gacha_records_user_created,sort:desc,priority:2"`

	UserID       string      `json:"user_id" gorm:"index;index:idx_user_gacha_records_user_created,priority:1;index:idx_user_gacha_records_user_pool,priority:1;index:idx_user_gacha_records_client_draw,priority:1;size:64;not null"`
	PoolID       string      `json:"pool_id" gorm:"index;index:idx_user_gacha_records_user_pool,priority:2;size:50;not null"`
	CharacterID  string      `json:"character_id" gorm:"size:64;not null"`
	Rarity       GachaRarity `json:"rarity" gorm:"size:10;not null"`
	Count        int         `json:"count"`        // 第几抽
	IsGuarantee  bool        `json:"is_guarantee"` // 是否保底
	Cost         int         `json:"cost"`         // 花费月亮数
	ClientDrawID string      `json:"client_draw_id,omitempty" gorm:"index;index:idx_user_gacha_records_client_draw,priority:2;size:96"`
}

func (UserGachaRecord) TableName() string {
	return "user_gacha_records"
}

type UserGachaDraw struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`

	UserID       string `json:"user_id" gorm:"uniqueIndex:idx_gacha_client_draw;size:64;not null"`
	ClientDrawID string `json:"client_draw_id" gorm:"uniqueIndex:idx_gacha_client_draw;size:96;not null"`
	PoolID       string `json:"pool_id" gorm:"size:50;not null"`
	Count        int    `json:"count" gorm:"not null"`
}

func (UserGachaDraw) TableName() string {
	return "user_gacha_draws"
}

type UserCharacter struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at"`

	UserID      string `json:"user_id" gorm:"index;index:idx_user_characters_user_character,priority:1;size:64;not null"`
	CharacterID string `json:"character_id" gorm:"index:idx_user_characters_user_character,priority:2;size:64;not null"`
	ObtainType  string `json:"obtain_type" gorm:"size:20"`  // gacha, purchase, event, system
	StarLevel   int    `json:"star_level" gorm:"default:1"` // 星级
	Exp         int    `json:"exp" gorm:"default:0"`
	IsFavorite  bool   `json:"is_favorite" gorm:"default:false"`
}

func (UserCharacter) TableName() string {
	return "user_characters"
}
