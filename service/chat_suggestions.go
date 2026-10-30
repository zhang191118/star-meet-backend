package service

import (
	"fmt"
	"strings"

	"star-meet-backend/model/database"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/pkg/logger"
)

func (s *ChatService) GetSuggestions(userID, characterID, lang string) ([]string, error) {
	if err := s.ensureChatCharacterAllowed(userID, characterID); err != nil {
		return nil, err
	}
	var character database.Character
	if err := s.DB.Where("character_id = ?", characterID).First(&character).Error; err != nil {
		return nil, ErrChatCharacterUnavailable
	}

	var messages []database.Message
	if err := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).
		Order("created_at DESC, id DESC").
		Limit(6).
		Find(&messages).Error; err != nil {
		return nil, err
	}

	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	langNorm := i18n.NormalizeLang(lang)
	characterName := i18n.TranslateCharacterName(character.Name, langNorm)
	langName := languageName(langNorm)

	if len(messages) == 0 {
		return defaultOpeningSuggestions(langNorm), nil
	}

	var historyBuilder strings.Builder
	for _, msg := range messages {
		if msg.Role == "user" {
			historyBuilder.WriteString("User: " + msg.Content + "\n")
		} else {
			historyBuilder.WriteString(characterName + ": " + msg.Content + "\n")
		}
	}
	historyText := historyBuilder.String()

	prompt := fmt.Sprintf(suggestionPromptTemplate, characterName, langName, historyText, langName)
	systemPrompt := "You are a chat suggestion generator."

	aiClient := s.currentOpenAICompatClient()
	if aiClient != nil {
		reply, err := aiClient.Generate(prompt, systemPrompt, 200)
		if err != nil {
			logger.Warn("suggestion generation failed", "error", err)
		} else {
			if suggestions := parseSuggestions(reply); len(suggestions) > 0 {
				return suggestions, nil
			}
		}
	}

	if s.QwenClient != nil && strings.TrimSpace(s.QwenClient.APIKey) != "" {
		reply, err := s.QwenClient.Generate(prompt, systemPrompt, 200)
		if err != nil {
			logger.Warn("suggestion generation failed (qwen)", "error", err)
		} else {
			if suggestions := parseSuggestions(reply); len(suggestions) > 0 {
				return suggestions, nil
			}
		}
	}

	return defaultFollowUpSuggestions(langNorm), nil
}

var suggestionPromptTemplate = `You generate reply suggestions for the USER in a two-person chat.
The USER is chatting with the CHARACTER below. The last CHARACTER message is the
message the USER needs to answer now.

Character: %s
Language: %s
Conversation:
%s

Generate exactly 3 different messages that the USER can send next to the CHARACTER.
Requirements:
- Write from the USER's point of view, never as the CHARACTER.
- Each suggestion must directly respond to the latest CHARACTER message.
- Keep each under 15 words, natural and casual like real texting.
- Do not write narration, analysis, labels, questions about the task, or a full
  character reply.
- Return only 3 lines, one suggestion per line, without numbering or quotes.
- Must be in %s`

func parseSuggestions(reply string) []string {
	lines := strings.Split(strings.TrimSpace(reply), "\n")
	var suggestions []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"1.", "2.", "3.", "1)", "2)", "3)", "- ", "* "} {
			line = strings.TrimPrefix(line, prefix)
		}
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(line)
		if line != "" && len([]rune(line)) <= 30 {
			suggestions = append(suggestions, line)
		}
	}
	if len(suggestions) > 3 {
		suggestions = suggestions[:3]
	}
	return suggestions
}

func languageName(lang string) string {
	switch lang {
	case "en":
		return "English"
	case "zh-Hans":
		return "Simplified Chinese"
	case "zh-Hant":
		return "Traditional Chinese"
	case "ko":
		return "Korean"
	case "ja":
		return "Japanese"
	default:
		return "English"
	}
}

func defaultOpeningSuggestions(lang string) []string {
	switch lang {
	case "zh-Hans":
		return []string{"你好呀！", "今天过得怎么样？", "在忙什么呢？"}
	case "zh-Hant":
		return []string{"你好呀！", "今天過得怎麼樣？", "在忙什麼呢？"}
	case "ko":
		return []string{"안녕!", "오늘 어딤?", "뭐 해?"}
	case "ja":
		return []string{"こんにちは！", "今日どうだった？", "何してるの？"}
	default:
		return []string{"Hi there!", "How was your day?", "What are you up to?"}
	}
}

func defaultFollowUpSuggestions(lang string) []string {
	switch lang {
	case "zh-Hans":
		return []string{"继续聊聊吧", "然后呢？", "说说你的想法"}
	case "zh-Hant":
		return []string{"繼續聊聊吧", "然後呢？", "說說你的想法"}
	case "ko":
		return []string{"계속 어기하자", "그래서?", "생각 조 들려줘"}
	case "ja":
		return []string{"もっと話そう", "それで？", "思うことを教えて"}
	default:
		return []string{"Let's keep talking", "And then?", "Share your thoughts"}
	}
}
