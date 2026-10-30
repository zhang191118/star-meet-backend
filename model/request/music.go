package request

type MusicCreateReq struct {
	Title    string `json:"title" binding:"required"`
	Artist   string `json:"artist"`
	URL      string `json:"url" binding:"required"`
	Duration int    `json:"duration"`
	Sort     int    `json:"sort"`
}

type MusicUpdateReq struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	URL      string `json:"url"`
	Duration int    `json:"duration"`
	Sort     int    `json:"sort"`
	Enabled  *bool  `json:"enabled"`
}
