package request

type ReportReq struct {
	TargetType  string `json:"target_type" binding:"required"`
	TargetID    string `json:"target_id" binding:"required"`
	Reason      string `json:"reason" binding:"required"`
	Description string `json:"description"`
}

type BlockReq struct {
	BlockedID string `json:"blocked_id" binding:"required"`
}