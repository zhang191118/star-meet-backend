package v1_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMemoryPaginationAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.Memory{}); err != nil {
		t.Fatalf("migrate memory api tables: %v", err)
	}
	return db
}

func TestGetMemoryListIncludesMobilePaginationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMemoryPaginationAPITestDB(t)
	if err := db.Create(&database.User{UserID: "memory-page-user", Nickname: "记忆分页用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now()
	for i := 0; i < 101; i++ {
		if err := db.Create(&database.Memory{
			MemoryID:    fmt.Sprintf("memory-page-%03d", i),
			UserID:      "memory-page-user",
			CharacterID: "memory-page-char",
			Content:     fmt.Sprintf("memory content %d", i),
			MemoryType:  "fact",
			Source:      "manual",
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatalf("create memory %d: %v", i, err)
		}
	}

	memoryAPI := apiv1.NewMemoryAPI(&service.MemoryService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/memory")
	group.Use(middleware.Auth())
	group.GET("/list", memoryAPI.GetList)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/memory/list?character_id=memory-page-char&page=1&page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "memory-page-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	data := decodePaginationData(t, recorder)
	if data["page"] != float64(1) {
		t.Fatalf("expected page 1, got %#v", data["page"])
	}
	if data["page_size"] != float64(100) {
		t.Fatalf("expected clamped page_size 100, got %#v", data["page_size"])
	}
	if data["has_more"] != true {
		t.Fatalf("expected has_more true, got %#v", data["has_more"])
	}
	if data["total"] != float64(101) {
		t.Fatalf("expected total 101, got %#v", data["total"])
	}
	list, ok := data["list"].([]interface{})
	if !ok {
		t.Fatalf("expected list array, got %#v", data["list"])
	}
	if len(list) != 100 {
		t.Fatalf("expected 100 memories after clamp, got %d", len(list))
	}
}

func TestGetMemoryStatsRejectsMissingCharacterIDWithValidationError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMemoryPaginationAPITestDB(t)
	if err := db.Create(&database.User{UserID: "memory-stats-missing-char-user", Nickname: "记忆统计缺参用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	memoryAPI := apiv1.NewMemoryAPI(&service.MemoryService{DB: db})
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/memory")
	group.Use(middleware.Auth())
	group.GET("/stats", memoryAPI.GetStats)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/memory/stats", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "memory-stats-missing-char-user"))
	req.Header.Set("X-Request-ID", "memory-stats-missing-character-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing character_id, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "memory-stats-missing-character-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "character_id" {
		t.Fatalf("expected character_id field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected reason detail, got %#v", details["reason"])
	}
}
