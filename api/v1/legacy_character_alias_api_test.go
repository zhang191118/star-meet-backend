package v1_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"star-meet-backend/model/database"
)

func TestLegacyPublicStreamRetryResolvesPrivateRoleBeforeQuota(t *testing.T) {
	db, router, _ := setupChatQuotaAPITest(t, 1)
	createChatQuotaAPIUser(t, db, "legacy01", "legacy01@example.test", "free", nil)
	if err := db.Create(&database.Character{
		CharacterID: "legacy-public",
		UserID:      "system_admin",
		Name:        "Legacy",
		Gender:      "male",
		IsPublic:    1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Character{
		CharacterID: "legacy-public_legacy01",
		UserID:      "legacy01",
		Name:        "Legacy private",
		Gender:      "male",
		IsPublic:    2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Session{
		SessionID:   "legacy-private-session",
		UserID:      "legacy01",
		CharacterID: "legacy-public_legacy01",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "legacy-user",
		SessionID:   "legacy-private-session",
		CharacterID: "legacy-public_legacy01",
		UserID:      "legacy01",
		Role:        "user",
		Content:     "hello",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Message{
		MessageID:   "legacy-reply",
		SessionID:   "legacy-private-session",
		CharacterID: "legacy-public_legacy01",
		UserID:      "legacy01",
		Role:        "assistant",
		Content:     "saved private reply",
		MessageType: "text",
	}).Error; err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(router)
	defer server.Close()
	req, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/v1/chat/stream",
		bytes.NewBufferString(`{"character_id":"legacy-public","message":"hello","user_message_id":"legacy-user","reply_message_id":"legacy-reply"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "legacy01"))
	req.Header.Set("X-Request-ID", "legacy-retry")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("legacy retry was rejected before service mapping: %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "saved private reply") {
		t.Fatalf("legacy retry did not replay the private reply: %s", body)
	}
	var publicMessages int64
	if err := db.Model(&database.Message{}).
		Where("user_id = ? AND character_id = ?", "legacy01", "legacy-public").
		Count(&publicMessages).Error; err != nil {
		t.Fatal(err)
	}
	if publicMessages != 0 {
		t.Fatalf("legacy stream retry created public history: %d", publicMessages)
	}
}
