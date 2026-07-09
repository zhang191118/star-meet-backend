package request

type UpsertRuntimeConfigReq struct {
	Value       string `json:"value"`
	Sensitive   *bool  `json:"sensitive"`
	Description string `json:"description"`
}
