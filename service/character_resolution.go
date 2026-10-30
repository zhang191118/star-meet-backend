package service

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"
)

// canonicalCharacterIDForUser maps a public role ID to the caller's owned
// private copy without modifying data. Legacy clients may keep using the
// public ID after /resolve has created the private role.
func canonicalCharacterIDForUser(db *gorm.DB, userID, characterID string) (string, error) {
	userID = strings.TrimSpace(userID)
	characterID = strings.TrimSpace(characterID)
	if userID == "" || characterID == "" {
		return "", ErrCharacterUnavailable
	}
	if !db.Migrator().HasTable(&database.Character{}) {
		return characterID, nil
	}

	var character database.Character
	err := db.Select("character_id", "user_id", "is_public").
		Where("character_id = ?", characterID).
		First(&character).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
		// A deleted/delisted source row must not make an already-owned private
		// copy unreachable to a client that kept the original public ID.
		privateID := characterID + "_" + shortUserID(userID)
		var count int64
		if err := db.Model(&database.Character{}).
			Where("character_id = ? AND user_id = ? AND is_public = ?", privateID, userID, 2).
			Count(&count).Error; err != nil {
			return "", err
		}
		if count > 0 {
			return privateID, nil
		}
		return "", gorm.ErrRecordNotFound
	}

	if character.IsPublic == 1 {
		privateID := characterID + "_" + shortUserID(userID)
		var count int64
		if err := db.Model(&database.Character{}).
			Where("character_id = ? AND user_id = ? AND is_public = ?", privateID, userID, 2).
			Count(&count).Error; err != nil {
			return "", err
		}
		if count > 0 {
			hasPublicState, err := hasPublicCharacterState(db, userID, characterID)
			if err != nil {
				return "", err
			}
			if hasPublicState {
				// Encounter can create the private copy before /resolve has merged
				// existing public sessions, messages, and memories. Keep serving
				// the public scope until the explicit merge removes those rows.
				return characterID, nil
			}
			return privateID, nil
		}
		// Existing public sessions, including delisted roles, keep their old
		// scope until the user explicitly resolves into a private copy.
		return characterID, nil
	}

	if character.UserID == userID || ownsCollectedCharacter(db, userID, characterID) {
		return characterID, nil
	}
	return "", ErrCharacterUnavailable
}

func hasPublicCharacterState(db *gorm.DB, userID, characterID string) (bool, error) {
	models := []interface{}{
		&database.Session{},
		&database.Message{},
		&database.Memory{},
	}
	for _, model := range models {
		if !db.Migrator().HasTable(model) {
			continue
		}
		var count int64
		// Only state still visible to the client can hide the private copy.
		// Deleted sessions leave soft-deleted messages behind, and the merge
		// below cannot move those orphans, so counting them would keep legacy
		// clients on the public scope forever.
		if err := db.Model(model).
			Where("user_id = ? AND character_id = ?", userID, characterID).
			Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

// ResolveChatCharacter gives every entry point the same owned character and conversation.
func (s *CharacterService) ResolveChatCharacter(userID, characterID, lang string) (*response.CharacterResp, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrCharacterUnavailable
	}
	var resolved database.Character
	created := false
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireExistingMemoryUser(tx, userID); err != nil {
			return err
		}
		var input database.Character
		if err := tx.Where("character_id = ?", characterID).First(&input).Error; err != nil {
			return err
		}
		if blocked, err := isCharacterBlocked(tx, userID, characterID); err != nil {
			return err
		} else if blocked {
			return ErrCharacterBlocked
		}
		publicID := ""
		if input.UserID == userID && input.IsPublic != 1 {
			resolved = input
			suffix := "_" + shortUserID(userID)
			if strings.HasSuffix(input.CharacterID, suffix) {
				publicID = strings.TrimSuffix(input.CharacterID, suffix)
			}
		} else {
			var available database.Character
			if err := applyPublicMarketplaceFilter(tx.Where("character_id = ?", characterID)).First(&available).Error; err != nil {
				// Existing historical sessions and explicitly collected characters stay usable when delisted.
				if ownsCollectedCharacter(tx, userID, characterID) || (input.IsPublic == 1 && ownsLegacySession(tx, userID, characterID)) {
					resolved = input
					return nil
				}
				return ErrCharacterUnavailable
			}
			input = available
			publicID = input.CharacterID
			child := *s
			child.DB = tx
			child.Notification = nil
			err := child.EncounterCharacter(userID, publicID, lang)
			if err != nil && !errors.Is(err, ErrCharacterAlreadyEncountered) {
				return err
			}
			created = err == nil
			if err := tx.Where("character_id = ? AND user_id = ?", publicID+"_"+shortUserID(userID), userID).First(&resolved).Error; err != nil {
				return err
			}
		}
		// Lock the existing character as the cross-process serialization point for legacy merging.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", resolved.ID, userID).First(&resolved).Error; err != nil {
			return err
		}
		if publicID != "" && publicID != resolved.CharacterID {
			return s.mergeLegacyCharacterState(tx, userID, publicID, resolved.CharacterID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if created && s.Notification != nil {
		s.Notification.NotifyNewCharacter(userID, i18n.TranslateCharacterName(resolved.Name, lang), lang, "")
	}
	result := s.toCharacterResp(resolved, lang)
	result.SystemPrompt = ""
	result.Mem0AgentID = ""
	return result, nil
}

func (s *CharacterService) mergeLegacyCharacterState(tx *gorm.DB, userID, publicID, privateID string) error {
	if tx.Migrator().HasTable(&database.Session{}) {
		var sessions []database.Session
		if err := tx.Where("user_id = ? AND character_id IN ?", userID, []string{publicID, privateID}).Order("last_message_at DESC, id DESC").Find(&sessions).Error; err != nil {
			return err
		}
		var legacyCount int64
		if err := tx.Model(&database.Session{}).Where("user_id = ? AND character_id = ?", userID, publicID).Count(&legacyCount).Error; err != nil {
			return err
		}
		if legacyCount > 0 && len(sessions) > 0 {
			target := sessions[0]
			ids := []string{}
			unread := 0
			pinned := false
			for _, session := range sessions {
				unread += session.UnreadCount
				pinned = pinned || session.IsPinned
				if session.SessionID != target.SessionID {
					ids = append(ids, session.SessionID)
				}
			}
			if tx.Migrator().HasTable(&database.Message{}) {
				if err := tx.Unscoped().Model(&database.Message{}).Where("user_id = ? AND character_id IN ?", userID, []string{publicID, privateID}).
					Updates(map[string]interface{}{"character_id": privateID, "session_id": target.SessionID}).Error; err != nil {
					return err
				}
				var count int64
				if err := tx.Model(&database.Message{}).Where("user_id = ? AND session_id = ?", userID, target.SessionID).Count(&count).Error; err != nil {
					return err
				}
				target.MessageCount = int(count)
			}
			target.CharacterID = privateID
			target.UnreadCount = unread
			target.IsPinned = pinned
			if err := tx.Save(&target).Error; err != nil {
				return err
			}
			if len(ids) > 0 {
				if err := tx.Unscoped().Where("user_id = ? AND session_id IN ?", userID, ids).Delete(&database.Session{}).Error; err != nil {
					return err
				}
			}
		}
	}
	if tx.Migrator().HasTable(&database.Memory{}) {
		var memories []database.Memory
		if err := tx.Unscoped().Where("user_id = ? AND character_id = ?", userID, publicID).Find(&memories).Error; err != nil {
			return err
		}
		for _, memory := range memories {
			memory.CharacterID = privateID
			memory.Revision++
			if err := tx.Unscoped().Save(&memory).Error; err != nil {
				return err
			}
			if s.Memory != nil {
				if err := s.Memory.queueIndex(tx, memory, publicID); err != nil {
					return err
				}
			}
		}
	}
	if tx.Migrator().HasTable(&database.UserAffection{}) {
		var old database.UserAffection
		if err := tx.Where("user_id = ? AND character_id = ?", userID, publicID).First(&old).Error; err == nil {
			var current database.UserAffection
			err := tx.Where("user_id = ? AND character_id = ?", userID, privateID).First(&current).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				old.CharacterID = privateID
				if err := tx.Save(&old).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				current.Exp += old.Exp
				current.Level = levelForExp(current.Exp)
				if err := tx.Save(&current).Error; err != nil {
					return err
				}
				if err := tx.Delete(&old).Error; err != nil {
					return err
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if tx.Migrator().HasTable(&database.UserCharacterNotificationSetting{}) {
		var old database.UserCharacterNotificationSetting
		if err := tx.Where("user_id = ? AND character_id = ?", userID, publicID).First(&old).Error; err == nil {
			var current database.UserCharacterNotificationSetting
			err := tx.Where("user_id = ? AND character_id = ?", userID, privateID).First(&current).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				old.CharacterID = privateID
				if err := tx.Save(&old).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				current.Enabled = current.Enabled && old.Enabled
				if old.MutedUntil != nil && (current.MutedUntil == nil || old.MutedUntil.After(*current.MutedUntil)) {
					current.MutedUntil = old.MutedUntil
				}
				if old.DailyLimit < current.DailyLimit {
					current.DailyLimit = old.DailyLimit
				}
				if err := tx.Save(&current).Error; err != nil {
					return err
				}
				if err := tx.Delete(&old).Error; err != nil {
					return err
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if tx.Migrator().HasTable(&database.Notification{}) {
		if err := tx.Model(&database.Notification{}).Where("user_id = ? AND character_id = ?", userID, publicID).Update("character_id", privateID).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&database.ProactiveEvent{}) {
		if err := tx.Model(&database.ProactiveEvent{}).Where("user_id = ? AND character_id = ? AND status IN ?", userID, publicID, []string{"pending", "processing"}).Updates(map[string]interface{}{"status": "cancelled", "updated_at": time.Now()}).Error; err != nil {
			return err
		}
	}
	return nil
}

func ownsCollectedCharacter(db *gorm.DB, userID, characterID string) bool {
	if !db.Migrator().HasTable(&database.UserCharacter{}) {
		return false
	}
	var count int64
	return db.Model(&database.UserCharacter{}).Where("user_id = ? AND character_id = ?", userID, characterID).Count(&count).Error == nil && count > 0
}
func ownsLegacySession(db *gorm.DB, userID, characterID string) bool {
	if !db.Migrator().HasTable(&database.Session{}) {
		return false
	}
	var count int64
	return db.Model(&database.Session{}).Where("user_id = ? AND character_id = ?", userID, characterID).Count(&count).Error == nil && count > 0
}
