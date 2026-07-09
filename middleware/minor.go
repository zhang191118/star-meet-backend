package middleware

import (
	"net/http"
	"star-meet-backend/global"
	"star-meet-backend/model/response"
	"strings"

	"github.com/gin-gonic/gin"
)

// MinorProtection 未成年人保护中间件
// Checks for curfew (22:00-06:00) and daily usage limits for minor users
func MinorProtection() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 尝试从多个来源获取 user_id
		// 1. 从上下文 (如果之前的中间件设置了)
		userID := c.GetString("user_id")

		// 2. 从 Query 参数
		if userID == "" {
			userID = c.Query("user_id")
		}

		// 3. 从 Form 参数
		if userID == "" {
			userID = c.PostForm("user_id")
		}

		// 如果找到了 userID，进行检查
		if userID != "" {
			err := global.UserService.CheckMinorStatus(userID)
			if err != nil {
				// 如果是"用户不存在"的错误，只记录日志但放行（允许新用户使用）
				if strings.Contains(err.Error(), "record not found") ||
					strings.Contains(err.Error(), "record not found") {
					// 用户不存在，放行
					c.Next()
					return
				}
				// 真正的未成年人限制才阻止
				response.Fail(c, http.StatusForbidden, "未成年人保护模式: "+err.Error())
				c.Abort()
				return
			}
		}

		c.Next()
	}
}
