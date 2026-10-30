package service

import (
	"errors"
	"fmt"
	"testing"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupReportServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.User{}, &database.Character{}, &database.Session{}, &database.Message{},
		&database.Block{}, &database.Report{},
	); err != nil {
		t.Fatalf("migrate report tables: %v", err)
	}
	return db
}

func TestBlockCharacterCoversPublicCharacterAndEncounteredCopy(t *testing.T) {
	db := setupReportServiceTestDB(t)
	userID := "block-user-123456"
	publicID := "block-public-character"
	copyID := publicID + "_" + shortUserID(userID)
	for _, character := range []database.Character{
		{CharacterID: publicID, UserID: "system_admin", Name: "公共角色", IsPublic: 1},
		{CharacterID: copyID, UserID: userID, Name: "已邂逅角色", IsPublic: 2},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %s: %v", character.CharacterID, err)
		}
	}

	service := NewReportService(db)
	if err := service.BlockUser(userID, copyID); err != nil {
		t.Fatalf("block encountered copy: %v", err)
	}
	for _, id := range []string{publicID, copyID} {
		blocked, err := service.IsBlocked(userID, id)
		if err != nil || !blocked {
			t.Fatalf("expected %s to be blocked, blocked=%v err=%v", id, blocked, err)
		}
	}

	if err := service.UnblockUser(userID, publicID); err != nil {
		t.Fatalf("unblock public character: %v", err)
	}
	var count int64
	if err := db.Model(&database.Block{}).Where("blocker_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected public character and copy to unblock together, found %d blocks", count)
	}
}

func TestBlockedCharacterIsHiddenAndCannotBeOpenedOrChattedWith(t *testing.T) {
	db := setupReportServiceTestDB(t)
	userID := "blocked-access-user"
	blockedID := "blocked-character"
	visibleID := "visible-character"
	if err := db.Create(&database.User{UserID: userID, Gender: "unknown"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, character := range []database.Character{
		{CharacterID: blockedID, UserID: "system_admin", Name: "屏蔽角色", Gender: "male", Relationship: "伙伴", IsPublic: 1, ChatCount: 100},
		{CharacterID: visibleID, UserID: "system_admin", Name: "可见角色", Gender: "male", Relationship: "伙伴", IsPublic: 1, ChatCount: 10},
	} {
		if err := db.Create(&character).Error; err != nil {
			t.Fatalf("create character %s: %v", character.CharacterID, err)
		}
	}
	for _, session := range []database.Session{
		{SessionID: "blocked-session", UserID: userID, CharacterID: blockedID},
		{SessionID: "visible-session", UserID: userID, CharacterID: visibleID},
	} {
		if err := db.Create(&session).Error; err != nil {
			t.Fatalf("create session %s: %v", session.SessionID, err)
		}
	}
	if err := db.Create(&database.Message{
		MessageID: "blocked-message", SessionID: "blocked-session", UserID: userID,
		CharacterID: blockedID, Role: "assistant", Content: "hidden",
	}).Error; err != nil {
		t.Fatalf("create blocked message: %v", err)
	}
	if err := NewReportService(db).BlockUser(userID, blockedID); err != nil {
		t.Fatalf("block character: %v", err)
	}

	characterService := &CharacterService{DB: db}
	publicCharacters, total, err := characterService.GetPublicList(request.GetPublicCharactersReq{
		UserID: userID, Page: 1, PageSize: 20,
	}, "zh-Hans")
	if err != nil {
		t.Fatalf("get public list: %v", err)
	}
	if total != 1 || len(publicCharacters) != 1 || publicCharacters[0].CharacterID != visibleID {
		t.Fatalf("blocked character leaked into public list: total=%d list=%+v", total, publicCharacters)
	}
	if _, err := characterService.GetCharacterDetail(userID, blockedID, "zh-Hans"); !errors.Is(err, ErrCharacterBlocked) {
		t.Fatalf("expected blocked detail to fail, got %v", err)
	}
	if err := characterService.EncounterCharacter(userID, blockedID, "zh-Hans"); !errors.Is(err, ErrCharacterBlocked) {
		t.Fatalf("expected blocked encounter to fail, got %v", err)
	}

	chatService := &ChatService{DB: db}
	sessions, sessionTotal, err := chatService.GetSessions(userID, 1, 20, "zh-Hans")
	if err != nil {
		t.Fatalf("get sessions: %v", err)
	}
	if sessionTotal != 1 || len(sessions) != 1 || sessions[0].CharacterInfo.CharacterID != visibleID {
		t.Fatalf("blocked session leaked: total=%d sessions=%+v", sessionTotal, sessions)
	}
	if _, _, err := chatService.GetHistory(userID, blockedID, 1, 20); !errors.Is(err, ErrChatCharacterBlocked) {
		t.Fatalf("expected blocked history to fail, got %v", err)
	}
	if _, err := chatService.SendMessage(userID, blockedID, "hello", "text"); !errors.Is(err, ErrChatCharacterBlocked) {
		t.Fatalf("expected blocked send to fail, got %v", err)
	}
	_, streamErrors := chatService.StreamMessage(userID, blockedID, "hello", "text", "", "", "")
	if err := <-streamErrors; !errors.Is(err, ErrChatCharacterBlocked) {
		t.Fatalf("expected blocked stream to fail, got %v", err)
	}

	mobileCards, err := (&MobileService{DB: db}).loadRecommendedCharacters(userID, 8, "zh-Hans")
	if err != nil {
		t.Fatalf("load mobile recommendations: %v", err)
	}
	if len(mobileCards) != 1 || mobileCards[0].CharacterID != visibleID {
		t.Fatalf("blocked character leaked into mobile recommendations: %+v", mobileCards)
	}
}

func TestCreateReportRejectsUnknownReasonAndMissingTarget(t *testing.T) {
	db := setupReportServiceTestDB(t)
	service := NewReportService(db)

	if err := service.CreateReport("report-user", request.ReportReq{
		TargetType: "character", TargetID: "missing", Reason: "spam",
	}); !errors.Is(err, ErrReportTargetUnavailable) {
		t.Fatalf("expected missing target rejection, got %v", err)
	}
	if err := service.CreateReport("report-user", request.ReportReq{
		TargetType: "character", TargetID: "missing", Reason: "invented",
	}); !errors.Is(err, ErrInvalidReportReason) {
		t.Fatalf("expected invalid reason rejection, got %v", err)
	}

	var count int64
	if err := db.Model(&database.Report{}).Count(&count).Error; err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected invalid reports not to persist, found %d", count)
	}
}
