package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openai "star-meet-backend/core/openai_compat"
	"star-meet-backend/model/database"
)

func collectReleaseStream(t *testing.T, events <-chan StreamEvent, errs <-chan error) ([]StreamEvent, error) {
	t.Helper()
	var result []StreamEvent
	var failure error
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for events != nil || errs != nil {
		select {
		case e, ok := <-events:
			if !ok {
				events = nil
			} else {
				result = append(result, e)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
			} else if err != nil {
				failure = err
			}
		case <-timer.C:
			t.Fatal("stream did not release its channels")
		}
	}
	return result, failure
}

func TestInterruptedStreamDoesNotCommitPartialReply(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	db.Create(&database.Character{CharacterID: "release-role", UserID: "owner", Name: "角色"})
	c := openai.NewClient("https://model.example.test", "test", "test")
	c.Client = &http.Client{Transport: chatRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"未完成的片段\"}}]}\n\n"))}, nil
	})}
	svc := &ChatService{DB: db, OpenAICompatClient: c, Analytics: &AnalyticsService{DB: db}}
	events, errs := svc.StreamMessage("owner", "release-role", "你好", "text", "release-user", "release-reply")
	got, err := collectReleaseStream(t, events, errs)
	if !errors.Is(err, ErrChatReplyInterrupted) {
		t.Fatalf("interrupted reply was accepted: %v", err)
	}
	if len(got) != 2 || got[1].Content != "未完成的片段" {
		t.Fatalf("visible fragment was lost: %+v", got)
	}
	var count int64
	db.Model(&database.Message{}).Where("message_id = ?", "release-reply").Count(&count)
	if count != 0 {
		t.Fatal("partial reply was committed as full history")
	}
	db.Model(&database.ProductEvent{}).Where("name = ?", "ai_reply_completed").Count(&count)
	if count != 0 {
		t.Fatal("interrupted reply counted as completed")
	}
	db.Model(&database.Memory{}).Count(&count)
	if count != 0 {
		t.Fatal("partial reply triggered automatic memory")
	}
}

func TestStreamCancellationReleasesUpstreamRequest(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	db.Create(&database.Character{CharacterID: "cancel-role", UserID: "owner", Name: "角色"})
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	c := openai.NewClient(server.URL, "test", "test")
	svc := &ChatService{DB: db, OpenAICompatClient: c}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, errs := svc.StreamMessageContext(ctx, "owner", "cancel-role", "你好", "text", "cancel-user", "cancel-reply")
	if first := <-events; first.Type != "meta" {
		t.Fatalf("missing acknowledgement: %+v", first)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model request did not start")
	}
	cancel()
	collectReleaseStream(t, events, errs)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("canceled client kept upstream HTTP alive")
	}
	var count int64
	db.Model(&database.Message{}).Where("role = ?", "assistant").Count(&count)
	if count != 0 {
		t.Fatal("canceled request committed a reply")
	}
}

func TestStreamCancellationReleasesUnreadMetadata(t *testing.T) {
	db := setupGrowthRegressionDB(t)
	db.Create(&database.Character{CharacterID: "unread-role", UserID: "owner", Name: "角色"})
	ctx, cancel := context.WithCancel(context.Background())
	svc := &ChatService{DB: db}
	events, errs := svc.StreamMessageContext(ctx, "owner", "unread-role", "你好", "text", "unread-user", "unread-reply")
	cancel()
	collectReleaseStream(t, events, errs)
}
