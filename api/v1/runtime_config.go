package v1

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

type RuntimeConfigAPI struct {
	RuntimeConfigService *service.RuntimeConfigService
}

func NewRuntimeConfigAPI(runtimeConfigService *service.RuntimeConfigService) *RuntimeConfigAPI {
	return &RuntimeConfigAPI{RuntimeConfigService: runtimeConfigService}
}

func (a *RuntimeConfigAPI) Public(c *gin.Context) {
	if a.RuntimeConfigService == nil {
		response.FailWithError(c, http.StatusInternalServerError, "runtime config service unavailable", "runtime_config_unavailable", nil)
		return
	}
	response.OkWithData(c, a.RuntimeConfigService.PublicConfig())
}

func (a *RuntimeConfigAPI) List(c *gin.Context) {
	if !a.requireAdmin(c) {
		return
	}
	reveal := c.Query("reveal") == "true"
	result, err := a.RuntimeConfigService.List(reveal)
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "list runtime configs failed", "runtime_config_list_failed", nil)
		return
	}
	response.OkWithData(c, result)
}

func (a *RuntimeConfigAPI) Get(c *gin.Context) {
	if !a.requireAdmin(c) {
		return
	}
	key := c.Param("key")
	reveal := c.Query("reveal") == "true"
	result, err := a.RuntimeConfigService.GetResponse(key, reveal)
	if err != nil {
		if errors.Is(err, service.ErrRuntimeConfigNotFound) {
			response.FailWithError(c, http.StatusNotFound, "runtime config not found", "runtime_config_not_found", nil)
			return
		}
		response.FailWithError(c, http.StatusInternalServerError, "get runtime config failed", "runtime_config_get_failed", nil)
		return
	}

	response.OkWithData(c, result)
}

func (a *RuntimeConfigAPI) Upsert(c *gin.Context) {
	if !a.requireAdmin(c) {
		return
	}

	var req request.UpsertRuntimeConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	sensitive := true
	if req.Sensitive != nil {
		sensitive = *req.Sensitive
	}

	result, err := a.RuntimeConfigService.Upsert(c.Param("key"), req.Value, sensitive, req.Description)
	if err != nil {
		response.FailWithError(c, http.StatusBadRequest, err.Error(), "runtime_config_upsert_failed", nil)
		return
	}
	response.OkWithData(c, result)
}

func (a *RuntimeConfigAPI) requireAdmin(c *gin.Context) bool {
	expected := os.Getenv("ADMIN_CONFIG_TOKEN")
	if expected == "" && a.RuntimeConfigService != nil {
		if value, err := a.RuntimeConfigService.Get("admin.config_token"); err == nil {
			expected = value
		}
	}
	if expected == "" {
		response.FailWithError(c, http.StatusForbidden, "admin config token is not configured", "admin_config_token_not_configured", nil)
		return false
	}

	token := c.GetHeader("X-Admin-Token")
	if token == "" {
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	if token != expected {
		response.FailWithError(c, http.StatusUnauthorized, "invalid admin config token", "invalid_admin_config_token", nil)
		return false
	}
	return true
}
