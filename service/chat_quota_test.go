package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"star-meet-backend/model/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupChatQuotaTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.User{},
		&database.UserSettings{},
		&database.Character{},
		&database.Session{},
		&database.Message{},
		&database.Block{},
		&database.RuntimeConfig{},
	); err != nil {
		t.Fatalf("migrate quota tables: %v", err)
	}
	return db
}

func createQuotaTestUser(t *testing.T, db *gorm.DB, userID, email, level string, expire *time.Time) {
	t.Helper()
	user := database.User{
		UserID:       userID,
		Email:        email,
		Nickname:     userID,
		MemberLevel:  level,
		MemberExpire: expire,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user %s: %v", userID, err)
	}
}

func createQuotaTestCharacter(t *testing.T, db *gorm.DB, characterID string) {
	t.Helper()
	if err := db.Create(&database.Character{
		CharacterID:  characterID,
		UserID:       "system_admin",
		Name:         "额度角色",
		Gender:       "female",
		Relationship: "伙伴",
		IsPublic:     1,
	}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
}

func seedUserMessages(t *testing.T, db *gorm.DB, userID, characterID string, count int, createdAt time.Time) {
	t.Helper()
	for i := 0; i < count; i++ {
		msg := database.Message{
			MessageID:   fmt.Sprintf("%s-user-%d-%d", userID, createdAt.UnixNano(), i),
			SessionID:   userID + "-session",
			CharacterID: characterID,
			UserID:      userID,
			Role:        "user",
			Content:     fmt.Sprintf("hello-%d", i),
			MessageType: "text",
			CreatedAt:   createdAt,
		}
		if err := db.Create(&msg).Error; err != nil {
			t.Fatalf("seed user message: %v", err)
		}
	}
}

func quotaServiceWithLimit(t *testing.T, db *gorm.DB, limit int) *ChatService {
	t.Helper()
	runtimeConfig := &RuntimeConfigService{DB: db}
	if err := runtimeConfig.SeedDefaults(ChatQuotaRuntimeDefaults()); err != nil {
		t.Fatalf("seed quota defaults: %v", err)
	}
	if _, err := runtimeConfig.Upsert(RuntimeConfigChatQuotaFreeDailyMessages, fmt.Sprintf("%d", limit), false, ""); err != nil {
		t.Fatalf("upsert daily messages: %v", err)
	}
	return &ChatService{DB: db, RuntimeConfig: runtimeConfig}
}

func TestCheckNewUserMessageQuotaBlocksFreeUserAtLimit(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-free", "quota-free@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-char")
	svc := quotaServiceWithLimit(t, db, 2)
	now := time.Now()
	seedUserMessages(t, db, "quota-free", "quota-char", 2, now)

	err := svc.CheckNewUserMessageQuota("quota-free", "quota-char", "new-user-msg", "new-reply-msg")
	var quotaErr *ChatQuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("expected quota exceeded, got %v", err)
	}
	if quotaErr.RemainingMessages != 0 || quotaErr.Limit != 2 || quotaErr.Used != 2 {
		t.Fatalf("unexpected quota error: %+v", quotaErr)
	}
}

func TestCheckNewUserMessageQuotaAllowsTwentiethMessage(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-free-20", "quota-free-20@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-char-20")
	svc := quotaServiceWithLimit(t, db, 20)
	seedUserMessages(t, db, "quota-free-20", "quota-char-20", 19, time.Now())

	if err := svc.CheckNewUserMessageQuota("quota-free-20", "quota-char-20", "user-20", "reply-20"); err != nil {
		t.Fatalf("20th message should pass, got %v", err)
	}

	seedUserMessages(t, db, "quota-free-20", "quota-char-20", 1, time.Now())
	if err := svc.CheckNewUserMessageQuota("quota-free-20", "quota-char-20", "user-21", "reply-21"); err == nil {
		t.Fatal("21st message should be blocked")
	}
}

func TestCheckNewUserMessageQuotaAllowsActiveMemberUnlimited(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	expire := time.Now().Add(24 * time.Hour)
	createQuotaTestUser(t, db, "quota-vip", "quota-vip@example.com", "monthly", &expire)
	createQuotaTestCharacter(t, db, "quota-vip-char")
	svc := quotaServiceWithLimit(t, db, 2)
	seedUserMessages(t, db, "quota-vip", "quota-vip-char", 5, time.Now())

	if err := svc.CheckNewUserMessageQuota("quota-vip", "quota-vip-char", "vip-user-msg", "vip-reply-msg"); err != nil {
		t.Fatalf("active member should be unlimited, got %v", err)
	}
}

func TestCheckNewUserMessageQuotaTreatsExpiredMemberAsFree(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	expire := time.Now().Add(-time.Hour)
	createQuotaTestUser(t, db, "quota-expired", "quota-expired@example.com", "monthly", &expire)
	createQuotaTestCharacter(t, db, "quota-expired-char")
	svc := quotaServiceWithLimit(t, db, 2)
	seedUserMessages(t, db, "quota-expired", "quota-expired-char", 2, time.Now())

	if err := svc.CheckNewUserMessageQuota("quota-expired", "quota-expired-char", "expired-user-msg", "expired-reply-msg"); err == nil {
		t.Fatal("expired member should use free quota")
	}
}

func TestCheckNewUserMessageQuotaTreatsExpireEqualNowAsActive(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	now := time.Now()
	createQuotaTestUser(t, db, "quota-edge-vip", "quota-edge-vip@example.com", "yearly", &now)
	svc := quotaServiceWithLimit(t, db, 1)
	status, err := svc.ChatQuotaSnapshot(database.User{
		UserID:       "quota-edge-vip",
		MemberLevel:  "yearly",
		MemberExpire: &now,
	}, now)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !status.Unlimited || !status.MembershipActive {
		t.Fatalf("expire==now should still count as active, got %+v", status)
	}
}

func TestCheckNewUserMessageQuotaIgnoresAssistantAndOpeningMessages(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-assistant", "quota-assistant@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-assistant-char")
	svc := quotaServiceWithLimit(t, db, 1)

	if err := db.Create(&database.Message{
		MessageID:   "opening-assistant",
		SessionID:   "s1",
		CharacterID: "quota-assistant-char",
		UserID:      "quota-assistant",
		Role:        "assistant",
		Content:     "你好，我是开场白",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatalf("create assistant message: %v", err)
	}

	if err := svc.CheckNewUserMessageQuota("quota-assistant", "quota-assistant-char", "first-user", "first-reply"); err != nil {
		t.Fatalf("assistant opening should not consume quota, got %v", err)
	}
}

func TestCheckNewUserMessageQuotaAllowsIdempotentRetry(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-retry", "quota-retry@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-retry-char")
	svc := quotaServiceWithLimit(t, db, 1)
	if err := db.Create(&database.Message{
		MessageID:   "existing-user-msg",
		SessionID:   "s1",
		CharacterID: "quota-retry-char",
		UserID:      "quota-retry",
		Role:        "user",
		Content:     "already sent",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatalf("create existing user message: %v", err)
	}

	if err := svc.CheckNewUserMessageQuota("quota-retry", "quota-retry-char", "existing-user-msg", "new-reply"); err != nil {
		t.Fatalf("idempotent retry should not be blocked, got %v", err)
	}
}

func TestCheckNewUserMessageQuotaCountsSoftDeletedMessages(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-deleted", "quota-deleted@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-deleted-char")
	svc := quotaServiceWithLimit(t, db, 1)
	msg := database.Message{
		MessageID:   "soft-deleted-user",
		SessionID:   "s1",
		CharacterID: "quota-deleted-char",
		UserID:      "quota-deleted",
		Role:        "user",
		Content:     "deleted later",
		MessageType: "text",
		CreatedAt:   time.Now(),
	}
	if err := db.Create(&msg).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := db.Delete(&msg).Error; err != nil {
		t.Fatalf("soft delete message: %v", err)
	}

	if err := svc.CheckNewUserMessageQuota("quota-deleted", "quota-deleted-char", "another-user", "another-reply"); err == nil {
		t.Fatal("soft deleted user messages should still consume quota")
	}
}

func TestCheckNewUserMessageQuotaResetsOnTaipeiDayBoundary(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-taipei", "quota-taipei@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-taipei-char")
	svc := quotaServiceWithLimit(t, db, 1)

	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterday := start.Add(-time.Hour)
	seedUserMessages(t, db, "quota-taipei", "quota-taipei-char", 1, yesterday)

	if err := svc.CheckNewUserMessageQuota("quota-taipei", "quota-taipei-char", "today-user", "today-reply"); err != nil {
		t.Fatalf("yesterday messages should not count after Taipei reset, got %v", err)
	}
}

func TestStreamMessageReturnsQuotaErrorBeforeOpeningStream(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-stream", "quota-stream@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-stream-char")
	svc := quotaServiceWithLimit(t, db, 1)
	seedUserMessages(t, db, "quota-stream", "quota-stream-char", 1, time.Now())

	events, errs := svc.StreamMessage("quota-stream", "quota-stream-char", "hello", "text", "stream-user", "stream-reply", "")
	select {
	case err := <-errs:
		var quotaErr *ChatQuotaExceededError
		if !errors.As(err, &quotaErr) {
			t.Fatalf("expected quota exceeded, got %v", err)
		}
	case event := <-events:
		t.Fatalf("expected quota error before stream events, got %+v", event)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for quota error")
	}
}

func TestChatQuotaSnapshotReportsRemainingForFreeUser(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "quota-remain", "quota-remain@example.com", "free", nil)
	createQuotaTestCharacter(t, db, "quota-remain-char")
	svc := quotaServiceWithLimit(t, db, 12)
	seedUserMessages(t, db, "quota-remain", "quota-remain-char", 3, time.Now())

	var user database.User
	if err := db.Where("user_id = ?", "quota-remain").First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	status, err := svc.ChatQuotaSnapshot(user, time.Now())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if status.Used != 3 || status.RemainingMessages != 9 || status.Unlimited {
		t.Fatalf("unexpected snapshot: %+v", status)
	}
}

func TestUserHasActiveMembershipAt(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Second)
	future := now.Add(time.Hour)

	if userHasActiveMembershipAt(database.User{MemberLevel: "free"}, now) {
		t.Fatal("free user should not be active member")
	}
	if userHasActiveMembershipAt(database.User{MemberLevel: "monthly", MemberExpire: &expired}, now) {
		t.Fatal("expired member should not be active")
	}
	if !userHasActiveMembershipAt(database.User{MemberLevel: "monthly"}, now) {
		t.Fatal("paid member with nil expire should still be active")
	}
	if !userHasActiveMembershipAt(database.User{MemberLevel: "monthly", MemberExpire: &now}, now) {
		t.Fatal("expire==now should still be active")
	}
	if !userHasActiveMembershipAt(database.User{MemberLevel: "vip", MemberExpire: &future}, now) {
		t.Fatal("future expire should be active")
	}
	if !userHasActiveMembershipAt(database.User{MemberLevel: "weekly", MemberExpire: &future}, now) {
		t.Fatal("weekly member (incl. free trial period) should be active")
	}
	if userHasActiveMembershipAt(database.User{MemberLevel: "weekly", MemberExpire: &expired}, now) {
		t.Fatal("expired weekly member should not be active")
	}
}
