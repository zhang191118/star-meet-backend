package v1

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/service"
)

type GrowthAPI struct {
	DB    *gorm.DB
	Admin *RuntimeConfigAPI
}

func NewGrowthAPI(db *gorm.DB, config *service.RuntimeConfigService) *GrowthAPI {
	return &GrowthAPI{DB: db, Admin: NewRuntimeConfigAPI(config)}
}
func (a *GrowthAPI) CreateFeedback(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	var input service.FeedbackInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithError(c, 400, "invalid feedback", "invalid_feedback", nil)
		return
	}
	userID := optionalAccessUserID(c)
	if c.GetHeader("Authorization") != "" && userID == "" {
		response.FailWithError(c, 401, "invalid access token", "invalid_token", nil)
		return
	}
	row, err := service.CreateFeedback(a.DB, userID, input)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.FailWithError(c, 401, "account unavailable", "invalid_token", nil)
		} else if errors.Is(err, service.ErrInvalidGrowthInput) {
			response.FailWithError(c, 400, "feedback must contain 5–2000 characters and a valid category", "invalid_feedback", nil)
		} else {
			response.FailWithError(c, 500, "feedback could not be saved", "feedback_save_failed", nil)
		}
		return
	}
	response.OkWithData(c, gin.H{"feedback_id": row.ID, "status": row.Status})
}
func (a *GrowthAPI) IngestEvents(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	var input struct {
		Events []service.ClientEvent `json:"events"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithError(c, 400, "invalid event batch", "invalid_events", nil)
		return
	}
	userID := optionalAccessUserID(c)
	if c.GetHeader("Authorization") != "" && userID == "" {
		response.FailWithError(c, 401, "invalid access token", "invalid_token", nil)
		return
	}
	err := (&service.AnalyticsService{DB: a.DB}).Ingest(userID, input.Events, time.Now())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.FailWithError(c, 401, "account unavailable", "invalid_token", nil)
		} else if errors.Is(err, service.ErrInvalidGrowthInput) {
			response.FailWithError(c, 400, "invalid event fields", "invalid_events", nil)
		} else {
			response.FailWithError(c, 500, "events could not be saved", "event_save_failed", nil)
		}
		return
	}
	response.OkWithData(c, gin.H{"accepted": len(input.Events)})
}
func (a *GrowthAPI) ListFeedback(c *gin.Context) {
	if !a.Admin.requireAdmin(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	if page > 10000 {
		page = 10000
	}
	query := a.DB.Model(&database.Feedback{})
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.FailWithError(c, 500, "feedback unavailable", "feedback_list_failed", nil)
		return
	}
	rows := make([]database.Feedback, 0)
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * 30).Limit(30).Find(&rows).Error; err != nil {
		response.FailWithError(c, 500, "feedback unavailable", "feedback_list_failed", nil)
		return
	}
	response.OkWithData(c, gin.H{"list": rows, "total": total, "page": page})
}
func (a *GrowthAPI) UpdateFeedback(c *gin.Context) {
	if !a.Admin.requireAdmin(c) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var input struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithError(c, 400, "invalid status", "invalid_feedback_status", nil)
		return
	}
	if err := service.UpdateFeedbackStatus(a.DB, c.Param("id"), input.Status); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.FailWithError(c, 404, "feedback not found", "feedback_not_found", nil)
		} else if errors.Is(err, service.ErrInvalidGrowthInput) {
			response.FailWithError(c, 400, "invalid status", "invalid_feedback_status", nil)
		} else {
			response.FailWithError(c, 500, "status update failed", "feedback_update_failed", nil)
		}
		return
	}
	response.OkWithData(c, gin.H{"status": input.Status})
}
func (a *GrowthAPI) Summary(c *gin.Context) {
	if !a.Admin.requireAdmin(c) {
		return
	}
	data, err := (&service.AnalyticsService{DB: a.DB}).Summary(time.Now())
	if err != nil {
		response.FailWithError(c, 500, "summary unavailable", "growth_summary_failed", nil)
		return
	}
	var pending int64
	if err := a.DB.Model(&database.MemoryIndexJob{}).Count(&pending).Error; err != nil {
		response.FailWithError(c, 500, "index status unavailable", "growth_summary_failed", nil)
		return
	}
	response.OkWithData(c, gin.H{"timezone": data.Timezone, "cohort_definition": data.CohortDefinition, "unlinked_first_opens": data.UnlinkedFirstOpens, "events_truncated": data.EventsTruncated, "days": data.Days, "pending_memory_index_jobs": pending})
}
