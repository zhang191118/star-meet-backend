package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"star-meet-backend/core/embedding"
	"star-meet-backend/core/mem0"
	"star-meet-backend/core/qdrant"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"
)

var ErrInvalidMemory = errors.New("memory content must contain 1–2000 characters and a valid type")
var ErrMemorySuperseded = errors.New("memory extraction superseded by user control")
var memoryUserLocks [64]sync.Mutex

func lockMemoryUser(userID string) func() {
	var bucket uint32
	for _, r := range userID {
		bucket = bucket*31 + uint32(r)
	}
	mu := &memoryUserLocks[bucket%uint32(len(memoryUserLocks))]
	mu.Lock()
	return mu.Unlock
}

type MemoryService struct {
	DB              *gorm.DB
	Mem0Client      *mem0.Client
	QdrantClient    *qdrant.Client
	EmbeddingClient *embedding.Client
}

func (s *MemoryService) hasExternalIndex() bool { return s.Mem0Client != nil || s.QdrantClient != nil }
func memoryResponse(mem database.Memory) response.MemoryResp {
	state := "synced"
	if mem.IndexedRevision < mem.Revision {
		state = "pending"
	}
	return response.MemoryResp{MemoryID: mem.MemoryID, UserID: mem.UserID, CharacterID: mem.CharacterID,
		Content: mem.Content, MemoryType: mem.MemoryType, Source: mem.Source, Importance: mem.Importance,
		Revision: mem.Revision, SyncStatus: state, CreatedAt: mem.CreatedAt.Unix(), UpdatedAt: mem.UpdatedAt.Unix()}
}
func (s *MemoryService) GetMemoryList(req request.GetMemoryListReq) ([]response.MemoryResp, int64, error) {
	resolvedCharacterID, err := s.resolveMemoryCharacterID(req.UserID, req.CharacterID)
	if err != nil {
		return nil, 0, err
	}
	req.CharacterID = resolvedCharacterID
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 {
		req.PageSize = 20
	}
	req.PageSize = clampPageSize(req.PageSize)
	query := s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ?", req.UserID, req.CharacterID)
	if req.MemoryType != "" && req.MemoryType != "all" {
		query = query.Where("memory_type = ?", req.MemoryType)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []database.Memory
	if err := query.Order("created_at DESC, id DESC").Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]response.MemoryResp, 0, len(rows))
	for _, row := range rows {
		resp := memoryResponse(row)
		if !s.hasExternalIndex() {
			resp.SyncStatus = "not_required"
		}
		result = append(result, resp)
	}
	return result, total, nil
}
func (s *MemoryService) AddMemory(req request.AddMemoryReq) (*response.MemoryResp, error) {
	resolvedCharacterID, err := s.resolveMemoryCharacterID(req.UserID, req.CharacterID)
	if err != nil {
		return nil, err
	}
	req.CharacterID = resolvedCharacterID
	req.Content = strings.TrimSpace(req.Content)
	if req.MemoryType == "" {
		req.MemoryType = MemoryTypeFact
	}
	if req.Source == "" {
		req.Source = "manual"
	}
	if utf8.RuneCountInString(req.Content) < 1 || utf8.RuneCountInString(req.Content) > 2000 {
		return nil, ErrInvalidMemory
	}
	if _, ok := structuredMemoryTypes[req.MemoryType]; !ok {
		return nil, ErrInvalidMemory
	}
	if req.Source != "auto" && req.Source != "manual" {
		return nil, ErrInvalidMemory
	}
	if req.Importance < 1 || req.Importance > 5 {
		req.Importance = 3
	}
	row := database.Memory{MemoryID: uuid.NewString(), UserID: req.UserID, CharacterID: req.CharacterID, Content: req.Content,
		MemoryType: req.MemoryType, Source: req.Source, Importance: req.Importance, Revision: 1}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// Serialize user-owned writes with account deletion using a short database row lock.
		if err := requireExistingMemoryUser(tx, req.UserID); err != nil {
			return err
		}

		if req.Source == "auto" && !req.ObservedAt.IsZero() {
			var controls int64
			if err := tx.Unscoped().Model(&database.Memory{}).Where("user_id = ? AND character_id = ? AND updated_at >= ? AND (deleted_at IS NOT NULL OR source = ?)", req.UserID, req.CharacterID, req.ObservedAt, "manual").Count(&controls).Error; err != nil {
				return err
			}
			if controls > 0 {
				return ErrMemorySuperseded
			}
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return s.queueIndex(tx, row, "")
	})
	if err != nil {
		return nil, err
	}
	resp := memoryResponse(row)
	if !s.hasExternalIndex() {
		resp.SyncStatus = "not_required"
	}
	return &resp, nil
}
func (s *MemoryService) queueIndex(tx *gorm.DB, row database.Memory, cleanupCharacterID string) error {
	if !s.hasExternalIndex() {
		// Preserve an existing cleanup job when a provider is temporarily disabled.
		if !tx.Migrator().HasTable(&database.MemoryIndexJob{}) {
			return nil
		}
		updates := map[string]interface{}{"revision": row.Revision, "character_id": row.CharacterID, "next_attempt_at": time.Now()}
		if cleanupCharacterID != "" {
			updates["cleanup_character_id"] = cleanupCharacterID
		}
		return tx.Model(&database.MemoryIndexJob{}).Where("memory_id = ?", row.MemoryID).Updates(updates).Error
	}
	job := database.MemoryIndexJob{MemoryID: row.MemoryID, UserID: row.UserID, CharacterID: row.CharacterID,
		Revision: row.Revision, CleanupCharacterID: cleanupCharacterID, NextAttemptAt: time.Now()}
	columns := []string{"user_id", "character_id", "revision", "attempts", "last_error", "next_attempt_at", "updated_at"}
	if cleanupCharacterID != "" {
		columns = append(columns, "cleanup_character_id")
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "memory_id"}}, DoUpdates: clause.AssignmentColumns(columns)}).Create(&job).Error
}
func (s *MemoryService) UpdateMemory(memoryID, userID, content string) error {
	content = strings.TrimSpace(content)
	if utf8.RuneCountInString(content) < 1 || utf8.RuneCountInString(content) > 2000 {
		return ErrInvalidMemory
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireExistingMemoryUser(tx, userID); err != nil {
			return err
		}
		var row database.Memory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("memory_id = ? AND user_id = ?", memoryID, userID).First(&row).Error; err != nil {
			return err
		}
		row.Content = content
		row.Source = "manual"
		row.Revision++
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return s.queueIndex(tx, row, "")
	})
}
func (s *MemoryService) DeleteMemory(memoryID, userID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireExistingMemoryUser(tx, userID); err != nil {
			return err
		}
		var row database.Memory
		if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("memory_id = ? AND user_id = ?", memoryID, userID).First(&row).Error; err != nil {
			return err
		}
		if row.DeletedAt.Valid {
			return s.queueIndex(tx, row, "")
		}
		row.Revision++
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		if !s.hasExternalIndex() {
			if err := tx.Unscoped().Model(&row).Update("content", "").Error; err != nil {
				return err
			}
		}
		return s.queueIndex(tx, row, "")
	})
}
func (s *MemoryService) SearchMemory(req request.SearchMemoryReq) ([]response.MemoryResp, error) {
	resolvedCharacterID, err := s.resolveMemoryCharacterID(req.UserID, req.CharacterID)
	if err != nil {
		return nil, err
	}
	req.CharacterID = resolvedCharacterID
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	rows, err := s.Retrieve(ctx, req.UserID, req.CharacterID, req.Query, 20)
	if err != nil {
		return nil, err
	}
	result := make([]response.MemoryResp, 0, len(rows))
	for _, row := range rows {
		resp := memoryResponse(row)
		if !s.hasExternalIndex() {
			resp.SyncStatus = "not_required"
		}
		result = append(result, resp)
	}
	return result, nil
}

// Retrieve uses current SQL rows only. Unknown, stale, foreign, or deleted external records cannot supply text.
func (s *MemoryService) Retrieve(ctx context.Context, userID, characterID, query string, limit int) ([]database.Memory, error) {
	var rows []database.Memory
	if err := s.DB.WithContext(ctx).Where("user_id = ? AND character_id = ?", userID, characterID).
		Order("source = 'manual' DESC, importance DESC, updated_at DESC, id DESC").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	candidates := map[string]database.Memory{}
	semantic := map[string]float64{}
	for _, row := range rows {
		candidates[row.MemoryID] = row
	}
	if s.QdrantClient != nil && s.EmbeddingClient != nil && ctx.Err() == nil {
		if vector, err := s.EmbeddingClient.GetEmbeddingContext(ctx, query); err == nil {
			if points, err := s.QdrantClient.SearchContext(ctx, "memories", vector, 10, memorySearchFilter(userID, characterID)); err == nil {
				ids := []string{}
				for _, point := range points {
					if id, ok := point.Payload["memory_id"].(string); ok && id != "" {
						ids = append(ids, id)
						semantic[id] = point.Score
					}
				}
				if len(ids) > 0 {
					var matched []database.Memory
					if err := s.DB.WithContext(ctx).Where("user_id = ? AND character_id = ? AND memory_id IN ?", userID, characterID, ids).Find(&matched).Error; err == nil {
						for _, row := range matched {
							candidates[row.MemoryID] = row
						}
					}
				}
			}
		}
	}
	// Mem0's legacy Search response does not carry a trusted canonical ID; map tracked external IDs through SQL.
	if s.Mem0Client != nil && s.QdrantClient == nil && ctx.Err() == nil {
		if results, err := s.Mem0Client.SearchContext(ctx, "user_"+userID, "char_"+characterID, query, 10); err == nil {
			ids := []string{}
			for _, point := range results {
				ids = append(ids, point.ID)
			}
			if len(ids) > 0 {
				var matched []database.Memory
				if err := s.DB.WithContext(ctx).Where("user_id = ? AND character_id = ? AND mem0_id IN ?", userID, characterID, ids).Find(&matched).Error; err == nil {
					for _, row := range matched {
						candidates[row.MemoryID] = row
						semantic[row.MemoryID] = 1
					}
				}
			}
		}
	}
	tokens := memoryQueryTokens(query)
	score := func(row database.Memory) float64 {
		value := float64(row.Importance)
		if row.Source == "manual" {
			value += 2
		}
		text := i18n.NormalizeDetectionText(row.Content)
		for _, token := range tokens {
			if strings.Contains(text, token) {
				value += 4
			}
		}
		return value + semantic[row.MemoryID]*8
	}
	result := make([]database.Memory, 0, len(candidates))
	for _, row := range candidates {
		result = append(result, row)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := score(result[i]), score(result[j])
		if a != b {
			return a > b
		}
		if !result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].UpdatedAt.After(result[j].UpdatedAt)
		}
		return result[i].ID > result[j].ID
	})
	if limit < 1 {
		limit = 5
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func memoryQueryTokens(text string) []string {
	text = i18n.NormalizeDetectionText(text)
	seen := map[string]bool{}
	tokens := []string{}
	add := func(s string) {
		if utf8.RuneCountInString(s) > 1 && !seen[s] {
			seen[s] = true
			tokens = append(tokens, s)
		}
	}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		runes := []rune(word)
		add(word)
		for i := 1; i < len(runes); i++ {
			if unicode.Is(unicode.Han, runes[i-1]) && unicode.Is(unicode.Han, runes[i]) {
				add(string(runes[i-1 : i+1]))
			}
		}
	}
	return tokens
}
func (s *MemoryService) GetMemoryStats(userID, characterID string) (*response.MemoryStatsResp, error) {
	resolvedCharacterID, err := s.resolveMemoryCharacterID(userID, characterID)
	if err != nil {
		return nil, err
	}
	characterID = resolvedCharacterID
	var rows []database.Memory
	if err := s.DB.Select("memory_type", "created_at").Where("user_id = ? AND character_id = ?", userID, characterID).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := &response.MemoryStatsResp{TotalCount: len(rows)}
	today := time.Now().In(chatQuotaLocation("Asia/Taipei")).Format("2006-01-02")
	for _, row := range rows {
		if row.CreatedAt.In(chatQuotaLocation("Asia/Taipei")).Format("2006-01-02") == today {
			result.TodayCount++
		}
		switch row.MemoryType {
		case "preference":
			result.PreferenceCnt++
		case "emotion":
			result.EmotionCnt++
		case "event":
			result.EventCnt++
		case "relationship":
			result.RelationshipCnt++
		}
	}
	return result, nil
}

func (s *MemoryService) resolveMemoryCharacterID(userID, characterID string) (string, error) {
	if !s.DB.Migrator().HasTable(&database.Character{}) {
		return characterID, nil
	}
	resolvedID, err := canonicalCharacterIDForUser(s.DB, userID, characterID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Memory-only deployments and historical rows can outlive a character
		// record. Preserve that scope; user_id still provides tenant isolation.
		return characterID, nil
	}
	return resolvedID, err
}

func requireExistingMemoryUser(tx *gorm.DB, userID string) error {
	if !tx.Migrator().HasTable(&database.User{}) {
		return nil
	}
	var user database.User
	return tx.Select("user_id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&user).Error
}
