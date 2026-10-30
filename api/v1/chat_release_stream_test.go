package v1_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	apiv1 "star-meet-backend/api/v1"
	openai "star-meet-backend/core/openai_compat"
	"star-meet-backend/model/database"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

func TestSSEReplyRequiresSuccessfulTerminalEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, complete := range []bool{false, true} {
		name := "interrupted"
		if complete {
			name = "complete"
		}
		t.Run(name, func(t *testing.T) {
			db := setupChatMessageIdempotencyAPITestDB(t)
			db.Create(&database.User{UserID: "sse-owner", Email: "sse@example.test"})
			db.Create(&database.Character{CharacterID: "sse-role", UserID: "sse-owner", Name: "角色"})
			client := openai.NewClient("https://model.example.test", "test", "test")
			client.Client = &http.Client{Transport: chatAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
				body := "data: {\"choices\":[{\"delta\":{\"content\":\"實際回覆\"}}]}\n\n"
				if complete {
					body += "data: [DONE]\n\n"
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			api := apiv1.NewChatAPI(&service.ChatService{DB: db, OpenAICompatClient: client})
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("user_id", "sse-owner"); c.Next() })
			router.POST("/stream", api.StreamMessage)
			server := httptest.NewServer(router)
			defer server.Close()
			resp, err := http.Post(server.URL+"/stream", "application/json", bytes.NewBufferString(`{"character_id":"sse-role","message":"你好","user_message_id":"sse-user","reply_message_id":"sse-reply"}`))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			if !strings.Contains(text, "實際回覆") {
				t.Fatalf("stream lost content: %s", text)
			}
			if resp.Header.Get("X-Accel-Buffering") != "no" {
				t.Fatal("stream buffering was not disabled")
			}
			if complete {
				if strings.Count(text, "event:done") != 1 || strings.Contains(text, "event:error") {
					t.Fatalf("incorrect success terminal: %s", text)
				}
			} else if !strings.Contains(text, "event:error") || strings.Contains(text, "event:done") {
				t.Fatalf("interrupted stream was marked successful: %s", text)
			}
		})
	}
}

func TestSSEReplayIncludesSavedBodyWithoutRegeneration(t *testing.T) {
	db := setupChatMessageIdempotencyAPITestDB(t)
	db.Create(&database.User{UserID: "replay-owner", Email: "replay@example.test"})
	db.Create(&database.Character{CharacterID: "replay-role", UserID: "replay-owner", Name: "角色"})
	db.Create(&database.Session{SessionID: "replay-session", UserID: "replay-owner", CharacterID: "replay-role"})
	db.Create(&database.Message{MessageID: "replay-user", SessionID: "replay-session", UserID: "replay-owner", CharacterID: "replay-role", Role: "user", Content: "你好"})
	expectedReply := "  已保存的完整回覆\n second line\n"
	db.Create(&database.Message{MessageID: "replay-reply", SessionID: "replay-session", UserID: "replay-owner", CharacterID: "replay-role", Role: "assistant", Content: expectedReply})
	var modelCalls atomic.Int32
	client := openai.NewClient("https://model.example.test", "test", "test")
	client.Client = &http.Client{Transport: chatAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		modelCalls.Add(1)
		return nil, io.ErrUnexpectedEOF
	})}
	api := apiv1.NewChatAPI(&service.ChatService{DB: db, OpenAICompatClient: client})
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", "replay-owner"); c.Next() })
	router.POST("/stream", api.StreamMessage)
	server := httptest.NewServer(router)
	defer server.Close()
	for i := 0; i < 10; i++ {
		resp, err := http.Post(server.URL+"/stream", "application/json", bytes.NewBufferString(`{"character_id":"replay-role","message":"你好","user_message_id":"replay-user","reply_message_id":"replay-reply"}`))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "已保存的完整回覆") || strings.Count(string(body), "event:done") != 1 {
			t.Fatalf("lost replay body or terminal event: %s", body)
		}
		var replay strings.Builder
		for _, frame := range strings.Split(string(body), "\n\n") {
			if !strings.HasPrefix(frame, "event:message\n") {
				continue
			}
			var lines []string
			for _, line := range strings.Split(frame, "\n") {
				if strings.HasPrefix(line, "data:") {
					value := strings.TrimPrefix(line, "data:")
					lines = append(lines, strings.TrimPrefix(value, " "))
				}
			}
			replay.WriteString(strings.Join(lines, "\n"))
		}
		if replay.String() != expectedReply {
			t.Fatalf("SSE lost meaningful whitespace: got %q want %q", replay.String(), expectedReply)
		}
	}
	if modelCalls.Load() != 0 {
		t.Fatal("a completed reply regenerated on retry")
	}
	var count int64
	db.Model(&database.Message{}).Where("user_id = ?", "replay-owner").Count(&count)
	if count != 2 {
		t.Fatalf("retry duplicated messages: %d", count)
	}
}
