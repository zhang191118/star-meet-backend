package openai_compat_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"star-meet-backend/core/deepseek"
	"star-meet-backend/core/minimax"
	openai "star-meet-backend/core/openai_compat"
	"star-meet-backend/core/qwen"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type streamingClient interface {
	StreamGenerateContext(context.Context, string, string, int) (<-chan string, <-chan error)
}

func streamClients(body string) map[string]streamingClient {
	httpClient := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	a := openai.NewClient("https://model.example.test", "test", "test")
	a.Client = httpClient
	b := minimax.NewClient("test")
	b.Client = httpClient
	return map[string]streamingClient{"gateway": a, "minimax": b}
}

func TestStreamsRequireExplicitProviderCompletion(t *testing.T) {
	chunk := "data:{\"choices\":[{\"delta\":{\"content\":\"回覆\"}}]}\n\n"
	for _, tc := range []struct {
		name, suffix string
		interrupted  bool
	}{
		{"done", "data: [DONE]\n\n", false},
		{"finish_without_trailing_newline", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}", false},
		{"truncated_eof", "", true},
	} {
		for name, client := range streamClients(chunk + tc.suffix) {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				content, errs := client.StreamGenerateContext(ctx, "hello", "", 100)
				var got strings.Builder
				for part := range content {
					got.WriteString(part)
				}
				if got.String() != "回覆" {
					t.Fatalf("lost content: %q", got.String())
				}
				err := <-errs
				if tc.interrupted && !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("EOF must be interrupted: %v", err)
				}
				if !tc.interrupted && err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestStreamTreatsStringNullFinishReasonAsNonTerminal(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"你\"},\"finish_reason\":\"null\"}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"null\"}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	for name, client := range streamClients(body) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			content, errs := client.StreamGenerateContext(ctx, "hello", "", 100)
			var got strings.Builder
			for part := range content {
				got.WriteString(part)
			}
			if got.String() != "你好" {
				t.Fatalf("reply was truncated: %q", got.String())
			}
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStreamCancellationUnblocksUnreadContent(t *testing.T) {
	for name, client := range streamClients("data: {\"choices\":[{\"delta\":{\"content\":\"waiting\"}}]}\n\n") {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			_, errs := client.StreamGenerateContext(ctx, "hello", "", 100)
			cancel()
			select {
			case err := <-errs:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected cancellation: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("producer leaked while consumer stopped reading")
			}
		})
	}
}

func TestGenerateCancellationReachesEveryChatFallback(t *testing.T) {
	httpClient := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	a := openai.NewClient("https://model.example.test", "test", "test")
	a.Client = httpClient
	b := minimax.NewClient("test")
	b.Client = httpClient
	c := deepseek.NewClient("test")
	c.Client = httpClient
	d := qwen.NewClient("test")
	d.Client = httpClient
	for name, generate := range map[string]func(context.Context, string, string, int) (string, error){
		"gateway": a.GenerateContext, "minimax": b.GenerateContext, "deepseek": c.GenerateContext, "qwen": d.GenerateContext,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			_, err := generate(ctx, "hello", "", 100)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("HTTP request lost shared deadline: %v", err)
			}
		})
	}
}

func TestMiniMaxDoesNotExposeProviderErrorBody(t *testing.T) {
	c := minimax.NewClient("test")
	c.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("private chat and credentials")), Header: make(http.Header)}, nil
	})}
	_, errs := c.StreamGenerateContext(context.Background(), "hello", "", 100)
	if err := <-errs; err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe error: %v", err)
	}
	if _, err := c.Generate("hello", "", 100); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe error: %v", err)
	}
}
