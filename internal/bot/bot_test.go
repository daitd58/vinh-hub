package bot

import (
	"path/filepath"
	"testing"
	"time"

	mezon "github.com/quangledang23/mezon-sdk-go"
	"vinh-hub/internal/fund"
	"vinh-hub/internal/sport"
)

func TestFormatBalanceReport(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 53, 0, 0, time.UTC)
	bal := fund.Balance{
		BalanceVND: 2_736_348,
		At:         now,
	}

	report := FormatBalanceReport(bal, time.UTC)
	want := "Thông tin Quỹ\n• Quỹ hiện có: 2.736.348 VNĐ\nCập nhật lúc: 09:53 28/09/2026"
	if report != want {
		t.Errorf("FormatBalanceReport() =\n%q\nwant:\n%q", report, want)
	}
}

func TestBotInitValidation(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Error("expected error for empty bot config")
	}

	_, err = New(Config{BotID: "bot1"})
	if err == nil {
		t.Error("expected error without token")
	}
}

func TestParseMarkdownSpans(t *testing.T) {
	text := "Chào **Vinh-CL**! Hẹn gặp lúc **19:00**."
	clean, spans := ParseMarkdownSpans(text)

	wantClean := "Chào Vinh-CL! Hẹn gặp lúc 19:00."
	if clean != wantClean {
		t.Errorf("clean = %q, want %q", clean, wantClean)
	}

	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	if spans[0]["type"] != "b" || spans[1]["type"] != "b" {
		t.Errorf("spans type not 'b'")
	}
}

func TestIsBillFilter(t *testing.T) {
	testCases := []struct {
		input string
		want  bool
	}{
		{"1", true},
		{"2", true},
		{"10", true},
		{"#1", true},
		{"#2", true},
		{"CL1", true},
		{"cl1", true},
		{"CL12", true},
		{"17/11", true},
		{"17-11", true},
		{"17/11/2026", true},
		{"1711", true},
		{"@dai.trinhduc", false},
		{"dai.trinhduc", false},
		{"user1", false},
		{"", false},
	}

	for _, tc := range testCases {
		got := isBillFilter(tc.input)
		if got != tc.want {
			t.Errorf("isBillFilter(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseCommandTagsAndExpr(t *testing.T) {
	// Case 1: *split 360k + 26*9 @dai.trinhduc @nam.nguyen
	expr1, part1 := parseCommandTagsAndExpr("*split 360k + 26*9 @dai.trinhduc @nam.nguyen", "*split", nil, nil)
	if expr1 != "360k + 26*9" {
		t.Errorf("expected expr '360k + 26*9', got %q", expr1)
	}
	if len(part1) != 2 || part1[0].Username != "dai.trinhduc" || part1[0].Slots != 1 || part1[1].Username != "nam.nguyen" || part1[1].Slots != 1 {
		t.Errorf("unexpected participants: %v", part1)
	}

	// Case 2: *split 360k + 26*9 @dai.trinhduc:2 @nam.nguyen:1
	expr2, part2 := parseCommandTagsAndExpr("*split 360k + 26*9 @dai.trinhduc:2 @nam.nguyen:1", "*split", nil, nil)
	if expr2 != "360k + 26*9" {
		t.Errorf("expected expr '360k + 26*9', got %q", expr2)
	}
	if len(part2) != 2 || part2[0].Slots != 2 || part2[1].Slots != 1 {
		t.Errorf("unexpected participants with slot modifiers: %v", part2)
	}

	// Case 3: *split 360k + 26*9 [tag] (literal placeholder ignored -> empty participants -> fallback)
	expr3, part3 := parseCommandTagsAndExpr("*split 360k + 26*9 [tag]", "*split", nil, nil)
	if expr3 != "360k + 26*9" {
		t.Errorf("expected expr '360k + 26*9', got %q", expr3)
	}
	if len(part3) != 0 {
		t.Errorf("expected empty participants for placeholder [tag], got %v", part3)
	}

	// Case 4: *split 360k + 26*9 [@dai.trinhduc, @nam.nguyen]
	expr4, part4 := parseCommandTagsAndExpr("*split 360k + 26*9 [@dai.trinhduc, @nam.nguyen]", "*split", nil, nil)
	if expr4 != "360k + 26*9" {
		t.Errorf("expected expr '360k + 26*9', got %q", expr4)
	}
	if len(part4) != 2 || part4[0].Username != "dai.trinhduc" || part4[1].Username != "nam.nguyen" {
		t.Errorf("unexpected participants for bracketed tags: %v", part4)
	}

	// Case 5: *money 45k @dai.trinhduc:2 @viet.le
	expr5, part5 := parseCommandTagsAndExpr("*money 45k @dai.trinhduc:2 @viet.le", "*money", nil, nil)
	if expr5 != "45k" {
		t.Errorf("expected expr '45k', got %q", expr5)
	}
	if len(part5) != 2 || part5[0].Slots != 2 || part5[1].Slots != 1 {
		t.Errorf("unexpected participants: %v", part5)
	}
}

func TestParseMonthYear(t *testing.T) {
	refTime := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		input     string
		wantMonth int
		wantYear  int
		wantErr   bool
	}{
		{"", 11, 2026, false},
		{"11/2026", 11, 2026, false},
		{"10/2026", 10, 2026, false},
		{"10-2026", 10, 2026, false},
		{"10.2026", 10, 2026, false},
		{"10/26", 10, 2026, false},
		{"10", 10, 2026, false},
		{"1", 1, 2026, false},
		{"12", 12, 2026, false},
		{"0", 0, 0, true},
		{"13", 0, 0, true},
		{"13/2026", 0, 0, true},
		{"invalid", 0, 0, true},
		{"11/12/2026", 0, 0, true},
	}

	for _, tc := range tests {
		m, y, err := parseMonthYear(tc.input, refTime)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseMonthYear(%q) err = %v, wantErr = %v", tc.input, err, tc.wantErr)
		}
		if !tc.wantErr {
			if m != tc.wantMonth || y != tc.wantYear {
				t.Errorf("parseMonthYear(%q) = (%d, %d), want (%d, %d)", tc.input, m, y, tc.wantMonth, tc.wantYear)
			}
		}
	}
}

func TestHandlePaidCommand_SelfConfirm(t *testing.T) {
	tmpDir := t.TempDir()
	sportMgr := sport.NewManager(sport.Config{
		ChannelID: "sport-chan",
		StatePath: filepath.Join(tmpDir, "badminton.json"),
	}, nil)

	parts := []sport.Participant{
		{UserID: "u1", Username: "son.nguyentruong1", DisplayName: "son.nguyentruong1", Slots: 1},
		{UserID: "u2", Username: "dai.trinhduc", DisplayName: "dai.trinhduc", Slots: 1},
	}
	_, err := sportMgr.CreateMoneyBill(45000, parts...)
	if err != nil {
		t.Fatalf("CreateMoneyBill failed: %v", err)
	}

	b := &Bot{
		cfg:      Config{SportChannelID: "sport-chan"},
		sportMgr: sportMgr,
	}

	// 1. Message from son.nguyentruong1: `*paid 1`
	m := &mezon.ChannelMessage{
		ChannelID: "sport-chan",
		SenderID:  "u1",
		Username:  "son.nguyentruong1",
	}
	b.handlePaidCommand(m, "*paid 1")

	// Verify bill state: son.nguyentruong1 is now marked paid!
	_, paid, unpaid, totalPaid, _, err := sportMgr.GetBillStatus("1")
	if err != nil {
		t.Fatalf("GetBillStatus failed: %v", err)
	}
	if len(paid) != 1 || paid[0].Username != "son.nguyentruong1" {
		t.Errorf("expected son.nguyentruong1 to be paid, got: %v", paid)
	}
	if len(unpaid) != 1 || unpaid[0].Username != "dai.trinhduc" {
		t.Errorf("expected dai.trinhduc to remain unpaid, got: %v", unpaid)
	}
	if totalPaid != 45000 {
		t.Errorf("expected totalPaid 45k, got %d", totalPaid)
	}

	// 2. Typing `*paid 1` again when already paid should keep state intact
	b.handlePaidCommand(m, "*paid 1")
	_, paid2, _, _, _, _ := sportMgr.GetBillStatus("1")
	if len(paid2) != 1 {
		t.Errorf("expected still 1 paid, got %d", len(paid2))
	}

	// 3. Admin / stranger typing `*paid 1`: should not mark anyone paid
	mStranger := &mezon.ChannelMessage{
		ChannelID: "sport-chan",
		SenderID:  "stranger_id",
		Username:  "stranger",
	}
	b.handlePaidCommand(mStranger, "*paid 1")
	_, paid3, _, _, _, _ := sportMgr.GetBillStatus("1")
	if len(paid3) != 1 {
		t.Errorf("stranger should not mark anyone paid, got %d", len(paid3))
	}
}

func TestHandleConfirmAndCancelCommand_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	sportMgr := sport.NewManager(sport.Config{
		ChannelID:    "sport-chan",
		AdminUserIDs: []string{"1783440675396653056"},
		StatePath:    filepath.Join(tmpDir, "badminton.json"),
	}, nil)

	b := &Bot{
		cfg:      Config{SportChannelID: "sport-chan"},
		sportMgr: sportMgr,
	}

	// 1. Stranger tries to confirm:
	mStranger := &mezon.ChannelMessage{
		ChannelID: "sport-chan",
		SenderID:  "stranger_id",
		Username:  "stranger",
	}
	b.handleConfirmCommand(mStranger, "*confirm 19:30")
	if sportMgr.IsConfirmed() {
		t.Errorf("stranger should not be able to confirm match")
	}

	// 2. Admin confirms:
	mAdmin := &mezon.ChannelMessage{
		ChannelID: "sport-chan",
		SenderID:  "1783440675396653056",
		Username:  "dai.trinhduc",
		ClanNick:  "dai.trinhduc",
	}
	b.handleConfirmCommand(mAdmin, "*confirm t4 19:30 san 3")
	if !sportMgr.IsConfirmed() {
		t.Errorf("admin should be able to confirm match")
	}

	// 3. Stranger tries to cancel:
	b.handleCancelCommand(mStranger)
	if !sportMgr.IsConfirmed() {
		t.Errorf("stranger should not be able to cancel match")
	}

	// 4. Admin cancels:
	b.handleCancelCommand(mAdmin)
	if sportMgr.IsConfirmed() {
		t.Errorf("admin cancel should set confirmed to false")
	}
}
