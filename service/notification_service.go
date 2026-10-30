package service

import (
	"fmt"
	"star-meet-backend/model/database"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/pkg/logger"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (s *NotificationService) RegisterPushDevice(userID, token, platform, environment string) error {
	if platform == "" {
		platform = "ios"
	}
	if environment != "sandbox" {
		environment = "production"
	}

	now := time.Now()
	var device database.PushDevice
	err := s.DB.Where("token = ?", token).First(&device).Error
	if err == nil {
		return s.DB.Model(&device).Updates(map[string]interface{}{
			"user_id":      userID,
			"platform":     platform,
			"environment":  environment,
			"is_active":    true,
			"last_seen_at": now,
		}).Error
	}
	if !isNotFound(err) {
		return err
	}

	return s.DB.Create(&database.PushDevice{
		UserID:      userID,
		Token:       token,
		Platform:    platform,
		Environment: environment,
		IsActive:    true,
		LastSeenAt:  now,
	}).Error
}

func (s *NotificationService) UnregisterPushDevice(userID, token string) error {
	return s.DB.Model(&database.PushDevice{}).
		Where("user_id = ? AND token = ?", userID, token).
		Updates(map[string]interface{}{"is_active": false, "last_seen_at": time.Now()}).Error
}

type NotificationService struct {
	DB                 *gorm.DB
	APNs               *APNsClient
	ProactiveGenerator ProactiveMessageGenerator
}

func NewNotificationService(db *gorm.DB) *NotificationService {
	return &NotificationService{DB: db}
}

func (s *NotificationService) ConfigureAPNs(client *APNsClient) {
	if s != nil {
		s.APNs = client
	}
}

// 通知分类
const (
	NotifyCategorySystem      = "system"
	NotifyCategoryCharacter   = "character"
	NotifyCategoryInteraction = "interaction"
	NotifyCategoryProactive   = "proactive"
)

// 亲密度等级阈值（与 iOS 端 LevelConfig 保持一致）
var affectionLevelThresholds = []int{0, 100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500}

func levelForExp(exp int) int {
	level := 1
	for i, threshold := range affectionLevelThresholds {
		if exp >= threshold {
			level = i + 1
		}
	}
	if level > 10 {
		level = 10
	}
	return level
}

// Create 写入一条通知（异步安全，失败仅记录日志）
func (s *NotificationService) Create(userID, category, title, preview string) {
	if s == nil || s.DB == nil || userID == "" {
		return
	}
	notification := database.Notification{
		UserID:   userID,
		Category: category,
		Title:    title,
		Preview:  preview,
	}
	if err := s.DB.Create(&notification).Error; err != nil {
		logger.Warn("创建通知失败", "error", err, "userID", userID, "title", title)
	}
}

func (s *NotificationService) createProactiveNotification(event database.ProactiveEvent, title, preview string) error {
	var existing database.Notification
	if err := s.DB.Where("proactive_event_id = ?", event.EventID).First(&existing).Error; err == nil {
		return nil
	}
	return s.DB.Create(&database.Notification{
		UserID:           event.UserID,
		Category:         NotifyCategoryProactive,
		Title:            title,
		Preview:          preview,
		CharacterID:      event.CharacterID,
		ProactiveEventID: event.EventID,
		DeepLink:         "starmeet://chat/" + event.CharacterID,
	}).Error
}

// List 分页查询通知
func (s *NotificationService) List(userID, category string, page, pageSize int) ([]database.Notification, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	query := s.DB.Model(&database.Notification{}).Where("user_id = ?", userID)
	if category != "" && category != "all" {
		query = query.Where("category = ?", category)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var notifications []database.Notification
	err := query.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&notifications).Error
	if err != nil {
		return nil, 0, err
	}
	return notifications, total, nil
}

// UnreadCount 未读数量
func (s *NotificationService) UnreadCount(userID string) (int64, error) {
	var count int64
	err := s.DB.Model(&database.Notification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Count(&count).Error
	return count, err
}

// MarkRead 标记单条已读
func (s *NotificationService) MarkRead(userID string, id uint) error {
	return s.DB.Model(&database.Notification{}).
		Where("user_id = ? AND id = ?", userID, id).
		Update("is_read", true).Error
}

// MarkAllRead 全部已读
func (s *NotificationService) MarkAllRead(userID string) error {
	return s.DB.Model(&database.Notification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Update("is_read", true).Error
}

// AddChatAffection 聊天增加亲密度，升级时自动发通知。返回最新 exp/level。
func (s *NotificationService) AddChatAffection(userID, characterID, characterName, lang string, bonus int) (int, int) {
	if s == nil || s.DB == nil || userID == "" || characterID == "" {
		return 0, 1
	}

	var affection database.UserAffection
	err := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).First(&affection).Error
	if err != nil && !isNotFound(err) {
		logger.Warn("读取亲密度失败", "error", err)
		return 0, 1
	}
	if isNotFound(err) {
		affection = database.UserAffection{UserID: userID, CharacterID: characterID, Exp: 0, Level: 1}
		if err := s.DB.Create(&affection).Error; err != nil {
			logger.Warn("初始化亲密度失败", "error", err)
			return 0, 1
		}
	}

	affection.Exp += 1 + bonus // 消息 +1，回复 bonus
	newLevel := levelForExp(affection.Exp)

	if newLevel > affection.Level {
		oldLevel := affection.Level
		affection.Level = newLevel
		s.Create(userID, NotifyCategoryInteraction,
			i18n.TranslateNotification("notif_affection_title", lang),
			fmt.Sprintf(i18n.TranslateNotification("notif_affection_preview", lang), safeName(characterName, lang), newLevel))
		logger.Info("亲密度升级", "userID", userID, "characterID", characterID, "from", oldLevel, "to", newLevel)
	}

	if err := s.DB.Model(&database.UserAffection{}).
		Where("user_id = ? AND character_id = ?", userID, characterID).
		Updates(map[string]interface{}{"exp": affection.Exp, "level": affection.Level}).Error; err != nil {
		logger.Warn("更新亲密度失败", "error", err)
	}

	// 同步角色表 affection 字段，便于列表展示
	s.DB.Model(&database.Character{}).
		Where("character_id = ?", characterID).
		Update("affection", affection.Exp)

	return affection.Exp, affection.Level
}

// GetAffection 查询亲密度
func (s *NotificationService) GetAffection(userID, characterID string) (int, int) {
	var affection database.UserAffection
	err := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).First(&affection).Error
	if err != nil {
		return 0, 1
	}
	return affection.Exp, affection.Level
}

// NotifyNewCharacter 邂逅/抽卡获得新角色通知
func (s *NotificationService) NotifyNewCharacter(userID, characterName, lang, rarity string) {
	title := i18n.TranslateNotification("notif_new_char_title", lang)
	preview := fmt.Sprintf(i18n.TranslateNotification("notif_encounter_preview", lang), safeName(characterName, lang))
	if rarity != "" {
		preview = fmt.Sprintf(i18n.TranslateNotification("notif_gacha_preview", lang), strings.ToUpper(rarity), safeName(characterName, lang))
	}
	s.Create(userID, NotifyCategoryCharacter, title, preview)
}

// NotifyWelcome 注册欢迎通知
func (s *NotificationService) NotifyWelcome(userID, lang string) {
	s.Create(userID, NotifyCategorySystem,
		i18n.TranslateNotification("notif_welcome_title", lang),
		i18n.TranslateNotification("notif_welcome_preview", lang))
}

func safeName(name, lang string) string {
	if strings.TrimSpace(name) == "" {
		return i18n.TranslateNotification("notif_default_name", lang)
	}
	return name
}

func isNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
