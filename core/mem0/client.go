package mem0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	Host   string
	APIKey string
	Client *http.Client
}

type Memory struct {
	ID        string                 `json:"id"`
	Memory    string                 `json:"memory"`
	Metadata  map[string]interface{} `json:"metadata"`
	Score     float64                `json:"score"`
	CreatedAt string                 `json:"created_at"`
}

type SearchResult struct {
	ID     string  `json:"id"`
	Memory string  `json:"memory"`
	Score  float64 `json:"score"`
}

type AddResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

type GetAllResponse struct {
	Results []Memory `json:"results"`
}

// 新建客户端
func NewClient(host, apiKey string) *Client {
	if host == "" {
		host = "http://localhost:9000"
	}
	return &Client{
		Host:   host,
		APIKey: apiKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.APIKey != "" && c.APIKey != "your_mem0_api_key"
}

// 添加记忆
func (c *Client) Add(userID, agentID, content string, metadata map[string]interface{}) (*AddResponse, error) {
	return c.AddContext(context.Background(), userID, agentID, content, metadata)
}

func (c *Client) AddContext(ctx context.Context, userID, agentID, content string, metadata map[string]interface{}) (*AddResponse, error) {
	url := fmt.Sprintf("%s/memories", c.Host)

	body := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": content,
			},
		},
		"infer":    false,
		"user_id":  userID,
		"agent_id": agentID,
	}

	if metadata != nil {
		body["metadata"] = metadata
	}

	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("invalid mem0 URL")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mem0 request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("mem0 add failed: status=%d", resp.StatusCode)
	}

	records, err := decodeMem0Memories(respBody, 0)
	if err != nil {
		return nil, err
	}
	id := ""
	if len(records) > 0 {
		id = records[0].ID
	}
	return &AddResponse{ID: id, Message: "memory added"}, nil
}

// 搜索记忆
func (c *Client) Search(userID, agentID, query string, limit int) ([]SearchResult, error) {
	return c.SearchContext(context.Background(), userID, agentID, query, limit)
}

func (c *Client) SearchContext(ctx context.Context, userID, agentID, query string, limit int) ([]SearchResult, error) {
	url := fmt.Sprintf("%s/memories/search", c.Host)

	body := map[string]interface{}{
		"query":    query,
		"user_id":  userID,
		"agent_id": agentID,
		"limit":    limit,
	}

	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("invalid mem0 URL")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mem0 request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("mem0 search failed: status=%d", resp.StatusCode)
	}

	records, err := decodeMem0Memories(respBody, 0)
	if err != nil {
		return nil, err
	}
	result := make([]SearchResult, 0, len(records))
	for _, row := range records {
		result = append(result, SearchResult{ID: row.ID, Memory: row.Memory, Score: row.Score})
	}
	return result, nil
}

// 获取所有记忆
func (c *Client) GetAll(userID, agentID string, limit, offset int) ([]Memory, error) {
	return c.GetAllContext(context.Background(), userID, agentID, limit, offset)
}

func (c *Client) GetAllContext(ctx context.Context, userID, agentID string, limit, offset int) ([]Memory, error) {
	endpoint, err := url.Parse(c.Host + "/memories")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("user_id", userID)
	if agentID != "" {
		query.Set("agent_id", agentID)
	}
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mem0 list failed: status=%d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	return decodeMem0Memories(body, 0)
}

// 删除记忆
func (c *Client) Delete(memoryID string) error {
	return c.DeleteContext(context.Background(), memoryID)
}

func (c *Client) DeleteContext(ctx context.Context, memoryID string) error {
	url := fmt.Sprintf("%s/memories/%s", c.Host, memoryID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("invalid mem0 URL")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("mem0 delete failed: status=%d", resp.StatusCode)
	}
	return nil
}

// Support the native SDK response and this repository's success/data envelope.
func decodeMem0Memories(body []byte, depth int) ([]Memory, error) {
	if depth > 4 {
		return nil, fmt.Errorf("mem0 response nesting is invalid")
	}
	var records []Memory
	if err := json.Unmarshal(body, &records); err == nil {
		return records, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, fmt.Errorf("invalid mem0 response")
	}
	for _, key := range []string{"results", "data"} {
		if value, ok := object[key]; ok && string(value) != "null" {
			return decodeMem0Memories(value, depth+1)
		}
	}
	if _, ok := object["id"]; ok {
		var row Memory
		if err := json.Unmarshal(body, &row); err != nil {
			return nil, fmt.Errorf("invalid mem0 record")
		}
		return []Memory{row}, nil
	}
	return []Memory{}, nil
}
