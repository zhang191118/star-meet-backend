package v1

import (
	"errors"
	"fmt"
	"io"
	"star-meet-backend/core/oss"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"
	"star-meet-backend/pkg/utils"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type CharacterAPI struct {
	CharacterService       *service.CharacterService
	CharacterConfigService *service.CharacterConfigService
}

func NewCharacterAPI(charService *service.CharacterService) *CharacterAPI {
	return &CharacterAPI{CharacterService: charService}
}

// Create 创建角色
// @Summary 创建角色
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param data body request.CreateCharacterReq true "请求参数"
// @Success 200 {object} response.CharacterResp
// @Router /api/v1/character/create [post]
func (a *CharacterAPI) Create(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	var req request.CreateCharacterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "参数错误: "+err.Error())
		return
	}

	req.UserID = userID.(string)

	result, err := a.CharacterService.CreateCharacter(req, i18n.NormalizeLang(resolveLang(c)))
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// UploadImage 上传角色图片
// @Summary 上传角色图片
// @Tags 角色模块
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "图片文件"
// @Success 200 {object} response.Response
// @Router /api/v1/character/upload-image [post]
func (a *CharacterAPI) UploadImage(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	file, err := c.FormFile("file")
	if err != nil {
		response.FailWithMessage(c, "请选择图片文件")
		return
	}

	buffer, ext, contentType, err := readImageUpload(file)
	if err != nil {
		response.FailWithError(c, http.StatusUnprocessableEntity, imageSizeError(), "invalid_image", nil)
		return
	}

	objectName := fmt.Sprintf("characters/%s_%d.%s", userIDStr, time.Now().UnixNano(), ext)
	ossClient, err := oss.NewOSSClient()
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "OSS配置不完整，无法上传图片", "oss_not_configured", err)
		return
	}

	url, uploadErr := ossClient.Upload(objectName, buffer, contentType)
	if uploadErr != nil {
		response.FailWithError(c, http.StatusInternalServerError, "图片上传到OSS失败", "oss_upload_failed", uploadErr)
		return
	}

	response.OkWithData(c, gin.H{
		"url": url,
	})
}

// ImportImage 导入远程图片到OSS
// @Summary 导入远程图片到OSS
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param url formData string true "远程图片URL"
// @Success 200 {object} response.Response
// @Router /api/v1/character/import-image [post]
func (a *CharacterAPI) ImportImage(c *gin.Context) {
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	var req struct {
		URL string `json:"url" form:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "请提供有效的图片URL")
		return
	}

	imageURL := strings.TrimSpace(req.URL)
	if imageURL == "" {
		response.FailWithMessage(c, "图片URL不能为空")
		return
	}

	if _, err := publicImageURL(imageURL); err != nil {
		response.FailWithError(c, http.StatusUnprocessableEntity, "仅支持公网 HTTPS 图片地址", "invalid_image_url", nil)
		return
	}

	// 下载远程图片
	httpClient := safeImageHTTPClient()
	resp, err := httpClient.Get(imageURL)
	if err != nil {
		response.FailWithError(c, http.StatusBadGateway, "下载远程图片失败", "remote_image_download_failed", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		response.FailWithError(c, http.StatusBadGateway, fmt.Sprintf("远程图片返回状态码: %d", resp.StatusCode), "remote_image_unavailable", nil)
		return
	}

	buffer, err := io.ReadAll(io.LimitReader(resp.Body, maxImageUploadBytes+1))
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "读取远程图片数据失败", "remote_image_read_failed", err)
		return
	}
	if int64(len(buffer)) > maxImageUploadBytes {
		response.FailWithError(c, http.StatusUnprocessableEntity, imageSizeError(), "invalid_image", nil)
		return
	}
	_, ext, contentType, err := validateImageBytes(buffer)
	if err != nil {
		response.FailWithError(c, http.StatusUnprocessableEntity, imageSizeError(), "invalid_image", nil)
		return
	}

	objectName := fmt.Sprintf("characters/%s_%d.%s", userIDStr, time.Now().UnixNano(), ext)

	ossClient, err := oss.NewOSSClient()
	if err != nil {
		response.FailWithError(c, http.StatusInternalServerError, "OSS配置不完整，无法导入图片", "oss_not_configured", err)
		return
	}

	ossURL, uploadErr := ossClient.Upload(objectName, buffer, contentType)
	if uploadErr != nil {
		response.FailWithError(c, http.StatusInternalServerError, "图片上传到OSS失败", "oss_upload_failed", uploadErr)
		return
	}

	response.OkWithData(c, gin.H{
		"url": ossURL,
	})
}

// GetDetail 获取角色详情
// @Summary 获取角色详情
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Success 200 {object} response.CharacterResp
// @Router /api/v1/character/{character_id} [get]
func (a *CharacterAPI) GetDetail(c *gin.Context) {
	characterID := c.Param("character_id")

	lang := resolveProfileLang(c)
	result, err := a.CharacterService.GetCharacterDetail(optionalAccessUserID(c), characterID, lang)
	if err != nil {
		if errors.Is(err, service.ErrCharacterBlocked) {
			response.FailWithError(c, http.StatusForbidden, "character blocked", "character_blocked", nil)
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Fail(c, 404, "角色不存在")
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// GetList 获取我的角色列表
// @Summary 获取我的角色列表
// @Tags 角色模块
// @Accept json
// @Produce json
// @Success 200 {object} response.CharacterResp
// @Router /api/v1/character/list [get]
func (a *CharacterAPI) GetList(c *gin.Context) {
	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	var req request.GetCharacterListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.UserID = userID.(string)
	req.Page, req.PageSize = normalizePagination(req.Page, req.PageSize, 20)

	lang := resolveProfileLang(c)
	result, total, err := a.CharacterService.GetCharacterList(req, lang)
	if err != nil {
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

// GetPublicList 获取公开角色列表（广场）
// @Summary 获取公开角色列表
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param category query string false "分类"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Param sort query string false "排序"
// @Success 200 {object} response.PublicCharacterResp
// @Router /api/v1/character/public [get]
func (a *CharacterAPI) GetPublicList(c *gin.Context) {
	var req request.GetPublicCharactersReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.Page, req.PageSize = normalizePagination(req.Page, req.PageSize, 20)

	req.UserID = optionalAccessUserID(c)

	lang := resolveProfileLang(c)
	result, total, err := a.CharacterService.GetPublicList(req, lang)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"list":           result,
		"total":          total,
		"page":           req.Page,
		"page_size":      req.PageSize,
		"has_more":       hasMore(req.Page, req.PageSize, total),
		"before_id":      req.BeforeID,
		"next_before_id": nextPublicCharacterBeforeID(result),
	})
}

func nextPublicCharacterBeforeID(characters []response.PublicCharacterResp) string {
	if len(characters) == 0 {
		return ""
	}
	return characters[len(characters)-1].CharacterID
}

// GetMyCharacters 获取用户的私有角色列表
// @Summary 获取用户的私有角色列表
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.Response
// @Router /api/v1/character/my [get]
func (a *CharacterAPI) GetMyCharacters(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = normalizePagination(page, pageSize, 20)

	lang := resolveProfileLang(c)
	result, total, err := a.CharacterService.GetMyCharacters(userID.(string), page, pageSize, lang)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"list":      result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"has_more":  hasMore(page, pageSize, total),
	})
}

// Encounter 邂逅角色
// @Summary 邂逅角色（将公共角色转为用户私有角色）
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/character/{character_id}/encounter [post]
func (a *CharacterAPI) Encounter(c *gin.Context) {
	characterID := c.Param("character_id")
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	if characterID == "" {
		response.FailWithMessage(c, "缺少角色ID")
		return
	}

	err := a.CharacterService.EncounterCharacter(userID.(string), characterID, i18n.NormalizeLang(resolveLang(c)))
	if err != nil {
		if errors.Is(err, service.ErrCharacterBlocked) {
			response.FailWithError(c, http.StatusForbidden, "character blocked", "character_blocked", nil)
			return
		}
		if errors.Is(err, service.ErrCharacterUnavailable) {
			response.FailWithError(c, 404, "character unavailable", "character_unavailable", gin.H{
				"field":  "character_id",
				"reason": "not_public_or_missing",
			})
			return
		}
		if errors.Is(err, service.ErrCharacterAlreadyEncountered) {
			response.FailWithError(c, 409, "character already encountered", "character_already_encountered", gin.H{
				"field":  "character_id",
				"reason": "already_encountered",
			})
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithMessage(c, "邂逅成功！")
}

// Update 更新角色
// @Summary 更新角色
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Param data body request.UpdateCharacterReq true "请求参数"
// @Success 200 {object} response.Response
// @Router /api/v1/character/{character_id} [put]
func (a *CharacterAPI) Update(c *gin.Context) {
	characterID := c.Param("character_id")

	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	var req request.UpdateCharacterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.UserID = userID.(string)
	req.CharacterID = characterID

	err := a.CharacterService.UpdateCharacter(characterID, req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failCharacterNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// Delete 删除角色
// @Summary 删除角色
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/character/{character_id} [delete]
func (a *CharacterAPI) Delete(c *gin.Context) {
	characterID := c.Param("character_id")

	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	err := a.CharacterService.DeleteCharacter(userID.(string), characterID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			failCharacterNotFound(c)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

func failCharacterNotFound(c *gin.Context) {
	response.FailWithError(c, 404, "character not found", "character_not_found", gin.H{
		"field":  "character_id",
		"reason": "not_found_or_not_owned",
	})
}

// Favorite 收藏角色
// @Summary 收藏角色
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/character/{character_id}/favorite [post]
func (a *CharacterAPI) Favorite(c *gin.Context) {
	characterID := c.Param("character_id")

	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	err := a.CharacterService.FavoriteCharacter(userID.(string), characterID)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// Unfavorite 取消收藏
// @Summary 取消收藏
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/character/{character_id}/unfavorite [post]
func (a *CharacterAPI) Unfavorite(c *gin.Context) {
	characterID := c.Param("character_id")

	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}

	err := a.CharacterService.UnfavoriteCharacter(userID.(string), characterID)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// InitDefault 初始化默认角色
func (a *CharacterAPI) InitDefault(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	lang := i18n.NormalizeLang(resolveLang(c))
	result, err := a.CharacterService.InitDefaultCharacter(userID.(string), lang)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// InitPresets 初始化预设角色列表
func (a *CharacterAPI) InitPresets(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	lang := i18n.NormalizeLang(resolveLang(c))
	results, err := a.CharacterService.InitPresetCharacters(userID.(string), lang)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	if len(results) == 0 {
		response.OkWithMessage(c, "预设角色已存在，无需重复添加")
		return
	}

	response.OkWithData(c, gin.H{
		"list":  results,
		"total": len(results),
	})
}

// Search 搜索角色
// @Summary 搜索角色
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param keyword query string false "关键词"
// @Param category query string false "分类"
// @Param gender query string false "性别"
// @Param personality query string false "性格"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.CharacterResp
// @Router /api/v1/character/search [get]
func (a *CharacterAPI) Search(c *gin.Context) {
	var req request.SearchCharacterReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}

	// 兼容 name 参数（前端可能用name而不是keyword）
	if req.Keyword == "" && c.Query("name") != "" {
		req.Keyword = c.Query("name")
	}
	req.Page, req.PageSize = normalizePagination(req.Page, req.PageSize, 20)

	req.UserID = optionalAccessUserID(c)

	lang := resolveProfileLang(c)
	result, total, err := a.CharacterService.SearchCharacter(req, lang)
	if err != nil {
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

// GetRecommend 获取推荐角色
// @Summary 获取推荐角色
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} response.CharacterResp
// @Router /api/v1/character/recommend [get]
func (a *CharacterAPI) GetRecommend(c *gin.Context) {
	var req request.GetRecommendReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}
	req.Page, req.PageSize = normalizePagination(req.Page, req.PageSize, 20)
	req.UserID = optionalAccessUserID(c)

	lang := resolveProfileLang(c)
	result, total, err := a.CharacterService.GetRecommend(req, lang)
	if err != nil {
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

func optionalAccessUserID(c *gin.Context) string {
	if value, exists := c.Get("user_id"); exists {
		if userID, ok := value.(string); ok && userID != "" {
			return userID
		}
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}
	claims, err := utils.ParseToken(parts[1])
	if err != nil || claims.UserID == "" || (claims.TokenType != "" && claims.TokenType != "access") {
		return ""
	}
	return claims.UserID
}

// GetCategories 获取角色分类
// @Summary 获取角色分类
// @Tags 角色模块
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/character/categories [get]
func (a *CharacterAPI) GetCategories(c *gin.Context) {
	lang := i18n.NormalizeLang(resolveLang(c))
	response.OkWithData(c, a.CharacterService.GetDiscoverCategories(lang))
}

// GetConfig 获取角色配置
// @Summary 获取角色配置
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Success 200 {object} response.Response
// @Router /api/v1/character/{character_id}/config [get]
func (a *CharacterAPI) GetConfig(c *gin.Context) {
	characterID := c.Param("character_id")
	userID, _ := c.Get("user_id")

	result, err := a.CharacterService.GetCharacterConfig(userID.(string), characterID)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// UpdateConfig 更新角色配置
// @Summary 更新角色配置
// @Tags 角色模块
// @Accept json
// @Produce json
// @Param character_id path string true "角色ID"
// @Param data body request.CharacterConfigReq true "请求参数"
// @Success 200 {object} response.Response
// @Router /api/v1/character/{character_id}/config [put]
func (a *CharacterAPI) UpdateConfig(c *gin.Context) {
	characterID := c.Param("character_id")
	userID, _ := c.Get("user_id")

	var req request.CharacterConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithMessage(c, "参数错误")
		return
	}

	err := a.CharacterService.UpdateCharacterConfig(userID.(string), characterID, req)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.Ok(c)
}

// GetTagConfig returns character tag config (tag groups + gender + scenes)
func (api *CharacterAPI) GetTagConfig(c *gin.Context) {
	configService := api.CharacterConfigService
	if configService == nil {
		configService = &service.CharacterConfigService{}
	}
	c.JSON(200, gin.H{"errNo": 0, "errMsg": "succ", "data": configService.GetConfig()})
}

// ResolveChat returns an idempotently resolved character owned by the authenticated user.
func (a *CharacterAPI) ResolveChat(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		failUnauthenticated(c)
		return
	}
	character, err := a.CharacterService.ResolveChatCharacter(userID.(string), c.Param("character_id"), resolveProfileLang(c))
	if err != nil {
		if errors.Is(err, service.ErrCharacterBlocked) {
			response.FailWithError(c, 403, "character blocked", "character_blocked", nil)
		} else if errors.Is(err, service.ErrCharacterUnavailable) || errors.Is(err, gorm.ErrRecordNotFound) {
			response.FailWithError(c, 404, "character unavailable", "character_unavailable", nil)
		} else {
			response.FailWithError(c, 500, "could not open character", "character_resolve_failed", nil)
		}
		return
	}
	response.OkWithData(c, character)
}
