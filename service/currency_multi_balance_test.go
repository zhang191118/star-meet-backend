package service

import (
	"testing"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupCurrencyMultiBalanceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.UserCurrency{}, &database.CurrencyTransaction{}); err != nil {
		t.Fatalf("migrate currency tables: %v", err)
	}

	if err := db.Create(&database.User{UserID: "multi-currency-user", Nickname: "多币种用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	return db
}

func TestUserCanHoldStarMoonAndSunBalances(t *testing.T) {
	db := setupCurrencyMultiBalanceTestDB(t)
	currencyService := NewCurrencyService(db)

	for _, currency := range []struct {
		currencyType database.CurrencyType
		amount       int64
	}{
		{database.CurrencyStar, 11},
		{database.CurrencyMoon, 22},
		{database.CurrencySun, 33},
	} {
		if err := currencyService.AddCurrency("multi-currency-user", currency.currencyType, currency.amount, database.TransactionSystem, "", "seed balance"); err != nil {
			t.Fatalf("add %s currency: %v", currency.currencyType, err)
		}
	}

	balance, err := currencyService.GetUserBalance("multi-currency-user")
	if err != nil {
		t.Fatalf("get user balance: %v", err)
	}
	if balance[database.CurrencyStar] != 11 {
		t.Fatalf("expected star balance 11, got %d", balance[database.CurrencyStar])
	}
	if balance[database.CurrencyMoon] != 22 {
		t.Fatalf("expected moon balance 22, got %d", balance[database.CurrencyMoon])
	}
	if balance[database.CurrencySun] != 33 {
		t.Fatalf("expected sun balance 33, got %d", balance[database.CurrencySun])
	}
}
