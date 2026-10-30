package service

import (
	"errors"
	"testing"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupGachaSeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Character{},
		&database.GachaPool{},
		&database.GachaCharacter{},
		&database.UserGachaDraw{},
		&database.UserGachaRecord{},
		&database.UserCharacter{},
		&database.UserCurrency{},
		&database.CurrencyTransaction{},
	); err != nil {
		t.Fatalf("migrate gacha seed tables: %v", err)
	}
	return db
}

func TestEnsureDefaultGachaContentSeedsDrawableStarterPool(t *testing.T) {
	db := setupGachaSeedTestDB(t)
	if err := db.Create(&database.Character{
		CharacterID: "seed-public-char",
		UserID:      "system_admin",
		Name:        "测试角色",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatalf("create public character: %v", err)
	}

	gachaService := NewGachaService(db)
	if err := gachaService.EnsureDefaultGachaContent(); err != nil {
		t.Fatalf("ensure default gacha content: %v", err)
	}

	pools, err := gachaService.GetPools()
	if err != nil {
		t.Fatalf("get pools: %v", err)
	}
	if len(pools) != 1 || pools[0].PoolID != "starter" {
		t.Fatalf("expected seeded starter pool, got %#v", pools)
	}

	for _, rarity := range []database.GachaRarity{database.GachaSSR, database.GachaSR, database.GachaR, database.GachaN} {
		var count int64
		if err := db.Model(&database.GachaCharacter{}).
			Where("pool_id = ? AND rarity = ?", "starter", rarity).
			Count(&count).Error; err != nil {
			t.Fatalf("count seeded %s candidates: %v", rarity, err)
		}
		if count == 0 {
			t.Fatalf("expected seeded %s candidate", rarity)
		}
	}

	if _, err := gachaService.Draw("draw-user", "starter", 1); !errors.Is(err, ErrInsufficientMoonBalance) {
		t.Fatalf("expected seeded pool to reach balance validation, got %v", err)
	}
}

func TestEnsureDefaultGachaContentDoesNotCreatePoolWithoutPublicCharacters(t *testing.T) {
	db := setupGachaSeedTestDB(t)

	gachaService := NewGachaService(db)
	if err := gachaService.EnsureDefaultGachaContent(); err != nil {
		t.Fatalf("ensure default gacha content: %v", err)
	}

	pools, err := gachaService.GetPools()
	if err != nil {
		t.Fatalf("get pools: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("expected no starter pool without public characters, got %#v", pools)
	}
}

func TestEnsureDefaultGachaContentIgnoresPublicFemaleCharacters(t *testing.T) {
	db := setupGachaSeedTestDB(t)
	if err := db.Create(&database.Character{
		CharacterID: "seed-public-female",
		UserID:      "system_admin",
		Name:        "女角色",
		Gender:      "female",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatalf("create public female character: %v", err)
	}

	gachaService := NewGachaService(db)
	if err := gachaService.EnsureDefaultGachaContent(); err != nil {
		t.Fatalf("ensure default gacha content: %v", err)
	}

	pools, err := gachaService.GetPools()
	if err != nil {
		t.Fatalf("get pools: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("expected no starter pool from female public characters, got %#v", pools)
	}
}
