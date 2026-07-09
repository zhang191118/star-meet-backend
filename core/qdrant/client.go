package qdrant

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
	DefaultURL = "http://localhost:6333"
)

type Client struct {
	BaseURL string
	Client  *http.Client
}

type SearchRequest struct {
	CollectionName string      `json:"collection_name"`
	Vector        []float64   `json:"vector"`
	Limit         int         `json:"limit"`
	Filter        interface{} `json:"filter,omitempty"`
}

type SearchResult struct {
	Result  []ScoredPoint `json:"result"`
	Status  string       `json:"status"`
	Time    float64      `json:"time"`
}

type ScoredPoint struct {
	ID        interface{}   `json:"id"`
	Version   int           `json:"version"`
	Score     float64       `json:"score"`
	Payload   map[string]interface{} `json:"payload"`
	Vector    []float64    `json:"vector,omitempty"`
}

type UpsertRequest struct {
	CollectionName string      `json:"collection_name"`
	Points         []Point     `json:"points"`
}

type Point struct {
	ID        interface{}   `json:"id"`
	Vector    []float64     `json:"vector"`
	Payload   map[string]interface{} `json:"payload"`
}

type CreateCollectionRequest struct {
	Name         string `json:"name"`
	VectorSize   int    `json:"vector_size"`
	Distance     string `json:"distance"`
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultURL
	}

	// 禁用代理
	os.Setenv("NO_PROXY", "localhost,127.0.0.1")

	return &Client{
		BaseURL: baseURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Search 搜索相似向量
func (c *Client) Search(collection string, vector []float64, limit int, filter map[string]interface{}) ([]ScoredPoint, error) {
	reqBody := SearchRequest{
		CollectionName: collection,
		Vector:         vector,
		Limit:          limit,
	}
	if filter != nil {
		reqBody.Filter = filter
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", c.BaseURL+"/collections/"+collection+"/points/search", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result SearchResult
	json.Unmarshal(respBody, &result)

	if result.Status != "ok" {
		return nil, fmt.Errorf("qdrant search failed: %s", string(respBody))
	}

	return result.Result, nil
}

// Upsert 插入或更新向量
func (c *Client) Upsert(collection string, points []Point) error {
	reqBody := UpsertRequest{
		CollectionName: collection,
		Points:         points,
	}

	jsonBody, _ := json.Marshal(reqBody)
	fmt.Printf("[Qdrant] Upsert URL: %s\n", c.BaseURL+"/collections/"+collection+"/points")
	fmt.Printf("[Qdrant] Request body: %s\n", string(jsonBody))

	req, err := http.NewRequest("PUT", c.BaseURL+"/collections/"+collection+"/points", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("[Qdrant] Response status: %d, body: %s\n", resp.StatusCode, string(respBody))

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("qdrant upsert failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CreateCollection 创建集合
func (c *Client) CreateCollection(name string, vectorSize int) error {
	reqBody := CreateCollectionRequest{
		Name:       name,
		VectorSize: vectorSize,
		Distance:   "Cosine",
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("PUT", c.BaseURL+"/collections/"+name, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// CollectionExists 检查集合是否存在
func (c *Client) CollectionExists(name string) bool {
	req, _ := http.NewRequest("GET", c.BaseURL+"/collections/"+name, nil)
	resp, err := c.Client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}
