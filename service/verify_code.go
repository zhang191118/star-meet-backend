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

// SendVerifyCode 发送验证码
func (s *VerifyCodeService) SendVerifyCode(email, codeType string) error {
	// 检查邮箱是否已注册
	var count int64
	s.DB.Model(&database.User{}).Where("email = ?", email).Count(&count)

	if codeType == "register" && count > 0 {
		return ErrEmailRegistered
	}
	if codeType == "reset-password" && count == 0 {
		return ErrUserNotFound
	}

	// 生成6位验证码
	code := fmt.Sprintf("%06d", rand.Intn(1000000))

	// 存储到 Redis，设置过期时间15分钟
	ctx := context.Background()
	key := fmt.Sprintf("verify_code:%s:%s", codeType, email)
	if err := s.Redis.Set(ctx, key, code, 15*time.Minute).Err(); err != nil {
		return err
	}

	// 发送邮件
	err := SendVerifyCodeEmail(email, code, codeType)
	if err != nil {
		fmt.Printf("[验证码] 邮箱: %s, 验证码: %s, 类型: %s (邮件发送失败: %v)\n", email, code, codeType, err)
	} else {
		fmt.Printf("[验证码] 邮箱: %s, 验证码: %s, 类型: %s\n", email, code, codeType)
	}

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
