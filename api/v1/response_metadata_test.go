package v1_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"star-meet-backend/global"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

func TestOkWithDataIncludesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/metadata", func(c *gin.Context) {
		response.OkWithData(c, gin.H{"value": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	req.Header.Set("X-Request-ID", "req-mobile-1")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "req-mobile-1" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime <= 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
}

func TestRequestIDPreservesValidClientID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/metadata", func(c *gin.Context) {
		response.OkWithData(c, gin.H{"value": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/metadata", nil)
	req.Header.Set("X-Request-ID", "req_Valid-123")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "req_Valid-123" {
		t.Fatalf("expected valid request_id to be preserved, got %q", resp.RequestID)
	}
	if header := recorder.Header().Get("X-Request-ID"); header != "req_Valid-123" {
		t.Fatalf("expected response header request id to match, got %q", header)
	}
}

func TestRequestIDRegeneratesInvalidClientID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/metadata", func(c *gin.Context) {
		response.OkWithData(c, gin.H{"value": "ok"})
	})

	for _, tc := range []struct {
		name     string
		input    string
		notEqual string
	}{
		{name: "spaces", input: "bad request id", notEqual: "bad request id"},
		{name: "too_long", input: strings.Repeat("a", 129), notEqual: strings.Repeat("a", 129)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metadata", nil)
			req.Header.Set("X-Request-ID", tc.input)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
			}

			resp := decodeResponse(t, recorder)
			if resp.RequestID == "" {
				t.Fatal("expected regenerated request_id to be present")
			}
			if resp.RequestID == tc.notEqual {
				t.Fatalf("expected invalid request_id to be regenerated, got original %q", resp.RequestID)
			}
			if header := recorder.Header().Get("X-Request-ID"); header != resp.RequestID {
				t.Fatalf("expected response header request id %q to match body %q", header, resp.RequestID)
			}
		})
	}
}

func TestLegacyOkContextWritesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.GET("/legacy-ok", func(c *gin.Context) {
		response.Ok(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/legacy-ok", nil)
	req.Header.Set("X-Request-ID", "req-legacy-ok")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "req-legacy-ok" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime <= 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
}

func TestAuthFailureIncludesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.Use(middleware.Auth())
	router.GET("/private", func(c *gin.Context) {
		response.OkWithData(c, gin.H{"value": "private"})
	})

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("X-Request-ID", "req-auth-fail")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "req-auth-fail" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime <= 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
	if resp.ErrorCode != "missing_authorization" {
		t.Fatalf("expected structured missing_authorization error_code, got %q", resp.ErrorCode)
	}
}

func TestAuthFailuresUseStableSpecificErrorCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		authHeader string
		errorCode  string
	}{
		{name: "missing", errorCode: "missing_authorization"},
		{name: "bad_format", authHeader: "Token abc", errorCode: "invalid_authorization_format"},
		{name: "invalid_token", authHeader: "Bearer not-a-valid-jwt", errorCode: "invalid_token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(middleware.RequestID())
			router.Use(middleware.Auth())
			router.GET("/private", func(c *gin.Context) {
				response.OkWithData(c, gin.H{"value": "private"})
			})

			req := httptest.NewRequest(http.MethodGet, "/private", nil)
			req.Header.Set("X-Request-ID", "req-auth-"+tt.name)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d body=%s", recorder.Code, recorder.Body.String())
			}

			resp := decodeResponse(t, recorder)
			if resp.ErrorCode != tt.errorCode {
				t.Fatalf("expected error_code %q, got %q", tt.errorCode, resp.ErrorCode)
			}
			if resp.RequestID != "req-auth-"+tt.name {
				t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
			}
			if resp.ServerTime <= 0 {
				t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
			}
		})
	}
}

func TestRateLimitFailureIncludesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.Use(middleware.RateLimiter())
	router.GET("/limited", func(c *gin.Context) {
		response.OkWithData(c, gin.H{"value": "ok"})
	})

	var recorder *httptest.ResponseRecorder
	for i := 0; i < 101; i++ {
		req := httptest.NewRequest(http.MethodGet, "/limited", nil)
		req.Header.Set("X-Request-ID", "req-rate-limit")
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
	}

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "req-rate-limit" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime <= 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
	if resp.ErrorCode != "rate_limited" {
		t.Fatalf("expected structured rate_limited error_code, got %q", resp.ErrorCode)
	}
	if resp.RetryAfter <= 0 {
		t.Fatalf("expected positive retry_after, got %d", resp.RetryAfter)
	}
	if header := recorder.Header().Get("Retry-After"); header == "" {
		t.Fatal("expected Retry-After header for rate limit response")
	}
}

func TestRecoveryFailureIncludesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.RequestID())
	router.Use(middleware.Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set("X-Request-ID", "req-recovery")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "req-recovery" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime <= 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
	if resp.ErrorCode != "internal_error" {
		t.Fatalf("expected structured internal_error error_code, got %q", resp.ErrorCode)
	}
}

func TestMinorProtectionFailureIncludesRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupAccessControlAPITestDB(t)
	if err := db.Create(&database.User{
		UserID:         "minor-user",
		Email:          "minor@example.com",
		IsMinor:        true,
		DailyUsageTime: 3600,
	}).Error; err != nil {
		t.Fatalf("create minor user: %v", err)
	}

	oldUserService := global.UserService
	global.UserService = &service.UserService{DB: db}
	defer func() { global.UserService = oldUserService }()

	router := gin.New()
	router.Use(middleware.RequestID())
	router.Use(middleware.MinorProtection())
	router.GET("/minor-protected", func(c *gin.Context) {
		response.OkWithData(c, gin.H{"value": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/minor-protected?user_id=minor-user", nil)
	req.Header.Set("X-Request-ID", "req-minor")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.RequestID != "req-minor" {
		t.Fatalf("expected request_id to be preserved, got %q", resp.RequestID)
	}
	if resp.ServerTime <= 0 {
		t.Fatalf("expected server_time to be set, got %d", resp.ServerTime)
	}
}
