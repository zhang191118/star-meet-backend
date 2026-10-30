package request

// GetMomentFeedReq 动态信息流请求
type GetMomentFeedReq struct {
	Page     int `form:"page"`
	PageSize int `form:"page_size"`
}
