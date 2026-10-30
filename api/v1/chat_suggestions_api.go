package v1

import (
	"errors"
	"net/http"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

// GetSuggestions 获取智能回复建议
// @Summary 获取智能回复建议
// @Tags 聊天模块
// @Accept json
// @Produce json
// @Param character_id query string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/chat/suggestions [get]
func (a *ChatAPI) GetSuggestions(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		failUnauthenticated(c)
		return
	}

	characterID := c.Query("character_id")
	if characterID == "" {
		response.FailWithError(c, http.StatusBadRequest, "character_id is required", "missing_character_id", nil)
		return
	}

	lang := i18n.NormalizeLang(resolveLang(c))
	suggestions, err := a.ChatService.GetSuggestions(userID.(string), characterID, lang)
	if err != nil {
		if errors.Is(err, service.ErrChatCharacterBlocked) {
			failChatCharacterBlocked(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"suggestions": suggestions,
	})
}
