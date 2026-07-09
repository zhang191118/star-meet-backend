package v1

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"star-meet-backend/global"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ChatAPI struct {
	ChatService *service.ChatService
}

func NewChatAPI(chatService *service.ChatService) *ChatAPI {
	return &ChatAPI{ChatService: chatService}
}

// StreamMessage 流式发送消息
// @Summary 流式发送消息
// @Tags 聊天模块
// @Accept json
// @Produce text/event-stream
// @Param data body request.SendMessageReq true "请求参数"
// @Success 200 {string} string "stream"
// @Router /api/v1/chat/stream [post]
func (a *ChatAPI) StreamMessage(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	var req request.SendMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	if req.MessageType == "" {
		req.MessageType = "text"
	}

	req.UserID = userID.(string)

	streamCh, errCh := a.ChatService.StreamMessage(req.UserID, req.CharacterID, req.Message, req.MessageType, req.UserMessageID, req.ReplyMessageID)

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-streamCh:
			if !ok {
				c.SSEvent("done", "[DONE]")
				return false
			}
			if event.Type == "meta" {
				// 发送元数据（消息ID、情绪等）
				// 注意：这里我们使用 JSON 格式发送 meta 事件
				c.SSEvent("meta", event.Meta)
			} else {
				// 发送内容
				c.SSEvent("message", event.Content)
			}
			return true
		case err := <-errCh:
			if err != nil {
				c.SSEvent("error", err.Error())
				return false
			}
			return true
		}
	})
}

// SendMessage 发送消息
// @Summary 发送消息
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param data body request.SendMessageReq true "请求参数"
// @Success 200 {object} response.SendMessageResp
// @Router /api/v1/chat/message [post]
func (a *ChatAPI) SendMessage(c *gin.Context) {
	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	var req request.SendMessageReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	// 验证必填字段
	if req.CharacterID == "" {
		response.FailWithMessage(c, "缺少角色ID")
		return
	}
	if req.Message == "" {
		response.FailWithMessage(c, "消息内容不能为空")
		return
	}

	req.UserID = userIDStr

	// 设置默认值
	if req.MessageType == "" {
		req.MessageType = "text"
	}

	result, err := a.ChatService.SendMessageWithIDs(req.UserID, req.CharacterID, req.Message, req.MessageType, req.UserMessageID, req.ReplyMessageID)
	if err != nil {
		if errors.Is(err, service.ErrChatCharacterUnavailable) {
			response.FailWithError(c, http.StatusNotFound, "character unavailable", "character_unavailable", gin.H{
				"field":  "character_id",
				"reason": "not_found",
			})
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// GetHistory 获取聊天历史
// @Summary 获取聊天历史
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param character_id query string true "角色ID"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.MessageResp
// @Router /api/v1/chat/history [get]
func (a *ChatAPI) GetHistory(c *gin.Context) {
	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	characterID := c.Query("character_id")
	if characterID == "" {
		failMissingField(c, "character_id")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	beforeID := c.Query("before_id")
	page, pageSize = normalizePagination(page, pageSize, 20)

	var (
		result []response.MessageResp
		total  int64
		err    error
	)
	if beforeID != "" {
		result, total, err = a.ChatService.GetHistoryBefore(userIDStr, characterID, beforeID, page, pageSize)
	} else {
		result, total, err = a.ChatService.GetHistory(userIDStr, characterID, page, pageSize)
	}
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"list":           result,
		"total":          total,
		"page":           page,
		"page_size":      pageSize,
		"has_more":       hasMore(page, pageSize, total),
		"before_id":      beforeID,
		"next_before_id": nextBeforeID(result),
	})
}

// GetSessions 获取会话列表
// @Summary 获取会话列表
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Success 200 {object} response.SessionResp
// @Router /api/v1/chat/sessions [get]
func (a *ChatAPI) GetSessions(c *gin.Context) {
	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = normalizePagination(page, pageSize, 20)

	result, total, err := a.ChatService.GetSessions(userIDStr, page, pageSize)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"has_more":  hasMore(page, pageSize, total),
	})
}

func normalizePagination(page, pageSize, defaultPageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func hasMore(page, pageSize int, total int64) bool {
	return int64(page*pageSize) < total
}

func nextBeforeID(messages []response.MessageResp) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].MessageID
}

// DeleteSession 删除会话
// @Summary 删除会话
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param session_id path string true "会话ID"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/session/{session_id} [delete]
func (a *ChatAPI) DeleteSession(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	sessionID := c.Param("session_id")

	err := a.ChatService.DeleteSession(sessionID, userID.(string))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failSessionNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// UpdateSession 更新会话
// @Summary 更新会话（置顶等）
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param session_id path string true "会话ID"
// @Param data body object true "更新字段"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/session/{session_id} [put]
func (a *ChatAPI) UpdateSession(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	sessionID := c.Param("session_id")

	var req struct {
		IsPinned *bool `json:"is_pinned"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}
	if req.IsPinned == nil {
		failMissingField(c, "is_pinned")
		return
	}

	err := a.ChatService.UpdateSession(sessionID, userID.(string), req.IsPinned)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failSessionNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

func failSessionNotFound(c *gin.Context) {
	response.FailWithError(c, http.StatusNotFound, "session not found", "session_not_found", gin.H{
		"field": "session_id",
	})
}

// DeleteSessionByCharacter 按角色ID删除会话
// @Summary 按角色ID删除会话
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param character_id query string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/session [delete]
func (a *ChatAPI) DeleteSessionByCharacter(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	characterID := c.Query("character_id")
	if characterID == "" {
		failMissingField(c, "character_id")
		return
	}

	err := a.ChatService.DeleteSessionByCharacter(characterID, userID.(string))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failSessionNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// ClearChatHistory 清除聊天历史
// @Summary 清除聊天历史
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param character_id query string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/clear-history [delete]
func (a *ChatAPI) ClearChatHistory(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	characterID := c.Query("character_id")

	if characterID == "" {
		failMissingField(c, "character_id")
		return
	}

	err := a.ChatService.ClearChatHistory(userID.(string), characterID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failSessionNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// MarkMessageRead 标记消息已读
// @Summary 标记消息已读
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param data body object true "请求参数"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/message/read [post]
func (a *ChatAPI) MarkMessageRead(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	var req struct {
		MessageID   string `json:"message_id" binding:"required"`
		CharacterID string `json:"character_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	readAt := time.Now()
	err := a.ChatService.MarkMessageReadAt(req.MessageID, userIDStr, readAt)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failMessageNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"message_id": req.MessageID,
		"read_at":    readAt.Unix(),
	})
}

// RecallMessage 撤回消息
// @Summary 撤回消息
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param message_id path string true "消息ID"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/message/{message_id} [delete]
func (a *ChatAPI) RecallMessage(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	messageID := c.Param("message_id")
	if messageID == "" {
		response.FailWithMessage(c, "缺少消息ID")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		failBindError(c, err, req)
		return
	}

	err := a.ChatService.RecallMessage(messageID, userIDStr, req.Reason)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failMessageNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithMessage(c, "撤回成功")
}

// GetMessageReadStatus 获取消息已读状态
// @Summary 获取消息已读状态
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param message_id query string true "消息ID"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/message/read [get]
func (a *ChatAPI) GetMessageReadStatus(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	messageID := c.Query("message_id")
	if messageID == "" {
		failMissingField(c, "message_id")
		return
	}

	message, err := a.ChatService.GetMessageByID(messageID, userID.(string))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failMessageNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	readAt := int64(0)
	if message.ReadAt != nil {
		readAt = message.ReadAt.Unix()
	}

	response.OkWithData(c, gin.H{
		"is_read": message.IsRead,
		"read_at": readAt,
	})
}

func failMessageNotFound(c *gin.Context) {
	response.FailWithError(c, http.StatusNotFound, "message not found", "message_not_found", gin.H{
		"field": "message_id",
	})
}

// Health 健康检查 - 增强版
func (a *ChatAPI) Health(c *gin.Context) {
	components := make(map[string]string)
	allHealthy := true

	// 检查数据库
	if err := a.ChatService.DB.Raw("SELECT 1").Error; err != nil {
		components["database"] = "disconnected"
		allHealthy = false
	} else {
		components["database"] = "connected"
	}

	// 检查Redis
	if err := a.ChatService.Redis.Ping(a.ChatService.Redis.Context()).Err(); err != nil {
		components["redis"] = "disconnected"
		allHealthy = false
	} else {
		components["redis"] = "connected"
	}

	// 检查AI服务 (MiniMax)
	if global.MiniMaxClient != nil && global.MiniMaxClient.APIKey != "" {
		components["minimax"] = "configured"
	} else {
		components["minimax"] = "not_configured"
	}

	// 检查AI服务 (DeepSeek)
	if global.DeepSeekClient != nil && global.DeepSeekClient.APIKey != "" {
		components["deepseek"] = "configured"
	} else {
		components["deepseek"] = "not_configured"
	}

	// 检查AI服务 (Qwen)
	if global.QwenClient != nil && global.QwenClient.APIKey != "" {
		components["qwen"] = "configured"
	} else {
		components["qwen"] = "not_configured"
	}

	// 检查Mem0
	if global.Mem0Client != nil {
		components["mem0"] = "configured"
	} else {
		components["mem0"] = "not_configured"
	}

	// 检查Qdrant
	if global.QdrantClient != nil {
		components["qdrant"] = "configured"
	} else {
		components["qdrant"] = "not_configured"
	}

	status := "running"
	if !allHealthy {
		status = "degraded"
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "ok",
		"data": gin.H{
			"service":    "star-meet-backend",
			"status":     status,
			"components": components,
		},
	})
}
