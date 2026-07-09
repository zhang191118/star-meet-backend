package v1_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupGachaDrawIdempotencyRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.User{},
		&database.GachaPool{},
		&database.GachaCharacter{},
		&database.UserGachaDraw{},
		&database.UserGachaRecord{},
		&database.UserCharacter{},
		&database.UserCurrency{},
		&database.CurrencyTransaction{},
	); err != nil {
		t.Fatalf("migrate gacha draw tables: %v", err)
	}

	if err := db.Create(&database.User{UserID: "draw-user", Nickname: "抽卡用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, pool := range []database.GachaPool{
		{PoolID: "starter", Name: "新手池", Type: "permanent", IsActive: true, SSRRate: 0, SRRate: 0, RRate: 1, NGuarantee: 0, Price: 100},
		{PoolID: "event", Name: "活动池", Type: "event", IsActive: true, SSRRate: 0, SRRate: 0, RRate: 1, NGuarantee: 0, Price: 100},
	} {
		if err := db.Create(&pool).Error; err != nil {
			t.Fatalf("create pool %s: %v", pool.PoolID, err)
		}
	}
	for _, character := range []database.GachaCharacter{
		{CharacterID: "starter-r-char", PoolID: "starter", Rarity: database.GachaR, Weight: 1},
		{CharacterID: "starter-n-char", PoolID: "starter", Rarity: database.GachaN, Weight: 1},
		{CharacterID: "starter-sr-char", PoolID: "starter", Rarity: database.GachaSR, Weight: 1},
		{CharacterID: "starter-ssr-char", PoolID: "starter", Rarity: database.GachaSSR, Weight: 1},
		{CharacterID: "event-r-char", PoolID: "event", Rarity: database.GachaR, Weight: 1},
		{CharacterID: "event-n-char", PoolID: "event", Rarity: database.GachaN, Weight: 1},
		{CharacterID: "event-sr-char", PoolID: "event", Rarity: database.GachaSR, Weight: 1},
		{CharacterID: "event-ssr-char", PoolID: "event", Rarity: database.GachaSSR, Weight: 1},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create gacha character %s: %v", character.CharacterID, err)
		}
	}

	api := &apiv1.GachaAPI{GachaService: service.NewGachaService(db)}
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/gacha")
	group.Use(middleware.Auth())
	group.POST("/draw", api.Draw)
	return router, db
}

func postGachaDraw(t *testing.T, router *gin.Engine, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/gacha/draw", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "draw-user"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func seedMoonBalance(t *testing.T, db *gorm.DB, balance int64) {
	t.Helper()

	if err := db.Create(&database.UserCurrency{
		UserID:   "draw-user",
		Currency: database.CurrencyMoon,
		Balance:  balance,
	}).Error; err != nil {
		t.Fatalf("create moon balance: %v", err)
	}
}

func TestGachaDrawReusesClientDrawIDOnRetry(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)
	seedMoonBalance(t, db, 1000)

	payload := map[string]interface{}{
		"pool_id":        "starter",
		"count":          1,
		"client_draw_id": "ios-draw-1",
	}
	for i := 0; i < 2; i++ {
		recorder := postGachaDraw(t, router, payload)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d expected status 200, got %d body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	var recordCount int64
	if err := db.Model(&database.UserGachaRecord{}).
		Where("user_id = ? AND client_draw_id = ?", "draw-user", "ios-draw-1").
		Count(&recordCount).Error; err != nil {
		t.Fatalf("count draw records: %v", err)
	}
	if recordCount != 1 {
		t.Fatalf("expected one gacha record after retry, got %d", recordCount)
	}

	var ownedCount int64
	if err := db.Model(&database.UserCharacter{}).Where("user_id = ?", "draw-user").Count(&ownedCount).Error; err != nil {
		t.Fatalf("count owned characters: %v", err)
	}
	if ownedCount != 1 {
		t.Fatalf("expected one owned character after retry, got %d", ownedCount)
	}
}

func TestGachaDrawRejectsClientDrawIDParameterMismatch(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)
	seedMoonBalance(t, db, 1000)

	first := postGachaDraw(t, router, map[string]interface{}{
		"pool_id":        "starter",
		"count":          1,
		"client_draw_id": "ios-draw-conflict",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first request expected status 200, got %d body=%s", first.Code, first.Body.String())
	}

	second := postGachaDraw(t, router, map[string]interface{}{
		"pool_id":        "event",
		"count":          1,
		"client_draw_id": "ios-draw-conflict",
	})
	if second.Code != http.StatusConflict {
		t.Fatalf("second request expected status 409, got %d body=%s", second.Code, second.Body.String())
	}
	resp := decodeResponse(t, second)
	if resp.ErrorCode != "gacha_idempotency_conflict" {
		t.Fatalf("expected gacha_idempotency_conflict error_code, got %q", resp.ErrorCode)
	}

	var recordCount int64
	if err := db.Model(&database.UserGachaRecord{}).Where("user_id = ?", "draw-user").Count(&recordCount).Error; err != nil {
		t.Fatalf("count draw records: %v", err)
	}
	if recordCount != 1 {
		t.Fatalf("expected conflict not to create another draw record, got %d", recordCount)
	}
}

func TestGachaDrawWithoutClientDrawIDKeepsLegacyRepeatBehavior(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)
	seedMoonBalance(t, db, 1000)

	payload := map[string]interface{}{
		"pool_id": "starter",
		"count":   1,
	}
	for i := 0; i < 2; i++ {
		recorder := postGachaDraw(t, router, payload)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d expected status 200, got %d body=%s", i+1, recorder.Code, recorder.Body.String())
		}
	}

	var recordCount int64
	if err := db.Model(&database.UserGachaRecord{}).Where("user_id = ?", "draw-user").Count(&recordCount).Error; err != nil {
		t.Fatalf("count draw records: %v", err)
	}
	if recordCount != 2 {
		t.Fatalf("expected legacy repeat draw to create two records, got %d", recordCount)
	}
}

func TestGachaDrawDeductsMoonAndReturnsRemainingBalance(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)

	seedMoonBalance(t, db, 777)

	recorder := postGachaDraw(t, router, map[string]interface{}{
		"pool_id": "starter",
		"count":   1,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected response data object, got %#v", resp.Data)
	}

	remainingMoon, ok := data["remaining_moon"].(float64)
	if !ok {
		t.Fatalf("expected remaining_moon number, got %#v", data["remaining_moon"])
	}
	if remainingMoon != 677 {
		t.Fatalf("expected remaining_moon to reflect balance after 100 moon draw, got %.0f", remainingMoon)
	}

	var moon database.UserCurrency
	if err := db.Where("user_id = ? AND currency = ?", "draw-user", database.CurrencyMoon).First(&moon).Error; err != nil {
		t.Fatalf("load moon balance: %v", err)
	}
	if moon.Balance != 677 {
		t.Fatalf("expected stored moon balance 677, got %d", moon.Balance)
	}

	var tx database.CurrencyTransaction
	if err := db.Where("user_id = ? AND currency = ? AND type = ?", "draw-user", database.CurrencyMoon, database.TransactionGacha).First(&tx).Error; err != nil {
		t.Fatalf("load gacha currency transaction: %v", err)
	}
	if tx.Amount != -100 {
		t.Fatalf("expected gacha transaction amount -100, got %d", tx.Amount)
	}
}

func TestGachaDrawRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	router, _ := setupGachaDrawIdempotencyRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/gacha/draw", bytes.NewReader([]byte(`{"pool_id":"starter"`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "draw-user"))
	req.Header.Set("X-Request-ID", "gacha-draw-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed gacha JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "gacha-draw-malformed-json-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "body" {
		t.Fatalf("expected body field detail, got %#v", details["field"])
	}
	if _, ok := details["reason"].(string); !ok {
		t.Fatalf("expected string reason detail, got %#v", details["reason"])
	}
}

func TestGachaDrawRejectsInsufficientMoonBalance(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)

	seedMoonBalance(t, db, 99)

	recorder := postGachaDraw(t, router, map[string]interface{}{
		"pool_id": "starter",
		"count":   1,
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for insufficient moon balance, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "insufficient_moon_balance" {
		t.Fatalf("expected insufficient_moon_balance error_code, got %q", resp.ErrorCode)
	}

	var recordCount int64
	if err := db.Model(&database.UserGachaRecord{}).Where("user_id = ?", "draw-user").Count(&recordCount).Error; err != nil {
		t.Fatalf("count draw records: %v", err)
	}
	if recordCount != 0 {
		t.Fatalf("expected insufficient balance not to create draw records, got %d", recordCount)
	}

	var moon database.UserCurrency
	if err := db.Where("user_id = ? AND currency = ?", "draw-user", database.CurrencyMoon).First(&moon).Error; err != nil {
		t.Fatalf("load moon balance: %v", err)
	}
	if moon.Balance != 99 {
		t.Fatalf("expected stored moon balance to remain 99, got %d", moon.Balance)
	}
}

func TestGachaDrawIdempotentRetryDoesNotDeductMoonTwice(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)

	seedMoonBalance(t, db, 777)

	payload := map[string]interface{}{
		"pool_id":        "starter",
		"count":          1,
		"client_draw_id": "ios-draw-deduct-once",
	}
	for i := 0; i < 2; i++ {
		recorder := postGachaDraw(t, router, payload)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d expected status 200, got %d body=%s", i+1, recorder.Code, recorder.Body.String())
		}

		resp := decodeResponse(t, recorder)
		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("request %d expected response data object, got %#v", i+1, resp.Data)
		}
		if remainingMoon, ok := data["remaining_moon"].(float64); !ok || remainingMoon != 677 {
			t.Fatalf("request %d expected remaining_moon 677, got %#v", i+1, data["remaining_moon"])
		}
	}

	var moon database.UserCurrency
	if err := db.Where("user_id = ? AND currency = ?", "draw-user", database.CurrencyMoon).First(&moon).Error; err != nil {
		t.Fatalf("load moon balance: %v", err)
	}
	if moon.Balance != 677 {
		t.Fatalf("expected idempotent retry to deduct moon once, got balance %d", moon.Balance)
	}

	var txCount int64
	if err := db.Model(&database.CurrencyTransaction{}).
		Where("user_id = ? AND currency = ? AND type = ?", "draw-user", database.CurrencyMoon, database.TransactionGacha).
		Count(&txCount).Error; err != nil {
		t.Fatalf("count gacha currency transactions: %v", err)
	}
	if txCount != 1 {
		t.Fatalf("expected one gacha currency transaction after retry, got %d", txCount)
	}
}

func TestGachaDrawReturnsBusinessErrorWhenPoolIsUnavailable(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)
	seedMoonBalance(t, db, 1000)

	recorder := postGachaDraw(t, router, map[string]interface{}{
		"pool_id": "missing-pool",
		"count":   1,
	})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for unavailable gacha pool, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "gacha_pool_unavailable" {
		t.Fatalf("expected gacha_pool_unavailable error_code, got %q", resp.ErrorCode)
	}

	var recordCount int64
	if err := db.Model(&database.UserGachaRecord{}).Where("user_id = ?", "draw-user").Count(&recordCount).Error; err != nil {
		t.Fatalf("count draw records: %v", err)
	}
	if recordCount != 0 {
		t.Fatalf("expected unavailable pool not to create draw records, got %d", recordCount)
	}
}

func TestGachaDrawReturnsBusinessErrorWhenPoolHasNoCharacters(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)
	seedMoonBalance(t, db, 1000)
	if err := db.Where("pool_id = ?", "starter").Delete(&database.GachaCharacter{}).Error; err != nil {
		t.Fatalf("delete starter characters: %v", err)
	}

	recorder := postGachaDraw(t, router, map[string]interface{}{
		"pool_id": "starter",
		"count":   1,
	})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for unconfigured gacha pool, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "gacha_pool_unconfigured" {
		t.Fatalf("expected gacha_pool_unconfigured error_code, got %q", resp.ErrorCode)
	}

	var moon database.UserCurrency
	if err := db.Where("user_id = ? AND currency = ?", "draw-user", database.CurrencyMoon).First(&moon).Error; err != nil {
		t.Fatalf("load moon balance: %v", err)
	}
	if moon.Balance != 1000 {
		t.Fatalf("expected unconfigured pool not to deduct moon, got balance %d", moon.Balance)
	}
}

func TestGachaDrawReturnsBusinessErrorWhenRarityHasNoCandidates(t *testing.T) {
	router, db := setupGachaDrawIdempotencyRouter(t)
	seedMoonBalance(t, db, 1000)
	if err := db.Model(&database.GachaPool{}).
		Where("pool_id = ?", "starter").
		Updates(map[string]interface{}{
			"ssr_rate": 0,
			"sr_rate":  0,
			"r_rate":   1,
		}).Error; err != nil {
		t.Fatalf("force starter pool rarity: %v", err)
	}
	if err := db.Where("pool_id = ? AND rarity = ?", "starter", database.GachaR).Delete(&database.GachaCharacter{}).Error; err != nil {
		t.Fatalf("delete starter R candidates: %v", err)
	}

	recorder := postGachaDraw(t, router, map[string]interface{}{
		"pool_id": "starter",
		"count":   1,
	})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing rarity candidates, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "gacha_pool_unconfigured" {
		t.Fatalf("expected gacha_pool_unconfigured error_code, got %q", resp.ErrorCode)
	}

	var recordCount int64
	if err := db.Model(&database.UserGachaRecord{}).Where("user_id = ?", "draw-user").Count(&recordCount).Error; err != nil {
		t.Fatalf("count draw records: %v", err)
	}
	if recordCount != 0 {
		t.Fatalf("expected missing rarity candidates not to create draw records, got %d", recordCount)
	}

	var moon database.UserCurrency
	if err := db.Where("user_id = ? AND currency = ?", "draw-user", database.CurrencyMoon).First(&moon).Error; err != nil {
		t.Fatalf("load moon balance: %v", err)
	}
	if moon.Balance != 1000 {
		t.Fatalf("expected missing rarity candidates not to deduct moon, got balance %d", moon.Balance)
	}
}
