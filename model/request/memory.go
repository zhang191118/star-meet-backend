package request

import "time"

// 获取记忆列表
type GetMemoryListReq struct {
	UserID      string `form:"user_id"`
	CharacterID string `form:"character_id" binding:"required"`
	MemoryType  string `form:"memory_type"` // all/preference/emotion/event/relationship
	Page        int    `form:"page"`
	PageSize    int    `form:"page_size"`
}

// 添加记忆
type AddMemoryReq struct {
	ObservedAt  time.Time `json:"-"`
	UserID      string    `json:"user_id"`
	CharacterID string    `json:"character_id" binding:"required"`
	Content     string    `json:"content" binding:"required"`
	MemoryType  string    `json:"memory_type"` // preference/emotion/event/relationship/fact
	Importance  int       `json:"importance"`
	Source      string    `json:"source"` // manual/auto
}

// 更新记忆
type UpdateMemoryReq struct {
	Content string `json:"content"`
}

// 搜索记忆
type SearchMemoryReq struct {
	UserID      string `form:"user_id"`
	CharacterID string `form:"character_id" binding:"required"`
	Query       string `form:"query" binding:"required"`
}
