package service

import (
	"errors"
	"fmt"
	"log"
	"star-meet-backend/core/mem0"
	"star-meet-backend/core/qdrant"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/pkg/utils"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrInvalidRefreshToken     = errors.New("invalid refresh token")
	ErrUserNotFound            = errors.New("user not found")
	ErrInvalidCredentials      = errors.New("invalid credentials")
	ErrEmailRegistered         = errors.New("email already registered")
	ErrInvalidVerifyCode       = errors.New("invalid verify code")
	ErrVerifyCodeExpired       = errors.New("verify code expired")
	ErrVerifyCodeLimitExceeded = errors.New("verify code daily limit exceeded")
	ErrInvalidUserField        = errors.New("invalid user field")
	ErrThirdPartyUnavailable   = errors.New("third-party login is not configured")
	ErrDatabaseOperation       = errors.New("database operation failed")
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
	Notification      *NotificationService
	Mem0Client        *mem0.Client
	QdrantClient      *qdrant.Client
}

func newLoginResponse(userID string, isNew bool) (*response.LoginResp, error) {
	accessToken, err := utils.GenerateAccessToken(userID)
	if err != nil {
		return nil, err
	}
	refreshToken, err := utils.GenerateRefreshToken(userID)
	if err != nil {
		return nil, err
	}
	return &response.LoginResp{UserID: userID, Token: accessToken, RefreshToken: refreshToken, IsNew: isNew}, nil
}

// Register 用户注册
func (s *UserService) Register(req request.RegisterReq) (*response.LoginResp, error) {
	// 检查邮箱是否已存在
	var count int64
	s.DB.Model(&database.User{}).Where("email = ?", req.Email).Count(&count)
	if count > 0 {
		return nil, ErrEmailRegistered
	}

	// 注册必须通过邮箱验证码校验（验证码由 send-verify-code 下发到邮箱）
	if s.VerifyCodeService != nil {
		code := strings.TrimSpace(req.Code)
		if code == "" {
			return nil, ErrInvalidVerifyCode
		}
		if err := s.VerifyCodeService.VerifyCode(req.Email, code, "register"); err != nil {
			return nil, err
		}
	}

	// 加密密码（未提供时自动生成随机密码）
	password := req.Password
	if password == "" {
		password = uuid.New().String()
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
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
		gender = "female" // 注册不再要求选择性别，缺省按女性用户建档
	}
	if gender != "male" && gender != "female" {
		return nil, InvalidUserFieldError{Field: "gender", Reason: "unsupported_value"}
	}
	if req.Age < 18 || req.Age > 150 {
		return nil, InvalidUserFieldError{Field: "age", Reason: "unsupported_value"}
	}

	// 生成默认昵称
	nickname := strings.TrimSpace(req.Nickname)
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
		Age:         req.Age,
	}

	if err := s.DB.Create(&user).Error; err != nil {
		return nil, err
	}

	if s.Notification != nil {
		s.Notification.NotifyWelcome(userID, i18n.NormalizeLang(language))
	}

	return newLoginResponse(userID, true)
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

	return newLoginResponse(user.UserID, isNew)
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

	return newLoginResponse(user.UserID, false)
}

// WechatLogin 微信登录
func (s *UserService) WechatLogin(req request.WechatLoginReq) (*response.LoginResp, error) {
	openID, err := exchangeWechatCode(req.Code)
	if err != nil {
		return nil, err
	}

	var user database.User
	err = s.DB.Where("wechat_openid = ?", openID).First(&user).Error

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
		if err := createThirdPartyUser(s.DB, &user); err != nil {
			log.Printf("[wechat-login] user creation failed openid=%s error=%v", openID, err)
			return nil, fmt.Errorf("%w: %v", ErrDatabaseOperation, err)
		}
	} else if err != nil {
		log.Printf("[wechat-login] user lookup failed openid=%s error=%v", openID, err)
		return nil, fmt.Errorf("%w: %v", ErrDatabaseOperation, err)
	}

	return newLoginResponse(user.UserID, isNew)
}

// AppleLogin 苹果登录
func (s *UserService) AppleLogin(req request.AppleLoginReq) (*response.LoginResp, error) {
	appleUserID, err := validateAppleIdentityToken(req.IdentityToken, req.UserID, req.Nonce)
	if err != nil {
		return nil, err
	}

	var user database.User
	err = s.DB.Where("apple_user_id = ?", appleUserID).First(&user).Error

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
		if err := createThirdPartyUser(s.DB, &user); err != nil {
			log.Printf("[apple-login] user creation failed apple_uid=%s error=%v", appleUserID, err)
			return nil, fmt.Errorf("%w: %v", ErrDatabaseOperation, err)
		}
	} else if err != nil {
		log.Printf("[apple-login] user lookup failed apple_uid=%s error=%v", appleUserID, err)
		return nil, fmt.Errorf("%w: %v", ErrDatabaseOperation, err)
	}

	return newLoginResponse(user.UserID, isNew)
}

// LineLogin LINE 登录：验证 ID Token，按 line_user_id 建号或关联已有账号
func (s *UserService) LineLogin(req request.LineLoginReq) (*response.LoginResp, error) {
	identity, err := validateLineIDToken(req.IDToken, req.UserID)
	if err != nil {
		return nil, err
	}

	var user database.User
	err = s.DB.Where("line_user_id = ?", identity.UserID).First(&user).Error

	isNew := false
	if err == gorm.ErrRecordNotFound {
		isNew = true
		userID := uuid.New().String()
		nickname := strings.TrimSpace(req.Nickname)
		if nickname == "" {
			nickname = strings.TrimSpace(identity.Name)
		}
		if nickname == "" {
			nickname = "LINE用户_" + userID[:4]
		}
		avatar := strings.TrimSpace(req.Picture)
		if avatar == "" {
			avatar = strings.TrimSpace(identity.Picture)
		}
		user = database.User{
			UserID:      userID,
			LineUserID:  identity.UserID,
			Nickname:    nickname,
			Avatar:      avatar,
			MemberLevel: "free",
		}
		if err := createThirdPartyUser(s.DB, &user); err != nil {
			log.Printf("[line-login] user creation failed line_uid=%s error=%v", identity.UserID, err)
			return nil, fmt.Errorf("%w: %v", ErrDatabaseOperation, err)
		}
	} else if err != nil {
		log.Printf("[line-login] user lookup failed line_uid=%s error=%v", identity.UserID, err)
		return nil, fmt.Errorf("%w: %v", ErrDatabaseOperation, err)
	}

	return newLoginResponse(user.UserID, isNew)
}

// Third-party identity providers do not always return an email address. Omitting
// the column stores NULL, which remains compatible with the unique email index;
// inserting Go's zero value ("") would allow only one third-party account.
func createThirdPartyUser(db *gorm.DB, user *database.User) error {
	return db.Omit("Email").Create(user).Error
}

// DeleteAccount permanently removes user-owned data. It is intentionally a hard
// delete so a user request does not leave personally identifiable data behind.
func (s *UserService) DeleteAccount(userID string) error {
	unlockMemory := lockMemoryUser(userID)
	defer unlockMemory()
	if strings.TrimSpace(userID) == "" {
		return ErrUserNotFound
	}
	if err := s.deleteExternalMemories(userID); err != nil {
		return err
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireExistingMemoryUser(tx, userID); err != nil {
			return err
		}
		var sessionIDs []string
		if err := tx.Model(&database.Session{}).Where("user_id = ?", userID).Pluck("session_id", &sessionIDs).Error; err != nil {
			return err
		}

		if len(sessionIDs) > 0 {
			if err := tx.Unscoped().Where("session_id IN ?", sessionIDs).Delete(&database.Message{}).Error; err != nil {
				return err
			}
		}
		for _, item := range []interface{}{
			&database.Message{}, &database.Session{}, &database.Memory{}, &database.Favorite{},
			&database.UserCharacter{}, &database.UserCurrency{}, &database.CurrencyTransaction{},
			&database.UserGachaDraw{}, &database.UserGachaRecord{}, &database.Subscription{},
			&database.AppleTransactionOwner{},
			&database.UserSettings{}, &database.Notification{}, &database.PushDevice{}, &database.UserAffection{},
		} {
			if err := tx.Unscoped().Where("user_id = ?", userID).Delete(item).Error; err != nil {
				return err
			}
		}
		for _, model := range []interface{}{&database.MemoryIndexJob{}, &database.Feedback{}, &database.ProductEvent{}} {
			if tx.Migrator().HasTable(model) {
				if err := tx.Unscoped().Where("user_id = ?", userID).Delete(model).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&database.Character{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("reporter_id = ?", userID).Delete(&database.Report{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("blocker_id = ? OR blocked_id = ?", userID, userID).Delete(&database.Block{}).Error; err != nil {
			return err
		}

		result := tx.Unscoped().Where("user_id = ?", userID).Delete(&database.User{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrUserNotFound
		}
		return nil
	})
}

const externalMemoryDeletePageSize = 100

// deleteExternalMemories runs before the database transaction. Returning an
// error is deliberate: the account must not be reported as deleted while a
// third-party memory store still retains the user's chat data. Repeating the
// operation is safe because both backends accept deleting an empty result set.
func (s *UserService) deleteExternalMemories(userID string) error {
	if s.Mem0Client != nil && s.Mem0Client.Configured() {
		mem0UserID := "user_" + userID
		for page := 0; ; page++ {
			// Always fetch offset zero. Deleting the current page shifts the next
			// records forward; advancing the offset would skip those records.
			memories, err := s.Mem0Client.GetAll(mem0UserID, "", externalMemoryDeletePageSize, 0)
			if err != nil {
				return fmt.Errorf("delete account Mem0 cleanup: %w", err)
			}
			if len(memories) == 0 {
				break
			}
			for _, memory := range memories {
				if strings.TrimSpace(memory.ID) == "" {
					return errors.New("delete account Mem0 cleanup: memory id is empty")
				}
				if err := s.Mem0Client.Delete(memory.ID); err != nil {
					return fmt.Errorf("delete account Mem0 cleanup: %w", err)
				}
			}
			// Guard against a broken upstream that acknowledges deletes but keeps
			// returning the same records forever.
			if page >= 9999 {
				return errors.New("delete account Mem0 cleanup: pagination did not converge")
			}
		}
	}

	if s.QdrantClient != nil {
		filter := map[string]interface{}{
			"must": []map[string]interface{}{
				{"key": "user_id", "match": map[string]interface{}{"value": userID}},
			},
		}
		if err := s.QdrantClient.DeleteByFilter("memories", filter); err != nil {
			return fmt.Errorf("delete account Qdrant cleanup: %w", err)
		}
	}

	return nil
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
	if req.Age != 0 && (req.Age < 18 || req.Age > 150) {
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
	if req.ProactiveQuietStart != nil {
		if _, ok := parseClockMinutes(*req.ProactiveQuietStart); !ok {
			return nil, InvalidUserFieldError{Field: "proactive_quiet_start", Reason: "invalid_time"}
		}
		updates["proactive_quiet_start"] = *req.ProactiveQuietStart
	}
	if req.ProactiveQuietEnd != nil {
		if _, ok := parseClockMinutes(*req.ProactiveQuietEnd); !ok {
			return nil, InvalidUserFieldError{Field: "proactive_quiet_end", Reason: "invalid_time"}
		}
		updates["proactive_quiet_end"] = *req.ProactiveQuietEnd
	}
	if req.ProactiveDailyLimit != nil {
		if *req.ProactiveDailyLimit < 0 || *req.ProactiveDailyLimit > 3 {
			return nil, InvalidUserFieldError{Field: "proactive_daily_limit", Reason: "out_of_range"}
		}
		updates["proactive_daily_limit"] = *req.ProactiveDailyLimit
	}
	if req.ProactiveOnlyWhenAway != nil {
		updates["proactive_only_when_away"] = *req.ProactiveOnlyWhenAway
	}
	if req.ProactivePreviewMode != nil {
		switch *req.ProactivePreviewMode {
		case "full", "name_only", "hidden":
		default:
			return nil, InvalidUserFieldError{Field: "proactive_preview_mode", Reason: "unsupported_value"}
		}
		updates["proactive_preview_mode"] = *req.ProactivePreviewMode
	}
	if req.PromiseReminderEnabled != nil {
		updates["promise_reminder_enabled"] = *req.PromiseReminderEnabled
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
	if err != nil || claims.UserID == "" || claims.TokenType != "refresh" {
		return nil, ErrInvalidRefreshToken
	}

	var user database.User
	if err := s.DB.Where("user_id = ?", claims.UserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return newLoginResponse(user.UserID, false)
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
	case "pro", "vip", "weekly", "monthly", "quarterly", "yearly", "lifetime":
		return true
	default:
		return false
	}
}

func userHasActiveMembershipAt(user database.User, now time.Time) bool {
	if !isPaidMemberLevel(user.MemberLevel) {
		return false
	}
	if user.MemberExpire != nil && user.MemberExpire.Before(now) {
		return false
	}
	return true
}

func (s *UserService) getOrCreateUserSettings(userID string) (database.UserSettings, error) {
	var settings database.UserSettings
	err := s.DB.Where("user_id = ?", userID).First(&settings).Error
	if err == nil {
		if normalizeProactiveUserSettings(&settings) {
			if err := s.DB.Save(&settings).Error; err != nil {
				return settings, err
			}
		}
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
		UserID:                 userID,
		RelationshipMode:       "romantic",
		ResponseStyle:          "balanced",
		ResponseLength:         "medium",
		MemoryEnabled:          true,
		VoiceAutoPlay:          false,
		PushEnabled:            true,
		ProactiveEnable:        true,
		ProactiveQuietStart:    "22:30",
		ProactiveQuietEnd:      "08:00",
		ProactiveDailyLimit:    1,
		ProactiveOnlyWhenAway:  true,
		ProactivePreviewMode:   "name_only",
		PromiseReminderEnabled: true,
		PrivacyMode:            false,
	}
}

func toUserSettingsResp(settings database.UserSettings) *response.UserSettingsResp {
	return &response.UserSettingsResp{
		UserID:                 settings.UserID,
		RelationshipMode:       settings.RelationshipMode,
		ResponseStyle:          settings.ResponseStyle,
		ResponseLength:         settings.ResponseLength,
		MemoryEnabled:          settings.MemoryEnabled,
		VoiceAutoPlay:          settings.VoiceAutoPlay,
		PushEnabled:            settings.PushEnabled,
		ProactiveEnable:        settings.ProactiveEnable,
		ProactiveQuietStart:    settings.ProactiveQuietStart,
		ProactiveQuietEnd:      settings.ProactiveQuietEnd,
		ProactiveDailyLimit:    settings.ProactiveDailyLimit,
		ProactiveOnlyWhenAway:  settings.ProactiveOnlyWhenAway,
		ProactivePreviewMode:   settings.ProactivePreviewMode,
		PromiseReminderEnabled: settings.PromiseReminderEnabled,
		PrivacyMode:            settings.PrivacyMode,
		UpdatedAt:              settings.UpdatedAt.Unix(),
	}
}
