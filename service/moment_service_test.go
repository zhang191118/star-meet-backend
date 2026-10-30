package service

import (
	"fmt"
	"testing"
	"time"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMomentServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Character{}, &database.CharacterMoment{}); err != nil {
		t.Fatalf("migrate moment tables: %v", err)
	}
	return db
}

func TestMomentFeedOnlyReturnsPublicCharactersAndNormalStatus(t *testing.T) {
	db := setupMomentServiceTestDB(t)

	chars := []database.Character{
		{CharacterID: "char-public", UserID: "system", Name: "公开角色", Avatar: "https://img/a.png", Gender: "male", IsPublic: 1},
		{CharacterID: "char-public-female", UserID: "system", Name: "公开女角色", Avatar: "https://img/c.png", Gender: "female", IsPublic: 1},
		{CharacterID: "char-private", UserID: "system", Name: "私有角色", Avatar: "https://img/b.png", Gender: "female", IsPublic: 0},
	}
	for _, ch := range chars {
		if err := db.Create(&ch).Error; err != nil {
			t.Fatalf("create character %s: %v", ch.CharacterID, err)
		}
	}

	now := time.Now()
	moments := []database.CharacterMoment{
		{CharacterID: "char-public", Content: "公开可见动态", ImageURLs: `["https://oss/x.jpg"]`, TopicTags: "日常,咖啡", Status: 1, PublishDay: "2026-08-18", PublishedAt: now},
		{CharacterID: "char-public", Content: "已下架动态", Status: 0, PublishDay: "2026-08-17", PublishedAt: now.Add(-time.Hour)},
		{CharacterID: "char-private", Content: "私有角色动态", Status: 1, PublishDay: "2026-08-18", PublishedAt: now},
		{CharacterID: "char-public-female", Content: "公开女角色动态", Status: 1, PublishDay: "2026-08-18", PublishedAt: now},
	}
	for _, m := range moments {
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("create moment: %v", err)
		}
	}

	svc := &MomentService{DB: db}
	items, total, err := svc.GetFeed(request.GetMomentFeedReq{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("get feed: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 visible moment, got total=%d len=%d", total, len(items))
	}
	item := items[0]
	if item.CharacterID != "char-public" || item.Name != "公开角色" || item.Avatar == "" {
		t.Fatalf("unexpected feed item: %#v", item)
	}
	if len(item.ImageURLs) != 1 || item.ImageURLs[0] != "https://oss/x.jpg" {
		t.Fatalf("unexpected image urls: %#v", item.ImageURLs)
	}
	if len(item.TopicTags) != 2 || item.TopicTags[0] != "日常" {
		t.Fatalf("unexpected topic tags: %#v", item.TopicTags)
	}
}

func TestMomentFeedPagination(t *testing.T) {
	db := setupMomentServiceTestDB(t)

	if err := db.Create(&database.Character{CharacterID: "char-paged", UserID: "system", Name: "分页角色", Gender: "male", IsPublic: 1}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	base := time.Now()
	for i := 0; i < 25; i++ {
		m := database.CharacterMoment{
			CharacterID: "char-paged",
			Content:     fmt.Sprintf("moment %d", i),
			Status:      1,
			PublishDay:  fmt.Sprintf("2026-07-%02d", i+1),
			PublishedAt: base.Add(time.Duration(i) * time.Hour),
		}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("create moment %d: %v", i, err)
		}
	}

	svc := &MomentService{DB: db}
	page1, total, err := svc.GetFeed(request.GetMomentFeedReq{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("get feed page1: %v", err)
	}
	if total != 25 || len(page1) != 20 {
		t.Fatalf("expected total=25 len=20, got total=%d len=%d", total, len(page1))
	}
	if page1[0].Content != "moment 24" {
		t.Fatalf("expected newest first, got %s", page1[0].Content)
	}
	page2, _, err := svc.GetFeed(request.GetMomentFeedReq{Page: 2, PageSize: 20})
	if err != nil {
		t.Fatalf("get feed page2: %v", err)
	}
	if len(page2) != 5 {
		t.Fatalf("expected 5 items on page2, got %d", len(page2))
	}
}

func TestCharacterMomentFeedOnlyReturnsRequestedPublicCharacter(t *testing.T) {
	db := setupMomentServiceTestDB(t)
	chars := []database.Character{
		{CharacterID: "char-one", UserID: "system", Name: "角色一", Gender: "male", IsPublic: 1},
		{CharacterID: "char-two", UserID: "system", Name: "角色二", Gender: "male", IsPublic: 1},
	}
	for _, ch := range chars {
		if err := db.Create(&ch).Error; err != nil {
			t.Fatalf("create character: %v", err)
		}
	}
	for i, characterID := range []string{"char-one", "char-two"} {
		if err := db.Create(&database.CharacterMoment{
			CharacterID: characterID,
			Content:     characterID + " 动态",
			Status:      1,
			PublishDay:  fmt.Sprintf("2026-08-%02d", i+1),
			PublishedAt: time.Now().Add(time.Duration(i) * time.Hour),
		}).Error; err != nil {
			t.Fatalf("create moment: %v", err)
		}
	}

	items, total, err := (&MomentService{DB: db}).GetCharacterFeed("char-two", request.GetMomentFeedReq{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("get character feed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].CharacterID != "char-two" {
		t.Fatalf("expected only char-two feed, got total=%d items=%#v", total, items)
	}
}
