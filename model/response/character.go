package response

// 角色详情响应
type CharacterResp struct {
	CharacterID      string   `json:"character_id"`
	UserID           string   `json:"user_id,omitempty"`
	Name             string   `json:"name"`
	Gender           string   `json:"gender"`
	Age              int      `json:"age"`
	Personality      []string `json:"personality"`
	Occupation       string   `json:"occupation"`
	Background       string   `json:"background"`
	VoiceID          string   `json:"voice_id"`
	Relationship     string   `json:"relationship"`
	InteractionStyle string   `json:"interaction_style"`
	Avatar           string   `json:"avatar"`
	SystemPrompt     string   `json:"system_prompt,omitempty"`
	IsPublic         int      `json:"is_public"` // 0=私有, 1=系统公共, 2=用户私有
	Affection        int      `json:"affection"`
	ChatCount        int      `json:"chat_count"`
	FavoriteCount    int      `json:"favorite_count"` // 收藏次数
	Mem0AgentID      string   `json:"mem0_agent_id,omitempty"`
	CreatedAt        int64    `json:"created_at"`
}

// 公开角色响应（广场）
type PublicCharacterResp struct {
	CharacterID   string   `json:"character_id"`
	Name          string   `json:"name"`
	Gender        string   `json:"gender"`
	Personality   []string `json:"personality"`
	Relationship  string   `json:"relationship"`
	Avatar        string   `json:"avatar"`
	ChatCount     int      `json:"chat_count"`     // 被聊天次数
	FavoriteCount int      `json:"favorite_count"` // 收藏次数
	IsFavorited   bool     `json:"is_favorited"`   // 当前用户是否收藏
	CreatedAt     int64    `json:"created_at"`
}

// 用户响应
type UserResp struct {
	UserID       string `json:"user_id"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	Nickname     string `json:"nickname"`
	Avatar       string `json:"avatar"`
	MemberLevel  string `json:"member_level"`
	MemberExpire string `json:"member_expire"`
	Language     string `json:"language"`
	Region       string `json:"region"`
	Gender       string `json:"gender"`
	Age          int    `json:"age"`
	ChatDays     int    `json:"chat_days"`
	CharCount    int    `json:"char_count"`
	MsgCount     int    `json:"msg_count"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	LastLoginAt  int64  `json:"last_login_at"`
}

// 登录响应
type LoginResp struct {
	UserID string `json:"user_id"`
	Token  string `json:"token"`
	IsNew  bool   `json:"is_new"`
}
