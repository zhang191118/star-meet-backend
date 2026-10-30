package service

import (
	"errors"
	"time"

	"star-meet-backend/model/database"

	"gorm.io/gorm"
)

var ErrChatQuotaExceeded = errors.New("chat quota exceeded")

type ChatQuotaExceededError struct {
	RemainingMessages int
	ResetAt           int64
	Used              int
	Limit             int
}

func (e *ChatQuotaExceededError) Error() string {
	if e == nil {
		return ErrChatQuotaExceeded.Error()
	}
	return ErrChatQuotaExceeded.Error()
}

func (e *ChatQuotaExceededError) Unwrap() error {
	return ErrChatQuotaExceeded
}

type ChatQuotaStatus struct {
	Enabled           bool
	Unlimited         bool
	MembershipActive  bool
	Limit             int
	Used              int
	RemainingMessages int
	ResetAt           int64
}

func (s *ChatService) quotaSettings() ChatQuotaSettings {
	if s != nil && s.RuntimeConfig != nil {
		return s.RuntimeConfig.ChatQuotaSettings()
	}
	return DefaultChatQuotaSettings()
}

func chatQuotaLocation(timezone string) *time.Location {
	if timezone == "" {
		timezone = DefaultChatQuotaResetTimezone
	}
	if loc, err := time.LoadLocation(timezone); err == nil && loc != nil {
		return loc
	}
	if loc, err := time.LoadLocation(DefaultChatQuotaResetTimezone); err == nil && loc != nil {
		return loc
	}
	return time.FixedZone("Asia/Taipei", 8*3600)
}

func chatQuotaDayWindow(now time.Time, timezone string) (start, resetAt time.Time) {
	loc := chatQuotaLocation(timezone)
	local := now.In(loc)
	start = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start, start.Add(24 * time.Hour)
}

func countUserMessagesInRange(db *gorm.DB, userID string, start, end time.Time) (int64, error) {
	if db == nil {
		return 0, nil
	}
	var count int64
	err := db.Unscoped().Model(&database.Message{}).
		Where("user_id = ? AND role = ? AND created_at >= ? AND created_at < ?", userID, "user", start, end).
		Count(&count).Error
	return count, err
}

func evaluateChatQuota(db *gorm.DB, settings ChatQuotaSettings, user database.User, now time.Time) (ChatQuotaStatus, error) {
	start, resetAt := chatQuotaDayWindow(now, settings.ResetTimezone)
	status := ChatQuotaStatus{
		Enabled:           settings.Enabled,
		MembershipActive:  userHasActiveMembershipAt(user, now),
		Limit:             settings.FreeDailyMessages,
		ResetAt:           resetAt.Unix(),
		RemainingMessages: settings.FreeDailyMessages,
	}
	if !settings.Enabled || (status.MembershipActive && settings.MemberUnlimited) {
		status.Unlimited = true
		status.RemainingMessages = unlimitedRemainingMessages
		return status, nil
	}

	used, err := countUserMessagesInRange(db, user.UserID, start, resetAt)
	if err != nil {
		return ChatQuotaStatus{}, err
	}
	status.Used = int(used)
	remaining := settings.FreeDailyMessages - status.Used
	if remaining < 0 {
		remaining = 0
	}
	status.RemainingMessages = remaining
	return status, nil
}

func (s *ChatService) ChatQuotaSnapshot(user database.User, now time.Time) (ChatQuotaStatus, error) {
	db := (*gorm.DB)(nil)
	if s != nil {
		db = s.DB
	}
	return evaluateChatQuota(db, s.quotaSettings(), user, now)
}

func (s *ChatService) existingChatMessage(userID, characterID, messageID, role string) bool {
	if s == nil || s.DB == nil || messageID == "" {
		return false
	}
	var existing database.Message
	err := s.DB.Where("message_id = ? AND user_id = ? AND character_id = ? AND role = ?", messageID, userID, characterID, role).
		First(&existing).Error
	return err == nil
}

// CheckNewUserMessageQuota 在真正写入一条新的用户消息前检查每日额度。
// 已存在的 user/reply 消息视为幂等重试，不重复扣额度，也不拦截。
func (s *ChatService) CheckNewUserMessageQuota(userID, characterID, userMsgID, replyMsgID string) error {
	resolvedCharacterID, err := s.ResolveChatCharacterID(userID, characterID)
	if err != nil {
		return err
	}
	return s.checkNewUserMessageQuota(userID, resolvedCharacterID, userMsgID, replyMsgID)
}

// ResolveChatCharacterAndCheckQuota applies the legacy ID mapping before the
// API's pre-stream quota gate and returns the ID the service should use.
func (s *ChatService) ResolveChatCharacterAndCheckQuota(userID, characterID, userMsgID, replyMsgID string) (string, error) {
	resolvedCharacterID, err := s.ResolveChatCharacterID(userID, characterID)
	if err != nil {
		return "", err
	}
	if err := s.checkNewUserMessageQuota(userID, resolvedCharacterID, userMsgID, replyMsgID); err != nil {
		return "", err
	}
	return resolvedCharacterID, nil
}

func (s *ChatService) checkNewUserMessageQuota(userID, characterID, userMsgID, replyMsgID string) error {
	settings := s.quotaSettings()
	if !settings.Enabled {
		return nil
	}
	if s.existingChatMessage(userID, characterID, replyMsgID, "assistant") {
		return nil
	}
	if s.existingChatMessage(userID, characterID, userMsgID, "user") {
		return nil
	}

	var user database.User
	if s != nil && s.DB != nil {
		if err := s.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			user = database.User{UserID: userID, MemberLevel: "free"}
		}
	} else {
		user = database.User{UserID: userID, MemberLevel: "free"}
	}

	status, err := evaluateChatQuota(s.DB, settings, user, time.Now())
	if err != nil {
		return err
	}
	if status.Unlimited || status.RemainingMessages > 0 {
		return nil
	}
	return &ChatQuotaExceededError{
		RemainingMessages: 0,
		ResetAt:           status.ResetAt,
		Used:              status.Used,
		Limit:             status.Limit,
	}
}
