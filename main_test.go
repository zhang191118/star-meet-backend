package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"star-meet-backend/model/database"

	"github.com/spf13/viper"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIsProductionModeUsesContainerEnvironment(t *testing.T) {
	t.Setenv("GIN_MODE", "release")
	t.Setenv("APP_ENV", "")
	t.Setenv("ENVIRONMENT", "")
	viper.Set("server.mode", "debug")
	if !isProductionMode() {
		t.Fatal("expected GIN_MODE=release to enable production mode")
	}

	_ = os.Unsetenv("GIN_MODE")
	viper.Set("server.mode", "debug")
	if isProductionMode() {
		t.Fatal("expected debug mode after clearing production environment")
	}
}

func TestEnsureUserCurrencyIndexesDropsLegacyUserOnlyUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.Exec(`
		CREATE TABLE user_currencies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			user_id VARCHAR(64) NOT NULL,
			currency VARCHAR(20) NOT NULL,
			balance INTEGER DEFAULT 0
		)
	`).Error; err != nil {
		t.Fatalf("create legacy user_currencies table: %v", err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_user_currencies_user_id ON user_currencies(user_id)").Error; err != nil {
		t.Fatalf("create legacy user_id unique index: %v", err)
	}

	if err := ensureUserCurrencyIndexes(db); err != nil {
		t.Fatalf("ensure user currency indexes: %v", err)
	}

	for _, currency := range []database.UserCurrency{
		{UserID: "legacy-user", Currency: database.CurrencyStar, Balance: 1},
		{UserID: "legacy-user", Currency: database.CurrencyMoon, Balance: 2},
	} {
		if err := db.Create(&currency).Error; err != nil {
			t.Fatalf("create %s balance after index reconciliation: %v", currency.Currency, err)
		}
	}

	duplicate := database.UserCurrency{UserID: "legacy-user", Currency: database.CurrencyMoon, Balance: 3}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatalf("expected duplicate user/currency balance to be rejected")
	}
}

func TestSafeAutoMigrateCreatesAppleTransactionOwnerTableWithUniqueKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := safeAutoMigrateModels(db, &database.AppleTransactionOwner{}); err != nil {
		t.Fatalf("migrate apple transaction owners: %v", err)
	}
	if !db.Migrator().HasTable(&database.AppleTransactionOwner{}) {
		t.Fatal("apple transaction owner table was not created")
	}

	first := database.AppleTransactionOwner{
		Environment:           "Sandbox",
		OriginalTransactionID: "migration-original",
		UserID:                "first-owner",
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	duplicate := database.AppleTransactionOwner{
		Environment:           "Sandbox",
		OriginalTransactionID: "migration-original",
		UserID:                "second-owner",
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("expected environment/original transaction unique key to reject a second owner")
	}
}

func TestSchemaOnlyFailureUpgradesWarningErrors(t *testing.T) {
	cause := errors.New("add sessions.context_summary failed")
	err := schemaOnlyFailure(true, "会话状态字段补齐失败", cause)
	if err == nil {
		t.Fatal("expected schema-only migration to return an error")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected wrapped migration cause, got %v", err)
	}
	if !strings.Contains(err.Error(), "会话状态字段补齐失败") {
		t.Fatalf("expected migration context in error, got %v", err)
	}

	if err := schemaOnlyFailure(false, "会话状态字段补齐失败", cause); err != nil {
		t.Fatalf("expected normal mode to preserve warning behavior, got %v", err)
	}
	if err := schemaOnlyFailure(true, "会话状态字段补齐失败", nil); err != nil {
		t.Fatalf("expected nil migration error to remain nil, got %v", err)
	}
}
