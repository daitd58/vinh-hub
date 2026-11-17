package sport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mezon "github.com/quangledang23/mezon-sdk-go"
)

// MessageRecord stores a chat message captured from the sport channel.
type MessageRecord struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name,omitempty"`
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
}

// Participant represents a confirmed match participant with their slots count.
type Participant struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	Slots       int    `json:"slots"`
}

// BillItem represents a single participant's share in a badminton bill.
type BillItem struct {
	UserID      string     `json:"user_id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name,omitempty"`
	Slots       int        `json:"slots"`
	Amount      int64      `json:"amount"`
	Paid        bool       `json:"paid"`
	PaidAt      *time.Time `json:"paid_at,omitempty"`
}

// BillSession holds the current active payment/splitting session.
type BillSession struct {
	ID           string               `json:"id"`
	BillNumber   int                  `json:"bill_number,omitempty"`
	BillCode     string               `json:"bill_code,omitempty"`
	MatchDay     string               `json:"match_day"`
	MatchDate    string               `json:"match_date"`
	CourtFee     int64                `json:"court_fee"`
	ShuttleCount int                  `json:"shuttle_count"`
	ShuttlePrice int64                `json:"shuttle_price"`
	ShuttleFee   int64                `json:"shuttle_fee"`
	TotalAmount  int64                `json:"total_amount"`
	TotalSlots   int                  `json:"total_slots"`
	SlotPrice    int64                `json:"slot_price"`
	Items        map[string]*BillItem `json:"items"` // keyed by userID
	MessageID    string               `json:"message_id,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
}

// MatchRecord stores historical attendance of a completed match for stats & leaderboard.
type MatchRecord struct {
	SessionID   string        `json:"session_id"`
	MatchDay    string        `json:"match_day"`
	MatchDate   string        `json:"match_date"` // DD/MM/YYYY
	Attendees   []Participant `json:"attendees"`
	Absentees   []string      `json:"absentees"`
	CompletedAt time.Time     `json:"completed_at"`
}

// SplitBreakdown details the parsed court and shuttle fees.
type SplitBreakdown struct {
	CourtFee     int64
	ShuttleCount int
	ShuttlePrice int64
	ShuttleFee   int64
	TotalFee     int64
}

// LeaderboardEntry tracks aggregate attendance statistics for a member.
type LeaderboardEntry struct {
	Username    string
	Matches     int
	Slots       int
	Rate        float64
	AbsentCount int
}

// AttendanceState persists the current check-in session, bill, and history.
type AttendanceState struct {
	SessionID                  string                 `json:"session_id"`          // e.g. "match-2026-11-17"
	CallTime                   time.Time              `json:"call_time"`           // when check-in was triggered
	DiemDanhMessageID          string                 `json:"diemdanh_message_id"` // message ID of the attendance call
	MatchDay                   string                 `json:"match_day"`           // e.g. "Thứ Ba"
	MatchDate                  string                 `json:"match_date"`          // e.g. "17/11/2026"
	MatchTime                  string                 `json:"match_time"`          // e.g. "18:00"
	LastDiemDanh               string                 `json:"last_diemdanh"`       // e.g. "2026-11-16_09"
	LastReminder               string                 `json:"last_reminder"`       // e.g. "2026-11-17_17"
	Messages                   []MessageRecord        `json:"messages"`
	ReactionsGoing             map[string]string      `json:"reactions_going"`     // userID -> username (:cong1:)
	ReactionsNotGoing          map[string]string      `json:"reactions_not_going"` // userID -> username (:tru1:)
	GoingParticipants          map[string]Participant `json:"going_participants,omitempty"`
	PreviousMatchParticipants []Participant          `json:"previous_match_participants,omitempty"`
	PreviousMatchDay           string                 `json:"previous_match_day,omitempty"`
	PreviousMatchDate          string                 `json:"previous_match_date,omitempty"`
	CustomPaymentQR            string                 `json:"custom_payment_qr,omitempty"`
	NextBillNumber             int                    `json:"next_bill_number,omitempty"`
	CurrentBill                *BillSession           `json:"current_bill,omitempty"`
	BillHistory                []BillSession          `json:"bill_history,omitempty"`
	MatchHistory               []MatchRecord          `json:"match_history,omitempty"`
	LastBXH                    string                 `json:"last_bxh,omitempty"`
}

// SummaryResult contains the parsed attendance details.
type SummaryResult struct {
	Going      []string
	TotalSlots int
	NotGoing   []string
	CourtInfo  string
	Notes      []string
	RawAI      string
}

// Config configures the badminton manager.
type Config struct {
	Enabled       bool
	ClanID        string
	ChannelID     string
	RoleID        string
	RoleName      string
	StatePath     string
	OpenAIKey     string
	OpenAIBaseURL string
	OpenAIModel   string
	GeminiKey     string
	PaymentQR     string
	Timezone      *time.Location
	Log           *slog.Logger
}

// MessageSender sends messages with optional mentions to a Mezon channel.
type MessageSender interface {
	SendToChannel(ctx context.Context, channelID string, text string, opts *mezon.SendOptions) (string, error)
	ReactToMessage(ctx context.Context, channelID, messageID, emoji string) error
}

// Manager coordinates badminton attendance schedules, message recording, and AI summaries.
type Manager struct {
	cfg      Config
	sender   MessageSender
	log      *slog.Logger
	registry *UserRegistry

	mu    sync.Mutex
	state AttendanceState
}

// NewManager creates a badminton manager.
func NewManager(cfg Config, sender MessageSender) *Manager {
	if cfg.RoleName == "" {
		cfg.RoleName = "Vinh-CL"
	}
	if cfg.RoleID == "" {
		cfg.RoleID = "1840693979155271680"
	}
	if cfg.ChannelID == "" {
		cfg.ChannelID = "1840689219983380480"
	}
	if cfg.StatePath == "" {
		cfg.StatePath = "badminton_state.json"
	}
	if cfg.Timezone == nil {
		cfg.Timezone = time.FixedZone("ICT", 7*3600)
	}
	logger := cfg.Log
	if logger == nil {
		logger = slog.Default()
	}

	regPath := "clan_users.json"
	if cfg.StatePath != "" {
		regPath = filepath.Join(filepath.Dir(cfg.StatePath), "clan_users.json")
	}

	m := &Manager{
		cfg:      cfg,
		sender:   sender,
		log:      logger,
		registry: NewUserRegistry(regPath),
	}
	m.loadState()
	return m
}

// RegisterUser records or updates a clan user's profile in the registry.
func (m *Manager) RegisterUser(userID, username, clanNick, displayName string) {
	if m == nil || m.registry == nil {
		return
	}
	m.registry.Register(userID, username, clanNick, displayName)
}

// ResolveDisplayName resolves a member's userID or username to their Clan Nickname.
func (m *Manager) ResolveDisplayName(userID, username string) string {
	if m == nil || m.registry == nil {
		if username != "" {
			return strings.TrimPrefix(username, "@")
		}
		return userID
	}
	return m.registry.ResolveDisplayName(userID, username)
}

func (m *Manager) loadState() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.cfg.StatePath)
	if err == nil {
		var s AttendanceState
		if json.Unmarshal(data, &s) == nil {
			if s.ReactionsGoing == nil {
				s.ReactionsGoing = make(map[string]string)
			}
			if s.ReactionsNotGoing == nil {
				s.ReactionsNotGoing = make(map[string]string)
			}
			if s.GoingParticipants == nil {
				s.GoingParticipants = make(map[string]Participant)
			}
			if s.MatchHistory == nil {
				s.MatchHistory = make([]MatchRecord, 0)
			}
			if s.BillHistory == nil {
				s.BillHistory = make([]BillSession, 0)
			}
			billNumSeq := 1
			for i := range s.BillHistory {
				if s.BillHistory[i].Items == nil {
					s.BillHistory[i].Items = make(map[string]*BillItem)
				}
				if s.BillHistory[i].BillNumber <= 0 {
					s.BillHistory[i].BillNumber = billNumSeq
					s.BillHistory[i].ID = strconv.Itoa(billNumSeq)
					s.BillHistory[i].BillCode = fmt.Sprintf("CL%d", billNumSeq)
				}
				if s.BillHistory[i].BillNumber >= billNumSeq {
					billNumSeq = s.BillHistory[i].BillNumber + 1
				}
			}
			if s.CurrentBill != nil {
				if s.CurrentBill.Items == nil {
					s.CurrentBill.Items = make(map[string]*BillItem)
				}
				if s.CurrentBill.BillNumber <= 0 {
					s.CurrentBill.BillNumber = billNumSeq
					s.CurrentBill.ID = strconv.Itoa(billNumSeq)
					s.CurrentBill.BillCode = fmt.Sprintf("CL%d", billNumSeq)
					billNumSeq++
				}
			}
			if s.NextBillNumber < billNumSeq {
				s.NextBillNumber = billNumSeq
			}
			for uid, uname := range s.ReactionsGoing {
				if _, ok := s.GoingParticipants[uid]; !ok {
					s.GoingParticipants[uid] = Participant{
						UserID:   uid,
						Username: uname,
						Slots:    1,
					}
				}
			}
			m.state = s
			m.log.Info("loaded badminton attendance state",
				"session", s.SessionID, "messages", len(s.Messages),
				"reactions_going", len(s.ReactionsGoing), "reactions_not_going", len(s.ReactionsNotGoing),
				"participants", len(s.GoingParticipants), "matches_history", len(s.MatchHistory),
				"bills_history", len(s.BillHistory), "next_bill_number", s.NextBillNumber)
			return
		}
	}
	m.state = AttendanceState{
		Messages:          make([]MessageRecord, 0),
		ReactionsGoing:    make(map[string]string),
		ReactionsNotGoing: make(map[string]string),
		GoingParticipants: make(map[string]Participant),
		BillHistory:       make([]BillSession, 0),
		MatchHistory:      make([]MatchRecord, 0),
		NextBillNumber:    1,
	}
}

func (m *Manager) saveStateLocked() {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err == nil {
		_ = os.WriteFile(m.cfg.StatePath, data, 0644)
	}
}

// RecordReaction registers a member reaction (:cong1: or :tru1:) to the attendance post.
func (m *Manager) RecordReaction(channelID, messageID, userID, username, emoji string, isRemove bool, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg.ChannelID != "" && channelID != m.cfg.ChannelID {
		return
	}

	cleanEmoji := strings.ToLower(strings.Trim(emoji, ":"))
	isGoing := strings.Contains(cleanEmoji, "cong1") || cleanEmoji == "+1" || cleanEmoji == "👍" || cleanEmoji == "7239659508431615752"
	isNotGoing := strings.Contains(cleanEmoji, "tru1") || cleanEmoji == "-1" || cleanEmoji == "👎" || cleanEmoji == "7294564276005201529"

	if !isGoing && !isNotGoing {
		return
	}

	if m.state.ReactionsGoing == nil {
		m.state.ReactionsGoing = make(map[string]string)
	}
	if m.state.ReactionsNotGoing == nil {
		m.state.ReactionsNotGoing = make(map[string]string)
	}
	if m.state.GoingParticipants == nil {
		m.state.GoingParticipants = make(map[string]Participant)
	}

	rawUsername := username
	if rawUsername == "" {
		rawUsername = userID
	}
	displayName := m.ResolveDisplayName(userID, rawUsername)
	if displayName == "" {
		displayName = rawUsername
	}

	slots := count
	if slots <= 0 {
		slots = 1
	}

	if isRemove {
		if isGoing {
			delete(m.state.ReactionsGoing, userID)
			delete(m.state.GoingParticipants, userID)
		}
		if isNotGoing {
			delete(m.state.ReactionsNotGoing, userID)
		}
	} else {
		if isGoing {
			m.state.ReactionsGoing[userID] = rawUsername
			m.state.GoingParticipants[userID] = Participant{
				UserID:      userID,
				Username:    rawUsername,
				DisplayName: displayName,
				Slots:       slots,
			}
			delete(m.state.ReactionsNotGoing, userID)
		} else if isNotGoing {
			m.state.ReactionsNotGoing[userID] = rawUsername
			delete(m.state.ReactionsGoing, userID)
			delete(m.state.GoingParticipants, userID)
		}
	}

	m.saveStateLocked()
	m.log.Info("recorded sport reaction", "user", displayName, "username", rawUsername, "emoji", emoji, "is_remove", isRemove, "slots", slots,
		"going_count", len(m.state.ReactionsGoing), "not_going_count", len(m.state.ReactionsNotGoing))
}

// RecordMessage stores incoming channel messages for the current attendance session.
func (m *Manager) RecordMessage(channelID, msgID, userID, username, content string, clanNicks ...string) {
	if channelID != m.cfg.ChannelID {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	clanNick := ""
	if len(clanNicks) > 0 {
		clanNick = clanNicks[0]
	}
	if clanNick != "" {
		m.RegisterUser(userID, username, clanNick, "")
	}
	displayName := m.ResolveDisplayName(userID, username)

	m.mu.Lock()
	defer m.mu.Unlock()

	rec := MessageRecord{
		ID:          msgID,
		UserID:      userID,
		Username:    username,
		DisplayName: displayName,
		Content:     content,
		Timestamp:   time.Now(),
	}

	m.state.Messages = append(m.state.Messages, rec)
	// Keep up to 300 recent messages
	if len(m.state.Messages) > 300 {
		m.state.Messages = m.state.Messages[len(m.state.Messages)-300:]
	}
	m.saveStateLocked()
}

// Run starts the periodic scheduler ticker.
func (m *Manager) Run(ctx context.Context) {
	if !m.cfg.Enabled {
		m.log.Info("badminton manager is disabled")
		return
	}

	m.log.Info("badminton scheduler started",
		"channel", m.cfg.ChannelID, "role", m.cfg.RoleName, "tz", m.cfg.Timezone.String())

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			m.tick(ctx, t.In(m.cfg.Timezone))
		}
	}
}

func (m *Manager) tick(ctx context.Context, now time.Time) {
	weekday := now.Weekday()
	hour := now.Hour()
	minute := now.Minute()

	// 1. Attendance Check-in Call: Monday, Wednesday, Friday at 09:00 AM
	if (weekday == time.Monday || weekday == time.Wednesday || weekday == time.Friday) && hour == 9 && minute == 0 {
		slotKey := now.Format("2006-01-02_09")
		m.mu.Lock()
		already := m.state.LastDiemDanh == slotKey
		m.mu.Unlock()
		if !already {
			m.mu.Lock()
			m.state.LastDiemDanh = slotKey
			m.saveStateLocked()
			m.mu.Unlock()

			if err := m.TriggerDiemDanh(ctx, now); err != nil {
				m.log.Error("failed to trigger auto diemdanh", "err", err)
			}
		}
	}

	// 2. Match Reminder on match days (Tuesday, Thursday, Saturday):
	// Reads messages to detect the confirmed booking time (e.g. 19:00 vs 19:30, or Saturday 14:00)
	// and sends the reminder 1 hour beforehand.
	isMatchDay := (weekday == time.Tuesday || weekday == time.Thursday || weekday == time.Saturday)
	if isMatchDay {
		m.mu.Lock()
		msgs := append([]MessageRecord(nil), m.state.Messages...)
		m.mu.Unlock()

		isSat := (weekday == time.Saturday)
		matchHour, matchMin, foundTime, matchTimeStr := ExtractMatchTime(msgs, isSat)

		remindHour := matchHour - 1
		remindMin := matchMin

		// If time was not explicitly found in messages, set fallback default:
		// - Tue/Thu: remind at 18:00 (1 hour before 19:00)
		// - Sat: remind at 13:00 (1 hour before 14:00)
		if !foundTime {
			if isSat {
				remindHour = 13
				remindMin = 0
			} else {
				remindHour = 18
				remindMin = 0
			}
		}

		if hour == remindHour && minute == remindMin {
			slotKey := now.Format("2006-01-02_reminder")
			m.mu.Lock()
			already := m.state.LastReminder == slotKey
			m.mu.Unlock()
			if !already {
				m.mu.Lock()
				m.state.LastReminder = slotKey
				m.saveStateLocked()
				m.mu.Unlock()

				m.log.Info("triggering match reminder based on detected time",
					"detected_time", matchTimeStr, "found", foundTime, "remind_at", fmt.Sprintf("%02d:%02d", remindHour, remindMin))

				if err := m.TriggerReminderWithTime(ctx, now, matchTimeStr, foundTime); err != nil {
					m.log.Error("failed to trigger auto reminder", "err", err)
				}
			}
		}
	}

	// 3. Monthly Leaderboard: Last day of the month at 20:00
	if now.AddDate(0, 0, 1).Day() == 1 && hour == 20 && minute == 0 {
		monthKey := now.Format("2006-01")
		m.mu.Lock()
		alreadyBXH := m.state.LastBXH == monthKey
		m.mu.Unlock()

		if !alreadyBXH {
			m.mu.Lock()
			m.state.LastBXH = monthKey
			m.saveStateLocked()
			m.mu.Unlock()

			if bxhText, err := m.GetLeaderboardText(now.Format("01/2006"), now); err == nil && bxhText != "" {
				_, _ = m.sendWithRoleMention(ctx, bxhText)
			}
		}
	}
}

// ExtractMatchTime parses messages to determine the confirmed booking time.
// On Tuesdays/Thursdays, typical booking time is 19:00 or 19:30 (with a ~30m variance).
// On Saturdays, typical booking time is 14:00 (e.g. "2-4", "2-3", "13h30", "14h").
func ExtractMatchTime(messages []MessageRecord, isSaturday bool) (hour int, min int, found bool, formatted string) {
	reColon := regexp.MustCompile(`(?i)\b(1[2-9]|2[0-2]):([0-5][0-9])\b`)
	reHM := regexp.MustCompile(`(?i)\b(1[2-9]|2[0-2])h([0-5][0-9])\b`)
	reH := regexp.MustCompile(`(?i)\b(1[2-9]|2[0-2])h\b`)
	reEveningHM := regexp.MustCompile(`(?i)\b(6|7|8)h([0-5][0-9])\b`)
	reEveningH := regexp.MustCompile(`(?i)\b(6|7|8)h\b`)
	reSatRange := regexp.MustCompile(`(?i)\b(1|2|3)\s*[-h]\s*(3|4|5)\b`)

	// Scan messages from newest to oldest for the latest booking confirmation
	for i := len(messages) - 1; i >= 0; i-- {
		c := strings.ToLower(messages[i].Content)

		// 1. Exact HH:MM (e.g. 19:30, 19:00, 18:30)
		if m := reColon.FindStringSubmatch(c); len(m) == 3 {
			h, _ := strconv.Atoi(m[1])
			mn, _ := strconv.Atoi(m[2])
			return h, mn, true, fmt.Sprintf("%02d:%02d", h, mn)
		}

		// 2. Exact HHhMM (e.g. 19h30, 18h30)
		if m := reHM.FindStringSubmatch(c); len(m) == 3 {
			h, _ := strconv.Atoi(m[1])
			mn, _ := strconv.Atoi(m[2])
			return h, mn, true, fmt.Sprintf("%02d:%02d", h, mn)
		}

		// 3. Exact HHh (e.g. 19h, 18h)
		if m := reH.FindStringSubmatch(c); len(m) == 2 {
			h, _ := strconv.Atoi(m[1])
			return h, 0, true, fmt.Sprintf("%02d:00", h)
		}

		// 4. Evening shorthand like 7h30 -> 19:30, 7h -> 19:00
		if m := reEveningHM.FindStringSubmatch(c); len(m) == 3 {
			h, _ := strconv.Atoi(m[1])
			mn, _ := strconv.Atoi(m[2])
			return h + 12, mn, true, fmt.Sprintf("%02d:%02d", h+12, mn)
		}
		if m := reEveningH.FindStringSubmatch(c); len(m) == 2 {
			h, _ := strconv.Atoi(m[1])
			return h + 12, 0, true, fmt.Sprintf("%02d:00", h+12)
		}

		// 5. Saturday shorthand like "2-4", "2h-4h" -> 14:00
		if isSaturday {
			if m := reSatRange.FindStringSubmatch(c); len(m) >= 2 {
				h, _ := strconv.Atoi(m[1])
				return h + 12, 0, true, fmt.Sprintf("%02d:00", h+12)
			}
		}
	}

	if isSaturday {
		return 14, 0, false, "14:00"
	}
	return 19, 0, false, "19:00 - 19:30"
}

// NextMatchInfo returns the match day and time for a check-in call on the given day.
func NextMatchInfo(now time.Time) (matchDay, matchDate, matchTime string) {
	weekday := now.Weekday()
	var targetTime time.Time
	var mTime string

	switch weekday {
	case time.Monday:
		// Điểm danh T2 cho T3 (thường 19:00 - 19:30)
		targetTime = now.AddDate(0, 0, 1)
		matchDay = "Thứ Ba"
		mTime = "19:00 - 19:30"
	case time.Wednesday:
		// Điểm danh T4 cho T5 (thường 19:00 - 19:30)
		targetTime = now.AddDate(0, 0, 1)
		matchDay = "Thứ Năm"
		mTime = "19:00 - 19:30"
	case time.Friday:
		// Điểm danh T6 cho T7 (thường 14:00 chiều)
		targetTime = now.AddDate(0, 0, 1)
		matchDay = "Thứ Bảy"
		mTime = "14:00"
	default:
		// Mặc định ngày mai
		targetTime = now.AddDate(0, 0, 1)
		matchDay = "ngày mai"
		mTime = "19:00 - 19:30"
	}

	matchDate = targetTime.Format("02/01/2006")
	return matchDay, matchDate, mTime
}

// DayOfWeekVietnamese converts time.Weekday to Vietnamese name.
func DayOfWeekVietnamese(w time.Weekday) string {
	switch w {
	case time.Monday:
		return "Thứ Hai"
	case time.Tuesday:
		return "Thứ Ba"
	case time.Wednesday:
		return "Thứ Tư"
	case time.Thursday:
		return "Thứ Năm"
	case time.Friday:
		return "Thứ Sáu"
	case time.Saturday:
		return "Thứ Bảy"
	case time.Sunday:
		return "Chủ Nhật"
	default:
		return ""
	}
}

// TriggerDiemDanh sends the attendance call message to the sport channel.
func (m *Manager) TriggerDiemDanh(ctx context.Context, now time.Time) error {
	matchDay, matchDate, matchTime := NextMatchInfo(now)

	m.mu.Lock()
	// Archive previous match session participants before starting fresh attendance
	var prev []Participant
	for _, p := range m.state.GoingParticipants {
		prev = append(prev, p)
	}
	if len(prev) == 0 {
		for uid, name := range m.state.ReactionsGoing {
			prev = append(prev, Participant{UserID: uid, Username: name, Slots: 1})
		}
	}
	if len(prev) > 0 {
		sort.Slice(prev, func(i, j int) bool {
			return prev[i].Username < prev[j].Username
		})
		m.state.PreviousMatchParticipants = prev
		m.state.PreviousMatchDate = m.state.MatchDate
		m.state.PreviousMatchDay = m.state.MatchDay
	}

	// Archive completed match into MatchHistory
	if m.state.MatchDate != "" && (len(prev) > 0 || len(m.state.ReactionsNotGoing) > 0) {
		var absentees []string
		for _, uname := range m.state.ReactionsNotGoing {
			absentees = append(absentees, uname)
		}
		sort.Strings(absentees)

		alreadyExists := false
		for _, h := range m.state.MatchHistory {
			if h.MatchDate == m.state.MatchDate {
				alreadyExists = true
				break
			}
		}
		if !alreadyExists {
			m.state.MatchHistory = append(m.state.MatchHistory, MatchRecord{
				SessionID:   m.state.SessionID,
				MatchDay:    m.state.MatchDay,
				MatchDate:   m.state.MatchDate,
				Attendees:   prev,
				Absentees:   absentees,
				CompletedAt: now,
			})
		}
	}

	m.state.SessionID = fmt.Sprintf("match-%s", matchDate)
	m.state.CallTime = now
	m.state.MatchDay = matchDay
	m.state.MatchDate = matchDate
	m.state.MatchTime = matchTime
	m.state.DiemDanhMessageID = ""
	// Clear current session messages and reactions to start fresh attendance collection
	m.state.Messages = make([]MessageRecord, 0)
	m.state.ReactionsGoing = make(map[string]string)
	m.state.ReactionsNotGoing = make(map[string]string)
	m.state.GoingParticipants = make(map[string]Participant)
	m.saveStateLocked()
	m.mu.Unlock()

	tag := "@" + m.cfg.RoleName
	msg := fmt.Sprintf(
		"🏸 **[ĐIỂM DANH CẦU LÔNG - %s (%s)]**\n\n"+
			"Chào cả nhà %s! Lịch đánh cầu lông tiếp theo diễn ra vào **%s (%s)** (khung giờ dự kiến: **%s**).\n\n"+
			"👉 Mọi người vào điểm danh xác nhận tham gia trước **16:00** chiều nay để tiện chốt người và đặt sân nhé! 🔥\n\n"+
			"*(Đi thì react :cong1:, không đi thì react :tru1: vào tin nhắn này nhé!)*",
		strings.ToUpper(matchDay), matchDate, tag, matchDay, matchDate, matchTime,
	)

	msgID, err := m.sendWithRoleMention(ctx, msg)
	if err != nil {
		return err
	}

	if msgID != "" {
		m.mu.Lock()
		m.state.DiemDanhMessageID = msgID
		m.saveStateLocked()
		m.mu.Unlock()

		// Auto-react with :cong1: and :tru1: to seed the buttons for members to click
		if m.sender != nil {
			_ = m.sender.ReactToMessage(ctx, m.cfg.ChannelID, msgID, ":cong1:")
			_ = m.sender.ReactToMessage(ctx, m.cfg.ChannelID, msgID, ":tru1:")
		}
	}

	return nil
}

// TriggerReminder compiles an AI summary of attendance messages and sends a 1-hour pre-match reminder.
func (m *Manager) TriggerReminder(ctx context.Context, now time.Time) error {
	m.mu.Lock()
	messages := append([]MessageRecord(nil), m.state.Messages...)
	isSat := now.Weekday() == time.Saturday
	_, _, foundTime, detectedTime := ExtractMatchTime(messages, isSat)
	m.mu.Unlock()

	return m.TriggerReminderWithTime(ctx, now, detectedTime, foundTime)
}

// TriggerReminderWithTime sends the reminder using the detected match time.
func (m *Manager) TriggerReminderWithTime(ctx context.Context, now time.Time, matchTimeStr string, isExplicitTime bool) error {
	m.mu.Lock()
	messages := append([]MessageRecord(nil), m.state.Messages...)
	matchDay := m.state.MatchDay
	matchDate := m.state.MatchDate
	m.mu.Unlock()

	if matchDay == "" {
		matchDay = "Hôm nay"
		matchDate = now.Format("02/01/2006")
	}

	summary := m.SummarizeMessages(ctx, messages)
	tag := "@" + m.cfg.RoleName

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🏸 **[NHẮC LỊCH ĐÁNH CẦU - %s (%s)]**\n\n", strings.ToUpper(matchDay), matchDate))

	if isExplicitTime {
		sb.WriteString(fmt.Sprintf("Chào %s, còn **1 tiếng** nữa là đến giờ quật cầu rồi (⏰ **%s** hôm nay)! 🏸\n\n", tag, matchTimeStr))
	} else {
		sb.WriteString(fmt.Sprintf("Chào %s, chuẩn bị đồ đạc nào, còn **1 tiếng** nữa là đến giờ đánh cầu dự kiến (⏰ **%s**)! 🏸\n\n", tag, matchTimeStr))
	}

	sb.WriteString("📋 **TỔNG KẾT TÌNH HÌNH ĐIỂM DANH & ĐẶT SÂN:**\n")

	// Format attendance strictly from reactions
	if len(summary.Going) > 0 {
		slotText := fmt.Sprintf("%d người", len(summary.Going))
		if summary.TotalSlots > len(summary.Going) {
			slotText = fmt.Sprintf("%d người - %d slot", len(summary.Going), summary.TotalSlots)
		}
		sb.WriteString(fmt.Sprintf("• ✅ **Tham gia (%s):** %s\n", slotText, strings.Join(summary.Going, ", ")))
	} else {
		sb.WriteString("• ✅ **Tham gia:** Chưa có ai điểm danh trên nhóm (react :cong1: để tham gia nhé!)\n")
	}

	if len(summary.NotGoing) > 0 {
		sb.WriteString(fmt.Sprintf("• ❌ **Vắng mặt (%d người):** %s\n", len(summary.NotGoing), strings.Join(summary.NotGoing, ", ")))
	}

	court := strings.TrimSpace(summary.CourtInfo)
	if court != "" && !strings.EqualFold(court, "Chưa có thông tin sân") {
		sb.WriteString(fmt.Sprintf("• 🏟️ **Sân bãi:** %s\n", court))
	} else {
		sb.WriteString("• 🏟️ **Sân bãi:** Chưa thấy thông tin chốt sân trên nhóm (anh em kiểm tra/đặt sân nhé!)\n")
	}

	if isExplicitTime {
		sb.WriteString(fmt.Sprintf("• ⏰ **Giờ đánh đã đặt:** %s (nhắc trước 1 tiếng)\n", matchTimeStr))
	} else {
		sb.WriteString(fmt.Sprintf("• ⏰ **Giờ đánh dự kiến:** %s (vui lòng kiểm tra lại giờ sân đã đặt)\n", matchTimeStr))
	}

	if len(summary.Notes) > 0 {
		sb.WriteString(fmt.Sprintf("• 💬 **Ghi chú:** %s\n", strings.Join(summary.Notes, " | ")))
	}

	sb.WriteString("\nChúc anh em một buổi giao lưu quật cầu thật cháy và nhiều tiếng cười! 🔥💪")

	_, err := m.sendWithRoleMention(ctx, sb.String())
	return err
}

// SummarizeMessages gathers attendance strictly from reactions, and extracts court info from messages via AI or NLP.
func (m *Manager) SummarizeMessages(ctx context.Context, messages []MessageRecord) SummaryResult {
	m.mu.Lock()
	var going []string
	totalSlots := 0

	// 1. Attendance strictly from reactions
	if len(m.state.GoingParticipants) > 0 {
		for _, p := range m.state.GoingParticipants {
			disp := p.DisplayName
			if disp == "" {
				disp = m.ResolveDisplayName(p.UserID, p.Username)
			}
			if disp == "" {
				disp = p.Username
			}
			if disp == "" {
				disp = p.UserID
			}
			if p.Slots > 1 {
				going = append(going, fmt.Sprintf("%s (%d slot)", disp, p.Slots))
			} else {
				going = append(going, disp)
			}
			totalSlots += p.Slots
		}
	} else if len(m.state.ReactionsGoing) > 0 {
		for uid, uname := range m.state.ReactionsGoing {
			disp := m.ResolveDisplayName(uid, uname)
			if disp == "" {
				disp = uname
			}
			if disp == "" {
				disp = uid
			}
			going = append(going, disp)
			totalSlots++
		}
	}

	var notGoing []string
	for uid, uname := range m.state.ReactionsNotGoing {
		disp := m.ResolveDisplayName(uid, uname)
		if disp == "" {
			disp = uname
		}
		if disp == "" {
			disp = uid
		}
		notGoing = append(notGoing, disp)
	}
	m.mu.Unlock()

	sort.Strings(going)
	sort.Strings(notGoing)

	// 2. Court info extracted from messages (AI preferred, fallback to NLP)
	courtInfo := ""
	var notes []string

	if len(messages) > 0 {
		if m.cfg.OpenAIKey != "" {
			aiCourt, err := m.callOpenAICourtInfo(ctx, messages)
			if err == nil && strings.TrimSpace(aiCourt) != "" {
				courtInfo = cleanCourtOutput(aiCourt)
			} else if err != nil {
				m.log.Warn("failed to call openai court extraction, falling back", "err", err)
			}
		}

		if courtInfo == "" && m.cfg.GeminiKey != "" {
			aiCourt, err := m.callGeminiCourtInfo(ctx, messages)
			if err == nil && strings.TrimSpace(aiCourt) != "" {
				courtInfo = cleanCourtOutput(aiCourt)
			} else if err != nil {
				m.log.Warn("failed to call gemini court extraction, falling back", "err", err)
			}
		}

		if courtInfo == "" {
			nlpRes := m.ParseMessagesNLP(messages)
			courtInfo = nlpRes.CourtInfo
			notes = nlpRes.Notes
		}
	}

	if courtInfo == "" {
		courtInfo = "Chưa có thông tin sân"
	}

	return SummaryResult{
		Going:      going,
		TotalSlots: totalSlots,
		NotGoing:   notGoing,
		CourtInfo:  courtInfo,
		Notes:      notes,
		RawAI:      courtInfo,
	}
}

// ParseMessagesNLP extracts attendance, absences, and court info using Vietnamese badminton patterns.
func (m *Manager) ParseMessagesNLP(messages []MessageRecord) SummaryResult {
	resolver := m.ResolveDisplayName
	return parseMessagesNLPInternal(messages, resolver)
}

// ParseMessagesNLP is also provided as a standalone function for backwards compatibility with tests.
func ParseMessagesNLP(messages []MessageRecord) SummaryResult {
	return parseMessagesNLPInternal(messages, nil)
}

func parseMessagesNLPInternal(messages []MessageRecord, resolver func(userID, username string) string) SummaryResult {
	goingMap := make(map[string]bool)
	notGoingMap := make(map[string]bool)
	var courtMentions []string
	var notes []string

	reGoing := regexp.MustCompile(`(?i)(\+1|1 slot|di nha|đi nha|di luon|đi luôn|co mat|có mặt|em di|em đi|minh di|mình đi|chieu danh|chiều đánh|ra san|ra sân|quật|chien|chiến|giao lưu|tham gia)`)
	reNotGoing := regexp.MustCompile(`(?i)(xin off|off|bận|ban|nghi nha|nghỉ nha|ko di|không đi|k đi|ko dc|không được|k dc|k tham gia|huy|hủy|chịu nha)`)
	reCourt := regexp.MustCompile(`(?i)(sân\s+\d+|san\s+\d+|sân\s+[a-zA-Z0-9\s]+|Minh Hải|minh hai|đặt sân|chưa có sân|k có sân|không có sân|full sân|hết sân)`)

	for _, msg := range messages {
		name := msg.DisplayName
		if name == "" && resolver != nil {
			name = resolver(msg.UserID, msg.Username)
		}
		if name == "" {
			name = msg.Username
		}
		if name == "" {
			name = msg.UserID
		}
		c := strings.ToLower(msg.Content)

		// Court information detection
		if courtMatches := reCourt.FindAllString(msg.Content, -1); len(courtMatches) > 0 {
			courtMentions = append(courtMentions, msg.Content)
		}

		// Not going check
		if reNotGoing.MatchString(c) {
			delete(goingMap, name)
			notGoingMap[name] = true
			continue
		}

		// Going check
		if reGoing.MatchString(c) {
			delete(notGoingMap, name)
			goingMap[name] = true
			continue
		}
	}

	var going []string
	for u := range goingMap {
		going = append(going, u)
	}

	var notGoing []string
	for u := range notGoingMap {
		notGoing = append(notGoing, u)
	}

	courtInfo := ""
	if len(courtMentions) > 0 {
		// Use the latest court mention
		courtInfo = courtMentions[len(courtMentions)-1]
	}

	return SummaryResult{
		Going:     going,
		NotGoing:  notGoing,
		CourtInfo: courtInfo,
		Notes:     notes,
	}
}

func cleanCourtOutput(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Chưa có thông tin sân"
	}
	lines := strings.Split(s, "\n")
	courtLine := ""
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		lower := strings.ToLower(l)
		if strings.Contains(l, "🏟️") || strings.Contains(lower, "sân") || strings.Contains(lower, "minh hải") {
			courtLine = l
			break
		}
	}
	if courtLine != "" {
		s = courtLine
	}

	for {
		trimmed := false
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "•") {
			s = strings.TrimPrefix(s, "•")
			trimmed = true
		}
		if strings.HasPrefix(s, "-") {
			s = strings.TrimPrefix(s, "-")
			trimmed = true
		}
		if strings.HasPrefix(s, "🏟️") {
			s = strings.TrimPrefix(s, "🏟️")
			trimmed = true
		}
		if strings.HasPrefix(s, "**Sân bãi:**") {
			s = strings.TrimPrefix(s, "**Sân bãi:**")
			trimmed = true
		}
		if strings.HasPrefix(s, "**Sân:**") {
			s = strings.TrimPrefix(s, "**Sân:**")
			trimmed = true
		}
		if strings.HasPrefix(s, "Sân bãi:") {
			s = strings.TrimPrefix(s, "Sân bãi:")
			trimmed = true
		}
		if strings.HasPrefix(s, "Sân:") {
			s = strings.TrimPrefix(s, "Sân:")
			trimmed = true
		}
		if !trimmed {
			break
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "Chưa có thông tin sân"
	}
	return s
}

func (m *Manager) buildTranscript(messages []MessageRecord, goingReactions, notGoingReactions []string) string {
	return m.buildMessagesTranscript(messages)
}

func (m *Manager) buildMessagesTranscript(messages []MessageRecord) string {
	var sb strings.Builder
	for _, msg := range messages {
		name := msg.DisplayName
		if name == "" {
			name = m.ResolveDisplayName(msg.UserID, msg.Username)
		}
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, msg.Content))
	}
	return sb.String()
}

func (m *Manager) callOpenAICourtInfo(ctx context.Context, messages []MessageRecord) (string, error) {
	transcript := m.buildMessagesTranscript(messages)
	if strings.TrimSpace(transcript) == "" {
		return "Chưa có thông tin sân", nil
	}

	baseURL := m.cfg.OpenAIBaseURL
	if baseURL == "" {
		baseURL = "https://llm.mrdnd.dev/v1"
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"

	model := m.cfg.OpenAIModel
	if model == "" {
		model = "gpt-4o-mini"
	}

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{
				"role": "system",
				"content": "Bạn là trợ lý cho câu lạc bộ cầu lông Vinh-CL. Nhiệm vụ của bạn là đọc các tin nhắn thảo luận trong nhóm và trích xuất thông tin về SÂN BÃI ĐÃ ĐẶT (tên sân, số sân, khung giờ đánh nếu có).\n" +
					"QUY TẮC:\n" +
					"1. Chỉ trích xuất thông tin sân bãi, số sân và khung giờ chơi (ví dụ: 'Sân Minh Hải, Sân 6, khung giờ 19:30 - 21:30 (tối nay)').\n" +
					"2. TUYỆT ĐỐI KHÔNG liệt kê danh sách người tham gia hay người vắng mặt.\n" +
					"3. Nếu trong tin nhắn chưa có ai chốt sân hoặc chưa có thông tin đặt sân, hãy trả lời ngắn gọn: 'Chưa có thông tin sân'.",
			},
			{
				"role":    "user",
				"content": fmt.Sprintf("Dưới đây là các tin nhắn trao đổi trong nhóm:\n\n%s\n\nHãy trích xuất thông tin sân bãi đã đặt:", transcript),
			},
		},
		"temperature": 0.2,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.cfg.OpenAIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.cfg.OpenAIKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai api HTTP %d: %s", resp.StatusCode, string(b))
	}

	var openAIResp struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return "", err
	}

	if len(openAIResp.Choices) > 0 {
		return strings.TrimSpace(openAIResp.Choices[0].Message.Content), nil
	}

	return "", fmt.Errorf("empty openai choices response")
}

func (m *Manager) callOpenAISummary(ctx context.Context, messages []MessageRecord, goingReactions, notGoingReactions []string) (string, error) {
	return m.callOpenAICourtInfo(ctx, messages)
}

func (m *Manager) callGeminiCourtInfo(ctx context.Context, messages []MessageRecord) (string, error) {
	transcript := m.buildMessagesTranscript(messages)
	if strings.TrimSpace(transcript) == "" {
		return "Chưa có thông tin sân", nil
	}

	prompt := fmt.Sprintf(
		"Dưới đây là các tin nhắn thảo luận trong nhóm chat câu lạc bộ cầu lông Vinh-CL:\n\n%s\n\n"+
			"Hãy trích xuất ngắn gọn thông tin về SÂN BÃI ĐÃ ĐẶT (tên sân, số sân, khung giờ chơi nếu có).\n"+
			"QUY TẮC:\n"+
			"1. Chỉ trích xuất thông tin sân bãi và giờ đánh. TUYỆT ĐỐI KHÔNG liệt kê danh sách người tham gia hoặc người vắng mặt.\n"+
			"2. Nếu chưa có thông tin đặt sân nào trong các tin nhắn, hãy trả về chính xác: 'Chưa có thông tin sân'.\n"+
			"Chỉ trả về nội dung ngắn gọn về sân bãi, không thêm lời chào hay giải thích rườm rà.",
		transcript,
	)

	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=%s", m.cfg.GeminiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini api HTTP %d: %s", resp.StatusCode, string(b))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return strings.TrimSpace(geminiResp.Candidates[0].Content.Parts[0].Text), nil
	}

	return "", fmt.Errorf("empty gemini response")
}

func (m *Manager) callGeminiSummary(ctx context.Context, messages []MessageRecord, goingReactions, notGoingReactions []string) (string, error) {
	return m.callGeminiCourtInfo(ctx, messages)
}

func (m *Manager) sendWithRoleMention(ctx context.Context, text string) (string, error) {
	if m.sender == nil {
		m.log.Warn("no message sender configured for sport manager")
		return "", nil
	}

	var opts *mezon.SendOptions
	tag := "@" + m.cfg.RoleName
	s, e, ok := mezon.MentionSpan(text, tag)
	if ok && m.cfg.RoleID != "" {
		opts = &mezon.SendOptions{
			Mentions: []mezon.Mention{
				{
					RoleID:   m.cfg.RoleID,
					Rolename: m.cfg.RoleName,
					S:        s,
					E:        e,
				},
			},
		}
	}

	return m.sender.SendToChannel(ctx, m.cfg.ChannelID, text, opts)
}

// GetSummary returns the current summary (used for interactive *summary command).
func (m *Manager) GetSummary(ctx context.Context) string {
	m.mu.Lock()
	messages := append([]MessageRecord(nil), m.state.Messages...)
	m.mu.Unlock()

	summary := m.SummarizeMessages(ctx, messages)

	var sb strings.Builder
	sb.WriteString("📋 **TÌNH HÌNH ĐIỂM DANH HIỆN TẠI:**\n")
	if len(summary.Going) > 0 {
		slotText := fmt.Sprintf("%d người", len(summary.Going))
		if summary.TotalSlots > len(summary.Going) {
			slotText = fmt.Sprintf("%d người - %d slot", len(summary.Going), summary.TotalSlots)
		}
		sb.WriteString(fmt.Sprintf("• ✅ **Tham gia (%s):** %s\n", slotText, strings.Join(summary.Going, ", ")))
	} else {
		sb.WriteString("• ✅ **Tham gia:** Chưa có ai điểm danh (react :cong1: để tham gia nhé!)\n")
	}

	if len(summary.NotGoing) > 0 {
		sb.WriteString(fmt.Sprintf("• ❌ **Vắng mặt (%d người):** %s\n", len(summary.NotGoing), strings.Join(summary.NotGoing, ", ")))
	}

	court := strings.TrimSpace(summary.CourtInfo)
	if court != "" && !strings.EqualFold(court, "Chưa có thông tin sân") {
		sb.WriteString(fmt.Sprintf("• 🏟️ **Sân:** %s\n", court))
	} else {
		sb.WriteString("• 🏟️ **Sân:** Chưa có thông tin sân\n")
	}

	return sb.String()
}

// GetPaymentQR returns the active payment QR image URL.
func (m *Manager) GetPaymentQR() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.CustomPaymentQR != "" {
		return m.state.CustomPaymentQR
	}
	return m.cfg.PaymentQR
}

// SetPaymentQR updates the custom payment QR code URL.
func (m *Manager) SetPaymentQR(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.CustomPaymentQR = strings.TrimSpace(url)
	m.saveStateLocked()
}

var reTwoSlots = regexp.MustCompile(`(?i)(\+2|2\s*slot|2\s*vé|2\s*ve|2\s*người|2\s*nguoi)`)

// GetParticipantsForMoney returns the target match day, date, and participants list for bill splitting.
func (m *Manager) GetParticipantsForMoney() (matchDay string, matchDate string, participants []Participant) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if current session match date is in the future, and we have archived previous match participants
	now := time.Now().In(m.cfg.Timezone)
	currentMatchInFuture := false
	if m.state.MatchDate != "" {
		if t, err := time.ParseInLocation("02/01/2006", m.state.MatchDate, m.cfg.Timezone); err == nil {
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, m.cfg.Timezone)
			if t.After(today) {
				currentMatchInFuture = true
			}
		}
	}

	// If current session is for a future match and previous match has participants, use previous match
	if currentMatchInFuture && len(m.state.PreviousMatchParticipants) > 0 {
		return m.state.PreviousMatchDay, m.state.PreviousMatchDate, m.state.PreviousMatchParticipants
	}

	// Otherwise, build from current session
	var current []Participant
	for _, p := range m.state.GoingParticipants {
		current = append(current, p)
	}
	if len(current) == 0 {
		for uid, name := range m.state.ReactionsGoing {
			current = append(current, Participant{UserID: uid, Username: name, Slots: 1})
		}
	}

	// Check recent chat messages in current session for slot upgrades
	current = m.enrichParticipantsWithMessagesLocked(current)

	if len(current) > 0 {
		sort.Slice(current, func(i, j int) bool {
			return current[i].Username < current[j].Username
		})
		return m.state.MatchDay, m.state.MatchDate, current
	}

	// Fallback to previous match if current session is empty
	if len(m.state.PreviousMatchParticipants) > 0 {
		return m.state.PreviousMatchDay, m.state.PreviousMatchDate, m.state.PreviousMatchParticipants
	}

	return m.state.MatchDay, m.state.MatchDate, nil
}

func (m *Manager) enrichParticipantsWithMessagesLocked(participants []Participant) []Participant {
	userMap := make(map[string]int)
	for i, p := range participants {
		userMap[strings.ToLower(p.Username)] = i
	}

	for _, msg := range m.state.Messages {
		uLower := strings.ToLower(msg.Username)
		idx, exists := userMap[uLower]
		if exists {
			if reTwoSlots.MatchString(msg.Content) && participants[idx].Slots < 2 {
				participants[idx].Slots = 2
			}
		}
	}
	return participants
}

// ParseMoneyAmount parses strings like "45k", "45K", "45000", "45.000", "45" into integer VND amount.
func ParseMoneyAmount(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("vui lòng nhập số tiền, ví dụ: *money 45k")
	}
	s = strings.ToLower(s)
	s = strings.TrimSuffix(s, "vnd")
	s = strings.TrimSuffix(s, "vnđ")
	s = strings.TrimSuffix(s, "đ")
	s = strings.TrimSuffix(s, "d")
	s = strings.TrimSpace(s)

	hasK := false
	if strings.HasSuffix(s, "k") {
		hasK = true
		s = strings.TrimSuffix(s, "k")
	}

	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)

	val, err := strconv.Atoi(s)
	if err != nil || val <= 0 {
		return 0, fmt.Errorf("số tiền không hợp lệ: %q", raw)
	}

	if hasK || val < 1000 {
		val *= 1000
	}

	return val, nil
}

// DefaultShuttlePrice is 26,000 VND per shuttlecock as standard for Vinh-CL.
const DefaultShuttlePrice = 26000

var reMult = regexp.MustCompile(`(?i)^\s*(\d+(?:\.\d+)?k?)\s*[*xX]\s*(\d+(?:\.\d+)?k?)\s*(?:quả|qua|cầu|cau)?\s*$`)
var reCauCount = regexp.MustCompile(`(?i)^\s*(\d+)\s*(?:quả|qua|cầu|cau|trái|trai)\s*(?:cầu|cau)?\s*$`)

func cleanPart(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "sân")
	s = strings.TrimPrefix(s, "san")
	s = strings.TrimSuffix(s, "sân")
	s = strings.TrimSuffix(s, "san")
	return strings.TrimSpace(s)
}

func parseShuttlePart(raw string, isExplicitCau bool) (count int, price int64, fee int64, err error) {
	s := strings.TrimSpace(raw)

	// Check multiplication: e.g. 26*3, 26k*3, 3*26k, 3*26, 26.000 * 3
	if m := reMult.FindStringSubmatch(raw); len(m) == 3 {
		op1Str, op2Str := m[1], m[2]
		var countVal int
		var priceVal int64

		hasK1 := strings.Contains(strings.ToLower(op1Str), "k")
		hasK2 := strings.Contains(strings.ToLower(op2Str), "k")

		if hasK1 && !hasK2 {
			p, err := ParseMoneyAmount(op1Str)
			if err != nil {
				return 0, 0, 0, err
			}
			c, err := strconv.Atoi(op2Str)
			if err != nil {
				return 0, 0, 0, err
			}
			priceVal = int64(p)
			countVal = c
		} else if hasK2 && !hasK1 {
			p, err := ParseMoneyAmount(op2Str)
			if err != nil {
				return 0, 0, 0, err
			}
			c, err := strconv.Atoi(op1Str)
			if err != nil {
				return 0, 0, 0, err
			}
			priceVal = int64(p)
			countVal = c
		} else {
			// e.g. "26*3", "3*26", or "26.000*3"
			p1, err1 := ParseMoneyAmount(op1Str)
			p2, err2 := ParseMoneyAmount(op2Str)
			if err1 == nil && err2 == nil {
				if p1 > p2 {
					priceVal = int64(p1)
					countVal = p2 / 1000
					if countVal == 0 {
						countVal = p2
					}
				} else {
					priceVal = int64(p2)
					countVal = p1 / 1000
					if countVal == 0 {
						countVal = p1
					}
				}
			} else {
				v1, err1 := strconv.Atoi(strings.ReplaceAll(op1Str, ".", ""))
				v2, err2 := strconv.Atoi(strings.ReplaceAll(op2Str, ".", ""))
				if err1 != nil || err2 != nil {
					return 0, 0, 0, fmt.Errorf("không hiểu phép nhân cầu: %q", raw)
				}
				if v1 < v2 {
					countVal = v1
					priceVal = int64(v2)
				} else {
					countVal = v2
					priceVal = int64(v1)
				}
				if priceVal < 1000 {
					priceVal *= 1000
				}
			}
		}

		return countVal, priceVal, int64(countVal) * priceVal, nil
	}

	// Check count only: e.g. "3 cau", "3 quả", "4 trái cầu"
	if m := reCauCount.FindStringSubmatch(raw); len(m) == 2 {
		c, err := strconv.Atoi(m[1])
		if err == nil && c > 0 {
			priceVal := int64(DefaultShuttlePrice)
			return c, priceVal, int64(c) * priceVal, nil
		}
	}

	// If explicit "cau" keyword was present in expression and part is a small number (1-30)
	if isExplicitCau {
		if c, err := strconv.Atoi(s); err == nil && c > 0 && c <= 30 {
			priceVal := int64(DefaultShuttlePrice)
			return c, priceVal, int64(c) * priceVal, nil
		}
	}

	// Check direct money amount: e.g. "45k", "78000"
	amt, err := ParseMoneyAmount(s)
	if err == nil {
		priceVal := int64(DefaultShuttlePrice)
		if int64(amt)%priceVal == 0 {
			countVal := int(int64(amt) / priceVal)
			return countVal, priceVal, int64(amt), nil
		}
		return 0, 0, int64(amt), nil
	}

	return 0, 0, 0, fmt.Errorf("không hiểu số lượng/tiền cầu: %q", raw)
}

// ParseSplitFormula parses badminton split expressions:
// e.g. "360k + 26*3", "360k + 26k*4 cau", "360k san 45k cau", "360k san 3 cau", "450k".
func ParseSplitFormula(expr string) (*SplitBreakdown, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("vui lòng nhập chi phí, ví dụ: *split 360k + 26*3 hoặc *split 360k san 45k cau")
	}

	var courtStr string
	var shuttleStr string
	isExplicitCau := false

	if strings.Contains(expr, "+") {
		parts := strings.SplitN(expr, "+", 2)
		courtStr = cleanPart(parts[0])
		shuttleStr = strings.TrimSpace(parts[1])
	} else {
		reKeywords := regexp.MustCompile(`(?i)^(.+?)(?:\s*(?:sân|san))\s+(.+?)(?:\s*(?:cầu|cau))?$`)
		if m := reKeywords.FindStringSubmatch(expr); len(m) == 3 {
			courtStr = cleanPart(m[1])
			shuttleStr = strings.TrimSpace(m[2])
			if strings.Contains(strings.ToLower(expr), "cau") || strings.Contains(strings.ToLower(expr), "cầu") {
				isExplicitCau = true
			}
		} else {
			reEndCau := regexp.MustCompile(`(?i)^(.+?)\s+(\d+(?:[*xX]\d+)?\s*(?:cầu|cau|quả|qua)?)$`)
			if m := reEndCau.FindStringSubmatch(expr); len(m) == 3 {
				courtStr = cleanPart(m[1])
				shuttleStr = strings.TrimSpace(m[2])
				isExplicitCau = true
			} else {
				courtStr = cleanPart(expr)
			}
		}
	}

	courtFee, err := ParseMoneyAmount(courtStr)
	if err != nil {
		return nil, fmt.Errorf("tiền sân không hợp lệ (%s): %v", courtStr, err)
	}

	res := &SplitBreakdown{
		CourtFee: int64(courtFee),
	}

	if shuttleStr != "" {
		c, p, f, err := parseShuttlePart(shuttleStr, isExplicitCau)
		if err != nil {
			return nil, err
		}
		res.ShuttleCount = c
		res.ShuttlePrice = p
		res.ShuttleFee = f
	}

	res.TotalFee = res.CourtFee + res.ShuttleFee
	return res, nil
}

// LookupParticipant searches existing attendance state, history, and records for a user by name or ID.
func (m *Manager) LookupParticipant(query string) (userID, username string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	clean := strings.ToLower(strings.TrimPrefix(query, "@"))
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return "", "", false
	}

	// 1. Current going participants
	for uid, p := range m.state.GoingParticipants {
		if strings.ToLower(strings.TrimPrefix(p.Username, "@")) == clean || uid == clean {
			return p.UserID, p.Username, true
		}
	}

	// 2. Previous match participants
	for _, p := range m.state.PreviousMatchParticipants {
		if strings.ToLower(strings.TrimPrefix(p.Username, "@")) == clean || p.UserID == clean {
			return p.UserID, p.Username, true
		}
	}

	// 3. Match history
	for _, mh := range m.state.MatchHistory {
		for _, p := range mh.Attendees {
			if strings.ToLower(strings.TrimPrefix(p.Username, "@")) == clean || p.UserID == clean {
				return p.UserID, p.Username, true
			}
		}
	}

	// 4. Message records
	for _, msg := range m.state.Messages {
		if strings.ToLower(strings.TrimPrefix(msg.Username, "@")) == clean || msg.UserID == clean {
			return msg.UserID, msg.Username, true
		}
	}

	return clean, clean, false
}

// CreateSplitBill parses the split formula, calculates slot price, and creates a BillSession.
// If customParticipants are provided, it splits among them instead of fetching from attendance.
func (m *Manager) CreateSplitBill(expr string, customParticipants ...Participant) (*BillSession, *SplitBreakdown, error) {
	breakdown, err := ParseSplitFormula(expr)
	if err != nil {
		return nil, nil, err
	}

	var matchDay, matchDate string
	var participants []Participant

	if len(customParticipants) > 0 {
		participants = customParticipants
		if m.state.PreviousMatchDate != "" {
			matchDay = m.state.PreviousMatchDay
			matchDate = m.state.PreviousMatchDate
		} else if m.state.MatchDate != "" {
			matchDay = m.state.MatchDay
			matchDate = m.state.MatchDate
		} else {
			now := time.Now().In(m.cfg.Timezone)
			matchDay = DayOfWeekVietnamese(now.Weekday())
			matchDate = now.Format("02/01/2006")
		}
	} else {
		matchDay, matchDate, participants = m.GetParticipantsForMoney()
		if len(participants) == 0 {
			return nil, nil, fmt.Errorf("chưa có danh sách người tham gia buổi đánh hôm trước để chia tiền. Bạn có thể tag trực tiếp người tham gia, ví dụ: `*split %s @user1 @user2`", expr)
		}
	}

	totalSlots := 0
	for _, p := range participants {
		totalSlots += p.Slots
	}
	if totalSlots <= 0 {
		totalSlots = len(participants)
	}

	// Round up slot price to the nearest 1,000 VND
	slotPrice := int64(math.Ceil(float64(breakdown.TotalFee)/float64(totalSlots)/1000.0) * 1000)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.archivePreviousBillLocked()

	billID, billNum, billCode := m.nextBillIDLocked()

	items := make(map[string]*BillItem)
	for _, p := range participants {
		userAmount := int64(p.Slots) * slotPrice
		disp := p.DisplayName
		if disp == "" {
			disp = m.ResolveDisplayName(p.UserID, p.Username)
		}
		items[p.UserID] = &BillItem{
			UserID:      p.UserID,
			Username:    p.Username,
			DisplayName: disp,
			Slots:       p.Slots,
			Amount:      userAmount,
			Paid:        false,
		}
	}

	bill := &BillSession{
		ID:           billID,
		BillNumber:   billNum,
		BillCode:     billCode,
		MatchDay:     matchDay,
		MatchDate:    matchDate,
		CourtFee:     breakdown.CourtFee,
		ShuttleCount: breakdown.ShuttleCount,
		ShuttlePrice: breakdown.ShuttlePrice,
		ShuttleFee:   breakdown.ShuttleFee,
		TotalAmount:  breakdown.TotalFee,
		TotalSlots:   totalSlots,
		SlotPrice:    slotPrice,
		Items:        items,
		CreatedAt:    time.Now().In(m.cfg.Timezone),
	}

	m.state.CurrentBill = bill
	m.saveStateLocked()
	return bill, breakdown, nil
}

// CreateMoneyBill creates a BillSession with a fixed per-slot price.
// If customParticipants are provided, it splits among them instead of fetching from attendance.
func (m *Manager) CreateMoneyBill(unitAmount int, customParticipants ...Participant) (*BillSession, error) {
	if unitAmount <= 0 {
		return nil, fmt.Errorf("số tiền mỗi slot không hợp lệ")
	}

	var matchDay, matchDate string
	var participants []Participant

	if len(customParticipants) > 0 {
		participants = customParticipants
		if m.state.PreviousMatchDate != "" {
			matchDay = m.state.PreviousMatchDay
			matchDate = m.state.PreviousMatchDate
		} else if m.state.MatchDate != "" {
			matchDay = m.state.MatchDay
			matchDate = m.state.MatchDate
		} else {
			now := time.Now().In(m.cfg.Timezone)
			matchDay = DayOfWeekVietnamese(now.Weekday())
			matchDate = now.Format("02/01/2006")
		}
	} else {
		matchDay, matchDate, participants = m.GetParticipantsForMoney()
		if len(participants) == 0 {
			return nil, fmt.Errorf("chưa có danh sách người tham gia buổi đánh hôm trước để chia tiền. Bạn có thể tag trực tiếp người tham gia, ví dụ: `*money %dk @user1 @user2`", unitAmount/1000)
		}
	}

	totalSlots := 0
	for _, p := range participants {
		totalSlots += p.Slots
	}
	if totalSlots <= 0 {
		totalSlots = len(participants)
	}

	slotPrice := int64(unitAmount)
	totalAmount := slotPrice * int64(totalSlots)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.archivePreviousBillLocked()

	billID, billNum, billCode := m.nextBillIDLocked()

	items := make(map[string]*BillItem)
	for _, p := range participants {
		userAmount := int64(p.Slots) * slotPrice
		disp := p.DisplayName
		if disp == "" {
			disp = m.ResolveDisplayName(p.UserID, p.Username)
		}
		items[p.UserID] = &BillItem{
			UserID:      p.UserID,
			Username:    p.Username,
			DisplayName: disp,
			Slots:       p.Slots,
			Amount:      userAmount,
			Paid:        false,
		}
	}

	bill := &BillSession{
		ID:          billID,
		BillNumber:  billNum,
		BillCode:    billCode,
		MatchDay:    matchDay,
		MatchDate:   matchDate,
		TotalAmount: totalAmount,
		TotalSlots:  totalSlots,
		SlotPrice:   slotPrice,
		Items:       items,
		CreatedAt:   time.Now().In(m.cfg.Timezone),
	}

	m.state.CurrentBill = bill
	m.saveStateLocked()
	return bill, nil
}

func (m *Manager) nextBillIDLocked() (id string, billNum int, code string) {
	if m.state.NextBillNumber <= 0 {
		maxNum := 0
		if m.state.CurrentBill != nil && m.state.CurrentBill.BillNumber > maxNum {
			maxNum = m.state.CurrentBill.BillNumber
		}
		for _, b := range m.state.BillHistory {
			if b.BillNumber > maxNum {
				maxNum = b.BillNumber
			}
		}
		if maxNum == 0 {
			maxNum = len(m.state.BillHistory)
			if m.state.CurrentBill != nil {
				maxNum++
			}
		}
		m.state.NextBillNumber = maxNum + 1
	}

	billNum = m.state.NextBillNumber
	m.state.NextBillNumber++
	id = strconv.Itoa(billNum)
	code = fmt.Sprintf("CL%d", billNum)
	return id, billNum, code
}

func (m *Manager) archivePreviousBillLocked() {
	if m.state.CurrentBill != nil && len(m.state.CurrentBill.Items) > 0 {
		already := false
		for i, old := range m.state.BillHistory {
			if old.ID == m.state.CurrentBill.ID ||
				(old.BillNumber > 0 && old.BillNumber == m.state.CurrentBill.BillNumber) ||
				(old.MessageID != "" && old.MessageID == m.state.CurrentBill.MessageID) {
				m.state.BillHistory[i] = *m.state.CurrentBill
				already = true
				break
			}
		}
		if !already {
			m.state.BillHistory = append(m.state.BillHistory, *m.state.CurrentBill)
		}
	}
}

func (m *Manager) findBillLocked(filter string) *BillSession {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return m.state.CurrentBill
	}

	cleanFilter := strings.TrimPrefix(filter, "#")
	cleanFilter = strings.TrimSpace(cleanFilter)
	cleanUpper := strings.ToUpper(cleanFilter)

	// Check if filter is explicitly numeric ID or CL-prefixed ID
	var filterNum int
	isIDFilter := false
	if num, err := strconv.Atoi(cleanFilter); err == nil && num > 0 && len(cleanFilter) < 4 {
		filterNum = num
		isIDFilter = true
	} else if strings.HasPrefix(cleanUpper, "CL") {
		numPart := strings.TrimPrefix(cleanUpper, "CL")
		if num, err := strconv.Atoi(numPart); err == nil && num > 0 {
			filterNum = num
			isIDFilter = true
		}
	}

	matches := func(b *BillSession) bool {
		if b == nil {
			return false
		}
		if isIDFilter {
			return b.BillNumber == filterNum || b.ID == cleanFilter || b.BillCode == cleanUpper
		}
		// Match exact ID (e.g. "1")
		if b.ID == filter || b.ID == cleanFilter {
			return true
		}
		// Match BillCode (e.g. "CL1", "cl1")
		if b.BillCode != "" && (b.BillCode == cleanUpper || strings.EqualFold(b.BillCode, filter)) {
			return true
		}
		// Match date ONLY if filter looks like a date (has '/' or '-' or is 4+ digits) or day name
		if strings.Contains(filter, "/") || strings.Contains(filter, "-") || len(cleanFilter) >= 4 {
			cleanDateFilter := strings.ReplaceAll(filter, "/", "")
			cleanDateFilter = strings.ReplaceAll(cleanDateFilter, "-", "")
			cleanDateFilter = strings.ToLower(cleanDateFilter)
			bDate := strings.ReplaceAll(b.MatchDate, "/", "")
			if cleanDateFilter != "" && strings.Contains(bDate, cleanDateFilter) {
				return true
			}
			if cleanDateFilter != "" && strings.Contains(strings.ToLower(b.MatchDay), cleanDateFilter) {
				return true
			}
		}
		return false
	}

	if matches(m.state.CurrentBill) {
		return m.state.CurrentBill
	}

	for i := len(m.state.BillHistory) - 1; i >= 0; i-- {
		b := &m.state.BillHistory[i]
		if matches(b) {
			return b
		}
	}

	return nil
}

// SetCurrentBillMessageID associates the sent Mezon message ID with the active bill.
func (m *Manager) SetCurrentBillMessageID(msgID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.CurrentBill != nil {
		m.state.CurrentBill.MessageID = msgID
		m.saveStateLocked()
	}
}

// IsCurrentBillMessage returns true if msgID matches any active or historical bill's message ID.
func (m *Manager) IsCurrentBillMessage(msgID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.CurrentBill != nil && m.state.CurrentBill.MessageID == msgID {
		return true
	}
	for _, b := range m.state.BillHistory {
		if b.MessageID == msgID {
			return true
		}
	}
	return false
}

// MarkPaidByUserIDOrNameOnBill marks a user as paid on a specific bill (identified by msgID) or current bill.
func (m *Manager) MarkPaidByUserIDOrNameOnBill(msgID, userID, username string) (*BillItem, *BillSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var targetBill *BillSession
	if msgID != "" {
		if m.state.CurrentBill != nil && m.state.CurrentBill.MessageID == msgID {
			targetBill = m.state.CurrentBill
		} else {
			for i := range m.state.BillHistory {
				if m.state.BillHistory[i].MessageID == msgID {
					targetBill = &m.state.BillHistory[i]
					break
				}
			}
		}
	}
	if targetBill == nil {
		targetBill = m.state.CurrentBill
	}
	if targetBill == nil || len(targetBill.Items) == 0 {
		return nil, nil, false
	}

	now := time.Now().In(m.cfg.Timezone)
	if item, ok := targetBill.Items[userID]; ok {
		item.Paid = true
		item.PaidAt = &now
		m.saveStateLocked()
		return item, targetBill, true
	}

	target := strings.ToLower(strings.TrimPrefix(username, "@"))
	for _, item := range targetBill.Items {
		uLower := strings.ToLower(strings.TrimPrefix(item.Username, "@"))
		dLower := strings.ToLower(strings.TrimPrefix(item.DisplayName, "@"))
		if uLower == target || dLower == target || item.UserID == target {
			item.Paid = true
			item.PaidAt = &now
			m.saveStateLocked()
			return item, targetBill, true
		}
	}

	return nil, nil, false
}

// MarkPaidByUserIDOrName marks a user as paid by their user ID or username on the active bill.
func (m *Manager) MarkPaidByUserIDOrName(userID, username string) (*BillItem, bool) {
	item, _, ok := m.MarkPaidByUserIDOrNameOnBill("", userID, username)
	return item, ok
}

// MarkPaidSelf marks the sender as paid on the specified bill (or current active bill).
// If the sender is found and was unpaid, it marks them as paid and returns (item, false, ...).
// If the sender is found but was already paid, it returns (item, true, ...).
// If the sender is not on the bill, it returns (nil, false, ...).
func (m *Manager) MarkPaidSelf(userID, username, clanNick, displayName string, filter ...string) (item *BillItem, alreadyPaid bool, totalPaid int64, totalCost int64, paidCount int, totalCount int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f := ""
	if len(filter) > 0 {
		f = filter[0]
	}
	targetBill := m.findBillLocked(f)
	if targetBill == nil || len(targetBill.Items) == 0 {
		return nil, false, 0, 0, 0, 0, fmt.Errorf("chưa có hoá đơn nào đang mở. Dùng `*split` hoặc `*money` để tạo hoá đơn")
	}

	// 1. Try matching by UserID if available
	var matched *BillItem
	if userID != "" {
		if it, ok := targetBill.Items[userID]; ok {
			matched = it
		}
	}

	// 2. If not matched yet, try candidate names (username, clanNick, displayName, resolved name)
	if matched == nil {
		candidates := []string{username, clanNick, displayName}
		if userID != "" {
			candidates = append(candidates, m.ResolveDisplayName(userID, username))
		}
		for _, cand := range candidates {
			clean := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cand), "@"))
			if clean == "" {
				continue
			}
			for _, it := range targetBill.Items {
				uLower := strings.ToLower(strings.TrimPrefix(it.Username, "@"))
				dLower := strings.ToLower(strings.TrimPrefix(it.DisplayName, "@"))
				if uLower == clean || dLower == clean || it.UserID == clean {
					matched = it
					break
				}
			}
			if matched != nil {
				break
			}
		}
	}

	for _, it := range targetBill.Items {
		totalCost += it.Amount
		if it.Paid {
			totalPaid += it.Amount
			paidCount++
		}
	}
	totalCount = len(targetBill.Items)

	if matched == nil {
		return nil, false, totalPaid, totalCost, paidCount, totalCount, nil
	}

	if matched.Paid {
		return matched, true, totalPaid, totalCost, paidCount, totalCount, nil
	}

	now := time.Now().In(m.cfg.Timezone)
	matched.Paid = true
	matched.PaidAt = &now
	totalPaid += matched.Amount
	paidCount++

	m.saveStateLocked()
	return matched, false, totalPaid, totalCost, paidCount, totalCount, nil
}

// MarkPaidBatch marks one or more members as paid on the active bill or a specific date bill.
func (m *Manager) MarkPaidBatch(names []string, filter ...string) (paidItems []*BillItem, notFound []string, totalPaid int64, totalCost int64, paidCount int, totalCount int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f := ""
	if len(filter) > 0 {
		f = filter[0]
	}
	targetBill := m.findBillLocked(f)
	if targetBill == nil || len(targetBill.Items) == 0 {
		return nil, nil, 0, 0, 0, 0, fmt.Errorf("chưa có hoá đơn nào đang mở. Dùng `*split` hoặc `*money` để tạo hoá đơn")
	}

	now := time.Now().In(m.cfg.Timezone)
	for _, raw := range names {
		clean := strings.ToLower(strings.TrimPrefix(raw, "@"))
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}

		var matched *BillItem
		for _, item := range targetBill.Items {
			uLower := strings.ToLower(strings.TrimPrefix(item.Username, "@"))
			dLower := strings.ToLower(strings.TrimPrefix(item.DisplayName, "@"))
			if uLower == clean || dLower == clean || item.UserID == clean {
				matched = item
				break
			}
		}

		if matched != nil {
			matched.Paid = true
			matched.PaidAt = &now
			paidItems = append(paidItems, matched)
		} else {
			notFound = append(notFound, raw)
		}
	}

	for _, item := range targetBill.Items {
		totalCost += item.Amount
		totalCount++
		if item.Paid {
			totalPaid += item.Amount
			paidCount++
		}
	}

	m.saveStateLocked()
	return paidItems, notFound, totalPaid, totalCost, paidCount, totalCount, nil
}

// MarkUnpaidOnBill marks one or more members as unpaid on a specific bill (identified by msgID) or active bill.
func (m *Manager) MarkUnpaidOnBill(msgID string, names []string) (unmarked []*BillItem, notFound []string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var targetBill *BillSession
	if msgID != "" {
		if m.state.CurrentBill != nil && m.state.CurrentBill.MessageID == msgID {
			targetBill = m.state.CurrentBill
		} else {
			for i := range m.state.BillHistory {
				if m.state.BillHistory[i].MessageID == msgID {
					targetBill = &m.state.BillHistory[i]
					break
				}
			}
		}
	}
	if targetBill == nil {
		targetBill = m.state.CurrentBill
	}
	if targetBill == nil || len(targetBill.Items) == 0 {
		return nil, nil, fmt.Errorf("chưa có hoá đơn nào đang mở")
	}

	for _, raw := range names {
		clean := strings.ToLower(strings.TrimPrefix(raw, "@"))
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}

		var matched *BillItem
		for _, item := range targetBill.Items {
			uLower := strings.ToLower(strings.TrimPrefix(item.Username, "@"))
			dLower := strings.ToLower(strings.TrimPrefix(item.DisplayName, "@"))
			if uLower == clean || dLower == clean || item.UserID == clean {
				matched = item
				break
			}
		}

		if matched != nil {
			matched.Paid = false
			matched.PaidAt = nil
			unmarked = append(unmarked, matched)
		} else {
			notFound = append(notFound, raw)
		}
	}

	m.saveStateLocked()
	return unmarked, notFound, nil
}

// MarkUnpaidBatch marks one or more members as unpaid on the active bill or filtered date bill.
func (m *Manager) MarkUnpaidBatch(names []string, filter ...string) (unmarked []*BillItem, notFound []string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f := ""
	if len(filter) > 0 {
		f = filter[0]
	}
	targetBill := m.findBillLocked(f)
	if targetBill == nil || len(targetBill.Items) == 0 {
		if f != "" {
			return nil, nil, fmt.Errorf("không tìm thấy hoá đơn cho ngày: %s", f)
		}
		return nil, nil, fmt.Errorf("chưa có hoá đơn nào đang mở. Dùng `*split` hoặc `*money` để tạo hoá đơn")
	}

	for _, raw := range names {
		clean := strings.ToLower(strings.TrimPrefix(raw, "@"))
		clean = strings.TrimSpace(clean)
		if clean == "" {
			continue
		}

		var matched *BillItem
		for _, item := range targetBill.Items {
			uLower := strings.ToLower(strings.TrimPrefix(item.Username, "@"))
			dLower := strings.ToLower(strings.TrimPrefix(item.DisplayName, "@"))
			if uLower == clean || dLower == clean || item.UserID == clean {
				matched = item
				break
			}
		}

		if matched != nil {
			matched.Paid = false
			matched.PaidAt = nil
			unmarked = append(unmarked, matched)
		} else {
			notFound = append(notFound, raw)
		}
	}

	m.saveStateLocked()
	return unmarked, notFound, nil
}

// GetBillStatus returns the bill breakdown, paid list, and unpaid list (optionally filtered by date).
func (m *Manager) GetBillStatus(filter ...string) (bill *BillSession, paid []*BillItem, unpaid []*BillItem, totalPaid int64, totalCost int64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f := ""
	if len(filter) > 0 {
		f = filter[0]
	}
	bill = m.findBillLocked(f)
	if bill == nil || len(bill.Items) == 0 {
		if f != "" {
			return nil, nil, nil, 0, 0, fmt.Errorf("không tìm thấy hoá đơn cho ngày: %s", f)
		}
		return nil, nil, nil, 0, 0, fmt.Errorf("chưa có hoá đơn nào đang mở. Dùng `*split` hoặc `*money` để tạo hoá đơn")
	}

	for _, item := range bill.Items {
		totalCost += item.Amount
		if item.Paid {
			totalPaid += item.Amount
			paid = append(paid, item)
		} else {
			unpaid = append(unpaid, item)
		}
	}

	sort.Slice(paid, func(i, j int) bool {
		return paid[i].Username < paid[j].Username
	})
	sort.Slice(unpaid, func(i, j int) bool {
		return unpaid[i].Username < unpaid[j].Username
	})

	return bill, paid, unpaid, totalPaid, totalCost, nil
}

// GetUnpaidList returns participants who have not paid yet (optionally filtered by date).
func (m *Manager) GetUnpaidList(filter ...string) (bill *BillSession, unpaid []*BillItem, totalUnpaid int64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f := ""
	if len(filter) > 0 {
		f = filter[0]
	}
	bill = m.findBillLocked(f)
	if bill == nil || len(bill.Items) == 0 {
		if f != "" {
			return nil, nil, 0, fmt.Errorf("không tìm thấy hoá đơn cho ngày: %s", f)
		}
		return nil, nil, 0, fmt.Errorf("chưa có hoá đơn nào đang mở. Dùng `*split` hoặc `*money` để tạo hoá đơn")
	}

	for _, item := range bill.Items {
		if !item.Paid {
			totalUnpaid += item.Amount
			unpaid = append(unpaid, item)
		}
	}

	sort.Slice(unpaid, func(i, j int) bool {
		return unpaid[i].Username < unpaid[j].Username
	})

	return bill, unpaid, totalUnpaid, nil
}

// ArchiveCurrentMatch archives current session attendance into MatchHistory if not already present.
func (m *Manager) ArchiveCurrentMatch(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var prev []Participant
	for _, p := range m.state.GoingParticipants {
		prev = append(prev, p)
	}
	if len(prev) == 0 {
		for uid, name := range m.state.ReactionsGoing {
			prev = append(prev, Participant{UserID: uid, Username: name, Slots: 1})
		}
	}
	if m.state.MatchDate != "" && (len(prev) > 0 || len(m.state.ReactionsNotGoing) > 0) {
		var absentees []string
		for _, uname := range m.state.ReactionsNotGoing {
			absentees = append(absentees, uname)
		}
		sort.Strings(absentees)

		for _, h := range m.state.MatchHistory {
			if h.MatchDate == m.state.MatchDate {
				return
			}
		}

		m.state.MatchHistory = append(m.state.MatchHistory, MatchRecord{
			SessionID:   m.state.SessionID,
			MatchDay:    m.state.MatchDay,
			MatchDate:   m.state.MatchDate,
			Attendees:   prev,
			Absentees:   absentees,
			CompletedAt: now,
		})
		m.saveStateLocked()
	}
}

func parseTargetMonthYear(raw string, now time.Time) (month int, year int, label string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return int(now.Month()), now.Year(), fmt.Sprintf("%02d/%04d", now.Month(), now.Year()), nil
	}
	if strings.Contains(raw, "/") {
		parts := strings.Split(raw, "/")
		if len(parts) == 2 {
			m, err1 := strconv.Atoi(parts[0])
			y, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil && m >= 1 && m <= 12 {
				if y < 100 {
					y += 2000
				}
				return m, y, fmt.Sprintf("%02d/%04d", m, y), nil
			}
		}
	} else if strings.Contains(raw, "-") {
		parts := strings.Split(raw, "-")
		if len(parts) == 2 {
			if len(parts[0]) == 4 {
				y, err1 := strconv.Atoi(parts[0])
				m, err2 := strconv.Atoi(parts[1])
				if err1 == nil && err2 == nil && m >= 1 && m <= 12 {
					return m, y, fmt.Sprintf("%02d/%04d", m, y), nil
				}
			} else {
				m, err1 := strconv.Atoi(parts[0])
				y, err2 := strconv.Atoi(parts[1])
				if err1 == nil && err2 == nil && m >= 1 && m <= 12 {
					return m, y, fmt.Sprintf("%02d/%04d", m, y), nil
				}
			}
		}
	} else {
		m, err := strconv.Atoi(raw)
		if err == nil && m >= 1 && m <= 12 {
			return m, now.Year(), fmt.Sprintf("%02d/%04d", m, now.Year()), nil
		}
	}
	return 0, 0, "", fmt.Errorf("định dạng tháng không hợp lệ (ví dụ: `*bxh` hoặc `*bxh 11/2026`)")
}

// GetLeaderboardText generates the formatted leaderboard for the specified month/year.
func (m *Manager) GetLeaderboardText(monthInput string, now time.Time) (string, error) {
	month, year, label, err := parseTargetMonthYear(monthInput, now)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	records := append([]MatchRecord(nil), m.state.MatchHistory...)
	var currentAttendees []Participant
	for _, p := range m.state.GoingParticipants {
		currentAttendees = append(currentAttendees, p)
	}
	if len(currentAttendees) == 0 {
		for uid, name := range m.state.ReactionsGoing {
			currentAttendees = append(currentAttendees, Participant{UserID: uid, Username: name, Slots: 1})
		}
	}
	currentDate := m.state.MatchDate
	currentDay := m.state.MatchDay
	var currentAbsentees []string
	for _, u := range m.state.ReactionsNotGoing {
		currentAbsentees = append(currentAbsentees, u)
	}
	m.mu.Unlock()

	// If current session date matches target month and is not in records, include it
	if currentDate != "" && (len(currentAttendees) > 0 || len(currentAbsentees) > 0) {
		already := false
		for _, r := range records {
			if r.MatchDate == currentDate {
				already = true
				break
			}
		}
		if !already {
			records = append(records, MatchRecord{
				SessionID:   "current",
				MatchDay:    currentDay,
				MatchDate:   currentDate,
				Attendees:   currentAttendees,
				Absentees:   currentAbsentees,
				CompletedAt: now,
			})
		}
	}

	var monthMatches []MatchRecord
	for _, r := range records {
		parts := strings.Split(r.MatchDate, "/")
		if len(parts) == 3 {
			mVal, _ := strconv.Atoi(parts[1])
			yVal, _ := strconv.Atoi(parts[2])
			if mVal == month && yVal == year {
				monthMatches = append(monthMatches, r)
			}
		}
	}

	if len(monthMatches) == 0 {
		return fmt.Sprintf("⚠️ Chưa có dữ liệu buổi đánh cầu nào trong tháng **%s**!", label), nil
	}

	type statsHolder struct {
		username    string
		matches     int
		slots       int
		absentCount int
	}

	statsMap := make(map[string]*statsHolder)
	getStats := func(uname string) *statsHolder {
		disp := m.ResolveDisplayName("", uname)
		key := strings.ToLower(disp)
		if s, ok := statsMap[key]; ok {
			return s
		}
		s := &statsHolder{username: disp}
		statsMap[key] = s
		return s
	}

	totalMatches := len(monthMatches)
	for _, match := range monthMatches {
		for _, att := range match.Attendees {
			s := getStats(att.Username)
			s.matches++
			s.slots += att.Slots
		}
		for _, abs := range match.Absentees {
			s := getStats(abs)
			s.absentCount++
		}
	}

	var goingList []*statsHolder
	var absentList []*statsHolder
	for _, s := range statsMap {
		if s.matches > 0 {
			goingList = append(goingList, s)
		}
		if s.absentCount > 0 {
			absentList = append(absentList, s)
		}
	}

	sort.Slice(goingList, func(i, j int) bool {
		if goingList[i].matches != goingList[j].matches {
			return goingList[i].matches > goingList[j].matches
		}
		if goingList[i].slots != goingList[j].slots {
			return goingList[i].slots > goingList[j].slots
		}
		return goingList[i].username < goingList[j].username
	})

	sort.Slice(absentList, func(i, j int) bool {
		if absentList[i].absentCount != absentList[j].absentCount {
			return absentList[i].absentCount > absentList[j].absentCount
		}
		return absentList[i].username < absentList[j].username
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🏆 **[BẢNG XẾP HẠNG CẦU LÔNG - THÁNG %s]** 🏸\n\n", label))
	sb.WriteString(fmt.Sprintf("📅 Tổng số buổi đã tổ chức: **%d buổi**\n\n", totalMatches))

	// Section 1: Top Chăm chỉ
	sb.WriteString("🌟 **BẢNG VÀNG CHĂM CHỈ (CHIẾN THẦN QUẬT CẦU):**\n")
	if len(goingList) == 0 {
		sb.WriteString("• Chưa có ai tham gia buổi nào trong tháng.\n")
	} else {
		for idx, s := range goingList {
			rate := (float64(s.matches) / float64(totalMatches)) * 100.0
			slotInfo := fmt.Sprintf("%d slot", s.slots)
			rateInfo := fmt.Sprintf("%.0f%%", rate)

			switch idx {
			case 0:
				sb.WriteString(fmt.Sprintf("1. 👑 **@%s**: %d/%d buổi (%s • %s) - *👑 Chiến thần quật cầu*\n",
					s.username, s.matches, totalMatches, rateInfo, slotInfo))
			case 1:
				sb.WriteString(fmt.Sprintf("2. 🥈 **@%s**: %d/%d buổi (%s • %s)\n",
					s.username, s.matches, totalMatches, rateInfo, slotInfo))
			case 2:
				sb.WriteString(fmt.Sprintf("3. 🥉 **@%s**: %d/%d buổi (%s • %s)\n",
					s.username, s.matches, totalMatches, rateInfo, slotInfo))
			default:
				if idx < 10 {
					sb.WriteString(fmt.Sprintf("• **@%s**: %d/%d buổi (%s • %s)\n",
						s.username, s.matches, totalMatches, rateInfo, slotInfo))
				}
			}
		}
	}

	sb.WriteString("\n")

	// Section 2: Top Chúa sủi
	sb.WriteString("🍗 **BẢNG PHONG THẦN \"CHÚA SỦI\" CỦA THÁNG:**\n")
	if len(absentList) == 0 {
		sb.WriteString("🎉 *Tháng này không ai xin off buổi nào! Tinh thần thể thao của anh em quá tuyệt vời!* 🔥\n")
	} else {
		for idx, s := range absentList {
			switch idx {
			case 0:
				sb.WriteString(fmt.Sprintf("1. 🍗 **@%s**: **%d lần** báo bận/off - *🍗 Chúa sủi của tháng*\n",
					s.username, s.absentCount))
			case 1:
				sb.WriteString(fmt.Sprintf("2. 🦥 **@%s**: **%d lần** báo bận/off\n",
					s.username, s.absentCount))
			default:
				if idx < 5 {
					sb.WriteString(fmt.Sprintf("• **@%s**: %d lần báo bận/off\n",
						s.username, s.absentCount))
				}
			}
		}
	}

	sb.WriteString("\n💪 *Chúc toàn thể anh em Vinh-CL luôn giữ vững phong độ và đam mê quật cầu thật cháy nhé!* 🔥🏸")
	return sb.String(), nil
}
