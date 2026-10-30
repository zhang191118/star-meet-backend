package service

import (
	"errors"
	"math/rand"
	"time"

	"star-meet-backend/model/database"
	"star-meet-backend/pkg/i18n"

	"gorm.io/gorm"
)

type GachaService struct {
	DB           *gorm.DB
	Notification *NotificationService
}

var ErrGachaIdempotencyConflict = errors.New("gacha idempotency conflict")
var ErrInsufficientMoonBalance = errors.New("insufficient moon balance")
var ErrGachaPoolUnavailable = errors.New("gacha pool unavailable")
var ErrGachaPoolUnconfigured = errors.New("gacha pool unconfigured")

func NewGachaService(db *gorm.DB) *GachaService {
	return &GachaService{DB: db}
}

type DrawResult struct {
	Records    []database.UserGachaRecord
	Characters []database.UserCharacter
}

func (s *GachaService) GetPools() ([]database.GachaPool, error) {
	var pools []database.GachaPool
	if err := s.DB.Where("is_active = ?", true).Find(&pools).Error; err != nil {
		return nil, err
	}
	return pools, nil
}

func (s *GachaService) EnsureDefaultGachaContent() error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		characterIDs, err := defaultGachaCandidateIDs(tx)
		if err != nil {
			return err
		}
		if len(characterIDs) == 0 {
			return nil
		}

		pool := defaultStarterGachaPool()
		var existingPool database.GachaPool
		err = tx.Where("pool_id = ?", pool.PoolID).First(&existingPool).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&pool).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !existingPool.IsActive {
			if err := tx.Model(&existingPool).Update("is_active", true).Error; err != nil {
				return err
			}
		}

		rarities := []database.GachaRarity{database.GachaSSR, database.GachaSR, database.GachaR, database.GachaN}
		for index, rarity := range rarities {
			var count int64
			if err := tx.Model(&database.GachaCharacter{}).
				Where("pool_id = ? AND rarity = ?", pool.PoolID, rarity).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}

			candidate := database.GachaCharacter{
				PoolID:      pool.PoolID,
				CharacterID: characterIDs[index%len(characterIDs)],
				Rarity:      rarity,
				Weight:      1,
			}
			if err := tx.Create(&candidate).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func defaultStarterGachaPool() database.GachaPool {
	return database.GachaPool{
		PoolID:     "starter",
		Name:       "新手池",
		Type:       "permanent",
		IsActive:   true,
		SSRRate:    0.02,
		SRRate:     0.08,
		RRate:      0.90,
		NGuarantee: 80,
		Price:      100,
	}
}

func defaultGachaCandidateIDs(tx *gorm.DB) ([]string, error) {
	var characters []database.Character
	if err := tx.Select("character_id").
		Where("is_public = ? AND gender = ? AND character_id <> ?", 1, publicMarketplaceGender, "").
		Order("chat_count DESC, created_at DESC, id DESC").
		Limit(20).
		Find(&characters).Error; err != nil {
		return nil, err
	}

	characterIDs := make([]string, 0, len(characters))
	for _, character := range characters {
		characterIDs = append(characterIDs, character.CharacterID)
	}
	return characterIDs, nil
}

func (s *GachaService) GetPool(poolID string) (*database.GachaPool, error) {
	var pool database.GachaPool
	if err := s.DB.Where("pool_id = ? AND is_active = ?", poolID, true).First(&pool).Error; err != nil {
		return nil, err
	}
	return &pool, nil
}

func (s *GachaService) GetPoolCharacters(poolID string) ([]database.GachaCharacter, error) {
	var characters []database.GachaCharacter
	err := s.DB.Model(&database.GachaCharacter{}).
		Joins("JOIN characters ON characters.character_id = gacha_characters.character_id AND characters.deleted_at IS NULL").
		Where("gacha_characters.pool_id = ?", poolID).
		Where("characters.is_public = ? AND characters.gender = ?", 1, publicMarketplaceGender).
		Select("gacha_characters.*").
		Find(&characters).Error
	if err != nil {
		return nil, err
	}
	return characters, nil
}

func (s *GachaService) GetUserCharacters(userID string) ([]database.UserCharacter, error) {
	var characters []database.UserCharacter
	if err := s.DB.Where("user_id = ?", userID).Find(&characters).Error; err != nil {
		return nil, err
	}
	return characters, nil
}

func (s *GachaService) GetUserGachaHistory(userID string, limit int) ([]database.UserGachaRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	records, _, err := s.GetUserGachaHistoryPage(userID, 1, limit)
	return records, err
}

func (s *GachaService) GetUserGachaHistoryPage(userID string, page, pageSize int) ([]database.UserGachaRecord, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pageSize = clampPageSize(pageSize)

	query := s.DB.Model(&database.UserGachaRecord{}).Where("user_id = ?", userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	var records []database.UserGachaRecord
	if err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func (s *GachaService) GetUserPullCount(userID, poolID string) int {
	var count int64
	s.DB.Model(&database.UserGachaRecord{}).Where("user_id = ? AND pool_id = ?", userID, poolID).Count(&count)
	return int(count)
}

func (s *GachaService) GetUserMoonBalance(userID string) (int64, error) {
	var currency database.UserCurrency
	err := s.DB.Where("user_id = ? AND currency = ?", userID, database.CurrencyMoon).First(&currency).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return currency.Balance, nil
}

func (s *GachaService) Draw(userID, poolID string, count int) (*DrawResult, error) {
	return s.draw(userID, poolID, count, "")
}

func (s *GachaService) DrawWithClientDrawID(userID, poolID string, count int, clientDrawID string) (*DrawResult, bool, error) {
	if clientDrawID == "" {
		result, err := s.draw(userID, poolID, count, "")
		return result, false, err
	}

	var existingDraw database.UserGachaDraw
	err := s.DB.Where("user_id = ? AND client_draw_id = ?", userID, clientDrawID).First(&existingDraw).Error
	if err == nil {
		if existingDraw.PoolID != poolID || existingDraw.Count != count {
			return nil, false, ErrGachaIdempotencyConflict
		}
		result, err := s.loadDrawResult(userID, clientDrawID)
		return result, true, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	result, err := s.draw(userID, poolID, count, clientDrawID)
	return result, false, err
}

func (s *GachaService) loadDrawResult(userID, clientDrawID string) (*DrawResult, error) {
	var records []database.UserGachaRecord
	if err := s.DB.Where("user_id = ? AND client_draw_id = ?", userID, clientDrawID).Order("id ASC").Find(&records).Error; err != nil {
		return nil, err
	}

	characterIDs := make([]string, 0, len(records))
	for _, record := range records {
		characterIDs = append(characterIDs, record.CharacterID)
	}

	var characters []database.UserCharacter
	if len(characterIDs) > 0 {
		if err := s.DB.Where("user_id = ? AND character_id IN ?", userID, characterIDs).Find(&characters).Error; err != nil {
			return nil, err
		}
	}

	return &DrawResult{Records: records, Characters: characters}, nil
}

func (s *GachaService) draw(userID, poolID string, count int, clientDrawID string) (*DrawResult, error) {
	pool, err := s.GetPool(poolID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGachaPoolUnavailable
		}
		return nil, err
	}

	characters, err := s.GetPoolCharacters(poolID)
	if err != nil {
		return nil, err
	}

	if len(characters) == 0 {
		return nil, ErrGachaPoolUnconfigured
	}

	// 获取用户抽卡次数
	userPullCount := s.GetUserPullCount(userID, poolID)
	isGuarantee := false

	// 保底判断
	if pool.NGuarantee > 0 && userPullCount >= pool.NGuarantee-1 {
		isGuarantee = true
	}

	type plannedDraw struct {
		rarity   database.GachaRarity
		selected database.GachaCharacter
	}
	plannedDraws := make([]plannedDraw, 0, count)
	for i := 0; i < count; i++ {
		rarity := s.calculateRarity(pool, isGuarantee && i == count-1)

		var candidates []database.GachaCharacter
		for _, c := range characters {
			if c.Rarity == rarity {
				candidates = append(candidates, c)
			}
		}
		if len(candidates) == 0 {
			return nil, ErrGachaPoolUnconfigured
		}
		plannedDraws = append(plannedDraws, plannedDraw{
			rarity:   rarity,
			selected: candidates[rand.Intn(len(candidates))],
		})
	}

	var results []database.UserGachaRecord
	var obtainedCharacters []database.UserCharacter

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if clientDrawID != "" {
			draw := database.UserGachaDraw{
				UserID:       userID,
				ClientDrawID: clientDrawID,
				PoolID:       poolID,
				Count:        count,
			}
			if err := tx.Create(&draw).Error; err != nil {
				return err
			}
		}

		totalCost := int64(pool.Price * count)
		if totalCost > 0 {
			var currency database.UserCurrency
			err := tx.Where("user_id = ? AND currency = ?", userID, database.CurrencyMoon).First(&currency).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInsufficientMoonBalance
			}
			if err != nil {
				return err
			}
			if currency.Balance < totalCost {
				return ErrInsufficientMoonBalance
			}

			currency.Balance -= totalCost
			if err := tx.Save(&currency).Error; err != nil {
				return err
			}

			transaction := database.CurrencyTransaction{
				UserID:      userID,
				Currency:    database.CurrencyMoon,
				Amount:      -totalCost,
				Type:        database.TransactionGacha,
				RelatedID:   poolID,
				Description: "抽卡消耗",
			}
			if err := tx.Create(&transaction).Error; err != nil {
				return err
			}
		}

		for i, planned := range plannedDraws {
			rarity := planned.rarity
			selected := planned.selected

			// 创建抽卡记录
			record := database.UserGachaRecord{
				UserID:       userID,
				PoolID:       poolID,
				CharacterID:  selected.CharacterID,
				Rarity:       rarity,
				Count:        userPullCount + i + 1,
				IsGuarantee:  isGuarantee && i == count-1,
				Cost:         pool.Price,
				ClientDrawID: clientDrawID,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			results = append(results, record)

			// 检查是否已有该角色
			var existing database.UserCharacter
			err := tx.Where("user_id = ? AND character_id = ?", userID, selected.CharacterID).First(&existing).Error
			if err == gorm.ErrRecordNotFound {
				// 新角色
				userChar := database.UserCharacter{
					UserID:      userID,
					CharacterID: selected.CharacterID,
					ObtainType:  "gacha",
					StarLevel:   s.rarityToStarLevel(rarity),
					Exp:         0,
					IsFavorite:  false,
				}
				if err := tx.Create(&userChar).Error; err != nil {
					return err
				}
				obtainedCharacters = append(obtainedCharacters, userChar)
			} else if err == nil {
				// 已存在，增加经验
				existing.Exp += 100
				existing.StarLevel = s.rarityToStarLevel(rarity)
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				obtainedCharacters = append(obtainedCharacters, existing)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if s.Notification != nil && len(results) > 0 {
		best := results[0].Rarity
		bestChar := results[0].CharacterID
		for _, rec := range results {
			if rarityRank(rec.Rarity) > rarityRank(best) {
				best = rec.Rarity
				bestChar = rec.CharacterID
			}
		}
		if best == database.GachaSSR || best == database.GachaSR {
			var ch database.Character
			name := ""
			if err := s.DB.Where("character_id = ?", bestChar).First(&ch).Error; err == nil {
				name = i18n.TranslateCharacterName(ch.Name, "en")
			}
			s.Notification.NotifyNewCharacter(userID, name, "en", string(best))
		}
	}

	return &DrawResult{
		Records:    results,
		Characters: obtainedCharacters,
	}, nil
}

func (s *GachaService) calculateRarity(pool *database.GachaPool, forceGuarantee bool) database.GachaRarity {
	if forceGuarantee {
		return database.GachaSSR
	}

	rand.Seed(time.Now().UnixNano())
	r := rand.Float64()

	// 根据概率计算
	ssrThreshold := pool.SSRRate
	srThreshold := ssrThreshold + pool.SRRate
	rThreshold := srThreshold + pool.RRate

	if r < ssrThreshold {
		return database.GachaSSR
	} else if r < srThreshold {
		return database.GachaSR
	} else if r < rThreshold {
		return database.GachaR
	}
	return database.GachaN
}

func (s *GachaService) rarityToStarLevel(rarity database.GachaRarity) int {
	switch rarity {
	case database.GachaSSR:
		return 5
	case database.GachaSR:
		return 4
	case database.GachaR:
		return 3
	default:
		return 2
	}
}

func (s *GachaService) ToggleFavorite(userID, characterID string) error {
	var userChar database.UserCharacter
	if err := s.DB.Where("user_id = ? AND character_id = ?", userID, characterID).First(&userChar).Error; err != nil {
		return err
	}

	userChar.IsFavorite = !userChar.IsFavorite
	return s.DB.Save(&userChar).Error
}

func rarityRank(r database.GachaRarity) int {
	switch r {
	case database.GachaSSR:
		return 4
	case database.GachaSR:
		return 3
	case database.GachaR:
		return 2
	default:
		return 1
	}
}
