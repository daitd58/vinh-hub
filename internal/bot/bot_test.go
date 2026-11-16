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
