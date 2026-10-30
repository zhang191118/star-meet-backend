package service

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// ============ Resend（优先） ============
// 配置来源：数据库 runtime_config 表（resend.api_key / resend.from），
// 由 main.go 注入解析器，数据库改动即时生效无需重启；
// 解析不到时回退环境变量 RESEND_API_KEY / RESEND_FROM。

const defaultResendFrom = "StarMeet <noreply@kwbb.com>"

var resendHTTPClient = &http.Client{Timeout: 15 * time.Second}

// ResendConfigResolver 每次发送时动态解析配置（数据库优先）。
var ResendConfigResolver func() (apiKey, from string)

func resolveResendConfig() (string, string) {
	if ResendConfigResolver != nil {
		apiKey, from := ResendConfigResolver()
		if strings.TrimSpace(apiKey) != "" {
			if strings.TrimSpace(from) == "" {
				from = defaultResendFrom
			}
			return apiKey, from
		}
	}
	from := strings.TrimSpace(os.Getenv("RESEND_FROM"))
	if from == "" {
		from = defaultResendFrom
	}
	return os.Getenv("RESEND_API_KEY"), from
}

func sendViaResend(to, from, apiKey, subject, html string) error {
	payload := map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := resendHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("Resend 请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, io.LimitReader(resp.Body, 512))
		return fmt.Errorf("Resend 返回 %d: %s", resp.StatusCode, buf.String())
	}
	return nil
}

// ============ 验证码邮件（文案随用户语言） ============

type verifyEmailCopy struct {
	subject  string // 邮件标题
	intro    string // 正文引导语
	expiry   string // 有效期说明
	footer   string // 落款
}

func verifyCopyFor(lang, emailType string) verifyEmailCopy {
	isReset := emailType == "reset-password"

	switch lang {
	case "zh-Hant":
		if isReset {
			return verifyEmailCopy{
				subject: "【StarMeet】重置密碼驗證碼",
				intro:   "你正在重置密碼，以下是你的驗證碼：",
				expiry:  "驗證碼 15 分鐘內有效。如果不是你本人操作，請忽略這封郵件。",
				footer:  "StarMeet 團隊",
			}
		}
		return verifyEmailCopy{
			subject: "【StarMeet】註冊驗證碼",
			intro:   "歡迎來到 StarMeet！以下是你的註冊驗證碼：",
			expiry:  "驗證碼 15 分鐘內有效。如果不是你本人操作，請忽略這封郵件。",
			footer:  "StarMeet 團隊",
		}
	case "en":
		if isReset {
			return verifyEmailCopy{
				subject: "[StarMeet] Password reset code",
				intro:   "You are resetting your password. Here is your verification code:",
				expiry:  "This code expires in 15 minutes. If you didn't request this, you can ignore this email.",
				footer:  "The StarMeet Team",
			}
		}
		return verifyEmailCopy{
			subject: "[StarMeet] Sign-up verification code",
			intro:   "Welcome to StarMeet! Here is your verification code:",
			expiry:  "This code expires in 15 minutes. If you didn't request this, you can ignore this email.",
			footer:  "The StarMeet Team",
		}
	default: // zh-Hans 及未覆盖语言回退简中
		if isReset {
			return verifyEmailCopy{
				subject: "【StarMeet】重置密码验证码",
				intro:   "你正在重置密码，以下是你的验证码：",
				expiry:  "验证码 15 分钟内有效。如果不是你本人操作，请忽略这封邮件。",
				footer:  "StarMeet 团队",
			}
		}
		return verifyEmailCopy{
			subject: "【StarMeet】注册验证码",
			intro:   "欢迎来到 StarMeet！以下是你的注册验证码：",
			expiry:  "验证码 15 分钟内有效。如果不是你本人操作，请忽略这封邮件。",
			footer:  "StarMeet 团队",
		}
	}
}

func verifyCodeHTML(code string, copy verifyEmailCopy) string {
	return fmt.Sprintf(`<!doctype html>
<html>
<body style="margin:0;padding:24px;background:#FFF0F5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'PingFang SC','Microsoft YaHei',sans-serif;">
  <div style="max-width:480px;margin:0 auto;background:#ffffff;border-radius:16px;padding:36px 32px;">
    <p style="margin:0 0 8px;font-size:16px;font-weight:bold;color:#333333;">StarMeet</p>
    <p style="margin:0 0 20px;font-size:14px;color:#555555;line-height:1.6;">%s</p>
    <div style="font-size:34px;font-weight:bold;letter-spacing:10px;color:#FF6B8A;text-align:center;padding:18px 0;background:#FFF0F5;border-radius:12px;margin:0 0 20px;">%s</div>
    <p style="margin:0;font-size:12px;color:#999999;line-height:1.6;">%s</p>
    <p style="margin:28px 0 0;font-size:12px;color:#bbbbbb;">%s</p>
  </div>
</body>
</html>`, copy.intro, code, copy.expiry, copy.footer)
}

func SendVerifyCodeEmail(toEmail, code, emailType, lang string) error {
	// 优先 Resend（配置从数据库读取，每次发送时解析）
	if apiKey, from := resolveResendConfig(); apiKey != "" {
		copy := verifyCopyFor(lang, emailType)
		if err := sendViaResend(toEmail, from, apiKey, copy.subject, verifyCodeHTML(code, copy)); err != nil {
			return err
		}
		fmt.Printf("[邮件发送成功/Resend] 发送到: %s 类型: %s 语言: %s\n", toEmail, emailType, lang)
		return nil
	}

	// 回退：传统 SMTP（旧配置）
	if emailConfig == nil {
		LoadEmailConfig()
	}
	if emailConfig.Username == "" {
		fmt.Printf("[邮件未配置] 验证码: %s (收件人: %s)\n", code, toEmail)
		return nil
	}

	var subject, body string
	switch emailType {
	case "register":
		subject = "【StarMeet】注册验证码"
		body = fmt.Sprintf(`您的注册验证码是: %s，15分钟内有效`, code)
	case "reset-password":
		subject = "【StarMeet】重置密码验证码"
		body = fmt.Sprintf(`您的重置密码验证码是: %s，15分钟内有效`, code)
	default:
		subject = "【StarMeet】验证码"
		body = fmt.Sprintf("您的验证码是: %s", code)
	}

	if strings.Contains(emailConfig.Host, "163.com") || strings.Contains(emailConfig.Host, "qq.com") {
		return sendEmailSSL(toEmail, subject, body)
	}
	return sendEmailTLS(toEmail, subject, body)
}

// ============ SMTP（保留作为无 Resend 时的兜底） ============

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
