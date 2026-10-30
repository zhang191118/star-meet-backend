package v1_test

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"strings"
	"testing"
	"time"
)

func setupGrowthAPITest(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("ADMIN_CONFIG_TOKEN", "growth-admin-test")
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.Feedback{}, &database.ProductEvent{}, &database.MemoryIndexJob{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.User{UserID: "owner", Email: "owner@example.test"}).Error; err != nil {
		t.Fatal(err)
	}
	api := apiv1.NewGrowthAPI(db, nil)
	router := gin.New()
	router.POST("/api/v1/feedback", api.CreateFeedback)
	router.POST("/api/v1/events", api.IngestEvents)
	router.GET("/api/v1/admin/feedback", api.ListFeedback)
	router.PUT("/api/v1/admin/feedback/:id", api.UpdateFeedback)
	router.GET("/api/v1/admin/growth", api.Summary)
	return db, router
}
func growthRequest(router *gin.Engine, method, path, body, token, admin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if admin != "" {
		req.Header.Set("X-Admin-Token", admin)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}
func TestFeedbackReceiptDoesNotExposePrivateContentAndAdminIsRequired(t *testing.T) {
	db, router := setupGrowthAPITest(t)
	submitted := growthRequest(router, "POST", "/api/v1/feedback", `{"category":"memory","content":"This is private feedback","contact":"private@example.test","user_id":"victim"}`, makeTestToken(t, "owner"), "")
	if submitted.Code != 200 || strings.Contains(submitted.Body.String(), "private@example.test") || strings.Contains(submitted.Body.String(), "This is private") {
		t.Fatalf("bad public response: %d %s", submitted.Code, submitted.Body.String())
	}
	var receipt struct {
		Data struct {
			ID string `json:"feedback_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(submitted.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Data.ID == "" {
		t.Fatal("missing receipt")
	}
	var row database.Feedback
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.UserID != "owner" {
		t.Fatal("request body forged ownership")
	}
	denied := growthRequest(router, "GET", "/api/v1/admin/feedback", "", "", "")
	if denied.Code != 401 || strings.Contains(denied.Body.String(), "private@example.test") {
		t.Fatal("admin data exposed")
	}
	allowed := growthRequest(router, "GET", "/api/v1/admin/feedback", "", "", "growth-admin-test")
	if allowed.Code != 200 || !strings.Contains(allowed.Body.String(), "This is private feedback") {
		t.Fatal("admin cannot read feedback")
	}
	updated := growthRequest(router, "PUT", "/api/v1/admin/feedback/"+receipt.Data.ID, `{"status":"resolved"}`, "", "growth-admin-test")
	if updated.Code != 200 {
		t.Fatalf("cannot resolve: %s", updated.Body.String())
	}
	db.First(&row)
	if row.Status != "resolved" {
		t.Fatal("status not persisted")
	}
}
func TestFeedbackRejectsInvalidBodiesAndDeletedUser(t *testing.T) {
	db, router := setupGrowthAPITest(t)
	for _, body := range []string{`{"category":"other","content":"x"}`, `{"category":"forged","content":"hello world"}`, `{"category":"other","content":"` + strings.Repeat("界", 2001) + `"}`} {
		result := growthRequest(router, "POST", "/api/v1/feedback", body, "", "")
		if result.Code != 400 {
			t.Fatalf("accepted invalid feedback: %d %s", result.Code, result.Body.String())
		}
	}
	db.Where("user_id = ?", "owner").Delete(&database.User{})
	result := growthRequest(router, "POST", "/api/v1/feedback", `{"category":"other","content":"after delete"}`, makeTestToken(t, "owner"), "")
	if result.Code != 401 {
		t.Fatalf("deleted account recreated data: %d %s", result.Code, result.Body.String())
	}
	var count int64
	db.Model(&database.Feedback{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid data was persisted")
	}
}
func TestGrowthEndpointRejectsPrivatePropertiesAndForgedSuccess(t *testing.T) {
	db, router := setupGrowthAPITest(t)
	for _, event := range []map[string]interface{}{
		{"event_id": uuid.NewString(), "installation_id": uuid.NewString(), "name": "ai_reply_completed", "occurred_at": time.Now().Unix()},
		{"event_id": uuid.NewString(), "installation_id": uuid.NewString(), "name": "chat_opened", "occurred_at": time.Now().Unix(), "properties": map[string]string{"password": "secret"}},
	} {
		body, _ := json.Marshal(map[string]interface{}{"events": []map[string]interface{}{event}})
		result := growthRequest(router, "POST", "/api/v1/events", string(body), "", "")
		if result.Code != 400 {
			t.Fatal("forged/private event accepted")
		}
	}
	var count int64
	db.Model(&database.ProductEvent{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid event was persisted")
	}
	summary := growthRequest(router, "GET", "/api/v1/admin/growth", "", "", "growth-admin-test")
	if summary.Code != 200 || !strings.Contains(summary.Body.String(), `"days":[]`) {
		t.Fatalf("empty summary is not usable: %s", summary.Body.String())
	}
}
func TestSubmissionLimiterBoundsPublicWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/limited", middleware.SubmissionRateLimiter(1, time.Hour), func(c *gin.Context) { c.Status(200) })
	if got := growthRequest(router, "POST", "/limited", "{}", "", "").Code; got != 200 {
		t.Fatal(got)
	}
	got := growthRequest(router, "POST", "/limited", "{}", "", "")
	if got.Code != http.StatusTooManyRequests || got.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit missing: %+v", got)
	}
}
