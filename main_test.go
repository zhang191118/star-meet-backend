package main

import (
	"testing"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
