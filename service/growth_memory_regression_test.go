package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"star-meet-backend/core/embedding"
	"star-meet-backend/core/qdrant"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
)

func setupGrowthRegressionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.UserSettings{}, &database.Character{}, &database.Block{}, &database.Session{}, &database.Message{}, &database.Memory{}, &database.MemoryIndexJob{}, &database.UserAffection{}, &database.UserCharacterNotificationSetting{}, &database.ProactiveEvent{}, &database.Notification{}, &database.Feedback{}, &database.ProductEvent{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"owner", "other"} {
		if err := db.Create(&database.User{UserID: id, Email: id + "@example.test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func testEmbeddingClient() *embedding.Client {
	client := embedding.NewClient("test-key")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"embedding":[0.1,0.2]}]}`)), Header: make(http.Header)}, nil
	})}
	return client
}
func TestCanonicalMemoryDoesNotServeStaleForeignOrDeletedIndexText(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	var id atomic.Value
	id.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/search") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "result": []map[string]interface{}{
				{"id": "stale", "score": 1, "payload": map[string]interface{}{"memory_id": id.Load(), "content": "stale poison"}},
				{"id": "foreign", "score": 1, "payload": map[string]interface{}{"memory_id": "foreign-memory", "content": "foreign secret"}},
				{"id": "unlinked", "score": 1, "payload": map[string]interface{}{"memory_id": "missing", "content": "unlinked secret"}},
			}})
		} else {
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	svc := &MemoryService{DB: db, QdrantClient: qdrant.NewClient(server.URL), EmbeddingClient: testEmbeddingClient()}
	row, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "old preference", MemoryType: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	id.Store(row.MemoryID)
	if err := db.Create(&database.Memory{MemoryID: "foreign-memory", UserID: "other", CharacterID: "char", Content: "foreign secret"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateMemory(id.Load().(string), "owner", "current preference"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.Retrieve(context.Background(), "owner", "char", "preference", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Content != "current preference" {
		t.Fatalf("must serve current scoped SQL content, got %+v", rows)
	}
	if err := svc.DeleteMemory(id.Load().(string), "owner"); err != nil {
		t.Fatal(err)
	}
	rows, err = svc.Retrieve(context.Background(), "owner", "char", "preference", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("deleted or foreign vectors must not resurrect text: %+v", rows)
	}
}
func TestMemoryIndexFailureRetriesAndDeletedTextIsPurgedAfterSync(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	svc := &MemoryService{DB: db, QdrantClient: qdrant.NewClient(server.URL)}
	row, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "delete me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteMemory(row.MemoryID, "owner"); err != nil {
		t.Fatal(err)
	}
	processed, err := svc.ProcessNextIndexJob(context.Background())
	if !processed || err == nil {
		t.Fatalf("expected a retriable provider failure, %v %v", processed, err)
	}
	var job database.MemoryIndexJob
	if err := db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Attempts != 1 || job.LastError != "index_sync_failed" || job.LeaseUntil != nil {
		t.Fatalf("bad retry state %+v", job)
	}
	fail.Store(false)
	if err := db.Model(&job).Update("next_attempt_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProcessNextIndexJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&database.MemoryIndexJob{}).Count(&count)
	if count != 0 {
		t.Fatal("completed job remains")
	}
	var stored database.Memory
	if err := db.Unscoped().Where("memory_id = ?", row.MemoryID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.DeletedAt.Valid || stored.Content != "" || stored.IndexedRevision != stored.Revision {
		t.Fatalf("deleted text retained after sync: %+v", stored)
	}
}
func TestMemoryIndexUsesStableIDAndDoesNotCopyTextIntoVectorPayload(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	var payload struct {
		Points []qdrant.Point `json:"points"`
	}
	var payloadMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			payloadMu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&payload)
			payloadMu.Unlock()
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	svc := &MemoryService{DB: db, QdrantClient: qdrant.NewClient(server.URL), EmbeddingClient: testEmbeddingClient()}
	row, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "important preference"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProcessNextIndexJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	payloadMu.Lock()
	defer payloadMu.Unlock()
	if len(payload.Points) != 1 || payload.Points[0].Payload["memory_id"] != row.MemoryID || payload.Points[0].Payload["content"] != nil {
		t.Fatalf("noncanonical payload %+v", payload)
	}
	var stored database.Memory
	db.Where("memory_id = ?", row.MemoryID).First(&stored)
	if stored.IndexedRevision != stored.Revision {
		t.Fatal("sync revision was not acknowledged")
	}
}
func TestMemoryWriteCannotRecreateDeletedAccountData(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	if err := db.Where("user_id = ?", "owner").Delete(&database.User{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := (&MemoryService{DB: db}).AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "late async result"})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("late extraction must not recreate deleted data: %v", err)
	}
}
func TestResolveChatIsIdempotentAndMergesOnlyCurrentUsersLegacyConversation(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	if err := db.Create(&database.Character{CharacterID: "public", UserID: "system_admin", Name: "Public", Gender: "male", IsPublic: 1}).Error; err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"owner", "other"} {
		if err := db.Create(&database.Session{SessionID: u + "-legacy", UserID: u, CharacterID: "public", UnreadCount: 2, IsPinned: true}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&database.Message{MessageID: u + "-msg", SessionID: u + "-legacy", UserID: u, CharacterID: "public", Role: "user", Content: u + " private"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&database.Memory{MemoryID: u + "-memory", UserID: u, CharacterID: "public", Content: u + " memory"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &CharacterService{DB: db}
	first, err := svc.ResolveChatCharacter("owner", "public", "en")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.ResolveChatCharacter("owner", first.CharacterID, "en")
	if err != nil {
		t.Fatal(err)
	}
	if first.CharacterID != "public_owner" || again.CharacterID != first.CharacterID {
		t.Fatalf("not idempotent: %+v %+v", first, again)
	}
	var message database.Message
	db.Where("message_id = ?", "owner-msg").First(&message)
	if message.CharacterID != first.CharacterID || message.SessionID != "owner-legacy" {
		t.Fatalf("history split: %+v", message)
	}
	var other database.Message
	db.Where("message_id = ?", "other-msg").First(&other)
	if other.CharacterID != "public" || other.Content != "other private" {
		t.Fatalf("another user was changed: %+v", other)
	}
	var memories []database.Memory
	db.Where("user_id = ?", "owner").Find(&memories)
	if len(memories) != 1 || memories[0].CharacterID != first.CharacterID {
		t.Fatalf("memory scope not migrated %+v", memories)
	}
	if _, err := svc.ResolveChatCharacter("other", first.CharacterID, "en"); err == nil {
		t.Fatal("foreign private character was exposed")
	}
}
func TestResolveCombinesExistingSessionsWithoutDuplicateMessagesOrAffection(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	db.Create(&database.Character{CharacterID: "public", UserID: "system_admin", Name: "Public", Gender: "male", IsPublic: 1})
	db.Create(&database.Character{CharacterID: "public_owner", UserID: "owner", Name: "Owned", Gender: "male", IsPublic: 2})
	for _, cid := range []string{"public", "public_owner"} {
		db.Create(&database.Session{SessionID: cid + "-session", UserID: "owner", CharacterID: cid, UnreadCount: 1})
		db.Create(&database.Message{MessageID: cid + "-msg", SessionID: cid + "-session", UserID: "owner", CharacterID: cid, Role: "user", Content: "hello"})
		db.Create(&database.UserAffection{UserID: "owner", CharacterID: cid, Exp: 100, Level: 2})
	}
	svc := &CharacterService{DB: db}
	for i := 0; i < 2; i++ {
		if _, err := svc.ResolveChatCharacter("owner", "public", "en"); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	db.Model(&database.Session{}).Where("user_id = ?", "owner").Count(&count)
	if count != 1 {
		t.Fatalf("sessions=%d", count)
	}
	db.Model(&database.Message{}).Where("user_id = ? AND character_id = ?", "owner", "public_owner").Count(&count)
	if count != 2 {
		t.Fatalf("messages=%d", count)
	}
	var affection database.UserAffection
	db.Where("user_id = ? AND character_id = ?", "owner", "public_owner").First(&affection)
	if affection.Exp != 200 {
		t.Fatalf("duplicate migration inflated/lost affection: %+v", affection)
	}
}
func TestTraditionalChineseTriggersStructuredMemory(t *testing.T) {
	if !shouldExtractStructuredMemory("請記住我喜歡無糖紅茶", "好，我會記得。") {
		t.Fatal("traditional Chinese preference was missed")
	}
	if shouldExtractStructuredMemory("嗯嗯", "好呀") {
		t.Fatal("small talk triggered extraction")
	}
}
func TestProductEventsRejectForgedServerFactsAndDeduplicateClientRetries(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	svc := &AnalyticsService{DB: db}
	now := time.Now()
	event := ClientEvent{ID: uuid.NewString(), Name: "first_open", InstallationID: uuid.NewString(), OccurredAt: now.Unix(), Properties: map[string]string{"platform": "ios"}}
	if err := svc.Ingest("", []ClientEvent{event}, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.Ingest("", []ClientEvent{event}, now); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&database.ProductEvent{}).Count(&count)
	if count != 1 {
		t.Fatalf("duplicate retry stored %d events", count)
	}
	event.Name = "ai_reply_completed"
	if err := svc.Ingest("owner", []ClientEvent{event}, now); !errors.Is(err, ErrInvalidGrowthInput) {
		t.Fatal("client forged a server-confirmed reply")
	}
	event.Name = "chat_opened"
	event.Properties["chat_text"] = "private"
	if err := svc.Ingest("owner", []ClientEvent{event}, now); !errors.Is(err, ErrInvalidGrowthInput) {
		t.Fatal("free text property accepted")
	}
}
func TestGrowthCohortsUseServerRepliesAndKeepImmatureDaysUnknown(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	svc := &AnalyticsService{DB: db}
	loc := chatQuotaLocation("Asia/Taipei")
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, loc)
	first := now.AddDate(0, 0, -2)
	event := ClientEvent{ID: uuid.NewString(), Name: "first_open", InstallationID: uuid.NewString(), OccurredAt: first.Unix()}
	if err := svc.Ingest("owner", []ClientEvent{event}, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := svc.RecordTurn("ai_reply_completed", "owner", "char", string(rune('a'+i)), first); err != nil {
			t.Fatal(err)
		}
	}
	_ = svc.RecordTurn("ai_reply_completed", "owner", "char", "a", first) // retry of the same turn
	_ = svc.RecordTurn("ai_reply_completed", "owner", "char", "next-day", first.AddDate(0, 0, 1))
	summary, err := svc.Summary(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Days) != 1 || summary.Days[0].Activated != 1 || summary.Days[0].Day1 == nil || *summary.Days[0].Day1 != 1 || summary.Days[0].Day7 != nil {
		t.Fatalf("bad cohort %+v", summary)
	}
}

func TestMemoryMutationDoesNotWaitForExternalIndexAndNewRevisionIsRetried(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var blockOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		block := false
		if r.Method == http.MethodPut {
			blockOnce.Do(func() { block = true; close(entered) })
		}
		if block {
			<-release
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	svc := &MemoryService{DB: db, QdrantClient: qdrant.NewClient(server.URL), EmbeddingClient: testEmbeddingClient()}
	row, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "before edit"})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := svc.ProcessNextIndexJob(context.Background()); finished <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("index request did not start")
	}
	saved := make(chan error, 1)
	go func() { saved <- svc.UpdateMemory(row.MemoryID, "owner", "after edit") }()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		unblock()
		t.Fatal("editing waited on the external provider")
	}
	unblock()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var job database.MemoryIndexJob
	if err := db.First(&job).Error; err != nil {
		t.Fatal("new revision lost from outbox", err)
	}
	if job.Revision != 2 || job.LeaseUntil != nil {
		t.Fatalf("new revision was not released for retry: %+v", job)
	}
	if _, err := svc.ProcessNextIndexJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	var stored database.Memory
	db.Where("memory_id = ?", row.MemoryID).First(&stored)
	if stored.Revision != 2 || stored.IndexedRevision != 2 || stored.Content != "after edit" {
		t.Fatalf("bad final state %+v", stored)
	}
}
func TestSlowSemanticLookupIsCancelledAndKeepsSQLFallback(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	emb := embedding.NewClient("test")
	emb.Client = &http.Client{Transport: chatRoundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	svc := &MemoryService{DB: db, EmbeddingClient: emb, QdrantClient: qdrant.NewClient("http://unused.invalid")}
	if _, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "a saved preference"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	rows, err := svc.Retrieve(ctx, "owner", "char", "preference", 5)
	if err != nil || len(rows) != 1 || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("slow lookup blocked or lost fallback: rows=%+v err=%v", rows, err)
	}
}
func TestGrowthDoesNotTreatReinstallOrTaskCohortAsNewNaturalUsers(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	svc := &AnalyticsService{DB: db}
	now := time.Now()
	db.Create(&database.ProductEvent{ID: "historical-first", Name: "first_open", UserID: "owner", OccurredAt: now.AddDate(0, 0, -40), Properties: "{}"})
	if err := svc.Ingest("owner", []ClientEvent{{ID: uuid.NewString(), Name: "first_open", InstallationID: uuid.NewString(), OccurredAt: now.Unix()}}, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.Ingest("other", []ClientEvent{{ID: uuid.NewString(), Name: "first_open", InstallationID: uuid.NewString(), OccurredAt: now.Unix(), Properties: map[string]string{"cohort": "task"}}}, now); err != nil {
		t.Fatal(err)
	}
	summary, err := svc.Summary(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Days) != 0 {
		t.Fatalf("reinstall/task inflated natural cohort: %+v", summary)
	}
}

func TestLateAutomaticExtractionCannotUndoMemoryCorrectionOrDeletion(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	svc := &MemoryService{DB: db}
	row, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "old preference"})
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Now().Add(-time.Second)
	if err := svc.UpdateMemory(row.MemoryID, "owner", "correct preference"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "old preference", Source: "auto", ObservedAt: observed}); !errors.Is(err, ErrMemorySuperseded) {
		t.Fatalf("late extraction overrode correction: %v", err)
	}
	if err := svc.DeleteMemory(row.MemoryID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMemory(request.AddMemoryReq{UserID: "owner", CharacterID: "char", Content: "old preference", Source: "auto", ObservedAt: observed}); !errors.Is(err, ErrMemorySuperseded) {
		t.Fatalf("late extraction resurrected deletion: %v", err)
	}
	var count int64
	db.Model(&database.Memory{}).Where("user_id = ?", "owner").Count(&count)
	if count != 0 {
		t.Fatalf("deleted fact was resurrected: %d", count)
	}
	if shouldExtractStructuredMemory("好的知道了", "我記得你喜歡紅茶") {
		t.Fatal("assistant claims must not become user memory signals")
	}
}

func TestMemoryBackfillQueuesLegacySQLAndDiscardsObsoleteJobs(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	svc := &MemoryService{DB: db, QdrantClient: qdrant.NewClient("http://unused.invalid")}
	row := database.Memory{MemoryID: "legacy-canonical", UserID: "owner", CharacterID: "char", Content: "legacy preference", Revision: 1}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.BackfillIndexJobs(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	var job database.MemoryIndexJob
	if err := db.First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.MemoryID != row.MemoryID {
		t.Fatalf("wrong backfill job %+v", job)
	}
	// A faster live update and sync has already completed before this old backfill job is processed.
	db.Model(&row).Updates(map[string]interface{}{"revision": 2, "indexed_revision": 2})
	if _, err := svc.ProcessNextIndexJob(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&database.MemoryIndexJob{}).Count(&count)
	if count != 0 {
		t.Fatal("obsolete job would loop forever")
	}
}

func TestChatRejectsOtherUsersPrivateCharacterAndKeepsHistoricalPublicSessions(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	db.Create(&database.Character{CharacterID: "private-owner", UserID: "owner", Name: "Private", IsPublic: 2})
	chat := &ChatService{DB: db}
	if err := chat.ensureChatCharacterAllowed("other", "private-owner"); !errors.Is(err, ErrChatCharacterUnavailable) {
		t.Fatalf("foreign private character allowed: %v", err)
	}
	if err := chat.ensureChatCharacterAllowed("owner", "private-owner"); err != nil {
		t.Fatal(err)
	}
	db.Create(&database.Character{CharacterID: "historical-public", UserID: "system_admin", Name: "Historical", Gender: "female", IsPublic: 1})
	db.Create(&database.Session{SessionID: "historical-session", UserID: "owner", CharacterID: "historical-public"})
	resolved, err := (&CharacterService{DB: db}).ResolveChatCharacter("owner", "historical-public", "en")
	if err != nil || resolved.CharacterID != "historical-public" {
		t.Fatalf("historical session disappeared: %+v %v", resolved, err)
	}
	if _, err := (&CharacterService{DB: db}).ResolveChatCharacter("other", "historical-public", "en"); err == nil {
		t.Fatal("new user accessed a delisted role without history")
	}
}
