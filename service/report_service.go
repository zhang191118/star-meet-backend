package service

import (
	"errors"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrReportTargetUnavailable = errors.New("report target unavailable")
	ErrInvalidReportReason     = errors.New("invalid report reason")
)

var allowedReportReasons = map[string]bool{
	"spam":          true,
	"harassment":    true,
	"inappropriate": true,
	"other":         true,
}

type ReportService struct {
	DB *gorm.DB
}

func NewReportService(db *gorm.DB) *ReportService {
	return &ReportService{DB: db}
}

func (s *ReportService) CreateReport(reporterID string, req request.ReportReq) error {
	if !allowedReportReasons[req.Reason] {
		return ErrInvalidReportReason
	}
	switch req.TargetType {
	case "character":
		var count int64
		if err := s.DB.Model(&database.Character{}).
			Where("character_id = ? AND (is_public = ? OR user_id = ?)", req.TargetID, 1, reporterID).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrReportTargetUnavailable
		}
	case "message":
		var count int64
		if err := s.DB.Model(&database.Message{}).
			Where("message_id = ? AND user_id = ?", req.TargetID, reporterID).
			Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrReportTargetUnavailable
		}
	default:
		return ErrReportTargetUnavailable
	}

	report := database.Report{
		ReporterID:  reporterID,
		TargetType:  req.TargetType,
		TargetID:    req.TargetID,
		Reason:      req.Reason,
		Description: req.Description,
		Status:      "pending",
	}
	return s.DB.Create(&report).Error
}

func (s *ReportService) BlockUser(blockerID, blockedID string) error {
	ids, err := relatedCharacterIDsForBlock(s.DB, blockerID, blockedID, true)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			block := database.Block{BlockerID: blockerID, BlockedID: id}
			if err := tx.Where("blocker_id = ? AND blocked_id = ?", blockerID, id).FirstOrCreate(&block).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *ReportService) UnblockUser(blockerID, blockedID string) error {
	ids, err := relatedCharacterIDsForBlock(s.DB, blockerID, blockedID, false)
	if err != nil {
		return err
	}
	return s.DB.Where("blocker_id = ? AND blocked_id IN ?", blockerID, ids).Delete(&database.Block{}).Error
}

func (s *ReportService) GetBlockedUsers(blockerID string) ([]string, error) {
	var blocks []database.Block
	if err := s.DB.Where("blocker_id = ?", blockerID).Find(&blocks).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(blocks))
	for i, b := range blocks {
		ids[i] = b.BlockedID
	}
	return ids, nil
}

func (s *ReportService) IsBlocked(blockerID, blockedID string) (bool, error) {
	return isCharacterBlocked(s.DB, blockerID, blockedID)
}

func isCharacterBlocked(db *gorm.DB, userID, characterID string) (bool, error) {
	var count int64
	err := db.Model(&database.Block{}).Where("blocker_id = ? AND blocked_id = ?", userID, characterID).Count(&count).Error
	return count > 0, err
}

func excludeBlockedCharacters(db, query *gorm.DB, userID string) *gorm.DB {
	if strings.TrimSpace(userID) == "" {
		return query
	}
	blockedIDs := db.Model(&database.Block{}).Select("blocked_id").Where("blocker_id = ?", userID)
	return query.Where("character_id NOT IN (?)", blockedIDs)
}

func relatedCharacterIDsForBlock(db *gorm.DB, userID, characterID string, requireTarget bool) ([]string, error) {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return nil, ErrCharacterUnavailable
	}

	ids := []string{characterID}
	var character database.Character
	err := db.Where("character_id = ?", characterID).First(&character).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if requireTarget {
			return nil, ErrCharacterUnavailable
		}
		return appendRelatedIDFromSuffix(ids, userID, characterID), nil
	}

	prefix := shortUserID(userID)
	suffix := "_" + prefix
	switch {
	case character.IsPublic == 1:
		copyID := characterID + suffix
		var count int64
		if err := db.Model(&database.Character{}).
			Where("character_id = ? AND user_id = ? AND is_public = ?", copyID, userID, 2).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			ids = append(ids, copyID)
		}
	case character.IsPublic == 2 && character.UserID == userID && strings.HasSuffix(characterID, suffix):
		originalID := strings.TrimSuffix(characterID, suffix)
		var count int64
		if err := db.Model(&database.Character{}).
			Where("character_id = ? AND is_public = ?", originalID, 1).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			ids = append(ids, originalID)
		}
	case character.UserID != userID:
		return nil, ErrCharacterUnavailable
	}

	return dedupeStrings(ids), nil
}

func appendRelatedIDFromSuffix(ids []string, userID, characterID string) []string {
	prefix := shortUserID(userID)
	suffix := "_" + prefix
	if prefix != "" && strings.HasSuffix(characterID, suffix) {
		ids = append(ids, strings.TrimSuffix(characterID, suffix))
	} else if prefix != "" {
		ids = append(ids, characterID+suffix)
	}
	return dedupeStrings(ids)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func shortUserID(userID string) string {
	if len(userID) > 8 {
		return userID[:8]
	}
	return userID
}
