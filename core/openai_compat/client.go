package openai_compat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GenerateRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type GenerateResponse struct {
	ID      string    `json:"id"`
	Model   string    `json:"model"`
	Choices []Choice  `json:"choices"`
	Error   *APIError `json:"error,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	Delta        Message `json:"delta"`
	FinishReason string  `json:"finish_reason"`
}

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: normalizeBaseURL(baseURL),
		Model:   strings.TrimSpace(model),
		Client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.APIKey != "" && c.BaseURL != "" && c.Model != ""
}

func (c *Client) Generate(prompt string, systemPrompt string, maxTokens int) (string, error) {
	return c.GenerateContext(context.Background(), prompt, systemPrompt, maxTokens)
}

func (c *Client) GenerateContext(ctx context.Context, prompt string, systemPrompt string, maxTokens int) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("openai-compatible client not configured")
	}

	body, err := c.requestBody(prompt, systemPrompt, maxTokens, false)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatCompletionsURL(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai-compatible api error: status=%d", resp.StatusCode)
	}

	var genResp GenerateResponse
	if err := json.Unmarshal(respBody, &genResp); err != nil {
		return "", fmt.Errorf("openai-compatible response parse error: %w", err)
	}
	if genResp.Error != nil && genResp.Error.Message != "" {
		return "", fmt.Errorf("openai-compatible provider returned an error")
	}
	if len(genResp.Choices) == 0 || strings.TrimSpace(genResp.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("openai-compatible api returned no choices")
	}

	return strings.TrimSpace(genResp.Choices[0].Message.Content), nil
}

func (c *Client) StreamGenerate(prompt string, systemPrompt string, maxTokens int) (<-chan string, <-chan error) {
	return c.StreamGenerateContext(context.Background(), prompt, systemPrompt, maxTokens)
}

func (c *Client) StreamGenerateContext(ctx context.Context, prompt string, systemPrompt string, maxTokens int) (<-chan string, <-chan error) {
	ch := make(chan string)
	errCh := make(chan error, 1)

	if !c.Configured() {
		errCh <- fmt.Errorf("openai-compatible client not configured")
		close(ch)
		close(errCh)
		return ch, errCh
	}

	go func() {
		defer close(ch)
		defer close(errCh)

		body, err := c.requestBody(prompt, systemPrompt, maxTokens, true)
		if err != nil {
			errCh <- err
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatCompletionsURL(), bytes.NewReader(body))
		if err != nil {
			errCh <- err
			return
		}
		c.setHeaders(req)

		resp, err := c.Client.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			errCh <- fmt.Errorf("openai-compatible api error: status=%d", resp.StatusCode)
			return
		}

		reader := bufio.NewScanner(resp.Body)
		reader.Buffer(make([]byte, 4096), 1024*1024)
		for reader.Scan() {
			line := strings.TrimSpace(reader.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return
				}
				continue
			}

			var genResp GenerateResponse
			if err := json.Unmarshal([]byte(data), &genResp); err != nil {
				continue
			}
			if genResp.Error != nil && genResp.Error.Message != "" {
				errCh <- fmt.Errorf("openai-compatible provider returned an error")
				return
			}
			if len(genResp.Choices) == 0 {
				continue
			}
			content := genResp.Choices[0].Delta.Content
			if content != "" {
				select {
				case ch <- content:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
			// DashScope serialises "null" as a string on every non-terminal chunk;
			// treating it as terminal returns after the first token.
			if reason := strings.ToLower(strings.TrimSpace(genResp.Choices[0].FinishReason)); reason != "" && reason != "null" {
				return
			}
		}
		if err := reader.Err(); err != nil {
			errCh <- err
		} else {
			// A clean TCP EOF is not a completed generation without a terminal marker.
			errCh <- io.ErrUnexpectedEOF
		}
	}()

	return ch, errCh
}

func (c *Client) requestBody(prompt string, systemPrompt string, maxTokens int, stream bool) ([]byte, error) {
	messages := make([]Message, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, Message{Role: "user", Content: prompt})

	return json.Marshal(GenerateRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: 0.9,
		TopP:        0.95,
		MaxTokens:   maxTokens,
		Stream:      stream,
	})
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
}

func (c *Client) chatCompletionsURL() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if strings.HasSuffix(base, "/v1/chat/completions") || strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
