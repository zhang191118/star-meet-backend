package v1

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 新契约：用户在应用内显式选择的语言（Accept-Language）优先，
// 地区只在没有语言头时兜底。
func TestResolveProfileLangPrefersLanguageHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		region string
		lang   string
		want   string
	}{
		{"TW", "en", "en"},
		{"CN", "zh-Hant", "zh-Hant"},
		{"US", "zh-Hans", "zh-Hans"},
		{"", "zh-Hant", "zh-Hant"},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Language", tt.lang)
		req.Header.Set("X-Client-Region", tt.region)
		ctx.Request = req
		if got := resolveProfileLang(ctx); got != tt.want {
			t.Fatalf("resolveProfileLang(region=%q, lang=%q)=%q want %q", tt.region, tt.lang, got, tt.want)
		}
	}
}

// 没有语言头时回退到地区映射。
func TestResolveProfileLangFallsBackToRegion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		region string
		want   string
	}{
		{"TW", "zh-Hant"},
		{"HK", "zh-Hant"},
		{"CN", "zh-Hans"},
		{"US", "en"},
		{"", "en"},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Client-Region", tt.region)
		ctx.Request = req
		if got := resolveProfileLang(ctx); got != tt.want {
			t.Fatalf("resolveProfileLang(region=%q, no lang)=%q want %q", tt.region, got, tt.want)
		}
	}
}
