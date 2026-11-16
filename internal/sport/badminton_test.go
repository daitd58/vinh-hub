package sport

import (
	"context"
	"strings"
	"testing"
	"time"

	mezon "github.com/quangledang23/mezon-sdk-go"
)

type mockSender struct {
	lastChannel  string
	lastText     string
	lastOptions  *mezon.SendOptions
	sendErr      error
	invocations  int
}

func (m *mockSender) SendToChannel(ctx context.Context, channelID string, text string, opts *mezon.SendOptions) error {
	m.lastChannel = channelID
	m.lastText = text
	m.lastOptions = opts
	m.invocations++
	return m.sendErr
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
