package sport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
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
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Participant represents a confirmed match participant with their slots count.
type Participant struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Slots    int    `json:"slots"`
}

// AttendanceState persists the current check-in session and recent messages.
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
}

// SummaryResult contains the parsed attendance details.
type SummaryResult struct {
	Going     []string
	NotGoing  []string
	CourtInfo string
	Notes     []string
	RawAI     string
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
	cfg    Config
	sender MessageSender
	log    *slog.Logger

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

	m := &Manager{
		cfg:    cfg,
		sender: sender,
		log:    logger,
	}
	m.loadState()
	return m
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
				"participants", len(s.GoingParticipants))
			return
		}
	}
	m.state = AttendanceState{
		Messages:          make([]MessageRecord, 0),
		ReactionsGoing:    make(map[string]string),
		ReactionsNotGoing: make(map[string]string),
		GoingParticipants: make(map[string]Participant),
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

	name := username
	if name == "" {
		name = userID
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
			m.state.ReactionsGoing[userID] = name
			m.state.GoingParticipants[userID] = Participant{
				UserID:   userID,
				Username: name,
				Slots:    slots,
			}
			delete(m.state.ReactionsNotGoing, userID)
		} else if isNotGoing {
			m.state.ReactionsNotGoing[userID] = name
			delete(m.state.ReactionsGoing, userID)
			delete(m.state.GoingParticipants, userID)
		}
	}

	m.saveStateLocked()
	m.log.Info("recorded sport reaction", "user", name, "emoji", emoji, "is_remove", isRemove, "slots", slots,
		"going_count", len(m.state.ReactionsGoing), "not_going_count", len(m.state.ReactionsNotGoing))
}

// RecordMessage stores incoming channel messages for the current attendance session.
func (m *Manager) RecordMessage(channelID, msgID, userID, username, content string) {
	if channelID != m.cfg.ChannelID {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	rec := MessageRecord{
		ID:        msgID,
		UserID:    userID,
		Username:  username,
		Content:   content,
		Timestamp: time.Now(),
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

	if summary.RawAI != "" {
		sb.WriteString(summary.RawAI)
	} else {
		// Format from rule-based parser
		if len(summary.Going) > 0 {
			sb.WriteString(fmt.Sprintf("• ✅ **Tham gia (%d người):** %s\n", len(summary.Going), strings.Join(summary.Going, ", ")))
		} else {
			sb.WriteString("• ✅ **Tham gia:** Chưa có ai xác nhận trên nhóm\n")
		}

		if len(summary.NotGoing) > 0 {
			sb.WriteString(fmt.Sprintf("• ❌ **Vắng mặt (%d người):** %s\n", len(summary.NotGoing), strings.Join(summary.NotGoing, ", ")))
		}

		if summary.CourtInfo != "" {
			sb.WriteString(fmt.Sprintf("• 🏟️ **Sân bãi:** %s\n", summary.CourtInfo))
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
	}

	sb.WriteString("\nChúc anh em một buổi giao lưu quật cầu thật cháy và nhiều tiếng cười! 🔥💪")

	_, err := m.sendWithRoleMention(ctx, sb.String())
	return err
}

// SummarizeMessages produces a summary using OpenAI if available, then Gemini, or the NLP rule-based parser merged with reactions.
func (m *Manager) SummarizeMessages(ctx context.Context, messages []MessageRecord) SummaryResult {
	m.mu.Lock()
	var goingReactions, notGoingReactions []string
	for _, uname := range m.state.ReactionsGoing {
		goingReactions = append(goingReactions, uname)
	}
	for _, uname := range m.state.ReactionsNotGoing {
		notGoingReactions = append(notGoingReactions, uname)
	}
	m.mu.Unlock()

	sort.Strings(goingReactions)
	sort.Strings(notGoingReactions)

	if len(messages) == 0 && len(goingReactions) == 0 && len(notGoingReactions) == 0 {
		return SummaryResult{
			CourtInfo: "Chưa có thông tin sân",
		}
	}

	if m.cfg.OpenAIKey != "" {
		aiText, err := m.callOpenAISummary(ctx, messages, goingReactions, notGoingReactions)
		if err == nil && strings.TrimSpace(aiText) != "" {
			return SummaryResult{
				RawAI: aiText,
			}
		}
		if err != nil {
			m.log.Warn("failed to call openai summary, falling back", "err", err)
		}
	}

	if m.cfg.GeminiKey != "" {
		aiText, err := m.callGeminiSummary(ctx, messages, goingReactions, notGoingReactions)
		if err == nil && strings.TrimSpace(aiText) != "" {
			return SummaryResult{
				RawAI: aiText,
			}
		}
		if err != nil {
			m.log.Warn("failed to call gemini summary, falling back", "err", err)
		}
	}

	// Fallback to Rule-based NLP Parser merged with reactions
	nlpRes := ParseMessagesNLP(messages)

	goingMap := make(map[string]bool)
	notGoingMap := make(map[string]bool)

	for _, u := range goingReactions {
		goingMap[u] = true
	}
	for _, u := range notGoingReactions {
		notGoingMap[u] = true
	}

	for _, u := range nlpRes.Going {
		delete(notGoingMap, u)
		goingMap[u] = true
	}
	for _, u := range nlpRes.NotGoing {
		delete(goingMap, u)
		notGoingMap[u] = true
	}

	var going []string
	for u := range goingMap {
		going = append(going, u)
	}
	sort.Strings(going)

	var notGoing []string
	for u := range notGoingMap {
		notGoing = append(notGoing, u)
	}
	sort.Strings(notGoing)

	return SummaryResult{
		Going:     going,
		NotGoing:  notGoing,
		CourtInfo: nlpRes.CourtInfo,
		Notes:     nlpRes.Notes,
	}
}

// ParseMessagesNLP extracts attendance, absences, and court info using Vietnamese badminton patterns.
func ParseMessagesNLP(messages []MessageRecord) SummaryResult {
	goingMap := make(map[string]bool)
	notGoingMap := make(map[string]bool)
	var courtMentions []string
	var notes []string

	reGoing := regexp.MustCompile(`(?i)(\+1|1 slot|di nha|đi nha|di luon|đi luôn|co mat|có mặt|em di|em đi|minh di|mình đi|chieu danh|chiều đánh|ra san|ra sân|quật|chien|chiến|giao lưu|tham gia)`)
	reNotGoing := regexp.MustCompile(`(?i)(xin off|off|bận|ban|nghi nha|nghỉ nha|ko di|không đi|k đi|ko dc|không được|k dc|k tham gia|huy|hủy|chịu nha)`)
	reCourt := regexp.MustCompile(`(?i)(sân\s+\d+|san\s+\d+|sân\s+[a-zA-Z0-9\s]+|Minh Hải|minh hai|đặt sân|chưa có sân|k có sân|không có sân|full sân|hết sân)`)

	for _, msg := range messages {
		name := msg.Username
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

func (m *Manager) buildTranscript(messages []MessageRecord, goingReactions, notGoingReactions []string) string {
	var sb strings.Builder

	if len(goingReactions) > 0 || len(notGoingReactions) > 0 {
		sb.WriteString("Danh sách điểm danh qua reaction tin nhắn:\n")
		if len(goingReactions) > 0 {
			sb.WriteString(fmt.Sprintf("- Đã react :cong1: (tham gia - %d người): %s\n", len(goingReactions), strings.Join(goingReactions, ", ")))
		}
		if len(notGoingReactions) > 0 {
			sb.WriteString(fmt.Sprintf("- Đã react :tru1: (vắng mặt/bận - %d người): %s\n", len(notGoingReactions), strings.Join(notGoingReactions, ", ")))
		}
		sb.WriteString("\n")
	}

	if len(messages) > 0 {
		sb.WriteString("Tin nhắn trò chuyện trong nhóm chat:\n")
		for _, msg := range messages {
			name := msg.Username
			if name == "" {
				name = msg.UserID
			}
			sb.WriteString(fmt.Sprintf("- %s: %s\n", name, msg.Content))
		}
	}

	return sb.String()
}

func (m *Manager) callOpenAISummary(ctx context.Context, messages []MessageRecord, goingReactions, notGoingReactions []string) (string, error) {
	transcript := m.buildTranscript(messages, goingReactions, notGoingReactions)

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
				"content": "Bạn là trợ lý cho câu lạc bộ cầu lông Vinh-CL. Dựa vào danh sách reaction và tin nhắn trong nhóm chat, hãy tóm tắt ngắn gọn và rõ ràng tình hình đánh cầu bằng tiếng Việt theo các gạch đầu dòng sau:\n" +
					"• ✅ **Tham gia (số lượng):** [Liệt kê tên những người xác nhận đi qua reaction :cong1: hoặc chat]\n" +
					"• ❌ **Vắng mặt:** [Liệt kê tên những người xin nghỉ/bận qua reaction :tru1: hoặc chat, nếu có]\n" +
					"• 🏟️ **Sân bãi:** [Thông tin sân đã đặt nào hoặc chưa/k có sân, giờ chơi nếu có]\n" +
					"Chỉ trả về nội dung gạch đầu dòng ngắn gọn, súc tích, không thêm lời chào hay giải thích rườm rà.",
			},
			{
				"role":    "user",
				"content": fmt.Sprintf("Dưới đây là thông tin điểm danh và tin nhắn nhóm:\n\n%s", transcript),
			},
		},
		"temperature": 0.3,
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

func (m *Manager) callGeminiSummary(ctx context.Context, messages []MessageRecord, goingReactions, notGoingReactions []string) (string, error) {
	transcript := m.buildTranscript(messages, goingReactions, notGoingReactions)

	prompt := fmt.Sprintf(
		"Dưới đây là thông tin điểm danh và tin nhắn trong nhóm chat câu lạc bộ cầu lông Vinh-CL:\n\n%s\n\n"+
			"Hãy tóm tắt ngắn gọn và rõ ràng tình hình đánh cầu bằng tiếng Việt theo các gạch đầu dòng sau:\n"+
			"• ✅ **Tham gia (số lượng):** [Liệt kê tên những người xác nhận đi qua reaction :cong1: hoặc chat]\n"+
			"• ❌ **Vắng mặt:** [Liệt kê tên những người xin nghỉ/bận qua reaction :tru1: hoặc chat, nếu có]\n"+
			"• 🏟️ **Sân bãi:** [Thông tin sân đã đặt nào hoặc chưa/k có sân]\n"+
			"Chỉ trả về nội dung gạch đầu dòng ngắn gọn, không rườm rà.",
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
	if summary.RawAI != "" {
		return summary.RawAI
	}

	var sb strings.Builder
	sb.WriteString("📋 **TÌNH HÌNH ĐIỂM DANH HIỆN TẠI:**\n")
	if len(summary.Going) > 0 {
		sb.WriteString(fmt.Sprintf("• ✅ **Tham gia (%d người):** %s\n", len(summary.Going), strings.Join(summary.Going, ", ")))
	} else {
		sb.WriteString("• ✅ **Tham gia:** Chưa có ai điểm danh\n")
	}

	if len(summary.NotGoing) > 0 {
		sb.WriteString(fmt.Sprintf("• ❌ **Vắng mặt (%d người):** %s\n", len(summary.NotGoing), strings.Join(summary.NotGoing, ", ")))
	}

	if summary.CourtInfo != "" {
		sb.WriteString(fmt.Sprintf("• 🏟️ **Sân:** %s\n", summary.CourtInfo))
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
