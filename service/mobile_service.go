package service

import (
	"encoding/json"
	"errors"
	"time"

	"star-meet-backend/model/database"
	"star-meet-backend/model/response"
	"star-meet-backend/pkg/i18n"

	"gorm.io/gorm"
)

type MobileService struct {
	DB            *gorm.DB
	RuntimeConfig *RuntimeConfigService
}

var (
	ErrMobileWalletUnavailable = errors.New("mobile wallet unavailable")
	ErrMobileUserNotFound      = errors.New("mobile user not found")
)

type MobileHomeResp struct {
	User                *response.UserResp         `json:"user,omitempty"`
	Settings            *response.UserSettingsResp `json:"settings,omitempty"`
	Membership          MobileMembership           `json:"membership"`
	Wallet              map[string]int64           `json:"wallet"`
	RecentSessions      []response.SessionResp     `json:"recent_sessions"`
	RecommendSections   []MobileSection            `json:"recommend_sections"`
	Banners             []MobileBanner             `json:"banners"`
	ServerTime          int64                      `json:"server_time"`
	Degraded            bool                       `json:"degraded,omitempty"`
	UnavailableSections []string                   `json:"unavailable_sections,omitempty"`
}

type MeDashboardResp struct {
	Profile             *response.UserResp         `json:"profile,omitempty"`
	Settings            *response.UserSettingsResp `json:"settings,omitempty"`
	Subscription        MobileMembership           `json:"subscription"`
	Wallet              map[string]int64           `json:"wallet"`
	DailyLimits         MobileDailyLimits          `json:"daily_limits"`
	ServerTime          int64                      `json:"server_time"`
	Degraded            bool                       `json:"degraded,omitempty"`
	UnavailableSections []string                   `json:"unavailable_sections,omitempty"`
}

type MobileBatchReq struct {
	CharacterIDs []string `json:"character_ids"`
	Include      []string `json:"include"`
}

type MobileBatchResp struct {
	Characters          []MobileCharacterCard `json:"characters,omitempty"`
	Membership          *MobileMembership     `json:"membership,omitempty"`
	Wallet              map[string]int64      `json:"wallet,omitempty"`
	ServerTime          int64                 `json:"server_time"`
	Degraded            bool                  `json:"degraded,omitempty"`
	UnavailableSections []string              `json:"unavailable_sections,omitempty"`
}

type MobileMembership struct {
	IsMember bool   `json:"is_member"`
	Level    string `json:"level"`
	ExpireAt int64  `json:"expire_at,omitempty"`
}

type MobileDailyLimits struct {
	RemainingMessages  int `json:"remaining_messages"`
	RemainingDraws     int `json:"remaining_draws"`
	FreeTrialRemaining int `json:"free_trial_remaining"`
}

type MobileSection struct {
	ID    string                `json:"id"`
	Title string                `json:"title"`
	Items []MobileCharacterCard `json:"items"`
}

type MobileCharacterCard struct {
	CharacterID   string   `json:"character_id"`
	Name          string   `json:"name"`
	Gender        string   `json:"gender"`
	Age           int      `json:"age"`
	Personality   []string `json:"personality"`
	Occupation    string   `json:"occupation"`
	Relationship  string   `json:"relationship"`
	Avatar        string   `json:"avatar"`
	Subtitle      string   `json:"subtitle"`
	Reason        string   `json:"reason"`
	ChatCount     int      `json:"chat_count"`
	FavoriteCount int      `json:"favorite_count"`
}

type MobileBanner struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Action   string `json:"action"`
}

func NewMobileService(db *gorm.DB) *MobileService {
	return &MobileService{DB: db}
}

func (s *MobileService) GetHome(userID, lang string) (*MobileHomeResp, error) {
	user, err := s.loadUser(userID)
	if err != nil {
		return nil, err
	}

	settings, err := s.loadSettings(userID)
	if err != nil {
		return nil, err
	}

	sessions, err := s.loadRecentSessions(userID, 10, lang)
	if err != nil {
		return nil, err
	}

	recommendations, err := s.loadRecommendedCharacters(userID, 8, lang)
	if err != nil {
		return nil, err
	}

	wallet, degraded, unavailableSections, err := s.loadDegradableWallet(userID)
	if err != nil {
		return nil, err
	}

	return &MobileHomeResp{
		User:           toUserResp(*user),
		Settings:       toUserSettingsResp(settings),
		Membership:     toMembership(*user),
		Wallet:         wallet,
		RecentSessions: sessions,
		RecommendSections: []MobileSection{
			{ID: "for_you", Title: i18n.TranslateMobileString("mobile_for_you_title", lang), Items: recommendations},
		},
		Banners:             defaultMobileBanners(lang),
		ServerTime:          time.Now().Unix(),
		Degraded:            degraded,
		UnavailableSections: unavailableSections,
	}, nil
}

func (s *MobileService) GetDashboard(userID string) (*MeDashboardResp, error) {
	user, err := s.loadUser(userID)
	if err != nil {
		return nil, err
	}

	settings, err := s.loadSettings(userID)
	if err != nil {
		return nil, err
	}

	wallet, degraded, unavailableSections, err := s.loadDegradableWallet(userID)
	if err != nil {
		return nil, err
	}

	return &MeDashboardResp{
		Profile:             toUserResp(*user),
		Settings:            toUserSettingsResp(settings),
		Subscription:        toMembership(*user),
		Wallet:              wallet,
		DailyLimits:         s.dailyLimits(*user),
		ServerTime:          time.Now().Unix(),
		Degraded:            degraded,
		UnavailableSections: unavailableSections,
	}, nil
}

func (s *MobileService) GetBatch(userID string, req MobileBatchReq, lang string) (*MobileBatchResp, error) {
	includes := normalizeMobileIncludes(req.Include)
	result := &MobileBatchResp{
		ServerTime: time.Now().Unix(),
	}

	user, err := s.loadUser(userID)
	if err != nil {
		return nil, err
	}

	if includes["characters"] {
		characters, err := s.loadCharactersByIDs(userID, req.CharacterIDs, 50, lang)
		if err != nil {
			return nil, err
		}
		result.Characters = characters
	}

	if includes["membership"] {
		membership := toMembership(*user)
		result.Membership = &membership
	}

	if includes["wallet"] {
		wallet, degraded, unavailableSections, err := s.loadDegradableWallet(userID)
		if err != nil {
			return nil, err
		}
		result.Wallet = wallet
		result.Degraded = degraded
		result.UnavailableSections = unavailableSections
	}

	return result, nil
}

func (s *MobileService) loadUser(userID string) (*database.User, error) {
	var user database.User
	if err := s.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMobileUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (s *MobileService) loadSettings(userID string) (database.UserSettings, error) {
	var settings database.UserSettings
	err := s.DB.Where("user_id = ?", userID).First(&settings).Error
	if err == nil {
		return settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return settings, err
	}

	settings = defaultUserSettings(userID)
	if err := s.DB.Create(&settings).Error; err != nil {
		return settings, err
	}
	return settings, nil
}

func (s *MobileService) loadDegradableWallet(userID string) (map[string]int64, bool, []string, error) {
	wallet, err := s.loadWallet(userID)
	if err == nil {
		return wallet, false, nil, nil
	}
	if errors.Is(err, ErrMobileWalletUnavailable) {
		return defaultWallet(), true, []string{"wallet"}, nil
	}
	return nil, false, nil, err
}

func (s *MobileService) loadWallet(userID string) (map[string]int64, error) {
	wallet := defaultWallet()

	var balances []database.UserCurrency
	if err := s.DB.Where("user_id = ?", userID).Find(&balances).Error; err != nil {
		return nil, ErrMobileWalletUnavailable
	}

	for _, balance := range balances {
		wallet[string(balance.Currency)] = balance.Balance
	}

	return wallet, nil
}

func defaultWallet() map[string]int64 {
	return map[string]int64{
		string(database.CurrencyStar): 0,
		string(database.CurrencyMoon): 0,
		string(database.CurrencySun):  0,
	}
}

func (s *MobileService) loadRecentSessions(userID string, limit int, lang string) ([]response.SessionResp, error) {
	chatService := &ChatService{DB: s.DB}
	sessions, _, err := chatService.GetSessions(userID, 1, limit, lang)
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *MobileService) loadRecommendedCharacters(userID string, limit int, lang string) ([]MobileCharacterCard, error) {
	var characters []database.Character
	query := excludeBlockedCharacters(s.DB, applyPublicMarketplaceFilter(s.DB.Model(&database.Character{})), userID)
	if err := query.
		Order("chat_count DESC, created_at DESC, id DESC").
		Limit(limit).
		Find(&characters).Error; err != nil {
		return nil, err
	}

	items := make([]MobileCharacterCard, 0, len(characters))
	for _, character := range characters {
		items = append(items, toMobileCharacterCard(character, lang))
	}
	return items, nil
}

func (s *MobileService) loadCharactersByIDs(userID string, characterIDs []string, limit int, lang string) ([]MobileCharacterCard, error) {
	ids := dedupeMobileIDs(characterIDs, limit)
	if len(ids) == 0 {
		return []MobileCharacterCard{}, nil
	}

	var characters []database.Character
	query := excludeBlockedCharacters(
		s.DB,
		applyPublicMarketplaceFilter(s.DB.Model(&database.Character{}).Where("character_id IN ?", ids)),
		userID,
	)
	if err := query.Find(&characters).Error; err != nil {
		return nil, err
	}

	characterByID := make(map[string]database.Character, len(characters))
	for _, character := range characters {
		characterByID[character.CharacterID] = character
	}

	items := make([]MobileCharacterCard, 0, len(characters))
	for _, id := range ids {
		if character, ok := characterByID[id]; ok {
			items = append(items, toMobileCharacterCard(character, lang))
		}
	}
	return items, nil
}

func normalizeMobileIncludes(include []string) map[string]bool {
	if len(include) == 0 {
		return map[string]bool{
			"characters": true,
			"membership": true,
			"wallet":     true,
		}
	}

	allowed := map[string]bool{
		"characters": true,
		"membership": true,
		"wallet":     true,
	}
	result := make(map[string]bool, len(include))
	for _, item := range include {
		if allowed[item] {
			result[item] = true
		}
	}
	return result
}

func dedupeMobileIDs(ids []string, limit int) []string {
	if limit <= 0 {
		limit = 50
	}

	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func toMobileCharacterCard(character database.Character, lang string) MobileCharacterCard {
	personality := parseMobilePersonality(character.Personality)
	subtitle := character.Occupation
	if subtitle == "" {
		subtitle = character.Relationship
	}

	reason := i18n.TranslateMobileString("mobile_reason_hot", lang)
	if character.ChatCount == 0 {
		reason = i18n.TranslateMobileString("mobile_reason_new", lang)
	}

	return MobileCharacterCard{
		CharacterID:   character.CharacterID,
		Name:          i18n.TranslateCharacterName(character.Name, lang),
		Gender:        character.Gender,
		Age:           character.Age,
		Personality:   personality,
		Occupation:    character.Occupation,
		Relationship:  i18n.TranslateCategoryName(character.Relationship, lang),
		Avatar:        character.Avatar,
		Subtitle:      subtitle,
		Reason:        reason,
		ChatCount:     character.ChatCount,
		FavoriteCount: character.LikeCount,
	}
}

func parseMobilePersonality(raw string) []string {
	if raw == "" {
		return []string{}
	}

	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err == nil {
		return tags
	}

	return []string{raw}
}

func toMembership(user database.User) MobileMembership {
	membership := MobileMembership{
		IsMember: isPaidMemberLevel(user.MemberLevel),
		Level:    user.MemberLevel,
	}
	if membership.Level == "" {
		membership.Level = "free"
	}
	if user.MemberExpire != nil {
		membership.ExpireAt = user.MemberExpire.Unix()
		if user.MemberExpire.Before(time.Now()) {
			membership.IsMember = false
		}
	}
	return membership
}

const unlimitedRemainingMessages = 9999

func (s *MobileService) dailyLimits(user database.User) MobileDailyLimits {
	quota := DefaultChatQuotaSettings()
	if s != nil && s.RuntimeConfig != nil {
		quota = s.RuntimeConfig.ChatQuotaSettings()
	}

	now := time.Now()
	var db *gorm.DB
	if s != nil {
		db = s.DB
	}
	status, err := evaluateChatQuota(db, quota, user, now)
	if err != nil {
		status = ChatQuotaStatus{
			Enabled:           quota.Enabled,
			MembershipActive:  userHasActiveMembershipAt(user, now),
			Limit:             quota.FreeDailyMessages,
			RemainingMessages: quota.FreeDailyMessages,
		}
		if !quota.Enabled || (status.MembershipActive && quota.MemberUnlimited) {
			status.Unlimited = true
			status.RemainingMessages = unlimitedRemainingMessages
		}
	}

	if status.Unlimited {
		return MobileDailyLimits{
			RemainingMessages:  status.RemainingMessages,
			RemainingDraws:     quota.MemberDailyDraws,
			FreeTrialRemaining: -1,
		}
	}
	return MobileDailyLimits{
		RemainingMessages:  status.RemainingMessages,
		RemainingDraws:     quota.FreeDailyDraws,
		FreeTrialRemaining: quota.FreeTrialMessages,
	}
}

func defaultMobileBanners(lang string) []MobileBanner {
	return []MobileBanner{
		{
			ID:       "daily_checkin",
			Title:    i18n.TranslateMobileString("mobile_banner_daily_title", lang),
			Subtitle: i18n.TranslateMobileString("mobile_banner_daily_subtitle", lang),
			Action:   "open_discovery",
		},
	}
}
