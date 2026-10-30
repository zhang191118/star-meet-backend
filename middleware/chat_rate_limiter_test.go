package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestChatGenerationRateLimiterIsPerUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	limiter := newChatGenerationRateLimiter(2, 0, time.Minute)
	router.POST("/chat", func(c *gin.Context) {
		c.Set("user_id", c.GetHeader("X-User-ID"))
	}, limiter, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := func(userID string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/chat", nil)
		req.Header.Set("X-User-ID", userID)
		router.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := request("user-a"); recorder.Code != http.StatusNoContent {
		t.Fatalf("first request: expected %d, got %d", http.StatusNoContent, recorder.Code)
	}
	if recorder := request("user-a"); recorder.Code != http.StatusNoContent {
		t.Fatalf("second request: expected %d, got %d", http.StatusNoContent, recorder.Code)
	}
	if recorder := request("user-a"); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: expected %d, got %d", http.StatusTooManyRequests, recorder.Code)
	} else if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response should include Retry-After")
	}
	if recorder := request("user-b"); recorder.Code != http.StatusNoContent {
		t.Fatalf("separate user should not share a limit, got %d", recorder.Code)
	}
}

func TestChatGenerationRateLimiterAppliesMinimumInterval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	limiter := newChatGenerationRateLimiter(10, time.Minute, time.Hour)
	router.POST("/chat", func(c *gin.Context) {
		c.Set("user_id", "user-a")
	}, limiter, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/chat", nil))
	if first.Code != http.StatusNoContent {
		t.Fatalf("first request: expected %d, got %d", http.StatusNoContent, first.Code)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/chat", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("immediate second request: expected %d, got %d", http.StatusTooManyRequests, second.Code)
	}
}
