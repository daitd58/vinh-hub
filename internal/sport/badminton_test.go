package sport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mezon "github.com/quangledang23/mezon-sdk-go"
)

type mockSender struct {
	lastChannel string
	lastText    string
	lastOptions *mezon.SendOptions
	sendErr     error
	invocations int
	reactions   []string
}

func (m *mockSender) SendToChannel(ctx context.Context, channelID string, text string, opts *mezon.SendOptions) (string, error) {
	m.lastChannel = channelID
	m.lastText = text
	m.lastOptions = opts
	m.invocations++
	return "test-msg-123", m.sendErr
}

func (m *mockSender) ReactToMessage(ctx context.Context, channelID, messageID, emoji string) error {
	m.reactions = append(m.reactions, emoji)
	return nil
}

func TestNextMatchInfo(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)

	// Monday Nov 16, 2026 -> Tuesday Nov 17, 2026 (19:00 - 19:30)
	mon := time.Date(2026, 11, 16, 9, 0, 0, 0, loc)
	day, date, matchTime := NextMatchInfo(mon)
	if day != "Thứ Ba" || date != "17/11/2026" || matchTime != "19:00 - 19:30" {
		t.Errorf("expected Thu Ba 17/11/2026 19:00 - 19:30, got %s %s %s", day, date, matchTime)
	}

	// Wednesday Nov 18, 2026 -> Thursday Nov 19, 2026 (19:00 - 19:30)
	wed := time.Date(2026, 11, 18, 9, 0, 0, 0, loc)
	day, date, matchTime = NextMatchInfo(wed)
	if day != "Thứ Năm" || date != "19/11/2026" || matchTime != "19:00 - 19:30" {
		t.Errorf("expected Thu Nam 19/11/2026 19:00 - 19:30, got %s %s %s", day, date, matchTime)
	}

	// Friday Nov 20, 2026 -> Saturday Nov 21, 2026 at 14:00
	fri := time.Date(2026, 11, 20, 9, 0, 0, 0, loc)
	day, date, matchTime = NextMatchInfo(fri)
	if day != "Thứ Bảy" || date != "21/11/2026" || matchTime != "14:00" {
		t.Errorf("expected Thu Bay 21/11/2026 14:00, got %s %s %s", day, date, matchTime)
	}
}

func TestExtractMatchTime(t *testing.T) {
	// Case 1: 19:30 detected on Tuesday
	msgs := []MessageRecord{
		{Username: "user1", Content: "+1"},
		{Username: "user2", Content: "đặt sân rồi nha, 19:30 sân Minh Hải"},
	}
	h, m, found, str := ExtractMatchTime(msgs, false)
	if !found || h != 19 || m != 30 || str != "19:30" {
		t.Errorf("expected 19:30, got %d:%d (%s), found=%v", h, m, str, found)
	}

	// Case 2: 19h detected
	msgs2 := []MessageRecord{
		{Username: "user1", Content: "chốt 19h sân 1 sân 2 nha"},
	}
	h, m, found, str = ExtractMatchTime(msgs2, false)
	if !found || h != 19 || m != 0 || str != "19:00" {
		t.Errorf("expected 19:00, got %d:%d (%s), found=%v", h, m, str, found)
	}

	// Case 3: Saturday 2-4
	msgs3 := []MessageRecord{
		{Username: "user1", Content: "vay chac e dat 2 san 2-4 nha"},
	}
	h, m, found, str = ExtractMatchTime(msgs3, true)
	if !found || h != 14 || m != 0 || str != "14:00" {
		t.Errorf("expected 14:00 on Saturday, got %d:%d (%s), found=%v", h, m, str, found)
	}

	// Case 4: No time found -> returns fallback default
	var emptyMsgs []MessageRecord
	h, m, found, str = ExtractMatchTime(emptyMsgs, false)
	if found || h != 19 || m != 0 || str != "19:00 - 19:30" {
		t.Errorf("expected fallback 19:00 - 19:30, got %d:%d (%s), found=%v", h, m, str, found)
	}
}

func TestParseMessagesNLP(t *testing.T) {
	msgs := []MessageRecord{
		{UserID: "1", Username: "dai.trinhduc", Content: "+1 slot nha"},
		{UserID: "2", Username: "hoang.nguyen", Content: "e nay off fullday xin off nha mn"},
		{UserID: "3", Username: "tai.cumanhtuan", Content: "chiều đánh giao lưu"},
		{UserID: "4", Username: "duy.tran", Content: "sân 1 sân 2 nha mn"},
		{UserID: "1", Username: "dai.trinhduc", Content: "Minh Hải nha"},
	}

	summary := ParseMessagesNLP(msgs)

	// Going: dai.trinhduc, tai.cumanhtuan
	if len(summary.Going) != 2 {
		t.Errorf("expected 2 going, got %d: %v", len(summary.Going), summary.Going)
	}

	// Not going: hoang.nguyen
	if len(summary.NotGoing) != 1 || summary.NotGoing[0] != "hoang.nguyen" {
		t.Errorf("expected hoang.nguyen in not going, got %v", summary.NotGoing)
	}

	// Court: Minh Hải nha (latest court mention)
	if !strings.Contains(summary.CourtInfo, "Minh Hải") {
		t.Errorf("expected court info to contain Minh Hải, got %q", summary.CourtInfo)
	}
}

func TestManager_TriggerDiemDanh(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "test-chan",
		RoleID:    "1840693979155271680",
		RoleName:  "Vinh-CL",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC) // Monday
	err := mgr.TriggerDiemDanh(context.Background(), now)
	if err != nil {
		t.Fatalf("TriggerDiemDanh failed: %v", err)
	}

	if sender.invocations != 1 {
		t.Errorf("expected 1 send, got %d", sender.invocations)
	}
	if !strings.Contains(sender.lastText, "ĐIỂM DANH CẦU LÔNG - THỨ BA") {
		t.Errorf("expected diem danh text for Thu Ba, got %q", sender.lastText)
	}
	if !strings.Contains(sender.lastText, "@Vinh-CL") {
		t.Errorf("expected text to mention @Vinh-CL, got %q", sender.lastText)
	}
	if sender.lastOptions == nil || len(sender.lastOptions.Mentions) != 1 {
		t.Fatalf("expected 1 mention option, got %v", sender.lastOptions)
	}
	m := sender.lastOptions.Mentions[0]
	if m.RoleID != "1840693979155271680" || m.Rolename != "Vinh-CL" {
		t.Errorf("unexpected mention fields: %+v", m)
	}

	// Verify auto reactions seeded
	if len(sender.reactions) != 2 || sender.reactions[0] != ":cong1:" || sender.reactions[1] != ":tru1:" {
		t.Errorf("expected auto reactions [:cong1:, :tru1:], got %v", sender.reactions)
	}
}

func TestManager_RecordReaction(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "sport-chan",
		RoleID:    "1840693979155271680",
		RoleName:  "Vinh-CL",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	// User 1 reacts :cong1: (going)
	mgr.RecordReaction("sport-chan", "m1", "u1", "dai.trinhduc", ":cong1:", false, 1)
	// User 2 reacts :tru1: (not going)
	mgr.RecordReaction("sport-chan", "m1", "u2", "hoang.nguyen", ":tru1:", false, 1)
	// User 3 reacts cong1 (going)
	mgr.RecordReaction("sport-chan", "m1", "u3", "nam.nguyen", "cong1", false, 1)

	summary := mgr.SummarizeMessages(context.Background(), nil)
	if len(summary.Going) != 2 {
		t.Errorf("expected 2 going, got %d: %v", len(summary.Going), summary.Going)
	}
	if len(summary.NotGoing) != 1 || summary.NotGoing[0] != "hoang.nguyen" {
		t.Errorf("expected hoang.nguyen in not going, got %v", summary.NotGoing)
	}

	// User 2 switches to :cong1:
	mgr.RecordReaction("sport-chan", "m1", "u2", "hoang.nguyen", ":cong1:", false, 1)
	summary2 := mgr.SummarizeMessages(context.Background(), nil)
	if len(summary2.Going) != 3 {
		t.Errorf("expected 3 going after switch, got %d: %v", len(summary2.Going), summary2.Going)
	}
	if len(summary2.NotGoing) != 0 {
		t.Errorf("expected 0 not going after switch, got %v", summary2.NotGoing)
	}

	// User 1 removes reaction (undo)
	mgr.RecordReaction("sport-chan", "m1", "u1", "dai.trinhduc", ":cong1:", true, 0)
	summary3 := mgr.SummarizeMessages(context.Background(), nil)
	if len(summary3.Going) != 2 {
		t.Errorf("expected 2 going after un-react, got %d: %v", len(summary3.Going), summary3.Going)
	}
}

func TestManager_TriggerReminder(t *testing.T) {
	sender := &mockSender{}
	stateFile := t.TempDir() + "/badminton.json"
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "test-chan",
		RoleID:    "1840693979155271680",
		RoleName:  "Vinh-CL",
		StatePath: stateFile,
	}, sender)

	// Add messages
	mgr.RecordMessage("test-chan", "m1", "u1", "nam.nguyen", "e đi nha")
	mgr.RecordMessage("test-chan", "m2", "u2", "viet.le", "bận rồi xin off")
	mgr.RecordMessage("test-chan", "m3", "u3", "truong.tran", "sân 3 sân 4 nha")

	now := time.Date(2026, 11, 17, 17, 0, 0, 0, time.UTC) // Tuesday 17:00
	err := mgr.TriggerReminder(context.Background(), now)
	if err != nil {
		t.Fatalf("TriggerReminder failed: %v", err)
	}

	if sender.invocations != 1 {
		t.Errorf("expected 1 send, got %d", sender.invocations)
	}
	if !strings.Contains(sender.lastText, "NHẮC LỊCH ĐÁNH CẦU") {
		t.Errorf("expected reminder header, got %q", sender.lastText)
	}
	if !strings.Contains(sender.lastText, "nam.nguyen") {
		t.Errorf("expected nam.nguyen in summary, got %q", sender.lastText)
	}
	if !strings.Contains(sender.lastText, "viet.le") {
		t.Errorf("expected viet.le in absences, got %q", sender.lastText)
	}
	if !strings.Contains(sender.lastText, "sân 3 sân 4 nha") {
		t.Errorf("expected court info in reminder, got %q", sender.lastText)
	}
}

func TestManager_OpenAISummary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if reqBody["model"] != "gpt-4o-mini" {
			t.Errorf("unexpected model: %v", reqBody["model"])
		}

		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "• ✅ **Tham gia (2 người):** a.nguyen, b.tran\n• ❌ **Vắng mặt:** c.le\n• 🏟️ **Sân bãi:** Sân Minh Hải 19:30",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:       true,
		ChannelID:     "test-chan",
		RoleID:        "1840693979155271680",
		RoleName:      "Vinh-CL",
		StatePath:     t.TempDir() + "/badminton.json",
		OpenAIKey:     "test-key",
		OpenAIBaseURL: ts.URL,
		OpenAIModel:   "gpt-4o-mini",
	}, sender)

	mgr.RecordMessage("test-chan", "m1", "u1", "a.nguyen", "+1")
	mgr.RecordMessage("test-chan", "m2", "u2", "b.tran", "+1")
	mgr.RecordMessage("test-chan", "m3", "u3", "c.le", "bận")

	summary := mgr.SummarizeMessages(context.Background(), mgr.state.Messages)
	if !strings.Contains(summary.RawAI, "Sân Minh Hải 19:30") {
		t.Errorf("expected OpenAI response in RawAI, got %q", summary.RawAI)
	}
}

func TestParseMoneyAmount(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"45k", 45000, false},
		{"45K", 45000, false},
		{"45000", 45000, false},
		{"45.000", 45000, false},
		{"45,000", 45000, false},
		{"45", 45000, false},
		{"45k vnđ", 45000, false},
		{"50k", 50000, false},
		{"0", 0, true},
		{"-45k", 0, true},
		{"abc", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseMoneyAmount(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseMoneyAmount(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseMoneyAmount(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestMoneyBillSplittingWithMultiSlot(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "sport-chan",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	// Simulate 8 participants, with User 1 reacting :cong1: 2 times (2 slots)
	mgr.RecordReaction("sport-chan", "m1", "u1", "dai.trinhduc", ":cong1:", false, 2)
	mgr.RecordReaction("sport-chan", "m1", "u2", "user2", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u3", "user3", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u4", "user4", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u5", "user5", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u6", "user6", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u7", "user7", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u8", "user8", ":cong1:", false, 1)

	// User 9 reacted :tru1: (off)
	mgr.RecordReaction("sport-chan", "m1", "u9", "user9_off", ":tru1:", false, 1)

	_, _, participants := mgr.GetParticipantsForMoney()
	if len(participants) != 8 {
		t.Fatalf("expected 8 participants, got %d", len(participants))
	}

	totalSlots := 0
	var u1Slots int
	for _, p := range participants {
		totalSlots += p.Slots
		if p.Username == "dai.trinhduc" {
			u1Slots = p.Slots
		}
	}

	if u1Slots != 2 {
		t.Errorf("expected dai.trinhduc to have 2 slots, got %d", u1Slots)
	}
	if totalSlots != 9 {
		t.Errorf("expected total slots 9, got %d", totalSlots)
	}

	unitAmount := 45000
	u1Amount := u1Slots * unitAmount
	if u1Amount != 90000 {
		t.Errorf("expected dai.trinhduc to owe 90k, got %d", u1Amount)
	}

	totalAmount := totalSlots * unitAmount
	if totalAmount != 405000 {
		t.Errorf("expected total amount 405k, got %d", totalAmount)
	}
}

func TestPreviousMatchArchivingOnTriggerDiemDanh(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "sport-chan",
		RoleID:    "1840693979155271680",
		RoleName:  "Vinh-CL",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	// Match 1: Tuesday
	mgr.state.MatchDay = "Thứ Ba"
	mgr.state.MatchDate = "17/11/2026"
	mgr.RecordReaction("sport-chan", "m1", "u1", "dai.trinhduc", ":cong1:", false, 2)
	mgr.RecordReaction("sport-chan", "m1", "u2", "nam.nguyen", ":cong1:", false, 1)

	// Trigger Next Diem danh for Thursday (Wednesday 9am)
	wedNow := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	if err := mgr.TriggerDiemDanh(context.Background(), wedNow); err != nil {
		t.Fatalf("TriggerDiemDanh failed: %v", err)
	}

	// Verify previous match was archived
	if len(mgr.state.PreviousMatchParticipants) != 2 {
		t.Fatalf("expected 2 previous match participants, got %d", len(mgr.state.PreviousMatchParticipants))
	}
	if mgr.state.PreviousMatchDay != "Thứ Ba" || mgr.state.PreviousMatchDate != "17/11/2026" {
		t.Errorf("unexpected previous match day/date: %s %s", mgr.state.PreviousMatchDay, mgr.state.PreviousMatchDate)
	}

	// Now call GetParticipantsForMoney: since current session match is in the future (Thursday), it should return Tuesday's previous match
	day, date, participants := mgr.GetParticipantsForMoney()
	if day != "Thứ Ba" || date != "17/11/2026" {
		t.Errorf("expected previous match Tuesday 17/11/2026, got %s %s", day, date)
	}
	if len(participants) != 2 {
		t.Fatalf("expected 2 participants from previous match, got %d", len(participants))
	}
}
