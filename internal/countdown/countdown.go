package countdown

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

// Animation Component Type in Mezon SDK
const ComponentTypeAnimation = 6

// Asset URLs
const (
	PositionURL       = "https://cdn.mezon.ai/1840653661416460288/1958044376819044352/1803263641638670300/1755681221532_countdown.json"
	TetDuongImageURL  = "https://cdn.mezon.ai/1840653661416460288/1958044376819044352/1803263641638670300/1755873430243_Frame_7__1_.png"
	LuongImageURL     = "https://cdn.mezon.ai/1840653661416460288/1958044376819044352/1803263641638670300/1755682031237_Frame_5.png"
	TetAmImageURL     = "https://cdn.mezon.ai/1803263641638670336/2008476669140013056.png"
	GiangSinhImageURL = "https://cdn.mezon.ai/1803263641638670336/2003390231352446976.png"
	TrungThuImageURL  = "https://cdn.mezon.ai/1840653661416460288/1958044376819044352/1803263641638670300/1757837228088_trung_thu.png"
	YepImageURL       = "https://cdn.mezon.ai/1803263641638670336/2008491659402153984.png"
)

// ImageItems is the standard 11-sprite list (0-9 digits and happy icon at index 10).
var ImageItems = []string{
	"1.png", "2.png", "3.png", "4.png", "5.png",
	"6.png", "7.png", "8.png", "9.png", "10.png", "11.png",
}

// SalaryImageItems is the 12-sprite list (0-9 digits, hourglass at 10, dollar at 11).
var SalaryImageItems = []string{
	"1.png", "2.png", "3.png", "4.png", "5.png",
	"6.png", "7.png", "8.png", "9.png", "10.png", "11.png", "12.png",
}

// Fixed Lunar New Year calendar dates (2024 to 2043)
var lunarNewYearDates = map[int]string{
	2024: "2024-02-10",
	2025: "2025-01-29",
	2026: "2026-02-17",
	2027: "2027-02-06",
	2028: "2028-01-26",
	2029: "2029-02-13",
	2030: "2030-02-03",
	2031: "2031-01-23",
	2032: "2032-02-11",
	2033: "2033-01-31",
	2034: "2034-02-19",
	2035: "2035-02-08",
	2036: "2036-01-28",
	2037: "2037-02-15",
	2038: "2038-02-04",
	2039: "2039-01-24",
	2040: "2040-02-12",
	2041: "2041-02-01",
	2042: "2042-01-22",
	2043: "2043-02-10",
}

// Fixed Mid-Autumn festival calendar dates (2024 to 2043)
var midAutumnDates = map[int]string{
	2024: "2024-09-17",
	2025: "2025-10-06",
	2026: "2026-09-25",
	2027: "2027-09-15",
	2028: "2028-10-03",
	2029: "2029-09-22",
	2030: "2030-09-12",
	2031: "2031-10-01",
	2032: "2032-09-20",
	2033: "2033-10-09",
	2034: "2034-09-28",
	2035: "2035-09-17",
	2036: "2036-10-05",
	2037: "2037-09-25",
	2038: "2038-09-15",
	2039: "2039-10-04",
	2040: "2040-09-22",
	2041: "2041-10-11",
	2042: "2042-09-30",
	2043: "2043-09-20",
}

// NCC Year End Party calendar dates
var yepDates = map[int]string{
	2025: "2026-01-31",
	2026: "2026-12-31",
	2027: "2027-12-31",
}

var yepDesc = map[int]string{
	2025: "Thời gian: Thứ 7, ngày 31/01/2026\nĐối tượng: Toàn thể nhân viên NCC (Staff và Intern)\nThông tin chi tiết: ims.nccsoft.vn/information/events/836\nLink đăng ký tham gia YEP 2025: forms.gle/K2fYJWVYdpSHpwHq5",
}

var embedColors = []string{
	"#1ABC9C", // Aqua
	"#11806A", // DarkAqua
	"#57F287", // Green
	"#1F8B4C", // DarkGreen
	"#3498DB", // Blue
	"#206694", // DarkBlue
	"#9B59B6", // Purple
	"#71368A", // DarkPurple
	"#E91E63", // LuminousVividPink
	"#AD1457", // DarkVividPink
	"#F1C40F", // Gold
	"#C27C0E", // DarkGold
	"#E67E22", // Orange
	"#A84300", // DarkOrange
	"#ED4245", // Red
	"#992D22", // DarkRed
	"#BCC0C0", // LightGrey
	"#FFFF00", // Yellow
}

// GetRandomColor returns a random hex color code.
func GetRandomColor() string {
	idx := rand.Intn(len(embedColors))
	return embedColors[idx]
}

// buildPool builds the 3-column reels pool ending with the target numbers.
func buildPool(items []string, numbers [3]int) [][]string {
	results := make([][]string, 3)
	for i := 0; i < 3; i++ {
		targetIdx := numbers[i]
		if targetIdx < 0 || targetIdx >= len(items) {
			targetIdx = 0
		}
		col := make([]string, len(items)+1)
		copy(col, items)
		col[len(items)] = items[targetIdx]
		results[i] = col
	}
	return results
}

// buildAnimationEmbed creates the initial embed structure for the animation.
func buildAnimationEmbed(title string, description string, imageURL string, pool [][]string, isResult int) map[string]any {
	component := map[string]any{
		"url_image":    imageURL,
		"url_position": PositionURL,
		"pool":         pool,
		"repeat":       3,
		"duration":     0.35,
		"isResult":     isResult,
	}

	inputs := map[string]any{
		"id":        "slots",
		"type":      ComponentTypeAnimation,
		"component": component,
	}

	field := map[string]any{
		"name":   "",
		"value":  "",
		"inputs": inputs,
	}

	embed := map[string]any{
		"color":  GetRandomColor(),
		"title":  title,
		"fields": []any{field},
	}
	if description != "" {
		embed["description"] = description
	}
	return embed
}

// SetEmbedResult sets isResult = 1 in the embed component so the slot animation stops.
func SetEmbedResult(embed map[string]any) {
	fields, ok := embed["fields"].([]any)
	if !ok || len(fields) == 0 {
		return
	}
	fieldMap, ok := fields[0].(map[string]any)
	if !ok {
		return
	}
	inputs, ok := fieldMap["inputs"].(map[string]any)
	if !ok {
		return
	}
	comp, ok := inputs["component"].(map[string]any)
	if !ok {
		return
	}
	comp["isResult"] = 1
}

// digitsToNumbers converts daysLeft into 3 digits.
func digitsToNumbers(daysLeft int, happyIconIndex int, isToday bool) [3]int {
	if isToday {
		return [3]int{happyIconIndex, happyIconIndex, happyIconIndex}
	}
	if daysLeft >= 100 {
		return [3]int{daysLeft / 100, (daysLeft / 10) % 10, daysLeft % 10}
	} else if daysLeft >= 10 {
		return [3]int{0, daysLeft / 10, daysLeft % 10}
	}
	return [3]int{0, 0, daysLeft}
}

// BuildTetDuongEmbed creates embed for Tết Dương Lịch.
func BuildTetDuongEmbed(now time.Time) map[string]any {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	newYear := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())

	var isToday bool
	var daysLeft int

	if today.Equal(newYear) {
		isToday = true
		daysLeft = 0
	} else {
		if today.After(newYear) {
			newYear = time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, now.Location())
		}
		daysLeft = int(newYear.Sub(today).Hours() / 24)
	}

	happyIconIndex := len(ImageItems) - 1
	var title string
	if isToday {
		title = "🎉 Chúc mừng năm mới!"
	} else {
		title = "⌛️ Đếm ngược đến Tết dương lịch"
	}

	numbers := digitsToNumbers(daysLeft, happyIconIndex, isToday)
	pool := buildPool(ImageItems, numbers)
	return buildAnimationEmbed(title, "", TetDuongImageURL, pool, 0)
}

// BuildTetAmEmbed creates embed for Tết Nguyên Đán.
func BuildTetAmEmbed(now time.Time) map[string]any {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayStr := today.Format("2006-01-02")

	years := make([]int, 0, len(lunarNewYearDates))
	for y := range lunarNewYearDates {
		years = append(years, y)
	}
	sort.Ints(years)

	var isToday bool
	var daysLeft int

	for _, y := range years {
		dStr := lunarNewYearDates[y]
		targetDate, err := time.ParseInLocation("2006-01-02", dStr, now.Location())
		if err != nil {
			continue
		}
		if dStr == todayStr {
			isToday = true
			daysLeft = 0
			break
		}
		if targetDate.After(today) {
			daysLeft = int(targetDate.Sub(today).Hours() / 24)
			break
		}
	}

	happyIconIndex := len(ImageItems) - 1
	var title string
	if isToday {
		title = "🎉 Chúc mừng năm mới!"
	} else {
		title = "⌛️ Đếm ngược đến Tết Nguyên Đán"
	}

	numbers := digitsToNumbers(daysLeft, happyIconIndex, isToday)
	pool := buildPool(ImageItems, numbers)
	return buildAnimationEmbed(title, "", TetAmImageURL, pool, 0)
}

// BuildLuongEmbed creates embed for Ngày nhận lương.
func BuildLuongEmbed(now time.Time, isFinished bool) map[string]any {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	payday := time.Date(now.Year(), now.Month(), 5, 0, 0, 0, 0, now.Location())
	if now.Day() > 5 {
		payday = payday.AddDate(0, 1, 0)
	}

	todayIsPayDate := now.Day() == 5
	daysLeft := int(payday.Sub(today).Hours() / 24)
	if daysLeft < 0 {
		daysLeft = 0
	}

	hourglassIndex := len(SalaryImageItems) - 2
	dollarIndex := len(SalaryImageItems) - 1

	var title string
	var numbers [3]int

	if todayIsPayDate {
		if isFinished {
			title = "⏳ Lương đã về! Đếm làm gì cho mệt anh ơi!"
			numbers = [3]int{dollarIndex, dollarIndex, dollarIndex}
		} else {
			title = "⏳ Lương sắp về rồi... Anh ơi cố lên!"
			numbers = [3]int{hourglassIndex, hourglassIndex, hourglassIndex}
		}
	} else {
		title = "⏳ Cùng đếm ngược bao lâu nữa đến ngày nhận lương nào"
		if daysLeft > 9 {
			numbers = [3]int{hourglassIndex, daysLeft / 10, daysLeft % 10}
		} else {
			numbers = [3]int{hourglassIndex, 0, daysLeft}
		}
	}

	pool := buildPool(SalaryImageItems, numbers)
	return buildAnimationEmbed(title, "", LuongImageURL, pool, 0)
}

// BuildGiangSinhEmbed creates embed for Giáng Sinh (25/12).
func BuildGiangSinhEmbed(now time.Time) map[string]any {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	christmas := time.Date(now.Year(), 12, 25, 0, 0, 0, 0, now.Location())

	var isToday bool
	var daysLeft int

	if today.Equal(christmas) {
		isToday = true
		daysLeft = 0
	} else {
		if today.After(christmas) {
			christmas = time.Date(now.Year()+1, 12, 25, 0, 0, 0, 0, now.Location())
		}
		daysLeft = int(christmas.Sub(today).Hours() / 24)
	}

	happyIconIndex := len(ImageItems) - 1
	var title string
	if isToday {
		title = "🎄 Chúc mừng Giáng Sinh an lành!"
	} else {
		title = "⌛️ Đếm ngược đến Giáng Sinh"
	}

	numbers := digitsToNumbers(daysLeft, happyIconIndex, isToday)
	pool := buildPool(ImageItems, numbers)
	return buildAnimationEmbed(title, "", GiangSinhImageURL, pool, 0)
}

// BuildTrungThuEmbed creates embed for Tết Trung Thu.
func BuildTrungThuEmbed(now time.Time) map[string]any {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayStr := today.Format("2006-01-02")

	years := make([]int, 0, len(midAutumnDates))
	for y := range midAutumnDates {
		years = append(years, y)
	}
	sort.Ints(years)

	var isToday bool
	var daysLeft int

	for _, y := range years {
		dStr := midAutumnDates[y]
		targetDate, err := time.ParseInLocation("2006-01-02", dStr, now.Location())
		if err != nil {
			continue
		}
		if dStr == todayStr {
			isToday = true
			daysLeft = 0
			break
		}
		if targetDate.After(today) {
			daysLeft = int(targetDate.Sub(today).Hours() / 24)
			break
		}
	}

	happyIconIndex := len(ImageItems) - 1
	var title string
	if isToday {
		title = "🥮 Chúc mọi người một Tết Trung Thu vui vẻ và hạnh phúc bên gia đình!"
	} else {
		title = "⌛️ Đếm ngược đến Tết Trung Thu"
	}

	numbers := digitsToNumbers(daysLeft, happyIconIndex, isToday)
	pool := buildPool(ImageItems, numbers)
	return buildAnimationEmbed(title, "", TrungThuImageURL, pool, 0)
}

// BuildYepEmbed creates embed for NCC Year End Party.
func BuildYepEmbed(now time.Time) map[string]any {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayStr := today.Format("2006-01-02")

	years := make([]int, 0, len(yepDates))
	for y := range yepDates {
		years = append(years, y)
	}
	sort.Ints(years)

	var isToday bool
	var daysLeft int
	targetYear := now.Year()

	for _, y := range years {
		dStr := yepDates[y]
		targetDate, err := time.ParseInLocation("2006-01-02", dStr, now.Location())
		if err != nil {
			continue
		}
		if dStr == todayStr {
			isToday = true
			daysLeft = 0
			targetYear = y
			break
		}
		if targetDate.After(today) {
			daysLeft = int(targetDate.Sub(today).Hours() / 24)
			targetYear = y
			break
		}
	}

	if daysLeft == 0 && !isToday {
		targetYear = now.Year()
		targetDate := time.Date(targetYear, 12, 31, 0, 0, 0, 0, now.Location())
		if today.After(targetDate) {
			targetYear++
			targetDate = time.Date(targetYear, 12, 31, 0, 0, 0, 0, now.Location())
		}
		daysLeft = int(targetDate.Sub(today).Hours() / 24)
	}

	happyIconIndex := len(ImageItems) - 1
	var title string
	if isToday {
		title = "🎉 Today is NCC YEP 🎉"
	} else {
		title = fmt.Sprintf("⌛️ Đếm ngược đến ngày YEP %d ⌛️", targetYear)
	}

	description := yepDesc[targetYear]

	numbers := digitsToNumbers(daysLeft, happyIconIndex, isToday)
	pool := buildPool(ImageItems, numbers)
	return buildAnimationEmbed(title, description, YepImageURL, pool, 0)
}
