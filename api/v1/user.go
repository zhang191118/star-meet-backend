package v1

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"star-meet-backend/core/oss"
	"star-meet-backend/model/request"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/logger"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
)

type UserAPI struct {
	UserService       *service.UserService
	VerifyCodeService *service.VerifyCodeService
}

func NewUserAPI(userService *service.UserService, verifyCodeService *service.VerifyCodeService) *UserAPI {
	return &UserAPI{UserService: userService, VerifyCodeService: verifyCodeService}
}

// Register 用户注册
// @Summary 用户注册
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.RegisterReq true "请求参数"
// @Success 200 {object} response.LoginResp
// @Router /api/v1/user/register [post]
func (a *UserAPI) Register(c *gin.Context) {
	var req request.RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	result, err := a.UserService.Register(req)
	if err != nil {
		if errors.Is(err, service.ErrEmailRegistered) {
			response.FailWithError(c, http.StatusConflict, "email already registered", "email_already_registered", gin.H{
				"field": "email",
			})
			return
		}
		if errors.Is(err, service.ErrVerifyCodeExpired) {
			response.FailWithError(c, http.StatusBadRequest, "verify code expired", "verify_code_expired", gin.H{
				"field": "code",
			})
			return
		}
		if errors.Is(err, service.ErrInvalidVerifyCode) {
			response.FailWithError(c, http.StatusBadRequest, "invalid verify code", "invalid_verify_code", gin.H{
				"field": "code",
			})
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// Login 用户登录
// @Summary 用户登录
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.LoginReq true "请求参数"
// @Success 200 {object} response.LoginResp
// @Router /api/v1/user/login [post]
func (a *UserAPI) Login(c *gin.Context) {
	var req request.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	result, err := a.UserService.Login(req)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user not found", "user_not_found", nil)
			return
		}
		if errors.Is(err, service.ErrInvalidCredentials) {
			response.FailWithError(c, http.StatusUnauthorized, "invalid credentials", "invalid_credentials", nil)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// WechatLogin 微信登录
// @Summary 微信登录
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.WechatLoginReq true "请求参数"
// @Success 200 {object} response.LoginResp
// @Router /api/v1/user/wechat-login [post]
func (a *UserAPI) WechatLogin(c *gin.Context) {
	var req request.WechatLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	result, err := a.UserService.WechatLogin(req)
	if err != nil {
		if errors.Is(err, service.ErrThirdPartyUnavailable) {
			response.FailWithError(c, http.StatusServiceUnavailable, "微信登录服务暂不可用", "third_party_login_unavailable", nil)
			return
		}
		if errors.Is(err, service.ErrDatabaseOperation) {
			response.FailWithError(c, http.StatusInternalServerError, "服务器内部错误，请稍后重试", "database_error", nil)
			return
		}
		response.FailWithError(c, http.StatusUnauthorized, "微信登录验证失败", "invalid_third_party_token", nil)
		return
	}

	response.OkWithData(c, result)
}

// AppleLogin 苹果登录
// @Summary 苹果登录
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.AppleLoginReq true "请求参数"
// @Success 200 {object} response.LoginResp
// @Router /api/v1/user/apple-login [post]
func (a *UserAPI) AppleLogin(c *gin.Context) {
	var req request.AppleLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	result, err := a.UserService.AppleLogin(req)
	if err != nil {
		if errors.Is(err, service.ErrThirdPartyUnavailable) {
			response.FailWithError(c, http.StatusServiceUnavailable, "Apple 登录服务暂不可用", "third_party_login_unavailable", nil)
			return
		}
		if errors.Is(err, service.ErrDatabaseOperation) {
			response.FailWithError(c, http.StatusInternalServerError, "服务器内部错误，请稍后重试", "database_error", nil)
			return
		}
		response.FailWithError(c, http.StatusUnauthorized, "Apple 登录验证失败", "invalid_third_party_token", nil)
		return
	}

	response.OkWithData(c, result)
}

// @Summary LINE 登录
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.LineLoginReq true "请求参数"
// @Success 200 {object} response.LoginResp
// @Router /api/v1/user/line-login [post]
func (a *UserAPI) LineLogin(c *gin.Context) {
	var req request.LineLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	result, err := a.UserService.LineLogin(req)
	if err != nil {
		if errors.Is(err, service.ErrThirdPartyUnavailable) {
			response.FailWithError(c, http.StatusServiceUnavailable, "LINE 登录服务暂不可用", "third_party_login_unavailable", nil)
			return
		}
		if errors.Is(err, service.ErrDatabaseOperation) {
			response.FailWithError(c, http.StatusInternalServerError, "服务器内部错误，请稍后重试", "database_error", nil)
			return
		}
		response.FailWithError(c, http.StatusUnauthorized, "LINE 登录验证失败", "invalid_third_party_token", nil)
		return
	}

	response.OkWithData(c, result)
}

// DeleteAccount permanently deletes the authenticated user's account and data.
func (a *UserAPI) DeleteAccount(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		failUnauthenticated(c)
		return
	}

	var req request.DeleteAccountReq
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		failBindError(c, err, req)
		return
	}
	if strings.TrimSpace(req.Confirmation) != "DELETE" {
		response.FailWithError(c, http.StatusUnprocessableEntity, "请确认输入 DELETE 以永久删除账号", "account_delete_confirmation_required", gin.H{"field": "confirmation"})
		return
	}
	if err := a.UserService.DeleteAccount(userID); err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			failUnauthenticated(c)
			return
		}
		logger.Errorf("delete account failed for user %s: %v", userID, err)
		response.FailWithError(c, http.StatusInternalServerError, "账号删除失败，请稍后重试", "account_delete_failed", nil)
		return
	}
	response.OkWithMessage(c, "账号已删除")
}

// GetUserInfo 获取用户信息
// @Summary 获取用户信息
// @Tags 用户模块
// @Accept json
// @Produce json
// @Success 200 {object} response.UserResp
// @Router /api/v1/user/info [get]
func (a *UserAPI) GetUserInfo(c *gin.Context) {
	// 从JWT token中获取user_id
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	result, err := a.UserService.GetUserInfo(userID.(string))
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// UpdateUserInfo 更新用户信息
// @Summary 更新用户信息
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param nickname query string false "昵称"
// @Param avatar query string false "头像"
// @Param language query string false "语言"
// @Param region query string false "地区"
// @Success 200 {object} response.Response
// @Router /api/v1/user/info [put]
func (a *UserAPI) UpdateUserInfo(c *gin.Context) {
	// 从JWT中获取user_id
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	var req request.UpdateUserInfoReq
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		failBindError(c, err, req)
		return
	}

	if req.Nickname == "" {
		req.Nickname = c.Query("nickname")
	}
	if req.Avatar == "" {
		req.Avatar = c.Query("avatar")
	}
	if req.Language == "" {
		req.Language = c.Query("language")
	}
	if req.Region == "" {
		req.Region = c.Query("region")
	}
	if req.Gender == "" {
		req.Gender = c.Query("gender")
	}
	if req.Age == 0 {
		if ageStr := c.Query("age"); ageStr != "" {
			age, err := strconv.Atoi(ageStr)
			if err != nil {
				failValidationField(c, "age", "invalid_format")
				return
			}
			req.Age = age
		}
	}

	result, err := a.UserService.UpdateUserInfo(userIDStr, req)
	if err != nil {
		if failInvalidUserField(c, err) {
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// GetUserSettings 获取用户陪伴偏好设置
func (a *UserAPI) GetUserSettings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	result, err := a.UserService.GetUserSettings(userID.(string))
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// UpdateUserSettings 更新用户陪伴偏好设置
func (a *UserAPI) UpdateUserSettings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		failUnauthenticated(c)
		return
	}

	var req request.UpdateUserSettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	result, err := a.UserService.UpdateUserSettings(userID.(string), req)
	if err != nil {
		if failInvalidUserField(c, err) {
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// GetOrCreateUser 获取或创建用户（用于测试）
// @Summary 获取或创建用户
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param phone query string false "手机号"
// @Success 200 {object} response.UserResp
// @Router /api/v1/user/get-or-create [get]
func (a *UserAPI) GetOrCreateUser(c *gin.Context) {
	phone := c.DefaultQuery("phone", "13800000000")

	result, err := a.UserService.GetOrCreateUser(phone)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// CheckMemberStatus 检查会员状态
// @Summary 检查会员状态
// @Tags 用户模块
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/user/member-status [get]
func (a *UserAPI) CheckMemberStatus(c *gin.Context) {
	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	isMember, err := a.UserService.CheckMemberStatus(userIDStr)
	if err != nil {
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, gin.H{
		"is_member": isMember,
	})
}

// Logout 用户登出
// @Summary 用户登出
// @Tags 用户模块
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/user/logout [post]
func (a *UserAPI) Logout(c *gin.Context) {
	if _, exists := c.Get("user_id"); !exists {
		failUnauthenticated(c)
		return
	}

	// 清除 Token 等操作可以在这里实现
	response.OkWithMessage(c, "登出成功")
}

// RefreshToken 刷新Token
// @Summary 刷新Token
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.RefreshTokenReq true "请求参数"
// @Success 200 {object} response.LoginResp
// @Router /api/v1/user/refresh [post]
func (a *UserAPI) RefreshToken(c *gin.Context) {
	var req request.RefreshTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	result, err := a.UserService.RefreshToken(req.RefreshToken)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRefreshToken) {
			response.FailWithError(c, http.StatusUnauthorized, "invalid refresh token", "invalid_refresh_token", nil)
			return
		}
		if errors.Is(err, service.ErrUserNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user not found", "user_not_found", nil)
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithData(c, result)
}

// UploadAvatar 上传头像
// @Summary 上传头像
// @Tags 用户模块
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "头像文件"
// @Success 200 {object} response.Response
// @Router /api/v1/user/avatar [post]
func (a *UserAPI) UploadAvatar(c *gin.Context) {
	// 从JWT token获取user_id
	userID, _ := c.Get("user_id")
	if userID == nil {
		failUnauthenticated(c)
		return
	}
	userIDStr := userID.(string)

	file, err := c.FormFile("file")
	if err != nil {
		response.FailWithMessage(c, "请选择头像文件")
		return
	}

	buffer, ext, contentType, err := readImageUpload(file)
	if err != nil {
		response.FailWithError(c, http.StatusUnprocessableEntity, imageSizeError(), "invalid_image", nil)
		return
	}
	objectName := fmt.Sprintf("avatars/%s_%d.%s", userIDStr, time.Now().Unix(), ext)

	ossClient, err := oss.NewOSSClient()
	if err != nil {
		response.FailWithError(c, http.StatusServiceUnavailable, "头像上传服务暂不可用", "oss_not_configured", nil)
		return
	}
	avatarURL, err := ossClient.Upload(objectName, buffer, contentType)
	if err != nil {
		response.FailWithError(c, http.StatusBadGateway, "头像上传失败，请稍后重试", "oss_upload_failed", nil)
		return
	}

	// 更新用户头像
	_, err = a.UserService.UpdateUserInfo(userIDStr, request.UpdateUserInfoReq{
		Avatar: avatarURL,
	})
	if err != nil {
		response.FailWithMessage(c, "更新头像失败")
		return
	}

	response.OkWithData(c, gin.H{
		"avatar": avatarURL,
	})
}

// SendVerifyCode 发送验证码
// @Summary 发送验证码
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.SendVerifyCodeReq true "请求参数"
// @Success 200 {object} response.Response
// @Router /api/v1/user/send-verify-code [post]
func (a *UserAPI) SendVerifyCode(c *gin.Context) {
	var req request.SendVerifyCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	lang := resolveLang(c)
	err := a.VerifyCodeService.SendVerifyCode(req.Email, req.Type, lang)
	if err != nil {
		if errors.Is(err, service.ErrEmailRegistered) {
			response.FailWithError(c, http.StatusConflict, "email already registered", "email_already_registered", gin.H{
				"field": "email",
			})
			return
		}
		if errors.Is(err, service.ErrUserNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user not found", "user_not_found", gin.H{
				"field": "email",
			})
			return
		}
		if errors.Is(err, service.ErrVerifyCodeLimitExceeded) {
			var limitMsg string
			switch lang {
			case "zh-Hant":
				limitMsg = "此信箱今日發送次數已達上限，請明天再試"
			case "en":
				limitMsg = "Daily limit reached for this email. Please try again tomorrow."
			default:
				limitMsg = "此邮箱今日发送次数已达上限，请明天再试"
			}
			response.FailWithError(c, http.StatusTooManyRequests, limitMsg, "verify_code_limit", gin.H{
				"field": "email",
			})
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	response.OkWithMessage(c, "验证码已发送到您的邮箱")
}

// ResetPassword 重置密码
// @Summary 重置密码
// @Tags 用户模块
// @Accept json
// @Produce json
// @Param data body request.ResetPasswordReq true "请求参数"
// @Success 200 {object} response.Response
// @Router /api/v1/user/reset-password [post]
func (a *UserAPI) ResetPassword(c *gin.Context) {
	var req request.ResetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		failBindError(c, err, req)
		return
	}

	// 先验证验证码
	err := a.VerifyCodeService.VerifyCode(req.Email, req.Code, "reset-password")
	if err != nil {
		if errors.Is(err, service.ErrVerifyCodeExpired) {
			response.FailWithError(c, http.StatusUnauthorized, "verify code expired", "invalid_verify_code", gin.H{
				"field":  "code",
				"reason": "expired",
			})
			return
		}
		if errors.Is(err, service.ErrInvalidVerifyCode) {
			response.FailWithError(c, http.StatusUnauthorized, "invalid verify code", "invalid_verify_code", gin.H{
				"field":  "code",
				"reason": "mismatch",
			})
			return
		}
		response.FailWithMessage(c, err.Error())
		return
	}

	// 重置密码
	err = a.VerifyCodeService.ResetPassword(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.FailWithError(c, http.StatusNotFound, "user not found", "user_not_found", gin.H{
				"field": "email",
			})
			return
		}
		response.FailWithMessage(c, "密码重置失败: "+err.Error())
		return
	}

	response.OkWithMessage(c, "密码重置成功，请使用新密码登录")
}
