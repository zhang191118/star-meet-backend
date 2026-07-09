package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	APIURL = "https://generativelanguage.googleapis.com/v1beta/models"
)

// Client Gemini客户端
type Client struct {
	APIKey  string
	Model   string
	Client  *http.Client
}

// Request 请求结构
type Request struct {
	Contents []Content `json:"contents"`
}

// Content 内容
type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// Part 部分
type Part struct {
	Text string `json:"text"`
}

// Response 响应结构
type Response struct {
	Candidates []Candidate `json:"candidates"`
}

// Candidate 候选
type Candidate struct {
	Content Content `json:"content"`
}

// NewClient 创建Gemini客户端
func NewClient(apiKey string, model string) *Client {
	// 禁用代理
	os.Unsetenv("http_proxy")
	os.Unsetenv("HTTP_PROXY")
	os.Unsetenv("https_proxy")
	os.Unsetenv("HTTPS_PROXY")

	if model == "" {
		model = "gemini-2.0-flash"
	}

	return &Client{
		APIKey: apiKey,
		Model:  model,
		Client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Generate 生成回复
func (c *Client) Generate(prompt, systemPrompt string, maxTokens int) (string, error) {
	// 构建完整提示词
	fullPrompt := systemPrompt + "\n\n" + prompt

	reqBody := Request{
		Contents: []Content{
			{
				Role: "user",
				Parts: []Part{
					{Text: fullPrompt},
				},
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request failed: %v", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", APIURL, c.Model, c.APIKey)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response failed: %v", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result Response
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal response failed: %v, body: %s", err, string(respBody))
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}
