package service

import (
	"errors"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/utils"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrEmailRegistered     = errors.New("email already registered")
	ErrInvalidVerifyCode   = errors.New("invalid verify code")
	ErrVerifyCodeExpired   = errors.New("verify code expired")
	ErrInvalidUserField    = errors.New("invalid user field")
)

type InvalidUserFieldError struct {
	Field  string
	Reason string
}

func (e InvalidUserFieldError) Error() string {
	return e.Field + ": " + e.Reason
}

func (e InvalidUserFieldError) Is(target error) bool {
	return target == ErrInvalidUserField
}

type UserService struct {
	DB                *gorm.DB
	VerifyCodeService *VerifyCodeService
}

// Register 用户注册
func (s *UserService) Register(req request.RegisterReq) (*response.LoginResp, error) {
	// 检查邮箱是否已存在
	var count int64
	s.DB.Model(&database.User{}).Where("email = ?", req.Email).Count(&count)
	if count > 0 {
		return nil, ErrEmailRegistered
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// 设置默认语言和地区
	language := req.Language
	if language == "" {
		language = "zh-Hant" // 台湾默认繁体中文
	}
	region := req.Region
	if region == "" {
		region = "TW" // 默认台湾
	}
	gender := req.Gender
	if gender == "" {
		gender = "unknown" // 默认未知
	}

	// 生成默认昵称
	nickname := req.Nickname
	if nickname == "" {
		nickname = "用户" + uuid.New().String()[:8]
	}

	userID := uuid.New().String()
	user := database.User{
		UserID:      userID,
		Email:       req.Email,
		Password:    string(hashedPassword),
		Nickname:    nickname,
		MemberLevel: "free",
		Language:    language,
		Region:      region,
		Gender:      gender,
	}

	if err := s.DB.Create(&user).Error; err != nil {
		return nil, err
	}

	// 生成JWT token
	token, err := utils.GenerateToken(userID)
	if err != nil {
		return nil, err
	}

	return &response.LoginResp{
		UserID: userID,
		Token:  token,
		IsNew:  true,
	}, nil
}

// Login 用户登录
func (s *UserService) Login(req request.LoginReq) (*response.LoginResp, error) {
	// 验证码登录模式
	if req.Code != "" {
		return s.loginWithCode(req)
	}

	// 密码登录模式
	return s.loginWithPassword(req)
}

// loginWithCode 验证码登录
func (s *UserService) loginWithCode(req request.LoginReq) (*response.LoginResp, error) {
	// 验证验证码
	if err := s.VerifyCodeService.VerifyCode(req.Email, req.Code, "login"); err != nil {
		return nil, ErrInvalidCredentials
	}

	// 查找用户，不存在则创建
	var user database.User
	err := s.DB.Where("email = ?", req.Email).First(&user).Error

	isNew := false
	if err == gorm.ErrRecordNotFound {
		// 创建新用户
		isNew = true
		userID := uuid.New().String()
		user = database.User{
			UserID:      userID,
			Email:       req.Email,
			Nickname:    "用户" + uuid.New().String()[:8],
			MemberLevel: "free",
			Language:    "zh-Hans",
			Region:      "CN",
			Gender:      "unknown",
		}
		if err := s.DB.Create(&user).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	token, err := utils.GenerateToken(user.UserID)
	if err != nil {
		return nil, err
	}

	return &response.LoginResp{
		UserID: user.UserID,
		Token:  token,
		IsNew:  isNew,
	}, nil
}

// loginWithPassword 密码登录
func (s *UserService) loginWithPassword(req request.LoginReq) (*response.LoginResp, error) {
	var user database.User
	if err := s.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, err := utils.GenerateToken(user.UserID)
	if err != nil {
		return nil, err
	}

	return &response.LoginResp{
		UserID: user.UserID,
		Token:  token,
		IsNew:  false,
	}, nil
}

// WechatLogin 微信登录
func (s *UserService) WechatLogin(req request.WechatLoginReq) (*response.LoginResp, error) {
	// 1. 调用微信接口换取 OpenID (模拟)
	// resp, err := http.Get(fmt.Sprintf("https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code", AppID, AppSecret, req.Code))
	openID := "mock_openid_" + req.Code

	var user database.User
	err := s.DB.Where("wechat_openid = ?", openID).First(&user).Error

	isNew := false
	if err == gorm.ErrRecordNotFound {
		// 创建新用户
		isNew = true
		userID := uuid.New().String()
		user = database.User{
			UserID:       userID,
			WechatOpenID: openID,
			Nickname:     "微信用户_" + userID[:4],
			MemberLevel:  "free",
		}
		if err := s.DB.Create(&user).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	token, err := utils.GenerateToken(user.UserID)
	if err != nil {
		return nil, err
	}
	return &response.LoginResp{
		UserID: user.UserID,
		Token:  token,
		IsNew:  isNew,
	}, nil
}

// AppleLogin 苹果登录
func (s *UserService) AppleLogin(req request.AppleLoginReq) (*response.LoginResp, error) {
	// 1. 验证 IdentityToken (模拟)
	// 这里通常需要使用苹果的公钥验证 JWT
	appleUserID := req.UserID

	var user database.User
	err := s.DB.Where("apple_user_id = ?", appleUserID).First(&user).Error

	isNew := false
	if err == gorm.ErrRecordNotFound {
		// 创建新用户
		isNew = true
		userID := uuid.New().String()
		nickname := req.Nickname
		if nickname == "" {
			nickname = "Apple用户_" + userID[:4]
		}
		user = database.User{
			UserID:      userID,
			AppleUserID: appleUserID,
			Nickname:    nickname,
			MemberLevel: "free",
		}
		if err := s.DB.Create(&user).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	token, err := utils.GenerateToken(user.UserID)
	if err != nil {
		return nil, err
	}
	return &response.LoginResp{
		UserID: user.UserID,
		Token:  token,
		IsNew:  isNew,
	}, nil
}

// GetUserInfo 获取用户信息
func (s *UserService) GetUserInfo(userID string) (*response.UserResp, error) {
	var user database.User
	if err := s.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}

	return toUserResp(user), nil
}

func (s *UserService) GetUserSettings(userID string) (*response.UserSettingsResp, error) {
	settings, err := s.getOrCreateUserSettings(userID)
	if err != nil {
		return nil, err
	}

	return toUserSettingsResp(settings), nil
}

// UpdateUserInfo 更新用户信息
func (s *UserService) UpdateUserInfo(userID string, req request.UpdateUserInfoReq) (*response.UserResp, error) {
	updates := make(map[string]interface{})
	if req.Nickname != "" {
		updates["nickname"] = req.Nickname
	}
	if req.Avatar != "" {
		updates["avatar"] = req.Avatar
	}
	if req.Language != "" {
		updates["language"] = req.Language
	}
	if req.Region != "" {
		updates["region"] = req.Region
	}
	if req.Gender != "" {
		switch req.Gender {
		case "male", "female", "unknown":
			updates["gender"] = req.Gender
		default:
			return nil, InvalidUserFieldError{Field: "gender", Reason: "unsupported_value"}
		}
	}
	if req.Age < 0 || req.Age > 150 {
		return nil, InvalidUserFieldError{Field: "age", Reason: "unsupported_value"}
	}
	if req.Age > 0 {
		updates["age"] = req.Age
	}

	if len(updates) > 0 {
		if err := s.DB.Model(&database.User{}).Where("user_id = ?", userID).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return s.GetUserInfo(userID)
}

func (s *UserService) UpdateUserSettings(userID string, req request.UpdateUserSettingsReq) (*response.UserSettingsResp, error) {
	if _, err := s.getOrCreateUserSettings(userID); err != nil {
		return nil, err
	}

	updates := make(map[string]interface{})
	if req.RelationshipMode != "" {
		switch req.RelationshipMode {
		case "romantic", "friend", "supportive":
			updates["relationship_mode"] = req.RelationshipMode
		default:
			return nil, InvalidUserFieldError{Field: "relationship_mode", Reason: "unsupported_value"}
		}
	}

	if req.ResponseStyle != "" {
		switch req.ResponseStyle {
		case "balanced", "gentle", "playful", "intense":
			updates["response_style"] = req.ResponseStyle
		default:
			return nil, InvalidUserFieldError{Field: "response_style", Reason: "unsupported_value"}
		}
	}

	if req.ResponseLength != "" {
		switch req.ResponseLength {
		case "short", "medium", "long":
			updates["response_length"] = req.ResponseLength
		default:
			return nil, InvalidUserFieldError{Field: "response_length", Reason: "unsupported_value"}
		}
	}

	if req.MemoryEnabled != nil {
		updates["memory_enabled"] = *req.MemoryEnabled
	}
	if req.VoiceAutoPlay != nil {
		updates["voice_auto_play"] = *req.VoiceAutoPlay
	}
	if req.PushEnabled != nil {
		updates["push_enabled"] = *req.PushEnabled
	}
	if req.ProactiveEnable != nil {
		updates["proactive_enable"] = *req.ProactiveEnable
	}
	if req.PrivacyMode != nil {
		updates["privacy_mode"] = *req.PrivacyMode
	}

	if len(updates) > 0 {
		if err := s.DB.Model(&database.UserSettings{}).Where("user_id = ?", userID).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	updated, err := s.getOrCreateUserSettings(userID)
	if err != nil {
		return nil, err
	}

	return toUserSettingsResp(updated), nil
}

// GetOrCreateUser 获取或创建用户（用于测试/演示）
func (s *UserService) GetOrCreateUser(phone string) (*database.User, error) {
	var user database.User
	err := s.DB.Where("phone = ?", phone).First(&user).Error

	if err == gorm.ErrRecordNotFound {
		// 创建新用户
		userID := uuid.New().String()
		if phone == "" {
			phone = "13800000000"
		}
		user = database.User{
			UserID:      userID,
			Phone:       phone,
			Nickname:    "用户" + userID[:6],
			MemberLevel: "free",
		}
		s.DB.Create(&user)
		return &user, nil
	}

	return &user, err
}

// CheckMemberStatus 检查会员状态
func (s *UserService) CheckMemberStatus(userID string) (bool, error) {
	var user database.User
	if err := s.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
		return false, err
	}

	if !isPaidMemberLevel(user.MemberLevel) {
		return false, nil
	}

	// 检查是否过期
	if user.MemberExpire != nil && user.MemberExpire.Before(time.Now()) {
		// 过期，更新状态
		s.DB.Model(&user).Update("member_level", "free")
		return false, nil
	}

	return true, nil
}

// CheckMinorStatus 检查未成年人状态
func (s *UserService) CheckMinorStatus(userID string) error {
	var user database.User
	if err := s.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 用户不存在，返回特定错误，让中间件放行
			return errors.New("record not found")
		}
		return err
	}

	if !user.IsMinor {
		return nil
	}

	// 1. 宵禁检查 (22:00 - 06:00)
	now := time.Now()
	hour := now.Hour()
	if hour >= 22 || hour < 6 {
		return errors.New("minor protection: curfew time (22:00-06:00)")
	}

	// 2. 使用时长检查 (每日限额 1小时 = 3600秒)
	if user.DailyUsageTime >= 3600 {
		return errors.New("minor protection: daily usage limit exceeded")
	}

	return nil
}

// IncrementUsageTime 增加使用时长
func (s *UserService) IncrementUsageTime(userID string, seconds int64) error {
	return s.DB.Model(&database.User{}).Where("user_id = ?", userID).
		UpdateColumn("daily_usage_time", gorm.Expr("daily_usage_time + ?", seconds)).Error
}

// RefreshToken 刷新Token
func (s *UserService) RefreshToken(refreshToken string) (*response.LoginResp, error) {
	claims, err := utils.ParseToken(refreshToken)
	if err != nil || claims.UserID == "" {
		return nil, ErrInvalidRefreshToken
	}

	var user database.User
	if err := s.DB.Where("user_id = ?", claims.UserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	token, err := utils.GenerateToken(user.UserID)
	if err != nil {
		return nil, err
	}

	return &response.LoginResp{
		UserID: user.UserID,
		Token:  token,
		IsNew:  false,
	}, nil
}

func toUserResp(user database.User) *response.UserResp {
	language := user.Language
	if language == "" {
		language = "en"
	}

	region := user.Region
	if region == "" {
		region = "US"
	}

	gender := user.Gender
	if gender == "" {
		gender = "unknown"
	}

	resp := &response.UserResp{
		UserID:      user.UserID,
		Phone:       user.Phone,
		Email:       user.Email,
		Nickname:    user.Nickname,
		Avatar:      user.Avatar,
		MemberLevel: user.MemberLevel,
		Language:    language,
		Region:      region,
		Gender:      gender,
		Age:         user.Age,
		ChatDays:    user.ChatDays,
		CharCount:   user.CharCount,
		MsgCount:    user.MsgCount,
		CreatedAt:   user.CreatedAt.Unix(),
		UpdatedAt:   user.UpdatedAt.Unix(),
	}

	if user.MemberExpire != nil {
		resp.MemberExpire = user.MemberExpire.Format("2006-01-02")
	}

	if user.LastLoginAt != nil {
		resp.LastLoginAt = user.LastLoginAt.Unix()
	}

	return resp
}

func isPaidMemberLevel(level string) bool {
	switch level {
	case "pro", "vip", "monthly", "quarterly", "yearly", "lifetime":
		return true
	default:
		return false
	}
}

func (s *UserService) getOrCreateUserSettings(userID string) (database.UserSettings, error) {
	var settings database.UserSettings
	err := s.DB.Where("user_id = ?", userID).First(&settings).Error
	if err == nil {
		return settings, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return settings, err
	}

	settings = defaultUserSettings(userID)
	if err := s.DB.Create(&settings).Error; err != nil {
		return settings, err
	}

	return settings, nil
}

func defaultUserSettings(userID string) database.UserSettings {
	return database.UserSettings{
		UserID:           userID,
		RelationshipMode: "romantic",
		ResponseStyle:    "balanced",
		ResponseLength:   "medium",
		MemoryEnabled:    true,
		VoiceAutoPlay:    false,
		PushEnabled:      true,
		ProactiveEnable:  true,
		PrivacyMode:      false,
	}
}

func toUserSettingsResp(settings database.UserSettings) *response.UserSettingsResp {
	return &response.UserSettingsResp{
		UserID:           settings.UserID,
		RelationshipMode: settings.RelationshipMode,
		ResponseStyle:    settings.ResponseStyle,
		ResponseLength:   settings.ResponseLength,
		MemoryEnabled:    settings.MemoryEnabled,
		VoiceAutoPlay:    settings.VoiceAutoPlay,
		PushEnabled:      settings.PushEnabled,
		ProactiveEnable:  settings.ProactiveEnable,
		PrivacyMode:      settings.PrivacyMode,
		UpdatedAt:        settings.UpdatedAt.Unix(),
	}
}
