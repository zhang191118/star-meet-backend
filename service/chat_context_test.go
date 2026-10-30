package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	openai_compat "star-meet-backend/core/openai_compat"
	"star-meet-backend/model/database"

	"gorm.io/gorm"
)

func TestBuildPromptKeepsCharacterAndUserFieldsInTheirSections(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.User{
		UserID:   "prompt-user",
		Nickname: "PromptUser",
		Language: "en",
		Region:   "US",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &ChatService{DB: db}
	prompt, err := service.buildPrompt("prompt-user", database.Character{
		CharacterID:  "prompt-character",
		Name:         "PromptCharacter",
		Personality:  "PERSONALITY_VALUE",
		Relationship: "RELATIONSHIP_VALUE",
		Background:   "BACKGROUND_VALUE",
		Style:        "温暖型",
		Example:      "EXAMPLE_VALUE",
		Narrative:    "NARRATIVE_VALUE",
		BasicSetting: "BASIC_SETTING_VALUE",
	}, "USER_MESSAGE_VALUE", "happy", nil)
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}

	expectations := []string{
		"名字：PromptCharacter",
		"性格：PERSONALITY_VALUE",
		"关系：RELATIONSHIP_VALUE",
		"背景：BACKGROUND_VALUE",
		"【角色说话风格规范（必须严格遵守）】\nEXAMPLE_VALUE",
		"【故事背景】\nNARRATIVE_VALUE",
		"【人设履历】\nBASIC_SETTING_VALUE",
		"【用户刚才说的话】\nUSER_MESSAGE_VALUE",
		"English",
	}
	for _, expected := range expectations {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt is missing expected field placement %q\n%s", expected, prompt)
		}
	}

	assertAfterSection := func(section, value string) {
		sectionIndex := strings.Index(prompt, section)
		valueIndex := strings.Index(prompt, value)
		if sectionIndex == -1 || valueIndex <= sectionIndex {
			t.Errorf("expected %q after section %q", value, section)
		}
	}
	assertAfterSection("【用户刚才说的话】", "USER_MESSAGE_VALUE")
	assertAfterSection("【语言要求】", "【重要】你必须用English回复用户的所有消息")
}

func TestMemorySearchFilterIsScopedToUserAndCharacter(t *testing.T) {
	filter := memorySearchFilter("user-1", "character-1")

	must, ok := filter["must"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected must clauses, got %#v", filter["must"])
	}
	if len(must) != 2 {
		t.Fatalf("expected user and character clauses, got %#v", must)
	}

	assertMatch := func(clause map[string]interface{}, key, value string) {
		if clause["key"] != key {
			t.Errorf("expected clause key %q, got %#v", key, clause["key"])
		}
		match, ok := clause["match"].(map[string]interface{})
		if !ok || match["value"] != value {
			t.Errorf("expected %s to match %q, got %#v", key, value, clause["match"])
		}
	}

	assertMatch(must[0], "user_id", "user-1")
	assertMatch(must[1], "character_id", "character-1")
}

func TestSelectHistoryWithinTokenBudgetKeepsNewestMessages(t *testing.T) {
	messages := []database.Message{
		{Role: "user", Content: "最早的消息"},
		{Role: "assistant", Content: "较早的回复"},
		{Role: "user", Content: "最新的问题"},
		{Role: "assistant", Content: "最新的回复"},
	}
	selected := selectHistoryWithinTokenBudget(messages, 18)
	if len(selected) != 2 {
		t.Fatalf("expected newest two messages within budget, got %d: %+v", len(selected), selected)
	}
	if selected[0].Content != "最新的问题" || selected[1].Content != "最新的回复" {
		t.Fatalf("expected chronological newest messages, got %+v", selected)
	}
}

func TestBuildChatContextIncludesSessionSummaryAndBudgetedHistory(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.Session{
		SessionID: "context-session", UserID: "context-user", CharacterID: "context-char",
		ContextSummary: "用户正在准备重要考试，角色答应持续陪伴。",
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i, content := range []string{"旧消息一", "旧消息二", "当前问题"} {
		if err := db.Create(&database.Message{
			MessageID: string(rune('a' + i)), SessionID: "context-session", UserID: "context-user",
			CharacterID: "context-char", Role: "user", Content: content, MessageType: "text",
		}).Error; err != nil {
			t.Fatalf("create message: %v", err)
		}
	}

	service := &ChatService{DB: db}
	context := service.buildChatContext("context-user", "context-char", 20)
	if !strings.Contains(context, "用户正在准备重要考试") {
		t.Fatalf("expected rolling summary in context: %s", context)
	}
	if !strings.Contains(context, "当前问题") || strings.Contains(context, "旧消息一") {
		t.Fatalf("expected budgeted recent history, got: %s", context)
	}
}

func TestUpdateSessionContextSummaryRollsForwardAndCapsLength(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.Session{
		SessionID: "summary-session", UserID: "summary-user", CharacterID: "summary-char",
		ContextSummary: "之前已经确认用户喜欢夜跑。",
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	service := &ChatService{DB: db}
	if err := service.updateSessionContextSummary("summary-session", "我明天要参加考试", "那今晚早点休息，我会陪你复习。", 80); err != nil {
		t.Fatalf("update summary: %v", err)
	}
	var session database.Session
	if err := db.Where("session_id = ?", "summary-session").First(&session).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if !strings.Contains(session.ContextSummary, "我明天要参加考试") || !strings.Contains(session.ContextSummary, "之前已经确认") {
		t.Fatalf("expected rolled summary, got %q", session.ContextSummary)
	}
	if len([]rune(session.ContextSummary)) > 80 {
		t.Fatalf("expected summary cap, got %d runes", len([]rune(session.ContextSummary)))
	}
}

func TestChatServiceReadsConfiguredContextBudget(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.RuntimeConfig{}); err != nil {
		t.Fatalf("migrate runtime config: %v", err)
	}
	runtimeConfig := &RuntimeConfigService{DB: db}
	if err := runtimeConfig.SeedDefaults(ChatContextRuntimeDefaults()); err != nil {
		t.Fatalf("seed context defaults: %v", err)
	}
	if _, err := runtimeConfig.Upsert(RuntimeConfigChatContextHistoryTokens, "2600", false, ""); err != nil {
		t.Fatalf("override history budget: %v", err)
	}
	service := &ChatService{DB: db, RuntimeConfig: runtimeConfig}
	budget := service.contextBudget()
	if budget.HistoryTokens != 2600 || budget.SummaryTokens != 600 || budget.MemoryTokens != 500 {
		t.Fatalf("unexpected service context budget: %+v", budget)
	}
}

func TestClearChatHistoryResetsConversationStateButKeepsSession(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.Session{
		SessionID: "clear-state-session", UserID: "clear-state-user", CharacterID: "clear-state-char",
		LastMessage: "旧回复", UnreadCount: 3, MessageCount: 8,
		DirectorState: `{"current_situation":"旧场景"}`, ContextSummary: "旧摘要",
		PlotState: PlotStateTension, PlotBeat: 3, PlotThreads: `["未完成约定"]`,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&database.Message{
		MessageID: "clear-state-message", SessionID: "clear-state-session", UserID: "clear-state-user",
		CharacterID: "clear-state-char", Role: "user", Content: "旧消息",
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	service := &ChatService{DB: db}
	if err := service.ClearChatHistory("clear-state-user", "clear-state-char"); err != nil {
		t.Fatalf("clear chat history: %v", err)
	}
	var session database.Session
	if err := db.Where("session_id = ?", "clear-state-session").First(&session).Error; err != nil {
		t.Fatalf("session should be retained: %v", err)
	}
	if session.ContextSummary != "" || session.DirectorState != "" || session.PlotState != PlotStateOpening || session.PlotBeat != 0 || session.PlotThreads != "" || session.MessageCount != 0 || session.LastMessage != "" || session.UnreadCount != 0 {
		t.Fatalf("conversation state was not reset: %+v", session)
	}
	var messageCount int64
	db.Model(&database.Message{}).Where("session_id = ?", "clear-state-session").Count(&messageCount)
	if messageCount != 0 {
		t.Fatalf("expected messages to be deleted, got %d", messageCount)
	}
}

func directorThrottleTestService(t *testing.T, db *gorm.DB) (*ChatService, *int) {
	t.Helper()
	calls := 0
	client := openai_compat.NewClient("http://director.test", "key", "model")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"choices":[{"message":{"content":"{\"current_situation\":\"新一輪的情境\",\"character_emotion\":\"平靜\",\"behavior_action\":\"acknowledge\"}"}}]}`)),
			Request: req,
		}, nil
	})}
	return &ChatService{DB: db, Director: &DirectorService{DB: db, OpenAICompatClient: client}}, &calls
}

func directorThrottleTestSession(t *testing.T, db *gorm.DB, sessionID string, messageCount int, saved *DirectorState, affectionScore int) database.Session {
	t.Helper()
	session := database.Session{
		SessionID:      sessionID,
		UserID:         "throttle-user",
		CharacterID:    "throttle-char",
		MessageCount:   messageCount,
		AffectionScore: affectionScore,
	}
	if saved != nil {
		stateJSON, err := json.Marshal(saved)
		if err != nil {
			t.Fatalf("marshal director state: %v", err)
		}
		session.DirectorState = string(stateJSON)
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	return session
}

func TestRefreshDirectorStateReusesSavedStateBetweenRefreshIntervals(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	saved := &DirectorState{CurrentSituation: "上一輪的情境", RelationshipStage: "初識", AffectionScore: 5}
	session := directorThrottleTestSession(t, db, "throttle-reuse-session", 1, saved, 6)
	service, calls := directorThrottleTestService(t, db)

	state := service.refreshDirectorState("throttle-user", database.Character{Name: "角色"}, session, "throttle-reuse-session", "你好")

	if *calls != 0 {
		t.Fatalf("expected reuse without director model call, got %d calls", *calls)
	}
	if state.CurrentSituation != "上一輪的情境" {
		t.Fatalf("expected saved director state to be reused, got %+v", state)
	}
	if state.AffectionScore != 6 || state.RelationshipStage != "初識" {
		t.Fatalf("expected refreshed affection snapshot on reuse, got %+v", state)
	}
	var reloaded database.Session
	if err := db.Where("session_id = ?", "throttle-reuse-session").First(&reloaded).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if reloaded.MessageCount != 2 {
		t.Fatalf("expected message count to advance on reuse, got %d", reloaded.MessageCount)
	}
}

func TestRefreshDirectorStateRegeneratesOnIntervalBoundary(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	saved := &DirectorState{CurrentSituation: "上一輪的情境", RelationshipStage: "初識", AffectionScore: 5}
	session := directorThrottleTestSession(t, db, "throttle-boundary-session", 2, saved, 6)
	service, calls := directorThrottleTestService(t, db)

	state := service.refreshDirectorState("throttle-user", database.Character{Name: "角色"}, session, "throttle-boundary-session", "你好")

	if *calls != 1 {
		t.Fatalf("expected director model call on interval boundary, got %d calls", *calls)
	}
	if state.CurrentSituation != "新一輪的情境" {
		t.Fatalf("expected regenerated director state, got %+v", state)
	}
}

func TestRefreshDirectorStateRegeneratesWhenRelationshipStageChanges(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	saved := &DirectorState{CurrentSituation: "上一輪的情境", RelationshipStage: "初識", AffectionScore: 5}
	session := directorThrottleTestSession(t, db, "throttle-stage-session", 1, saved, 45)
	service, calls := directorThrottleTestService(t, db)

	state := service.refreshDirectorState("throttle-user", database.Character{Name: "角色"}, session, "throttle-stage-session", "你好")

	if *calls != 1 {
		t.Fatalf("expected director model call when stage changes, got %d calls", *calls)
	}
	if state.RelationshipStage != "心動" {
		t.Fatalf("expected stage to follow current affection, got %+v", state)
	}
}

func TestRefreshDirectorStateGeneratesWhenNoSavedState(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	session := directorThrottleTestSession(t, db, "throttle-first-session", 0, nil, 0)
	service, calls := directorThrottleTestService(t, db)

	state := service.refreshDirectorState("throttle-user", database.Character{Name: "角色"}, session, "throttle-first-session", "你好")

	if *calls != 1 {
		t.Fatalf("expected director model call on first turn, got %d calls", *calls)
	}
	if state == nil || state.CurrentSituation != "新一輪的情境" {
		t.Fatalf("expected generated director state, got %+v", state)
	}
}

func TestRefreshDirectorStateHonorsConfiguredRefreshInterval(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.RuntimeConfig{}); err != nil {
		t.Fatalf("migrate runtime config: %v", err)
	}
	runtimeConfig := &RuntimeConfigService{DB: db}
	if err := runtimeConfig.SeedDefaults(ChatContextRuntimeDefaults()); err != nil {
		t.Fatalf("seed context defaults: %v", err)
	}
	if _, err := runtimeConfig.Upsert(RuntimeConfigChatContextDirectorRefreshInterval, "1", false, ""); err != nil {
		t.Fatalf("override refresh interval: %v", err)
	}
	saved := &DirectorState{CurrentSituation: "上一輪的情境", RelationshipStage: "初識", AffectionScore: 5}
	session := directorThrottleTestSession(t, db, "throttle-interval-session", 1, saved, 6)
	service, calls := directorThrottleTestService(t, db)
	service.RuntimeConfig = runtimeConfig

	state := service.refreshDirectorState("throttle-user", database.Character{Name: "角色"}, session, "throttle-interval-session", "你好")

	if *calls != 1 {
		t.Fatalf("expected interval=1 to regenerate every turn, got %d calls", *calls)
	}
	if state.CurrentSituation != "新一輪的情境" {
		t.Fatalf("expected regenerated director state, got %+v", state)
	}
}
