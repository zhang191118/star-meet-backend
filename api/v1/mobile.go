package v1

import (
	"errors"
	"net/http"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

type MobileAPI struct {
	MobileService *service.MobileService
}

func NewMobileAPI(mobileService *service.MobileService) *MobileAPI {
	return &MobileAPI{MobileService: mobileService}
}

func (a *MobileAPI) GetHome(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		failUnauthenticated(c)
		return
	}

	result, err := a.MobileService.GetHome(userID.(string))
	if err != nil {
		if errors.Is(err, service.ErrMobileUserNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user not found", "user_not_found", nil)
			return
		}
		if errors.Is(err, service.ErrMobileWalletUnavailable) {
			response.FailWithError(c, http.StatusInternalServerError, "wallet unavailable", "wallet_unavailable", nil)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

func (a *MobileAPI) GetDashboard(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		failUnauthenticated(c)
		return
	}

	result, err := a.MobileService.GetDashboard(userID.(string))
	if err != nil {
		if errors.Is(err, service.ErrMobileUserNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user not found", "user_not_found", nil)
			return
		}
		if errors.Is(err, service.ErrMobileWalletUnavailable) {
			response.FailWithError(c, http.StatusInternalServerError, "wallet unavailable", "wallet_unavailable", nil)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

func (a *MobileAPI) PostBatch(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		failUnauthenticated(c)
		return
	}

	var req service.MobileBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithError(c, http.StatusBadRequest, "invalid JSON body", "invalid_json", gin.H{
			"field":  "body",
			"reason": err.Error(),
		})
		return
	}
	if invalidInclude := invalidMobileBatchIncludes(req.Include); len(invalidInclude) > 0 {
		response.FailWithError(c, http.StatusUnprocessableEntity, "invalid include keys", "validation_failed", gin.H{
			"invalid_include": invalidInclude,
			"allowed_include": []string{"characters", "membership", "wallet"},
		})
		return
	}

	result, err := a.MobileService.GetBatch(userID.(string), req)
	if err != nil {
		if errors.Is(err, service.ErrMobileUserNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user not found", "user_not_found", nil)
			return
		}
		if errors.Is(err, service.ErrMobileWalletUnavailable) {
			response.FailWithError(c, http.StatusInternalServerError, "wallet unavailable", "wallet_unavailable", nil)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

func invalidMobileBatchIncludes(include []string) []string {
	allowed := map[string]bool{
		"characters": true,
		"membership": true,
		"wallet":     true,
	}
	seen := make(map[string]bool)
	invalid := make([]string, 0)
	for _, item := range include {
		if item == "" || allowed[item] || seen[item] {
			continue
		}
		seen[item] = true
		invalid = append(invalid, item)
	}
	return invalid
}
