package service

import (
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"

	"gorm.io/gorm"
)

type MusicService struct {
	DB *gorm.DB
}

func NewMusicService(db *gorm.DB) *MusicService {
	return &MusicService{DB: db}
}

// GetEnabled 获取所有启用的音乐，按 sort 排序
func (s *MusicService) GetEnabled() ([]response.MusicResp, error) {
	var musics []database.Music
	if err := s.DB.Where("enabled = ?", true).Order("sort ASC, id ASC").Find(&musics).Error; err != nil {
		return nil, err
	}

	result := make([]response.MusicResp, len(musics))
	for i, m := range musics {
		result[i] = response.MusicResp{
			ID:       m.ID,
			Title:    m.Title,
			Artist:   m.Artist,
			URL:      m.URL,
			Duration: m.Duration,
			Sort:     m.Sort,
		}
	}
	return result, nil
}

// List 获取所有音乐（管理后台用）
func (s *MusicService) List() ([]response.MusicResp, error) {
	var musics []database.Music
	if err := s.DB.Order("sort ASC, id ASC").Find(&musics).Error; err != nil {
		return nil, err
	}

	result := make([]response.MusicResp, len(musics))
	for i, m := range musics {
		result[i] = response.MusicResp{
			ID:       m.ID,
			Title:    m.Title,
			Artist:   m.Artist,
			URL:      m.URL,
			Duration: m.Duration,
			Sort:     m.Sort,
		}
	}
	return result, nil
}

// Create 创建音乐记录
func (s *MusicService) Create(title, artist, url string, duration, sort int) (*database.Music, error) {
	m := &database.Music{
		Title:    title,
		Artist:   artist,
		URL:      url,
		Duration: duration,
		Sort:     sort,
		Enabled:  true,
	}
	if err := s.DB.Create(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

// SeedDefaults 初始化默认音乐数据（幂等，按 URL 去重）
func (s *MusicService) SeedDefaults(musics []database.Music) error {
	for _, m := range musics {
		var existing database.Music
		err := s.DB.Where("url = ?", m.URL).First(&existing).Error
		if err == nil {
			continue // 已存在，跳过
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if err := s.DB.Create(&m).Error; err != nil {
			return err
		}
	}
	return nil
}
