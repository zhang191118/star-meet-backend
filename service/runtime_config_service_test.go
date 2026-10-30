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

func TestChatQuotaDefaultsSeedWithoutOverwritingEdits(t *testing.T) {
	db := setupRuntimeConfigTestDB(t)
	svc := &RuntimeConfigService{DB: db}

	if err := svc.SeedDefaults(ChatQuotaRuntimeDefaults()); err != nil {
		t.Fatalf("seed chat quota defaults: %v", err)
	}

	settings := svc.ChatQuotaSettings()
	if !settings.Enabled || settings.FreeDailyMessages != DefaultChatQuotaFreeDailyMessages || !settings.MemberUnlimited {
		t.Fatalf("unexpected seeded quota settings: %+v", settings)
	}
	if settings.ResetTimezone != DefaultChatQuotaResetTimezone {
		t.Fatalf("unexpected reset timezone: %q", settings.ResetTimezone)
	}

	if _, err := svc.Upsert(RuntimeConfigChatQuotaFreeDailyMessages, "8", false, "custom daily cap"); err != nil {
		t.Fatalf("upsert custom daily cap: %v", err)
	}
	if err := svc.SeedDefaults(ChatQuotaRuntimeDefaults()); err != nil {
		t.Fatalf("reseed chat quota defaults: %v", err)
	}

	settings = svc.ChatQuotaSettings()
	if settings.FreeDailyMessages != 8 {
		t.Fatalf("expected admin edit to be preserved, got %d", settings.FreeDailyMessages)
	}
}

func TestChatContextBudgetDefaultsAndOverrides(t *testing.T) {
	db := setupRuntimeConfigTestDB(t)
	svc := &RuntimeConfigService{DB: db}
	if err := svc.SeedDefaults(ChatContextRuntimeDefaults()); err != nil {
		t.Fatalf("seed chat context defaults: %v", err)
	}

	defaults := svc.ChatContextBudget()
	if defaults.ModelContextTokens != 8192 || defaults.HistoryTokens != 2200 ||
		defaults.SummaryTokens != 600 || defaults.MemoryTokens != 500 ||
		defaults.DirectorHistoryTokens != 350 || defaults.DirectorOutputTokens != 350 || defaults.OutputTokens != 800 ||
		defaults.SafetyMarginTokens != 400 || defaults.FixedPromptTokens != 1200 || defaults.DirectorRefreshInterval != 2 {
		t.Fatalf("unexpected chat context defaults: %+v", defaults)
	}

	if _, err := svc.Upsert(RuntimeConfigChatContextHistoryTokens, "2600", false, "custom history"); err != nil {
		t.Fatalf("override history budget: %v", err)
	}
	if got := svc.ChatContextBudget().HistoryTokens; got != 2600 {
		t.Fatalf("expected overridden history budget 2600, got %d", got)
	}
}

func TestChatContextBudgetCapsHistoryAgainstModelWindow(t *testing.T) {
	budget := DefaultChatContextBudget()
	if got := budget.EffectiveHistoryTokens(); got != 2200 {
		t.Fatalf("expected 8K budget to retain 2200 history tokens, got %d", got)
	}
	budget.ModelContextTokens = 4096
	if got := budget.EffectiveHistoryTokens(); got != 596 {
		t.Fatalf("expected 4K budget to cap history at 596, got %d", got)
	}
}

func TestGetBoolOrDefaultParsesCommonValues(t *testing.T) {
	db := setupRuntimeConfigTestDB(t)
	svc := &RuntimeConfigService{DB: db}

	if _, err := svc.Upsert(RuntimeConfigChatQuotaEnabled, "false", false, ""); err != nil {
		t.Fatalf("upsert enabled=false: %v", err)
	}
	if svc.GetBoolOrDefault(RuntimeConfigChatQuotaEnabled, true) {
		t.Fatal("expected enabled=false")
	}
	if _, err := svc.Upsert(RuntimeConfigChatQuotaMemberUnlimited, "YES", false, ""); err != nil {
		t.Fatalf("upsert unlimited=yes: %v", err)
	}
	if !svc.GetBoolOrDefault(RuntimeConfigChatQuotaMemberUnlimited, false) {
		t.Fatal("expected member_unlimited=true")
	}
	if svc.GetBoolOrDefault("chat.quota.missing", true) != true {
		t.Fatal("expected missing key to use default true")
	}
}
