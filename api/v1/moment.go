package v1

import (
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

type MomentAPI struct {
	MomentService *service.MomentService
}

func NewMomentAPI(momentService *service.MomentService) *MomentAPI {
	return &MomentAPI{MomentService: momentService}
}

// GetFeed 获取角色动态信息流
// @Summary 获取角色动态信息流
// @Tags 动态模块
// @Accept json
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.MomentFeedItem
// @Router /api/v1/moment/feed [get]
func (a *MomentAPI) GetFeed(c *gin.Context) {
	var req request.GetMomentFeedReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.Page, req.PageSize = normalizePagination(req.Page, req.PageSize, 20)

	list, total, err := a.MomentService.GetFeed(req)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"list":      list,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
		"has_more":  hasMore(req.Page, req.PageSize, total),
	})
}

// GetCharacterFeed 获取指定官方角色的动态信息流。
func (a *MomentAPI) GetCharacterFeed(c *gin.Context) {
	var req request.GetMomentFeedReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.Page, req.PageSize = normalizePagination(req.Page, req.PageSize, 20)

	list, total, err := a.MomentService.GetCharacterFeed(c.Param("character_id"), req)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"list":      list,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
		"has_more":  hasMore(req.Page, req.PageSize, total),
	})
}
