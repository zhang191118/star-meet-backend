package mem0

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	Host    string
	APIKey  string
	Client  *http.Client
}

type Memory struct {
	ID        string                 `json:"id"`
	Memory    string                 `json:"memory"`
	Metadata  map[string]interface{} `json:"metadata"`
	Score     float64                `json:"score"`
	CreatedAt string                 `json:"created_at"`
}

type SearchResult struct {
	ID      string  `json:"id"`
	Memory  string  `json:"memory"`
	Score   float64 `json:"score"`
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
		Host: host,
		APIKey: apiKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// 添加记忆
func (c *Client) Add(userID, agentID, content string, metadata map[string]interface{}) (*AddResponse, error) {
	url := fmt.Sprintf("%s/memories", c.Host)

	body := map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": content,
			},
		},
		"user_id":  userID,
		"agent_id": agentID,
	}

	if metadata != nil {
		body["metadata"] = metadata
	}

	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
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
		return nil, fmt.Errorf("mem0 add failed: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	// 解析返回结果
	id := ""
	if idVal, ok := result["id"]; ok {
		id = idVal.(string)
	}

	return &AddResponse{
		ID:      id,
		Message: "memory added",
	}, nil
}

// 搜索记忆
func (c *Client) Search(userID, agentID, query string, limit int) ([]SearchResult, error) {
	url := fmt.Sprintf("%s/memories/search", c.Host)

	body := map[string]interface{}{
		"query":    query,
		"user_id":  userID,
		"agent_id": agentID,
		"limit":    limit,
	}

	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
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
		return nil, fmt.Errorf("mem0 search failed: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var result SearchResponse
	json.Unmarshal(respBody, &result)

	return result.Results, nil
}

// 获取所有记忆
func (c *Client) GetAll(userID, agentID string, limit, offset int) ([]Memory, error) {
	url := fmt.Sprintf("%s/memories?user_id=%s&agent_id=%s&limit=%d&offset=%d",
		c.Host, userID, agentID, limit, offset)

	req, _ := http.NewRequest("GET", url, nil)
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result GetAllResponse
	json.NewDecoder(resp.Body).Decode(&result)

	return result.Results, nil
}

// 删除记忆
func (c *Client) Delete(memoryID string) error {
	url := fmt.Sprintf("%s/memories/%s", c.Host, memoryID)

	req, _ := http.NewRequest("DELETE", url, nil)
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
