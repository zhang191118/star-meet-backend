package middleware

import (
	"github.com/gin-gonic/gin"
	"star-meet-backend/model/response"
	"strconv"
	"sync"
	"time"
)

// SubmissionRateLimiter bounds public write endpoints and the in-memory client map.
func SubmissionRateLimiter(limit int, window time.Duration) gin.HandlerFunc {
	type entry struct {
		Count int
		Reset time.Time
	}
	var mu sync.Mutex
	clients := map[string]entry{}
	return func(c *gin.Context) {
		key := c.ClientIP()
		now := time.Now()
		mu.Lock()
		if len(clients) >= 4096 {
			for ip, item := range clients {
				if !item.Reset.After(now) {
					delete(clients, ip)
				}
			}
		}
		item, exists := clients[key]
		if !exists && len(clients) >= 4096 {
			mu.Unlock()
			response.FailWithRetryAfter(c, 429, "Please try later", "rate_limited", 60)
			c.Abort()
			return
		}
		if !exists || !item.Reset.After(now) {
			item = entry{Reset: now.Add(window)}
		}
		item.Count++
		clients[key] = item
		mu.Unlock()
		if item.Count > limit {
			retry := int64(time.Until(item.Reset).Seconds()) + 1
			c.Header("Retry-After", strconv.FormatInt(retry, 10))
			response.FailWithRetryAfter(c, 429, "Please try later", "rate_limited", retry)
			c.Abort()
			return
		}
		c.Next()
	}
}
