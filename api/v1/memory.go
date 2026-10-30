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

type MemoryAPI struct {
	MemoryService *service.MemoryService
}

func NewMemoryAPI(memoryService *service.MemoryService) *MemoryAPI {
	return &MemoryAPI{MemoryService: memoryService}
}

// GetList 获取记忆列表
// @Summary 获取记忆列表
// @Tags 记忆模块
// @Accept json
// @Produce json
// @Param character_id query string true "角色ID"
// @Param memory_type query string false "记忆类型"
// @Success 200 {object} response.MemoryResp
// @Router /api/v1/memory/list [get]
func (a *MemoryAPI) GetList(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	var req request.GetMemoryListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.UserID = userID.(string)
	req.Page, req.PageSize = normalizePagination(req.Page, req.PageSize, 20)

	result, total, err := a.MemoryService.GetMemoryList(req)
	if err != nil {
		if failMemoryCharacterAccess(c, err) {
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
		"has_more":  hasMore(req.Page, req.PageSize, total),
	})
}

// GetStats 获取记忆统计
// @Summary 获取记忆统计
// @Tags 记忆模块
// @Accept json
// @Produce json
// @Param character_id query string true "角色ID"
// @Success 200 {object} response.MemoryStatsResp
// @Router /api/v1/memory/stats [get]
func (a *MemoryAPI) GetStats(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}
	characterID := c.Query("character_id")

	if characterID == "" {
		failMissingField(c, "character_id")
		return
	}

	result, err := a.MemoryService.GetMemoryStats(userID.(string), characterID)
	if err != nil {
		if failMemoryCharacterAccess(c, err) {
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// Add 添加记忆
// @Summary 添加记忆
// @Tags 记忆模块
// @Accept json
// @Produce json
// @Param data body request.AddMemoryReq true "请求参数"
// @Success 200 {object} response.MemoryResp
// @Router /api/v1/memory/add [post]
func (a *MemoryAPI) Add(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	var req request.AddMemoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "参数错误: "+err.Error())
		return
	}
	req.UserID = userID.(string)
	req.Source = "manual"

	result, err := a.MemoryService.AddMemory(req)
	if err != nil {
		if failMemoryCharacterAccess(c, err) {
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// Update 更新记忆
// @Summary 更新记忆
// @Tags 记忆模块
// @Accept json
// @Produce json
// @Param memory_id path string true "记忆ID"
// @Param data body request.UpdateMemoryReq true "请求参数"
// @Success 200 {object} response.Response
// @Router /api/v1/memory/{memory_id} [put]
func (a *MemoryAPI) Update(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	memoryID := c.Param("memory_id")

	var req request.UpdateMemoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}

	err := a.MemoryService.UpdateMemory(memoryID, userID.(string), req.Content)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failMemoryNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// Delete 删除记忆
// @Summary 删除记忆
// @Tags 记忆模块
// @Accept json
// @Produce json
// @Param memory_id path string true "记忆ID"
// @Success 200 {object} response.Response
// @Router /api/v1/memory/{memory_id} [delete]
func (a *MemoryAPI) Delete(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	memoryID := c.Param("memory_id")

	err := a.MemoryService.DeleteMemory(memoryID, userID.(string))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failMemoryNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

func failMemoryNotFound(c *gin.Context) {
	response.FailWithError(c, http.StatusNotFound, "memory not found", "memory_not_found", gin.H{
		"field":  "memory_id",
		"reason": "not_found_or_not_owned",
	})
}

// Search 搜索记忆
// @Summary 搜索记忆
// @Tags 记忆模块
// @Accept json
// @Produce json
// @Param character_id query string true "角色ID"
// @Param q query string true "搜索关键词"
// @Success 200 {object} response.MemoryResp
// @Router /api/v1/memory/search [get]
func (a *MemoryAPI) Search(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	var req request.SearchMemoryReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.UserID = userID.(string)

	result, err := a.MemoryService.SearchMemory(req)
	if err != nil {
		if failMemoryCharacterAccess(c, err) {
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

func failMemoryCharacterAccess(c *gin.Context, err error) bool {
	if !errors.Is(err, service.ErrCharacterUnavailable) {
		return false
	}
	response.FailWithError(c, http.StatusNotFound, "character unavailable", "character_unavailable", gin.H{
		"field":  "character_id",
		"reason": "not_found_or_not_owned",
	})
	return true
}
