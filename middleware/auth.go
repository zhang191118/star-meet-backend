package middleware

import (
	"net/http"
	"strings"

	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TokenClaims is kept as a compatibility alias for tests and older callers.
type TokenClaims = utils.Claims

// authDB 由 main 启动时通过 SetAuthDB 注入，用于同步用户语言等轻量写入。
// 测试环境不注入时为 nil，相关逻辑自动跳过。
var authDB *gorm.DB

func SetAuthDB(db *gorm.DB) { authDB = db }

// Auth 认证中间件
func Auth() gin.HandlerFunc {
	return authMiddleware(authDB)
}

func authMiddleware(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 公开路径不需要认证
		publicPaths := []string{
			"/health",
			"/api/v1/user/login",
			"/api/v1/user/register",
			"/api/v1/user/send-verify-code",
			"/api/v1/user/reset-password",
			"/api/v1/user/verify-code",
			"/api/v1/character/public",
			"/api/v1/character/search",
			"/api/v1/gacha/pools",
			"/api/v1/subscription/plans",
		}

		// 尝试解析可选 token（公开路径也尝试提取 user_id，用于个性化标记）
		tryParseOptionalToken(c)

		for _, p := range publicPaths {
			if strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}

		// 验证Token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.FailWithError(c, http.StatusUnauthorized, "Missing Authorization header", "missing_authorization", nil)
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.FailWithError(c, http.StatusUnauthorized, "Invalid Authorization header format", "invalid_authorization_format", nil)
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims, err := utils.ParseToken(tokenString)
		if err != nil {
			response.FailWithError(c, http.StatusUnauthorized, "Invalid token", "invalid_token", nil)
			c.Abort()
			return
		}
		if claims.TokenType != "" && claims.TokenType != "access" {
			response.FailWithError(c, http.StatusUnauthorized, "Invalid access token", "invalid_access_token", nil)
			c.Abort()
			return
		}

		// 将用户ID存入上下文
		c.Set("user_id", claims.UserID)
		// 把客户端当前语言同步到用户记录：开场白等服务端生成内容据此选语言
		syncUserLanguage(db, claims.UserID, c.GetHeader("Accept-Language"))
		c.Next()
	}
}

// syncUserLanguage 将请求携带的 Accept-Language 同步到 users.language。
// 仅在与库中值不同时更新（0 行更新，代价极低），语言未变的请求无实际写入。
func syncUserLanguage(db *gorm.DB, userID, acceptLanguage string) {
	if db == nil || userID == "" || strings.TrimSpace(acceptLanguage) == "" {
		return
	}
	lang := i18n.NormalizeLang(acceptLanguage)
	db.Model(&database.User{}).
		Where("user_id = ? AND (language IS NULL OR language = '' OR language <> ?)", userID, lang).
		Update("language", lang)
}

// tryParseOptionalToken 尝试从 Authorization header 解析 token，成功则设置 user_id
// 用于公开接口：已登录用户可携带 token 获取个性化标记（如 is_encountered），未登录用户不受影响
func tryParseOptionalToken(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return
	}
	claims, err := utils.ParseToken(parts[1])
	if err != nil {
		return
	}
	if claims.UserID != "" {
		c.Set("user_id", claims.UserID)
	}
}
