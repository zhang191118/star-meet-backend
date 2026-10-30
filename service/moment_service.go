package service

import (
	"encoding/json"
	"strings"

	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"

	"gorm.io/gorm"
)

type MomentService struct {
	DB *gorm.DB
}

func (s *MomentService) feedQuery() *gorm.DB {
	return s.DB.Table("character_moments").
		Joins("JOIN characters ON characters.character_id = character_moments.character_id").
		Where("character_moments.status = ?", 1).
		Where("characters.is_public = ? AND characters.gender = ?", 1, publicMarketplaceGender).
		Where("characters.deleted_at IS NULL")
}

func (s *MomentService) characterFeedQuery(characterID string) *gorm.DB {
	return s.DB.Table("character_moments").
		Joins("JOIN characters ON characters.character_id = character_moments.character_id").
		Where("character_moments.status = ?", 1).
		Where("characters.is_public = ?", 1).
		Where("characters.deleted_at IS NULL").
		Where("character_moments.character_id = ?", characterID)
}

// GetFeed 获取官方角色的动态信息流（仅正常状态 + 公开角色）
func (s *MomentService) GetFeed(req request.GetMomentFeedReq) ([]response.MomentFeedItem, int64, error) {
	return s.getFeed(s.feedQuery(), req)
}

// GetCharacterFeed 获取官方公开角色的动态（用于角色介绍页）。
func (s *MomentService) GetCharacterFeed(characterID string, req request.GetMomentFeedReq) ([]response.MomentFeedItem, int64, error) {
	return s.getFeed(s.characterFeedQuery(characterID), req)
}

func (s *MomentService) getFeed(query *gorm.DB, req request.GetMomentFeedReq) ([]response.MomentFeedItem, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	req.PageSize = clampPageSize(req.PageSize)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type row struct {
		database.CharacterMoment
		Name   string
		Avatar string
	}
	var rows []row
	offset := (req.Page - 1) * req.PageSize
	err := query.
		Select("character_moments.*, characters.name AS name, characters.avatar AS avatar").
		Order("character_moments.published_at DESC, character_moments.id DESC").
		Offset(offset).Limit(req.PageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	items := make([]response.MomentFeedItem, 0, len(rows))
	for _, r := range rows {
		imageURLs := []string{}
		if r.ImageURLs != "" {
			_ = json.Unmarshal([]byte(r.ImageURLs), &imageURLs)
		}
		tags := []string{}
		if r.TopicTags != "" {
			for _, t := range strings.Split(r.TopicTags, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
		}
		items = append(items, response.MomentFeedItem{
			ID:          r.ID,
			CharacterID: r.CharacterID,
			Name:        r.Name,
			Avatar:      r.Avatar,
			Content:     r.Content,
			ImageURLs:   imageURLs,
			TopicTags:   tags,
			LikeCount:   r.LikeCount,
			PublishedAt: r.PublishedAt.Unix(),
		})
	}
	return items, total, nil
}
