package request

// 发送消息请求
type SendMessageReq struct {
	UserID          string `json:"user_id" form:"user_id"`
	CharacterID     string `json:"character_id" form:"character_id" binding:"required"`
	Message         string `json:"message" form:"message" binding:"required"`
	MessageType     string `json:"message_type" form:"message_type"`
	UserMessageID   string `json:"user_message_id" form:"user_message_id"`   // 前端生成的ID
	ReplyMessageID  string `json:"reply_message_id" form:"reply_message_id"` // 前端预生成的回复ID
	QuotedMessageID string `json:"quoted_message_id" form:"quoted_message_id"`
}

// 获取历史消息请求
type GetHistoryReq struct {
	UserID      string `form:"user_id"`
	CharacterID string `form:"character_id" binding:"required"`
	Page        int    `form:"page"`
	PageSize    int    `form:"page_size"`
	BeforeID    string `form:"before_id"` // 分页用
}

// 获取会话列表请求
type GetSessionsReq struct {
	UserID   string `form:"user_id"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// 发送语音请求
type SendVoiceReq struct {
	UserID      string `json:"user_id" form:"user_id" binding:"required"`
	CharacterID string `json:"character_id" form:"character_id" binding:"required"`
}
