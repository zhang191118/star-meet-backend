package response

type RuntimeConfigResp struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Sensitive   bool   `json:"sensitive"`
	HasValue    bool   `json:"has_value"`
	Description string `json:"description,omitempty"`
	UpdatedAt   int64  `json:"updated_at"`
}

type RuntimeAIProviderResp struct {
	BaseURL          string `json:"base_url,omitempty"`
	Model            string `json:"model,omitempty"`
	Configured       bool   `json:"configured"`
	APIKeyConfigured bool   `json:"api_key_configured"`
}

type PublicRuntimeConfigResp struct {
	ChatProvider     string                `json:"chat_provider"`
	ChatPrimaryModel string                `json:"chat_primary_model,omitempty"`
	OpenAICompat     RuntimeAIProviderResp `json:"openai_compat"`
}
