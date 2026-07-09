package service

import (
	"testing"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRuntimeConfigTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.RuntimeConfig{}); err != nil {
		t.Fatalf("migrate runtime configs: %v", err)
	}
	return db
}

func TestSeedDefaultsCreatesMissingRuntimeConfigs(t *testing.T) {
	db := setupRuntimeConfigTestDB(t)
	service := &RuntimeConfigService{DB: db}

	err := service.SeedDefaults([]RuntimeConfigDefault{
		{
			Key:         RuntimeConfigOpenAICompatBaseURL,
			Value:       "https://ai98pro.xyz",
			Sensitive:   false,
			Description: "base url",
		},
		{
			Key:         RuntimeConfigOpenAICompatAPIKey,
			Value:       "test-secret",
			Sensitive:   true,
			Description: "api key",
		},
		{
			Key:         RuntimeConfigOpenAICompatModel,
			Value:       "grok-4.3-fast",
			Sensitive:   false,
			Description: "model",
		},
	})
	if err != nil {
		t.Fatalf("seed defaults: %v", err)
	}

	baseURL, apiKey, model := service.OpenAICompatValues()
	if baseURL != "https://ai98pro.xyz" || apiKey != "test-secret" || model != "grok-4.3-fast" {
		t.Fatalf("unexpected openai compat values: %q %q %q", baseURL, apiKey, model)
	}
}

func TestSeedDefaultsDoesNotOverwriteExistingValues(t *testing.T) {
	db := setupRuntimeConfigTestDB(t)
	service := &RuntimeConfigService{DB: db}

	if _, err := service.Upsert(RuntimeConfigOpenAICompatModel, "custom-model", false, ""); err != nil {
		t.Fatalf("upsert existing model: %v", err)
	}
	if err := service.SeedDefaults([]RuntimeConfigDefault{
		{
			Key:         RuntimeConfigOpenAICompatModel,
			Value:       "grok-4.3-fast",
			Sensitive:   false,
			Description: "model",
		},
	}); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}

	model := service.GetOrDefault(RuntimeConfigOpenAICompatModel, "")
	if model != "custom-model" {
		t.Fatalf("expected existing model to be preserved, got %q", model)
	}
}

func TestPublicConfigDoesNotExposeAPIKey(t *testing.T) {
	db := setupRuntimeConfigTestDB(t)
	service := &RuntimeConfigService{DB: db}

	for _, item := range []RuntimeConfigDefault{
		{Key: RuntimeConfigOpenAICompatBaseURL, Value: "https://ai98pro.xyz", Sensitive: false},
		{Key: RuntimeConfigOpenAICompatAPIKey, Value: "test-secret", Sensitive: true},
		{Key: RuntimeConfigOpenAICompatModel, Value: "grok-4.3-fast", Sensitive: false},
	} {
		if _, err := service.Upsert(item.Key, item.Value, item.Sensitive, item.Description); err != nil {
			t.Fatalf("upsert %s: %v", item.Key, err)
		}
	}

	resp := service.PublicConfig()
	if !resp.OpenAICompat.Configured || !resp.OpenAICompat.APIKeyConfigured {
		t.Fatalf("expected openai compat to be configured: %+v", resp.OpenAICompat)
	}
	if resp.ChatProvider != "openai_compat" || resp.ChatPrimaryModel != "grok-4.3-fast" {
		t.Fatalf("unexpected chat provider/model: %+v", resp)
	}
}
