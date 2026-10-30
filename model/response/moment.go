package response

// MomentFeedItem 动态信息流条目（含角色信息）
type MomentFeedItem struct {
	ID          uint     `json:"id"`
	CharacterID string   `json:"character_id"`
	Name        string   `json:"name"`
	Avatar      string   `json:"avatar"`
	Content     string   `json:"content"`
	ImageURLs   []string `json:"image_urls"`
	TopicTags   []string `json:"topic_tags"`
	LikeCount   int      `json:"like_count"`
	PublishedAt int64    `json:"published_at"`
}
