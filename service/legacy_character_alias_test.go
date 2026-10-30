package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
)

func TestResolvedPublicIDKeepsLegacyChatAndMemoryOnOwnedPrivateRole(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	if err := db.Create(&database.Character{
		CharacterID: "legacy-public",
		UserID:      "system_admin",
		Name:        "Legacy",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "legacy-session",
		UserID:      "owner",
		CharacterID: "legacy-public",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "legacy-message",
		SessionID:   "legacy-session",
		CharacterID: "legacy-public",
		UserID:      "owner",
		Role:        "user",
		Content:     "before resolve",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatal(err)
	}

	resolved, err := (&CharacterService{DB: db}).ResolveChatCharacter("owner", "legacy-public", "en")
	if err != nil {
		t.Fatal(err)
	}
	privateID := resolved.CharacterID
	if privateID != "legacy-public_owner" {
		t.Fatalf("unexpected resolved role: %s", privateID)
	}

	chat := &ChatService{DB: db}
	if mapped, err := chat.ResolveChatCharacterID("owner", "legacy-public"); err != nil || mapped != privateID {
		t.Fatalf("legacy public ID did not map to private role: %q %v", mapped, err)
	}
	history, total, err := chat.GetHistory("owner", "legacy-public", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(history) != 1 || history[0].Content != "before resolve" {
		t.Fatalf("legacy history was not read from the private role: total=%d history=%+v", total, history)
	}

	_, err = chat.SendMessageWithIDsContext(
		context.Background(), "owner", "legacy-public", "after resolve", "text",
		"legacy-new-user", "legacy-new-reply",
	)
	if !errors.Is(err, ErrAIServiceUnavailable) {
		t.Fatalf("expected the saved user message followed by unavailable AI, got %v", err)
	}

	memory := &MemoryService{DB: db}
	added, err := memory.AddMemory(request.AddMemoryReq{
		UserID:      "owner",
		CharacterID: "legacy-public",
		Content:     "remember the same role",
		MemoryType:  MemoryTypeFact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.CharacterID != privateID {
		t.Fatalf("memory was written to %q instead of %q", added.CharacterID, privateID)
	}
	memories, memoryTotal, err := memory.GetMemoryList(request.GetMemoryListReq{
		UserID:      "owner",
		CharacterID: "legacy-public",
		Page:        1,
		PageSize:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if memoryTotal != 1 || len(memories) != 1 || memories[0].Content != "remember the same role" {
		t.Fatalf("memory read did not use the private scope: total=%d memories=%+v", memoryTotal, memories)
	}
	stats, err := memory.GetMemoryStats("owner", "legacy-public")
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalCount != 1 {
		t.Fatalf("memory stats did not use the private scope: %+v", stats)
	}

	var sessionCount int64
	if err := db.Model(&database.Session{}).Where("user_id = ?", "owner").Count(&sessionCount).Error; err != nil {
		t.Fatal(err)
	}
	if sessionCount != 1 {
		t.Fatalf("legacy public calls split into %d sessions", sessionCount)
	}
	var publicMessages int64
	if err := db.Model(&database.Message{}).
		Where("user_id = ? AND character_id = ?", "owner", "legacy-public").
		Count(&publicMessages).Error; err != nil {
		t.Fatal(err)
	}
	if publicMessages != 0 {
		t.Fatalf("legacy public calls wrote %d messages outside the private role", publicMessages)
	}
	var privateMessages int64
	if err := db.Model(&database.Message{}).
		Where("user_id = ? AND character_id = ?", "owner", privateID).
		Count(&privateMessages).Error; err != nil {
		t.Fatal(err)
	}
	if privateMessages != 2 {
		t.Fatalf("expected merged history plus one new message, got %d", privateMessages)
	}
}

func TestLegacyPublicAliasIsolatesUsersAndPreservesUnmigratedHistory(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	if err := db.Create(&database.Character{
		CharacterID: "shared-public",
		UserID:      "system_admin",
		Name:        "Shared",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	characters := &CharacterService{DB: db}
	owner, err := characters.ResolveChatCharacter("owner", "shared-public", "en")
	if err != nil {
		t.Fatal(err)
	}
	other, err := characters.ResolveChatCharacter("other", "shared-public", "en")
	if err != nil {
		t.Fatal(err)
	}
	if owner.CharacterID != "shared-public_owner" || other.CharacterID != "shared-public_other" {
		t.Fatalf("resolve did not create per-user copies: owner=%s other=%s", owner.CharacterID, other.CharacterID)
	}

	chat := &ChatService{DB: db}
	if mapped, err := chat.ResolveChatCharacterID("owner", "shared-public"); err != nil || mapped != owner.CharacterID {
		t.Fatalf("owner legacy alias mismatch: %q %v", mapped, err)
	}
	if mapped, err := chat.ResolveChatCharacterID("other", "shared-public"); err != nil || mapped != other.CharacterID {
		t.Fatalf("other legacy alias mismatch: %q %v", mapped, err)
	}
	if _, err := chat.ResolveChatCharacterID("other", owner.CharacterID); !errors.Is(err, ErrChatCharacterUnavailable) {
		t.Fatalf("other user accessed owner's private role: %v", err)
	}

	memory := &MemoryService{DB: db}
	if _, err := memory.AddMemory(request.AddMemoryReq{
		UserID:      "other",
		CharacterID: owner.CharacterID,
		Content:     "cross-user write",
		MemoryType:  MemoryTypeFact,
	}); !errors.Is(err, ErrCharacterUnavailable) {
		t.Fatalf("other user wrote owner's private memory scope: %v", err)
	}
	if _, _, err := memory.GetMemoryList(request.GetMemoryListReq{
		UserID:      "other",
		CharacterID: owner.CharacterID,
		Page:        1,
		PageSize:    20,
	}); !errors.Is(err, ErrCharacterUnavailable) {
		t.Fatalf("other user read owner's private memory scope: %v", err)
	}

	if _, err := memory.AddMemory(request.AddMemoryReq{
		UserID:      "owner",
		CharacterID: "shared-public",
		Content:     "owner memory",
		MemoryType:  MemoryTypeFact,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.AddMemory(request.AddMemoryReq{
		UserID:      "other",
		CharacterID: "shared-public",
		Content:     "other memory",
		MemoryType:  MemoryTypeFact,
	}); err != nil {
		t.Fatal(err)
	}
	ownerRows, _, err := memory.GetMemoryList(request.GetMemoryListReq{
		UserID:      "owner",
		CharacterID: "shared-public",
		Page:        1,
		PageSize:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerRows) != 1 || ownerRows[0].Content != "owner memory" || ownerRows[0].CharacterID != owner.CharacterID {
		t.Fatalf("owner memory crossed scopes: %+v", ownerRows)
	}

	if err := db.Create(&database.Character{
		CharacterID: "unmigrated-public",
		UserID:      "system_admin",
		Name:        "Unmigrated",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if mapped, err := chat.ResolveChatCharacterID("owner", "unmigrated-public"); err != nil || mapped != "unmigrated-public" {
		t.Fatalf("unmigrated public role was changed without /resolve: %q %v", mapped, err)
	}
	var privateCopies int64
	if err := db.Model(&database.Character{}).
		Where("user_id = ? AND is_public = ?", "owner", 2).
		Count(&privateCopies).Error; err != nil {
		t.Fatal(err)
	}
	if privateCopies != 1 {
		t.Fatalf("legacy chat created an implicit private copy; count=%d", privateCopies)
	}

	if err := db.Create(&database.Character{
		CharacterID: "historical-public",
		UserID:      "system_admin",
		Name:        "Historical",
		Gender:      "female",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "historical-session",
		UserID:      "owner",
		CharacterID: "historical-public",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "historical-message",
		SessionID:   "historical-session",
		CharacterID: "historical-public",
		UserID:      "owner",
		Role:        "user",
		Content:     "historical chat",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatal(err)
	}
	history, total, err := chat.GetHistory("owner", "historical-public", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(history) != 1 || history[0].Content != "historical chat" {
		t.Fatalf("delisted historical role lost its chat: total=%d history=%+v", total, history)
	}

	if err := db.AutoMigrate(&database.UserCharacter{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "explicitly-collected",
		UserID:      "system_admin",
		Name:        "Collected",
		IsPublic:    0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.UserCharacter{
		UserID:      "owner",
		CharacterID: "explicitly-collected",
		ObtainType:  "gacha",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if mapped, err := chat.ResolveChatCharacterID("owner", "explicitly-collected"); err != nil || mapped != "explicitly-collected" {
		t.Fatalf("explicitly collected role was not preserved: %q %v", mapped, err)
	}
}

func TestExistingPrivateCopyDoesNotHideUnmergedPublicHistory(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	if err := db.Create(&database.Character{
		CharacterID: "encounter-public",
		UserID:      "system_admin",
		Name:        "Encounter",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "encounter-public_owner",
		UserID:      "owner",
		Name:        "Encounter private",
		Gender:      "male",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "encounter-public-session",
		UserID:      "owner",
		CharacterID: "encounter-public",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "encounter-public-message",
		SessionID:   "encounter-public-session",
		CharacterID: "encounter-public",
		UserID:      "owner",
		Role:        "user",
		Content:     "public history before resolve",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Memory{
		MemoryID:    "encounter-public-memory",
		UserID:      "owner",
		CharacterID: "encounter-public",
		Content:     "public memory before resolve",
		MemoryType:  MemoryTypeFact,
		Source:      "manual",
	}).Error; err != nil {
		t.Fatal(err)
	}

	chat := &ChatService{DB: db}
	memory := &MemoryService{DB: db}
	if mapped, err := chat.ResolveChatCharacterID("owner", "encounter-public"); err != nil || mapped != "encounter-public" {
		t.Fatalf("private copy hid unmerged public state: %q %v", mapped, err)
	}
	publicHistory, total, err := chat.GetHistory("owner", "encounter-public", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(publicHistory) != 1 || publicHistory[0].Content != "public history before resolve" {
		t.Fatalf("public history was hidden before resolve: total=%d history=%+v", total, publicHistory)
	}
	publicMemories, memoryTotal, err := memory.GetMemoryList(request.GetMemoryListReq{
		UserID:      "owner",
		CharacterID: "encounter-public",
		Page:        1,
		PageSize:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if memoryTotal != 1 || len(publicMemories) != 1 || publicMemories[0].CharacterID != "encounter-public" {
		t.Fatalf("public memory was hidden before resolve: total=%d memories=%+v", memoryTotal, publicMemories)
	}
	added, err := memory.AddMemory(request.AddMemoryReq{
		UserID:      "owner",
		CharacterID: "encounter-public",
		Content:     "more public memory",
		MemoryType:  MemoryTypeFact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.CharacterID != "encounter-public" {
		t.Fatalf("memory write escaped the still-unmerged public scope: %+v", added)
	}

	if _, err := (&CharacterService{DB: db}).ResolveChatCharacter("owner", "encounter-public", "en"); err != nil {
		t.Fatal(err)
	}
	if mapped, err := chat.ResolveChatCharacterID("owner", "encounter-public"); err != nil || mapped != "encounter-public_owner" {
		t.Fatalf("resolved role did not start using the private copy: %q %v", mapped, err)
	}
	mergedHistory, total, err := chat.GetHistory("owner", "encounter-public", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(mergedHistory) != 1 || mergedHistory[0].Content != "public history before resolve" {
		t.Fatalf("resolved history lost legacy messages: total=%d history=%+v", total, mergedHistory)
	}
	mergedMemories, memoryTotal, err := memory.GetMemoryList(request.GetMemoryListReq{
		UserID:      "owner",
		CharacterID: "encounter-public",
		Page:        1,
		PageSize:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if memoryTotal != 2 || len(mergedMemories) != 2 {
		t.Fatalf("resolved memory lost legacy rows: total=%d memories=%+v", memoryTotal, mergedMemories)
	}
	for _, row := range mergedMemories {
		if row.CharacterID != "encounter-public_owner" {
			t.Fatalf("resolved memory kept a public scope: %+v", row)
		}
	}
	for _, model := range []interface{}{&database.Session{}, &database.Message{}, &database.Memory{}} {
		var count int64
		if err := db.Unscoped().Model(model).
			Where("user_id = ? AND character_id = ?", "owner", "encounter-public").
			Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("explicit resolve left public state in %T: %d", model, count)
		}
	}

	_, err = chat.SendMessageWithIDsContext(
		context.Background(), "owner", "encounter-public", "after explicit resolve", "text",
		"encounter-new-user", "encounter-new-reply",
	)
	if !errors.Is(err, ErrAIServiceUnavailable) {
		t.Fatalf("expected the post-resolve message to be stored before AI failure, got %v", err)
	}
	var postResolveMessage database.Message
	if err := db.Where("message_id = ?", "encounter-new-user").First(&postResolveMessage).Error; err != nil {
		t.Fatal(err)
	}
	if postResolveMessage.CharacterID != "encounter-public_owner" {
		t.Fatalf("post-resolve legacy send split back to public: %+v", postResolveMessage)
	}
}

func TestDeletedPublicHistoryDoesNotBlockPrivateCopy(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	if err := db.Create(&database.Character{
		CharacterID: "deleted-history-public",
		UserID:      "system_admin",
		Name:        "Deleted history",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "deleted-history-public_owner",
		UserID:      "owner",
		Name:        "Deleted history private",
		Gender:      "male",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "deleted-history-session",
		UserID:      "owner",
		CharacterID: "deleted-history-public",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "deleted-history-message",
		SessionID:   "deleted-history-session",
		CharacterID: "deleted-history-public",
		UserID:      "owner",
		Role:        "user",
		Content:     "deleted public history",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Memory{
		MemoryID:    "deleted-history-memory",
		UserID:      "owner",
		CharacterID: "deleted-history-public",
		Content:     "deleted public memory",
		MemoryType:  MemoryTypeFact,
		Source:      "manual",
	}).Error; err != nil {
		t.Fatal(err)
	}

	chat := &ChatService{DB: db}
	if err := chat.DeleteSession("deleted-history-session", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("memory_id = ?", "deleted-history-memory").Delete(&database.Memory{}).Error; err != nil {
		t.Fatal(err)
	}
	var deletedMessages int64
	if err := db.Unscoped().Model(&database.Message{}).
		Where("user_id = ? AND character_id = ?", "owner", "deleted-history-public").
		Count(&deletedMessages).Error; err != nil {
		t.Fatal(err)
	}
	if deletedMessages == 0 {
		t.Fatal("expected deleted session to leave soft-deleted messages behind")
	}

	if mapped, err := chat.ResolveChatCharacterID("owner", "deleted-history-public"); err != nil || mapped != "deleted-history-public_owner" {
		t.Fatalf("deleted public history blocked the private copy: %q %v", mapped, err)
	}
}

func TestResolvedPublicIDCreatesPrivateSessionOnFirstLegacyRequest(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	if err := db.Create(&database.Character{
		CharacterID: "no-session-public",
		UserID:      "system_admin",
		Name:        "No session",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	resolved, err := (&CharacterService{DB: db}).ResolveChatCharacter("owner", "no-session-public", "en")
	if err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.Model(&database.Session{}).Where("user_id = ?", "owner").Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatalf("test setup unexpectedly created %d sessions", before)
	}

	chat := &ChatService{DB: db}
	_, err = chat.SendMessageWithIDsContext(
		context.Background(), "owner", "no-session-public", "first legacy request", "text",
		"no-session-user", "no-session-reply",
	)
	if !errors.Is(err, ErrAIServiceUnavailable) {
		t.Fatalf("expected the user message to be saved before AI failure, got %v", err)
	}
	var session database.Session
	if err := db.Where("user_id = ?", "owner").First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.CharacterID != resolved.CharacterID {
		t.Fatalf("first legacy request created a split session: %+v", session)
	}
	var message database.Message
	if err := db.Where("message_id = ?", "no-session-user").First(&message).Error; err != nil {
		t.Fatal(err)
	}
	if message.CharacterID != resolved.CharacterID || message.SessionID != session.SessionID {
		t.Fatalf("first legacy message did not use the private session: %+v", message)
	}
}

func TestLegacyPublicQuotaRetryUsesPrivateMessageIDs(t *testing.T) {
	db := setupChatQuotaTestDB(t)
	createQuotaTestUser(t, db, "owner", "owner-quota@example.test", "free", nil)
	if err := db.Create(&database.Character{
		CharacterID: "quota-public",
		UserID:      "system_admin",
		Name:        "Quota",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "quota-public_owner",
		UserID:      "owner",
		Name:        "Quota private",
		Gender:      "male",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "quota-private-session",
		UserID:      "owner",
		CharacterID: "quota-public_owner",
	}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, message := range []database.Message{
		{
			MessageID:   "quota-existing-user",
			SessionID:   "quota-private-session",
			CharacterID: "quota-public_owner",
			UserID:      "owner",
			Role:        "user",
			Content:     "already sent",
			MessageType: "text",
			CreatedAt:   now,
		},
		{
			MessageID:   "quota-existing-reply",
			SessionID:   "quota-private-session",
			CharacterID: "quota-public_owner",
			UserID:      "owner",
			Role:        "assistant",
			Content:     "already answered",
			MessageType: "text",
			CreatedAt:   now,
		},
	} {
		if err := db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
	}

	chat := quotaServiceWithLimit(t, db, 1)
	if err := chat.CheckNewUserMessageQuota("owner", "quota-public", "quota-existing-user", "quota-existing-reply"); err != nil {
		t.Fatalf("legacy retry was charged against quota: %v", err)
	}
	events, errs := chat.StreamMessage(
		"owner", "quota-public", "already sent", "text",
		"quota-existing-user", "quota-existing-reply",
	)
	meta := <-events
	content := <-events
	if meta.Type != "meta" || content.Type != "content" || content.Content != "already answered" {
		t.Fatalf("legacy retry did not replay the private reply: meta=%+v content=%+v", meta, content)
	}
	if err := <-errs; err != nil {
		t.Fatalf("legacy retry attempted a new generation: %v", err)
	}
	var count int64
	if err := db.Model(&database.Message{}).Where("user_id = ?", "owner").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("legacy retry duplicated messages: %d", count)
	}
	if err := chat.CheckNewUserMessageQuota("owner", "quota-public", "quota-new-user", "quota-new-reply"); err == nil {
		t.Fatal("a genuinely new legacy message bypassed the exhausted quota")
	}
}
