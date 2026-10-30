package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"star-meet-backend/model/database"
)

// APNsConfig contains the credentials for Apple's token-based provider API.
// PrivateKey accepts the PEM contents of the .p8 file.
type APNsConfig struct {
	KeyID         string
	TeamID        string
	Topic         string
	PrivateKey    string
	UseProduction bool
}

type APNsClient struct {
	keyID         string
	teamID        string
	topic         string
	privateKey    *ecdsa.PrivateKey
	production    bool
	httpClient    *http.Client
	tokenMu       sync.Mutex
	providerToken string
	tokenExpires  time.Time
}

func NewAPNsClient(config APNsConfig) (*APNsClient, error) {
	config.KeyID = strings.TrimSpace(config.KeyID)
	config.TeamID = strings.TrimSpace(config.TeamID)
	config.Topic = strings.TrimSpace(config.Topic)
	if config.KeyID == "" || config.TeamID == "" || config.Topic == "" || strings.TrimSpace(config.PrivateKey) == "" {
		return nil, errors.New("APNs requires key id, team id, topic and private key")
	}

	block, _ := pem.Decode([]byte(config.PrivateKey))
	if block == nil {
		return nil, errors.New("invalid APNs private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse APNs private key: %w", err)
	}
	privateKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("APNs private key is not an ECDSA key")
	}

	return &APNsClient{
		keyID:      config.KeyID,
		teamID:     config.TeamID,
		topic:      config.Topic,
		privateKey: privateKey,
		production: config.UseProduction,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (c *APNsClient) endpointFor(device database.PushDevice) string {
	production := c.production
	if device.Environment == "production" {
		production = true
	} else if device.Environment == "sandbox" {
		production = false
	}
	if production {
		return "https://api.push.apple.com"
	}
	return "https://api.sandbox.push.apple.com"
}

func (c *APNsClient) token(now time.Time) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.providerToken != "" && now.Before(c.tokenExpires) {
		return c.providerToken, nil
	}

	claims := jwt.MapClaims{
		"iss": c.teamID,
		"iat": now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = c.keyID
	signed, err := token.SignedString(c.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign APNs provider token: %w", err)
	}
	c.providerToken = signed
	// Apple accepts a token for up to one hour. Refresh slightly early.
	c.tokenExpires = now.Add(50 * time.Minute)
	return signed, nil
}

type APNsError struct {
	Status int
	Reason string
}

func (e *APNsError) Error() string {
	return fmt.Sprintf("APNs request failed with status %d: %s", e.Status, e.Reason)
}

func (c *APNsClient) Send(ctx context.Context, device database.PushDevice, payload map[string]interface{}) error {
	if c == nil || c.privateKey == nil {
		return errors.New("APNs client is not configured")
	}
	providerToken, err := c.token(time.Now().UTC())
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode APNs payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpointFor(device)+"/3/device/"+device.Token, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "bearer "+providerToken)
	request.Header.Set("apns-topic", c.topic)
	request.Header.Set("apns-push-type", "alert")
	request.Header.Set("apns-priority", "10")
	request.Header.Set("content-type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	var errorBody struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(responseBody, &errorBody)
	if errorBody.Reason == "" {
		errorBody.Reason = strings.TrimSpace(string(responseBody))
	}
	return &APNsError{Status: response.StatusCode, Reason: errorBody.Reason}
}
