package service

import (
	"strconv"

	"star-meet-backend/model/database"
)

const (
	RuntimeConfigChatContextModelTokens           = "chat.context.model_tokens"
	RuntimeConfigChatContextHistoryTokens         = "chat.context.history_tokens"
	RuntimeConfigChatContextSummaryTokens         = "chat.context.summary_tokens"
	RuntimeConfigChatContextMemoryTokens          = "chat.context.memory_tokens"
	RuntimeConfigChatContextDirectorHistoryTokens = "chat.context.director_history_tokens"
	RuntimeConfigChatContextDirectorOutputTokens  = "chat.context.director_output_tokens"
	RuntimeConfigChatContextOutputTokens          = "chat.context.output_tokens"
	RuntimeConfigChatContextSafetyMarginTokens    = "chat.context.safety_margin_tokens"
	RuntimeConfigChatContextFixedPromptTokens     = "chat.context.fixed_prompt_tokens"

	RuntimeConfigChatContextDirectorRefreshInterval = "chat.context.director_refresh_interval"
)

// ChatContextBudget controls the input and output allocations for one chat
// turn. The model window is shared by fixed character data, history, memory,
// and the generated response.
type ChatContextBudget struct {
	ModelContextTokens      int
	HistoryTokens           int
	SummaryTokens           int
	MemoryTokens            int
	DirectorHistoryTokens   int
	DirectorOutputTokens    int
	DirectorRefreshInterval int
	OutputTokens            int
	SafetyMarginTokens      int
	FixedPromptTokens       int
}

func DefaultChatContextBudget() ChatContextBudget {
	return ChatContextBudget{
		ModelContextTokens:      8192,
		HistoryTokens:           2200,
		SummaryTokens:           600,
		MemoryTokens:            500,
		DirectorHistoryTokens:   350,
		DirectorOutputTokens:    350,
		DirectorRefreshInterval: 2,
		OutputTokens:            800,
		SafetyMarginTokens:      400,
		FixedPromptTokens:       1200,
	}
}

func ChatContextRuntimeDefaults() []RuntimeConfigDefault {
	defaults := DefaultChatContextBudget()
	return []RuntimeConfigDefault{
		{Key: RuntimeConfigChatContextModelTokens, Value: strconv.Itoa(defaults.ModelContextTokens), Description: "模型单轮上下文窗口预算"},
		{Key: RuntimeConfigChatContextHistoryTokens, Value: strconv.Itoa(defaults.HistoryTokens), Description: "普通聊天最近历史预算"},
		{Key: RuntimeConfigChatContextSummaryTokens, Value: strconv.Itoa(defaults.SummaryTokens), Description: "会话滚动摘要预算"},
		{Key: RuntimeConfigChatContextMemoryTokens, Value: strconv.Itoa(defaults.MemoryTokens), Description: "长期记忆注入预算"},
		{Key: RuntimeConfigChatContextDirectorHistoryTokens, Value: strconv.Itoa(defaults.DirectorHistoryTokens), Description: "导演层历史上下文预算"},
		{Key: RuntimeConfigChatContextDirectorOutputTokens, Value: strconv.Itoa(defaults.DirectorOutputTokens), Description: "导演层最大输出预算"},
		{Key: RuntimeConfigChatContextOutputTokens, Value: strconv.Itoa(defaults.OutputTokens), Description: "普通聊天最大输出预算"},
		{Key: RuntimeConfigChatContextSafetyMarginTokens, Value: strconv.Itoa(defaults.SafetyMarginTokens), Description: "上下文安全余量预算"},
		{Key: RuntimeConfigChatContextFixedPromptTokens, Value: strconv.Itoa(defaults.FixedPromptTokens), Description: "角色资料与系统提示预留预算"},
		{Key: RuntimeConfigChatContextDirectorRefreshInterval, Value: strconv.Itoa(defaults.DirectorRefreshInterval), Description: "导演层刷新间隔（每N轮重新生成一次叙事指导）"},
	}
}

func (s *RuntimeConfigService) ChatContextBudget() ChatContextBudget {
	defaults := DefaultChatContextBudget()
	if s == nil || s.DB == nil {
		return defaults
	}
	keys := []string{
		RuntimeConfigChatContextModelTokens, RuntimeConfigChatContextHistoryTokens,
		RuntimeConfigChatContextSummaryTokens, RuntimeConfigChatContextMemoryTokens,
		RuntimeConfigChatContextDirectorHistoryTokens, RuntimeConfigChatContextDirectorOutputTokens,
		RuntimeConfigChatContextOutputTokens, RuntimeConfigChatContextSafetyMarginTokens,
		RuntimeConfigChatContextFixedPromptTokens, RuntimeConfigChatContextDirectorRefreshInterval,
	}
	var rows []database.RuntimeConfig
	values := make(map[string]string, len(keys))
	if err := s.DB.Where("config_key IN ?", keys).Find(&rows).Error; err == nil {
		for _, row := range rows {
			values[row.Key] = row.Value
		}
	}
	getInt := func(key string, fallback int) int {
		value, err := strconv.Atoi(values[key])
		if err != nil {
			return fallback
		}
		return positiveRuntimeInt(value, fallback)
	}
	return ChatContextBudget{
		ModelContextTokens:      getInt(RuntimeConfigChatContextModelTokens, defaults.ModelContextTokens),
		HistoryTokens:           getInt(RuntimeConfigChatContextHistoryTokens, defaults.HistoryTokens),
		SummaryTokens:           getInt(RuntimeConfigChatContextSummaryTokens, defaults.SummaryTokens),
		MemoryTokens:            getInt(RuntimeConfigChatContextMemoryTokens, defaults.MemoryTokens),
		DirectorHistoryTokens:   getInt(RuntimeConfigChatContextDirectorHistoryTokens, defaults.DirectorHistoryTokens),
		DirectorOutputTokens:    getInt(RuntimeConfigChatContextDirectorOutputTokens, defaults.DirectorOutputTokens),
		DirectorRefreshInterval: getInt(RuntimeConfigChatContextDirectorRefreshInterval, defaults.DirectorRefreshInterval),
		OutputTokens:            getInt(RuntimeConfigChatContextOutputTokens, defaults.OutputTokens),
		SafetyMarginTokens:      getInt(RuntimeConfigChatContextSafetyMarginTokens, defaults.SafetyMarginTokens),
		FixedPromptTokens:       getInt(RuntimeConfigChatContextFixedPromptTokens, defaults.FixedPromptTokens),
	}
}

// EffectiveHistoryTokens prevents history from consuming space reserved for
// fixed prompts, summary, memory, output, and safety headroom.
func (b ChatContextBudget) EffectiveHistoryTokens() int {
	available := b.ModelContextTokens - b.FixedPromptTokens - b.SummaryTokens - b.MemoryTokens - b.OutputTokens - b.SafetyMarginTokens
	if available < 1 {
		return 1
	}
	if b.HistoryTokens > available {
		return available
	}
	return b.HistoryTokens
}

func positiveRuntimeInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func (s *ChatService) contextBudget() ChatContextBudget {
	if s != nil && s.RuntimeConfig != nil {
		return s.RuntimeConfig.ChatContextBudget()
	}
	return DefaultChatContextBudget()
}
