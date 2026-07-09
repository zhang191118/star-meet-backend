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

	return response.PublicRuntimeConfigResp{
		ChatProvider:     chatProvider,
		ChatPrimaryModel: chatPrimaryModel,
		OpenAICompat: response.RuntimeAIProviderResp{
			BaseURL:          baseURL,
			Model:            model,
			Configured:       openAICompatConfigured,
			APIKeyConfigured: strings.TrimSpace(apiKey) != "",
		},
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
