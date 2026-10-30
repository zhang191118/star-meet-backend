package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"star-meet-backend/core/qdrant"
	"star-meet-backend/model/database"
	"star-meet-backend/pkg/logger"
)

// RunIndexWorker uses bounded requests; ordinary memory writes never wait for external providers.
func (s *MemoryService) RunIndexWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.hasExternalIndex() {
				continue
			}
			if err := s.BackfillIndexJobs(ctx, 50); err != nil {
				logger.Warn("memory index backfill deferred")
				continue
			}
			for n := 0; n < 5; n++ {
				processed, err := s.ProcessNextIndexJob(ctx)
				if err != nil {
					logger.Warn("memory index retry scheduled")
					break
				}
				if !processed {
					break
				}
			}
		}
	}
}
func (s *MemoryService) ProcessNextIndexJob(ctx context.Context) (bool, error) {
	now := time.Now()
	var job database.MemoryIndexJob
	err := s.DB.WithContext(ctx).Where("next_attempt_at <= ? AND (lease_until IS NULL OR lease_until < ?)", now, now).
		Order("next_attempt_at ASC, memory_id ASC").First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	token := uuid.NewString()
	until := now.Add(30 * time.Second)
	claim := s.DB.Model(&database.MemoryIndexJob{}).Where("memory_id = ? AND revision = ? AND (lease_until IS NULL OR lease_until < ?)", job.MemoryID, job.Revision, now).
		Updates(map[string]interface{}{"lease_token": token, "lease_until": until})
	if claim.Error != nil {
		return false, claim.Error
	}
	if claim.RowsAffected == 0 {
		return true, nil
	}
	unlock := lockMemoryUser(job.UserID)
	defer unlock()
	renewed := s.DB.Model(&database.MemoryIndexJob{}).Where("memory_id = ? AND lease_token = ?", job.MemoryID, token).Update("lease_until", time.Now().Add(30*time.Second))
	if renewed.Error != nil {
		return true, renewed.Error
	}
	if renewed.RowsAffected == 0 {
		return true, nil
	}
	var row database.Memory
	err = s.DB.Unscoped().Where("memory_id = ? AND user_id = ?", job.MemoryID, job.UserID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, s.DB.Where("memory_id = ? AND lease_token = ?", job.MemoryID, token).Delete(&database.MemoryIndexJob{}).Error
	}
	if err == nil && s.DB.Migrator().HasTable(&database.User{}) {
		var users int64
		if countErr := s.DB.Model(&database.User{}).Where("user_id = ?", job.UserID).Count(&users).Error; countErr != nil {
			return true, countErr
		}
		if users == 0 {
			return true, s.DB.Where("memory_id = ? AND lease_token = ?", job.MemoryID, token).Delete(&database.MemoryIndexJob{}).Error
		}
	}
	if err == nil && row.Revision != job.Revision {
		if row.IndexedRevision >= row.Revision {
			return true, s.DB.Where("memory_id = ? AND revision = ? AND lease_token = ?", job.MemoryID, job.Revision, token).Delete(&database.MemoryIndexJob{}).Error
		}
		updates := map[string]interface{}{"revision": row.Revision, "character_id": row.CharacterID, "attempts": 0, "next_attempt_at": time.Now()}
		if job.CharacterID != row.CharacterID && job.CleanupCharacterID == "" {
			updates["cleanup_character_id"] = job.CharacterID
		}
		if rebaseErr := s.DB.Model(&database.MemoryIndexJob{}).Where("memory_id = ? AND revision = ? AND lease_token = ?", job.MemoryID, job.Revision, token).Updates(updates).Error; rebaseErr != nil {
			return true, rebaseErr
		}
		return true, s.DB.Model(&database.MemoryIndexJob{}).Where("memory_id = ? AND lease_token = ?", job.MemoryID, token).Updates(map[string]interface{}{"lease_until": nil, "lease_token": ""}).Error
	}

	if err == nil {
		opCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = s.syncMemoryIndex(opCtx, &row, job.CleanupCharacterID)
		cancel()
	}
	if err != nil {
		attempts := job.Attempts + 1
		power := attempts
		if power > 12 {
			power = 12
		}
		next := time.Now().Add(time.Duration(1<<power) * time.Second)
		failed := s.DB.Model(&database.MemoryIndexJob{}).Where("memory_id = ? AND revision = ? AND lease_token = ?", job.MemoryID, job.Revision, token).
			Updates(map[string]interface{}{"attempts": attempts, "next_attempt_at": next, "lease_until": nil, "lease_token": "", "last_error": "index_sync_failed"})
		if failed.Error != nil {
			return true, failed.Error
		}
		if failed.RowsAffected == 0 {
			if releaseErr := s.DB.Model(&database.MemoryIndexJob{}).Where("memory_id = ? AND lease_token = ?", job.MemoryID, token).
				Updates(map[string]interface{}{"lease_until": nil, "lease_token": ""}).Error; releaseErr != nil {
				return true, releaseErr
			}
		}

		return true, err
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{"indexed_revision": row.Revision, "mem0_id": row.Mem0ID}
		if row.DeletedAt.Valid {
			updates["content"] = ""
			updates["mem0_id"] = ""
		}
		if err := tx.Unscoped().Model(&database.Memory{}).Where("memory_id = ? AND revision = ?", row.MemoryID, row.Revision).Updates(updates).Error; err != nil {
			return err
		}
		done := tx.Where("memory_id = ? AND revision = ? AND lease_token = ?", job.MemoryID, job.Revision, token).Delete(&database.MemoryIndexJob{})
		if done.Error != nil {
			return done.Error
		}
		if done.RowsAffected == 0 {
			return tx.Model(&database.MemoryIndexJob{}).Where("memory_id = ? AND lease_token = ?", job.MemoryID, token).Updates(map[string]interface{}{"lease_until": nil, "lease_token": ""}).Error
		}
		return nil
	})
	return true, err
}
func indexPointID(userID, memoryID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(userID+"\x00"+memoryID)).String()
}
func (s *MemoryService) syncMemoryIndex(ctx context.Context, row *database.Memory, cleanupCharacterID string) error {
	// Drop old unlinked vectors in this namespace; new canonical vectors are explicitly tagged.
	scopes := []string{row.CharacterID}
	if cleanupCharacterID != "" && cleanupCharacterID != row.CharacterID {
		scopes = append(scopes, cleanupCharacterID)
	}
	for _, characterID := range scopes {
		if s.QdrantClient != nil {
			filter := memorySearchFilter(row.UserID, characterID)
			filter["must_not"] = []map[string]interface{}{{"key": "canonical_schema", "match": map[string]interface{}{"value": 1}}}
			if err := s.QdrantClient.DeleteByFilterContext(ctx, "memories", filter); err != nil {
				return err
			}
		}
		if s.Mem0Client != nil {
			// Legacy auto memories have no canonical SQL ID; they are never served by Retrieve.
			legacyIDs := []string{}
			for offset := 0; offset < 10000; offset += 100 {
				memories, err := s.Mem0Client.GetAllContext(ctx, "user_"+row.UserID, "char_"+characterID, 100, offset)
				if err != nil {
					return err
				}
				for _, item := range memories {
					if item.ID != "" {
						if id, _ := item.Metadata["memory_id"].(string); id == "" || id == row.MemoryID {
							legacyIDs = append(legacyIDs, item.ID)
						}
					}
				}
				if len(memories) < 100 {
					break
				}
				if offset == 9900 {
					return fmt.Errorf("legacy memory cleanup requires another pass")
				}
			}
			for _, id := range legacyIDs {
				if err := s.Mem0Client.DeleteContext(ctx, id); err != nil {
					return err
				}
			}
		}
	}
	if s.QdrantClient != nil {
		if cleanupCharacterID != "" && cleanupCharacterID != row.CharacterID {
			filter := memorySearchFilter(row.UserID, cleanupCharacterID)
			must := filter["must"].([]map[string]interface{})
			filter["must"] = append(must, map[string]interface{}{"key": "memory_id", "match": map[string]interface{}{"value": row.MemoryID}})
			if err := s.QdrantClient.DeleteByFilterContext(ctx, "memories", filter); err != nil {
				return err
			}
		}
		if row.DeletedAt.Valid {
			filter := memorySearchFilter(row.UserID, row.CharacterID)
			must := filter["must"].([]map[string]interface{})
			filter["must"] = append(must, map[string]interface{}{"key": "memory_id", "match": map[string]interface{}{"value": row.MemoryID}})
			if err := s.QdrantClient.DeleteByFilterContext(ctx, "memories", filter); err != nil {
				return err
			}
		} else {
			if s.EmbeddingClient == nil {
				return errors.New("embedding unavailable")
			}
			vector, err := s.EmbeddingClient.GetEmbeddingContext(ctx, row.Content)
			if err != nil {
				return err
			}
			if err := s.QdrantClient.UpsertContext(ctx, "memories", []qdrant.Point{{ID: indexPointID(row.UserID, row.MemoryID), Vector: vector, Payload: map[string]interface{}{
				"memory_id": row.MemoryID, "user_id": row.UserID, "character_id": row.CharacterID, "revision": row.Revision, "canonical_schema": 1,
			}}}); err != nil {
				return err
			}
		}
	}
	if s.Mem0Client != nil {
		if row.Mem0ID != "" {
			if err := s.Mem0Client.DeleteContext(ctx, row.Mem0ID); err != nil {
				return err
			}
			row.Mem0ID = ""
		}
		if !row.DeletedAt.Valid {
			reply, err := s.Mem0Client.AddContext(ctx, "user_"+row.UserID, "char_"+row.CharacterID, row.Content, map[string]interface{}{"memory_id": row.MemoryID, "revision": row.Revision})
			if err != nil {
				return err
			}
			if reply.ID == "" {
				return errors.New("missing external memory id")
			}
			row.Mem0ID = reply.ID
		}
	}
	return nil
}

// PendingIndexJobs is safe to show in the admin dashboard; it does not return memory text.
func (s *MemoryService) PendingIndexJobs() (int64, error) {
	var count int64
	err := s.DB.Model(&database.MemoryIndexJob{}).Count(&count).Error
	return count, err
}

// Backfill old SQL memories incrementally. Old unlinked provider text is never used as a fallback.
func (s *MemoryService) BackfillIndexJobs(ctx context.Context, limit int) error {
	if !s.hasExternalIndex() {
		return nil
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var rows []database.Memory
	query := s.DB.WithContext(ctx).Where("indexed_revision < revision").
		Where("NOT EXISTS (SELECT 1 FROM memory_index_jobs WHERE memory_index_jobs.memory_id = memories.memory_id)")
	if s.DB.Migrator().HasTable(&database.User{}) {
		query = query.Where("EXISTS (SELECT 1 FROM users WHERE users.user_id = memories.user_id)")
	}
	if err := query.Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Ignore a race with a live mutation; its newer outbox revision must win.
			job := database.MemoryIndexJob{MemoryID: row.MemoryID, UserID: row.UserID, CharacterID: row.CharacterID, Revision: row.Revision, NextAttemptAt: time.Now()}
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&job).Error
		}); err != nil {
			return err
		}
	}
	return nil
}
