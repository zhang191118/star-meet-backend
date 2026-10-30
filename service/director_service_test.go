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

func TestGenerateDirectorStateCarriesPreviousStateIntoPrompt(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	previous, _ := json.Marshal(&DirectorState{
		CurrentSituation:   "雨夜的誤會尚未解開",
		CharacterEmotion:   "克制",
		RelationshipStage:  "曖昧",
		NarrativeDirection: "保留一個未說出口的問題",
		SceneSetting:       "車站月台",
		Method:             "先試探再靠近",
	})
	session := database.Session{
		SessionID:      "director-session",
		UserID:         "director-user",
		CharacterID:    "director-character",
		DirectorState:  string(previous),
		AffectionScore: 0,
	}

	var requestPrompt string
	client := openai_compat.NewClient("http://director.test", "key", "model")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &payload)
		if len(payload.Messages) > 1 {
			requestPrompt = payload.Messages[len(payload.Messages)-1].Content
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"current_situation\":\"新的情境\"}"}}]}`)),
			Request:    req,
		}, nil
	})}

	service := &DirectorService{DB: db, OpenAICompatClient: client}
	_, err := service.GenerateDirectorState(database.Character{Name: "角色"}, session, "我到了", "最近的對話")
	if err != nil {
		t.Fatalf("generate director state: %v", err)
	}
	if !strings.Contains(requestPrompt, "[Previous Director State]") || !strings.Contains(requestPrompt, "雨夜的誤會尚未解開") {
		t.Fatalf("director prompt did not carry previous state: %s", requestPrompt)
	}
}

func TestGenerateDirectorStateUsesUserAffectionAsAuthoritativeScore(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.AutoMigrate(&database.UserAffection{}); err != nil {
		t.Fatalf("migrate affection table: %v", err)
	}
	if err := db.Create(&database.UserAffection{
		UserID: "affection-user", CharacterID: "affection-character", Exp: 42, Level: 1,
	}).Error; err != nil {
		t.Fatalf("create affection: %v", err)
	}

	var requestPrompt string
	client := openai_compat.NewClient("http://director.test", "key", "model")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &payload)
		if len(payload.Messages) > 1 {
			requestPrompt = payload.Messages[len(payload.Messages)-1].Content
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"current_situation\":\"情境\",\"relationship_stage\":\"熱戀\"}"}}]}`)),
			Request:    req,
		}, nil
	})}

	service := &DirectorService{DB: db, OpenAICompatClient: client}
	state, err := service.GenerateDirectorState(database.Character{Name: "角色"}, database.Session{
		SessionID: "affection-session", UserID: "affection-user", CharacterID: "affection-character",
		AffectionScore: 999, RelationshipStage: "靈魂伴侶",
	}, "你好", "")
	if err != nil {
		t.Fatalf("generate director state: %v", err)
	}
	if state.AffectionScore != 42 || state.RelationshipStage != "心動" {
		t.Fatalf("expected affection-derived stage and authoritative score, got %+v", state)
	}
	if !strings.Contains(requestPrompt, "[Stage] 心動 (Affection: 42)") {
		t.Fatalf("director prompt used session affection instead of UserAffection: %s", requestPrompt)
	}
}

func TestGenerateDirectorStateAdvancesPersistedPlotFromTrigger(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	client := openai_compat.NewClient("http://director.test", "key", "model")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"current_situation\":\"矛盾浮现\",\"plot_trigger\":\"conflict\",\"plot_thread\":\"未解释的离开原因\"}"}}]}`)),
			Request:    req,
		}, nil
	})}
	service := &DirectorService{DB: db, OpenAICompatClient: client}
	state, err := service.GenerateDirectorState(database.Character{Name: "角色"}, database.Session{
		SessionID: "plot-director-session", UserID: "plot-user", CharacterID: "plot-char",
		PlotState: PlotStateAmbiguous, PlotBeat: 2,
	}, "你为什么瞒着我", "")
	if err != nil {
		t.Fatalf("generate director state: %v", err)
	}
	if state.PlotState != PlotStateTension || state.PlotBeat != 3 || state.PlotTrigger != PlotTriggerConflict {
		t.Fatalf("expected plot transition, got %+v", state)
	}
}

func TestGenerateDirectorStateNormalizesBehaviorAction(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	client := openai_compat.NewClient("http://director.test", "key", "model")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"current_situation\":\"用户很难过\",\"behavior_action\":\"做一个客服式总结\"}"}}]}`)),
			Request:    req,
		}, nil
	})}
	service := &DirectorService{DB: db, OpenAICompatClient: client}
	state, err := service.GenerateDirectorState(database.Character{Name: "角色"}, database.Session{
		UserID: "behavior-user", CharacterID: "behavior-char", AffectionScore: 20,
	}, "我今天很难过", "")
	if err != nil {
		t.Fatalf("generate director state: %v", err)
	}
	if state.BehaviorAction != BehaviorActionComfort {
		t.Fatalf("expected invalid model action to use emotion-aware fallback, got %+v", state)
	}
}

func TestBuildDirectorPromptIncludesPreviousBehavior(t *testing.T) {
	prompt := buildDirectorPrompt(database.Character{Name: "角色"}, "初識", 10, "你好", "", &DirectorState{
		BehaviorAction: BehaviorActionInvite,
	})
	if !strings.Contains(prompt, BehaviorActionInvite) {
		t.Fatalf("director prompt omitted previous behavior action: %s", prompt)
	}
}

func TestGenerateDirectorStateCarriesPersistedCharacterEmotion(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	client := openai_compat.NewClient("http://director.test", "key", "model")
	client.Client = &http.Client{Transport: chatRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"current_situation\":\"對話繼續\",\"character_emotion\":\"平靜\",\"behavior_action\":\"acknowledge\"}"}}]}`)),
			Request:    req,
		}, nil
	})}
	service := &DirectorService{DB: db, OpenAICompatClient: client}
	state, err := service.GenerateDirectorState(database.Character{Name: "角色"}, database.Session{
		UserID: "emotion-user", CharacterID: "emotion-char", CharacterEmotion: "生氣", EmotionIntensity: 3,
	}, "嗯嗯", "")
	if err != nil {
		t.Fatalf("generate director state: %v", err)
	}
	if state.CharacterEmotion != "生氣" || state.EmotionIntensity != 2 {
		t.Fatalf("expected persisted emotion inertia, got %+v", state)
	}
}

func TestGenerateDirectorStateFallbackInitializesEmotion(t *testing.T) {
	service := &DirectorService{}
	state, err := service.GenerateDirectorState(database.Character{Name: "角色"}, database.Session{}, "我今天很开心", "")
	if err != nil {
		t.Fatalf("generate fallback director state: %v", err)
	}
	if state.CharacterEmotion != "开心" || state.EmotionIntensity < 2 {
		t.Fatalf("expected fallback emotion to be initialized, got %+v", state)
	}
}
