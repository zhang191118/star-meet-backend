package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"star-meet-backend/pkg/i18n"
	"strings"
	"time"

	"gorm.io/gorm"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
)

var structuredMemorySignalWords = []string{
	"喜欢", "不喜欢", "讨厌", "习惯", "记住", "记得", "答应", "承诺", "约定",
	"生日", "考试", "工作", "家人", "害怕", "希望", "不要", "不能", "以后",
	"like", "love", "prefer", "hate", "remember", "promise", "birthday", "exam",
}

const (
	MemoryTypeFact         = "fact"
	MemoryTypePreference   = "preference"
	MemoryTypeEmotion      = "emotion"
	MemoryTypeEvent        = "event"
	MemoryTypeRelationship = "relationship"
	MemoryTypePromise      = "promise"
	MemoryTypeBoundary     = "boundary"
)

var structuredMemoryTypes = map[string]struct{}{
	MemoryTypeFact: {}, MemoryTypePreference: {}, MemoryTypeEmotion: {},
	MemoryTypeEvent: {}, MemoryTypeRelationship: {}, MemoryTypePromise: {},
	MemoryTypeBoundary: {},
}

type StructuredMemory struct {
	Type       string  `json:"type"`
	Content    string  `json:"content"`
	Importance int     `json:"importance"`
	Confidence float64 `json:"confidence,omitempty"`
}

type StructuredMemoryExtraction struct {
	Memories []StructuredMemory `json:"memories"`
}

func parseStructuredMemoryExtraction(raw string) (StructuredMemoryExtraction, error) {
	cleaned := extractJSONBlock(raw)
	var payload StructuredMemoryExtraction
	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return StructuredMemoryExtraction{}, fmt.Errorf("invalid memory extraction json: %w", err)
	}

	result := StructuredMemoryExtraction{Memories: make([]StructuredMemory, 0, len(payload.Memories))}
	byKey := make(map[string]int, len(payload.Memories))
	for _, item := range payload.Memories {
		typ := strings.ToLower(strings.TrimSpace(item.Type))
		content := strings.TrimSpace(item.Content)
		if typ == "" || content == "" {
			return StructuredMemoryExtraction{}, errors.New("memory item requires type and content")
		}
		if _, ok := structuredMemoryTypes[typ]; !ok {
			continue
		}
		if len([]rune(content)) > 500 {
			content = trimTextRunes(content, 500)
		}
		if item.Importance < 1 {
			item.Importance = 3
		}
		if item.Importance > 5 {
			item.Importance = 5
		}
		if item.Confidence < 0 {
			item.Confidence = 0
		}
		if item.Confidence > 1 {
			item.Confidence = 1
		}
		item.Type = typ
		item.Content = content
		key := typ + "\x00" + content
		if existing, ok := byKey[key]; ok {
			if item.Importance > result.Memories[existing].Importance {
				result.Memories[existing] = item
			}
			continue
		}
		byKey[key] = len(result.Memories)
		result.Memories = append(result.Memories, item)
		if len(result.Memories) >= 10 {
			break
		}
	}
	return result, nil
}

func buildMemoryExtractionPrompt(userMessage, assistantMessage string) string {
	return fmt.Sprintf(`你是长期记忆整理器。只输出合法 JSON，不要 markdown、不要解释。
请从下面这一轮对话中提取未来仍然有用、且明确被用户表达或双方确认的记忆。
允许的 type 只有：fact、preference、emotion、event、relationship、promise、boundary。
没有值得保存的内容时返回 {"memories":[]}。
每条 content 使用简短陈述句，importance 为 1-5，confidence 为 0-1。
不要把模型猜测、临时寒暄或角色自说自话当成用户事实。
输出形状：{"memories":[{"type":"preference","content":"...","importance":3,"confidence":0.9}]}

用户消息：%s
角色回复：%s`, userMessage, assistantMessage)
}

func shouldExtractStructuredMemory(userMessage, assistantMessage string) bool {
	text := i18n.NormalizeDetectionText(strings.TrimSpace(userMessage))
	if len([]rune(strings.TrimSpace(userMessage))) < 4 {
		return false
	}
	for _, signal := range structuredMemorySignalWords {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func (s *ChatService) extractStructuredMemories(userMessage, assistantMessage string) (StructuredMemoryExtraction, error) {
	client := s.currentOpenAICompatClient()
	if client == nil {
		return StructuredMemoryExtraction{}, errors.New("structured memory model unavailable")
	}
	raw, err := client.Generate(
		buildMemoryExtractionPrompt(userMessage, assistantMessage),
		"你负责提取长期记忆，必须遵守用户隐私设置。",
		s.contextBudget().DirectorOutputTokens,
	)
	if err != nil {
		return StructuredMemoryExtraction{}, err
	}
	return parseStructuredMemoryExtraction(raw)
}

func (s *ChatService) persistStructuredMemories(userID, characterID string, extraction StructuredMemoryExtraction) error {
	return s.persistStructuredMemoriesObserved(userID, characterID, extraction, time.Now())
}

func (s *ChatService) persistStructuredMemoriesObserved(userID, characterID string, extraction StructuredMemoryExtraction, observedAt time.Time) error {
	if s == nil || s.DB == nil || userID == "" || characterID == "" {
		return nil
	}
	for _, item := range extraction.Memories {
		if _, ok := structuredMemoryTypes[item.Type]; !ok || strings.TrimSpace(item.Content) == "" {
			continue
		}
		var existing database.Memory
		err := s.DB.Where("user_id = ? AND character_id = ? AND memory_type = ? AND content = ?", userID, characterID, item.Type, item.Content).First(&existing).Error
		if err == nil {
			if item.Importance > existing.Importance {
				if err := s.DB.Model(&existing).Update("importance", item.Importance).Error; err != nil {
					return err
				}
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if _, err := s.memoryStore().AddMemory(request.AddMemoryReq{UserID: userID, CharacterID: characterID, Content: item.Content, MemoryType: item.Type, Source: "auto", Importance: item.Importance, ObservedAt: observedAt}); err != nil {
			return err
		}
	}
	return nil
}
