package minimax

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

const (
	// MiniMax V2 API URL (OpenAI 兼容)
	APIURL = "https://api.minimaxi.com/v1/chat/completions"
)

type Client struct {
	APIKey string
	Model  string
	Client *http.Client
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

type StreamResponse struct {
	ID      string         `json:"id"`
	Choices []StreamChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta"`
	FinishReason string  `json:"finish_reason"`
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		Model:  "abab6.5s-chat", // 对应 M2-her 等级的高性能模型
		Client: &http.Client{
			Timeout: 30 * time.Second, // 优化：减少超时时间，加快失败检测
		},
	}
}

// StreamGenerate 调用 MiniMax API 生成流式回复
func (c *Client) StreamGenerate(prompt string, systemPrompt string, maxTokens int) (<-chan string, <-chan error) {
	return c.StreamGenerateContext(context.Background(), prompt, systemPrompt, maxTokens)
}

func (c *Client) StreamGenerateContext(ctx context.Context, prompt string, systemPrompt string, maxTokens int) (<-chan string, <-chan error) {
	ch := make(chan string)
	errCh := make(chan error, 1)

	if c.APIKey == "" {
		errCh <- fmt.Errorf("minimax api key not configured")
		close(ch)
		close(errCh)
		return ch, errCh
	}

	go func() {
		defer close(ch)
		defer close(errCh)

		messages := []Message{}
		if systemPrompt != "" {
			messages = append(messages, Message{
				Role:    "system",
				Content: systemPrompt,
			})
		}
		messages = append(messages, Message{
			Role:    "user",
			Content: prompt,
		})

		reqBody := GenerateRequest{
			Model:       c.Model,
			Messages:    messages,
			Temperature: 0.9,
			TopP:        0.95,
			MaxTokens:   maxTokens,
			Stream:      true,
		}

		jsonBody, _ := json.Marshal(reqBody)

		req, err := http.NewRequestWithContext(ctx, "POST", APIURL, bytes.NewBuffer(jsonBody))
		if err != nil {
			errCh <- err
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.APIKey)

		resp, err := c.Client.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			errCh <- fmt.Errorf("minimax api error: status=%d", resp.StatusCode)
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
			if data == "[DONE]" {
				return
			}

			var genResp StreamResponse
			if err := json.Unmarshal([]byte(data), &genResp); err != nil {
				continue
			}
			if genResp.Error != nil {
				errCh <- fmt.Errorf("minimax provider returned an error")
				return
			}

			if len(genResp.Choices) > 0 {
				content := genResp.Choices[0].Delta.Content
				if content != "" {
					select {
					case ch <- content:
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					}
				}
				// Some OpenAI-compatible providers serialise "null" as a string.
				if reason := strings.ToLower(strings.TrimSpace(genResp.Choices[0].FinishReason)); reason != "" && reason != "null" {
					return
				}
			}
		}
		if err := reader.Err(); err != nil {
			errCh <- err
		} else {
			errCh <- io.ErrUnexpectedEOF
		}
	}()

	return ch, errCh
}

// Generate 调用 MiniMax API 生成回复 (非流式)
func (c *Client) Generate(prompt string, systemPrompt string, maxTokens int) (string, error) {
	return c.GenerateContext(context.Background(), prompt, systemPrompt, maxTokens)
}

func (c *Client) GenerateContext(ctx context.Context, prompt string, systemPrompt string, maxTokens int) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("minimax api key not configured")
	}

	messages := []Message{}
	if systemPrompt != "" {
		messages = append(messages, Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	messages = append(messages, Message{
		Role:    "user",
		Content: prompt,
	})

	reqBody := GenerateRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: 0.9,
		TopP:        0.95,
		MaxTokens:   maxTokens,
		Stream:      false,
	}

	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", APIURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("minimax api error: status=%d", resp.StatusCode)
	}

	var genResp struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", err
	}

	if len(genResp.Choices) > 0 {
		return genResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no content generated")
}
