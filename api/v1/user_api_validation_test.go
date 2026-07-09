package v1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiv1 "star-meet-backend/api/v1"
	"star-meet-backend/middleware"
	"star-meet-backend/model/database"
	"star-meet-backend/pkg/utils"
	"star-meet-backend/service"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupUserValidationRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)

	api := &apiv1.UserAPI{}
	return setupUserValidationRouterWithAPI(api)
}

func setupUserValidationRouterWithAPI(api *apiv1.UserAPI) *gin.Engine {
	router := gin.New()
	router.Use(middleware.RequestID())
	group := router.Group("/api/v1/user")
	group.POST("/login", api.Login)
	group.POST("/register", api.Register)
	group.POST("/refresh", api.RefreshToken)
	group.POST("/send-verify-code", api.SendVerifyCode)
	group.POST("/reset-password", api.ResetPassword)
	group.PUT("/info", middleware.Auth(), api.UpdateUserInfo)
	group.PUT("/settings", middleware.Auth(), api.UpdateUserSettings)
	return router
}

type verifyCodeTestRedis struct {
	addr  string
	close func()
}

func startVerifyCodeTestRedis(t *testing.T, values map[string]string) verifyCodeTestRedis {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen redis fake: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleVerifyCodeRedisConn(conn, values)
		}
	}()

	return verifyCodeTestRedis{
		addr: listener.Addr().String(),
		close: func() {
			_ = listener.Close()
			<-done
		},
	}
}

func handleVerifyCodeRedisConn(conn net.Conn, values map[string]string) {
	defer conn.Close()
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		request := string(buf[:n])
		upper := strings.ToUpper(request)
		switch {
		case strings.Contains(upper, "PING"):
			_, _ = conn.Write([]byte("+PONG\r\n"))
		case strings.Contains(upper, "GET"):
			key := redisBulkArg(request, 1)
			if value, ok := values[key]; ok {
				_, _ = fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(value), value)
			} else {
				_, _ = conn.Write([]byte("$-1\r\n"))
			}
		case strings.Contains(upper, "DEL"):
			_, _ = conn.Write([]byte(":1\r\n"))
		default:
			_, _ = conn.Write([]byte("+OK\r\n"))
		}
	}
}

func redisBulkArg(request string, index int) string {
	parts := strings.Split(request, "\r\n")
	args := make([]string, 0, len(parts)/2)
	for i := 0; i < len(parts)-1; i++ {
		if strings.HasPrefix(parts[i], "$") {
			args = append(args, parts[i+1])
			i++
		}
	}
	if index < 0 || index >= len(args) {
		return ""
	}
	return args[index]
}

func setupUserValidationDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.UserSettings{}); err != nil {
		t.Fatalf("migrate user table: %v", err)
	}
	return db
}

func TestUserRegisterRejectsMalformedJSONWithStableErrorCode(t *testing.T) {
	router := setupUserValidationRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/register", bytes.NewBufferString(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-register-malformed-json-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for malformed register JSON, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_json" {
		t.Fatalf("expected invalid_json error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-register-malformed-json-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "body" {
		t.Fatalf("expected body field detail, got %#v", details["field"])
	}
}

func TestUserRegisterRejectsMissingEmailWithValidationError(t *testing.T) {
	router := setupUserValidationRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/register", bytes.NewBufferString(`{"password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-register-missing-email-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing register email, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "email" {
		t.Fatalf("expected email field detail, got %#v", details["field"])
	}
}

func TestUserRegisterRejectsDuplicateEmailWithStableErrorCode(t *testing.T) {
	db := setupUserValidationDB(t)
	if err := db.Create(&database.User{
		UserID:   "duplicate-register-user",
		Email:    "duplicate@example.com",
		Nickname: "已注册用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		UserService: &service.UserService{DB: db},
	})

	payload, err := json.Marshal(map[string]string{
		"email":    "duplicate@example.com",
		"password": "secret123",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-register-duplicate-email-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status 409 for duplicate register email, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "email_already_registered" {
		t.Fatalf("expected email_already_registered error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-register-duplicate-email-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "email" {
		t.Fatalf("expected email field detail, got %#v", details["field"])
	}
}

func TestUserLoginRejectsMissingUserWithStableErrorCode(t *testing.T) {
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		UserService: &service.UserService{DB: setupUserValidationDB(t)},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/login", bytes.NewBufferString(`{"email":"missing@example.com","password":"secret123"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-login-missing-user-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing login user, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "user_not_found" {
		t.Fatalf("expected user_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-login-missing-user-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
}

func TestUserLoginRejectsWrongPasswordWithStableErrorCode(t *testing.T) {
	db := setupUserValidationDB(t)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.Create(&database.User{
		UserID:   "login-password-user",
		Email:    "login-password@example.com",
		Password: string(hashedPassword),
		Nickname: "密码用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		UserService: &service.UserService{DB: db},
	})

	payload, err := json.Marshal(map[string]string{
		"email":    "login-password@example.com",
		"password": "wrong-password",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-login-wrong-password-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for wrong password, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_credentials" {
		t.Fatalf("expected invalid_credentials error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-login-wrong-password-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
}

func TestUserRefreshRejectsMissingTokenWithValidationError(t *testing.T) {
	router := setupUserValidationRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/refresh", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-refresh-missing-token-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for missing refresh token, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "refresh_token" {
		t.Fatalf("expected refresh_token field detail, got %#v", details["field"])
	}
}

func TestUserRefreshRejectsInvalidTokenWithStableErrorCode(t *testing.T) {
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		UserService: &service.UserService{DB: setupUserValidationDB(t)},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/refresh", bytes.NewBufferString(`{"refresh_token":"not-a-valid-refresh-token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-refresh-invalid-token-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for invalid refresh token, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_refresh_token" {
		t.Fatalf("expected invalid_refresh_token error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-refresh-invalid-token-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
}

func TestUserRefreshRejectsTokenForMissingUserWithStableErrorCode(t *testing.T) {
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		UserService: &service.UserService{DB: setupUserValidationDB(t)},
	})
	refreshToken, err := utils.GenerateToken("missing-refresh-user")
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/refresh", bytes.NewBufferString(`{"refresh_token":"`+refreshToken+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-refresh-missing-user-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing refresh user, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "user_not_found" {
		t.Fatalf("expected user_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-refresh-missing-user-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
}

func TestUpdateUserInfoRejectsInvalidAgeWithValidationError(t *testing.T) {
	db := setupUserValidationDB(t)
	if err := db.Create(&database.User{UserID: "user-update-invalid-age", Nickname: "资料用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		UserService: &service.UserService{DB: db},
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/user/info?age=not-a-number", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "user-update-invalid-age"))
	req.Header.Set("X-Request-ID", "user-update-invalid-age-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for invalid age, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-update-invalid-age-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "age" || details["reason"] != "invalid_format" {
		t.Fatalf("unexpected details: %#v", details)
	}
}

func TestUpdateUserSettingsRejectsUnsupportedEnumWithValidationError(t *testing.T) {
	db := setupUserValidationDB(t)
	if err := db.Create(&database.User{UserID: "user-settings-invalid-enum", Nickname: "设置用户"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		UserService: &service.UserService{DB: db},
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/user/settings", bytes.NewReader([]byte(`{"relationship_mode":"robot"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeTestToken(t, "user-settings-invalid-enum"))
	req.Header.Set("X-Request-ID", "user-settings-invalid-enum-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for invalid settings enum, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-settings-invalid-enum-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "relationship_mode" || details["reason"] != "unsupported_value" {
		t.Fatalf("unexpected details: %#v", details)
	}
}

func TestSendVerifyCodeRejectsInvalidEmailWithValidationError(t *testing.T) {
	router := setupUserValidationRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/send-verify-code", bytes.NewBufferString(`{"email":"not-an-email","type":"register"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-verify-code-invalid-email-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422 for invalid verify-code email, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "validation_failed" {
		t.Fatalf("expected validation_failed error_code, got %q", resp.ErrorCode)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "email" {
		t.Fatalf("expected email field detail, got %#v", details["field"])
	}
}

func TestSendVerifyCodeRejectsRegisteredEmailWithStableErrorCode(t *testing.T) {
	db := setupUserValidationDB(t)
	if err := db.Create(&database.User{
		UserID:   "verify-code-registered-user",
		Email:    "registered@example.com",
		Nickname: "验证码用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		VerifyCodeService: &service.VerifyCodeService{DB: db},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/send-verify-code", bytes.NewBufferString(`{"email":"registered@example.com","type":"register"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-verify-code-registered-email-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status 409 for registered verify-code email, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "email_already_registered" {
		t.Fatalf("expected email_already_registered error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-verify-code-registered-email-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "email" {
		t.Fatalf("expected email field detail, got %#v", details["field"])
	}
}

func TestSendVerifyCodeRejectsMissingResetEmailWithStableErrorCode(t *testing.T) {
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		VerifyCodeService: &service.VerifyCodeService{DB: setupUserValidationDB(t)},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/send-verify-code", bytes.NewBufferString(`{"email":"missing-reset@example.com","type":"reset-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-verify-code-missing-reset-email-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing reset email, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "user_not_found" {
		t.Fatalf("expected user_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-verify-code-missing-reset-email-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "email" {
		t.Fatalf("expected email field detail, got %#v", details["field"])
	}
}

func TestResetPasswordRejectsInvalidVerifyCodeWithStableErrorCode(t *testing.T) {
	db := setupUserValidationDB(t)
	if err := db.Create(&database.User{
		UserID:   "reset-password-user",
		Email:    "reset-password@example.com",
		Nickname: "重置密码用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	fakeRedis := startVerifyCodeTestRedis(t, map[string]string{})
	defer fakeRedis.close()
	redisClient := redis.NewClient(&redis.Options{Addr: fakeRedis.addr})
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		VerifyCodeService: &service.VerifyCodeService{DB: db, Redis: redisClient},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/reset-password", bytes.NewBufferString(`{"email":"reset-password@example.com","code":"000000","password":"new-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-reset-password-invalid-code-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for invalid reset verify code, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "invalid_verify_code" {
		t.Fatalf("expected invalid_verify_code error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-reset-password-invalid-code-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "code" {
		t.Fatalf("expected code field detail, got %#v", details["field"])
	}
}

func TestResetPasswordRejectsMissingUserWithStableErrorCode(t *testing.T) {
	db := setupUserValidationDB(t)
	fakeRedis := startVerifyCodeTestRedis(t, map[string]string{
		"verify_code:reset-password:missing-reset@example.com": "123456",
	})
	defer fakeRedis.close()
	redisClient := redis.NewClient(&redis.Options{Addr: fakeRedis.addr})
	router := setupUserValidationRouterWithAPI(&apiv1.UserAPI{
		VerifyCodeService: &service.VerifyCodeService{DB: db, Redis: redisClient},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/reset-password", bytes.NewBufferString(`{"email":"missing-reset@example.com","code":"123456","password":"new-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "user-reset-password-missing-user-req")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for missing reset user, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	resp := decodeResponse(t, recorder)
	if resp.ErrorCode != "user_not_found" {
		t.Fatalf("expected user_not_found error_code, got %q", resp.ErrorCode)
	}
	if resp.RequestID != "user-reset-password-missing-user-req" {
		t.Fatalf("expected request id in response, got %q", resp.RequestID)
	}
	details, ok := resp.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details object, got %#v", resp.Details)
	}
	if details["field"] != "email" {
		t.Fatalf("expected email field detail, got %#v", details["field"])
	}
}
