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

func setupMomentAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.Character{}, &database.CharacterMoment{}); err != nil {
		t.Fatalf("migrate moment api tables: %v", err)
	}
	return db
}

func TestGetMomentFeedReturnsPaginationMetadataAndCharacterInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMomentAPITestDB(t)
	if err := db.Create(&database.User{UserID: "moment-user", Nickname: "动态用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "moment-char", UserID: "system", Name: "林晚晴",
		Avatar: "https://oss/avatar.png", Gender: "male", IsPublic: 1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}

	now := time.Now()
	for i := 0; i < 25; i++ {
		if err := db.Create(&database.CharacterMoment{
			CharacterID: "moment-char",
			Content:     fmt.Sprintf("今日份的心情 %d", i),
			ImageURLs:   `["https://oss/m1.jpg","https://oss/m2.jpg"]`,
			TopicTags:   "日常,咖啡",
			Status:      1,
			PublishDay:  fmt.Sprintf("2026-08-%02d", i%28+1),
			PublishedAt: now.Add(time.Duration(i) * time.Hour),
		}).Error; err != nil {
			t.Fatalf("create moment %d: %v", i, err)
		}
	}

	momentAPI := apiv1.NewMomentAPI(&service.MomentService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/moment")
	group.Use(middleware.Auth())
	group.GET("/feed", momentAPI.GetFeed)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moment/feed?page=1&page_size=10", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "moment-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	data := decodePaginationData(t, recorder)
	if data["total"] != float64(25) {
		t.Fatalf("expected total 25, got %#v", data["total"])
	}
	if data["has_more"] != true {
		t.Fatalf("expected has_more true, got %#v", data["has_more"])
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 10 {
		t.Fatalf("expected 10 items, got %#v", data["list"])
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected item type: %#v", list[0])
	}
	if first["name"] != "林晚晴" {
		t.Fatalf("expected character name in feed, got %#v", first["name"])
	}
	if first["avatar"] != "https://oss/avatar.png" {
		t.Fatalf("expected avatar in feed, got %#v", first["avatar"])
	}
	images, ok := first["image_urls"].([]interface{})
	if !ok || len(images) != 2 {
		t.Fatalf("expected 2 image urls, got %#v", first["image_urls"])
	}
}

func TestGetMomentFeedRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupMomentAPITestDB(t)
	momentAPI := apiv1.NewMomentAPI(&service.MomentService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/moment")
	group.Use(middleware.Auth())
	group.GET("/feed", momentAPI.GetFeed)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moment/feed", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("expected non-200 without token, got 200")
	}
}

func TestGetCharacterMomentFeedReturnsOnlyRequestedCharacter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupMomentAPITestDB(t)
	if err := db.Create(&database.User{UserID: "character-moment-user", Nickname: "动态用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	for i, characterID := range []string{"character-moment-one", "character-moment-two"} {
		if err := db.Create(&database.Character{CharacterID: characterID, UserID: "system", Name: characterID, Gender: "male", IsPublic: 1}).Error; err != nil {
			t.Fatalf("create character: %v", err)
		}
		if err := db.Create(&database.CharacterMoment{CharacterID: characterID, Content: "动态", Status: 1, PublishDay: fmt.Sprintf("2026-08-%02d", i+1), PublishedAt: time.Now()}).Error; err != nil {
			t.Fatalf("create moment: %v", err)
		}
	}

	momentAPI := apiv1.NewMomentAPI(&service.MomentService{DB: db})
	router := gin.New()
	group := router.Group("/api/v1/moment")
	group.Use(middleware.Auth())
	group.GET("/character/:character_id", momentAPI.GetCharacterFeed)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/moment/character/character-moment-two?page=1&page_size=20", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "character-moment-user"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	data := decodePaginationData(t, recorder)
	if data["total"] != float64(1) {
		t.Fatalf("expected one character moment, got %#v", data["total"])
	}
}
