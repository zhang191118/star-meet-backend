package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	APIURL = "https://open.bigmodel.cn/api/paas/v4/embeddings"
)

type Client struct {
	APIKey string
	Client *http.Client
}

type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type EmbeddingResponse struct {
	Data  []EmbeddingData `json:"data"`
	Usage Usage           `json:"usage"`
}

type EmbeddingData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type Usage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetEmbedding 获取文本的embedding向量
func (c *Client) GetEmbedding(text string) ([]float64, error) {
	return c.GetEmbeddingContext(context.Background(), text)
}

func (c *Client) GetEmbeddingContext(ctx context.Context, text string) ([]float64, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("zhipu api key not configured")
	}

	reqBody := EmbeddingRequest{
		Model: "embedding-2",
		Input: []string{text},
	}

	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", APIURL, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var embResp EmbeddingResponse
	json.Unmarshal(respBody, &embResp)

	if len(embResp.Data) == 0 {
		return nil, fmt.Errorf("embedding failed: status=%d", resp.StatusCode)
	}

	return embResp.Data[0].Embedding, nil
}
