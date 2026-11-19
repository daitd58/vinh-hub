package countdown

import (
	"testing"
	"time"
)

func TestCountdownBuilders(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, loc)

	t.Run("TetDuong", func(t *testing.T) {
		embed := BuildTetDuongEmbed(now)
		if embed["title"] != "⌛️ Đếm ngược đến Tết dương lịch" {
			t.Errorf("unexpected title: %v", embed["title"])
		}
		fields, ok := embed["fields"].([]any)
		if !ok || len(fields) != 1 {
			t.Fatalf("expected 1 field, got %v", embed["fields"])
		}
		field := fields[0].(map[string]any)
		inputs := field["inputs"].(map[string]any)
		if inputs["type"] != ComponentTypeAnimation {
			t.Errorf("expected type %d, got %v", ComponentTypeAnimation, inputs["type"])
		}
		comp := inputs["component"].(map[string]any)
		if comp["url_image"] != TetDuongImageURL {
			t.Errorf("expected url_image %s, got %v", TetDuongImageURL, comp["url_image"])
		}
		pool, ok := comp["pool"].([][]string)
		if !ok || len(pool) != 3 {
			t.Fatalf("expected 3 pool columns, got %v", comp["pool"])
		}
		if comp["isResult"] != 0 {
			t.Errorf("expected isResult 0, got %v", comp["isResult"])
		}

		SetEmbedResult(embed)
		if comp["isResult"] != 1 {
			t.Errorf("expected isResult 1 after SetEmbedResult, got %v", comp["isResult"])
		}
	})

	t.Run("TetDuong_NewYearDay", func(t *testing.T) {
		ny := time.Date(2027, 1, 1, 0, 0, 0, 0, loc)
		embed := BuildTetDuongEmbed(ny)
		if embed["title"] != "🎉 Chúc mừng năm mới!" {
			t.Errorf("unexpected title: %v", embed["title"])
		}
	})

	t.Run("TetAm", func(t *testing.T) {
		embed := BuildTetAmEmbed(now)
		if embed["title"] != "⌛️ Đếm ngược đến Tết Nguyên Đán" {
			t.Errorf("unexpected title: %v", embed["title"])
		}
	})

	t.Run("Luong", func(t *testing.T) {
		embed := BuildLuongEmbed(now, false)
		if embed["title"] != "⏳ Cùng đếm ngược bao lâu nữa đến ngày nhận lương nào" {
			t.Errorf("unexpected title: %v", embed["title"])
		}

		// Payday test
		payday := time.Date(2026, 12, 5, 10, 0, 0, 0, loc)
		embedPayday := BuildLuongEmbed(payday, false)
		if embedPayday["title"] != "⏳ Lương sắp về rồi... Anh ơi cố lên!" {
			t.Errorf("unexpected title: %v", embedPayday["title"])
		}

		embedFinished := BuildLuongEmbed(payday, true)
		if embedFinished["title"] != "⏳ Lương đã về! Đếm làm gì cho mệt anh ơi!" {
			t.Errorf("unexpected title: %v", embedFinished["title"])
		}
	})

	t.Run("GiangSinh", func(t *testing.T) {
		embed := BuildGiangSinhEmbed(now)
		if embed["title"] != "⌛️ Đếm ngược đến Giáng Sinh" {
			t.Errorf("unexpected title: %v", embed["title"])
		}
	})

	t.Run("TrungThu", func(t *testing.T) {
		embed := BuildTrungThuEmbed(now)
		if embed["title"] != "⌛️ Đếm ngược đến Tết Trung Thu" {
			t.Errorf("unexpected title: %v", embed["title"])
		}
	})

	t.Run("Yep", func(t *testing.T) {
		embed := BuildYepEmbed(now)
		if embed["title"] != "⌛️ Đếm ngược đến ngày YEP 2026 ⌛️" {
			t.Errorf("unexpected title: %v", embed["title"])
		}
	})
}
