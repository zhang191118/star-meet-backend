package service

import (
	"star-meet-backend/core/mem0"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MemoryService struct {
	DB         *gorm.DB
	Mem0Client *mem0.Client
}

// GetMemoryList 获取记忆列表
func (s *MemoryService) GetMemoryList(req request.GetMemoryListReq) ([]response.MemoryResp, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	req.PageSize = clampPageSize(req.PageSize)

	query := s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ?", req.UserID, req.CharacterID)

	// 类型筛选
	if req.MemoryType != "" && req.MemoryType != "all" {
		query = query.Where("memory_type = ?", req.MemoryType)
	}

	var total int64
	query.Count(&total)

	offset := (req.Page - 1) * req.PageSize
	var memories []database.Memory
	err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(req.PageSize).Find(&memories).Error

	if err != nil {
		return nil, 0, err
	}

	resp := make([]response.MemoryResp, 0, len(memories))
	for _, mem := range memories {
		resp = append(resp, response.MemoryResp{
			MemoryID:    mem.MemoryID,
			UserID:      mem.UserID,
			CharacterID: mem.CharacterID,
			Content:     mem.Content,
			MemoryType:  mem.MemoryType,
			Source:      mem.Source,
			Importance:  mem.Importance,
			CreatedAt:   mem.CreatedAt.Unix(),
			UpdatedAt:   mem.UpdatedAt.Unix(),
		})
	}

	// 如果Mem0可用，也从Mem0获取
	if s.Mem0Client != nil {
		memories, err := s.Mem0Client.GetAll(
			"user_"+req.UserID,
			"char_"+req.CharacterID,
			req.PageSize,
			offset,
		)
		if err == nil && len(memories) > 0 {
			for _, m := range memories {
				// 避免重复
				exists := false
				for _, r := range resp {
					if r.Content == m.Memory {
						exists = true
						break
					}
				}
				if !exists {
					resp = append(resp, response.MemoryResp{
						MemoryID:    m.ID,
						UserID:      req.UserID,
						CharacterID: req.CharacterID,
						Content:     m.Memory,
						MemoryType:  "chat",
						Source:      "auto",
						CreatedAt:   time.Now().Unix(),
					})
				}
			}
		}
	}

	return resp, total, nil
}

// AddMemory 添加记忆
func (s *MemoryService) AddMemory(req request.AddMemoryReq) (*response.MemoryResp, error) {
	memoryID := uuid.New().String()

	// 先存入本地数据库
	mem := database.Memory{
		MemoryID:    memoryID,
		UserID:      req.UserID,
		CharacterID: req.CharacterID,
		Content:     req.Content,
		MemoryType:  req.MemoryType,
		Source:      req.Source,
	}

	if mem.MemoryType == "" {
		mem.MemoryType = "fact"
	}
	if mem.Source == "" {
		mem.Source = "manual"
	}

	if err := s.DB.Create(&mem).Error; err != nil {
		return nil, err
	}

	// 尝试存入Mem0
	if s.Mem0Client != nil {
		result, err := s.Mem0Client.Add(
			"user_"+req.UserID,
			"char_"+req.CharacterID,
			req.Content,
			map[string]interface{}{
				"type":      req.MemoryType,
				"source":    req.Source,
				"memory_id": memoryID,
				"timestamp": time.Now().Unix(),
			},
		)
		if err == nil && result.ID != "" {
			// 更新Mem0 ID
			s.DB.Model(&mem).Update("mem0_id", result.ID)
			mem.Mem0ID = result.ID
		}
	}

	return &response.MemoryResp{
		MemoryID:    mem.MemoryID,
		UserID:      mem.UserID,
		CharacterID: mem.CharacterID,
		Content:     mem.Content,
		MemoryType:  mem.MemoryType,
		Source:      mem.Source,
		Importance:  mem.Importance,
		CreatedAt:   mem.CreatedAt.Unix(),
		UpdatedAt:   mem.UpdatedAt.Unix(),
	}, nil
}

// UpdateMemory 更新记忆
func (s *MemoryService) UpdateMemory(memoryID, userID, content string) error {
	// 更新本地
	result := s.DB.Model(&database.Memory{}).Where("memory_id = ? AND user_id = ?", memoryID, userID).Update("content", content)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	// 如果有Mem0 ID，也更新Mem0（Mem0不支持更新，需要删除后重新添加）
	// 这里简化处理，只更新本地

	return nil
}

// DeleteMemory 删除记忆
func (s *MemoryService) DeleteMemory(memoryID, userID string) error {
	// 获取记忆
	var mem database.Memory
	if err := s.DB.Where("memory_id = ? AND user_id = ?", memoryID, userID).First(&mem).Error; err != nil {
		return err
	}

	// 删除Mem0中的记忆
	if s.Mem0Client != nil && mem.Mem0ID != "" {
		s.Mem0Client.Delete(mem.Mem0ID)
	}

	// 删除本地
	return s.DB.Where("memory_id = ? AND user_id = ?", memoryID, userID).Delete(&database.Memory{}).Error
}

// SearchMemory 搜索记忆
func (s *MemoryService) SearchMemory(req request.SearchMemoryReq) ([]response.MemoryResp, error) {
	// 优先从Mem0搜索
	if s.Mem0Client != nil {
		results, err := s.Mem0Client.Search(
			"user_"+req.UserID,
			"char_"+req.CharacterID,
			req.Query,
			10,
		)
		if err == nil && len(results) > 0 {
			var resp []response.MemoryResp
			for _, r := range results {
				resp = append(resp, response.MemoryResp{
					MemoryID:    r.ID,
					UserID:      req.UserID,
					CharacterID: req.CharacterID,
					Content:     r.Memory,
					MemoryType:  "chat",
					Source:      "auto",
					CreatedAt:   time.Now().Unix(),
				})
			}
			return resp, nil
		}
	}

	// 降级到本地搜索
	var memories []database.Memory
	err := s.DB.Where("user_id = ? AND character_id = ? AND content LIKE ?",
		req.UserID, req.CharacterID, "%"+req.Query+"%").
		Limit(20).
		Find(&memories).Error

	if err != nil {
		return nil, err
	}

	resp := make([]response.MemoryResp, 0, len(memories))
	for _, mem := range memories {
		resp = append(resp, response.MemoryResp{
			MemoryID:    mem.MemoryID,
			UserID:      mem.UserID,
			CharacterID: mem.CharacterID,
			Content:     mem.Content,
			MemoryType:  mem.MemoryType,
			Source:      mem.Source,
			CreatedAt:   mem.CreatedAt.Unix(),
		})
	}

	return resp, nil
}

// GetMemoryStats 获取记忆统计
func (s *MemoryService) GetMemoryStats(userID, characterID string) (*response.MemoryStatsResp, error) {
	resp := &response.MemoryStatsResp{}

	var totalCount, todayCount, prefCnt, emoCnt, evtCnt, relCnt int64

	// 总数
	s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ?", userID, characterID).Count(&totalCount)

	// 今日新增
	today := time.Now().Format("2006-01-02")
	s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ? AND DATE(created_at) = ?", userID, characterID, today).Count(&todayCount)

	// 各类型数量
	s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ? AND memory_type = ?", userID, characterID, "preference").Count(&prefCnt)
	s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ? AND memory_type = ?", userID, characterID, "emotion").Count(&emoCnt)
	s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ? AND memory_type = ?", userID, characterID, "event").Count(&evtCnt)
	s.DB.Model(&database.Memory{}).Where("user_id = ? AND character_id = ? AND memory_type = ?", userID, characterID, "relationship").Count(&relCnt)

	resp.TotalCount = int(totalCount)
	resp.TodayCount = int(todayCount)
	resp.PreferenceCnt = int(prefCnt)
	resp.EmotionCnt = int(emoCnt)
	resp.EventCnt = int(evtCnt)
	resp.RelationshipCnt = int(relCnt)

	return resp, nil
}
