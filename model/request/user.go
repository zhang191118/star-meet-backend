package request

// RegisterReq 注册请求
type RegisterReq struct {
	Email    string `json:"email" form:"email" binding:"required,email"`               // 邮箱
	Password string `json:"password" form:"password"`                                  // 可选密码；未提供时由服务端生成
	Nickname string `json:"nickname" form:"nickname"`                                  // 昵称
	Age      int    `json:"age" form:"age" binding:"required,min=18,max=150"`          // 年龄（必须年满18岁）
	Code     string `json:"code" form:"code"`                                          // 验证码
	Language string `json:"language" form:"language"`                                  // 语言: en, zh-Hans, zh-Hant, ko, ja
	Region   string `json:"region" form:"region"`                                      // 地区: US, TW, HK, SG, MY, AU, IN, KR, JP, CN
	Gender   string `json:"gender" form:"gender" binding:"omitempty,oneof=male female"` // 性别: male, female（可选，缺省默认 female）
}

// LoginReq 登录请求
type LoginReq struct {
	Email    string `json:"email" form:"email"`       // 邮箱
	Password string `json:"password" form:"password"` // 密码
	Code     string `json:"code" form:"code"`         // 验证码
}

// RefreshTokenReq 刷新Token请求
type RefreshTokenReq struct {
	RefreshToken string `json:"refresh_token" form:"refresh_token" binding:"required"`
}

// GetUserInfoReq 获取用户信息请求
type GetUserInfoReq struct {
	UserID string `json:"user_id" form:"user_id"`
}

// UpdateUserInfoReq 更新用户信息请求
type UpdateUserInfoReq struct {
	UserID   string `json:"user_id" form:"user_id"`
	Nickname string `json:"nickname" form:"nickname"`
	Avatar   string `json:"avatar" form:"avatar"`
	Language string `json:"language" form:"language"` // 语言
	Region   string `json:"region" form:"region"`     // 地区
	Gender   string `json:"gender" form:"gender"`     // 性别: male, female, unknown
	Age      int    `json:"age" form:"age"`           // 年龄
}

// WechatLoginReq 微信登录请求
type WechatLoginReq struct {
	Code string `json:"code" binding:"required"`
}

// AppleLoginReq 苹果登录请求
type AppleLoginReq struct {
	IdentityToken string `json:"identity_token" binding:"required"`
	UserID        string `json:"user_id" binding:"required"`
	// Nonce 客户端生成随机串的原文，服务端校验其 SHA-256 与 identity_token 内的
	// nonce 声明一致（Apple 官方推荐），防止缓存/过期 token 被重放；旧客户端不传则跳过
	Nonce    string `json:"nonce"`
	Nickname string `json:"nickname"`
}

// LineLoginReq LINE 登录请求（ID Token 由客户端 LINE SDK 提供）
type LineLoginReq struct {
	IDToken  string `json:"id_token" binding:"required"`
	UserID   string `json:"user_id" binding:"required"`
	Nickname string `json:"nickname"`
	Picture  string `json:"picture"`
}

// DeleteAccountReq allows the client to explicitly acknowledge the destructive action.
type DeleteAccountReq struct {
	Confirmation string `json:"confirmation"`
}

// SendVerifyCodeReq 发送验证码请求
type SendVerifyCodeReq struct {
	Email string `json:"email" form:"email" binding:"required,email"`
	Type  string `json:"type" form:"type"` // register, reset-password
}

// ResetPasswordReq 重置密码请求
type ResetPasswordReq struct {
	Email    string `json:"email" form:"email" binding:"required,email"`
	Code     string `json:"code" form:"code" binding:"required"`
	Password string `json:"password" form:"password" binding:"required,min=6"`
}
