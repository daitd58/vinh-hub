package fund

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// TxRecord represents a transaction retrieved from the indexer API.
type TxRecord struct {
	Hash        string    `json:"hash"`
	FromAddress string    `json:"from_address"`
	ToAddress   string    `json:"to_address"`
	Value       string    `json:"value"`
	Timestamp   time.Time `json:"-"`
	TxTimestamp int64     `json:"transaction_timestamp"`
	TextData    string    `json:"text_data"`
	Status      int       `json:"status"`
}

type indexerTxResponse struct {
	Meta struct {
		ChainID    int    `json:"chain_id"`
		Page       int    `json:"page"`
		Limit      int    `json:"limit"`
		TotalItems int    `json:"total_items"`
		TotalPages int    `json:"total_pages"`
	} `json:"meta"`
	Data []TxRecord `json:"data"`
}

// RecentTransactions fetches up to limit recent transactions for a wallet.
func (i *Indexer) RecentTransactions(ctx context.Context, wallet string, limit int) ([]TxRecord, error) {
	if i == nil || wallet == "" {
		return nil, ErrNotConfigured
	}
	if limit <= 0 {
		limit = 5
	}
	httpClient := i.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	// Try infinite transactions endpoint first
	endpoint := fmt.Sprintf("%s/%s/transactions/infinite?wallet_address=%s&limit=%d",
		i.BaseURL, url.PathEscape(i.ChainID), url.QueryEscape(wallet), limit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build recent tx request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var body indexerTxResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			for idx := range body.Data {
				if body.Data[idx].TxTimestamp > 0 {
					body.Data[idx].Timestamp = time.Unix(body.Data[idx].TxTimestamp, 0)
				}
			}
			return body.Data, nil
		}
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Fallback to standard transactions endpoint
	endpointFallback := fmt.Sprintf("%s/%s/transactions?wallet_address=%s&limit=%d",
		i.BaseURL, url.PathEscape(i.ChainID), url.QueryEscape(wallet), limit)

	reqFallback, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointFallback, nil)
	if err != nil {
		return nil, fmt.Errorf("build recent tx fallback request: %w", err)
	}
	respFallback, err := httpClient.Do(reqFallback)
	if err != nil {
		return nil, fmt.Errorf("fetch recent tx: %w", err)
	}
	defer respFallback.Body.Close()

	if respFallback.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer returned %s for recent transactions", respFallback.Status)
	}

	var bodyFallback indexerTxResponse
	if err := json.NewDecoder(respFallback.Body).Decode(&bodyFallback); err != nil {
		return nil, fmt.Errorf("decode recent tx: %w", err)
	}

	for idx := range bodyFallback.Data {
		if bodyFallback.Data[idx].TxTimestamp > 0 {
			bodyFallback.Data[idx].Timestamp = time.Unix(bodyFallback.Data[idx].TxTimestamp, 0)
		}
	}
	return bodyFallback.Data, nil
}

// MonthlyTransactions fetches all transactions for a wallet in a given month and year.
func (i *Indexer) MonthlyTransactions(ctx context.Context, wallet string, year int, month int) ([]TxRecord, error) {
	if i == nil || wallet == "" {
		return nil, ErrNotConfigured
	}
	if month < 1 || month > 12 || year < 2000 {
		return nil, fmt.Errorf("thời gian không hợp lệ: %02d/%04d", month, year)
	}

	httpClient := i.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	firstOfMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	lastOfMonth := firstOfMonth.AddDate(0, 1, -1)
	startTime := fmt.Sprintf("%04d-%02d-01", year, month)
	endTime := fmt.Sprintf("%04d-%02d-%02d", year, month, lastOfMonth.Day())

	var allTxs []TxRecord
	page := 0
	limit := 100

	for {
		endpoint := fmt.Sprintf("%s/%s/transactions?wallet_address=%s&start_time=%s&end_time=%s&limit=%d&page=%d",
			i.BaseURL, url.PathEscape(i.ChainID), url.QueryEscape(wallet), startTime, endTime, limit, page)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build monthly tx request: %w", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch monthly tx page %d: %w", page, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("indexer returned %s for monthly transactions", resp.Status)
		}

		var body indexerTxResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode monthly tx page %d: %w", page, decodeErr)
		}

		for idx := range body.Data {
			if body.Data[idx].TxTimestamp > 0 {
				body.Data[idx].Timestamp = time.Unix(body.Data[idx].TxTimestamp, 0)
			}
			allTxs = append(allTxs, body.Data[idx])
		}

		page++
		if page >= body.Meta.TotalPages || len(body.Data) == 0 || page > 10 {
			break
		}
	}

	return allTxs, nil
}

func cleanTransferContent(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "transfer funds - ") {
		return strings.TrimSpace(s[len("transfer funds - "):])
	}
	if lower == "transfer funds" {
		return ""
	}
	return s
}

// ResolveTxUser determines the display username or address, and clean content for a transaction.
func ResolveTxUser(tx TxRecord, fundWallet string, wMap *UserWalletMap) (user string, content string, isIncoming bool) {
	isIncoming = strings.EqualFold(tx.ToAddress, fundWallet)
	text := strings.TrimSpace(tx.TextData)

	if isIncoming {
		if strings.Contains(text, " transfer via bot: ") {
			parts := strings.SplitN(text, " transfer via bot: ", 2)
			user = strings.TrimSpace(parts[0])
			content = cleanTransferContent(parts[1])
		} else if strings.Contains(text, " - ") {
			parts := strings.SplitN(text, " - ", 2)
			candidate := strings.TrimSpace(parts[0])
			if !strings.EqualFold(candidate, "transfer funds") {
				if wMap != nil {
					if _, ok := wMap.LookupWallet(candidate); ok {
						user = candidate
						content = strings.TrimSpace(parts[1])
					} else if u, ok := wMap.LookupUser(tx.FromAddress); ok {
						user = u
						content = strings.TrimSpace(parts[1])
					}
				}
				if user == "" {
					user = candidate
					content = strings.TrimSpace(parts[1])
				}
			} else {
				content = cleanTransferContent(parts[1])
			}
		}

		if user == "" && wMap != nil {
			if u, ok := wMap.LookupUser(tx.FromAddress); ok {
				user = u
			}
		}
		if user == "" {
			if len(tx.FromAddress) > 12 {
				user = tx.FromAddress[:6] + "..." + tx.FromAddress[len(tx.FromAddress)-4:]
			} else {
				user = tx.FromAddress
			}
		}
		if content == "" {
			content = cleanTransferContent(text)
		}
	} else {
		// Outgoing
		if wMap != nil {
			if u, ok := wMap.LookupUser(tx.ToAddress); ok {
				user = u
			}
		}
		if user == "" {
			if len(tx.ToAddress) > 12 {
				user = tx.ToAddress[:6] + "..." + tx.ToAddress[len(tx.ToAddress)-4:]
			} else {
				user = tx.ToAddress
			}
		}
		content = text
	}

	return user, content, isIncoming
}

// FormatRecentHistory renders the 5 most recent fund movements in a clean markdown format.
func FormatRecentHistory(txs []TxRecord, fundWallet string, wMap *UserWalletMap, decimals int, loc *time.Location, curBalance *int64) string {
	if loc == nil {
		loc = time.Local
	}
	var sb strings.Builder
	sb.WriteString("📋 **LỊCH SỬ 5 BIẾN ĐỘNG QUỸ GẦN NHẤT**\n\n")

	if len(txs) == 0 {
		sb.WriteString("*(Chưa có giao dịch nào gần đây)*\n")
	} else {
		for i, tx := range txs {
			if i >= 5 {
				break
			}
			user, content, incoming := ResolveTxUser(tx, fundWallet, wMap)
			userDisplay := user
			if !strings.Contains(userDisplay, "...") {
				userDisplay = "@" + userDisplay
			}
			raw, _ := new(big.Int).SetString(tx.Value, 10)
			vnd := ToVND(raw, decimals)

			prefix := "🟢 **+"
			typeLabel := "Nhận từ"
			if !incoming {
				prefix = "🔴 **-"
				typeLabel = "Chuyển tới"
			}

			shortHash := tx.Hash
			if len(shortHash) > 12 {
				shortHash = shortHash[:8] + "..." + shortHash[len(shortHash)-4:]
			}

			tStr := tx.Timestamp.In(loc).Format("15:04 02/01/2006")

			sb.WriteString(fmt.Sprintf("%d. %s%s VNĐ** (%s %s)\n", i+1, prefix, FormatVND(vnd), typeLabel, userDisplay))
			sb.WriteString(fmt.Sprintf("   • ⏰ Thời gian: `%s`\n", tStr))
			if content != "" {
				sb.WriteString(fmt.Sprintf("   • 💬 Nội dung: `%s`\n", content))
			}
			sb.WriteString(fmt.Sprintf("   • 🔗 Hash: `%s`\n\n", shortHash))
		}
	}

	if curBalance != nil {
		sb.WriteString(fmt.Sprintf("💰 **Số dư quỹ hiện tại:** **%s VNĐ**", FormatVND(*curBalance)))
	}

	return sb.String()
}

// UserSpending aggregates total spent and count of purchases for a member in a month.
type UserSpending struct {
	Username string
	TotalVND int64
	Count    int
}

// GenerateMonthlyRanking aggregates transactions into a monthly spending leaderboard.
func GenerateMonthlyRanking(txs []TxRecord, fundWallet string, wMap *UserWalletMap, decimals int, year, month int) (string, error) {
	spenders := make(map[string]*UserSpending)
	var totalMonthVND int64
	var totalTxCount int

	for _, tx := range txs {
		if !strings.EqualFold(tx.ToAddress, fundWallet) {
			continue
		}
		raw, ok := new(big.Int).SetString(tx.Value, 10)
		if !ok || raw.Sign() <= 0 {
			continue
		}
		vnd := ToVND(raw, decimals)
		user, _, _ := ResolveTxUser(tx, fundWallet, wMap)

		if s, ok := spenders[user]; ok {
			s.TotalVND += vnd
			s.Count++
		} else {
			spenders[user] = &UserSpending{
				Username: user,
				TotalVND: vnd,
				Count:    1,
			}
		}
		totalMonthVND += vnd
		totalTxCount++
	}

	if len(spenders) == 0 {
		return fmt.Sprintf("⚠️ Không có giao dịch chi tiêu nào trong tháng %02d/%04d.", month, year), nil
	}

	var sortedSpenders []*UserSpending
	for _, s := range spenders {
		sortedSpenders = append(sortedSpenders, s)
	}
	sort.Slice(sortedSpenders, func(i, j int) bool {
		if sortedSpenders[i].TotalVND == sortedSpenders[j].TotalVND {
			return sortedSpenders[i].Count > sortedSpenders[j].Count
		}
		return sortedSpenders[i].TotalVND > sortedSpenders[j].TotalVND
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🏆 **BẢNG XẾP HẠNG CHI TIÊU VINHCORNER - THÁNG %02d/%04d** 🛒✨\n\n", month, year))

	for i, s := range sortedSpenders {
		medal := fmt.Sprintf("%d.", i+1)
		switch i {
		case 0:
			medal = "🥇 **1.**"
		case 1:
			medal = "🥈 **2.**"
		case 2:
			medal = "🥉 **3.**"
		}
		userDisplay := s.Username
		if !strings.Contains(userDisplay, "...") {
			userDisplay = "@" + userDisplay
		}
		sb.WriteString(fmt.Sprintf("%s %s: **%s VNĐ** *(%d lượt)*\n",
			medal, userDisplay, FormatVND(s.TotalVND), s.Count))
	}

	sb.WriteString("\n━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("📊 **Tổng kết tháng %02d/%04d:**\n", month, year))
	sb.WriteString(fmt.Sprintf("• 💰 Tổng tiền thu: **%s VNĐ**\n", FormatVND(totalMonthVND)))
	sb.WriteString(fmt.Sprintf("• 🧾 Tổng số giao dịch: **%d lượt**\n", totalTxCount))
	sb.WriteString(fmt.Sprintf("• 👥 Số người chi tiêu: **%d người**", len(spenders)))

	return sb.String(), nil
}
