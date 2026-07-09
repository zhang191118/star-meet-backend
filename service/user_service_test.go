package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/pkg/utils"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupUserServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.UserSettings{}); err != nil {
		t.Fatalf("migrate user tables: %v", err)
	}

	return db
}

func TestGetUserInfoIncludesProfileFields(t *testing.T) {
	db := setupUserServiceTestDB(t)
	now := time.Unix(1710000000, 0)

	user := database.User{
		UserID:      "user-1",
		Email:       "user@example.com",
		Nickname:    "测试用户",
		Avatar:      "https://example.com/avatar.png",
		MemberLevel: "vip",
		Language:    "zh-Hant",
		Region:      "TW",
		Gender:      "female",
		Age:         26,
		ChatDays:    12,
		CharCount:   3,
		MsgCount:    42,
		LastLoginAt: &now,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.GetUserInfo("user-1")
	if err != nil {
		t.Fatalf("get user info: %v", err)
	}

	if resp.Email != "user@example.com" {
		t.Fatalf("expected email to be returned, got %q", resp.Email)
	}
	if resp.Gender != "female" || resp.Age != 26 {
		t.Fatalf("expected gender/age to be returned, got %q/%d", resp.Gender, resp.Age)
	}
	if resp.Language != "zh-Hant" || resp.Region != "TW" {
		t.Fatalf("expected language/region to be preserved, got %q/%q", resp.Language, resp.Region)
	}
	if resp.LastLoginAt != now.Unix() {
		t.Fatalf("expected last login timestamp %d, got %d", now.Unix(), resp.LastLoginAt)
	}
}

func TestUpdateUserInfoReturnsUpdatedProfile(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{
		UserID:      "user-2",
		Nickname:    "旧昵称",
		MemberLevel: "free",
		Language:    "en",
		Region:      "US",
		Gender:      "unknown",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.UpdateUserInfo("user-2", request.UpdateUserInfoReq{
		Nickname: "新昵称",
		Avatar:   "https://example.com/new.png",
		Language: "zh-Hans",
		Region:   "CN",
		Gender:   "male",
		Age:      30,
	})
	if err != nil {
		t.Fatalf("update user info: %v", err)
	}

	if resp.Nickname != "新昵称" || resp.Gender != "male" || resp.Age != 30 {
		t.Fatalf("unexpected response payload: %+v", resp)
	}

	var stored database.User
	if err := db.Where("user_id = ?", "user-2").First(&stored).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}

	if stored.Avatar != "https://example.com/new.png" || stored.Language != "zh-Hans" || stored.Region != "CN" {
		t.Fatalf("unexpected stored user: %+v", stored)
	}
}

func TestUpdateUserInfoRejectsInvalidGender(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{UserID: "user-3", Nickname: "测试"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	if _, err := service.UpdateUserInfo("user-3", request.UpdateUserInfoReq{Gender: "robot"}); err == nil {
		t.Fatal("expected invalid gender error")
	}
}

func TestRefreshTokenRejectsInvalidToken(t *testing.T) {
	db := setupUserServiceTestDB(t)
	service := &UserService{DB: db}

	resp, err := service.RefreshToken("not-a-valid-refresh-token")
	if err == nil {
		t.Fatalf("expected invalid refresh token to fail, got response %+v", resp)
	}
	if resp != nil {
		t.Fatalf("expected no response for invalid refresh token, got %+v", resp)
	}
}

func TestResetPasswordReturnsUserNotFoundForMissingEmail(t *testing.T) {
	db := setupUserServiceTestDB(t)
	service := &VerifyCodeService{DB: db}

	err := service.ResetPassword("missing-reset@example.com", "new-secret")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestRefreshTokenIssuesTokenForValidJWTUser(t *testing.T) {
	db := setupUserServiceTestDB(t)
	if err := db.Create(&database.User{
		UserID:   "refresh-user-1",
		Email:    "refresh@example.com",
		Nickname: "刷新用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	refreshToken, err := utils.GenerateToken("refresh-user-1")
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.RefreshToken(refreshToken)
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if resp.UserID != "refresh-user-1" || resp.Token == "" || resp.IsNew {
		t.Fatalf("unexpected refresh response: %+v", resp)
	}

	claims, err := utils.ParseToken(resp.Token)
	if err != nil {
		t.Fatalf("parse refreshed token: %v", err)
	}
	if claims.UserID != "refresh-user-1" {
		t.Fatalf("expected refreshed token for user, got %q", claims.UserID)
	}
}

func TestGetUserSettingsCreatesDefaults(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{UserID: "user-settings-1", Nickname: "设置用户"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.GetUserSettings("user-settings-1")
	if err != nil {
		t.Fatalf("get user settings: %v", err)
	}

	if resp.RelationshipMode != "romantic" || !resp.MemoryEnabled || !resp.PushEnabled {
		t.Fatalf("unexpected default settings: %+v", resp)
	}
}

func TestUpdateUserSettingsPersistsPreferences(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{UserID: "user-settings-2", Nickname: "设置用户2"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	memoryEnabled := false
	voiceAutoPlay := true
	pushEnabled := false
	proactiveEnable := false
	privacyMode := true

	resp, err := service.UpdateUserSettings("user-settings-2", request.UpdateUserSettingsReq{
		RelationshipMode: "supportive",
		ResponseStyle:    "gentle",
		ResponseLength:   "short",
		MemoryEnabled:    &memoryEnabled,
		VoiceAutoPlay:    &voiceAutoPlay,
		PushEnabled:      &pushEnabled,
		ProactiveEnable:  &proactiveEnable,
		PrivacyMode:      &privacyMode,
	})
	if err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	if resp.RelationshipMode != "supportive" || resp.ResponseStyle != "gentle" || resp.ResponseLength != "short" {
		t.Fatalf("unexpected updated settings: %+v", resp)
	}
	if resp.MemoryEnabled || !resp.VoiceAutoPlay || resp.PushEnabled || resp.ProactiveEnable || !resp.PrivacyMode {
		t.Fatalf("unexpected toggle values: %+v", resp)
	}
}

func TestCheckMemberStatusSupportsPaidLevels(t *testing.T) {
	db := setupUserServiceTestDB(t)
	expireAt := time.Now().Add(24 * time.Hour)

	user := database.User{
		UserID:       "member-1",
		Nickname:     "会员用户",
		MemberLevel:  "monthly",
		MemberExpire: &expireAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	isMember, err := service.CheckMemberStatus("member-1")
	if err != nil {
		t.Fatalf("check member status: %v", err)
	}
	if !isMember {
		t.Fatal("expected monthly member to be treated as paid member")
	}
}

func TestCheckMemberStatusExpiresPaidLevels(t *testing.T) {
	db := setupUserServiceTestDB(t)
	expireAt := time.Now().Add(-24 * time.Hour)

	user := database.User{
		UserID:       "member-2",
		Nickname:     "过期会员",
		MemberLevel:  "yearly",
		MemberExpire: &expireAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	isMember, err := service.CheckMemberStatus("member-2")
	if err != nil {
		t.Fatalf("check member status: %v", err)
	}
	if isMember {
		t.Fatal("expected expired yearly member to be downgraded")
	}

	var stored database.User
	if err := db.Where("user_id = ?", "member-2").First(&stored).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.MemberLevel != "free" {
		t.Fatalf("expected member level to downgrade to free, got %q", stored.MemberLevel)
	}
}
