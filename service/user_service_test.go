package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"star-meet-backend/core/mem0"
	"star-meet-backend/core/qdrant"
	"star-meet-backend/model/database"
	"star-meet-backend/model/request"
	"star-meet-backend/pkg/utils"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupUserServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&database.User{}, &database.UserSettings{}); err != nil {
		t.Fatalf("migrate user tables: %v", err)
	}

	return db
}

func setupAccountDeletionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.User{}, &database.UserSettings{}, &database.Character{}, &database.Session{},
		&database.Message{}, &database.Memory{}, &database.Favorite{}, &database.UserCharacter{},
		&database.UserCurrency{}, &database.CurrencyTransaction{}, &database.UserGachaDraw{},
		&database.UserGachaRecord{}, &database.Plan{}, &database.Subscription{}, &database.Notification{}, &database.PushDevice{},
		&database.AppleTransactionOwner{},
		&database.UserAffection{}, &database.Report{}, &database.Block{},
	); err != nil {
		t.Fatalf("migrate account data: %v", err)
	}
	return db
}

func TestGetUserInfoIncludesProfileFields(t *testing.T) {
	db := setupUserServiceTestDB(t)
	now := time.Unix(1710000000, 0)

	user := database.User{
		UserID:      "user-1",
		Email:       "user@example.com",
		Nickname:    "测试用户",
		Avatar:      "https://example.com/avatar.png",
		MemberLevel: "vip",
		Language:    "zh-Hant",
		Region:      "TW",
		Gender:      "female",
		Age:         26,
		ChatDays:    12,
		CharCount:   3,
		MsgCount:    42,
		LastLoginAt: &now,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.GetUserInfo("user-1")
	if err != nil {
		t.Fatalf("get user info: %v", err)
	}

	if resp.Email != "user@example.com" {
		t.Fatalf("expected email to be returned, got %q", resp.Email)
	}
	if resp.Gender != "female" || resp.Age != 26 {
		t.Fatalf("expected gender/age to be returned, got %q/%d", resp.Gender, resp.Age)
	}
	if resp.Language != "zh-Hant" || resp.Region != "TW" {
		t.Fatalf("expected language/region to be preserved, got %q/%q", resp.Language, resp.Region)
	}
	if resp.LastLoginAt != now.Unix() {
		t.Fatalf("expected last login timestamp %d, got %d", now.Unix(), resp.LastLoginAt)
	}
}

func TestUpdateUserInfoReturnsUpdatedProfile(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{
		UserID:      "user-2",
		Nickname:    "旧昵称",
		MemberLevel: "free",
		Language:    "en",
		Region:      "US",
		Gender:      "unknown",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.UpdateUserInfo("user-2", request.UpdateUserInfoReq{
		Nickname: "新昵称",
		Avatar:   "https://example.com/new.png",
		Language: "zh-Hans",
		Region:   "CN",
		Gender:   "male",
		Age:      30,
	})
	if err != nil {
		t.Fatalf("update user info: %v", err)
	}

	if resp.Nickname != "新昵称" || resp.Gender != "male" || resp.Age != 30 {
		t.Fatalf("unexpected response payload: %+v", resp)
	}

	var stored database.User
	if err := db.Where("user_id = ?", "user-2").First(&stored).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}

	if stored.Avatar != "https://example.com/new.png" || stored.Language != "zh-Hans" || stored.Region != "CN" {
		t.Fatalf("unexpected stored user: %+v", stored)
	}
}

func TestUpdateUserInfoRejectsInvalidGender(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{UserID: "user-3", Nickname: "测试"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	if _, err := service.UpdateUserInfo("user-3", request.UpdateUserInfoReq{Gender: "robot"}); err == nil {
		t.Fatal("expected invalid gender error")
	}
}

func TestRefreshTokenRejectsInvalidToken(t *testing.T) {
	db := setupUserServiceTestDB(t)
	service := &UserService{DB: db}

	resp, err := service.RefreshToken("not-a-valid-refresh-token")
	if err == nil {
		t.Fatalf("expected invalid refresh token to fail, got response %+v", resp)
	}
	if resp != nil {
		t.Fatalf("expected no response for invalid refresh token, got %+v", resp)
	}
}

func TestResetPasswordReturnsUserNotFoundForMissingEmail(t *testing.T) {
	db := setupUserServiceTestDB(t)
	service := &VerifyCodeService{DB: db}

	err := service.ResetPassword("missing-reset@example.com", "new-secret")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestRefreshTokenIssuesTokenForValidJWTUser(t *testing.T) {
	db := setupUserServiceTestDB(t)
	if err := db.Create(&database.User{
		UserID:   "refresh-user-1",
		Email:    "refresh@example.com",
		Nickname: "刷新用户",
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	refreshToken, err := utils.GenerateRefreshToken("refresh-user-1")
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.RefreshToken(refreshToken)
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if resp.UserID != "refresh-user-1" || resp.Token == "" || resp.IsNew {
		t.Fatalf("unexpected refresh response: %+v", resp)
	}

	claims, err := utils.ParseToken(resp.Token)
	if err != nil {
		t.Fatalf("parse refreshed token: %v", err)
	}
	if claims.UserID != "refresh-user-1" {
		t.Fatalf("expected refreshed token for user, got %q", claims.UserID)
	}
}

func TestGetUserSettingsCreatesDefaults(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{UserID: "user-settings-1", Nickname: "设置用户"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	resp, err := service.GetUserSettings("user-settings-1")
	if err != nil {
		t.Fatalf("get user settings: %v", err)
	}

	if resp.RelationshipMode != "romantic" || !resp.MemoryEnabled || !resp.PushEnabled {
		t.Fatalf("unexpected default settings: %+v", resp)
	}
}

func TestUpdateUserSettingsPersistsPreferences(t *testing.T) {
	db := setupUserServiceTestDB(t)

	user := database.User{UserID: "user-settings-2", Nickname: "设置用户2"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	memoryEnabled := false
	voiceAutoPlay := true
	pushEnabled := false
	proactiveEnable := false
	privacyMode := true

	resp, err := service.UpdateUserSettings("user-settings-2", request.UpdateUserSettingsReq{
		RelationshipMode: "supportive",
		ResponseStyle:    "gentle",
		ResponseLength:   "short",
		MemoryEnabled:    &memoryEnabled,
		VoiceAutoPlay:    &voiceAutoPlay,
		PushEnabled:      &pushEnabled,
		ProactiveEnable:  &proactiveEnable,
		PrivacyMode:      &privacyMode,
	})
	if err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	if resp.RelationshipMode != "supportive" || resp.ResponseStyle != "gentle" || resp.ResponseLength != "short" {
		t.Fatalf("unexpected updated settings: %+v", resp)
	}
	if resp.MemoryEnabled || !resp.VoiceAutoPlay || resp.PushEnabled || resp.ProactiveEnable || !resp.PrivacyMode {
		t.Fatalf("unexpected toggle values: %+v", resp)
	}
}

func TestCheckMemberStatusSupportsPaidLevels(t *testing.T) {
	db := setupUserServiceTestDB(t)
	expireAt := time.Now().Add(24 * time.Hour)

	user := database.User{
		UserID:       "member-1",
		Nickname:     "会员用户",
		MemberLevel:  "monthly",
		MemberExpire: &expireAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	isMember, err := service.CheckMemberStatus("member-1")
	if err != nil {
		t.Fatalf("check member status: %v", err)
	}
	if !isMember {
		t.Fatal("expected monthly member to be treated as paid member")
	}
}

func TestCheckMemberStatusExpiresPaidLevels(t *testing.T) {
	db := setupUserServiceTestDB(t)
	expireAt := time.Now().Add(-24 * time.Hour)

	user := database.User{
		UserID:       "member-2",
		Nickname:     "过期会员",
		MemberLevel:  "yearly",
		MemberExpire: &expireAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	service := &UserService{DB: db}
	isMember, err := service.CheckMemberStatus("member-2")
	if err != nil {
		t.Fatalf("check member status: %v", err)
	}
	if isMember {
		t.Fatal("expected expired yearly member to be downgraded")
	}

	var stored database.User
	if err := db.Where("user_id = ?", "member-2").First(&stored).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.MemberLevel != "free" {
		t.Fatalf("expected member level to downgrade to free, got %q", stored.MemberLevel)
	}
}

func TestDeleteAccountRemovesOwnedData(t *testing.T) {
	db := setupAccountDeletionTestDB(t)

	userID := "delete-user-1"
	characterID := "delete-character-1"
	sessionID := "delete-session-1"
	if err := db.Create(&database.User{UserID: userID, Email: "delete@example.com"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&database.Character{CharacterID: characterID, UserID: userID, Name: "私有角色"}).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	if err := db.Create(&database.Session{SessionID: sessionID, UserID: userID, CharacterID: characterID}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db.Create(&database.Message{MessageID: "delete-message-1", SessionID: sessionID, UserID: userID, CharacterID: characterID, Role: "user", Content: "private"}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := db.Create(&database.Memory{MemoryID: "delete-memory-1", UserID: userID, CharacterID: characterID, Content: "private"}).Error; err != nil {
		t.Fatalf("create memory: %v", err)
	}
	if err := db.Create(&database.Report{ReporterID: userID, TargetType: "message", TargetID: "delete-message-1", Reason: "spam"}).Error; err != nil {
		t.Fatalf("create report: %v", err)
	}
	if err := db.Create(&database.Block{BlockerID: userID, BlockedID: "other-user"}).Error; err != nil {
		t.Fatalf("create block: %v", err)
	}

	service := &UserService{DB: db}
	if err := service.DeleteAccount(userID); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	for _, check := range []struct {
		name  string
		model interface{}
		query string
	}{
		{"user", &database.User{}, "user_id = ?"},
		{"character", &database.Character{}, "user_id = ?"},
		{"session", &database.Session{}, "user_id = ?"},
		{"message", &database.Message{}, "user_id = ?"},
		{"memory", &database.Memory{}, "user_id = ?"},
		{"report", &database.Report{}, "reporter_id = ?"},
		{"block", &database.Block{}, "blocker_id = ?"},
	} {
		var count int64
		if err := db.Unscoped().Model(check.model).Where(check.query, userID).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if count != 0 {
			t.Fatalf("expected %s data to be removed, found %d", check.name, count)
		}
	}
}

func TestDeleteAccountReleasesAppleTransactionOwnership(t *testing.T) {
	db := setupAccountDeletionTestDB(t)
	for _, userID := range []string{"billing-owner", "billing-next"} {
		if err := db.Create(&database.User{UserID: userID, Email: userID + "@example.test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	subscriptions := NewSubscriptionService(db)
	expiry := time.Now().Add(30 * 24 * time.Hour)
	if _, err := subscriptions.CreateSubscription("billing-owner", "monthly", "Sandbox", "deleted-account-original", expiry); err != nil {
		t.Fatal(err)
	}

	if err := (&UserService{DB: db}).DeleteAccount("billing-owner"); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	var owners int64
	if err := db.Model(&database.AppleTransactionOwner{}).
		Where("environment = ? AND original_transaction_id = ?", "Sandbox", "deleted-account-original").
		Count(&owners).Error; err != nil {
		t.Fatal(err)
	}
	if owners != 0 {
		t.Fatalf("account deletion retained %d ownership rows", owners)
	}
	if _, err := subscriptions.CreateSubscription("billing-owner", "monthly", "Sandbox", "deleted-account-original", expiry); err == nil {
		t.Fatal("deleted account received a new entitlement")
	}

	if _, err := subscriptions.CreateSubscription("billing-next", "monthly", "Sandbox", "deleted-account-original", expiry); err != nil {
		t.Fatalf("new account could not re-verify the transaction after deletion: %v", err)
	}
}

func TestDeleteAccountRemovesAllExternalMemoriesBeforeLocalData(t *testing.T) {
	db := setupAccountDeletionTestDB(t)
	userID := "delete-external-user"
	if err := db.Create(&database.User{UserID: userID, Email: "external@example.com"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	memoryIDs := make([]string, 101)
	for index := range memoryIDs {
		memoryIDs[index] = fmt.Sprintf("memory-%03d", index)
	}
	listCalls := 0
	qdrantCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/memories":
			listCalls++
			if r.URL.Query().Get("user_id") != "user_"+userID || r.URL.Query().Get("offset") != "0" {
				t.Errorf("unexpected Mem0 list query: %s", r.URL.RawQuery)
			}
			if _, present := r.URL.Query()["agent_id"]; present {
				t.Errorf("account cleanup must query all agents: %s", r.URL.RawQuery)
			}
			limit := externalMemoryDeletePageSize
			if len(memoryIDs) < limit {
				limit = len(memoryIDs)
			}
			items := make([]map[string]string, 0, limit)
			for _, id := range memoryIDs[:limit] {
				items = append(items, map[string]string{"id": id, "memory": "private"})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": items})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/memories/"):
			id := strings.TrimPrefix(r.URL.Path, "/memories/")
			for index, current := range memoryIDs {
				if current == id {
					memoryIDs = append(memoryIDs[:index], memoryIDs[index+1:]...)
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/collections/memories/points/delete":
			qdrantCalls++
			var body struct {
				Filter struct {
					Must []struct {
						Key   string `json:"key"`
						Match struct {
							Value string `json:"value"`
						} `json:"match"`
					} `json:"must"`
				} `json:"filter"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode Qdrant delete: %v", err)
			} else if len(body.Filter.Must) != 1 || body.Filter.Must[0].Key != "user_id" || body.Filter.Must[0].Match.Value != userID {
				t.Errorf("unexpected Qdrant filter: %+v", body.Filter.Must)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mem0Client := mem0.NewClient(server.URL, "test-api-key")
	qdrantClient := qdrant.NewClient(server.URL)
	service := &UserService{DB: db, Mem0Client: mem0Client, QdrantClient: qdrantClient}
	if err := service.DeleteAccount(userID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if len(memoryIDs) != 0 || listCalls != 3 {
		t.Fatalf("expected all Mem0 pages deleted from offset zero, remaining=%d list_calls=%d", len(memoryIDs), listCalls)
	}
	if qdrantCalls != 1 {
		t.Fatalf("expected one Qdrant delete call, got %d", qdrantCalls)
	}
}

func TestDeleteAccountKeepsLocalDataWhenExternalCleanupFails(t *testing.T) {
	db := setupAccountDeletionTestDB(t)
	userID := "delete-external-failure-user"
	if err := db.Create(&database.User{UserID: userID, Email: "external-failure@example.com"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private chat content must not escape through errors", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := &UserService{DB: db, Mem0Client: mem0.NewClient(server.URL, "test-api-key")}
	err := service.DeleteAccount(userID)
	if err == nil {
		t.Fatal("expected external cleanup failure")
	}
	if strings.Contains(err.Error(), "private chat content") {
		t.Fatalf("external response body leaked through error: %v", err)
	}
	var count int64
	if err := db.Model(&database.User{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count user: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected local account to remain retryable, found %d", count)
	}
}

func TestThirdPartyLoginNeverUsesMockIdentity(t *testing.T) {
	t.Setenv("WECHAT_APP_ID", "")
	t.Setenv("WECHAT_APP_SECRET", "")
	service := &UserService{DB: setupUserServiceTestDB(t)}
	if _, err := service.WechatLogin(request.WechatLoginReq{Code: "client-controlled"}); !errors.Is(err, ErrThirdPartyUnavailable) {
		t.Fatalf("expected unconfigured WeChat login to fail closed, got %v", err)
	}

	t.Setenv("APPLE_CLIENT_ID", "")
	t.Setenv("APPLE_BUNDLE_ID", "")
	if _, err := service.AppleLogin(request.AppleLoginReq{IdentityToken: "fake", UserID: "fake"}); !errors.Is(err, ErrThirdPartyUnavailable) {
		t.Fatalf("expected unconfigured Apple login to fail closed, got %v", err)
	}
}

func TestCreateThirdPartyUserUsesNullEmail(t *testing.T) {
	db := setupUserServiceTestDB(t)

	// Reproduce the production state that caused App Review's Apple login to
	// fail: one older third-party account already occupied the unique "" value.
	legacy := database.User{UserID: "legacy-third-party", Email: "", AppleUserID: "legacy-apple"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy user: %v", err)
	}

	for _, user := range []*database.User{
		{UserID: "apple-review-user", AppleUserID: "apple-review-subject"},
		{UserID: "line-review-user", LineUserID: "line-review-subject"},
	} {
		if err := createThirdPartyUser(db, user); err != nil {
			t.Fatalf("create third-party user %q: %v", user.UserID, err)
		}
	}

	var nullEmailCount int64
	if err := db.Model(&database.User{}).Where("email IS NULL").Count(&nullEmailCount).Error; err != nil {
		t.Fatalf("count null emails: %v", err)
	}
	if nullEmailCount != 2 {
		t.Fatalf("null email count = %d, want 2", nullEmailCount)
	}
}
