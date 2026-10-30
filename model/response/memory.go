package response

// 记忆详情
type MemoryResp struct {
	MemoryID    string `json:"memory_id"`
	UserID      string `json:"user_id"`
	CharacterID string `json:"character_id"`
	Content     string `json:"content"`
	MemoryType  string `json:"memory_type"`
	Source      string `json:"source"` // manual/auto
	Revision    uint64 `json:"revision"`
	SyncStatus  string `json:"sync_status"`
	Importance  int    `json:"importance"` // 1-5
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// 记忆统计
type MemoryStatsResp struct {
	TotalCount      int `json:"total_count"`
	TodayCount      int `json:"today_count"`
	PreferenceCnt   int `json:"preference_cnt"`
	EmotionCnt      int `json:"emotion_cnt"`
	EventCnt        int `json:"event_cnt"`
	RelationshipCnt int `json:"relationship_cnt"`
}
