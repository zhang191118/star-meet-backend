package v1

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"star-meet-backend/global"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

var apnsTokenPattern = regexp.MustCompile(`^[a-fA-F0-9]{64,128}$`)

type NotificationAPI struct{}

func NewNotificationAPI() *NotificationAPI {
	return &NotificationAPI{}
}

func notificationService() *service.NotificationService {
	return global.NotificationService
}

// List 通知列表
func (a *NotificationAPI) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	svc := notificationService()
	if svc == nil {
		response.FailWithError(c, http.StatusInternalServerError, "notification service unavailable", "notification_unavailable", nil)
		return
	}

	category := c.Query("category")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	notifications, total, err := svc.List(userID, category, page, pageSize)
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "list notifications failed", "notification_list_failed", nil)
		return
	}

	items := make([]gin.H, 0, len(notifications))
	for _, n := range notifications {
		items = append(items, gin.H{
			"id":           n.ID,
			"category":     n.Category,
			"title":        n.Title,
			"preview":      n.Preview,
			"character_id": n.CharacterID,
			"deep_link":    n.DeepLink,
			"is_read":      n.IsRead,
			"created_at":   n.CreatedAt.Unix(),
		})
	}

	response.OkWithData(c, gin.H{
		"list":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// UnreadCount 未读数量
func (a *NotificationAPI) UnreadCount(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	svc := notificationService()
	if svc == nil {
		response.OkWithData(c, gin.H{"unread_count": 0})
		return
	}
	count, err := svc.UnreadCount(userID)
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "unread count failed", "notification_count_failed", nil)
		return
	}
	response.OkWithData(c, gin.H{"unread_count": count})
}

// MarkRead 标记单条已读
func (a *NotificationAPI) MarkRead(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.FailWithError(c, http.StatusBadRequest, "invalid notification id", "invalid_notification_id", nil)
		return
	}
	svc := notificationService()
	if svc == nil {
		response.FailWithError(c, http.StatusInternalServerError, "notification service unavailable", "notification_unavailable", nil)
		return
	}
	if err := svc.MarkRead(userID, uint(id)); err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "mark read failed", "notification_mark_failed", nil)
		return
	}
	response.OkWithMessage(c, "ok")
}

// MarkAllRead 全部已读
func (a *NotificationAPI) MarkAllRead(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	svc := notificationService()
	if svc == nil {
		response.FailWithError(c, http.StatusInternalServerError, "notification service unavailable", "notification_unavailable", nil)
		return
	}
	if err := svc.MarkAllRead(userID); err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "mark all read failed", "notification_mark_failed", nil)
		return
	}
	response.OkWithMessage(c, "ok")
}

// Affection 查询亲密度
func (a *NotificationAPI) Affection(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	characterID := c.Query("character_id")
	if characterID == "" {
		response.FailWithError(c, http.StatusBadRequest, "character_id is required", "missing_character_id", nil)
		return
	}
	svc := notificationService()
	if svc == nil {
		response.FailWithError(c, http.StatusInternalServerError, "notification service unavailable", "notification_unavailable", nil)
		return
	}
	exp, level := svc.GetAffection(userID, characterID)
	response.OkWithData(c, gin.H{"exp": exp, "level": level})
}

func (a *NotificationAPI) RegisterDevice(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Token       string `json:"token" binding:"required"`
		Platform    string `json:"platform"`
		Environment string `json:"environment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}
	if !apnsTokenPattern.MatchString(req.Token) {
		response.FailWithError(c, http.StatusUnprocessableEntity, "invalid device token", "invalid_device_token", nil)
		return
	}
	if req.Platform != "" && req.Platform != "ios" {
		response.FailWithError(c, http.StatusUnprocessableEntity, "unsupported platform", "unsupported_push_platform", nil)
		return
	}
	svc := notificationService()
	if svc == nil {
		response.FailWithError(c, http.StatusInternalServerError, "notification service unavailable", "notification_unavailable", nil)
		return
	}
	if err := svc.RegisterPushDevice(userID, req.Token, req.Platform, req.Environment); err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "register device failed", "push_device_register_failed", nil)
		return
	}
	response.OkWithMessage(c, "ok")
}

func (a *NotificationAPI) UnregisterDevice(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}
	if !apnsTokenPattern.MatchString(req.Token) {
		response.FailWithError(c, http.StatusUnprocessableEntity, "invalid device token", "invalid_device_token", nil)
		return
	}
	svc := notificationService()
	if svc == nil {
		response.FailWithError(c, http.StatusInternalServerError, "notification service unavailable", "notification_unavailable", nil)
		return
	}
	if err := svc.UnregisterPushDevice(userID, req.Token); err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "unregister device failed", "push_device_unregister_failed", nil)
		return
	}
	response.OkWithMessage(c, "ok")
}

func (a *NotificationAPI) GetCharacterProactiveSettings(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	characterID := c.Param("character_id")
	setting, err := notificationService().GetCharacterProactiveSettings(userID, characterID)
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "get proactive settings failed", "proactive_settings_failed", nil)
		return
	}
	response.OkWithData(c, gin.H{"character_id": characterID, "enabled": setting.Enabled, "daily_limit": setting.DailyLimit, "muted_until": setting.MutedUntil})
}

func (a *NotificationAPI) UpdateCharacterProactiveSettings(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	characterID := c.Param("character_id")
	var req struct {
		Enabled    *bool      `json:"enabled"`
		DailyLimit *int       `json:"daily_limit"`
		MutedUntil *time.Time `json:"muted_until"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}
	setting, err := notificationService().UpsertCharacterProactiveSettings(userID, characterID, req.Enabled, req.DailyLimit, req.MutedUntil)
	if err != nil {
		response.FailWithError(c, http.StatusBadRequest, err.Error(), "proactive_settings_invalid", nil)
		return
	}
	response.OkWithData(c, gin.H{"character_id": characterID, "enabled": setting.Enabled, "daily_limit": setting.DailyLimit, "muted_until": setting.MutedUntil})
}

func currentUserID(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists || userID == nil {
		failUnauthenticated(c)
		return "", false
	}
	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		failUnauthenticated(c)
		return "", false
	}
	return userIDStr, true
}
