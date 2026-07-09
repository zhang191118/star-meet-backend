package middleware

import (
	"net/http"
	"strings"

	"star-meet-backend/model/response"
	"star-meet-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// TokenClaims is kept as a compatibility alias for tests and older callers.
type TokenClaims = utils.Claims

// Auth 认证中间件
func Auth() gin.HandlerFunc {
	return authMiddleware()
}

func authMiddleware() gin.HandlerFunc {
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

		// 将用户ID存入上下文
		c.Set("user_id", claims.UserID)
		c.Next()
	}
}
