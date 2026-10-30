package mem0

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSupportsRepositoryEnvelopesAndWritesCanonicalContentWithoutInference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "DELETE" {
			w.WriteHeader(404)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/memories" {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["infer"] != false {
				t.Error("canonical content was sent for a second inference")
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]interface{}{"results": []map[string]interface{}{{"id": "external-id", "memory": "saved text", "metadata": map[string]string{"memory_id": "canonical-id"}, "score": 0.9}}}})
	}))
	defer server.Close()
	client := NewClient(server.URL, "test")
	added, err := client.Add("user", "char", "saved text", map[string]interface{}{"memory_id": "canonical-id"})
	if err != nil || added.ID != "external-id" {
		t.Fatalf("bad add: %+v %v", added, err)
	}
	rows, err := client.GetAll("user", "char", 100, 0)
	if err != nil || len(rows) != 1 || rows[0].Metadata["memory_id"] != "canonical-id" {
		t.Fatalf("bad list %+v %v", rows, err)
	}
	found, err := client.Search("user", "char", "saved", 5)
	if err != nil || len(found) != 1 || found[0].ID != "external-id" {
		t.Fatalf("bad search %+v %v", found, err)
	}
	if err := client.Delete("already-gone"); err != nil {
		t.Fatal("delete retry must be idempotent", err)
	}
}
func TestClientRejectsInvalidURLWithoutPanic(t *testing.T) {
	client := NewClient("://invalid", "test")
	if _, err := client.Add("u", "c", "text", nil); err == nil {
		t.Fatal("bad URL accepted")
	}
	if err := client.Delete("id"); err == nil {
		t.Fatal("bad delete URL accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewClient("http://127.0.0.1:1", "test").GetAllContext(ctx, "u", "c", 1, 0); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
