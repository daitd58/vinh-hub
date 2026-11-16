package fund

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIndexerRecentTransactions(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/transactions/infinite") {
			resp := indexerTxResponse{
				Data: []TxRecord{
					{
						Hash:        "0xabc123456789def0",
						FromAddress: "addr_sender_1",
						ToAddress:   "addr_fund",
						Value:       "50000000000",
						TxTimestamp: 1731744000,
						TextData:    "alice transfer via bot: Snack",
						Status:      1,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	indexer := NewIndexer(ts.URL, "1337", 6, ts.Client())

	ctx := context.Background()
	txs, err := indexer.RecentTransactions(ctx, "addr_fund", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}
	if txs[0].Hash != "0xabc123456789def0" {
		t.Errorf("expected hash 0xabc123456789def0, got %s", txs[0].Hash)
	}
	if txs[0].Timestamp.Unix() != 1731744000 {
		t.Errorf("expected timestamp 1731744000, got %d", txs[0].Timestamp.Unix())
	}
}

func TestIndexerRecentTransactions_Fallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/transactions/infinite") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "/transactions") {
			resp := indexerTxResponse{
				Data: []TxRecord{
					{
						Hash:        "0xfallback12345678",
						FromAddress: "addr_sender_fallback",
						ToAddress:   "addr_fund",
						Value:       "30000000000",
						TxTimestamp: 1731745000,
						TextData:    "bob - nước ngọt",
						Status:      1,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer ts.Close()

	indexer := NewIndexer(ts.URL, "1337", 6, ts.Client())
	txs, err := indexer.RecentTransactions(context.Background(), "addr_fund", 5)
	if err != nil {
		t.Fatalf("unexpected error on fallback: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 tx, got %d", len(txs))
	}
	if txs[0].Hash != "0xfallback12345678" {
		t.Errorf("expected hash 0xfallback12345678, got %s", txs[0].Hash)
	}
}

func TestIndexerMonthlyTransactions(t *testing.T) {
	pageCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pageCount++
		query := r.URL.Query()
		page := query.Get("page")

		var resp indexerTxResponse
		resp.Meta.TotalPages = 2
		resp.Meta.TotalItems = 3

		if page == "0" {
			resp.Meta.Page = 0
			resp.Data = []TxRecord{
				{
					Hash:        "0x111",
					FromAddress: "addr1",
					ToAddress:   "addr_fund",
					Value:       "10000000000",
					TxTimestamp: 1731740000,
					TextData:    "user1 transfer via bot: Item A",
				},
				{
					Hash:        "0x222",
					FromAddress: "addr2",
					ToAddress:   "addr_fund",
					Value:       "20000000000",
					TxTimestamp: 1731741000,
					TextData:    "user2 - Item B",
				},
			}
		} else {
			resp.Meta.Page = 1
			resp.Data = []TxRecord{
				{
					Hash:        "0x333",
					FromAddress: "addr1",
					ToAddress:   "addr_fund",
					Value:       "15000000000",
					TxTimestamp: 1731742000,
					TextData:    "user1 transfer via bot: Item C",
				},
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	indexer := NewIndexer(ts.URL, "1337", 6, ts.Client())
	txs, err := indexer.MonthlyTransactions(context.Background(), "addr_fund", 2026, 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txs) != 3 {
		t.Fatalf("expected 3 transactions across 2 pages, got %d", len(txs))
	}
	if pageCount != 2 {
		t.Errorf("expected 2 page requests, got %d", pageCount)
	}
}

func TestResolveTxUser(t *testing.T) {
	fundWallet := "DbAF8GFuEyLZ6trdhcJESi8hovowCiUASC2tW2LEZ7v7"
	wMap := NewUserWalletMap(map[string]string{
		"chinh.nguyenduc": "4N1mKjV2L2H22P88H6N8x8H71r5hG6z3N5f2C1y9Q4b1",
		"hoang.nguyen":    "2aB3cDeFgHiJkLmNoPqRsTuVwXyZ1234567890abcdef",
	})

	// Case 1: Bot forward
	tx1 := TxRecord{
		FromAddress: "4N1mKjV2L2H22P88H6N8x8H71r5hG6z3N5f2C1y9Q4b1",
		ToAddress:   fundWallet,
		TextData:    "chinh.nguyenduc transfer via bot: Sting dâu",
	}
	u1, c1, in1 := ResolveTxUser(tx1, fundWallet, wMap)
	if !in1 || u1 != "chinh.nguyenduc" || c1 != "Sting dâu" {
		t.Errorf("Case 1 failed: u=%s, c=%s, in=%v", u1, c1, in1)
	}

	// Case 2: Dash format with known user
	tx2 := TxRecord{
		FromAddress: "2aB3cDeFgHiJkLmNoPqRsTuVwXyZ1234567890abcdef",
		ToAddress:   fundWallet,
		TextData:    "hoang.nguyen - 2 xúc xích, 1 mì tôm",
	}
	u2, c2, in2 := ResolveTxUser(tx2, fundWallet, wMap)
	if !in2 || u2 != "hoang.nguyen" || c2 != "2 xúc xích, 1 mì tôm" {
		t.Errorf("Case 2 failed: u=%s, c=%s, in=%v", u2, c2, in2)
	}

	// Case 3: Wallet map fallback when textData is just note
	tx3 := TxRecord{
		FromAddress: "4N1mKjV2L2H22P88H6N8x8H71r5hG6z3N5f2C1y9Q4b1",
		ToAddress:   fundWallet,
		TextData:    "1 sting",
	}
	u3, c3, in3 := ResolveTxUser(tx3, fundWallet, wMap)
	if !in3 || u3 != "chinh.nguyenduc" || c3 != "1 sting" {
		t.Errorf("Case 3 failed: u=%s, c=%s, in=%v", u3, c3, in3)
	}

	// Case 4: Unknown sender address (shortened)
	tx4 := TxRecord{
		FromAddress: "1111222233334444555566667777888899990000aaaa",
		ToAddress:   fundWallet,
		TextData:    "chuyển tiền",
	}
	u4, c4, in4 := ResolveTxUser(tx4, fundWallet, wMap)
	if !in4 || u4 != "111122...aaaa" || c4 != "chuyển tiền" {
		t.Errorf("Case 4 failed: u=%s, c=%s, in=%v", u4, c4, in4)
	}

	// Case 5: Outgoing transaction from fund
	tx5 := TxRecord{
		FromAddress: fundWallet,
		ToAddress:   "2aB3cDeFgHiJkLmNoPqRsTuVwXyZ1234567890abcdef",
		TextData:    "hoàn tiền",
	}
	u5, c5, in5 := ResolveTxUser(tx5, fundWallet, wMap)
	if in5 || u5 != "hoang.nguyen" || c5 != "hoàn tiền" {
		t.Errorf("Case 5 failed: u=%s, c=%s, in=%v", u5, c5, in5)
	}
}

func TestFormatRecentHistory(t *testing.T) {
	fundWallet := "addr_fund"
	txs := []TxRecord{
		{
			Hash:        "0x1234567890abcdef1234",
			FromAddress: "addr_sender_alice",
			ToAddress:   fundWallet,
			Value:       "45000000000",
			Timestamp:   time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC),
			TextData:    "alice transfer via bot: Bò húc",
		},
		{
			Hash:        "0xabcdef1234567890fedc",
			FromAddress: fundWallet,
			ToAddress:   "addr_bob",
			Value:       "10000000000",
			Timestamp:   time.Date(2026, 11, 16, 9, 15, 0, 0, time.UTC),
			TextData:    "refund",
		},
	}

	curBal := int64(1250000)
	report := FormatRecentHistory(txs, fundWallet, nil, 6, time.UTC, &curBal)

	if !strings.Contains(report, "LỊCH SỬ 5 BIẾN ĐỘNG QUỸ GẦN NHẤT") {
		t.Errorf("missing header in report: %s", report)
	}
	if !strings.Contains(report, "🟢 **+45.000 VNĐ** (Nhận từ @alice)") {
		t.Errorf("missing incoming tx in report: %s", report)
	}
	if !strings.Contains(report, "🔴 **-10.000 VNĐ** (Chuyển tới") {
		t.Errorf("missing outgoing tx in report: %s", report)
	}
	if !strings.Contains(report, "**Số dư quỹ hiện tại:** **1.250.000 VNĐ**") {
		t.Errorf("missing balance in report: %s", report)
	}
}

func TestGenerateMonthlyRanking(t *testing.T) {
	fundWallet := "addr_fund"
	txs := []TxRecord{
		{
			FromAddress: "addr_alice",
			ToAddress:   fundWallet,
			Value:       "50000000000", // 50k
			TextData:    "alice transfer via bot: Bò húc",
		},
		{
			FromAddress: "addr_bob",
			ToAddress:   fundWallet,
			Value:       "100000000000", // 100k
			TextData:    "bob transfer via bot: Bánh",
		},
		{
			FromAddress: "addr_alice",
			ToAddress:   fundWallet,
			Value:       "70000000000", // 70k -> total Alice = 120k
			TextData:    "alice transfer via bot: Trà",
		},
	}

	report, err := GenerateMonthlyRanking(txs, fundWallet, nil, 6, 2026, 11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Alice: 120k (2 txs) -> 1st
	// Bob: 100k (1 tx) -> 2nd
	if !strings.Contains(report, "BẢNG XẾP HẠNG CHI TIÊU VINHCORNER - THÁNG 11/2026") {
		t.Errorf("missing header: %s", report)
	}
	if !strings.Contains(report, "🥇 **1.** @alice: **120.000 VNĐ** *(2 lượt)*") {
		t.Errorf("Alice should be 1st: %s", report)
	}
	if !strings.Contains(report, "🥈 **2.** @bob: **100.000 VNĐ** *(1 lượt)*") {
		t.Errorf("Bob should be 2nd: %s", report)
	}
	if !strings.Contains(report, "Tổng tiền thu: **220.000 VNĐ**") {
		t.Errorf("Total VND incorrect: %s", report)
	}
	if !strings.Contains(report, "Tổng số giao dịch: **3 lượt**") {
		t.Errorf("Total count incorrect: %s", report)
	}
	if !strings.Contains(report, "Số người chi tiêu: **2 người**") {
		t.Errorf("Total users incorrect: %s", report)
	}
}
