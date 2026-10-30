package service

import (
	"errors"
	"strconv"
	"strings"

	"star-meet-backend/model/database"
	"star-meet-backend/model/response"

	"gorm.io/gorm"
)

const redactedSecret = "********"

const (
	RuntimeConfigOpenAICompatBaseURL = "openai_compat.base_url"
	RuntimeConfigOpenAICompatAPIKey  = "openai_compat.api_key"
	RuntimeConfigOpenAICompatModel   = "openai_compat.model"

	RuntimeConfigResendAPIKey = "resend.api_key"
	RuntimeConfigResendFrom   = "resend.from"

	RuntimeConfigLineChannelID     = "line.channel_id"
	RuntimeConfigLineChannelSecret = "line.channel_secret"

	RuntimeConfigChatQuotaEnabled           = "chat.quota.enabled"
	RuntimeConfigChatQuotaFreeDailyMessages = "chat.quota.free_daily_messages"
	RuntimeConfigChatQuotaFreeTrialMessages = "chat.quota.free_trial_messages"
	RuntimeConfigChatQuotaMemberUnlimited   = "chat.quota.member_unlimited"
	RuntimeConfigChatQuotaResetTimezone     = "chat.quota.reset_timezone"
	RuntimeConfigChatQuotaFreeDailyDraws    = "chat.quota.free_daily_draws"
	RuntimeConfigChatQuotaMemberDailyDraws  = "chat.quota.member_daily_draws"
)

const (
	DefaultChatQuotaFreeDailyMessages = 20
	DefaultChatQuotaFreeTrialMessages = 0
	DefaultChatQuotaFreeDailyDraws    = 3
	DefaultChatQuotaMemberDailyDraws  = 20
	DefaultChatQuotaResetTimezone     = "Asia/Taipei"
)

var ErrRuntimeConfigNotFound = errors.New("runtime config not found")

type RuntimeConfigService struct {
	DB *gorm.DB
}

type RuntimeConfigDefault struct {
	Key         string
	Value       string
	Sensitive   bool
	Description string
}

func (s *RuntimeConfigService) Get(key string) (string, error) {
	key = normalizeRuntimeConfigKey(key)
	if key == "" {
		return "", ErrRuntimeConfigNotFound
	}

	var config database.RuntimeConfig
	if err := s.DB.Where("config_key = ?", key).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrRuntimeConfigNotFound
		}
		return "", err
	}
	return config.Value, nil
}

func (s *RuntimeConfigService) GetOrDefault(key, defaultValue string) string {
	value, err := s.Get(key)
	if err != nil || strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func (s *RuntimeConfigService) GetIntOrDefault(key string, defaultValue int) int {
	value := s.GetOrDefault(key, "")
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func (s *RuntimeConfigService) GetBoolOrDefault(key string, defaultValue bool) bool {
	switch strings.ToLower(strings.TrimSpace(s.GetOrDefault(key, ""))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

func (s *RuntimeConfigService) SeedDefaults(defaults []RuntimeConfigDefault) error {
	for _, item := range defaults {
		key := normalizeRuntimeConfigKey(item.Key)
		if key == "" {
			continue
		}

		var config database.RuntimeConfig
		err := s.DB.Where("config_key = ?", key).First(&config).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			config = database.RuntimeConfig{
				Key:         key,
				Value:       item.Value,
				Sensitive:   item.Sensitive,
				Description: item.Description,
			}
			if err := s.DB.Create(&config).Error; err != nil {
				return err
			}
			continue
		}

		changed := false
		if config.Sensitive != item.Sensitive {
			config.Sensitive = item.Sensitive
			changed = true
		}
		if config.Description == "" && item.Description != "" {
			config.Description = item.Description
			changed = true
		}
		if changed {
			if err := s.DB.Save(&config).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *RuntimeConfigService) OpenAICompatValues() (baseURL, apiKey, model string) {
	return s.GetOrDefault(RuntimeConfigOpenAICompatBaseURL, ""),
		s.GetOrDefault(RuntimeConfigOpenAICompatAPIKey, ""),
		s.GetOrDefault(RuntimeConfigOpenAICompatModel, "")
}

type ChatQuotaSettings struct {
	Enabled           bool
	FreeDailyMessages int
	FreeTrialMessages int
	MemberUnlimited   bool
	ResetTimezone     string
	FreeDailyDraws    int
	MemberDailyDraws  int
}

func DefaultChatQuotaSettings() ChatQuotaSettings {
	return ChatQuotaSettings{
		Enabled:           true,
		FreeDailyMessages: DefaultChatQuotaFreeDailyMessages,
		FreeTrialMessages: DefaultChatQuotaFreeTrialMessages,
		MemberUnlimited:   true,
		ResetTimezone:     DefaultChatQuotaResetTimezone,
		FreeDailyDraws:    DefaultChatQuotaFreeDailyDraws,
		MemberDailyDraws:  DefaultChatQuotaMemberDailyDraws,
	}
}

func ChatQuotaRuntimeDefaults() []RuntimeConfigDefault {
	settings := DefaultChatQuotaSettings()
	return []RuntimeConfigDefault{
		{
			Key:         RuntimeConfigChatQuotaEnabled,
			Value:       strconv.FormatBool(settings.Enabled),
			Sensitive:   false,
			Description: "是否启用免费用户每日聊天条数限制",
		},
		{
			Key:         RuntimeConfigChatQuotaFreeDailyMessages,
			Value:       strconv.Itoa(settings.FreeDailyMessages),
			Sensitive:   false,
			Description: "免费用户每天可发送的用户消息条数（不含 AI 回复，台北时间 0 点重置）",
		},
		{
			Key:         RuntimeConfigChatQuotaFreeTrialMessages,
			Value:       strconv.Itoa(settings.FreeTrialMessages),
			Sensitive:   false,
			Description: "兼容保留字段，目前仅支持 0；额外试用权益必须由服务端授权",
		},
		{
			Key:         RuntimeConfigChatQuotaMemberUnlimited,
			Value:       strconv.FormatBool(settings.MemberUnlimited),
			Sensitive:   false,
			Description: "开通会员后是否不受每日聊天条数限制",
		},
		{
			Key:         RuntimeConfigChatQuotaResetTimezone,
			Value:       settings.ResetTimezone,
			Sensitive:   false,
			Description: "每日聊天额度重置时区",
		},
		{
			Key:         RuntimeConfigChatQuotaFreeDailyDraws,
			Value:       strconv.Itoa(settings.FreeDailyDraws),
			Sensitive:   false,
			Description: "免费用户每日抽卡次数（占位，后续抽卡再接入）",
		},
		{
			Key:         RuntimeConfigChatQuotaMemberDailyDraws,
			Value:       strconv.Itoa(settings.MemberDailyDraws),
			Sensitive:   false,
			Description: "会员每日抽卡次数（占位，后续抽卡再接入）",
		},
	}
}

func (s *RuntimeConfigService) ChatQuotaSettings() ChatQuotaSettings {
	defaults := DefaultChatQuotaSettings()
	if s == nil {
		return defaults
	}

	settings := ChatQuotaSettings{
		Enabled:           s.GetBoolOrDefault(RuntimeConfigChatQuotaEnabled, defaults.Enabled),
		FreeDailyMessages: s.GetIntOrDefault(RuntimeConfigChatQuotaFreeDailyMessages, defaults.FreeDailyMessages),
		FreeTrialMessages: s.GetIntOrDefault(RuntimeConfigChatQuotaFreeTrialMessages, defaults.FreeTrialMessages),
		MemberUnlimited:   s.GetBoolOrDefault(RuntimeConfigChatQuotaMemberUnlimited, defaults.MemberUnlimited),
		ResetTimezone:     strings.TrimSpace(s.GetOrDefault(RuntimeConfigChatQuotaResetTimezone, defaults.ResetTimezone)),
		FreeDailyDraws:    s.GetIntOrDefault(RuntimeConfigChatQuotaFreeDailyDraws, defaults.FreeDailyDraws),
		MemberDailyDraws:  s.GetIntOrDefault(RuntimeConfigChatQuotaMemberDailyDraws, defaults.MemberDailyDraws),
	}
	if settings.FreeDailyMessages < 0 {
		settings.FreeDailyMessages = defaults.FreeDailyMessages
	}
	if settings.FreeTrialMessages < 0 {
		settings.FreeTrialMessages = defaults.FreeTrialMessages
	}
	if settings.FreeDailyDraws < 0 {
		settings.FreeDailyDraws = defaults.FreeDailyDraws
	}
	if settings.MemberDailyDraws < 0 {
		settings.MemberDailyDraws = defaults.MemberDailyDraws
	}
	if settings.ResetTimezone == "" {
		settings.ResetTimezone = defaults.ResetTimezone
	}
	return settings
}

func (s *RuntimeConfigService) PublicConfig() response.PublicRuntimeConfigResp {
	baseURL, apiKey, model := s.OpenAICompatValues()
	openAICompatConfigured := strings.TrimSpace(baseURL) != "" &&
		strings.TrimSpace(apiKey) != "" &&
		strings.TrimSpace(model) != ""

	chatProvider := ""
	chatPrimaryModel := ""
	if openAICompatConfigured {
		chatProvider = "openai_compat"
		chatPrimaryModel = model
	}

	// 背景音乐总开关
	musicEnabled := s.GetOrDefault("music.enabled", "true") == "true"

	return response.PublicRuntimeConfigResp{
		ChatProvider:     chatProvider,
		ChatPrimaryModel: chatPrimaryModel,
		OpenAICompat: response.RuntimeAIProviderResp{
			BaseURL:          baseURL,
			Model:            model,
			Configured:       openAICompatConfigured,
			APIKeyConfigured: strings.TrimSpace(apiKey) != "",
		},
		MusicEnabled: musicEnabled,
	}
}

func (s *RuntimeConfigService) List(reveal bool) ([]response.RuntimeConfigResp, error) {
	var configs []database.RuntimeConfig
	if err := s.DB.Order("config_key ASC").Find(&configs).Error; err != nil {
		return nil, err
	}

	result := make([]response.RuntimeConfigResp, 0, len(configs))
	for _, config := range configs {
		result = append(result, runtimeConfigResponse(config, reveal))
	}
	return result, nil
}

func (s *RuntimeConfigService) GetResponse(key string, reveal bool) (*response.RuntimeConfigResp, error) {
	key = normalizeRuntimeConfigKey(key)
	if key == "" {
		return nil, ErrRuntimeConfigNotFound
	}

	var config database.RuntimeConfig
	if err := s.DB.Where("config_key = ?", key).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRuntimeConfigNotFound
		}
		return nil, err
	}

	resp := runtimeConfigResponse(config, reveal)
	return &resp, nil
}

func (s *RuntimeConfigService) Upsert(key, value string, sensitive bool, description string) (*response.RuntimeConfigResp, error) {
	key = normalizeRuntimeConfigKey(key)
	if key == RuntimeConfigChatQuotaFreeTrialMessages && strings.TrimSpace(value) != "0" {
		return nil, errors.New("extra trial quota is not enabled; use server-managed membership entitlements")
	}
	if key == "" {
		return nil, errors.New("config key is required")
	}

	var config database.RuntimeConfig
	err := s.DB.Where("config_key = ?", key).First(&config).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		config = database.RuntimeConfig{
			Key:         key,
			Value:       value,
			Sensitive:   sensitive,
			Description: description,
		}
		if err := s.DB.Create(&config).Error; err != nil {
			return nil, err
		}
		resp := runtimeConfigResponse(config, false)
		return &resp, nil
	}

	config.Value = value
	config.Sensitive = sensitive
	config.Description = description
	if err := s.DB.Save(&config).Error; err != nil {
		return nil, err
	}
	resp := runtimeConfigResponse(config, false)
	return &resp, nil
}

func runtimeConfigResponse(config database.RuntimeConfig, reveal bool) response.RuntimeConfigResp {
	value := config.Value
	if config.Sensitive && !reveal && value != "" {
		value = redactedSecret
	}
	return response.RuntimeConfigResp{
		Key:         config.Key,
		Value:       value,
		Sensitive:   config.Sensitive,
		HasValue:    config.Value != "",
		Description: config.Description,
		UpdatedAt:   config.UpdatedAt.Unix(),
	}
}

func normalizeRuntimeConfigKey(key string) string {
	return strings.TrimSpace(strings.ToLower(key))
}
