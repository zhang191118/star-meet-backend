package service

import (
	"context"
	"testing"
	"time"

	"star-meet-backend/model/database"
)

func TestIsProactiveAllowedHonorsTaiwanQuietHoursAndAwayWindow(t *testing.T) {
	settings := database.UserSettings{
		PushEnabled: true, ProactiveEnable: true, ProactiveQuietStart: "22:30", ProactiveQuietEnd: "08:00",
		ProactiveDailyLimit: 1, ProactiveOnlyWhenAway: true,
	}
	day := time.Date(2026, 8, 23, 12, 0, 0, 0, time.FixedZone("Asia/Taipei", 8*60*60))
	if IsProactiveAllowed(settings, day, day.Add(-30*time.Minute), 0, 1, nil) {
		t.Fatal("recently active user should not receive proactive message")
	}
	if IsProactiveAllowed(settings, day.Add(11*time.Hour), day.Add(-10*time.Hour), 0, 1, nil) {
		t.Fatal("user should not receive proactive message during quiet hours")
	}
	if !IsProactiveAllowed(settings, day, day.Add(-3*time.Hour), 0, 1, nil) {
		t.Fatal("eligible away user should receive proactive message")
	}
}

func TestIsProactiveAllowedHonorsLimitsAndPrivacy(t *testing.T) {
	settings := database.UserSettings{PushEnabled: true, ProactiveEnable: true, ProactiveDailyLimit: 1}
	now := time.Now()
	if IsProactiveAllowed(settings, now, now.Add(-3*time.Hour), 1, 1, nil) {
		t.Fatal("daily limit should block delivery")
	}
	settings.PrivacyMode = true
	if IsProactiveAllowed(settings, now, now.Add(-3*time.Hour), 0, 1, nil) {
		t.Fatal("privacy mode should block delivery")
	}
	settings.PrivacyMode = false
	mutedUntil := now.Add(time.Hour)
	if IsProactiveAllowed(settings, now, now.Add(-3*time.Hour), 0, 1, &mutedUntil) {
		t.Fatal("character mute should block delivery")
	}
}

func TestIsProactiveAllowedSeparatesUserAndCharacterLimits(t *testing.T) {
	settings := database.UserSettings{PushEnabled: true, ProactiveEnable: true, ProactiveDailyLimit: 3}
	now := time.Now()
	if !IsProactiveAllowed(settings, now, now.Add(-3*time.Hour), 1, 1, nil, 0) {
		t.Fatal("a different character's message should not consume this character's daily limit")
	}
	if IsProactiveAllowed(settings, now, now.Add(-3*time.Hour), 1, 1, nil, 1) {
		t.Fatal("this character's own daily limit should block delivery")
	}
}

func TestScheduleProactiveEventIsIdempotentByDedupeKey(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.ProactiveEvent{}, &database.UserCharacterNotificationSetting{}); err != nil {
		t.Fatalf("migrate proactive tables: %v", err)
	}
	service := NewNotificationService(db)
	input := ProactiveEventInput{UserID: "proactive-user", CharacterID: "proactive-char", EventType: ProactiveEventPromiseReminder, TriggerAt: time.Now(), DedupeKey: "promise:memory-1", Reason: "週末看展"}
	first, err := service.ScheduleProactiveEvent(input)
	if err != nil {
		t.Fatalf("schedule first event: %v", err)
	}
	second, err := service.ScheduleProactiveEvent(input)
	if err != nil {
		t.Fatalf("schedule duplicate event: %v", err)
	}
	if first.EventID != second.EventID {
		t.Fatalf("expected same event for duplicate key, got %s and %s", first.EventID, second.EventID)
	}
	var count int64
	db.Model(&database.ProactiveEvent{}).Where("dedupe_key = ?", input.DedupeKey).Count(&count)
	if count != 1 {
		t.Fatalf("expected one deduplicated event, got %d", count)
	}
}

func TestScheduleMissedConversationCandidatesDeduplicatesPerTaiwanDay(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.ProactiveEvent{}, &database.UserCharacterNotificationSetting{}); err != nil {
		t.Fatalf("migrate proactive tables: %v", err)
	}
	now := time.Date(2026, 8, 23, 14, 0, 0, 0, time.FixedZone("Asia/Taipei", 8*60*60))
	lastMessageAt := now.Add(-72 * time.Hour)
	if err := db.Create(&database.User{UserID: "missed-user", Language: "zh-Hant", Region: "TW"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.UserSettings{UserID: "missed-user", PushEnabled: true, ProactiveEnable: true, ProactiveDailyLimit: 1, ProactiveOnlyWhenAway: true}).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := db.Create(&database.Character{CharacterID: "missed-char", UserID: "system_admin", Name: "小星"}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{SessionID: "missed-session", UserID: "missed-user", CharacterID: "missed-char", LastMessageAt: &lastMessageAt}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	service := NewNotificationService(db)
	created, err := service.ScheduleMissedConversationCandidates(now, 10)
	if err != nil {
		t.Fatalf("schedule missed candidate: %v", err)
	}
	if created != 1 {
		t.Fatalf("expected one new candidate, got %d", created)
	}
	if _, err := service.ScheduleMissedConversationCandidates(now.Add(5*time.Minute), 10); err != nil {
		t.Fatalf("schedule duplicate candidate: %v", err)
	}
	var count int64
	if err := db.Model(&database.ProactiveEvent{}).Where("user_id = ? AND character_id = ?", "missed-user", "missed-char").Count(&count).Error; err != nil {
		t.Fatalf("count candidates: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one candidate for the day, got %d", count)
	}
}

func TestProcessDueProactiveEventsReclaimsStaleProcessingEvent(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.ProactiveEvent{}); err != nil {
		t.Fatalf("migrate proactive events: %v", err)
	}
	if err := db.Create(&database.User{UserID: "stale-user", Language: "zh-Hant"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{CharacterID: "stale-char", Name: "小星"}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	stale := time.Now().Add(-20 * time.Minute)
	event := database.ProactiveEvent{
		EventID: "stale-event", UserID: "stale-user", CharacterID: "stale-char",
		EventType: ProactiveEventMissedChat, TriggerAt: time.Now().Add(-time.Minute),
		DedupeKey: "stale-event-key", Status: ProactiveEventStatusProcessing, ProcessingAt: &stale,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("create stale event: %v", err)
	}
	service := NewNotificationService(db)
	service.APNs = &APNsClient{}
	if _, err := service.ProcessDueProactiveEvents(context.Background(), time.Now(), 10); err != nil {
		t.Fatalf("process stale event: %v", err)
	}
	var refreshed database.ProactiveEvent
	if err := db.First(&refreshed, event.ID).Error; err != nil {
		t.Fatalf("reload stale event: %v", err)
	}
	if refreshed.Status != ProactiveEventStatusPending || refreshed.ProcessingAt != nil {
		t.Fatalf("expected stale event to return to pending, got status=%s processing_at=%v", refreshed.Status, refreshed.ProcessingAt)
	}
}

type proactiveTestGenerator struct {
	content string
	called  int
}

func (g *proactiveTestGenerator) GenerateProactiveMessage(user database.User, character database.Character, session database.Session, event database.ProactiveEvent) (ProactiveMessage, error) {
	g.called++
	return ProactiveMessage{Content: g.content, ReplyEmotion: "neutral"}, nil
}

func TestProcessDueProactiveEventPersistsAssistantMessageBeforePush(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.ProactiveEvent{}, &database.Notification{}, &database.PushDevice{}, &database.UserCharacterNotificationSetting{}); err != nil {
		t.Fatalf("migrate proactive delivery tables: %v", err)
	}
	now := time.Date(2026, 8, 23, 14, 0, 0, 0, time.FixedZone("Asia/Taipei", 8*60*60))
	if err := db.Create(&database.User{UserID: "deliver-user", Language: "zh-Hant", Nickname: "小明"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.UserSettings{UserID: "deliver-user", PushEnabled: true, ProactiveEnable: true, ProactiveDailyLimit: 1, ProactiveOnlyWhenAway: true}).Error; err != nil {
		t.Fatalf("create settings: %v", err)
	}
	if err := db.Create(&database.Character{CharacterID: "deliver-char", Name: "小星", SystemPrompt: "溫柔陪伴"}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{SessionID: "deliver-session", UserID: "deliver-user", CharacterID: "deliver-char", LastMessageAt: ptrTime(now.Add(-72 * time.Hour))}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&database.PushDevice{UserID: "deliver-user", Token: "aabbcc", Platform: "ios", Environment: "sandbox", IsActive: true}).Error; err != nil {
		t.Fatalf("create device: %v", err)
	}
	event := database.ProactiveEvent{EventID: "deliver-event", UserID: "deliver-user", CharacterID: "deliver-char", EventType: ProactiveEventMissedChat, TriggerAt: now.Add(-time.Minute), DedupeKey: "deliver-key", Status: ProactiveEventStatusPending}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("create event: %v", err)
	}
	service := NewNotificationService(db)
	service.APNs = &APNsClient{privateKey: nil}
	generator := &proactiveTestGenerator{content: "今天過得還好嗎？有空再回我就好。"}
	service.ConfigureProactiveGenerator(generator)
	// APNs is deliberately unavailable, so generation and persistence can be
	// verified without making an external request.
	_, err := service.ProcessDueProactiveEvents(context.Background(), now, 10)
	if err != nil {
		t.Fatalf("process proactive event: %v", err)
	}
	if generator.called != 1 {
		t.Fatalf("expected generator once, got %d", generator.called)
	}
	var message database.Message
	if err := db.Where("user_id = ? AND character_id = ? AND role = ?", "deliver-user", "deliver-char", "assistant").First(&message).Error; err != nil {
		t.Fatalf("expected persisted proactive assistant message: %v", err)
	}
	if message.Content != generator.content || message.MessageID != "proactive-deliver-event" {
		t.Fatalf("unexpected proactive message: %+v", message)
	}
	var refreshed database.ProactiveEvent
	if err := db.First(&refreshed, event.ID).Error; err != nil {
		t.Fatalf("reload event: %v", err)
	}
	if refreshed.Status != ProactiveEventStatusPending || refreshed.AssistantMessageID != message.MessageID {
		t.Fatalf("expected generated event to remain retryable with message id, got status=%s message=%s", refreshed.Status, refreshed.AssistantMessageID)
	}
}

func TestCancelPendingProactiveEventsOnUserReply(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.ProactiveEvent{}); err != nil {
		t.Fatalf("migrate proactive events: %v", err)
	}
	for _, status := range []string{ProactiveEventStatusPending, ProactiveEventStatusProcessing, ProactiveEventStatusSent} {
		if err := db.Create(&database.ProactiveEvent{EventID: "cancel-" + status, UserID: "cancel-user", CharacterID: "cancel-char", EventType: ProactiveEventMissedChat, TriggerAt: time.Now(), DedupeKey: "cancel-key-" + status, Status: status}).Error; err != nil {
			t.Fatalf("create %s event: %v", status, err)
		}
	}
	service := NewNotificationService(db)
	if err := service.CancelPendingProactiveEvents("cancel-user", "cancel-char"); err != nil {
		t.Fatalf("cancel proactive events: %v", err)
	}
	var pending int64
	db.Model(&database.ProactiveEvent{}).Where("status IN ?", []string{ProactiveEventStatusPending, ProactiveEventStatusProcessing}).Count(&pending)
	if pending != 0 {
		t.Fatalf("expected pending events cancelled, got %d", pending)
	}
}
