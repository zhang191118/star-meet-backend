package minimax

import (
	"bufio"
	"bytes"
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

		req, _ := http.NewRequest("POST", APIURL, bytes.NewBuffer(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.APIKey)

		resp, err := c.Client.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("minimax api error: %d - %s", resp.StatusCode, string(body))
			return
		}

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				errCh <- err
				return
			}

			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var genResp StreamResponse
			if err := json.Unmarshal([]byte(data), &genResp); err != nil {
				continue
			}

			if len(genResp.Choices) > 0 {
				content := genResp.Choices[0].Delta.Content
				if content != "" {
					ch <- content
				}
			}
		}
	}()

	return ch, errCh
}

// Generate 调用 MiniMax API 生成回复 (非流式)
func (c *Client) Generate(prompt string, systemPrompt string, maxTokens int) (string, error) {
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

	req, _ := http.NewRequest("POST", APIURL, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("minimax api error: %d - %s", resp.StatusCode, string(body))
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
