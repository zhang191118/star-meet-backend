package global

import (
	"os"
	"strconv"
)

// EmailConfig 邮件配置
type EmailConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	FromName  string
	FromEmail string
}

var EmailConfigInstance *EmailConfig

// LoadEmailConfigFromEnv 从环境变量加载邮件配置
func LoadEmailConfigFromEnv() *EmailConfig {
	port, _ := strconv.Atoi(os.Getenv("EMAIL_PORT"))
	if port == 0 {
		port = 587
	}

	return &EmailConfig{
		Host:      getEnv("EMAIL_HOST", "smtp.gmail.com"),
		Port:      port,
		Username:  getEnv("EMAIL_USERNAME", ""),
		Password:  getEnv("EMAIL_PASSWORD", ""),
		FromName:  getEnv("EMAIL_FROM_NAME", "星遇 StarMeet"),
		FromEmail: getEnv("EMAIL_FROM_EMAIL", "noreply@starmeet.app"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
