package v1

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"star-meet-backend/core/oss"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type CharacterAPI struct {
	CharacterService *service.CharacterService
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

	result, err := a.CharacterService.CreateCharacter(req)
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

	src, err := file.Open()
	if err != nil {
		response.FailWithMessage(c, "文件打开失败")
		return
	}
	defer src.Close()

	buffer, err := io.ReadAll(src)
	if err != nil {
		response.FailWithMessage(c, "文件读取失败")
		return
	}

	ext := "jpg"
	filename := strings.ToLower(file.Filename)
	for _, candidate := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp"} {
		if strings.HasSuffix(filename, candidate) {
			ext = strings.TrimPrefix(candidate, ".")
			break
		}
	}

	objectName := fmt.Sprintf("characters/%s_%d.%s", userIDStr, time.Now().UnixNano(), ext)
	contentType := "image/" + ext
	if ext == "jpg" {
		contentType = "image/jpeg"
	}

	imageURL := "/uploads/character/" + filepath.Base(objectName)

	ossClient, err := oss.NewOSSClient()
	if err == nil {
		if url, uploadErr := ossClient.Upload(objectName, buffer, contentType); uploadErr == nil {
			imageURL = url
		}
	}

	if strings.HasPrefix(imageURL, "/") {
		localDir := "./uploads/character"
		if err := os.MkdirAll(localDir, 0755); err != nil {
			response.FailWithMessage(c, "创建上传目录失败")
			return
		}
		localPath := filepath.Join(localDir, filepath.Base(objectName))
		if err := os.WriteFile(localPath, buffer, 0644); err != nil {
			response.FailWithMessage(c, "保存图片失败")
			return
		}
	}

	response.OkWithData(c, gin.H{
		"url": imageURL,
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

	result, err := a.CharacterService.GetCharacterDetail(characterID)
	if err != nil {
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

	result, total, err := a.CharacterService.GetCharacterList(req)
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

	result, total, err := a.CharacterService.GetPublicList(req)
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

	result, total, err := a.CharacterService.GetMyCharacters(userID.(string), page, pageSize)
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

	err := a.CharacterService.EncounterCharacter(userID.(string), characterID)
	if err != nil {
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

	result, err := a.CharacterService.InitDefaultCharacter(userID.(string))
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

	results, err := a.CharacterService.InitPresetCharacters(userID.(string))
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

	result, total, err := a.CharacterService.SearchCharacter(req)
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

	result, total, err := a.CharacterService.GetRecommend(req)
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

// GetCategories 获取角色分类
// @Summary 获取角色分类
// @Tags 角色模块
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/character/categories [get]
func (a *CharacterAPI) GetCategories(c *gin.Context) {
	categories := []map[string]interface{}{
		{"id": "恋人", "name": "恋人", "icon": "heart.fill", "count": 0},
		{"id": "朋友", "name": "朋友", "icon": "person.2.fill", "count": 0},
		{"id": "导师", "name": "导师", "icon": "book.fill", "count": 0},
		{"id": "虚拟偶像", "name": "虚拟偶像", "icon": "star.fill", "count": 0},
		{"id": "萌宠", "name": "萌宠", "icon": "pawprint.fill", "count": 0},
		{"id": "二次元", "name": "二次元", "icon": "sparkles", "count": 0},
		{"id": "医生", "name": "医生", "icon": "cross.case.fill", "count": 0},
		{"id": "律师", "name": "律师", "icon": "scale.3d", "count": 0},
		{"id": "教师", "name": "教师", "icon": "graduationcap.fill", "count": 0},
		{"id": "警察", "name": "警察", "icon": "shield.fill", "count": 0},
		{"id": "咖啡师", "name": "咖啡师", "icon": "cup.and.saucer.fill", "count": 0},
		{"id": "灵魂伴侣", "name": "灵魂伴侣", "icon": "sparkling", "count": 0},
		{"id": "知己", "name": "知己", "icon": "heart.circle.fill", "count": 0},
		{"id": "青梅竹马", "name": "青梅竹马", "icon": "figure.child", "count": 0},
	}

	// 获取每个分类的角色数量
	for i := range categories {
		var count int64
		a.CharacterService.DB.Model(&database.Character{}).
			Where("relationship = ? AND is_public = ?", categories[i]["id"], true).
			Count(&count)
		categories[i]["count"] = count
	}

	response.OkWithData(c, categories)
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
