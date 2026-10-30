package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	openai_compat "star-meet-backend/core/openai_compat"
	"star-meet-backend/model/database"
)

func TestParseStructuredMemoryExtractionNormalizesAndDeduplicates(t *testing.T) {
	raw := "```json\n{\"memories\":[" +
		"{\"type\":\"preference\",\"content\":\"用户喜欢夜跑\",\"importance\":9}," +
		"{\"type\":\"preference\",\"content\":\"用户喜欢夜跑\",\"importance\":2}," +
		"{\"type\":\"promise\",\"content\":\"角色答应周末陪用户复习\",\"importance\":4}," +
		"{\"type\":\"unknown\",\"content\":\"忽略我\",\"importance\":3}" +
		"]}\n```"
	extraction, err := parseStructuredMemoryExtraction(raw)
	if err != nil {
		t.Fatalf("parse extraction: %v", err)
	}
	if len(extraction.Memories) != 2 {
		t.Fatalf("expected two normalized memories, got %+v", extraction.Memories)
	}
	if extraction.Memories[0].Type != "preference" || extraction.Memories[0].Importance != 5 {
		t.Fatalf("expected clamped preference memory, got %+v", extraction.Memories[0])
	}
	if extraction.Memories[1].Type != "promise" {
		t.Fatalf("expected promise memory, got %+v", extraction.Memories[1])
	}
}

func TestParseStructuredMemoryExtractionRejectsMalformedPayload(t *testing.T) {
	if _, err := parseStructuredMemoryExtraction(`{"memories":[{"content":"missing type"}]}`); err == nil {
		t.Fatal("expected malformed memory item to be rejected")
	}
}

func TestBuildMemoryExtractionPromptContainsConversationAndSchema(t *testing.T) {
	prompt := buildMemoryExtractionPrompt("我周末要考试，也喜欢在晚上跑步。", "那我记住了，周末陪你复习。")
	for _, expected := range []string{"我周末要考试", "陪你复习", "preference", "promise", "只输出合法 JSON"} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("memory extraction prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestPersistStructuredMemoriesDeduplicatesAndStoresType(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.Memory{}); err != nil {
		t.Fatalf("migrate memories: %v", err)
	}
	if err := db.Create(&database.User{UserID: "memory-user", Nickname: "Memory User"}).Error; err != nil {
		t.Fatal(err)
	}
	service := &ChatService{DB: db}
	extraction := StructuredMemoryExtraction{Memories: []StructuredMemory{
		{Type: MemoryTypePreference, Content: "用户喜欢夜跑", Importance: 3},
		{Type: MemoryTypePreference, Content: "用户喜欢夜跑", Importance: 5},
		{Type: MemoryTypePromise, Content: "周末陪用户复习", Importance: 4},
	}}
	if err := service.persistStructuredMemories("memory-user", "memory-char", extraction); err != nil {
		t.Fatalf("persist memories: %v", err)
	}
	var memories []database.Memory
	if err := db.Where("user_id = ? AND character_id = ?", "memory-user", "memory-char").Order("memory_type ASC").Find(&memories).Error; err != nil {
		t.Fatalf("load memories: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected duplicate preference to collapse, got %+v", memories)
	}
	if memories[0].MemoryType != MemoryTypePreference || memories[0].Importance != 5 {
		t.Fatalf("expected strongest preference to persist, got %+v", memories[0])
	}
}

func TestShouldExtractStructuredMemoryOnlyForHighSignalTurns(t *testing.T) {
	if shouldExtractStructuredMemory("嗯嗯", "好呀") {
		t.Fatal("small talk should not trigger memory extraction")
	}
	if !shouldExtractStructuredMemory("我喜欢晚上跑步，周末还要考试", "我记住了") {
		t.Fatal("explicit preference/event should trigger memory extraction")
	}
}

func TestExtractStructuredMemoriesUsesStructuredModelOutput(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	client := openai_compat.NewClient("http://memory.test", "key", "model")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"memories\":[{\"type\":\"preference\",\"content\":\"用户喜欢夜跑\",\"importance\":4}]}"}}]}`)),
			Request:    req,
		}, nil
	})}
	service := &ChatService{DB: db, OpenAICompatClient: client}
	extraction, err := service.extractStructuredMemories("我喜欢晚上跑步", "我记住了")
	if err != nil {
		t.Fatalf("extract structured memories: %v", err)
	}
	if len(extraction.Memories) != 1 || extraction.Memories[0].Type != MemoryTypePreference {
		t.Fatalf("unexpected extraction: %+v", extraction)
	}
	if _, err := json.Marshal(extraction); err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
}
