package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"star-meet-backend/model/database"
)

var ErrInvalidGrowthInput = errors.New("invalid feedback or event input")

type FeedbackInput struct {
	Category   string `json:"category"`
	Content    string `json:"content"`
	Contact    string `json:"contact"`
	AppVersion string `json:"app_version"`
	Platform   string `json:"platform"`
	Device     string `json:"device"`
	RequestID  string `json:"request_id"`
}

func CreateFeedback(db *gorm.DB, userID string, input FeedbackInput) (*database.Feedback, error) {
	input.Content = strings.TrimSpace(input.Content)
	input.Contact = strings.TrimSpace(input.Contact)
	switch input.Category {
	case "chat", "memory", "bug", "other":
	default:
		return nil, ErrInvalidGrowthInput
	}
	if utf8.RuneCountInString(input.Content) < 5 || utf8.RuneCountInString(input.Content) > 2000 ||
		utf8.RuneCountInString(input.Contact) > 200 || len(input.AppVersion) > 32 || len(input.Platform) > 20 ||
		utf8.RuneCountInString(input.Device) > 100 || len(input.RequestID) > 100 {
		return nil, ErrInvalidGrowthInput
	}
	row := database.Feedback{ID: "SM-" + time.Now().UTC().Format("20060102") + "-" + uuid.NewString(),
		UserID: userID, Category: input.Category, Content: input.Content, Contact: input.Contact,
		AppVersion: input.AppVersion, Platform: input.Platform, Device: input.Device, RequestID: input.RequestID, Status: "received"}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if userID != "" {
			if err := requireExistingMemoryUser(tx, userID); err != nil {
				return err
			}
		}
		return tx.Create(&row).Error
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

type ClientEvent struct {
	ID             string            `json:"event_id"`
	Name           string            `json:"name"`
	InstallationID string            `json:"installation_id"`
	OccurredAt     int64             `json:"occurred_at"`
	Internal       bool              `json:"internal"`
	Properties     map[string]string `json:"properties"`
}

var clientEventNames = map[string]bool{
	"first_open": true, "login_started": true, "login_completed": true, "login_failed": true,
	"onboarding_step": true, "onboarding_completed": true, "character_selected": true, "chat_opened": true,
	"ai_first_content": true, "ai_reply_failed": true, "quota_hit": true, "paywall_view": true,
	"purchase_completed": true, "feedback_submitted": true, "share_started": true, "share_completed": true,
	"notification_opt_in": true, "notification_opened": true,
}
var clientPropertyNames = map[string]bool{
	"app_version": true, "platform": true, "os_version": true, "language": true, "method": true,
	"step": true, "character_id": true, "entry": true, "duration_ms": true, "error_code": true,
	"product_id": true, "category": true, "allowed": true, "type": true, "cohort": true,
}

type AnalyticsService struct{ DB *gorm.DB }

func (s *AnalyticsService) Ingest(userID string, input []ClientEvent, now time.Time) error {
	if len(input) == 0 || len(input) > 25 {
		return ErrInvalidGrowthInput
	}
	rows := make([]database.ProductEvent, 0, len(input))
	for _, e := range input {
		if _, err := uuid.Parse(e.ID); err != nil {
			return ErrInvalidGrowthInput
		}
		if _, err := uuid.Parse(e.InstallationID); err != nil {
			return ErrInvalidGrowthInput
		}
		if !clientEventNames[e.Name] || len(e.Properties) > 12 {
			return ErrInvalidGrowthInput
		}
		at := time.Unix(e.OccurredAt, 0)
		if at.After(now.Add(5*time.Minute)) || at.Before(now.Add(-30*24*time.Hour)) {
			return ErrInvalidGrowthInput
		}
		for key, value := range e.Properties {
			if !clientPropertyNames[key] || utf8.RuneCountInString(value) > 120 {
				return ErrInvalidGrowthInput
			}
		}
		props, _ := json.Marshal(e.Properties)
		// Client IDs are namespaced by installation so an unrelated account cannot overwrite an event.
		sum := sha256.Sum256([]byte("client:" + e.InstallationID + ":" + e.ID))
		rows = append(rows, database.ProductEvent{ID: hex.EncodeToString(sum[:]), Name: e.Name, InstallationID: e.InstallationID,
			UserID: userID, Properties: string(props), Internal: e.Internal, OccurredAt: at})
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if userID != "" {
			if err := requireExistingMemoryUser(tx, userID); err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
			return err
		}
		if userID != "" {
			for _, e := range input {
				if e.Name == "login_completed" {
					if err := tx.Model(&database.ProductEvent{}).Where("installation_id = ? AND user_id = ?", e.InstallationID, "").
						Update("user_id", userID).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// RecordTurn records successful server facts only; duplicate retries/regenerations share a user-message key.
func (s *AnalyticsService) RecordTurn(name, userID, characterID, userMessageID string, at time.Time) error {
	if s == nil || s.DB == nil {
		return nil
	}
	if name != "user_message_accepted" && name != "ai_reply_completed" {
		return ErrInvalidGrowthInput
	}
	if userMessageID == "" {
		return ErrInvalidGrowthInput
	}
	sum := sha256.Sum256([]byte("server:" + name + ":" + userID + ":" + userMessageID))
	row := database.ProductEvent{ID: hex.EncodeToString(sum[:]), Name: name, UserID: userID, CharacterID: characterID,
		UserMessageID: userMessageID, OccurredAt: at, Properties: "{}"}
	return s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

type CohortDay struct {
	Date       string `json:"date"`
	FirstOpens int    `json:"first_opens"`
	Activated  int    `json:"activated_5_turns"`
	Day1       *int   `json:"day1_returned"`
	Day7       *int   `json:"day7_returned"`
}
type GrowthSummary struct {
	Timezone           string      `json:"timezone"`
	CohortDefinition   string      `json:"cohort_definition"`
	UnlinkedFirstOpens int         `json:"unlinked_first_opens"`
	EventsTruncated    bool        `json:"events_truncated"`
	Days               []CohortDay `json:"days"`
}

func (s *AnalyticsService) Summary(now time.Time) (*GrowthSummary, error) {
	loc := chatQuotaLocation("Asia/Taipei")
	today := now.In(loc).Format("2006-01-02")
	start := now.AddDate(0, 0, -35)
	var firstEvents []database.ProductEvent
	// Account cohorts use the earliest known first-open across all retained history, not the first event in this window.
	if err := s.DB.Where("name = ? AND internal = ? AND user_id <> ?", "first_open", false, "").Order("occurred_at ASC").Limit(50001).Find(&firstEvents).Error; err != nil {
		return nil, err
	}
	var events []database.ProductEvent
	if err := s.DB.Where("name = ? AND occurred_at >= ? AND internal = ?", "ai_reply_completed", start, false).Order("occurred_at ASC").Limit(50001).Find(&events).Error; err != nil {
		return nil, err
	}
	result := &GrowthSummary{Timezone: "Asia/Taipei", CohortDefinition: "Earliest retained account-linked first open; server-confirmed replies. Internal, task and TestFlight cohorts excluded.", Days: []CohortDay{}, EventsTruncated: len(events) > 50000 || len(firstEvents) > 50000}
	var unlinked int64
	if err := s.DB.Model(&database.ProductEvent{}).Where("name = ? AND occurred_at >= ? AND internal = ? AND user_id = ?", "first_open", start, false, "").Count(&unlinked).Error; err != nil {
		return nil, err
	}
	result.UnlinkedFirstOpens = int(unlinked)
	if len(events) > 50000 {
		events = events[:50000]
	}
	if len(firstEvents) > 50000 {
		firstEvents = firstEvents[:50000]
	}
	first := map[string]string{}
	seen := map[string]bool{}
	replies := map[string]map[string]map[string]bool{}
	for _, e := range firstEvents {
		if seen[e.UserID] {
			continue
		}
		seen[e.UserID] = true
		var props map[string]string
		_ = json.Unmarshal([]byte(e.Properties), &props)
		if props["cohort"] == "task" || props["cohort"] == "testflight" || e.OccurredAt.Before(start) {
			continue
		}
		first[e.UserID] = e.OccurredAt.In(loc).Format("2006-01-02")
	}
	for _, e := range events {
		if e.UserID == "" || e.UserMessageID == "" {
			continue
		}
		day := e.OccurredAt.In(loc).Format("2006-01-02")
		if replies[e.UserID] == nil {
			replies[e.UserID] = map[string]map[string]bool{}
		}
		if replies[e.UserID][day] == nil {
			replies[e.UserID][day] = map[string]bool{}
		}
		replies[e.UserID][day][e.UserMessageID] = true
	}
	byDay := map[string]*CohortDay{}
	for user, day := range first {
		row := byDay[day]
		if row == nil {
			row = &CohortDay{Date: day}
			date, _ := time.ParseInLocation("2006-01-02", day, loc)
			if date.AddDate(0, 0, 1).Format("2006-01-02") < today {
				v := 0
				row.Day1 = &v
			}
			if date.AddDate(0, 0, 7).Format("2006-01-02") < today {
				v := 0
				row.Day7 = &v
			}
			byDay[day] = row
		}
		row.FirstOpens++
		if len(replies[user][day]) >= 5 {
			row.Activated++
		}
		date, _ := time.ParseInLocation("2006-01-02", day, loc)
		if row.Day1 != nil && len(replies[user][date.AddDate(0, 0, 1).Format("2006-01-02")]) > 0 {
			(*row.Day1)++
		}
		if row.Day7 != nil && len(replies[user][date.AddDate(0, 0, 7).Format("2006-01-02")]) > 0 {
			(*row.Day7)++
		}
	}
	keys := make([]string, 0, len(byDay))
	for key := range byDay {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.Days = append(result.Days, *byDay[key])
	}
	return result, nil
}

func UpdateFeedbackStatus(db *gorm.DB, id, status string) error {
	switch status {
	case "received", "investigating", "resolved", "closed":
	default:
		return ErrInvalidGrowthInput
	}
	result := db.Model(&database.Feedback{}).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := db.Model(&database.Feedback{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w", gorm.ErrRecordNotFound)
		}
	}
	return nil
}
