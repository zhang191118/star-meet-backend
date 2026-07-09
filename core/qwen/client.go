package qwen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	APIURL = "https://dashscope.aliyuncs.com/api/v1/services/aigc/text-generation/generation"
)

type Client struct {
	APIKey  string
	Client  *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GenerateRequest struct {
	Model      string    `json:"model"`
	Input      Input     `json:"input"`
	Parameters Parameters `json:"parameters"`
}

type Input struct {
	Messages []Message `json:"messages"`
}

type Parameters struct {
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"top_p"`
	MaxTokens   int     `json:"max_tokens"`
	Stream      bool    `json:"stream"`
}

type GenerateResponse struct {
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Output    Output `json:"output"`
	Usage     Usage  `json:"usage"`
}

type Output struct {
	Text         string `json:"text"`
	FinishReason string `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// 直接调用Qwen（不使用RAG Flow时）
func (c *Client) Generate(prompt string, systemPrompt string, maxTokens int) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("qwen api key not configured")
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
		Model: "qwen3-coder-plus",
		Input: Input{
			Messages: messages,
		},
		Parameters: Parameters{
			Temperature: 0.7,
			TopP:        0.8,
			MaxTokens:   maxTokens,
			Stream:      false,
		},
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

	respBody, _ := io.ReadAll(resp.Body)

	var genResp GenerateResponse
	json.Unmarshal(respBody, &genResp)

	if genResp.Code != "" && genResp.Code != "success" {
		return "", fmt.Errorf("qwen api error: %s - %s", genResp.Code, genResp.Message)
	}

	return genResp.Output.Text, nil
}
