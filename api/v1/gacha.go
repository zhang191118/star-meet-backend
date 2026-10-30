package v1

import (
	"errors"
	"net/http"
	"star-meet-backend/global"
	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/service"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type GachaAPI struct {
	GachaService *service.GachaService
}

func NewGachaAPI() *GachaAPI {
	svc := service.NewGachaService(global.GVA_DB)
	svc.Notification = global.NotificationService
	return &GachaAPI{GachaService: svc}
}

func (a *GachaAPI) GetPools(c *gin.Context) {
	pools, err := a.GachaService.GetPools()
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if pools == nil {
		pools = []database.GachaPool{}
	}
	response.OkWithData(c, pools)
}

func (a *GachaAPI) GetCharacters(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	characters, err := a.GachaService.GetUserCharacters(userIDStr)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if characters == nil {
		characters = []database.UserCharacter{}
	}
	response.OkWithData(c, characters)
}

func (a *GachaAPI) GetHistory(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	if c.Query("page_size") != "" {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		page, pageSize = normalizePagination(page, pageSize, 20)

		records, total, err := a.GachaService.GetUserGachaHistoryPage(userIDStr, page, pageSize)
		if err != nil {
			response.Fail(c, http.StatusInternalServerError, err.Error())
			return
		}
		if records == nil {
			records = []database.UserGachaRecord{}
		}
		response.OkWithData(c, gin.H{
			"list":      records,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
			"has_more":  hasMore(page, pageSize, total),
		})
		return
	}

	records, err := a.GachaService.GetUserGachaHistory(userIDStr, 50)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []database.UserGachaRecord{}
	}
	response.OkWithData(c, records)
}

func (a *GachaAPI) Draw(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	var req struct {
		PoolID       string `json:"pool_id"`
		Count        int    `json:"count"`
		ClientDrawID string `json:"client_draw_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithError(c, http.StatusBadRequest, "invalid JSON body", "invalid_json", gin.H{
			"field":  "body",
			"reason": err.Error(),
		})
		return
	}

	// 如果未指定pool_id，使用默认卡池
	if req.PoolID == "" {
		req.PoolID = "starter"
	}
	if req.Count == 0 {
		req.Count = 1
	}
	if req.Count != 1 && req.Count != 10 {
		req.Count = 1
	}

	idempotent := false
	var result *service.DrawResult
	var err error
	if req.ClientDrawID != "" {
		result, idempotent, err = a.GachaService.DrawWithClientDrawID(userIDStr, req.PoolID, req.Count, req.ClientDrawID)
		if err != nil {
			if errors.Is(err, service.ErrGachaIdempotencyConflict) {
				response.FailWithError(c, http.StatusConflict, "client_draw_id conflicts with an existing draw", "gacha_idempotency_conflict", nil)
				return
			}
			if errors.Is(err, service.ErrGachaPoolUnavailable) {
				response.FailWithError(c, http.StatusNotFound, "gacha pool unavailable", "gacha_pool_unavailable", nil)
				return
			}
			if errors.Is(err, service.ErrGachaPoolUnconfigured) {
				response.FailWithError(c, http.StatusUnprocessableEntity, "gacha pool is not configured for drawing", "gacha_pool_unconfigured", nil)
				return
			}
			if errors.Is(err, service.ErrInsufficientMoonBalance) {
				response.FailWithError(c, http.StatusBadRequest, "insufficient moon balance", "insufficient_moon_balance", nil)
				return
			}
			response.Fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		result, err = a.GachaService.Draw(userIDStr, req.PoolID, req.Count)
		if err != nil {
			if errors.Is(err, service.ErrGachaPoolUnavailable) {
				response.FailWithError(c, http.StatusNotFound, "gacha pool unavailable", "gacha_pool_unavailable", nil)
				return
			}
			if errors.Is(err, service.ErrGachaPoolUnconfigured) {
				response.FailWithError(c, http.StatusUnprocessableEntity, "gacha pool is not configured for drawing", "gacha_pool_unconfigured", nil)
				return
			}
			if errors.Is(err, service.ErrInsufficientMoonBalance) {
				response.FailWithError(c, http.StatusBadRequest, "insufficient moon balance", "insufficient_moon_balance", nil)
				return
			}
			response.Fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	remainingMoon, err := a.GachaService.GetUserMoonBalance(userIDStr)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OkWithData(c, gin.H{
		"records": result.Records, "characters": result.Characters, "remaining_moon": remainingMoon, "idempotent": idempotent,
	})
}

func (a *GachaAPI) ToggleFavorite(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	var req struct {
		CharacterID string `json:"character_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}
	if err := a.GachaService.ToggleFavorite(userIDStr, req.CharacterID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user character not found", "user_character_not_found", gin.H{
				"field":  "character_id",
				"reason": "not_found_or_not_owned",
			})
			return
		}
		response.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.OkWithData(c, nil)
}
