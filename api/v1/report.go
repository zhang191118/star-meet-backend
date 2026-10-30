package v1

import (
	"errors"
	"net/http"

	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ReportAPI struct {
	ReportService *service.ReportService
}

func NewReportAPI(db *gorm.DB) *ReportAPI {
	return &ReportAPI{ReportService: service.NewReportService(db)}
}

// CreateReport 举报内容
func (a *ReportAPI) CreateReport(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		response.FailWithError(c, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
		return
	}

	var req request.ReportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "invalid request")
		return
	}

	if err := a.ReportService.CreateReport(userID.(string), req); err != nil {
		if errors.Is(err, service.ErrInvalidReportReason) {
			response.FailWithError(c, http.StatusUnprocessableEntity, "invalid report reason", "invalid_report_reason", nil)
			return
		}
		if errors.Is(err, service.ErrReportTargetUnavailable) {
			response.FailWithError(c, http.StatusNotFound, "report target unavailable", "report_target_unavailable", nil)
			return
		}
		response.FailWithError(c, http.StatusInternalServerError, err.Error(), "report_failed", nil)
		return
	}

	response.OkWithMessage(c, "举报已提交，我们会尽快处理")
}

// BlockUser 拉黑用户
func (a *ReportAPI) BlockUser(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		response.FailWithError(c, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
		return
	}

	var req request.BlockReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "invalid request")
		return
	}

	if err := a.ReportService.BlockUser(userID.(string), req.BlockedID); err != nil {
		if errors.Is(err, service.ErrCharacterUnavailable) {
			response.FailWithError(c, http.StatusNotFound, "character unavailable", "character_unavailable", nil)
			return
		}
		response.FailWithError(c, http.StatusInternalServerError, err.Error(), "block_failed", nil)
		return
	}

	response.OkWithMessage(c, "已拉黑")
}

// UnblockUser 取消拉黑
func (a *ReportAPI) UnblockUser(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		response.FailWithError(c, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
		return
	}

	blockedID := c.Param("blocked_id")
	if blockedID == "" {
		response.FailWithMessage(c, "blocked_id required")
		return
	}

	if err := a.ReportService.UnblockUser(userID.(string), blockedID); err != nil {
		response.FailWithError(c, http.StatusInternalServerError, err.Error(), "unblock_failed", nil)
		return
	}

	response.OkWithMessage(c, "已取消拉黑")
}

// GetBlockedUsers 获取拉黑列表
func (a *ReportAPI) GetBlockedUsers(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		response.FailWithError(c, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
		return
	}

	ids, err := a.ReportService.GetBlockedUsers(userID.(string))
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, err.Error(), "fetch_failed", nil)
		return
	}

	response.OkWithData(c, gin.H{"blocked_ids": ids})
}
