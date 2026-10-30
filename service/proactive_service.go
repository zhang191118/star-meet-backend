package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"star-meet-backend/model/database"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/pkg/logger"
)

const (
	ProactiveEventPromiseReminder   = "promise_reminder"
	ProactiveEventEventFollowup     = "event_followup"
	ProactiveEventPlotHook          = "open_plot_hook"
	ProactiveEventMissedChat        = "missed_conversation"
	ProactiveEventManualTest        = "manual_test"
	ProactiveEventStatusPending     = "pending"
	ProactiveEventStatusProcessing  = "processing"
	ProactiveEventStatusCancelled   = "cancelled"
	ProactiveEventStatusSent        = "sent"
	proactiveMessageMaxRunes        = 240
	proactiveMissedChatAfter        = 48 * time.Hour
	proactiveMissedChatExpiresAfter = 14 * 24 * time.Hour
	proactiveRepeatAfter            = 72 * time.Hour
)

var errProactiveDeferred = errors.New("proactive event deferred")
var errProactiveCancelled = errors.New("proactive event cancelled")

type ProactiveEventInput struct {
	UserID         string
	CharacterID    string
	EventType      string
	SourceMemoryID string
	TriggerAt      time.Time
	ExpiresAt      *time.Time
	DedupeKey      string
	Reason         string
}

type ProactiveMessage struct {
	Content      string
	ReplyEmotion string
	DirectorNote string
}

type ProactiveMessageGenerator interface {
	GenerateProactiveMessage(user database.User, character database.Character, session database.Session, event database.ProactiveEvent) (ProactiveMessage, error)
}

func (s *NotificationService) ConfigureProactiveGenerator(generator ProactiveMessageGenerator) {
	if s != nil {
		s.ProactiveGenerator = generator
	}
}

func DefaultProactiveSettings() database.UserSettings {
	return database.UserSettings{
		ProactiveQuietStart:    "22:30",
		ProactiveQuietEnd:      "08:00",
		ProactiveDailyLimit:    1,
		ProactiveOnlyWhenAway:  true,
		ProactivePreviewMode:   "name_only",
		PromiseReminderEnabled: true,
	}
}

func normalizeProactiveUserSettings(settings *database.UserSettings) bool {
	if settings == nil {
		return false
	}
	changed := false
	if strings.TrimSpace(settings.ProactiveQuietStart) == "" {
		settings.ProactiveQuietStart = "22:30"
		changed = true
	}
	if strings.TrimSpace(settings.ProactiveQuietEnd) == "" {
		settings.ProactiveQuietEnd = "08:00"
		changed = true
	}
	if strings.TrimSpace(settings.ProactivePreviewMode) == "" {
		settings.ProactivePreviewMode = "name_only"
		changed = true
	}
	// New fields on legacy rows are all zero-valued. Only apply the default
	// away policy when the row has no proactive configuration at all, so an
	// explicit daily limit of zero remains a valid opt-out.
	if settings.ProactiveDailyLimit == 0 && !settings.ProactiveOnlyWhenAway && !settings.PromiseReminderEnabled {
		settings.ProactiveDailyLimit = 1
		settings.ProactiveOnlyWhenAway = true
		settings.PromiseReminderEnabled = true
		changed = true
	}
	return changed
}

func IsProactiveAllowed(settings database.UserSettings, now, lastActive time.Time, sentToday, characterDailyLimit int, mutedUntil *time.Time, characterSentToday ...int) bool {
	if !settings.PushEnabled || !settings.ProactiveEnable || settings.PrivacyMode || sentToday >= normalizedDailyLimit(settings.ProactiveDailyLimit) {
		return false
	}
	characterCount := sentToday
	if len(characterSentToday) > 0 {
		characterCount = characterSentToday[0]
	}
	if characterDailyLimit > 0 && characterCount >= characterDailyLimit {
		return false
	}
	if mutedUntil != nil && now.Before(*mutedUntil) {
		return false
	}
	if settings.ProactiveOnlyWhenAway && !lastActive.IsZero() && now.Sub(lastActive) < 2*time.Hour {
		return false
	}
	return !inQuietHours(now, settings.ProactiveQuietStart, settings.ProactiveQuietEnd)
}

func normalizedDailyLimit(limit int) int {
	if limit < 0 {
		return 0
	}
	if limit > 3 {
		return 3
	}
	return limit
}

func inQuietHours(now time.Time, start, end string) bool {
	startMinutes, okStart := parseClockMinutes(start)
	endMinutes, okEnd := parseClockMinutes(end)
	if !okStart || !okEnd || startMinutes == endMinutes {
		return false
	}
	minutes := now.Hour()*60 + now.Minute()
	if startMinutes < endMinutes {
		return minutes >= startMinutes && minutes < endMinutes
	}
	return minutes >= startMinutes || minutes < endMinutes
}

func parseClockMinutes(value string) (int, bool) {
	var hour, minute int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d:%d", &hour, &minute); err != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func (s *NotificationService) ScheduleProactiveEvent(input ProactiveEventInput) (*database.ProactiveEvent, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("notification service unavailable")
	}
	if input.UserID == "" || input.CharacterID == "" || input.EventType == "" || input.DedupeKey == "" {
		return nil, errors.New("user, character, event type and dedupe key are required")
	}
	if input.TriggerAt.IsZero() {
		input.TriggerAt = time.Now()
	}
	if input.ExpiresAt == nil {
		expiresAt := input.TriggerAt.Add(24 * time.Hour)
		input.ExpiresAt = &expiresAt
	}
	var existing database.ProactiveEvent
	if err := s.DB.Where("dedupe_key = ?", input.DedupeKey).First(&existing).Error; err == nil {
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	event := database.ProactiveEvent{
		EventID: uuid.New().String(), UserID: input.UserID, CharacterID: input.CharacterID,
		EventType: input.EventType, SourceMemoryID: input.SourceMemoryID, TriggerAt: input.TriggerAt,
		ExpiresAt: input.ExpiresAt, DedupeKey: input.DedupeKey, Reason: strings.TrimSpace(input.Reason),
		Status: ProactiveEventStatusPending,
	}
	if err := s.DB.Create(&event).Error; err != nil {
		// The unique index is the final arbiter when two workers schedule the
		// same event concurrently. Return the already-created row to preserve
		// the idempotent API contract.
		if lookupErr := s.DB.Where("dedupe_key = ?", input.DedupeKey).First(&existing).Error; lookupErr == nil {
			return &existing, nil
		}
		return nil, err
	}
	return &event, nil
}

func (s *NotificationService) GetCharacterProactiveSettings(userID, characterID string) (database.UserCharacterNotificationSetting, error) {
	var setting database.UserCharacterNotificationSetting
	err := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return database.UserCharacterNotificationSetting{UserID: userID, CharacterID: characterID, Enabled: true, DailyLimit: 1}, nil
	}
	return setting, err
}

func (s *NotificationService) UpsertCharacterProactiveSettings(userID, characterID string, enabled *bool, dailyLimit *int, mutedUntil *time.Time) (database.UserCharacterNotificationSetting, error) {
	if userID == "" || characterID == "" {
		return database.UserCharacterNotificationSetting{}, errors.New("user and character are required")
	}
	setting, err := s.GetCharacterProactiveSettings(userID, characterID)
	if err != nil {
		return setting, err
	}
	if enabled != nil {
		setting.Enabled = *enabled
	}
	if dailyLimit != nil {
		setting.DailyLimit = normalizedDailyLimit(*dailyLimit)
	}
	if mutedUntil != nil {
		if mutedUntil.IsZero() || mutedUntil.Before(time.Now()) {
			setting.MutedUntil = nil
		} else {
			setting.MutedUntil = mutedUntil
		}
	}
	if setting.ID == 0 {
		err = s.DB.Create(&setting).Error
	} else {
		err = s.DB.Save(&setting).Error
	}
	return setting, err
}

// RunProactiveScanner consumes due events. Event creation remains separate so
// promise, plot and missed-conversation producers can use the same delivery
// guarantees without coupling themselves to APNs.
func (s *NotificationService) RunProactiveScanner(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.processProactiveTick(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.processProactiveTick(ctx, now)
		}
	}
}

func (s *NotificationService) processProactiveTick(ctx context.Context, now time.Time) {
	if s == nil || s.APNs == nil {
		return
	}
	if _, err := s.ScheduleMissedConversationCandidates(now, 100); err != nil {
		logger.Warn("主动陪伴候选事件生成失败", "error", err)
	}
	if _, err := s.ProcessDueProactiveEvents(ctx, now, 100); err != nil {
		logger.Warn("主动陪伴事件扫描失败", "error", err)
	}
}

// ScheduleMissedConversationCandidates creates at most one candidate per
// user/character/day for conversations that have gone quiet. Delivery still
// applies the user's quiet hours, away policy, limits and character settings.
func (s *NotificationService) ScheduleMissedConversationCandidates(now time.Time, limit int) (int, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	oldest := now.Add(-proactiveMissedChatExpiresAfter)
	quietSince := now.Add(-proactiveMissedChatAfter)
	var sessions []database.Session
	if err := s.DB.Where("last_message_at IS NOT NULL AND last_message_at <= ? AND last_message_at >= ?", quietSince, oldest).
		Order("last_message_at ASC, id ASC").Limit(limit).Find(&sessions).Error; err != nil {
		return 0, err
	}

	dayKey := now.In(taipeiLocation()).Format("2006-01-02")
	created := 0
	for _, session := range sessions {
		var user database.User
		if err := s.DB.Where("user_id = ?", session.UserID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return created, err
		}
		event, err := s.ScheduleProactiveEvent(ProactiveEventInput{
			UserID:      session.UserID,
			CharacterID: session.CharacterID,
			EventType:   ProactiveEventMissedChat,
			TriggerAt:   now,
			ExpiresAt:   ptrTime(now.Add(24 * time.Hour)),
			DedupeKey:   fmt.Sprintf("missed_chat:%s:%s:%s", session.UserID, session.CharacterID, dayKey),
			Reason:      i18n.TranslateNotification("notif_proactive_missed_reason", user.Language),
		})
		if err != nil {
			return created, err
		}
		if event.CreatedAt.Equal(event.UpdatedAt) || event.CreatedAt.After(now.Add(-time.Second)) {
			created++
		}
	}
	return created, nil
}

func (s *NotificationService) ProcessDueProactiveEvents(ctx context.Context, now time.Time, limit int) (int, error) {
	if s == nil || s.DB == nil || s.APNs == nil {
		return 0, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	var events []database.ProactiveEvent
	staleProcessingAt := now.Add(-10 * time.Minute)
	if err := s.DB.Where("trigger_at <= ? AND ((status = ?) OR (status = ? AND (processing_at IS NULL OR processing_at < ?)))", now, ProactiveEventStatusPending, ProactiveEventStatusProcessing, staleProcessingAt).
		Order("trigger_at ASC, id ASC").Limit(limit).Find(&events).Error; err != nil {
		return 0, err
	}

	sent := 0
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		if event.ExpiresAt != nil && !now.Before(*event.ExpiresAt) {
			s.DB.Model(&database.ProactiveEvent{}).
				Where("id = ? AND status IN ?", event.ID, []string{ProactiveEventStatusPending, ProactiveEventStatusProcessing}).
				Updates(map[string]interface{}{"status": ProactiveEventStatusCancelled, "processing_at": nil, "last_error": "expired"})
			continue
		}
		claimed := s.DB.Model(&database.ProactiveEvent{}).
			Where("id = ? AND ((status = ?) OR (status = ? AND (processing_at IS NULL OR processing_at < ?)))", event.ID, ProactiveEventStatusPending, ProactiveEventStatusProcessing, staleProcessingAt).
			Updates(map[string]interface{}{"status": ProactiveEventStatusProcessing, "processing_at": now, "attempt_count": gorm.Expr("attempt_count + 1")}).RowsAffected
		if claimed != 1 {
			continue
		}

		if err := s.deliverProactiveEvent(ctx, event, now); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errProactiveCancelled) {
				_ = s.markProactiveEvent(event.ID, ProactiveEventStatusCancelled, nil)
				continue
			}
			if errors.Is(err, errProactiveDeferred) {
				_ = s.markProactiveEvent(event.ID, ProactiveEventStatusPending, nil)
			} else {
				_ = s.markProactiveEventFailure(event.ID, err)
			}
			if !errors.Is(err, errProactiveDeferred) {
				logger.Warn("主动陪伴事件发送失败", "eventID", event.EventID, "error", err)
			}
			continue
		}
		sentAt := now
		if err := s.markProactiveEvent(event.ID, ProactiveEventStatusSent, &sentAt); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

func (s *NotificationService) deliverProactiveEvent(ctx context.Context, event database.ProactiveEvent, now time.Time) error {
	if s.ProactiveGenerator == nil {
		return errProactiveDeferred
	}
	var user database.User
	if err := s.DB.Where("user_id = ?", event.UserID).First(&user).Error; err != nil {
		return err
	}
	var settings database.UserSettings
	if err := s.DB.Where("user_id = ?", event.UserID).First(&settings).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		settings = defaultUserSettings(event.UserID)
	} else if err != nil {
		return err
	}
	if normalizeProactiveUserSettings(&settings) {
		_ = s.DB.Save(&settings).Error
	}
	if event.EventType == ProactiveEventPromiseReminder && !settings.PromiseReminderEnabled {
		return errProactiveDeferred
	}

	var character database.Character
	if err := s.DB.Where("character_id = ?", event.CharacterID).First(&character).Error; err != nil {
		return err
	}
	characterSetting, err := s.GetCharacterProactiveSettings(event.UserID, event.CharacterID)
	if err != nil {
		return err
	}

	lastActive := user.LastLoginAt
	var session database.Session
	sessionErr := s.DB.Where("user_id = ? AND character_id = ?", event.UserID, event.CharacterID).
		Order("last_message_at DESC").First(&session).Error
	if sessionErr == nil && session.LastMessageAt != nil {
		if lastActive == nil || session.LastMessageAt.After(*lastActive) {
			lastActive = session.LastMessageAt
		}
	} else if errors.Is(sessionErr, gorm.ErrRecordNotFound) {
		session = database.Session{SessionID: uuid.New().String(), UserID: event.UserID, CharacterID: event.CharacterID}
		if err := s.DB.Create(&session).Error; err != nil {
			return err
		}
	} else if sessionErr != nil {
		return sessionErr
	}

	location, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		location = time.FixedZone("Asia/Taipei", 8*60*60)
	}
	taipeiNow := now.In(location)
	dayStart := time.Date(taipeiNow.Year(), taipeiNow.Month(), taipeiNow.Day(), 0, 0, 0, 0, location)
	var sentToday int64
	if err := s.DB.Model(&database.ProactiveEvent{}).
		Where("user_id = ? AND status = ? AND sent_at >= ?", event.UserID, ProactiveEventStatusSent, dayStart).
		Count(&sentToday).Error; err != nil {
		return err
	}
	var characterSentToday int64
	if err := s.DB.Model(&database.ProactiveEvent{}).
		Where("user_id = ? AND character_id = ? AND status = ? AND sent_at >= ?", event.UserID, event.CharacterID, ProactiveEventStatusSent, dayStart).
		Count(&characterSentToday).Error; err != nil {
		return err
	}
	var lastActiveAt time.Time
	if lastActive != nil {
		lastActiveAt = *lastActive
	}
	manualTest := event.EventType == ProactiveEventManualTest
	if !characterSetting.Enabled || !settings.PushEnabled || !settings.ProactiveEnable || settings.PrivacyMode {
		return errProactiveDeferred
	}
	if !manualTest {
		if !IsProactiveAllowed(settings, taipeiNow, lastActiveAt, int(sentToday), characterSetting.DailyLimit, characterSetting.MutedUntil, int(characterSentToday)) {
			return errProactiveDeferred
		}
		if characterSetting.DailyLimit > 0 && int(characterSentToday) >= characterSetting.DailyLimit {
			return errProactiveDeferred
		}
	}
	var currentEvent database.ProactiveEvent
	if err := s.DB.Select("status").First(&currentEvent, event.ID).Error; err != nil {
		return err
	}
	if currentEvent.Status != ProactiveEventStatusProcessing {
		return errProactiveCancelled
	}
	if !manualTest {
		var recentlySent int64
		if err := s.DB.Model(&database.ProactiveEvent{}).
			Where("id <> ? AND user_id = ? AND character_id = ? AND status = ? AND sent_at >= ?", event.ID, event.UserID, event.CharacterID, ProactiveEventStatusSent, now.Add(-proactiveRepeatAfter)).
			Count(&recentlySent).Error; err != nil {
			return err
		}
		if recentlySent > 0 {
			return errProactiveDeferred
		}
	}

	var devices []database.PushDevice
	if err := s.DB.Where("user_id = ? AND platform = ? AND is_active = ?", event.UserID, "ios", true).Find(&devices).Error; err != nil {
		return err
	}
	if len(devices) == 0 {
		return errProactiveDeferred
	}

	name := safeName(character.Name, user.Language)
	previewMode := settings.ProactivePreviewMode
	if previewMode == "" {
		previewMode = "name_only"
	}
	var generated ProactiveMessage
	messageID := proactiveMessageID(event.EventID)
	if strings.TrimSpace(event.AssistantMessageID) != "" {
		var existingMessage database.Message
		if err := s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", event.AssistantMessageID, event.UserID, event.CharacterID, "assistant").First(&existingMessage).Error; err == nil {
			generated = ProactiveMessage{Content: existingMessage.Content, ReplyEmotion: existingMessage.ReplyEmotion, DirectorNote: existingMessage.DirectorNote}
		}
	}
	if strings.TrimSpace(generated.Content) == "" {
		var existingMessage database.Message
		if err := s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", messageID, event.UserID, event.CharacterID, "assistant").First(&existingMessage).Error; err == nil {
			generated = ProactiveMessage{Content: existingMessage.Content, ReplyEmotion: existingMessage.ReplyEmotion, DirectorNote: existingMessage.DirectorNote}
			event.AssistantMessageID = messageID
			_ = s.DB.Model(&database.ProactiveEvent{}).Where("id = ?", event.ID).Updates(map[string]interface{}{"assistant_message_id": messageID})
		}
	}
	if strings.TrimSpace(generated.Content) == "" {
		generated, err = s.ProactiveGenerator.GenerateProactiveMessage(user, character, session, event)
		if err != nil {
			return err
		}
		generated.Content = normalizeProactiveMessage(generated.Content)
		if generated.Content == "" {
			return errors.New("主动陪伴 AI 返回空消息")
		}
		generatedAt := now
		assistantMessage := database.Message{
			MessageID: messageID, SessionID: session.SessionID, CharacterID: event.CharacterID,
			UserID: event.UserID, Role: "assistant", Content: generated.Content, MessageType: "text",
			ReplyEmotion: generated.ReplyEmotion, DirectorNote: generated.DirectorNote, IsRead: false,
		}
		if err := s.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&assistantMessage).Error; err != nil {
				return err
			}
			result := tx.Model(&database.ProactiveEvent{}).Where("id = ? AND status = ?", event.ID, ProactiveEventStatusProcessing).Updates(map[string]interface{}{
				"assistant_message_id": messageID, "generated_at": generatedAt, "last_error": "",
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errProactiveCancelled
			}
			return nil
		}); err != nil {
			return err
		}
		event.AssistantMessageID = messageID
		_ = s.DB.Model(&database.Session{}).Where("session_id = ?", session.SessionID).Updates(map[string]interface{}{
			"last_message": generated.Content, "last_message_at": now, "unread_count": gorm.Expr("unread_count + 1"),
		}).Error
	}
	var beforeDelivery database.ProactiveEvent
	if err := s.DB.Select("status").First(&beforeDelivery, event.ID).Error; err != nil {
		return err
	}
	if beforeDelivery.Status != ProactiveEventStatusProcessing {
		return errProactiveCancelled
	}

	body := generated.Content
	if previewMode == "name_only" {
		body = fmt.Sprintf(i18n.TranslateNotification("notif_proactive_preview", user.Language), name)
	}
	title := fmt.Sprintf(i18n.TranslateNotification("notif_proactive_title", user.Language), name)
	payload := map[string]interface{}{
		"aps": map[string]interface{}{
			"alert": map[string]string{"title": title, "body": body},
			"sound": "default",
		},
		"type":                 "proactive_message",
		"event_id":             event.EventID,
		"character_id":         character.CharacterID,
		"character_name":       character.Name,
		"character_avatar":     character.Avatar,
		"character_background": character.Background,
		"background_image":     character.BgImage,
		"message_id":           event.AssistantMessageID,
		"session_id":           session.SessionID,
		"message_preview":      generated.Content,
	}

	var lastErr error
	delivered := 0
	for _, device := range devices {
		if err := s.APNs.Send(ctx, device, payload); err != nil {
			lastErr = err
			var apnsErr *APNsError
			if errors.As(err, &apnsErr) && (apnsErr.Status == http.StatusGone || apnsErr.Reason == "BadDeviceToken" || apnsErr.Reason == "DeviceTokenNotForTopic") {
				_ = s.DB.Model(&database.PushDevice{}).Where("id = ?", device.ID).Update("is_active", false).Error
			}
			continue
		}
		delivered++
	}
	if delivered == 0 {
		return lastErr
	}
	if err := s.createProactiveNotification(event, title, body); err != nil {
		logger.Warn("主动陪伴站内通知写入失败", "eventID", event.EventID, "error", err)
	}
	return nil
}

func proactiveMessageID(eventID string) string {
	return "proactive-" + strings.TrimSpace(eventID)
}

func (s *NotificationService) markProactiveEvent(id uint, status string, sentAt *time.Time) error {
	updates := map[string]interface{}{"status": status, "processing_at": nil, "last_error": ""}
	if sentAt != nil {
		updates["sent_at"] = sentAt
	}
	result := s.DB.Model(&database.ProactiveEvent{}).Where("id = ? AND status = ?", id, ProactiveEventStatusProcessing).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errProactiveCancelled
	}
	return nil
}

func (s *NotificationService) markProactiveEventFailure(id uint, cause error) error {
	lastError := "主动陪伴事件处理失败"
	if cause != nil {
		lastError = trimProactiveError(cause.Error(), 500)
	}
	var event database.ProactiveEvent
	if err := s.DB.Select("attempt_count").First(&event, id).Error; err != nil {
		return err
	}
	status := ProactiveEventStatusPending
	if event.AttemptCount >= 3 {
		status = ProactiveEventStatusCancelled
	}
	return s.DB.Model(&database.ProactiveEvent{}).Where("id = ? AND status = ?", id, ProactiveEventStatusProcessing).Updates(map[string]interface{}{
		"status": status, "processing_at": nil, "last_error": lastError,
	}).Error
}

func trimProactiveError(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}

// CancelPendingProactiveEvents stops candidates as soon as the user resumes
// the conversation. A new candidate can be scheduled on a later quiet day.
func (s *NotificationService) CancelPendingProactiveEvents(userID, characterID string) error {
	if s == nil || s.DB == nil || userID == "" || characterID == "" {
		return nil
	}
	return s.DB.Model(&database.ProactiveEvent{}).
		Where("user_id = ? AND character_id = ? AND status IN ?", userID, characterID, []string{ProactiveEventStatusPending, ProactiveEventStatusProcessing}).
		Updates(map[string]interface{}{"status": ProactiveEventStatusCancelled, "processing_at": nil, "last_error": "user_replied"}).Error
}

func normalizeProactiveMessage(content string) string {
	content = strings.TrimSpace(content)
	content = strings.Trim(content, "`\"'“”‘’")
	content = strings.TrimSpace(content)
	if len([]rune(content)) > proactiveMessageMaxRunes {
		content = string([]rune(content)[:proactiveMessageMaxRunes])
	}
	return content
}

func taipeiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.FixedZone("Asia/Taipei", 8*60*60)
	}
	return location
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
