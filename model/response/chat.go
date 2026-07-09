package response

import (
	"time"

	"github.com/gin-gonic/gin"
)

// 发送消息响应
type SendMessageResp struct {
	MessageID     string `json:"message_id"`
	Reply         string `json:"reply"`
	ReplyType     string `json:"reply_type"`
	ReplyVoiceURL string `json:"reply_voice_url,omitempty"` // 语音回复URL
	UserEmotion   string `json:"user_emotion"`
	ReplyEmotion  string `json:"reply_emotion"`
	CreatedAt     int64  `json:"created_at"`
}

// 消息详情
type MessageResp struct {
	MessageID     string  `json:"message_id"`
	Role          string  `json:"role"`
	Content       string  `json:"content"`
	MessageType   string  `json:"message_type"`
	VoiceURL      string  `json:"voice_url,omitempty"`
	VoiceDuration float64 `json:"voice_duration,omitempty"`
	UserEmotion   string  `json:"user_emotion,omitempty"`
	CreatedAt     int64   `json:"created_at"`
}

// 会话详情
type SessionResp struct {
	SessionID     string         `json:"session_id"`
	CharacterInfo CharacterBasic `json:"character_info"`
	LastMessage   string         `json:"last_message"`
	LastMessageAt int64          `json:"last_message_at"`
	UnreadCount   int            `json:"unread_count"`
	IsPinned      bool           `json:"is_pinned"`
}

// 角色基本信息（用于会话中）
type CharacterBasic struct {
	CharacterID  string `json:"character_id"`
	Name         string `json:"name"`
	Avatar       string `json:"avatar"`
	Relationship string `json:"relationship"`
}

// 通用响应
type Response struct {
	Code       int         `json:"code"`
	Message    string      `json:"message"`
	Data       interface{} `json:"data"`
	RequestID  string      `json:"request_id,omitempty"`
	ServerTime int64       `json:"server_time,omitempty"`
	ErrorCode  string      `json:"error_code,omitempty"`
	Details    interface{} `json:"details,omitempty"`
	RetryAfter int64       `json:"retry_after,omitempty"`
}

// OK responses
func Ok(data interface{}) Response {
	if c, ok := data.(*gin.Context); ok {
		resp := buildResponse(c, 200, "success", nil)
		c.JSON(200, resp)
		return resp
	}
	return Response{Code: 200, Message: "success", Data: data}
}

func buildResponse(c *gin.Context, code int, message string, data interface{}) Response {
	resp := Response{
		Code:       code,
		Message:    message,
		Data:       data,
		ServerTime: time.Now().Unix(),
	}
	if c != nil {
		resp.RequestID = c.GetString("request_id")
	}
	return resp
}

func buildErrorResponse(c *gin.Context, code int, message, errorCode string, details interface{}, retryAfter int64) Response {
	resp := buildResponse(c, code, message, nil)
	if errorCode == "" {
		errorCode = errorCodeForStatus(code)
	}
	resp.ErrorCode = errorCode
	resp.Details = details
	resp.RetryAfter = retryAfter
	return resp
}

func errorCodeForStatus(code int) string {
	switch code {
	case 400:
		return "bad_request"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 422:
		return "validation_failed"
	case 429:
		return "rate_limited"
	case 500:
		return "internal_error"
	default:
		if code >= 500 {
			return "server_error"
		}
		if code >= 400 {
			return "client_error"
		}
		return ""
	}
}

// Gin context helpers
func OkWithData(c *gin.Context, data interface{}) {
	c.JSON(200, buildResponse(c, 200, "success", data))
}

func OkWithMessageAndData(c *gin.Context, message string, data interface{}) {
	c.JSON(200, buildResponse(c, 200, message, data))
}

func OkWithMessageGin(c *gin.Context, message string) {
	c.JSON(200, buildResponse(c, 200, message, nil))
}

func OkWithMessage(c *gin.Context, message string) {
	c.JSON(200, buildResponse(c, 200, message, nil))
}

func FailWithMessage(c *gin.Context, message string) {
	c.JSON(400, buildErrorResponse(c, 400, message, "", nil, 0))
}

func Fail(c *gin.Context, code int, message string) {
	c.JSON(code, buildErrorResponse(c, code, message, "", nil, 0))
}

func FailWithError(c *gin.Context, code int, message, errorCode string, details interface{}) {
	c.JSON(code, buildErrorResponse(c, code, message, errorCode, details, 0))
}

func FailWithRetryAfter(c *gin.Context, code int, message, errorCode string, retryAfter int64) {
	c.JSON(code, buildErrorResponse(c, code, message, errorCode, nil, retryAfter))
}

// Aliases for compatibility
func OkGin(c *gin.Context, data interface{}) {
	c.JSON(200, buildResponse(c, 200, "success", data))
}
