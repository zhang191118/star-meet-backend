package service

import (
	"encoding/json"
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
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/logger"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrChatCharacterUnavailable = errors.New("chat character unavailable")

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
}

// SendMessage 发送消息
func (s *ChatService) SendMessage(userID, characterID, message, msgType string) (*response.SendMessageResp, error) {
	return s.SendMessageWithIDs(userID, characterID, message, msgType, "", "")
}

// SendMessageWithIDs 发送消息，并在前端提供消息ID时支持弱网重试幂等。
func (s *ChatService) SendMessageWithIDs(userID, characterID, message, msgType, userMsgID, replyMsgID string) (*response.SendMessageResp, error) {
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
	} else if err != nil {
		return nil, err
	}

	// 5. 构建Prompt并调用AI
	reply, replyEmotion, err := s.generateReply(userID, character, message, userEmotion)
	if err != nil {
		logger.Error("AI生成回复失败", "error", err, "userID", userID, "characterID", characterID)
		reply = "抱歉，我现在有点累，让我休息一下好不好~"
		replyEmotion = emotion.EmotionSad
	}

	// 打印AI回复到终端
	fmt.Printf("\n========== AI Reply (Non-Stream) ==========\n%s\n===========================================\n", reply)

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
				CreatedAt:    existingReply.CreatedAt.Unix(),
			}, nil
		}
		return nil, err
	}

	// 7. 更新会话
	now := time.Now()
	s.DB.Model(&database.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]interface{}{
			"last_message":    reply,
			"last_message_at": now,
		})

	// 8. 异步写入记忆（不阻塞响应）
	go s.asyncAddMemory(userID, characterID, message, reply)

	// 9. 更新角色聊天次数
	s.DB.Model(&character).Update("chat_count", gorm.Expr("chat_count + 1"))

	// 10. 更新用户统计
	s.DB.Model(&database.User{}).Where("user_id = ?", userID).Update("msg_count", gorm.Expr("msg_count + 2"))

	return &response.SendMessageResp{
		MessageID:    replyID,
		Reply:        reply,
		ReplyType:    "text",
		UserEmotion:  userEmotion,
		ReplyEmotion: replyEmotion,
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
func (s *ChatService) getChatHistory(userID, characterID string, messageCount int) string {
	var messages []database.Message
	s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).
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

// generateReply 生成回复
func (s *ChatService) generateReply(userID string, character database.Character, userMessage, userEmotion string) (string, string, error) {
	prompt, err := s.buildPrompt(userID, character, userMessage, userEmotion)
	if err != nil {
		return "", "", err
	}

	// 优先使用 OpenAI-compatible 网关模型（例如 ai98pro/grok-4.3-fast）。
	if openAICompatClient := s.currentOpenAICompatClient(); openAICompatClient != nil {
		reply, err := openAICompatClient.Generate(prompt, character.SystemPrompt, 1024)
		if err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, nil
		}
		logger.Warn("OpenAI-compatible 调用失败，尝试降级", "error", err)
	}

	// 降级到 MiniMax
	if s.MiniMaxClient != nil {
		reply, err := s.MiniMaxClient.Generate(prompt, character.SystemPrompt, 1024)
		if err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, nil
		}
		logger.Warn("MiniMax调用失败，尝试降级", "error", err)
	}

	// 降级到 DeepSeek
	if s.DeepSeekClient != nil {
		reply, err := s.DeepSeekClient.Generate(prompt, character.SystemPrompt, 1024)
		if err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, nil
		}
		logger.Warn("DeepSeek调用失败，尝试降级", "error", err)
	}

	// 降级到Gemini
	if s.GeminiClient != nil {
		reply, err := s.GeminiClient.Generate(prompt, character.SystemPrompt, 1024)
		if err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, nil
		}
	}

	// 降级到直接调用Qwen
	if s.QwenClient != nil {
		reply, err := s.QwenClient.Generate(prompt, character.SystemPrompt, 1024)
		if err == nil {
			replyEmotion := emotion.GetReplyEmotion(userEmotion)
			return reply, replyEmotion, nil
		}
		logger.Warn("Qwen调用失败，返回默认回复", "error", err)
	}

	// 如果都没有配置，返回默认回复
	return getDefaultReply(userEmotion), emotion.GetReplyEmotion(userEmotion), nil
}

// getDefaultReply 获取默认回复
func getDefaultReply(userEmotion string) string {
	replies := map[string][]string{
		emotion.EmotionHappy:   {"好开心能和你分享快乐~", "你开心我也开心！", "哇，太棒了！"},
		emotion.EmotionSad:     {"抱抱你~不舒服的话可以跟我聊聊", "我一直都在你身边", "乖~有我在"},
		emotion.EmotionAngry:   {"消消气嘛~", "别生气啦，对身体不好", "我陪你"},
		emotion.EmotionLove:    {"我也好想你~", "爱你哟", "啾咪~"},
		emotion.EmotionLonely:  {"我会一直陪着你", "别怕，有我在", "我们永远在一起"},
		emotion.EmotionNeutral: {"嗯嗯，我听着呢", "然后呢？", "我在~"},
	}

	if list, ok := replies[userEmotion]; ok {
		return list[time.Now().Unix()%int64(len(list))]
	}
	return "嗯嗯，我听着呢"
}

// asyncAddMemory 异步添加记忆
func (s *ChatService) asyncAddMemory(userID, characterID, userMsg, aiMsg string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("异步添加记忆失败", "panic", r)
		}
	}()

	userSettings := s.getUserSettings(userID)
	if !userSettings.MemoryEnabled || userSettings.PrivacyMode {
		return
	}

	content := fmt.Sprintf("用户：%s\nAI：%s", userMsg, aiMsg)

	// 存入Qdrant向量数据库
	if s.QdrantClient != nil && s.EmbeddingClient != nil {
		vector, err := s.EmbeddingClient.GetEmbedding(content)
		if err == nil {
			// 使用整数ID
			memoryID := time.Now().UnixNano()
			err = s.QdrantClient.Upsert("memories", []qdrant.Point{
				{
					ID:     memoryID,
					Vector: vector,
					Payload: map[string]interface{}{
						"memory_id":    fmt.Sprintf("%d", memoryID),
						"user_id":      userID,
						"character_id": characterID,
						"content":      content,
						"type":         "chat",
						"timestamp":    time.Now().Unix(),
					},
				},
			})
			if err != nil {
				logger.Warn("Qdrant添加记忆失败", "error", err)
			}
		}
	}

	// 存入Mem0（如果启用）
	if s.Mem0Client != nil {
		_, err := s.Mem0Client.Add(
			fmt.Sprintf("user_%s", userID),
			fmt.Sprintf("char_%s", characterID),
			content,
			map[string]interface{}{
				"type":      "chat",
				"timestamp": time.Now().Unix(),
			},
		)
		if err != nil {
			logger.Warn("Mem0添加记忆失败", "error", err)
		}
	}

	// 存入本地数据库
	mem := database.Memory{
		MemoryID:    uuid.New().String(),
		UserID:      userID,
		CharacterID: characterID,
		Content:     content,
		MemoryType:  "chat",
		Source:      "auto",
	}
	s.DB.Create(&mem)
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
	err := query.
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&messages).Error

	if err != nil {
		return nil, 0, err
	}

	// 初始化为空切片，避免 JSON 返回 null
	resp := make([]response.MessageResp, 0, len(messages))
	for _, m := range messages {
		resp = append(resp, response.MessageResp{
			MessageID:     m.MessageID,
			Role:          m.Role,
			Content:       m.Content,
			MessageType:   m.MessageType,
			VoiceURL:      m.VoiceURL,
			VoiceDuration: m.VoiceDuration,
			UserEmotion:   m.UserEmotion,
			CreatedAt:     m.CreatedAt.Unix(),
		})
	}

	return resp, total, nil
}

// GetSessions 获取会话列表
func (s *ChatService) GetSessions(userID string, page, pageSize int) ([]response.SessionResp, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pageSize = clampPageSize(pageSize)

	var sessions []database.Session
	var total int64

	s.DB.Model(&database.Session{}).
		Where("user_id = ?", userID).
		Count(&total)

	offset := (page - 1) * pageSize
	err := s.DB.Where("user_id = ?", userID).
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
				CharacterID:  char.CharacterID,
				Name:         char.Name,
				Avatar:       char.Avatar,
				Relationship: char.Relationship,
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
	var session database.Session
	if err := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).First(&session).Error; err != nil {
		return err
	}

	// 删除该用户与该角色的所有消息
	return s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).Delete(&database.Message{}).Error
}

// buildPrompt 构建提示词
func (s *ChatService) buildPrompt(userID string, character database.Character, userMessage, userEmotion string) (string, error) {
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
	case "撒娇型":
		styleHint = "用撒娇的语气回复，可以适当撒骄，使用~和🥺等符号"
	case "温暖型":
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
	historyText := s.getChatHistory(userID, character.CharacterID, 4) // 最近4轮对话

	// 2. 检索记忆 - 使用Qdrant向量数据库
	var memText string
	if userSettings.MemoryEnabled && !userSettings.PrivacyMode && s.QdrantClient != nil && s.EmbeddingClient != nil {
		// 获取用户消息的embedding
		vector, err := s.EmbeddingClient.GetEmbedding(userMessage)
		if err == nil {
			// 搜索相似记忆
			collection := "memories"
			filter := map[string]interface{}{
				"must": []map[string]interface{}{
					{"key": "user_id", "match": map[string]interface{}{"value": userID}},
				},
			}
			results, err := s.QdrantClient.Search(collection, vector, 5, filter)
			if err == nil && len(results) > 0 {
				for _, r := range results {
					if payload, ok := r.Payload["content"].(string); ok {
						memText += payload + "\n"
					}
				}
			}
		}
	}

	// 如果Qdrant没有结果，尝试使用Mem0
	if memText == "" && userSettings.MemoryEnabled && !userSettings.PrivacyMode && s.Mem0Client != nil {
		memories, err := s.Mem0Client.Search(
			fmt.Sprintf("user_%s", userID),
			fmt.Sprintf("char_%s", character.CharacterID),
			userMessage,
			5,
		)
		if err == nil && len(memories) > 0 {
			for _, m := range memories {
				memText += m.Memory + "\n"
			}
		}
	}

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

	// 3. 获取用户信息（名字）
	var userName string
	var userInfo database.User
	if err := s.DB.Where("user_id = ?", userID).First(&userInfo).Error; err == nil {
		userName = userInfo.Nickname
	}
	if userName == "" {
		userName = "用户"
	}

	companionModeHint := map[string]string{
		"romantic":   "保持恋人感和亲密感，可以自然表达想念、依赖和偏爱。",
		"friend":     "像很懂用户的亲密朋友，轻松自然，减少过强暧昧表达。",
		"supportive": "以治愈、陪伴、安抚为主，更稳重体贴，优先接住情绪。",
	}[userSettings.RelationshipMode]
	if companionModeHint == "" {
		companionModeHint = "保持自然亲密的陪伴氛围。"
	}

	responseStyleHint := map[string]string{
		"balanced": "整体自然、稳定、不过度用力。",
		"gentle":   "更温柔、更会安抚，语气放软。",
		"playful":  "更俏皮、更有互动感，可以带一点撒娇和玩笑。",
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

	// 4. 构建更自然的Prompt - 优化版
	prompt := fmt.Sprintf(`【基础角色信息】
名字：%s
性格：%s
关系：%s
背景：%s
互动风格：%s
语气提示：%s

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
- 可以撒娇、关心、调皮，但要自然
- 绝对不要列出1、2、3这样的要点
- 不要使用"根据角色设定"这样的开场白
- 像真人在手机打字一样自然，可以稍作思考再回复
- 适当使用表情符号增强情感表达

【场景参考】
%s

【用户刚才说的话】
%s

请代入角色，用最自然、最情感丰富的方式回复！`,
		character.Name,
		character.Personality,
		character.Relationship,
		character.Background,
		character.Style,
		styleHint,
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
		character.Name,
		knowledgeText,
		userMessage)

	return prompt, nil
}

type StreamMeta struct {
	MessageID      string `json:"message_id"`
	SessionID      string `json:"session_id"`
	UserMessageID  string `json:"user_message_id"`
	ReplyMessageID string `json:"reply_message_id"`
	ReplyEmotion   string `json:"reply_emotion"`
	CreatedAt      int64  `json:"created_at"`
}

type StreamEvent struct {
	Type    string      `json:"type"` // "content", "meta"
	Content string      `json:"content,omitempty"`
	Meta    *StreamMeta `json:"meta,omitempty"`
}

// StreamMessage 流式发送消息
func (s *ChatService) StreamMessage(userID, characterID, message, msgType, userMsgID, replyMsgID string) (<-chan StreamEvent, <-chan error) {
	outCh := make(chan StreamEvent)
	errCh := make(chan error, 1)

	// 1. 获取角色信息
	var character database.Character
	if err := s.DB.Where("character_id = ?", characterID).First(&character).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = ErrChatCharacterUnavailable
		}
		errCh <- err
		close(outCh)
		close(errCh)
		return outCh, errCh
	}

	go func() {
		defer close(outCh)
		defer close(errCh)

		// 2. 情绪识别
		userEmotion := emotion.Detect(message)
		replyEmotion := emotion.GetReplyEmotion(userEmotion)

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
				outCh <- StreamEvent{
					Type: "meta",
					Meta: &StreamMeta{
						MessageID:      existingReply.MessageID,
						SessionID:      existingReply.SessionID,
						UserMessageID:  messageID,
						ReplyMessageID: existingReply.MessageID,
						ReplyEmotion:   existingReply.ReplyEmotion,
						CreatedAt:      existingReply.CreatedAt.Unix(),
					},
				}
				return
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
				errCh <- err
				return
			}
		} else if err != nil {
			errCh <- err
			return
		}

		// 发送元数据事件
		outCh <- StreamEvent{
			Type: "meta",
			Meta: &StreamMeta{
				MessageID:      replyID,
				SessionID:      sessionID,
				UserMessageID:  messageID,
				ReplyMessageID: replyID,
				ReplyEmotion:   replyEmotion,
				CreatedAt:      time.Now().Unix(),
			},
		}

		// 5. 构建Prompt
		prompt, err := s.buildPrompt(userID, character, message, userEmotion)
		if err != nil {
			errCh <- err
			return
		}

		finalizeReply := func(reply string, finalReplyEmotion string) error {
			if reply == "" {
				return fmt.Errorf("AI生成回复为空")
			}

			logger.Infof("AI Reply (Stream): %s", reply)

			aiMsg := database.Message{
				MessageID:    replyID,
				SessionID:    sessionID,
				CharacterID:  characterID,
				UserID:       userID,
				Role:         "assistant",
				Content:      reply,
				MessageType:  "text",
				ReplyEmotion: finalReplyEmotion,
			}
			if err := s.DB.Create(&aiMsg).Error; err != nil {
				return err
			}

			now := time.Now()
			s.DB.Model(&database.Session{}).
				Where("session_id = ?", sessionID).
				Updates(map[string]interface{}{
					"last_message":    reply,
					"last_message_at": now,
				})

			go s.asyncAddMemory(userID, characterID, message, reply)
			s.DB.Model(&character).Update("chat_count", gorm.Expr("chat_count + 1"))
			s.DB.Model(&database.User{}).Where("user_id = ?", userID).Update("msg_count", gorm.Expr("msg_count + 2"))

			return nil
		}

		sendFallbackReply := func(cause error) bool {
			logger.Warn("流式生成失败，降级为非流式回复", "error", cause, "userID", userID, "characterID", characterID)
			reply, fallbackEmotion, fallbackErr := s.generateReply(userID, character, message, userEmotion)
			if fallbackErr != nil {
				errCh <- fallbackErr
				return false
			}
			if reply == "" {
				errCh <- fmt.Errorf("AI生成回复为空")
				return false
			}

			outCh <- StreamEvent{
				Type:    "content",
				Content: reply,
			}
			if err := finalizeReply(reply, fallbackEmotion); err != nil {
				errCh <- err
				return false
			}
			return true
		}

		// 6. 调用流式生成，失败时降级到非流式生成，保证客户端能收到可落库的助手回复。
		var streamCh <-chan string
		var streamErrCh <-chan error
		if openAICompatClient := s.currentOpenAICompatClient(); openAICompatClient != nil {
			streamCh, streamErrCh = openAICompatClient.StreamGenerate(prompt, character.SystemPrompt, 1024)
		} else if s.MiniMaxClient != nil {
			streamCh, streamErrCh = s.MiniMaxClient.StreamGenerate(prompt, character.SystemPrompt, 1024)
		} else {
			sendFallbackReply(fmt.Errorf("stream client not initialized"))
			return
		}

		var fullReply strings.Builder

		timeout := time.After(120 * time.Second)
		for streamCh != nil || streamErrCh != nil {
			select {
			case content, ok := <-streamCh:
				if !ok {
					streamCh = nil
					continue
				}
				fullReply.WriteString(content)
				outCh <- StreamEvent{
					Type:    "content",
					Content: content,
				}
			case err, ok := <-streamErrCh:
				streamErrCh = nil
				if ok && err != nil {
					if fullReply.Len() > 0 {
						if saveErr := finalizeReply(fullReply.String(), replyEmotion); saveErr != nil {
							errCh <- saveErr
						}
						return
					}
					sendFallbackReply(err)
					return
				}
			case <-timeout:
				if fullReply.Len() > 0 {
					if saveErr := finalizeReply(fullReply.String(), replyEmotion); saveErr != nil {
						errCh <- saveErr
					}
					return
				}
				sendFallbackReply(fmt.Errorf("AI生成超时"))
				return
			}
		}

		reply := fullReply.String()
		if reply == "" {
			sendFallbackReply(fmt.Errorf("AI生成回复为空"))
			return
		}

		// 7. 保存AI回复
		if err := finalizeReply(reply, replyEmotion); err != nil {
			errCh <- err
			return
		}
	}()

	return outCh, errCh
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
func (s *ChatService) MarkMessageRead(messageID, userID string) error {
	return s.MarkMessageReadAt(messageID, userID, time.Now())
}

// MarkMessageReadAt 标记消息已读，并允许调用方复用同一个已读时间。
func (s *ChatService) MarkMessageReadAt(messageID, userID string, readAt time.Time) error {
	var message database.Message
	result := s.DB.Where("message_id = ? AND user_id = ?", messageID, userID).First(&message)
	if result.Error != nil {
		return result.Error
	}

	// 更新已读状态
	return s.DB.Model(&message).Updates(map[string]interface{}{
		"is_read": true,
		"read_at": readAt,
	}).Error
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

// MarshalJSON 自定义JSON序列化
func (s *ChatService) MarshalJSON() []byte {
	data, _ := json.Marshal(s)
	return data
}
