package middleware

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"star-meet-backend/model/response"
	"star-meet-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// RequestIDKey 请求ID键
const RequestIDKey = "request_id"

// RequestID 请求ID中间件
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if !isValidRequestID(requestID) {
			requestID = generateRequestID()
		}
		c.Set(RequestIDKey, requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func isValidRequestID(requestID string) bool {
	if len(requestID) == 0 || len(requestID) > 128 {
		return false
	}
	for _, ch := range requestID {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-' || ch == '_' || ch == '.' || ch == ':':
		default:
			return false
		}
	}
	return true
}

// generateRequestID 生成请求ID
func generateRequestID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), randomString(8))
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		requestID := c.GetString(RequestIDKey)

		logger.Infof("[%s] %s %s %d %v %s",
			requestID, method, path, status, latency, c.ClientIP())
	}
}

// Recovery 恢复中间件（带日志）
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				logger.Errorf("Panic recovered: %v", err)
				response.Fail(c, http.StatusInternalServerError, "服务器内部错误")
				c.Abort()
			}
		}()
		c.Next()
	}
}

// CORS 跨域中间件（生产环境优化版）
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		isAllowed := false
		for _, allowed := range allowedCORSOrigins() {
			if origin == allowed || isStarMeetSubdomain(origin) {
				isAllowed = true
				break
			}
		}

		if isAllowed {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Request-ID")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID, X-Total-Count, Retry-After")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func allowedCORSOrigins() []string {
	allowedOrigins := []string{
		"http://localhost:3000",
		"http://localhost:8080",
		"https://starmeet.com",
		"https://www.starmeet.com",
	}

	for _, origin := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowedOrigins = append(allowedOrigins, origin)
		}
	}

	return allowedOrigins
}

func isStarMeetSubdomain(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "starmeet.com" || strings.HasSuffix(host, ".starmeet.com")
}

// RateLimiter 限流中间件
func RateLimiter() gin.HandlerFunc {
	// 简单实现：每个IP每分钟100次请求
	type counter struct {
		count   int
		resetAt time.Time
	}

	clients := make(map[string]*counter)
	var mu sync.Mutex

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		mu.Lock()
		cl, exists := clients[ip]

		if !exists {
			clients[ip] = &counter{count: 1, resetAt: now.Add(time.Minute)}
			mu.Unlock()
			c.Next()
			return
		}

		if now.After(cl.resetAt) {
			cl.count = 1
			cl.resetAt = now.Add(time.Minute)
			mu.Unlock()
			c.Next()
			return
		}

		cl.count++
		if cl.count > 100 {
			retryAfter := int64(time.Until(cl.resetAt).Seconds())
			if retryAfter <= 0 {
				retryAfter = 1
			}
			mu.Unlock()
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			response.FailWithRetryAfter(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试", "rate_limited", retryAfter)
			c.Abort()
			return
		}

		mu.Unlock()
		c.Next()
	}
}

const (
	chatGenerationWindow         = time.Minute
	chatGenerationRequestsPerMin = 30
	chatGenerationMinInterval    = 2 * time.Second
)

type chatGenerationCounter struct {
	count       int
	resetAt     time.Time
	lastRequest time.Time
}

// ChatGenerationRateLimiter protects model-backed chat endpoints per account.
// It intentionally has no daily quota: paid users can keep chatting normally,
// while accidental duplicate requests and automated bursts are rejected.
func ChatGenerationRateLimiter() gin.HandlerFunc {
	return newChatGenerationRateLimiter(
		chatGenerationRequestsPerMin,
		chatGenerationMinInterval,
		chatGenerationWindow,
	)
}

func newChatGenerationRateLimiter(limit int, minInterval, window time.Duration) gin.HandlerFunc {
	clients := make(map[string]*chatGenerationCounter)
	var mu sync.Mutex
	lastCleanup := time.Now()

	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.Next()
			return
		}

		now := time.Now()
		var retryAfter time.Duration

		mu.Lock()
		if now.Sub(lastCleanup) >= window {
			for id, counter := range clients {
				if now.After(counter.resetAt) {
					delete(clients, id)
				}
			}
			lastCleanup = now
		}

		counter := clients[userID]
		if counter == nil || now.After(counter.resetAt) {
			clients[userID] = &chatGenerationCounter{
				count:       1,
				resetAt:     now.Add(window),
				lastRequest: now,
			}
			mu.Unlock()
			c.Next()
			return
		}

		if elapsed := now.Sub(counter.lastRequest); elapsed < minInterval {
			retryAfter = minInterval - elapsed
		} else if counter.count >= limit {
			retryAfter = time.Until(counter.resetAt)
		} else {
			counter.count++
			counter.lastRequest = now
			mu.Unlock()
			c.Next()
			return
		}
		mu.Unlock()

		seconds := int64(retryAfter.Seconds())
		if retryAfter%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", fmt.Sprintf("%d", seconds))
		response.FailWithRetryAfter(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试", "rate_limited", seconds)
		c.Abort()
	}
}
