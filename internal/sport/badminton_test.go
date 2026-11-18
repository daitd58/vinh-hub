package sport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	// Add reactions for attendance
	mgr.RecordReaction("test-chan", "m0", "u1", "nam.nguyen", ":cong1:", false, 1)
	mgr.RecordReaction("test-chan", "m0", "u2", "viet.le", ":tru1:", false, 1)

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

func TestParseSplitFormula(t *testing.T) {
	tests := []struct {
		input       string
		courtFee    int64
		shuttleCnt  int
		shuttleFee  int64
		totalFee    int64
		expectErr   bool
	}{
		{"360k + 26*3", 360000, 3, 78000, 438000, false},
		{"360k + 26k*3", 360000, 3, 78000, 438000, false},
		{"360k + 3*26k", 360000, 3, 78000, 438000, false},
		{"360k + 3*26", 360000, 3, 78000, 438000, false},
		{"360k + 26*4 cau", 360000, 4, 104000, 464000, false},
		{"360k + 3 cau", 360000, 3, 78000, 438000, false},
		{"360k + 3 quả cầu", 360000, 3, 78000, 438000, false},
		{"360k + 45k", 360000, 0, 45000, 405000, false},
		{"360k san 45k cau", 360000, 0, 45000, 405000, false},
		{"360k san 3 cau", 360000, 3, 78000, 438000, false},
		{"360k san 26*3 cau", 360000, 3, 78000, 438000, false},
		{"450k", 450000, 0, 0, 450000, false},
		{"360.000 + 26.000 * 3", 360000, 3, 78000, 438000, false},
		{"", 0, 0, 0, 0, true},
	}

	for _, tc := range tests {
		res, err := ParseSplitFormula(tc.input)
		if tc.expectErr {
			if err == nil {
				t.Errorf("expected error for %q, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tc.input, err)
			continue
		}
		if res.CourtFee != tc.courtFee {
			t.Errorf("[%s] expected court fee %d, got %d", tc.input, tc.courtFee, res.CourtFee)
		}
		if tc.shuttleCnt > 0 && res.ShuttleCount != tc.shuttleCnt {
			t.Errorf("[%s] expected shuttle count %d, got %d", tc.input, tc.shuttleCnt, res.ShuttleCount)
		}
		if res.ShuttleFee != tc.shuttleFee {
			t.Errorf("[%s] expected shuttle fee %d, got %d", tc.input, tc.shuttleFee, res.ShuttleFee)
		}
		if res.TotalFee != tc.totalFee {
			t.Errorf("[%s] expected total fee %d, got %d", tc.input, tc.totalFee, res.TotalFee)
		}
	}
}

func TestCreateSplitBillAndPaymentTracking(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "sport-chan",
		RoleID:    "1840693979155271680",
		RoleName:  "Vinh-CL",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	mgr.state.MatchDay = "Thứ Ba"
	mgr.state.MatchDate = "17/11/2026"
	mgr.RecordReaction("sport-chan", "m1", "u1", "dai.trinhduc", ":cong1:", false, 2)
	mgr.RecordReaction("sport-chan", "m1", "u2", "user2", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u3", "user3", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u4", "user4", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u5", "user5", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u6", "user6", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u7", "user7", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m1", "u8", "user8", ":cong1:", false, 1)

	// Total slots = 2 + 7 = 9 slots. Total fee = 360k + 78k = 438,000.
	// 438,000 / 9 = 48,666.67 -> rounds up to 49,000 VND / slot.
	bill, breakdown, err := mgr.CreateSplitBill("360k + 26*3")
	if err != nil {
		t.Fatalf("CreateSplitBill failed: %v", err)
	}

	if breakdown.TotalFee != 438000 {
		t.Errorf("expected total fee 438k, got %d", breakdown.TotalFee)
	}
	if bill.SlotPrice != 49000 {
		t.Errorf("expected slot price 49k, got %d", bill.SlotPrice)
	}

	u1Item := bill.Items["u1"]
	if u1Item == nil || u1Item.Amount != 98000 {
		t.Errorf("expected u1 (2 slots) to owe 98k, got %v", u1Item)
	}
	u2Item := bill.Items["u2"]
	if u2Item == nil || u2Item.Amount != 49000 {
		t.Errorf("expected u2 (1 slot) to owe 49k, got %v", u2Item)
	}

	// Test message ID association
	mgr.SetCurrentBillMessageID("bill-msg-123")
	if !mgr.IsCurrentBillMessage("bill-msg-123") {
		t.Errorf("expected IsCurrentBillMessage to return true")
	}

	// 1. Mark u1 paid via reaction / userID
	item, ok := mgr.MarkPaidByUserIDOrName("u1", "dai.trinhduc")
	if !ok || !item.Paid {
		t.Errorf("expected u1 to be marked paid")
	}

	// 2. Mark u2 and u3 paid via batch command
	paid, notFound, totalPaid, _, paidCount, totalCount, err := mgr.MarkPaidBatch([]string{"@user2", "user3", "unknown_user"})
	if err != nil {
		t.Fatalf("MarkPaidBatch failed: %v", err)
	}
	if len(paid) != 2 {
		t.Errorf("expected 2 paid items, got %d", len(paid))
	}
	if len(notFound) != 1 || notFound[0] != "unknown_user" {
		t.Errorf("expected unknown_user in notFound, got %v", notFound)
	}
	if paidCount != 3 || totalCount != 8 {
		t.Errorf("expected 3/8 paid, got %d/%d", paidCount, totalCount)
	}
	// u1 (98k) + u2 (49k) + u3 (49k) = 196,000
	if totalPaid != 196000 {
		t.Errorf("expected totalPaid 196k, got %d", totalPaid)
	}

	// 3. Test GetBillStatus
	_, paidList, unpaidList, curPaid, _, err := mgr.GetBillStatus()
	if err != nil {
		t.Fatalf("GetBillStatus failed: %v", err)
	}
	if len(paidList) != 3 || len(unpaidList) != 5 {
		t.Errorf("expected 3 paid, 5 unpaid, got %d paid, %d unpaid", len(paidList), len(unpaidList))
	}
	if curPaid != 196000 {
		t.Errorf("expected curPaid 196k, got %d", curPaid)
	}

	// 4. Test GetUnpaidList
	_, unpaidItems, totalUnpaid, err := mgr.GetUnpaidList()
	if err != nil {
		t.Fatalf("GetUnpaidList failed: %v", err)
	}
	if len(unpaidItems) != 5 {
		t.Errorf("expected 5 unpaid items, got %d", len(unpaidItems))
	}
	// 5 unpaid * 49k = 245,000
	if totalUnpaid != 245000 {
		t.Errorf("expected totalUnpaid 245k, got %d", totalUnpaid)
	}

	// 5. Test MarkUnpaidBatch (e.g. unmarking u2)
	unmarked, notFound2, err := mgr.MarkUnpaidBatch([]string{"@user2"})
	if err != nil {
		t.Fatalf("MarkUnpaidBatch failed: %v", err)
	}
	if len(unmarked) != 1 || unmarked[0].Paid != false {
		t.Errorf("expected user2 to be unmarked paid")
	}
	if len(notFound2) != 0 {
		t.Errorf("unexpected notFound items: %v", notFound2)
	}

	_, paidList2, _, _, _, _ := mgr.GetBillStatus()
	if len(paidList2) != 2 {
		t.Errorf("expected 2 paid items after unmarking user2, got %d", len(paidList2))
	}
}

func TestLeaderboard(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "sport-chan",
		RoleID:    "1840693979155271680",
		RoleName:  "Vinh-CL",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	// Add 3 matches in November 2026
	mgr.state.MatchHistory = []MatchRecord{
		{
			SessionID: "m1",
			MatchDay:  "Thứ Ba",
			MatchDate: "03/11/2026",
			Attendees: []Participant{
				{Username: "dai.trinhduc", Slots: 2},
				{Username: "nam.nguyen", Slots: 1},
			},
			Absentees: []string{"sui.master"},
		},
		{
			SessionID: "m2",
			MatchDay:  "Thứ Năm",
			MatchDate: "05/11/2026",
			Attendees: []Participant{
				{Username: "dai.trinhduc", Slots: 1},
				{Username: "nam.nguyen", Slots: 1},
			},
			Absentees: []string{"sui.master", "luoi.bieng"},
		},
		{
			SessionID: "m3",
			MatchDay:  "Thứ Bảy",
			MatchDate: "07/11/2026",
			Attendees: []Participant{
				{Username: "dai.trinhduc", Slots: 1},
			},
			Absentees: []string{"nam.nguyen", "sui.master"},
		},
	}

	now := time.Date(2026, 11, 30, 20, 0, 0, 0, time.UTC)
	text, err := mgr.GetLeaderboardText("11/2026", now)
	if err != nil {
		t.Fatalf("GetLeaderboardText failed: %v", err)
	}

	if !strings.Contains(text, "THÁNG 11/2026") {
		t.Errorf("expected text to mention THÁNG 11/2026, got %s", text)
	}
	if !strings.Contains(text, "Tổng số buổi đã tổ chức: **3 buổi**") {
		t.Errorf("expected 3 matches, got %s", text)
	}
	// dai.trinhduc attended 3/3 -> Chiến thần quật cầu (must show canonical Clan Nickname)
	if !strings.Contains(text, "👑 **@dai.trinhduc**: 3/3 buổi") || !strings.Contains(text, "Chiến thần quật cầu") {
		t.Errorf("expected dai.trinhduc as Chien than quat cau, got %s", text)
	}
	if strings.Contains(text, "@Trịnh Đức Đại") {
		t.Errorf("should never contain Vietnamese full name, got: %s", text)
	}
	// sui.master absent 3 times -> Chúa sủi của tháng
	if !strings.Contains(text, "🍗 **@sui.master**: **3 lần**") || !strings.Contains(text, "Chúa sủi của tháng") {
		t.Errorf("expected sui.master as Chua sui, got %s", text)
	}

	// Test historical records having Vietnamese real names: must be resolved to Clan Nicknames
	mgr.state.MatchHistory = []MatchRecord{
		{
			SessionID: "m_realnames",
			MatchDay:  "Thứ Hai",
			MatchDate: "02/11/2026",
			Attendees: []Participant{
				{UserID: "1946167706600345600", Username: "Nguyễn Thành Đôn", Slots: 2},
				{UserID: "1946197154334773248", Username: "Đới Phi Hậu", Slots: 1},
			},
			Absentees: []string{"Nguyễn Đình Toàn", "Phạm Minh Tuấn", "Đặng Cao Trí"},
		},
	}
	text2, err := mgr.GetLeaderboardText("11/2026", now)
	if err != nil {
		t.Fatalf("GetLeaderboardText failed: %v", err)
	}
	// Verify that real names are converted to clan nicknames
	if !strings.Contains(text2, "@don.nguyenthanh") {
		t.Errorf("expected @don.nguyenthanh, got:\n%s", text2)
	}
	if !strings.Contains(text2, "@hau.duongphuc") {
		t.Errorf("expected @hau.duongphuc, got:\n%s", text2)
	}
	if !strings.Contains(text2, "@toan.nguyendinh") {
		t.Errorf("expected @toan.nguyendinh, got:\n%s", text2)
	}
	if !strings.Contains(text2, "@tuan.phamminh2") {
		t.Errorf("expected @tuan.phamminh2, got:\n%s", text2)
	}
	if !strings.Contains(text2, "@tri.dangcao") {
		t.Errorf("expected @tri.dangcao, got:\n%s", text2)
	}
	// Verify that none of the Vietnamese real names appear with @
	for _, realName := range []string{"Nguyễn Thành Đôn", "Đới Phi Hậu", "Nguyễn Đình Toàn", "Phạm Minh Tuấn", "Đặng Cao Trí"} {
		if strings.Contains(text2, "@"+realName) {
			t.Errorf("found unconverted real name @%s in leaderboard:\n%s", realName, text2)
		}
	}
}

func TestBillHistoryAndMultiSessionDistinction(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "sport-chan",
		RoleID:    "1840693979155271680",
		RoleName:  "Vinh-CL",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	// Match 1: 17/11/2026
	mgr.state.MatchDate = "17/11/2026"
	mgr.state.MatchDay = "Thứ Ba"
	mgr.RecordReaction("sport-chan", "m1", "u1", "dai.trinhduc", ":cong1:", false, 2)
	mgr.RecordReaction("sport-chan", "m1", "u2", "nam.nguyen", ":cong1:", false, 1)

	bill1, _, err := mgr.CreateSplitBill("360k san 2 cau")
	if err != nil {
		t.Fatalf("CreateSplitBill 1 failed: %v", err)
	}
	mgr.SetCurrentBillMessageID("msg_bill_1")

	if bill1.MatchDate != "17/11/2026" {
		t.Fatalf("expected MatchDate 17/11/2026, got %s", bill1.MatchDate)
	}

	// Match 2: 19/11/2026
	mgr.state.MatchDate = "19/11/2026"
	mgr.state.MatchDay = "Thứ Năm"
	mgr.state.GoingParticipants = make(map[string]Participant)
	mgr.RecordReaction("sport-chan", "m2", "u1", "dai.trinhduc", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m2", "u2", "nam.nguyen", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m2", "u3", "viet.le", ":cong1:", false, 1)

	bill2, err := mgr.CreateMoneyBill(45000)
	if err != nil {
		t.Fatalf("CreateMoneyBill 2 failed: %v", err)
	}
	mgr.SetCurrentBillMessageID("msg_bill_2")

	if bill2.MatchDate != "19/11/2026" {
		t.Fatalf("expected MatchDate 19/11/2026, got %s", bill2.MatchDate)
	}

	// Check BillHistory
	if len(mgr.state.BillHistory) != 1 {
		t.Fatalf("expected 1 archived bill in history, got %d", len(mgr.state.BillHistory))
	}
	if mgr.state.BillHistory[0].MessageID != "msg_bill_1" {
		t.Errorf("expected archived bill message ID msg_bill_1, got %s", mgr.state.BillHistory[0].MessageID)
	}

	// Both message IDs should be recognized
	if !mgr.IsCurrentBillMessage("msg_bill_1") {
		t.Errorf("expected msg_bill_1 to be recognized as bill message")
	}
	if !mgr.IsCurrentBillMessage("msg_bill_2") {
		t.Errorf("expected msg_bill_2 to be recognized as bill message")
	}

	// Test reaction payment specifically on bill 1
	item1, targetBill1, ok := mgr.MarkPaidByUserIDOrNameOnBill("msg_bill_1", "u1", "dai.trinhduc")
	if !ok || item1 == nil {
		t.Fatalf("failed to mark u1 as paid on bill 1")
	}
	if targetBill1.MatchDate != "17/11/2026" {
		t.Errorf("expected target bill to be 17/11/2026, got %s", targetBill1.MatchDate)
	}

	// Verify dai.trinhduc is paid on bill 1, but STILL UNPAID on bill 2
	_, b1Paid, b1Unpaid, _, _, err := mgr.GetBillStatus("17/11")
	if err != nil {
		t.Fatalf("GetBillStatus(17/11) failed: %v", err)
	}
	if len(b1Paid) != 1 || b1Paid[0].Username != "dai.trinhduc" {
		t.Errorf("expected dai.trinhduc paid on bill 1")
	}
	if len(b1Unpaid) != 1 || b1Unpaid[0].Username != "nam.nguyen" {
		t.Errorf("expected nam.nguyen unpaid on bill 1")
	}

	// Bill 2 (current bill) status
	_, b2Paid, b2Unpaid, _, _, err := mgr.GetBillStatus("19/11")
	if err != nil {
		t.Fatalf("GetBillStatus(19/11) failed: %v", err)
	}
	if len(b2Paid) != 0 {
		t.Errorf("expected 0 paid on bill 2 so far, got %d", len(b2Paid))
	}
	if len(b2Unpaid) != 3 {
		t.Errorf("expected 3 unpaid on bill 2, got %d", len(b2Unpaid))
	}

	// Test default GetBillStatus (without date filter) returns current bill (bill 2)
	curBill, _, curUnpaid, _, _, err := mgr.GetBillStatus()
	if err != nil {
		t.Fatalf("GetBillStatus() failed: %v", err)
	}
	if curBill.MatchDate != "19/11/2026" {
		t.Errorf("expected current bill 19/11/2026, got %s", curBill.MatchDate)
	}
	if len(curUnpaid) != 3 {
		t.Errorf("expected 3 unpaid on current bill, got %d", len(curUnpaid))
	}

	// Test GetUnpaidList with date filter
	_, unp1, totalUnp1, err := mgr.GetUnpaidList("17/11")
	if err != nil {
		t.Fatalf("GetUnpaidList(17/11) failed: %v", err)
	}
	if len(unp1) != 1 || unp1[0].Username != "nam.nguyen" {
		t.Errorf("expected nam.nguyen unpaid on bill 1")
	}
	if totalUnp1 <= 0 {
		t.Errorf("expected total unpaid > 0, got %d", totalUnp1)
	}

	// Test MarkPaidBatch on historical bill (17/11)
	paidBatch, notFound, _, _, _, _, err := mgr.MarkPaidBatch([]string{"nam.nguyen"}, "17/11")
	if err != nil {
		t.Fatalf("MarkPaidBatch(17/11) failed: %v", err)
	}
	if len(notFound) > 0 || len(paidBatch) != 1 {
		t.Errorf("expected nam.nguyen marked paid on bill 1")
	}

	// Now bill 1 should be completely paid
	_, b1PaidAfter, b1UnpaidAfter, _, _, _ := mgr.GetBillStatus("17/11")
	if len(b1PaidAfter) != 2 || len(b1UnpaidAfter) != 0 {
		t.Errorf("expected bill 1 to have 2 paid and 0 unpaid")
	}

	// Mark unpaid on historical bill
	unmarked, notFound2, err := mgr.MarkUnpaidBatch([]string{"nam.nguyen"}, "17/11")
	if err != nil {
		t.Fatalf("MarkUnpaidBatch(17/11) failed: %v", err)
	}
	if len(notFound2) > 0 || len(unmarked) != 1 {
		t.Errorf("expected nam.nguyen marked unpaid on bill 1")
	}

	// Reaction removal on bill 1
	unmarkedOnBill, _, err := mgr.MarkUnpaidOnBill("msg_bill_1", []string{"dai.trinhduc"})
	if err != nil {
		t.Fatalf("MarkUnpaidOnBill failed: %v", err)
	}
	if len(unmarkedOnBill) != 1 || unmarkedOnBill[0].Username != "dai.trinhduc" {
		t.Errorf("expected dai.trinhduc marked unpaid via reaction removal")
	}
}

func TestBillIDGenerationAndCustomTagSplit(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:   true,
		ChannelID: "sport-chan",
		StatePath: t.TempDir() + "/badminton.json",
	}, sender)

	// 1. Create a bill with custom tagged participants directly (no prior check-in)
	customParticipants := []Participant{
		{UserID: "u1", Username: "dai.trinhduc", Slots: 2},
		{UserID: "u2", Username: "nam.nguyen", Slots: 1},
		{UserID: "u3", Username: "viet.le", Slots: 1},
	}

	bill1, breakdown1, err := mgr.CreateSplitBill("360k + 26*9", customParticipants...)
	if err != nil {
		t.Fatalf("CreateSplitBill with custom participants failed: %v", err)
	}

	// Total fee = 360k + 234k = 594,000 VND
	if breakdown1.TotalFee != 594000 {
		t.Errorf("expected total fee 594k, got %d", breakdown1.TotalFee)
	}
	// Slots = 4. 594,000 / 4 = 148,500 -> round up to 149,000 VND / slot
	if bill1.SlotPrice != 149000 {
		t.Errorf("expected slot price 149k, got %d", bill1.SlotPrice)
	}
	if bill1.BillNumber != 1 || bill1.ID != "1" || bill1.BillCode != "CL1" {
		t.Errorf("expected bill 1 (CL1), got num=%d id=%s code=%s", bill1.BillNumber, bill1.ID, bill1.BillCode)
	}
	if bill1.Items["u1"].Amount != 298000 {
		t.Errorf("expected u1 amount 298k, got %d", bill1.Items["u1"].Amount)
	}

	// 2. Create second bill using *money with custom participants
	bill2, err := mgr.CreateMoneyBill(45000, customParticipants[:2]...)
	if err != nil {
		t.Fatalf("CreateMoneyBill 2 failed: %v", err)
	}

	if bill2.BillNumber != 2 || bill2.ID != "2" || bill2.BillCode != "CL2" {
		t.Errorf("expected bill 2 (CL2), got num=%d id=%s code=%s", bill2.BillNumber, bill2.ID, bill2.BillCode)
	}

	// 3. Test findBillLocked with ID variations
	// Find bill 1 by ID "1"
	b1ByID, _, _, _, _, err := mgr.GetBillStatus("1")
	if err != nil || b1ByID.BillNumber != 1 {
		t.Errorf("expected to find bill 1 by ID '1', got %v", b1ByID)
	}

	// Find bill 1 by Code "CL1"
	b1ByCode, _, _, _, _, err := mgr.GetBillStatus("CL1")
	if err != nil || b1ByCode.BillNumber != 1 {
		t.Errorf("expected to find bill 1 by Code 'CL1', got %v", b1ByCode)
	}

	// Find bill 1 by "#1"
	b1ByHash, _, _, _, _, err := mgr.GetBillStatus("#1")
	if err != nil || b1ByHash.BillNumber != 1 {
		t.Errorf("expected to find bill 1 by Hash '#1', got %v", b1ByHash)
	}

	// Find bill 2 by ID "2"
	b2ByID, _, _, _, _, err := mgr.GetBillStatus("2")
	if err != nil || b2ByID.BillNumber != 2 {
		t.Errorf("expected to find bill 2 by ID '2', got %v", b2ByID)
	}

	// Find bill 2 by Code "cl2" (case-insensitive)
	b2ByCode, _, _, _, _, err := mgr.GetBillStatus("cl2")
	if err != nil || b2ByCode.BillNumber != 2 {
		t.Errorf("expected to find bill 2 by Code 'cl2', got %v", b2ByCode)
	}

	// 4. Test MarkPaidBatch with Bill ID
	paidItems, notFound, totalPaid, _, _, _, err := mgr.MarkPaidBatch([]string{"@dai.trinhduc"}, "1")
	if err != nil || len(paidItems) != 1 || len(notFound) > 0 {
		t.Fatalf("failed to mark paid on bill 1 using ID '1'")
	}
	if totalPaid != 298000 {
		t.Errorf("expected totalPaid 298k on bill 1, got %d", totalPaid)
	}

	// Verify bill 2 still has dai.trinhduc unpaid
	_, b2Paid, b2Unpaid, _, _, _ := mgr.GetBillStatus("2")
	if len(b2Paid) != 0 || len(b2Unpaid) != 2 {
		t.Errorf("expected bill 2 to remain completely unpaid, got paid=%d unpaid=%d", len(b2Paid), len(b2Unpaid))
	}
}

func TestAttendanceStrictlyFromReactionsAndCourtFromChat(t *testing.T) {
	mgr := NewManager(Config{
		ChannelID: "sport-chan",
		StatePath: filepath.Join(t.TempDir(), "test_badminton.json"),
	}, nil)

	// UserA reacts :cong1:, UserB reacts :tru1:
	mgr.RecordReaction("sport-chan", "m0", "u_a", "anh.le", ":cong1:", false, 1)
	mgr.RecordReaction("sport-chan", "m0", "u_b", "binh.tran", ":tru1:", false, 1)

	// In chat, other users send messages but DO NOT react
	mgr.RecordMessage("sport-chan", "m1", "u_c", "cuong.pham", "+1 em di nha")
	mgr.RecordMessage("sport-chan", "m2", "u_d", "dung.hoang", "bận rồi xin off")
	mgr.RecordMessage("sport-chan", "m3", "u_e", "em.nguyen", "Sân Minh Hải, Sân 6, khung giờ 19:30 - 21:30 (tối nay)")

	summary := mgr.SummarizeMessages(context.Background(), mgr.state.Messages)

	// Going must strictly be UserA only
	if len(summary.Going) != 1 || !strings.Contains(summary.Going[0], "anh.le") {
		t.Fatalf("expected strictly UserA in going, got: %v", summary.Going)
	}

	// NotGoing must strictly be UserB only
	if len(summary.NotGoing) != 1 || !strings.Contains(summary.NotGoing[0], "binh.tran") {
		t.Fatalf("expected strictly UserB in not going, got: %v", summary.NotGoing)
	}

	// Court info must be extracted from UserE's chat message
	if !strings.Contains(summary.CourtInfo, "Minh Hải") || !strings.Contains(summary.CourtInfo, "Sân 6") {
		t.Fatalf("expected court info to contain Minh Hải Sân 6, got: %q", summary.CourtInfo)
	}

	// Verify formatted output in GetSummary
	text := mgr.GetSummary(context.Background())
	if !strings.Contains(text, "Tham gia (1 người):") || !strings.Contains(text, "anh.le") {
		t.Errorf("expected anh.le in GetSummary, got: %s", text)
	}
	if strings.Contains(text, "cuong.pham") {
		t.Errorf("cuong.pham chatted +1 but did not react, must NOT appear in summary: %s", text)
	}
	if strings.Contains(text, "dung.hoang") {
		t.Errorf("dung.hoang chatted off but did not react, must NOT appear in summary: %s", text)
	}
	if !strings.Contains(text, "Vắng mặt (1 người):") || !strings.Contains(text, "binh.tran") {
		t.Errorf("expected binh.tran in absences: %s", text)
	}
	if !strings.Contains(text, "Sân 6") {
		t.Errorf("expected Sân 6 in court info: %s", text)
	}
}

func TestMarkPaidSelf(t *testing.T) {
	mgr := NewManager(Config{
		ChannelID: "sport-chan",
		StatePath: filepath.Join(t.TempDir(), "test_badminton.json"),
	}, nil)

	// Create a bill session with 2 participants
	parts := []Participant{
		{UserID: "u1", Username: "son.nguyentruong1", DisplayName: "son.nguyentruong1", Slots: 1},
		{UserID: "u2", Username: "dai.trinhduc", DisplayName: "dai.trinhduc", Slots: 1},
	}
	bill, err := mgr.CreateMoneyBill(45000, parts...)
	if err != nil {
		t.Fatalf("CreateMoneyBill failed: %v", err)
	}

	// 1. Self-mark unpaid participant on active bill
	item, alreadyPaid, totalPaid, totalCost, paidCount, totalCount, err := mgr.MarkPaidSelf("u1", "son.nguyentruong1", "", "")
	if err != nil {
		t.Fatalf("MarkPaidSelf failed: %v", err)
	}
	if item == nil || !item.Paid {
		t.Fatalf("expected item to be marked paid")
	}
	if alreadyPaid {
		t.Errorf("expected alreadyPaid to be false on first self-confirmation")
	}
	if paidCount != 1 || totalCount != 2 || totalPaid != 45000 || totalCost != 90000 {
		t.Errorf("unexpected counts: paidCount=%d totalCount=%d totalPaid=%d totalCost=%d", paidCount, totalCount, totalPaid, totalCost)
	}

	// 2. Self-mark again: should report alreadyPaid = true
	item2, alreadyPaid2, _, _, _, _, err := mgr.MarkPaidSelf("u1", "son.nguyentruong1", "", "")
	if err != nil {
		t.Fatalf("second MarkPaidSelf failed: %v", err)
	}
	if item2 == nil || !alreadyPaid2 {
		t.Errorf("expected alreadyPaid=true on second call")
	}

	// 3. Self-mark on specific bill by Bill ID "1"
	item3, alreadyPaid3, totalPaid3, _, paidCount3, _, err := mgr.MarkPaidSelf("u2", "dai.trinhduc", "", "", bill.ID)
	if err != nil {
		t.Fatalf("MarkPaidSelf with bill ID failed: %v", err)
	}
	if item3 == nil || !item3.Paid || alreadyPaid3 {
		t.Errorf("expected item3 to be marked paid, alreadyPaid=false")
	}
	if paidCount3 != 2 || totalPaid3 != 90000 {
		t.Errorf("expected full payment (2/2): paidCount=%d totalPaid=%d", paidCount3, totalPaid3)
	}

	// 4. Non-participant attempts self-confirm: should return nil item without error
	item4, alreadyPaid4, _, _, _, _, err := mgr.MarkPaidSelf("u999", "stranger", "", "", bill.ID)
	if err != nil {
		t.Fatalf("MarkPaidSelf for stranger returned error: %v", err)
	}
	if item4 != nil || alreadyPaid4 {
		t.Errorf("expected nil item and alreadyPaid=false for non-participant")
	}
}

func TestParseMatchSchedule(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, loc) // Wednesday 18/11/2026

	// Case 1: Empty input -> NextMatchInfo (Thursday 19/11)
	d1, dt1, tm1, _, spec1, err := ParseMatchSchedule("", now, loc)
	if err != nil || d1 != "Thứ Năm" || dt1 != "19/11/2026" || spec1 {
		t.Errorf("Case 1 failed: day=%s date=%s time=%s spec=%v err=%v", d1, dt1, tm1, spec1, err)
	}

	// Case 2: "toi nay 19:30"
	d2, dt2, tm2, _, spec2, err := ParseMatchSchedule("toi nay 19:30", now, loc)
	if err != nil || d2 != "Thứ Tư" || dt2 != "18/11/2026" || tm2 != "19:30" || !spec2 {
		t.Errorf("Case 2 failed: day=%s date=%s time=%s spec=%v err=%v", d2, dt2, tm2, spec2, err)
	}

	// Case 3: "t4 19:30 san 3"
	d3, dt3, tm3, c3, spec3, err := ParseMatchSchedule("t4 19:30 san 3", now, loc)
	if err != nil || d3 != "Thứ Tư" || dt3 != "18/11/2026" || tm3 != "19:30" || c3 != "Sân 3" || !spec3 {
		t.Errorf("Case 3 failed: day=%s date=%s time=%s court=%s spec=%v err=%v", d3, dt3, tm3, c3, spec3, err)
	}

	// Case 4: "t2 19:00" -> Next Monday 23/11/2026
	d4, dt4, tm4, _, spec4, err := ParseMatchSchedule("t2 19:00", now, loc)
	if err != nil || d4 != "Thứ Hai" || dt4 != "23/11/2026" || tm4 != "19:00" || !spec4 {
		t.Errorf("Case 4 failed: day=%s date=%s time=%s spec=%v err=%v", d4, dt4, tm4, spec4, err)
	}

	// Case 5: "19/11 20:00" -> 19/11/2026
	d5, dt5, tm5, _, spec5, err := ParseMatchSchedule("19/11 20:00", now, loc)
	if err != nil || d5 != "Thứ Năm" || dt5 != "19/11/2026" || tm5 != "20:00" || !spec5 {
		t.Errorf("Case 5 failed: day=%s date=%s time=%s spec=%v err=%v", d5, dt5, tm5, spec5, err)
	}
}

func TestConfirmAndCancelMatch_Permissions(t *testing.T) {
	sender := &mockSender{}
	mgr := NewManager(Config{
		Enabled:      true,
		ChannelID:    "chan1",
		RoleName:     "Vinh-CL",
		AdminUserIDs: []string{"1783440675396653056"},
		StatePath:    filepath.Join(t.TempDir(), "state.json"),
	}, sender)

	adminID := "1783440675396653056"
	strangerID := "9999999999999999999"

	// 1. Check IsAdmin
	if !mgr.IsAdmin(adminID) {
		t.Errorf("expected adminID %s to be admin", adminID)
	}
	if mgr.IsAdmin(strangerID) {
		t.Errorf("expected strangerID %s NOT to be admin", strangerID)
	}

	// 2. Stranger attempts ConfirmMatch -> should fail with unauthorized error
	_, err := mgr.ConfirmMatch(context.Background(), strangerID, "stranger", "19:30")
	if err == nil {
		t.Errorf("expected error when stranger confirms match")
	}

	// 3. Admin confirms match
	msg, err := mgr.ConfirmMatch(context.Background(), adminID, "dai.trinhduc", "t4 19:30 san 3")
	if err != nil {
		t.Fatalf("admin ConfirmMatch failed: %v", err)
	}
	if !mgr.IsConfirmed() {
		t.Errorf("expected manager to be confirmed")
	}
	if !strings.Contains(msg, "ĐÃ XÁC NHẬN LỊCH ĐÁNH CẦU") || !strings.Contains(msg, "19:30") {
		t.Errorf("unexpected confirm message: %s", msg)
	}

	// 4. Stranger attempts CancelMatch -> should fail
	_, err = mgr.CancelMatch(context.Background(), strangerID, "stranger")
	if err == nil {
		t.Errorf("expected error when stranger cancels match")
	}

	// 5. Admin cancels match
	cancelMsg, err := mgr.CancelMatch(context.Background(), adminID, "dai.trinhduc")
	if err != nil {
		t.Fatalf("admin CancelMatch failed: %v", err)
	}
	if mgr.IsConfirmed() {
		t.Errorf("expected manager to NOT be confirmed after cancel")
	}
	if !strings.Contains(cancelMsg, "THÔNG BÁO HỦY LỊCH") {
		t.Errorf("unexpected cancel message: %s", cancelMsg)
	}
}

func TestTick_ReminderOnlyWhenConfirmed(t *testing.T) {
	sender := &mockSender{}
	loc := time.FixedZone("ICT", 7*3600)
	mgr := NewManager(Config{
		Enabled:      true,
		ChannelID:    "chan1",
		RoleName:     "Vinh-CL",
		AdminUserIDs: []string{"1783440675396653056"},
		Timezone:     loc,
		StatePath:    filepath.Join(t.TempDir(), "state.json"),
	}, sender)

	matchDate := "18/11/2026"
	mgr.state.MatchDate = matchDate
	mgr.state.MatchDay = "Thứ Tư"
	mgr.state.MatchTime = "19:30"
	mgr.state.Confirmed = false // NOT confirmed!

	// 18:30 on match date (1 hour before 19:30)
	tickTime := time.Date(2026, 11, 18, 18, 30, 0, 0, loc)

	// Tick when unconfirmed: should NOT send reminder
	mgr.tick(context.Background(), tickTime)
	if sender.invocations > 0 {
		t.Fatalf("expected NO reminder sent when unconfirmed, got %d invocations", sender.invocations)
	}

	// Now confirm by admin
	_, err := mgr.ConfirmMatch(context.Background(), "1783440675396653056", "dai.trinhduc", "19:30")
	if err != nil {
		t.Fatalf("ConfirmMatch failed: %v", err)
	}
	sender.invocations = 0
	sender.lastText = ""

	// Tick when confirmed: SHOULD send reminder
	mgr.state.LastReminder = "" // clear last reminder
	mgr.tick(context.Background(), tickTime)
	if sender.invocations == 0 {
		t.Fatalf("expected reminder sent when confirmed!")
	}
	if !strings.Contains(sender.lastText, "NHẮC LỊCH ĐÁNH CẦU") {
		t.Errorf("unexpected reminder message: %s", sender.lastText)
	}
}

