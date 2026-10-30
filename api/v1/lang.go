package v1

import (
	"star-meet-backend/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// resolveLang extracts the user's language preference from the request context.
// For authenticated requests it reads the user's language from the database (cached in context by middleware).
// For public requests it falls back to the Accept-Language header.
// If neither is available it defaults to "en".
func resolveLang(c *gin.Context) string {
	// Try Accept-Language header first - it's always sent by the iOS client
	if al := c.GetHeader("Accept-Language"); al != "" {
		return i18n.NormalizeLang(al)
	}
	// Fallback to default
	return "en"
}

// resolveProfileLang picks the locale for character backstories and profiles.
// 用户显式选择的 App 语言（Accept-Language）优先；客户端地区只在没有
// 语言头时兜底——地区反映的是设备区域，不等于用户选择的内容语言。
func resolveProfileLang(c *gin.Context) string {
	if al := c.GetHeader("Accept-Language"); al != "" {
		return i18n.NormalizeLang(al)
	}
	switch c.GetHeader("X-Client-Region") {
	case "TW", "HK", "MO":
		return "zh-Hant"
	case "CN":
		return "zh-Hans"
	case "US", "CA", "AU", "GB", "NZ":
		return "en"
	default:
		return "en"
	}
}

// resolveLangWithUser attempts to resolve language from the user's DB record.
// For authenticated requests, the middleware may have stored user info in context.
// This is used when we want the user's persisted language preference.
func resolveLangWithUser(c *gin.Context, userID string) string {
	// Accept-Language header is the primary source - the iOS client always sends it
	if al := c.GetHeader("Accept-Language"); al != "" {
		return i18n.NormalizeLang(al)
	}
	return "en"
}
