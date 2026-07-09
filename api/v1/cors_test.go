package v1_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"star-meet-backend/middleware"

	"github.com/gin-gonic/gin"
)

func TestCORSAllowsRequestIDHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.CORS())
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, X-Request-ID")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", recorder.Code)
	}
	if allowHeaders := recorder.Header().Get("Access-Control-Allow-Headers"); allowHeaders == "" || !containsHeaderToken(allowHeaders, "X-Request-ID") {
		t.Fatalf("expected X-Request-ID to be allowed, got %q", allowHeaders)
	}
	if exposeHeaders := recorder.Header().Get("Access-Control-Expose-Headers"); exposeHeaders == "" || !containsHeaderToken(exposeHeaders, "X-Request-ID") {
		t.Fatalf("expected X-Request-ID to be exposed, got %q", exposeHeaders)
	}
}

func TestCORSReflectsAllowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.CORS())
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://www.starmeet.com")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if allowOrigin := recorder.Header().Get("Access-Control-Allow-Origin"); allowOrigin != "https://www.starmeet.com" {
		t.Fatalf("expected allowed origin to be reflected, got %q", allowOrigin)
	}
}

func TestCORSReflectsEnvConfiguredOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://preview.starmeet.test, https://app.starmeet.test")

	router := gin.New()
	router.Use(middleware.CORS())
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://app.starmeet.test")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if allowOrigin := recorder.Header().Get("Access-Control-Allow-Origin"); allowOrigin != "https://app.starmeet.test" {
		t.Fatalf("expected env configured origin to be reflected, got %q", allowOrigin)
	}
}

func TestCORSDoesNotWildcardUnknownOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.CORS())
	router.GET("/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if allowOrigin := recorder.Header().Get("Access-Control-Allow-Origin"); allowOrigin != "" {
		t.Fatalf("expected unknown origin to be omitted, got %q", allowOrigin)
	}
}

func containsHeaderToken(value, token string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(item), token) {
			return true
		}
	}
	return false
}
