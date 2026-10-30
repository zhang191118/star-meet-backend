package request

// 创建角色请求
type CreateCharacterReq struct {
	UserID           string   `json:"user_id"`
	Name             string   `json:"name" binding:"required,max=10"`
	Gender           string   `json:"gender"` // female/male/neutral
	Age              int      `json:"age"`
	Personality      []string `json:"personality"` // 性格标签数组
	Occupation       string   `json:"occupation"`
	Background       string   `json:"background"`
	VoiceID          string   `json:"voice_id"`
	Relationship     string   `json:"relationship"`      // 伙伴/朋友/知己/自定义
	InteractionStyle string   `json:"interaction_style"` // 温暖型/成熟型/互怼型/治愈型
	Avatar           string   `json:"avatar"`            // base64或URL
	IsPublic         int      `json:"is_public"`         // 0=私有, 1=系统公共, 2=用户私有
	ChatCount        int      `json:"chat_count"`        // 初始聊天数
}

// 更新角色请求
type UpdateCharacterReq struct {
	CharacterID      string   `json:"character_id"`
	UserID           string   `json:"user_id"`
	Name             string   `json:"name"`
	Gender           string   `json:"gender"`
	Age              int      `json:"age"`
	Personality      []string `json:"personality"`
	Occupation       string   `json:"occupation"`
	Background       string   `json:"background"`
	BgImage          string   `json:"bg_image"`
	Relationship     string   `json:"relationship"`
	InteractionStyle string   `json:"interaction_style"`
	Avatar           string   `json:"avatar"`
}

// 获取角色列表请求
type GetCharacterListReq struct {
	UserID   string `form:"user_id"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// 获取公开角色请求
type GetPublicCharactersReq struct {
	UserID   string `form:"-"` // 从 JWT 中获取，不直接由前端传入
	Category string `form:"category"` // 男友/女友/朋友/全部
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
	BeforeID string `form:"before_id"` // 游标分页
	Sort     string `form:"sort"`      // hot/latest
	Search   string `form:"search"`
}

// 搜索角色请求
type SearchCharacterReq struct {
	UserID      string `form:"-"` // 从 JWT 中获取，不直接由前端传入
	Keyword     string `form:"keyword"`
	Category    string `form:"category"`
	Gender      string `form:"gender"`
	Personality string `form:"personality"`
	Page        int    `form:"page"`
	PageSize    int    `form:"page_size"`
}

// 推荐角色请求
type GetRecommendReq struct {
	UserID   string `form:"user_id"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}
