package bot

import (
	"testing"
	"time"

	"vinh-hub/internal/fund"
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
