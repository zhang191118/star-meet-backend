package service

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
	"strings"
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

var emailConfig *EmailConfig

func SetEmailConfig(config *EmailConfig) {
	emailConfig = config
	if emailConfig != nil && strings.Contains(emailConfig.Host, "163.com") && emailConfig.Port == 587 {
		emailConfig.Port = 465
	}
}

func LoadEmailConfig() {
	emailConfig = &EmailConfig{
		Host:      getEnv("EMAIL_HOST", "smtp.gmail.com"),
		Port:      587,
		Username:  getEnv("EMAIL_USERNAME", ""),
		Password:  getEnv("EMAIL_PASSWORD", ""),
		FromName:  getEnv("EMAIL_FROM_NAME", "星遇 StarMeet"),
		FromEmail: getEnv("EMAIL_FROM_EMAIL", "noreply@starmeet.app"),
	}
	if strings.Contains(emailConfig.Host, "163.com") {
		emailConfig.Port = 465
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func SendVerifyCodeEmail(toEmail, code, emailType string) error {
	if emailConfig == nil {
		LoadEmailConfig()
	}
	if emailConfig.Username == "" {
		fmt.Printf("[邮件未配置] 验证码: %s\n", code)
		return nil
	}

	var subject, body string
	switch emailType {
	case "register":
		subject = "【星遇】注册验证码"
		body = fmt.Sprintf(`您的注册验证码是: %s，15分钟内有效`, code)
	case "reset-password":
		subject = "【星遇】重置密码验证码"
		body = fmt.Sprintf(`您的重置密码验证码是: %s，15分钟内有效`, code)
	default:
		subject = "【星遇】验证码"
		body = fmt.Sprintf("您的验证码是: %s", code)
	}

	if strings.Contains(emailConfig.Host, "163.com") || strings.Contains(emailConfig.Host, "qq.com") {
		return sendEmailSSL(toEmail, subject, body)
	}
	return sendEmailTLS(toEmail, subject, body)
}

func sendEmailSSL(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", emailConfig.Host, emailConfig.Port)
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: emailConfig.Host, InsecureSkipVerify: false})
	if err != nil {
		return fmt.Errorf("TLS连接失败: %v", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, emailConfig.Host)
	if err != nil {
		return fmt.Errorf("创建客户端失败: %v", err)
	}
	defer client.Quit()

	auth := smtp.PlainAuth("", emailConfig.Username, emailConfig.Password, emailConfig.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("认证失败: %v", err)
	}
	if err := client.Mail(emailConfig.Username); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		emailConfig.FromName, emailConfig.FromEmail, to, subject, body)
	w.Write([]byte(msg))
	w.Close()

	fmt.Printf("[邮件发送成功] 发送到: %s\n", to)
	return nil
}

func sendEmailTLS(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", emailConfig.Host, emailConfig.Port)
	auth := smtp.PlainAuth("", emailConfig.Username, emailConfig.Password, strings.Split(addr, ":")[0])
	msg := fmt.Sprintf("From: %s <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		emailConfig.FromName, emailConfig.FromEmail, to, subject, body)
	err := smtp.SendMail(addr, auth, emailConfig.Username, []string{to}, []byte(msg))
	if err != nil {
		return err
	}
	fmt.Printf("[邮件发送成功] 发送到: %s\n", to)
	return nil
}
