package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMobileServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(
		&database.User{},
		&database.UserSettings{},
		&database.Character{},
		&database.Session{},
		&database.UserCurrency{},
		&database.Subscription{},
	); err != nil {
		t.Fatalf("migrate mobile tables: %v", err)
	}

	return db
}

func TestMobileHomeAggregatesFirstScreenData(t *testing.T) {
	db := setupMobileServiceTestDB(t)
	now := time.Now()

	if err := db.Create(&database.User{
		UserID:      "mobile-user",
		Nickname:    "移动端用户",
		MemberLevel: "monthly",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.UserSettings{
		UserID:           "mobile-user",
		RelationshipMode: "supportive",
		ResponseStyle:    "gentle",
		ResponseLength:   "short",
		MemoryEnabled:    true,
		PushEnabled:      true,
		ProactiveEnable:  true,
	}).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "char-recent",
		UserID:       "system_admin",
		Name:         "最近角色",
		Gender:       "female",
		Relationship: "恋人",
		IsPublic:     1,
		ChatCount:    99,
	}).Error; err != nil {
		t.Fatalf("create recent character: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "char-recommend",
		UserID:       "system_admin",
		Name:         "推荐角色",
		Gender:       "female",
		Relationship: "朋友",
		SystemPrompt: "首屏不应该暴露的人设提示词",
		Mem0AgentID:  "mem0-secret-agent",
		IsPublic:     1,
		ChatCount:    200,
	}).Error; err != nil {
		t.Fatalf("create recommend character: %v", err)
	}
	if err := db.Create(&database.Session{
		SessionID:     "session-1",
		UserID:        "mobile-user",
		CharacterID:   "char-recent",
		LastMessage:   "今晚一起聊天吗？",
		LastMessageAt: &now,
		UnreadCount:   2,
		IsPinned:      true,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&database.UserCurrency{
		UserID:   "mobile-user",
		Currency: database.CurrencyMoon,
		Balance:  320,
	}).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	service := &MobileService{DB: db}
	home, err := service.GetHome("mobile-user")
	if err != nil {
		t.Fatalf("get mobile home: %v", err)
	}

	if home.User == nil {
		t.Fatal("expected user to be included")
	}
	if home.Settings == nil {
		t.Fatal("expected settings to be included")
	}
	if home.Membership.Level != "monthly" || !home.Membership.IsMember {
		t.Fatalf("expected active monthly membership, got %+v", home.Membership)
	}
	if home.Wallet[string(database.CurrencyMoon)] != 320 {
		t.Fatalf("expected moon balance 320, got %+v", home.Wallet)
	}
	if len(home.RecentSessions) != 1 {
		t.Fatalf("expected one recent session, got %d", len(home.RecentSessions))
	}
	if len(home.RecommendSections) == 0 {
		t.Fatal("expected recommend sections")
	}
	if len(home.RecommendSections[0].Items) == 0 {
		t.Fatal("expected recommend section items")
	}
	firstRecommendation := home.RecommendSections[0].Items[0]
	if firstRecommendation.CharacterID == "" || firstRecommendation.Name == "" {
		t.Fatalf("expected compact character identity, got %+v", firstRecommendation)
	}
	payload, err := json.Marshal(home.RecommendSections)
	if err != nil {
		t.Fatalf("marshal recommend sections: %v", err)
	}
	if strings.Contains(string(payload), "system_prompt") {
		t.Fatalf("mobile recommendation must not include system_prompt: %s", payload)
	}
	if strings.Contains(string(payload), "mem0_agent_id") {
		t.Fatalf("mobile recommendation must not include mem0_agent_id: %s", payload)
	}
	if len(home.Banners) == 0 {
		t.Fatal("expected mobile banners")
	}
	if home.ServerTime <= 0 {
		t.Fatalf("expected server time, got %d", home.ServerTime)
	}
}

func TestMobileHomeDegradesWalletWhenWalletLoadFails(t *testing.T) {
	db := setupMobileServiceTestDB(t)

	if err := db.Create(&database.User{
		UserID:   "mobile-wallet-error-user",
		Nickname: "钱包异常用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{
		CharacterID:  "mobile-wallet-error-char",
		UserID:       "system_admin",
		Name:         "钱包异常推荐角色",
		Gender:       "female",
		Relationship: "朋友",
		IsPublic:     1,
		ChatCount:    1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Migrator().DropTable(&database.UserCurrency{}); err != nil {
		t.Fatalf("drop wallet table: %v", err)
	}

	service := &MobileService{DB: db}
	home, err := service.GetHome("mobile-wallet-error-user")
	if err != nil {
		t.Fatalf("expected degraded mobile home instead of wallet error: %v", err)
	}
	if home == nil {
		t.Fatal("expected degraded home response")
	}
	if !home.Degraded {
		t.Fatalf("expected degraded=true, got %+v", home)
	}
	if len(home.UnavailableSections) != 1 || home.UnavailableSections[0] != "wallet" {
		t.Fatalf("expected unavailable wallet section, got %+v", home.UnavailableSections)
	}
	if home.Wallet[string(database.CurrencyStar)] != 0 || home.Wallet[string(database.CurrencyMoon)] != 0 || home.Wallet[string(database.CurrencySun)] != 0 {
		t.Fatalf("expected zero fallback wallet, got %+v", home.Wallet)
	}
	if home.User == nil || len(home.RecommendSections) == 0 || len(home.Banners) == 0 {
		t.Fatalf("expected degraded home to preserve primary content, got %+v", home)
	}
}

func TestMobileDashboardDegradesWalletWhenWalletLoadFails(t *testing.T) {
	db := setupMobileServiceTestDB(t)

	if err := db.Create(&database.User{
		UserID:      "dashboard-wallet-error-user",
		Nickname:    "面板钱包异常用户",
		MemberLevel: "monthly",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Migrator().DropTable(&database.UserCurrency{}); err != nil {
		t.Fatalf("drop wallet table: %v", err)
	}

	service := &MobileService{DB: db}
	dashboard, err := service.GetDashboard("dashboard-wallet-error-user")
	if err != nil {
		t.Fatalf("expected degraded dashboard instead of wallet error: %v", err)
	}
	if dashboard == nil {
		t.Fatal("expected degraded dashboard response")
	}
	if !dashboard.Degraded {
		t.Fatalf("expected degraded=true, got %+v", dashboard)
	}
	if len(dashboard.UnavailableSections) != 1 || dashboard.UnavailableSections[0] != "wallet" {
		t.Fatalf("expected unavailable wallet section, got %+v", dashboard.UnavailableSections)
	}
	if dashboard.Wallet[string(database.CurrencyStar)] != 0 || dashboard.Wallet[string(database.CurrencyMoon)] != 0 || dashboard.Wallet[string(database.CurrencySun)] != 0 {
		t.Fatalf("expected zero fallback wallet, got %+v", dashboard.Wallet)
	}
	if dashboard.Profile == nil || dashboard.Subscription.Level == "" || dashboard.DailyLimits.RemainingMessages <= 0 {
		t.Fatalf("expected degraded dashboard to preserve primary content, got %+v", dashboard)
	}
}

func TestMobileDashboardCreatesDefaultSettingsWhenMissing(t *testing.T) {
	db := setupMobileServiceTestDB(t)

	if err := db.Create(&database.User{
		UserID:   "dashboard-default-settings-user",
		Nickname: "默认设置用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &MobileService{DB: db}
	dashboard, err := service.GetDashboard("dashboard-default-settings-user")
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	if dashboard.Settings == nil {
		t.Fatal("expected dashboard settings")
	}
	if dashboard.Settings.RelationshipMode != "romantic" || !dashboard.Settings.MemoryEnabled {
		t.Fatalf("unexpected default dashboard settings: %+v", dashboard.Settings)
	}

	var count int64
	if err := db.Model(&database.UserSettings{}).Where("user_id = ?", "dashboard-default-settings-user").Count(&count).Error; err != nil {
		t.Fatalf("count user settings: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected dashboard to persist one default settings row, got %d", count)
	}
}

func TestMobileHomeRecommendedCharactersUseStableTieBreakerOrdering(t *testing.T) {
	db := setupMobileServiceTestDB(t)
	baseTime := time.Now()

	if err := db.Create(&database.User{
		UserID:   "mobile-stable-user",
		Nickname: "稳定排序用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	for i := 0; i < 3; i++ {
		character := database.Character{
			CharacterID:  fmt.Sprintf("mobile-stable-char-%03d", i),
			UserID:       "system_admin",
			Name:         fmt.Sprintf("稳定推荐角色 %d", i),
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     1,
			ChatCount:    77,
			CreatedAt:    baseTime.Add(time.Duration(i) * time.Second),
			UpdatedAt:    baseTime.Add(time.Duration(10-i) * time.Second),
		}
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %d: %v", i, err)
		}
	}

	service := &MobileService{DB: db}
	home, err := service.GetHome("mobile-stable-user")
	if err != nil {
		t.Fatalf("get mobile home: %v", err)
	}

	if len(home.RecommendSections) == 0 || len(home.RecommendSections[0].Items) != 3 {
		t.Fatalf("expected 3 recommendations, got %+v", home.RecommendSections)
	}

	first := home.RecommendSections[0].Items[0]
	if first.CharacterID != "mobile-stable-char-002" {
		t.Fatalf("expected newest tied mobile recommendation first, got %+v", first)
	}
}

func TestMobileBatchAggregatesSafeCardsByInclude(t *testing.T) {
	db := setupMobileServiceTestDB(t)

	if err := db.Create(&database.User{
		UserID:      "batch-service-user",
		Nickname:    "批量服务用户",
		MemberLevel: "yearly",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.UserCurrency{
		UserID:   "batch-service-user",
		Currency: database.CurrencySun,
		Balance:  7,
	}).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	for _, character := range []database.Character{
		{
			CharacterID:  "batch-service-char-a",
			UserID:       "system_admin",
			Name:         "批量服务角色A",
			Gender:       "female",
			Relationship: "恋人",
			SystemPrompt: "不能暴露的人设",
			Mem0AgentID:  "secret-mem0",
			IsPublic:     1,
			ChatCount:    10,
		},
		{
			CharacterID:  "batch-service-private",
			UserID:       "owner",
			Name:         "私有角色",
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     2,
			ChatCount:    99,
		},
		{
			CharacterID:  "batch-service-char-b",
			UserID:       "system_admin",
			Name:         "批量服务角色B",
			Gender:       "male",
			Relationship: "朋友",
			IsPublic:     1,
			ChatCount:    5,
		},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %s: %v", character.CharacterID, err)
		}
	}

	service := &MobileService{DB: db}
	batch, err := service.GetBatch("batch-service-user", MobileBatchReq{
		CharacterIDs: []string{"batch-service-char-a", "batch-service-private", "batch-service-char-a", "batch-service-char-b"},
		Include:      []string{"characters"},
	})
	if err != nil {
		t.Fatalf("get mobile batch: %v", err)
	}

	if len(batch.Characters) != 2 {
		t.Fatalf("expected 2 public deduped cards, got %+v", batch.Characters)
	}
	if batch.Characters[0].CharacterID != "batch-service-char-a" || batch.Characters[1].CharacterID != "batch-service-char-b" {
		t.Fatalf("expected stable requested order, got %+v", batch.Characters)
	}
	if batch.Wallet != nil {
		t.Fatalf("wallet should not be included when not requested, got %+v", batch.Wallet)
	}
	if batch.Membership != nil {
		t.Fatalf("membership should not be included when not requested, got %+v", batch.Membership)
	}

	payload, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	if strings.Contains(string(payload), "system_prompt") {
		t.Fatalf("mobile batch must not include system_prompt: %s", payload)
	}
	if strings.Contains(string(payload), "mem0_agent_id") {
		t.Fatalf("mobile batch must not include mem0_agent_id: %s", payload)
	}

	defaultBatch, err := service.GetBatch("batch-service-user", MobileBatchReq{
		CharacterIDs: []string{"batch-service-char-a"},
	})
	if err != nil {
		t.Fatalf("get default mobile batch: %v", err)
	}
	if defaultBatch.Wallet[string(database.CurrencySun)] != 7 {
		t.Fatalf("expected default wallet balance, got %+v", defaultBatch.Wallet)
	}
	if defaultBatch.Membership == nil || defaultBatch.Membership.Level != "yearly" {
		t.Fatalf("expected default membership, got %+v", defaultBatch.Membership)
	}
}

func TestMobileBatchDegradesWalletWhenWalletLoadFails(t *testing.T) {
	db := setupMobileServiceTestDB(t)

	if err := db.Create(&database.User{
		UserID:      "batch-wallet-error-user",
		Nickname:    "批量钱包异常用户",
		MemberLevel: "monthly",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, character := range []database.Character{
		{
			CharacterID:  "batch-wallet-error-char-a",
			UserID:       "system_admin",
			Name:         "批量钱包异常角色A",
			Gender:       "female",
			Relationship: "恋人",
			IsPublic:     1,
			ChatCount:    10,
		},
		{
			CharacterID:  "batch-wallet-error-char-b",
			UserID:       "system_admin",
			Name:         "批量钱包异常角色B",
			Gender:       "male",
			Relationship: "朋友",
			IsPublic:     1,
			ChatCount:    5,
		},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %s: %v", character.CharacterID, err)
		}
	}
	if err := db.Migrator().DropTable(&database.UserCurrency{}); err != nil {
		t.Fatalf("drop wallet table: %v", err)
	}

	service := &MobileService{DB: db}
	batch, err := service.GetBatch("batch-wallet-error-user", MobileBatchReq{
		CharacterIDs: []string{"batch-wallet-error-char-a", "batch-wallet-error-char-b"},
		Include:      []string{"characters", "wallet", "membership"},
	})
	if err != nil {
		t.Fatalf("expected degraded batch instead of wallet error: %v", err)
	}
	if len(batch.Characters) != 2 {
		t.Fatalf("expected characters to survive wallet degradation, got %+v", batch.Characters)
	}
	if batch.Membership == nil || batch.Membership.Level != "monthly" {
		t.Fatalf("expected membership to survive wallet degradation, got %+v", batch.Membership)
	}
	if !batch.Degraded {
		t.Fatalf("expected degraded=true, got %+v", batch)
	}
	if len(batch.UnavailableSections) != 1 || batch.UnavailableSections[0] != "wallet" {
		t.Fatalf("expected unavailable wallet section, got %+v", batch.UnavailableSections)
	}
	if batch.Wallet[string(database.CurrencyStar)] != 0 || batch.Wallet[string(database.CurrencyMoon)] != 0 || batch.Wallet[string(database.CurrencySun)] != 0 {
		t.Fatalf("expected zero fallback wallet, got %+v", batch.Wallet)
	}
}
