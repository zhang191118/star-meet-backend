package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"star-meet-backend/core/emotion"
	"star-meet-backend/model/database"
	"star-meet-backend/pkg/logger"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	openai_compat "star-meet-backend/core/openai_compat"
)

type DirectorState struct {
	CurrentSituation   string   `json:"current_situation"`
	CharacterEmotion   string   `json:"character_emotion"`
	RelationshipStage  string   `json:"relationship_stage"`
	NarrativeDirection string   `json:"narrative_direction"`
	SceneSetting       string   `json:"scene_setting"`
	Method             string   `json:"method"`
	AffectionScore     int      `json:"affection_score"`
	BehaviorAction     string   `json:"behavior_action,omitempty"`
	EmotionIntensity   int      `json:"emotion_intensity,omitempty"`
	PlotState          string   `json:"plot_state,omitempty"`
	PlotBeat           int      `json:"plot_beat,omitempty"`
	PlotTrigger        string   `json:"plot_trigger,omitempty"`
	PlotThread         string   `json:"plot_thread,omitempty"`
	PlotThreads        []string `json:"plot_threads,omitempty"`
}

type DirectorService struct {
	DB                 *gorm.DB
	Redis              *redis.Client
	OpenAICompatClient *openai_compat.Client
}

func relationshipStageFromScore(score int) string {
	switch {
	case score <= 20:
		return "初識"
	case score <= 40:
		return "曖昧"
	case score <= 60:
		return "心動"
	case score <= 80:
		return "熱戀"
	default:
		return "靈魂伴侶"
	}
}

func (d *DirectorService) GenerateDirectorState(
	character database.Character,
	session database.Session,
	userMessage string,
	historySummary string,
) (*DirectorState, error) {
	affectionScore := d.authoritativeAffectionScore(session)
	// Relationship stage is derived from the same authoritative score. The
	// session fields remain a denormalized snapshot for clients and migrations.
	stage := relationshipStageFromScore(affectionScore)
	previousState := parseDirectorState(session.DirectorState)
	if previousState == nil {
		previousState = &DirectorState{
			CharacterEmotion: session.CharacterEmotion,
			EmotionIntensity: session.EmotionIntensity,
		}
	}
	plotSnapshot := plotSnapshotFromSession(session)

	directorPrompt := buildDirectorPrompt(character, stage, affectionScore, userMessage, historySummary, previousState)

	var rawReply string
	var err error
	if d.OpenAICompatClient != nil {
		directorBudget := DefaultChatContextBudget().DirectorOutputTokens
		if d.DB != nil {
			directorBudget = (&RuntimeConfigService{DB: d.DB}).ChatContextBudget().DirectorOutputTokens
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		rawReply, err = d.OpenAICompatClient.GenerateContext(ctx, directorPrompt, directorSystemPrompt(), directorBudget)
		cancel()
	}
	if err != nil || strings.TrimSpace(rawReply) == "" {
		logger.Warn("director fallback", "error", err)
		return fallbackDirectorState(session, stage, affectionScore, userMessage, plotSnapshot), nil
	}

	cleaned := extractJSONBlock(rawReply)
	var state DirectorState
	if err := json.Unmarshal([]byte(cleaned), &state); err != nil {
		logger.Warn("director json parse fail", "error", err)
		return fallbackDirectorState(session, stage, affectionScore, userMessage, plotSnapshot), nil
	}
	state.RelationshipStage = stage
	state.AffectionScore = affectionScore
	state.BehaviorAction = normalizeBehaviorAction(state.BehaviorAction, emotion.Detect(userMessage), state.CharacterEmotion, plotSnapshot.State)
	state.CharacterEmotion, state.EmotionIntensity = evolveCharacterEmotion(session.CharacterEmotion, session.EmotionIntensity, state.CharacterEmotion, userMessage, state.BehaviorAction)
	plotSnapshot = advancePlotSnapshot(plotSnapshot, state.PlotTrigger, state.PlotThread)
	state.PlotState = plotSnapshot.State
	state.PlotBeat = plotSnapshot.Beat
	state.PlotThreads = plotSnapshot.OpenThreads
	return &state, nil
}

func fallbackDirectorState(session database.Session, stage string, affectionScore int, userMessage string, plotSnapshot PlotSnapshot) *DirectorState {
	action := normalizeBehaviorAction("", emotion.Detect(userMessage), session.CharacterEmotion, plotSnapshot.State)
	characterEmotion, emotionIntensity := evolveCharacterEmotion(session.CharacterEmotion, session.EmotionIntensity, "", userMessage, action)
	return &DirectorState{
		CurrentSituation:   "對話進行中",
		CharacterEmotion:   characterEmotion,
		EmotionIntensity:   emotionIntensity,
		RelationshipStage:  stage,
		NarrativeDirection: "推進對話",
		SceneSetting:       "",
		Method:             "按角色風格回應",
		AffectionScore:     affectionScore,
		BehaviorAction:     action,
		PlotState:          plotSnapshot.State,
		PlotBeat:           plotSnapshot.Beat,
		PlotThreads:        plotSnapshot.OpenThreads,
	}
}

// authoritativeAffectionScore reads the per-user/per-character affection
// record. Session.AffectionScore is only a compatibility fallback for older
// sessions that predate user_affections.
func (d *DirectorService) authoritativeAffectionScore(session database.Session) int {
	if d != nil && d.DB != nil && session.UserID != "" && session.CharacterID != "" {
		if !d.DB.Migrator().HasTable(&database.UserAffection{}) {
			return session.AffectionScore
		}
		var affection database.UserAffection
		if err := d.DB.Where("user_id = ? AND character_id = ?", session.UserID, session.CharacterID).First(&affection).Error; err == nil {
			return affection.Exp
		}
	}
	return session.AffectionScore
}

func parseDirectorState(raw string) *DirectorState {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var state DirectorState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil
	}
	return &state
}

// DirectorNoteForClient 生成客户端展示的导演批注（"当前形势分析"卡片正文）。
// 只保留对用户有叙事价值的行；无内容时返回空串。
func DirectorNoteForClient(state *DirectorState) string {
	if state == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if s := strings.TrimSpace(state.CurrentSituation); s != "" {
		parts = append(parts, "— "+s)
	}
	if s := strings.TrimSpace(state.SceneSetting); s != "" && s != state.CurrentSituation {
		parts = append(parts, "场景："+s)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func (d *DirectorService) SaveDirectorState(sessionID string, state *DirectorState) error {
	if state == nil {
		return nil
	}
	stateJSON, _ := json.Marshal(state)
	plotThreads, _ := json.Marshal(state.PlotThreads)
	return d.DB.Model(&database.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]interface{}{
			"director_state":     string(stateJSON),
			"relationship_stage": state.RelationshipStage,
			"affection_score":    state.AffectionScore,
			"plot_state":         state.PlotState,
			"plot_beat":          state.PlotBeat,
			"plot_threads":       string(plotThreads),
			"character_emotion":  state.CharacterEmotion,
			"emotion_intensity":  state.EmotionIntensity,
		}).Error
}

func (d *DirectorService) IncrementMessageCount(sessionID string) error {
	return d.DB.Model(&database.Session{}).
		Where("session_id = ?", sessionID).
		UpdateColumn("message_count", gorm.Expr("message_count + 1")).Error
}

func buildDirectorPrompt(
	character database.Character,
	stage string,
	affectionScore int,
	userMessage string,
	historySummary string,
	previousState *DirectorState,
) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[Character] %s\n", character.Name))
	b.WriteString(fmt.Sprintf("[Age] %d\n", character.Age))
	b.WriteString(fmt.Sprintf("[Job] %s\n", character.Occupation))
	if character.Personality != "" {
		b.WriteString(fmt.Sprintf("[Personality] %s\n", character.Personality))
	}
	if character.Example != "" {
		b.WriteString(fmt.Sprintf("[Speech Style] %s\n", character.Example))
	}
	if character.Narrative != "" {
		b.WriteString(fmt.Sprintf("[Story] %s\n", character.Narrative))
	}
	if character.BasicSetting != "" {
		b.WriteString(fmt.Sprintf("[Bio] %s\n", character.BasicSetting))
	}
	b.WriteString(fmt.Sprintf("[Stage] %s (Affection: %d)\n", stage, affectionScore))
	if historySummary != "" {
		b.WriteString(fmt.Sprintf("[History] %s\n", historySummary))
	}
	if previousState != nil {
		if encoded, err := json.Marshal(previousState); err == nil {
			b.WriteString(fmt.Sprintf("[Previous Director State] %s\n", encoded))
		}
	}
	if previousState != nil && previousState.PlotState != "" {
		b.WriteString(fmt.Sprintf("[Plot State] %s (Beat: %d)\n", previousState.PlotState, previousState.PlotBeat))
	}
	if previousState != nil && previousState.BehaviorAction != "" {
		b.WriteString(fmt.Sprintf("[Previous Behavior] %s\n", previousState.BehaviorAction))
	}
	if previousState != nil && previousState.CharacterEmotion != "" {
		b.WriteString(fmt.Sprintf("[Previous Character Emotion] %s (Intensity: %d)\n", previousState.CharacterEmotion, previousState.EmotionIntensity))
	}
	b.WriteString(fmt.Sprintf("[User Message] %s\n", userMessage))
	return b.String()
}

func directorSystemPrompt() string {
	return "你是恋愛物語導演。只輸出單一合法 JSON 物件，不要 markdown、不要程式碼圍欄、不要任何解釋文字。" +
		`必須形狀：{"current_situation":"...","character_emotion":"...","relationship_stage":"...","narrative_direction":"...","scene_setting":"...","method":"...","behavior_action":"acknowledge|comfort|explain|ask|playful|invite|vulnerable|repair|pause","plot_trigger":"interaction|disclosure|conflict|reconciliation|promise|pause","plot_thread":"..."}。` +
		"所有欄位值必須使用繁體中文。relationship_stage 必須從以下固定詞彙中選一個：初識、曖昧、心動、熱戀、靈魂伴侶。" +
		"behavior_action 必須从允许列表选择；不要每轮默认 comfort，必须根据用户情绪和上一轮状态决定。每个值最多两句短话。"
}

// extractJSONBlock strips markdown fences and surrounding prose so that
// lenient models still yield a parseable JSON object.
func extractJSONBlock(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```JSON")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(text[start : end+1])
	}
	return strings.TrimSpace(text)
}
