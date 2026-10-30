package service

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"star-meet-backend/model/database"

	"github.com/go-redis/redis/v8"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type VerifyCodeService struct {
	DB    *gorm.DB
	Redis *redis.Client
}

// 同一邮箱每日验证码邮件上限（邮件服务按量计费/限额，防恶意刷量）
const verifyCodeDailyLimitPerEmail = 5

// SendVerifyCode 发送验证码。lang 用于按用户语言生成邮件文案。
func (s *VerifyCodeService) SendVerifyCode(email, codeType, lang string) error {
	// 检查邮箱是否已注册
	var count int64
	s.DB.Model(&database.User{}).Where("email = ?", email).Count(&count)

	if codeType == "register" && count > 0 {
		return ErrEmailRegistered
	}
	if codeType == "reset-password" && count == 0 {
		return ErrUserNotFound
	}

	// 防刷配额：同一邮箱每天最多发 5 封验证码邮件（邮件服务有发送限额）
	ctx := context.Background()
	limitKey := fmt.Sprintf("verify_code_limit:%s", email)
	sentToday, err := s.Redis.Incr(ctx, limitKey).Result()
	if err != nil {
		return err
	}
	if sentToday == 1 {
		s.Redis.Expire(ctx, limitKey, 24*time.Hour)
	}
	if sentToday > verifyCodeDailyLimitPerEmail {
		return ErrVerifyCodeLimitExceeded
	}

	// 生成6位验证码
	code := fmt.Sprintf("%06d", rand.Intn(1000000))

	// 存储到 Redis，设置过期时间15分钟
	key := fmt.Sprintf("verify_code:%s:%s", codeType, email)
	if err := s.Redis.Set(ctx, key, code, 15*time.Minute).Err(); err != nil {
		return err
	}

	// 发送邮件：失败时返回错误，让客户端提示重试，避免用户干等不会到达的邮件
	if err := SendVerifyCodeEmail(email, code, codeType, lang); err != nil {
		fmt.Printf("[验证码] 邮箱: %s, 类型: %s 邮件发送失败: %v\n", email, codeType, err)
		return err
	}
	fmt.Printf("[验证码] 邮箱: %s, 类型: %s 已发送（语言: %s）\n", email, codeType, lang)
	return nil
}

// VerifyCode 验证验证码
func (s *VerifyCodeService) VerifyCode(email, code, codeType string) error {
	ctx := context.Background()
	key := fmt.Sprintf("verify_code:%s:%s", codeType, email)
	storedCode, err := s.Redis.Get(ctx, key).Result()
	if err != nil {
		return ErrVerifyCodeExpired
	}

	if storedCode != code {
		return ErrInvalidVerifyCode
	}

	// 验证成功后删除验证码
	s.Redis.Del(ctx, key)

	return nil
}

// ResetPassword 重置密码
func (s *VerifyCodeService) ResetPassword(email, newPassword string) error {
	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 更新密码
	result := s.DB.Model(&database.User{}).Where("email = ?", email).Update("password", string(hashedPassword))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}
