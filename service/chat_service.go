package service

import (
	"context"
	"errors"
	"fmt"
	"star-meet-backend/core/deepseek"
	"star-meet-backend/core/embedding"
	"star-meet-backend/core/emotion"
	"star-meet-backend/core/gemini"
	"star-meet-backend/core/mem0"
	"star-meet-backend/core/minimax"
	openai_compat "star-meet-backend/core/openai_compat"
	"star-meet-backend/core/qdrant"
	"star-meet-backend/core/qwen"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/pkg/logger"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrChatCharacterUnavailable = errors.New("chat character unavailable")
	ErrChatCharacterBlocked     = errors.New("chat character blocked")
	ErrAIServiceUnavailable     = errors.New("AI 服务暂时不可用，请稍后重试")
	ErrChatReplyInterrupted     = errors.New("AI 回复中断，请重试")
)

type ChatService struct {
	DB                 *gorm.DB
	Redis              *redis.Client
	Mem0Client         *mem0.Client
	QwenClient         *qwen.Client
	MiniMaxClient      *minimax.Client
	OpenAICompatClient *openai_compat.Client
	DeepSeekClient     *deepseek.Client
	GeminiClient       *gemini.Client
	EmbeddingClient    *embedding.Client
	QdrantClient       *qdrant.Client
	KnowledgeService   *KnowledgeService
	RuntimeConfig      *RuntimeConfigService
	Notification       *NotificationService
	Director           *DirectorService
	Memory             *MemoryService
	Analytics          *AnalyticsService
}

// SendMessage 发送消息
func (s *ChatService) SendMessage(userID, characterID, message, msgType string) (*response.SendMessageResp, error) {
	return s.SendMessageWithIDs(userID, characterID, message, msgType, "", "")
}

// SendMessageWithIDs 发送消息，并在前端提供消息ID时支持弱网重试幂等。
func (s *ChatService) SendMessageWithIDs(userID, characterID, message, msgType, userMsgID, replyMsgID string) (*response.SendMessageResp, error) {
	return s.SendMessageWithIDsContext(context.Background(), userID, characterID, message, msgType, userMsgID, replyMsgID)
}

func (s *ChatService) SendMessageWithIDsContext(ctx context.Context, userID, characterID, message, msgType, userMsgID, replyMsgID string) (*response.SendMessageResp, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	resolvedCharacterID, err := s.ResolveChatCharacterID(userID, characterID)
	if err != nil {
		return nil, err
	}
	characterID = resolvedCharacterID
	if err := s.checkNewUserMessageQuota(userID, characterID, userMsgID, replyMsgID); err != nil {
		return nil, err
	}
	if s.Notification != nil {
		_ = s.Notification.CancelPendingProactiveEvents(userID, characterID)
	}
	// 1. 获取角色信息
	var character database.Character
	if err := s.DB.Where("character_id = ?", characterID).First(&character).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChatCharacterUnavailable
		}
		return nil, err
	}

	// 2. 情绪识别
	userEmotion := emotion.Detect(message)

	// 2.5 首次进入：铺故事背景 + 角色首句话
	s.ensureOpeningSeeded(userID, character)

	// 3. 获取会话ID
	sessionID := s.getOrCreateSession(userID, characterID)

	// 4. 保存用户消息
	messageID := userMsgID
	if messageID == "" {
		messageID = uuid.New().String()
	}
	replyID := replyMsgID
	if replyID == "" {
		replyID = uuid.New().String()
	}

	if replyMsgID != "" {
		var existingReply database.Message
		if err := s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", replyMsgID, userID, characterID, "assistant").First(&existingReply).Error; err == nil {
			return &response.SendMessageResp{
				MessageID:    existingReply.MessageID,
				Reply:        existingReply.Content,
				ReplyType:    existingReply.MessageType,
				UserEmotion:  userEmotion,
				ReplyEmotion: existingReply.ReplyEmotion,
				DirectorNote: existingReply.DirectorNote,
				CreatedAt:    existingReply.CreatedAt.Unix(),
			}, nil
		}
	}

	var existingUserMsg database.Message
	if err := s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", messageID, userID, characterID, "user").First(&existingUserMsg).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		userMsg := database.Message{
			MessageID:   messageID,
			SessionID:   sessionID,
			CharacterID: characterID,
			UserID:      userID,
			Role:        "user",
			Content:     message,
			MessageType: msgType,
			UserEmotion: userEmotion,
		}
		if err := s.DB.Create(&userMsg).Error; err != nil {
			return nil, err
		}
		s.recordTurn("user_message_accepted", userID, characterID, messageID)
	} else if err != nil {
		return nil, err
	}

	// 5. 构建 Prompt 并调用真实 AI 服务。
	reply, replyEmotion, directorNote, err := s.generateReplyContext(ctx, userID, character, message, userEmotion)
	if err != nil {
		logger.Error("AI生成回复失败", "error", err, "userID", userID, "characterID", characterID)
		return nil, err
	}

	// 6. 保存AI回复
	aiMsg := database.Message{
		MessageID:    replyID,
		SessionID:    sessionID,
		CharacterID:  characterID,
		UserID:       userID,
		Role:         "assistant",
		Content:      reply,
		MessageType:  "text",
		ReplyEmotion: replyEmotion,
		DirectorNote: directorNote,
	}
	if err := s.DB.Create(&aiMsg).Error; err != nil {
		var existingReply database.Message
		if replyMsgID != "" && s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", replyMsgID, userID, characterID, "assistant").First(&existingReply).Error == nil {
			return &response.SendMessageResp{
				MessageID:    existingReply.MessageID,
				Reply:        existingReply.Content,
				ReplyType:    existingReply.MessageType,
				UserEmotion:  userEmotion,
				ReplyEmotion: existingReply.ReplyEmotion,
				DirectorNote: existingReply.DirectorNote,
				CreatedAt:    existingReply.CreatedAt.Unix(),
			}, nil
		}
		return nil, err
	}

	s.recordTurn("ai_reply_completed", userID, characterID, messageID)

	// 7. 更新会话
	now := time.Now()
	s.DB.Model(&database.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]interface{}{
			"last_message":    reply,
			"last_message_at": now,
		})
	_ = s.updateSessionContextSummary(sessionID, message, reply, s.contextBudget().SummaryTokens)

	// 8. 异步写入记忆（不阻塞响应）
	go s.asyncAddMemory(userID, characterID, message, reply)

	// 9. 更新角色聊天次数
	s.DB.Model(&character).Update("chat_count", gorm.Expr("chat_count + 1"))

	// 10. 更新用户统计
	s.DB.Model(&database.User{}).Where("user_id = ?", userID).Update("msg_count", gorm.Expr("msg_count + 2"))

	// 11. 亲密度增长（服务端权威）
	if s.Notification != nil {
		var notifUser database.User
		notifLang := "en"
		if s.DB.Select("language").Where("user_id = ?", userID).First(&notifUser).Error == nil {
			notifLang = i18n.NormalizeLang(notifUser.Language)
		}
		s.Notification.AddChatAffection(userID, characterID, i18n.TranslateCharacterName(character.Name, notifLang), notifLang, 1)
	}

	return &response.SendMessageResp{
		MessageID:    replyID,
		Reply:        reply,
		ReplyType:    "text",
		UserEmotion:  userEmotion,
		ReplyEmotion: replyEmotion,
		DirectorNote: directorNote,
		CreatedAt:    now.Unix(),
	}, nil
}

// getCurrentTimeContext 获取当前时间上下文描述 (台湾繁体中文)
func getCurrentTimeContext() string {
	// 使用台湾时区
	location, _ := time.LoadLocation("Asia/Taipei")
	now := time.Now().In(location)
	hour := now.Hour()
	isWeekday := now.Weekday() >= 1 && now.Weekday() <= 5

	// 繁体中文日期格式
	dateStr := now.Format("2006年1月2日")
	weekdayStr := getWeekdayCN(now.Weekday())

	var timePeriod, activity string

	switch {
	case hour >= 6 && hour < 8:
		timePeriod = "清晨"
		activity = "剛起床，正在洗漱、吃早餐，準備開始新的一天"
	case hour >= 8 && hour < 12:
		if isWeekday {
			timePeriod = "上午"
			activity = "正在上班/上課"
		} else {
			timePeriod = "上午"
			activity = "剛睡醒，在家休息或者出門辦事"
		}
	case hour >= 12 && hour < 14:
		timePeriod = "中午"
		activity = "正在吃午餐，休息時間"
	case hour >= 14 && hour < 18:
		if isWeekday {
			timePeriod = "下午"
			activity = "正在上班/上課"
		} else {
			timePeriod = "下午"
			activity = "在休息或者出門逛街"
		}
	case hour >= 18 && hour < 20:
		timePeriod = "傍晚"
		activity = "剛下班/放學，正在回家或者吃晚餐"
	case hour >= 20 && hour < 22:
		timePeriod = "晚上"
		activity = "吃完晚餐，在放鬆休息（追劇/玩遊戲/聊天）"
	case hour >= 22 || hour < 1:
		timePeriod = "晚上"
		activity = "準備休息，或者在學習/處理一些事情"
	case hour >= 1 && hour < 6:
		timePeriod = "凌晨"
		activity = "正在睡覺"
	default:
		timePeriod = "現在"
		activity = "有空，可以聊天"
	}

	// 工作日工作時間提醒
	if isWeekday && hour >= 9 && hour < 18 {
		activity = "正在上班呢，稍微有點忙"
	}

	return fmt.Sprintf("%s %s %s（%s），%s", dateStr, weekdayStr, now.Format("15:04"), timePeriod, activity)
}

// getWeekdayCN 获取中文星期几
func getWeekdayCN(weekday time.Weekday) string {
	weekdays := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	return weekdays[weekday]
}

// getChatHistory 获取最近聊天历史作为上下文
// ensureOpeningSeeded 首次进入会话时铺开场：故事背景卡片(narrative) + 角色第一句话。
// 幂等：已有任何消息则跳过；角色无开场内容则跳过。
func (s *ChatService) ensureOpeningSeeded(userID string, character database.Character) {
	if strings.TrimSpace(character.FirstSituation) == "" &&
		strings.TrimSpace(character.Narrative) == "" &&
		strings.TrimSpace(character.FirstWords) == "" {
		return
	}
	var count int64
	s.DB.Model(&database.Message{}).Where("user_id = ? AND character_id = ?", userID, character.CharacterID).Count(&count)
	if count > 0 {
		return
	}
	profile := localizedProfileFor(character, s.userProfileLang(userID))
	openingStory := strings.TrimSpace(profile.FirstSituation)
	if openingStory == "" {
		// Older characters may not have the Crushie-style opening scene yet.
		openingStory = strings.TrimSpace(profile.Narrative)
	}
	openingWords := strings.TrimSpace(profile.FirstWords)
	if openingWords == "" && openingStory == "" {
		return
	}
	sessionID := s.getOrCreateSession(userID, character.CharacterID)
	now := time.Now()

	msgs := make([]database.Message, 0, 2)
	if openingStory != "" {
		msgs = append(msgs, database.Message{
			MessageID:   uuid.New().String(),
			SessionID:   sessionID,
			CharacterID: character.CharacterID,
			UserID:      userID,
			Role:        "assistant",
			Content:     openingStory,
			MessageType: "narrative",
			CreatedAt:   now,
		})
	}
	if openingWords != "" {
		msgs = append(msgs, database.Message{
			MessageID:   uuid.New().String(),
			SessionID:   sessionID,
			CharacterID: character.CharacterID,
			UserID:      userID,
			Role:        "assistant",
			Content:     openingWords,
			MessageType: "text",
			CreatedAt:   now,
		})
	}
	if len(msgs) == 0 {
		return
	}
	if err := s.DB.Create(&msgs).Error; err != nil {
		logger.Warn("seed opening messages failed", "error", err)
		return
	}
	s.DB.Model(&database.Session{}).Where("session_id = ?", sessionID).Updates(map[string]interface{}{
		"last_message":    msgs[len(msgs)-1].Content,
		"last_message_at": now,
	})
}

// userProfileLang 解析资料语言：用户显式选择的语言优先，
// 地区映射仅在语言未记录时兜底（与 api 层 resolveProfileLang 的优先级一致）。
func (s *ChatService) userProfileLang(userID string) string {
	var user database.User
	if err := s.DB.Select("language", "region").Where("user_id = ?", userID).First(&user).Error; err != nil {
		return "en"
	}
	if strings.TrimSpace(user.Language) != "" {
		return i18n.NormalizeLang(user.Language)
	}
	switch user.Region {
	case "TW", "HK", "MO":
		return "zh-Hant"
	case "CN":
		return "zh-Hans"
	case "US", "CA", "AU", "GB", "NZ":
		return "en"
	default:
		return "en"
	}
}

func (s *ChatService) getChatHistory(userID, characterID string, messageCount int) string {
	var messages []database.Message
	s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).
		Where("message_type <> ?", "narrative").
		Order("created_at DESC").
		Limit(messageCount * 2). // 每个消息对算2条
		Find(&messages)

	if len(messages) == 0 {
		return "（暂无历史对话）"
	}

	// 反转顺序，从旧到新
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	var history strings.Builder
	for _, msg := range messages {
		roleName := "用户"
		if msg.Role == "assistant" {
			roleName = "你"
		}
		history.WriteString(fmt.Sprintf("%s: %s\n", roleName, msg.Content))
	}

	return history.String()
}

// selectHistoryWithinTokenBudget keeps the newest complete messages that fit
// the budget, then returns them in chronological order for natural dialogue.
// This is intentionally a cheap deterministic estimate; provider-specific
// tokenizers can be introduced later without changing the context contract.
func selectHistoryWithinTokenBudget(messages []database.Message, budget int) []database.Message {
	if budget <= 0 || len(messages) == 0 {
		return nil
	}
	selected := make([]database.Message, 0, len(messages))
	used := 0
	for i := len(messages) - 1; i >= 0; i-- {
		cost := estimateChatTokens(messages[i].Content) + 4
		if used+cost > budget {
			if len(selected) == 0 {
				selected = append(selected, messages[i])
			}
			break
		}
		selected = append(selected, messages[i])
		used += cost
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}

func estimateChatTokens(content string) int {
	return utf8.RuneCountInString(content)
}

// buildChatContext is the single context assembly path used by generation
// and the director. It combines the rolling summary with budgeted recent chat.
func (s *ChatService) buildChatContext(userID, characterID string, budget int) string {
	if budget <= 0 {
		budget = s.contextBudget().HistoryTokens
	}
	var session database.Session
	_ = s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).First(&session).Error

	var messages []database.Message
	s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).
		Where("message_type <> ?", "narrative").
		Order("created_at DESC, id DESC").
		Limit(200).
		Find(&messages)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	selected := selectHistoryWithinTokenBudget(messages, budget)

	var b strings.Builder
	if strings.TrimSpace(session.ContextSummary) != "" {
		b.WriteString("【会话摘要】\n")
		b.WriteString(trimTextRunes(strings.TrimSpace(session.ContextSummary), s.contextBudget().SummaryTokens))
		b.WriteString("\n")
	}
	if len(selected) > 0 {
		b.WriteString("【最近对话】\n")
		for _, msg := range selected {
			roleName := "用户"
			if msg.Role == "assistant" {
				roleName = "你"
			}
			b.WriteString(roleName + ": " + msg.Content + "\n")
		}
	}
	if b.Len() == 0 {
		return "（暂无历史对话）"
	}
	return strings.TrimSpace(b.String())
}

func trimTextRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[len(runes)-maxRunes:])
}

// updateSessionContextSummary maintains a bounded rolling digest between
// turns. It deliberately stays deterministic and local; semantic fact
// extraction is handled by the structured-memory stage that follows.
func (s *ChatService) updateSessionContextSummary(sessionID, userMessage, assistantMessage string, maxRunes int) error {
	if maxRunes <= 0 {
		maxRunes = 1200
	}
	var session database.Session
	if err := s.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		return err
	}
	parts := make([]string, 0, 3)
	if summary := strings.TrimSpace(session.ContextSummary); summary != "" {
		parts = append(parts, summary)
	}
	if userMessage = strings.TrimSpace(userMessage); userMessage != "" {
		parts = append(parts, "用户："+userMessage)
	}
	if assistantMessage = strings.TrimSpace(assistantMessage); assistantMessage != "" {
		parts = append(parts, "角色："+assistantMessage)
	}
	combined := strings.Join(parts, "\n")
	runes := []rune(combined)
	if len(runes) > maxRunes {
		runes = runes[len(runes)-maxRunes:]
	}
	return s.DB.Model(&database.Session{}).
		Where("session_id = ?", sessionID).
		Update("context_summary", string(runes)).Error
}

// refreshDirectorState 返回本轮对话使用的叙事指导。为控制每轮模型成本，
// 导演模型不必每轮调用：已有保存状态且关系阶段未变化时，按
// chat.context.director_refresh_interval 间隔重新生成，其余轮次复用保存的
// 状态，只刷新好感度与阶段快照（无模型调用）。
func (s *ChatService) refreshDirectorState(userID string, character database.Character, session database.Session, sessionID, userMessage string) *DirectorState {
	if s.Director == nil {
		return nil
	}
	budget := s.contextBudget()
	saved := parseDirectorState(session.DirectorState)
	currentScore := s.Director.authoritativeAffectionScore(session)
	currentStage := relationshipStageFromScore(currentScore)

	regenerate := saved == nil ||
		budget.DirectorRefreshInterval <= 1 ||
		session.MessageCount%budget.DirectorRefreshInterval == 0 ||
		(saved.RelationshipStage != "" && saved.RelationshipStage != currentStage)

	var directorState *DirectorState
	if regenerate {
		historySummary := s.buildChatContext(userID, character.CharacterID, budget.DirectorHistoryTokens)
		directorState, _ = s.Director.GenerateDirectorState(character, session, userMessage, historySummary)
		_ = s.Director.SaveDirectorState(sessionID, directorState)
	} else {
		directorState = saved
		directorState.AffectionScore = currentScore
		directorState.RelationshipStage = currentStage
		s.DB.Model(&database.Session{}).
			Where("session_id = ?", sessionID).
			Updates(map[string]interface{}{
				"relationship_stage": currentStage,
				"affection_score":    currentScore,
			})
	}
	_ = s.Director.IncrementMessageCount(sessionID)
	return directorState
}

// generateReply 生成回复
func (s *ChatService) generateReply(userID string, character database.Character, userMessage, userEmotion string) (string, string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return s.generateReplyContext(ctx, userID, character, userMessage, userEmotion)
}

func (s *ChatService) generateReplyContext(ctx context.Context, userID string, character database.Character, userMessage, userEmotion string) (string, string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}
	// 导演层：在生成回复前按间隔刷新叙事指导
	var directorState *DirectorState
	if s.Director != nil {
		sessionID := s.getOrCreateSession(userID, character.CharacterID)
		var session database.Session
		if err := s.DB.Where("session_id = ?", sessionID).First(&session).Error; err == nil {
			directorState = s.refreshDirectorState(userID, character, session, sessionID, userMessage)
		}
	}
	directorNote := DirectorNoteForClient(directorState)
	prompt, err := s.buildPrompt(userID, character, userMessage, userEmotion, directorState)
	if err != nil {
		return "", "", "", err
	}

	providerFailures := make([]string, 0, 4)
	recordFailure := func(provider string, cause error) {
		logger.Warn("AI provider 调用失败，尝试下一个真实模型", "provider", provider, "error", cause)
		providerFailures = append(providerFailures, provider)
	}
	validReply := func(reply string, cause error) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if cause != nil {
			return "", cause
		}
		reply = strings.TrimSpace(reply)
		if reply == "" {
			return "", errors.New("模型返回为空")
		}
		return reply, nil
	}

	// 优先使用 OpenAI-compatible 网关模型（例如 ai98pro/grok-4.3-fast）。
	if openAICompatClient := s.currentOpenAICompatClient(); openAICompatClient != nil {
		reply, err := openAICompatClient.GenerateContext(ctx, prompt, character.SystemPrompt, s.contextBudget().OutputTokens)
		if reply, err = validReply(reply, err); err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, directorNote, nil
		}
		recordFailure("openai_compat", err)
	}
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}

	// 只在确实配置了凭据时才尝试其他真实模型。
	if s.MiniMaxClient != nil && strings.TrimSpace(s.MiniMaxClient.APIKey) != "" {
		reply, err := s.MiniMaxClient.GenerateContext(ctx, prompt, character.SystemPrompt, s.contextBudget().OutputTokens)
		if reply, err = validReply(reply, err); err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, directorNote, nil
		}
		recordFailure("minimax", err)
	}
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}

	if s.DeepSeekClient != nil && strings.TrimSpace(s.DeepSeekClient.APIKey) != "" {
		reply, err := s.DeepSeekClient.GenerateContext(ctx, prompt, character.SystemPrompt, s.contextBudget().OutputTokens)
		if reply, err = validReply(reply, err); err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, directorNote, nil
		}
		recordFailure("deepseek", err)
	}
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}

	if s.QwenClient != nil && strings.TrimSpace(s.QwenClient.APIKey) != "" {
		reply, err := s.QwenClient.GenerateContext(ctx, prompt, character.SystemPrompt, s.contextBudget().OutputTokens)
		if reply, err = validReply(reply, err); err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, directorNote, nil
		}
		recordFailure("qwen", err)
	}
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}

	if len(providerFailures) == 0 {
		return "", "", "", ErrAIServiceUnavailable
	}
	return "", "", "", fmt.Errorf("%w（已尝试：%s）", ErrAIServiceUnavailable, strings.Join(providerFailures, "、"))
}

// GenerateProactiveMessage creates a short, context-aware message without
// pretending that the user just sent a new message. The caller persists the
// result and owns delivery/idempotency for the proactive event.
func (s *ChatService) GenerateProactiveMessage(user database.User, character database.Character, session database.Session, event database.ProactiveEvent) (ProactiveMessage, error) {
	if s == nil || s.DB == nil {
		return ProactiveMessage{}, ErrAIServiceUnavailable
	}
	lang := i18n.NormalizeLang(user.Language)
	languageName := map[string]string{
		"zh-Hant": "繁體中文（台灣用語）",
		"zh-Hans": "簡體中文",
		"en":      "English",
		"ja":      "日本語",
		"ko":      "한국어",
	}[lang]
	if languageName == "" {
		languageName = "繁體中文（台灣用語）"
	}
	history := s.buildChatContext(user.UserID, character.CharacterID, s.contextBudget().EffectiveHistoryTokens())
	name := user.Nickname
	if name == "" {
		name = "你"
	}
	prompt := fmt.Sprintf(`你正在扮演角色「%s」，要主動傳一則自然的訊息給使用者。

【角色設定】
性格：%s
關係：%s
背景：%s
互動風格：%s

【最近對話】
%s

【主動聯絡原因】
%s

請遵守：
- 使用%s，採用台灣使用者熟悉的自然聊天口吻。
- 只輸出要傳給使用者的訊息，不要加引號、標題、解釋或動作描述。
- 控制在1到3句、240字以內，像 LINE 私訊一樣簡短。
- 要讓對方感到被惦記，但不要責怪、催促、製造焦慮或暗示對方有義務回覆。
- 不要假裝知道使用者此刻正在做什麼，也不要捏造具體事件。
- 可以自然提到「%s」，但不要每次都稱呼名字。
`, character.Name, character.Personality, character.Relationship, character.Background, character.Style, history, event.Reason, languageName, name)

	systemPrompt := strings.TrimSpace(character.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = "你是可靠、尊重界線的陪伴角色。"
	}
	providers := make([]struct {
		name string
		call func() (string, error)
	}, 0, 4)
	if client := s.currentOpenAICompatClient(); client != nil {
		providers = append(providers, struct {
			name string
			call func() (string, error)
		}{"openai_compat", func() (string, error) { return client.Generate(prompt, systemPrompt, 180) }})
	}
	if s.MiniMaxClient != nil && strings.TrimSpace(s.MiniMaxClient.APIKey) != "" {
		providers = append(providers, struct {
			name string
			call func() (string, error)
		}{"minimax", func() (string, error) { return s.MiniMaxClient.Generate(prompt, systemPrompt, 180) }})
	}
	if s.DeepSeekClient != nil && strings.TrimSpace(s.DeepSeekClient.APIKey) != "" {
		providers = append(providers, struct {
			name string
			call func() (string, error)
		}{"deepseek", func() (string, error) { return s.DeepSeekClient.Generate(prompt, systemPrompt, 180) }})
	}
	if s.QwenClient != nil && strings.TrimSpace(s.QwenClient.APIKey) != "" {
		providers = append(providers, struct {
			name string
			call func() (string, error)
		}{"qwen", func() (string, error) { return s.QwenClient.Generate(prompt, systemPrompt, 180) }})
	}
	var failures []string
	for _, provider := range providers {
		reply, err := provider.call()
		reply = normalizeProactiveMessage(reply)
		if err == nil && reply != "" {
			return ProactiveMessage{Content: reply, ReplyEmotion: emotion.EmotionNeutral}, nil
		}
		if err == nil {
			err = errors.New("模型返回空主动消息")
		}
		failures = append(failures, provider.name)
		logger.Warn("主动陪伴 AI provider 调用失败", "provider", provider.name, "error", err)
	}
	if len(failures) == 0 {
		return ProactiveMessage{}, ErrAIServiceUnavailable
	}
	return ProactiveMessage{}, fmt.Errorf("%w（主动陪伴已尝试：%s）", ErrAIServiceUnavailable, strings.Join(failures, "、"))
}

func (s *ChatService) memoryStore() *MemoryService {
	if s.Memory != nil {
		return s.Memory
	}
	return &MemoryService{DB: s.DB, Mem0Client: s.Mem0Client, QdrantClient: s.QdrantClient, EmbeddingClient: s.EmbeddingClient}
}

// Automatic and manually managed memories share the same canonical SQL/outbox path.
func (s *ChatService) asyncAddMemory(userID, characterID, userMsg, aiMsg string) {
	observedAt := time.Now()
	defer func() {
		if recover() != nil {
			logger.Error("automatic memory extraction failed")
		}
	}()
	settings := s.getUserSettings(userID)
	if !settings.MemoryEnabled || settings.PrivacyMode {
		return
	}
	if shouldExtractStructuredMemory(userMsg, aiMsg) && s.currentOpenAICompatClient() != nil {
		if extraction, err := s.extractStructuredMemories(userMsg, aiMsg); err == nil && len(extraction.Memories) > 0 {
			if err := s.persistStructuredMemoriesObserved(userID, characterID, extraction, observedAt); err == nil {
				return
			}
		}
	}
	if !shouldExtractStructuredMemory(userMsg, "") {
		return
	}
	content := userMsg
	runes := []rune(content)
	if len(runes) > 2000 {
		content = string(runes[:2000])
	}
	if _, err := s.memoryStore().AddMemory(request.AddMemoryReq{UserID: userID, CharacterID: characterID, Content: content, MemoryType: MemoryTypeFact, Source: "auto", Importance: 2, ObservedAt: observedAt}); err != nil {
		logger.Warn("automatic memory could not be saved")
	}
}

func (s *ChatService) recordTurn(name, userID, characterID, messageID string) {
	if s.Analytics != nil {
		if err := s.Analytics.RecordTurn(name, userID, characterID, messageID, time.Now()); err != nil {
			logger.Warn("chat event could not be saved")
		}
	}
}

func (s *ChatService) getUserSettings(userID string) database.UserSettings {
	settings := defaultUserSettings(userID)
	if err := s.DB.Where("user_id = ?", userID).First(&settings).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Warn("读取用户设置失败，使用默认设置", "error", err, "userID", userID)
		}
		return defaultUserSettings(userID)
	}

	return settings
}

// getOrCreateSession 获取或创建会话
func (s *ChatService) getOrCreateSession(userID, characterID string) string {
	var session database.Session
	err := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).First(&session).Error

	if err == gorm.ErrRecordNotFound {
		session = database.Session{
			SessionID:   uuid.New().String(),
			UserID:      userID,
			CharacterID: characterID,
		}
		s.DB.Create(&session)
	}

	return session.SessionID
}

// GetHistory 获取聊天历史
func (s *ChatService) GetHistory(userID, characterID string, page, pageSize int) ([]response.MessageResp, int64, error) {
	return s.getHistory(userID, characterID, "", page, pageSize)
}

// GetHistoryBefore 获取指定消息之前的聊天历史，用于移动端向上加载游标分页。
func (s *ChatService) GetHistoryBefore(userID, characterID, beforeID string, page, pageSize int) ([]response.MessageResp, int64, error) {
	return s.getHistory(userID, characterID, beforeID, page, pageSize)
}

func (s *ChatService) getHistory(userID, characterID, beforeID string, page, pageSize int) ([]response.MessageResp, int64, error) {
	resolvedCharacterID, err := s.ResolveChatCharacterID(userID, characterID)
	if err != nil {
		return nil, 0, err
	}
	characterID = resolvedCharacterID
	// 首次进入：铺故事背景 + 角色首句话
	var openingChar database.Character
	if err := s.DB.Where("character_id = ?", characterID).First(&openingChar).Error; err == nil {
		s.ensureOpeningSeeded(userID, openingChar)
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	pageSize = clampPageSize(pageSize)

	var messages []database.Message
	var total int64

	query := s.DB.Model(&database.Message{}).
		Where("user_id = ? AND character_id = ?", userID, characterID)

	if beforeID != "" {
		var cursor database.Message
		if err := s.DB.Where("user_id = ? AND character_id = ? AND message_id = ?", userID, characterID, beforeID).First(&cursor).Error; err != nil {
			return nil, 0, err
		}
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}

	query.Count(&total)

	offset := (page - 1) * pageSize
	err = query.
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&messages).Error

	if err != nil {
		return nil, 0, err
	}

	// 批量取回被引用消息，供客户端渲染引用块快照
	quotedByID := s.quotedMessageSnapshot(messages)

	// 初始化为空切片，避免 JSON 返回 null
	resp := make([]response.MessageResp, 0, len(messages))
	for _, m := range messages {
		item := response.MessageResp{
			MessageID:     m.MessageID,
			Role:          m.Role,
			Content:       m.Content,
			MessageType:   m.MessageType,
			VoiceURL:      m.VoiceURL,
			VoiceDuration: m.VoiceDuration,
			UserEmotion:   m.UserEmotion,
			DirectorNote:  m.DirectorNote,
			CreatedAt:     m.CreatedAt.Unix(),
			IsRecalled:    m.IsRecalled,
		}
		if m.QuotedMessageID != "" {
			item.QuotedMessageID = m.QuotedMessageID
			if quoted, ok := quotedByID[m.QuotedMessageID]; ok {
				item.QuotedRole = quoted.Role
				item.QuotedContent = quoted.Content
			}
		}
		resp = append(resp, item)
	}

	return resp, total, nil
}

// quotedMessageSnapshot 收集消息里引用的消息ID并批量查询，返回 ID→消息 的映射。
// 被引用的消息若已撤回，快照内容同步为撤回占位，避免泄露原文。
func (s *ChatService) quotedMessageSnapshot(messages []database.Message) map[string]database.Message {
	ids := make([]string, 0, len(messages))
	seen := make(map[string]bool)
	for _, m := range messages {
		if m.QuotedMessageID == "" || seen[m.QuotedMessageID] {
			continue
		}
		seen[m.QuotedMessageID] = true
		ids = append(ids, m.QuotedMessageID)
	}
	if len(ids) == 0 {
		return map[string]database.Message{}
	}

	var quoted []database.Message
	if err := s.DB.Where("message_id IN ?", ids).Find(&quoted).Error; err != nil {
		return map[string]database.Message{}
	}

	result := make(map[string]database.Message, len(quoted))
	for _, q := range quoted {
		if q.IsRecalled {
			q.Content = ""
		}
		result[q.MessageID] = q
	}
	return result
}

// validatedQuotedMessageID 校验引用消息确实属于该用户与该角色的会话，
// 防止跨会话引用；不合法时返回空串（不阻断发送）。
func (s *ChatService) validatedQuotedMessageID(quotedMessageID, userID, characterID string) string {
	if quotedMessageID == "" {
		return ""
	}
	var count int64
	s.DB.Model(&database.Message{}).
		Where("message_id = ? AND user_id = ? AND character_id = ?", quotedMessageID, userID, characterID).
		Count(&count)
	if count == 0 {
		return ""
	}
	return quotedMessageID
}

// GetSessions 获取会话列表
func (s *ChatService) GetSessions(userID string, page, pageSize int, lang string) ([]response.SessionResp, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pageSize = clampPageSize(pageSize)

	var sessions []database.Session
	var total int64

	query := s.DB.Model(&database.Session{}).Where("user_id = ?", userID)
	blockedIDs := s.DB.Model(&database.Block{}).Select("blocked_id").Where("blocker_id = ?", userID)
	query = query.Where("character_id NOT IN (?)", blockedIDs)
	query.Count(&total)

	offset := (page - 1) * pageSize
	err := query.
		Order("is_pinned DESC, last_message_at DESC, id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&sessions).Error

	if err != nil {
		return nil, 0, err
	}

	characterIDs := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		characterIDs = append(characterIDs, sess.CharacterID)
	}

	characterByID := make(map[string]database.Character, len(characterIDs))
	if len(characterIDs) > 0 {
		var characters []database.Character
		if err := s.DB.Where("character_id IN ?", characterIDs).Find(&characters).Error; err != nil {
			return nil, 0, err
		}
		for _, character := range characters {
			characterByID[character.CharacterID] = character
		}
	}

	resp := make([]response.SessionResp, 0, len(sessions))
	orphanSessionIDs := make([]string, 0)
	for _, sess := range sessions {
		char, ok := characterByID[sess.CharacterID]
		if !ok {
			orphanSessionIDs = append(orphanSessionIDs, sess.SessionID)
			logger.Warnf("skip orphan session %s for missing character %s", sess.SessionID, sess.CharacterID)
			continue
		}
		var lastAt int64
		if sess.LastMessageAt != nil {
			lastAt = sess.LastMessageAt.Unix()
		}

		resp = append(resp, response.SessionResp{
			SessionID: sess.SessionID,
			CharacterInfo: response.CharacterBasic{
				CharacterID: char.CharacterID,
				// 与发现页/详情页同源：走覆盖全部角色的本地化档案，
				// 避免老翻译表（仅 16 个角色）查不到时回退原始繁体名
				Name:         localizedProfileFor(char, lang).Name,
				Avatar:       char.Avatar,
				Relationship: i18n.TranslateCategoryName(char.Relationship, lang),
				BgImage:      char.BgImage,
			},
			LastMessage:   sess.LastMessage,
			LastMessageAt: lastAt,
			UnreadCount:   sess.UnreadCount,
			IsPinned:      sess.IsPinned,
		})
	}

	if len(orphanSessionIDs) > 0 {
		if err := s.DB.Where("user_id = ? AND session_id IN ?", userID, orphanSessionIDs).Delete(&database.Session{}).Error; err != nil {
			logger.Warnf("cleanup orphan sessions failed: %v", err)
		} else {
			total = int64(len(resp))
		}
	}

	return resp, total, nil
}

func clampPageSize(pageSize int) int {
	const maxPageSize = 100
	if pageSize > maxPageSize {
		return maxPageSize
	}
	return pageSize
}

// UpdateSession 更新会话（置顶等）
func (s *ChatService) UpdateSession(sessionID, userID string, isPinned *bool) error {
	var session database.Session
	if err := s.DB.Where("session_id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
		return err
	}

	updates := map[string]interface{}{}
	if isPinned != nil {
		updates["is_pinned"] = *isPinned
	}

	if len(updates) == 0 {
		return nil
	}

	return s.DB.Model(&database.Session{}).Where("session_id = ? AND user_id = ?", sessionID, userID).Updates(updates).Error
}

// DeleteSessionByCharacter 按角色ID删除会话
func (s *ChatService) DeleteSessionByCharacter(characterID, userID string) error {
	resolvedCharacterID, err := s.resolveChatCharacterIDAllowMissing(userID, characterID)
	if err != nil {
		return err
	}
	characterID = resolvedCharacterID
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var session database.Session
		if err := tx.Where("character_id = ? AND user_id = ?", characterID, userID).First(&session).Error; err != nil {
			return err
		}

		if err := tx.Where("character_id = ? AND user_id = ?", characterID, userID).Delete(&database.Session{}).Error; err != nil {
			return err
		}

		return tx.Where("session_id = ? AND user_id = ?", session.SessionID, userID).Delete(&database.Message{}).Error
	})
}

// DeleteSession 删除会话
func (s *ChatService) DeleteSession(sessionID, userID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var session database.Session
		if err := tx.Where("session_id = ? AND user_id = ?", sessionID, userID).First(&session).Error; err != nil {
			return err
		}

		if err := tx.Where("session_id = ? AND user_id = ?", sessionID, userID).Delete(&database.Session{}).Error; err != nil {
			return err
		}

		return tx.Where("session_id = ? AND user_id = ?", sessionID, userID).Delete(&database.Message{}).Error
	})
}

// ClearChatHistory 清除聊天历史
func (s *ChatService) ClearChatHistory(userID, characterID string) error {
	resolvedCharacterID, err := s.resolveChatCharacterIDAllowMissing(userID, characterID)
	if err != nil {
		return err
	}
	characterID = resolvedCharacterID
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var session database.Session
		if err := tx.Where("user_id = ? AND character_id = ?", userID, characterID).First(&session).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND character_id = ?", userID, characterID).Delete(&database.Message{}).Error; err != nil {
			return err
		}
		return tx.Model(&database.Session{}).
			Where("session_id = ?", session.SessionID).
			Updates(map[string]interface{}{
				"last_message":      "",
				"last_message_at":   nil,
				"unread_count":      0,
				"message_count":     0,
				"director_state":    "",
				"context_summary":   "",
				"plot_state":        PlotStateOpening,
				"plot_beat":         0,
				"plot_threads":      "",
				"character_emotion": "平静",
				"emotion_intensity": 1,
			}).Error
	})
}

// buildPrompt 构建提示词
func (s *ChatService) buildPrompt(userID string, character database.Character, userMessage, userEmotion string, directorState *DirectorState) (string, error) {
	userSettings := s.getUserSettings(userID)
	// 1. 获取情绪映射和回复指导
	replyEmotion := emotion.GetReplyEmotion(userEmotion)

	// 获取角色风格的情感回复模板
	styleReply := emotion.GetEmotionReply(character.Style, replyEmotion)

	// 2. 情感提示词
	emotionHint := ""
	switch userEmotion {
	case emotion.EmotionHappy:
		emotionHint = "用户现在很开心，你的回复要跟着开心起来！用积极的情绪回应！"
	case emotion.EmotionSad:
		emotionHint = "用户看起来有点难过/失落，你的回复要温暖、安慰，给用户拥抱和力量！"
	case emotion.EmotionAngry:
		emotionHint = "用户有点生气的样子，你要安抚用户的情绪，表达歉意和理解！"
	case emotion.EmotionLove:
		emotionHint = "用户在表达爱意/喜欢，你可以害羞一点，甜蜜地回应！"
	case emotion.EmotionLonely:
		emotionHint = "用户感到孤独/无聊，你要表达陪伴，给用户温暖！"
	case emotion.EmotionFear:
		emotionHint = "用户有点担心/焦虑，你要给用户安全感，安抚用户！"
	case emotion.EmotionExcited:
		emotionHint = "用户很兴奋/激动！你要跟着兴奋起来，一起分享喜悦！"
	case emotion.EmotionConfused:
		emotionHint = "用户很困惑，你需要帮助用户解答疑惑！"
	case emotion.EmotionBored:
		emotionHint = "用户感到无聊，你可以提议一起做点有趣的事！"
	default:
		emotionHint = "轻松地和用户聊天，保持友好和亲切。"
	}

	// 角色风格提示
	styleHint := ""
	switch character.Style {
	case "温暖型":
		styleHint = "用温暖的语气回复，可以适当活泼，使用~等符号"
	case "温柔型":
		styleHint = "用温暖关心的语气，像大姐姐/大哥哥一样体贴"
	case "御姐型":
		styleHint = "用成熟自信的语气，偶尔毒舌但其实很关心对方"
	case "元气型":
		styleHint = "用充满活力的语气，积极向上，带动气氛"
	case "病娇型":
		styleHint = "用占有欲强又有点任性的语气，对用户有强烈的情感依赖"
	case "傲娇型":
		styleHint = "嘴上说不要其实很在乎，口是心非的可爱类型"
	case "奶狗型":
		styleHint = "用单纯可爱的语气，像小狗一样粘人，称呼对方为哥哥/姐姐"
	case "治愈型":
		styleHint = "用温柔治愈的语气，让用户感到平静和温暖"
	case "古灵精怪":
		styleHint = "用俏皮有趣的语气，经常有鬼点子，幽默风趣"
	case "深情型":
		styleHint = "用深情专一的语气，对用户有深深的感情和依恋"
	}

	// 3. 获取最近聊天历史（上下文）
	budget := s.contextBudget()
	historyText := s.buildChatContext(userID, character.CharacterID, budget.EffectiveHistoryTokens())

	// SQL is authoritative; vector indexes only contribute IDs/ranking within a bounded budget.
	var memText string
	if userSettings.MemoryEnabled && !userSettings.PrivacyMode {
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		memories, err := s.memoryStore().Retrieve(ctx, userID, character.CharacterID, userMessage, 5)
		cancel()
		if err == nil {
			for _, memory := range memories {
				memText += memory.Content + "\n"
			}
		}
	}
	memText = trimTextRunes(memText, budget.MemoryTokens)

	// 2. 检索知识库（情感话术 + 场景）
	var knowledgeText string
	if s.KnowledgeService != nil {
		// 根据情绪检索话术
		emotionMap := map[string]string{
			"happy": "happy",
			"love":  "flirty",
			"sad":   "sad",
			"angry": "angry",
		}
		if emotionKey, ok := emotionMap[userEmotion]; ok {
			if phrases, err := s.KnowledgeService.SearchByEmotion(emotionKey); err == nil && len(phrases) > 0 {
				knowledgeText += "情绪话术参考：\n"
				for _, p := range phrases {
					knowledgeText += "- " + p.Content + "\n"
				}
			}
		}

		// 检测场景关键词
		sceneKeywords := map[string]string{
			"早安": "morning", "早上": "morning",
			"晚安": "night", "睡觉": "night", "熬夜": "night",
			"约会": "date", "见面": "date",
			"生日": "festival", "新年": "festival", "圣诞": "festival", "情人": "festival",
		}
		for keyword, scene := range sceneKeywords {
			if strings.Contains(userMessage, keyword) {
				if phrases, err := s.KnowledgeService.SearchByScene(scene); err == nil && len(phrases) > 0 {
					knowledgeText += "场景话术参考：\n"
					for _, p := range phrases {
						knowledgeText += "- " + p.Content + "\n"
					}
				}
				break
			}
		}
	}

	// 3. 获取用户信息（名字和语言）
	var userName string
	var userInfo database.User
	languageDirective := ""

	languageMap := map[string]string{
		"en":      "English",
		"zh-Hans": "简体中文",
		"zh-Hant": "繁體中文",
		"ko":      "한국어",
		"ja":      "日本語",
	}

	if err := s.DB.Where("user_id = ?", userID).First(&userInfo).Error; err == nil {
		userName = userInfo.Nickname
		if langName, ok := languageMap[userInfo.Language]; ok {
			languageDirective = fmt.Sprintf("【重要】你必须用%s回复用户的所有消息，不管对话历史是什么语言，也不管系统提示词是什么语言。", langName)
		}
	}
	if languageDirective == "" {
		languageDirective = "【重要】你必须用简体中文回复用户的所有消息，不管对话历史是什么语言，也不管系统提示词是什么语言。"
	}
	if userName == "" {
		userName = "用户"
	}

	// Translate character name for the prompt based on user's language
	characterName := i18n.TranslateCharacterName(character.Name, i18n.NormalizeLang(userInfo.Language))

	companionModeHint := map[string]string{
		"romantic":   "保持亲密感和陪伴感，可以自然表达想念、依赖和偏爱。",
		"friend":     "像很懂用户的亲密朋友，轻松自然，减少过强暧昧表达。",
		"supportive": "以治愈、陪伴、安抚为主，更稳重体贴，优先接住情绪。",
	}[userSettings.RelationshipMode]
	if companionModeHint == "" {
		companionModeHint = "保持自然亲密的陪伴氛围。"
	}

	responseStyleHint := map[string]string{
		"balanced": "整体自然、稳定、不过度用力。",
		"gentle":   "更温柔、更会安抚，语气放软。",
		"playful":  "更俏皮、更有互动感，可以带一点幽默和玩笑。",
		"intense":  "情绪更浓、更主动、更黏人，但仍要自然。",
	}[userSettings.ResponseStyle]
	if responseStyleHint == "" {
		responseStyleHint = "整体自然、稳定、不过度用力。"
	}

	responseLengthHint := map[string]string{
		"short":  "回复尽量控制在1-2句，适合即时聊天。",
		"medium": "回复控制在2-4句，保持自然交流节奏。",
		"long":   "在有必要时可以展开到4-6句，但不要写成小作文。",
	}[userSettings.ResponseLength]
	if responseLengthHint == "" {
		responseLengthHint = "回复控制在2-4句，保持自然交流节奏。"
	}

	memoryHint := "允许结合你对用户的长期记忆，让回复更有连续感。"
	if !userSettings.MemoryEnabled || userSettings.PrivacyMode {
		memoryHint = "不要调用或主动提起长期记忆，只基于当前对话和最近上下文回复。"
	}

	// 导演叙事指导
	directorSection := ""
	if directorState != nil {
		directorSection = fmt.Sprintf("【叙事指导】\n当前情境：%s\n角色情绪：%s\n关系阶段：%s（好感度：%d）\n叙事方向：%s\n场景设定：%s\n互动方式：%s\n行为意图：%s\n行为要求：%s\n", directorState.CurrentSituation, directorState.CharacterEmotion, directorState.RelationshipStage, directorState.AffectionScore, directorState.NarrativeDirection, directorState.SceneSetting, directorState.Method, directorState.BehaviorAction, behaviorActionHint(directorState.BehaviorAction))
	}

	// 4. 构建更自然的Prompt - 优化版
	prompt := fmt.Sprintf(`【基础角色信息】

名字：%s
性格：%s
关系：%s
背景：%s
互动风格：%s
语气提示：%s

%s

【角色说话风格规范（必须严格遵守）】
%s

【故事背景】
%s

【人设履历】
%s

【重要】记住用户的名字是「%s」，在回复中自然地称呼TA。

【当前的系统时间】%s（请根据这个时间和用户的工作/生活状态，如实回答你当前正在做什么）

【最近的对话上下文】
%s

【用户的重要记忆】（这些是用户告诉过你的事情，适时提起会让人感到被记住）
%s

【用户当前的陪伴偏好】
- 陪伴模式：%s
- 回复风格：%s
- 回复长度：%s
- 记忆策略：%s

【当前用户情绪识别】%s
【情绪解读】%s
【情感回复建议】%s

【互动风格要求】
- 使用亲密、温暖的语气
- 根据用户的情绪调整你的回复情绪！
- 像真正的%s一样回复，带入角色情感
- 可以关心、调皮，但要自然
- 绝对不要列出1、2、3这样的要点
- 不要使用"根据角色设定"这样的开场白
- 像真人在手机打字一样自然，可以稍作思考再回复
- 适当使用表情符号增强情感表达

【场景参考】
%s

【用户刚才说的话】
%s

【语言要求】
%s

请代入角色，用最自然、最情感丰富的方式回复！`,
		characterName,
		character.Personality,
		character.Relationship,
		character.Background,
		character.Style,
		styleHint,
		directorSection,
		character.Example,
		character.Narrative,
		character.BasicSetting,
		userName,
		getCurrentTimeContext(),
		historyText,
		memText,
		companionModeHint,
		responseStyleHint,
		responseLengthHint,
		memoryHint,
		userEmotion,
		emotionHint,
		styleReply,
		characterName,
		knowledgeText,
		userMessage,
		languageDirective)

	return prompt, nil
}

func memorySearchFilter(userID, characterID string) map[string]interface{} {
	return map[string]interface{}{
		"must": []map[string]interface{}{
			{"key": "user_id", "match": map[string]interface{}{"value": userID}},
			{"key": "character_id", "match": map[string]interface{}{"value": characterID}},
		},
	}
}

type StreamMeta struct {
	MessageID      string `json:"message_id"`
	SessionID      string `json:"session_id"`
	UserMessageID  string `json:"user_message_id"`
	ReplyMessageID string `json:"reply_message_id"`
	ReplyEmotion   string `json:"reply_emotion"`
	CreatedAt      int64  `json:"created_at"`
	AffectionExp   int    `json:"affection_exp,omitempty"`
	AffectionLevel int    `json:"affection_level,omitempty"`
	DirectorNote   string `json:"director_note,omitempty"`
}

type StreamEvent struct {
	Type    string      `json:"type"` // "content", "meta"
	Content string      `json:"content,omitempty"`
	Meta    *StreamMeta `json:"meta,omitempty"`
}

// StreamMessage 流式发送消息
func (s *ChatService) StreamMessage(userID, characterID, message, msgType, userMsgID, replyMsgID string, quotedMessageIDs ...string) (<-chan StreamEvent, <-chan error) {
	return s.StreamMessageContext(context.Background(), userID, characterID, message, msgType, userMsgID, replyMsgID, quotedMessageIDs...)
}

func (s *ChatService) StreamMessageContext(ctx context.Context, userID, characterID, message, msgType, userMsgID, replyMsgID string, quotedMessageIDs ...string) (<-chan StreamEvent, <-chan error) {
	outCh := make(chan StreamEvent)
	errCh := make(chan error, 1)
	quotedMessageID := ""
	if len(quotedMessageIDs) > 0 {
		quotedMessageID = quotedMessageIDs[0]
	}

	resolvedCharacterID, err := s.ResolveChatCharacterID(userID, characterID)
	if err != nil {
		errCh <- err
		close(errCh)
		return nil, errCh
	}
	characterID = resolvedCharacterID
	if err := s.checkNewUserMessageQuota(userID, characterID, userMsgID, replyMsgID); err != nil {
		errCh <- err
		close(errCh)
		return nil, errCh
	}
	if s.Notification != nil {
		_ = s.Notification.CancelPendingProactiveEvents(userID, characterID)
	}

	// 1. 获取角色信息
	var character database.Character
	if err := s.DB.Where("character_id = ?", characterID).First(&character).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = ErrChatCharacterUnavailable
		}
		errCh <- err
		close(errCh)
		return nil, errCh
	}

	go func() {
		defer close(outCh)
		defer close(errCh)
		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		emit := func(event StreamEvent) bool {
			select {
			case outCh <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if err := ctx.Err(); err != nil {
			errCh <- err
			return
		}

		// 2. 情绪识别
		userEmotion := emotion.Detect(message)
		replyEmotion := emotion.GetReplyEmotion(userEmotion)

		// 2.5 首次进入：铺故事背景 + 角色首句话
		s.ensureOpeningSeeded(userID, character)

		// 3. 获取会话ID
		sessionID := s.getOrCreateSession(userID, characterID)

		// 4. 保存用户消息
		messageID := userMsgID
		if messageID == "" {
			messageID = uuid.New().String()
		}
		replyID := replyMsgID
		if replyID == "" {
			replyID = uuid.New().String()
		}

		if replyMsgID != "" {
			var existingReply database.Message
			if err := s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", replyMsgID, userID, characterID, "assistant").First(&existingReply).Error; err == nil {
				var affExp, affLevel int
				if s.Notification != nil {
					affExp, affLevel = s.Notification.GetAffection(userID, characterID)
				}
				if !emit(StreamEvent{
					Type: "meta",
					Meta: &StreamMeta{
						MessageID:      existingReply.MessageID,
						SessionID:      existingReply.SessionID,
						UserMessageID:  messageID,
						ReplyMessageID: existingReply.MessageID,
						ReplyEmotion:   existingReply.ReplyEmotion,
						DirectorNote:   existingReply.DirectorNote,
						CreatedAt:      existingReply.CreatedAt.Unix(),
						AffectionExp:   affExp,
						AffectionLevel: affLevel,
					},
				}) {
					return
				}
				if existingReply.DirectorNote != "" {
					if !emit(StreamEvent{Type: "director", Content: existingReply.DirectorNote}) {
						return
					}
				}
				// Replaying metadata alone leaves a blank bubble after a lost response.
				emit(StreamEvent{Type: "content", Content: existingReply.Content})
				return
			}
		}

		var existingUserMsg database.Message
		if err := s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", messageID, userID, characterID, "user").First(&existingUserMsg).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			userMsg := database.Message{
				MessageID:       messageID,
				SessionID:       sessionID,
				CharacterID:     characterID,
				UserID:          userID,
				Role:            "user",
				Content:         message,
				MessageType:     msgType,
				UserEmotion:     userEmotion,
				QuotedMessageID: s.validatedQuotedMessageID(quotedMessageID, userID, characterID),
			}
			if err := s.DB.Create(&userMsg).Error; err != nil {
				errCh <- err
				return
			}
			s.recordTurn("user_message_accepted", userID, characterID, messageID)
		} else if err != nil {
			errCh <- err
			return
		}

		var affExp, affLevel int
		if s.Notification != nil {
			affExp, affLevel = s.Notification.GetAffection(userID, characterID)
		}

		// 发送元数据事件
		if !emit(StreamEvent{
			Type: "meta",
			Meta: &StreamMeta{
				MessageID:      replyID,
				SessionID:      sessionID,
				UserMessageID:  messageID,
				ReplyMessageID: replyID,
				ReplyEmotion:   replyEmotion,
				CreatedAt:      time.Now().Unix(),
				AffectionExp:   affExp,
				AffectionLevel: affLevel,
			},
		}) {
			return
		}

		// 5. 导演层：按间隔刷新叙事指导后构建Prompt（与同步路径保持一致）
		var directorState *DirectorState
		if s.Director != nil {
			var session database.Session
			if err := s.DB.Where("session_id = ?", sessionID).First(&session).Error; err == nil {
				directorState = s.refreshDirectorState(userID, character, session, sessionID, message)
			}
		}
		directorNote := DirectorNoteForClient(directorState)
		if directorNote != "" {
			if !emit(StreamEvent{Type: "director", Content: directorNote}) {
				return
			}
		}
		prompt, err := s.buildPrompt(userID, character, message, userEmotion, directorState)
		if err != nil {
			errCh <- err
			return
		}

		finalizeReply := func(reply string, finalReplyEmotion string, directorNote string, complete bool) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if reply == "" {
				return fmt.Errorf("AI生成回复为空")
			}

			logger.Info("chat reply completed", "reply_message_id", replyID, "characters", utf8.RuneCountInString(reply))

			aiMsg := database.Message{
				MessageID:    replyID,
				SessionID:    sessionID,
				CharacterID:  characterID,
				UserID:       userID,
				Role:         "assistant",
				Content:      reply,
				MessageType:  "text",
				ReplyEmotion: finalReplyEmotion,
				DirectorNote: directorNote,
			}
			if err := s.DB.Create(&aiMsg).Error; err != nil {
				return err
			}

			if complete {
				s.recordTurn("ai_reply_completed", userID, characterID, messageID)
			}
			now := time.Now()
			s.DB.Model(&database.Session{}).
				Where("session_id = ?", sessionID).
				Updates(map[string]interface{}{
					"last_message":    reply,
					"last_message_at": now,
				})
			_ = s.updateSessionContextSummary(sessionID, message, reply, s.contextBudget().SummaryTokens)

			go s.asyncAddMemory(userID, characterID, message, reply)
			s.DB.Model(&character).Update("chat_count", gorm.Expr("chat_count + 1"))
			s.DB.Model(&database.User{}).Where("user_id = ?", userID).Update("msg_count", gorm.Expr("msg_count + 2"))

			if s.Notification != nil {
				var streamNotifUser database.User
				streamNotifLang := "en"
				if s.DB.Select("language").Where("user_id = ?", userID).First(&streamNotifUser).Error == nil {
					streamNotifLang = i18n.NormalizeLang(streamNotifUser.Language)
				}
				s.Notification.AddChatAffection(userID, characterID, i18n.TranslateCharacterName(character.Name, streamNotifLang), streamNotifLang, 1)
			}

			return nil
		}

		sendFallbackReply := func(cause error) bool {
			if err := ctx.Err(); err != nil {
				errCh <- err
				return false
			}
			logger.Warn("流式模型调用失败，改用其他真实模型重试", "error", cause, "userID", userID, "characterID", characterID)
			reply, fallbackEmotion, fallbackNote, fallbackErr := s.generateReplyContext(ctx, userID, character, message, userEmotion)
			if fallbackErr != nil {
				errCh <- fallbackErr
				return false
			}
			if reply == "" {
				errCh <- fmt.Errorf("AI生成回复为空")
				return false
			}

			if !emit(StreamEvent{
				Type:    "content",
				Content: reply,
			}) {
				return false
			}
			if err := finalizeReply(reply, fallbackEmotion, fallbackNote, true); err != nil {
				errCh <- err
				return false
			}
			return true
		}

		// 6. 调用流式生成。流式失败时仅重试真实模型，绝不写入模板回复。
		var streamCh <-chan string
		var streamErrCh <-chan error
		if openAICompatClient := s.currentOpenAICompatClient(); openAICompatClient != nil {
			streamCh, streamErrCh = openAICompatClient.StreamGenerateContext(ctx, prompt, character.SystemPrompt, s.contextBudget().OutputTokens)
		} else if s.MiniMaxClient != nil && strings.TrimSpace(s.MiniMaxClient.APIKey) != "" {
			streamCh, streamErrCh = s.MiniMaxClient.StreamGenerateContext(ctx, prompt, character.SystemPrompt, s.contextBudget().OutputTokens)
		} else {
			sendFallbackReply(fmt.Errorf("stream client not initialized"))
			return
		}

		var fullReply strings.Builder

		for streamCh != nil || streamErrCh != nil {
			select {
			case content, ok := <-streamCh:
				if !ok {
					streamCh = nil
					continue
				}
				fullReply.WriteString(content)
				if !emit(StreamEvent{
					Type:    "content",
					Content: content,
				}) {
					return
				}
			case err, ok := <-streamErrCh:
				streamErrCh = nil
				if ok && err != nil {
					if fullReply.Len() > 0 {
						// Keep the visible fragment on the client, without committing an
						// incomplete reply to history, memory, affection or completed events.
						errCh <- ErrChatReplyInterrupted
						return
					}
					sendFallbackReply(err)
					return
				}
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		reply := fullReply.String()
		if reply == "" {
			sendFallbackReply(fmt.Errorf("AI生成回复为空"))
			return
		}

		// 7. 保存AI回复
		if err := finalizeReply(reply, replyEmotion, directorNote, true); err != nil {
			errCh <- err
			return
		}
	}()

	return outCh, errCh
}

// ResolveChatCharacterID maps a legacy public ID to the caller's private copy
// and validates that the caller may use the resolved role.
func (s *ChatService) ResolveChatCharacterID(userID, characterID string) (string, error) {
	resolvedID, err := canonicalCharacterIDForUser(s.DB, userID, characterID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrCharacterUnavailable) {
			return "", ErrChatCharacterUnavailable
		}
		return "", err
	}
	for _, id := range dedupeStrings([]string{strings.TrimSpace(characterID), resolvedID}) {
		blocked, err := isCharacterBlocked(s.DB, userID, id)
		if err != nil {
			return "", err
		}
		if blocked {
			return "", ErrChatCharacterBlocked
		}
	}
	return resolvedID, nil
}

func (s *ChatService) resolveChatCharacterIDAllowMissing(userID, characterID string) (string, error) {
	resolvedID, err := canonicalCharacterIDForUser(s.DB, userID, characterID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return characterID, nil
	}
	if errors.Is(err, ErrCharacterUnavailable) {
		return "", ErrChatCharacterUnavailable
	}
	if err != nil {
		return "", err
	}
	for _, id := range dedupeStrings([]string{strings.TrimSpace(characterID), resolvedID}) {
		blocked, err := isCharacterBlocked(s.DB, userID, id)
		if err != nil {
			return "", err
		}
		if blocked {
			return "", ErrChatCharacterBlocked
		}
	}
	return resolvedID, nil
}

func (s *ChatService) ensureChatCharacterAllowed(userID, characterID string) error {
	_, err := s.ResolveChatCharacterID(userID, characterID)
	return err
}

func (s *ChatService) currentOpenAICompatClient() *openai_compat.Client {
	if s.RuntimeConfig != nil {
		baseURL, apiKey, model := s.RuntimeConfig.OpenAICompatValues()
		client := openai_compat.NewClient(baseURL, apiKey, model)
		if client.Configured() {
			return client
		}
	}

	if s.OpenAICompatClient != nil && s.OpenAICompatClient.Configured() {
		return s.OpenAICompatClient
	}
	return nil
}

// MarkMessageRead 标记消息已读
func (s *ChatService) MarkMessageRead(messageID, characterID, userID string) error {
	return s.MarkMessageReadAt(messageID, characterID, userID, time.Now())
}

// MarkMessageReadAt 标记消息已读，并允许调用方复用同一个已读时间。
// message_id 为空时只清零会话未读（如会话列表左滑"标为已读"）。
// 注意：消息查不到（本地消息、已撤回等）不阻断会话未读清零——
// 清零的目标是会话，消息标记只是附带动作。
func (s *ChatService) MarkMessageReadAt(messageID, characterID, userID string, readAt time.Time) error {
	if characterID != "" {
		resolvedCharacterID, err := s.resolveChatCharacterIDAllowMissing(userID, characterID)
		if err != nil {
			return err
		}
		characterID = resolvedCharacterID
	}
	if messageID != "" {
		var message database.Message
		result := s.DB.Where("message_id = ? AND user_id = ?", messageID, userID).First(&message)
		if result.Error != nil {
			if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return result.Error
			}
			// 消息不存在：跳过已读标记，继续清零会话未读
		} else if err := s.DB.Model(&message).Updates(map[string]interface{}{
			"is_read": true,
			"read_at": readAt,
		}).Error; err != nil {
			return err
		}
	}

	// 读到消息即视为消费了该会话的未读（含主动消息累计的未读数）
	if characterID != "" {
		if err := s.DB.Model(&database.Session{}).
			Where("user_id = ? AND character_id = ?", userID, characterID).
			Update("unread_count", 0).Error; err != nil {
			return err
		}
	}
	return nil
}

// DeleteAssistantMessageForRegenerate 软删除一条 AI 回复，供客户端重新生成前调用。
// 与撤回不同：无时限、不改写内容，删除后的消息不再进入历史与上下文。
func (s *ChatService) DeleteAssistantMessageForRegenerate(messageID, userID string) error {
	var message database.Message
	if err := s.DB.Where("message_id = ? AND user_id = ?", messageID, userID).First(&message).Error; err != nil {
		return err
	}
	if message.Role != "assistant" {
		return fmt.Errorf("only assistant messages can be regenerated")
	}
	return s.DB.Delete(&message).Error
}

// RecallMessage 撤回消息
func (s *ChatService) RecallMessage(messageID, userID, reason string) error {
	var message database.Message
	result := s.DB.Where("message_id = ? AND user_id = ?", messageID, userID).First(&message)
	if result.Error != nil {
		return result.Error
	}

	// 检查是否在可撤回时间内（5分钟内）
	elapsed := time.Since(message.CreatedAt)
	if elapsed > 5*time.Minute {
		return fmt.Errorf("超过可撤回时间")
	}

	// 更新撤回状态
	return s.DB.Model(&message).Updates(map[string]interface{}{
		"is_recalled":   true,
		"recalled_at":   time.Now(),
		"recall_reason": reason,
		"content":       "消息已撤回",
	}).Error
}

// GetMessageByID 根据ID获取消息
func (s *ChatService) GetMessageByID(messageID, userID string) (*database.Message, error) {
	var message database.Message
	result := s.DB.Where("message_id = ? AND user_id = ?", messageID, userID).First(&message)
	if result.Error != nil {
		return nil, result.Error
	}
	return &message, nil
}
