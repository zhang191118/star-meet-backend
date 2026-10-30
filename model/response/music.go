package response

type MusicResp struct {
	ID       uint   `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	URL      string `json:"url"`
	Duration int    `json:"duration"`
	Sort     int    `json:"sort"`
}
