package fund

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	mmn "github.com/mezonai/mmn-sdk/go-sdk/client"
	mmnpb "github.com/mezonai/mmn-sdk/go-sdk/proto"

	"vinh-hub/internal/outbox"
)

// Queue is the destination outbox a stream posts to.
type Queue interface {
	Enqueue(ctx context.Context, kind, channelID, threadID, body, dedupeKey string) error
}

// ErrAlreadyQueued reports that a movement has already been recorded in the outbox.
var ErrAlreadyQueued = outbox.ErrAlreadyQueued

// BalanceReader reports what the fund currently holds.
type BalanceReader interface {
	Balance(ctx context.Context) (Balance, error)
}

// Movement is one confirmed transaction involving the fund's wallet.
type Movement struct {
	Hash       string
	Incoming   bool
	Raw        *big.Int
	At         time.Time
	Sender     string
	Recipient  string
	Content    string
	ExtraInfo  string
	BotAddress string
}

// VND converts the raw token amount to whole đồng.
func (m Movement) VND(decimals int) int64 {
	return ToVND(m.Raw, decimals)
}

// Stream monitors the blockchain for wallet activity and enqueues fund updates.
type Stream struct {
	Endpoint   string
	UseTLS     bool
	Address    string
	BotAddress string
	Forwarder  *Forwarder
	ChannelID  string
	ThreadID   string
	Decimals   int
	Queue      Queue
	Log        *slog.Logger
	Balance    BalanceReader
	Wallets    *UserWalletMap
	Silence    time.Duration
	Timezone   *time.Location
	RetryDelay time.Duration
	MaxRetries int

	mu          sync.Mutex
	lastBalance int64
	seen        map[string]time.Time
}

// events is the subset of the gRPC stream client used to receive tx updates.
type events interface {
	Recv() (*mmnpb.TransactionStatusInfo, error)
}

const heartbeatNote = "HeartBeat"
const reconnectDelay = 10 * time.Second
const defaultSilence = 2 * time.Minute

// Run consumes the node's transaction feed and reconnects whenever it ends.
func (s *Stream) Run(ctx context.Context) {
	for {
		if err := s.consume(ctx); err != nil && ctx.Err() == nil {
			s.logger().Warn("fund stream ended; reconnecting",
				"err", err, "in", reconnectDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (s *Stream) consume(ctx context.Context) error {
	client, err := mmn.NewClient(mmn.Config{Endpoint: s.Endpoint, UseTLS: s.UseTLS})
	if err != nil {
		return fmt.Errorf("connect to %s: %w", s.Endpoint, err)
	}
	defer client.Close()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := client.SubscribeTransactionStatus(streamCtx)
	if err != nil {
		return fmt.Errorf("subscribe to %s: %w", s.Endpoint, err)
	}
	s.logger().Info("watching the fund",
		"node", s.Endpoint,
		"channel", s.ChannelID,
		"thread", s.ThreadID,
		"wallet", s.Address)

	return s.read(ctx, stream, cancel)
}

func (s *Stream) read(ctx context.Context, stream events, giveUp func()) error {
	silence := s.Silence
	if silence <= 0 {
		silence = defaultSilence
	}
	quiet := time.AfterFunc(silence, func() {
		s.logger().Warn("fund stream went silent; reconnecting", "after", silence)
		giveUp()
	})
	defer quiet.Stop()

	for {
		event, err := stream.Recv()
		if err != nil {
			return err
		}
		quiet.Reset(silence)
		s.handle(ctx, event)
	}
}

func (s *Stream) handle(ctx context.Context, event *mmnpb.TransactionStatusInfo) {
	if event == nil || event.GetTextData() == heartbeatNote {
		return
	}

	// Only confirmed or finalized transactions moved actual money
	switch event.GetStatus() {
	case mmnpb.TransactionStatus_CONFIRMED, mmnpb.TransactionStatus_FINALIZED:
	default:
		return
	}

	// Case 1: Transaction sent TO the Bot Wallet -> Auto-forward to the Fund Wallet
	if s.BotAddress != "" && event.GetRecipient() == s.BotAddress &&
		event.GetSender() != s.BotAddress && event.GetSender() != s.Address {
		s.handleBotIncoming(ctx, event)
		return
	}

	// Case 2: Transaction involving the Fund Wallet (incoming to or outgoing from Fund)
	m, ok := s.movement(event)
	if !ok {
		return
	}

	if s.isDuplicate(m.Hash) {
		return
	}

	wallets := s.Wallets
	if wallets == nil {
		wallets = DefaultWallets
	}
	body := MovementText(m, s.Decimals, s.Timezone, wallets) + s.balanceLine(ctx, m)
	dedupeKey := "fund:" + m.Hash

	err := s.Queue.Enqueue(ctx, "fund_activity", s.ChannelID, s.ThreadID, body, dedupeKey)
	switch {
	case errors.Is(err, ErrAlreadyQueued):
		// Already queued before; duplicate safely ignored.
	case err != nil:
		s.logger().Warn("could not queue a fund movement", "hash", m.Hash, "err", err)
	default:
		s.logger().Info("fund movement announced",
			"hash", m.Hash, "incoming", m.Incoming, "vnd", m.VND(s.Decimals))
	}
}

func (s *Stream) handleBotIncoming(ctx context.Context, event *mmnpb.TransactionStatusInfo) {
	hash := event.GetTxHash()
	if hash == "" || s.isDuplicate("bot_fwd:"+hash) {
		return
	}

	raw, ok := new(big.Int).SetString(event.GetAmount(), 10)
	if !ok || raw.Sign() <= 0 {
		return
	}

	m := Movement{
		Hash:       hash,
		Incoming:   true,
		Raw:        raw,
		At:         time.Now(),
		Sender:     event.GetSender(),
		Recipient:  event.GetRecipient(),
		Content:    strings.TrimSpace(event.GetTextData()),
		ExtraInfo:  strings.TrimSpace(event.GetExtraInfo()),
		BotAddress: s.BotAddress,
	}
	if ts := event.GetTimestamp(); ts > 0 {
		m.At = time.Unix(int64(ts), 0)
	}

	s.logger().Info("received incoming transfer to bot wallet",
		"sender", m.Sender, "amount_vnd", m.VND(s.Decimals), "content", m.Content, "tx", hash)

	if s.Forwarder == nil {
		s.logger().Warn("bot forwarder is not configured (BOT_PRIVATE_KEY missing); cannot auto-forward to fund",
			"sender", m.Sender, "amount", m.VND(s.Decimals))
		return
	}

	fwdTxHash, err := s.Forwarder.Forward(ctx, m)
	if err != nil {
		s.logger().Error("failed to forward transfer from bot to fund",
			"err", err, "sender", m.Sender, "amount_vnd", m.VND(s.Decimals))
		s.clearDuplicate("bot_fwd:" + hash)
		return
	}

	s.logger().Info("successfully forwarded transfer to fund wallet",
		"fwd_tx_hash", fwdTxHash, "from_user", m.Sender, "amount_vnd", m.VND(s.Decimals))
}

func (s *Stream) clearDuplicate(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.seen, hash)
}

func (s *Stream) isDuplicate(hash string) bool {
	if hash == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = make(map[string]time.Time)
	}
	if _, ok := s.seen[hash]; ok {
		return true
	}
	s.seen[hash] = time.Now()
	if len(s.seen) > 1000 {
		cutoff := time.Now().Add(-24 * time.Hour)
		for h, t := range s.seen {
			if t.Before(cutoff) {
				delete(s.seen, h)
			}
		}
	}
	return false
}

func (s *Stream) movement(e *mmnpb.TransactionStatusInfo) (Movement, bool) {
	if e == nil || e.GetTextData() == heartbeatNote {
		return Movement{}, false
	}

	// Only confirmed or finalized transactions moved actual money
	switch e.GetStatus() {
	case mmnpb.TransactionStatus_CONFIRMED, mmnpb.TransactionStatus_FINALIZED:
	default:
		return Movement{}, false
	}

	var incoming bool
	switch s.Address {
	case e.GetRecipient():
		incoming = true
	case e.GetSender():
		incoming = false
	default:
		return Movement{}, false
	}

	raw, ok := new(big.Int).SetString(e.GetAmount(), 10)
	if !ok || e.GetTxHash() == "" {
		return Movement{}, false
	}

	at := time.Now()
	if ts := e.GetTimestamp(); ts > 0 {
		at = time.Unix(int64(ts), 0)
	}

	return Movement{
		Hash:       e.GetTxHash(),
		Incoming:   incoming,
		Raw:        raw,
		At:         at,
		Sender:     e.GetSender(),
		Recipient:  e.GetRecipient(),
		Content:    strings.TrimSpace(e.GetTextData()),
		ExtraInfo:  strings.TrimSpace(e.GetExtraInfo()),
		BotAddress: s.BotAddress,
	}, true
}

// SetLastBalance sets the last known fund balance.
func (s *Stream) SetLastBalance(bal int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastBalance = bal
}

// LastBalance returns the last known fund balance.
func (s *Stream) LastBalance() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBalance
}

func (s *Stream) maxRetries() int {
	if s.MaxRetries < 0 {
		return 0
	}
	if s.MaxRetries == 0 {
		return 3
	}
	return s.MaxRetries
}

func (s *Stream) retryDelay() time.Duration {
	if s.RetryDelay < 0 {
		return 0
	}
	if s.RetryDelay == 0 {
		return 300 * time.Millisecond
	}
	return s.RetryDelay
}

func (s *Stream) balanceLine(ctx context.Context, m Movement) string {
	if s.Balance == nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	fetch := func() (Balance, error) {
		if fb, ok := s.Balance.(FreshBalancer); ok {
			return fb.FreshBalance(ctx)
		}
		return s.Balance.Balance(ctx)
	}

	b, err := fetch()
	if err != nil && s.lastBalance == 0 {
		s.logger().Warn("could not read the fund balance for the activity post", "err", err)
		return ""
	}

	// If the reading returned the exact same balance as before this movement,
	// the node may still be committing the block. Retry briefly for the updated balance.
	if err == nil && s.lastBalance > 0 && b.BalanceVND == s.lastBalance {
		maxRetries := s.maxRetries()
		delay := s.retryDelay()
		for i := 0; i < maxRetries; i++ {
			select {
			case <-ctx.Done():
				break
			case <-time.After(delay):
			}
			newB, newErr := fetch()
			if newErr == nil {
				b = newB
				if b.BalanceVND != s.lastBalance {
					break
				}
			}
		}
	}

	if err != nil {
		if s.lastBalance > 0 {
			return fmt.Sprintf("\nQuỹ hiện có %s VNĐ", FormatVND(s.lastBalance))
		}
		return ""
	}

	s.lastBalance = b.BalanceVND
	return fmt.Sprintf("\nQuỹ hiện có %s VNĐ", FormatVND(b.BalanceVND))
}

func (s *Stream) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// ForwardInfo contains the extracted user and content for transactions forwarded via bot.
type ForwardInfo struct {
	OriginalUser string
	Content      string
}

// ParseForwardInfo extracts original user and message from a forwarded transaction.
func ParseForwardInfo(m Movement, wMap *UserWalletMap) (ForwardInfo, bool) {
	// 1. Check ExtraInfo if present (JSON)
	if m.ExtraInfo != "" {
		var extra map[string]string
		if err := json.Unmarshal([]byte(m.ExtraInfo), &extra); err == nil {
			if extra["type"] == "forward_via_bot" {
				user := extra["original_user"]
				if user == "" {
					user = extra["original_sender"]
					if wMap != nil && user != "" {
						if u, ok := wMap.LookupUser(user); ok {
							user = u
						}
					}
				}
				return ForwardInfo{
					OriginalUser: user,
					Content:      extra["original_content"],
				}, true
			}
		}
	}

	// 2. Check TextData / Content for pattern:
	// "<user> transfer via bot: <content>" or "<user> transfer via bot"
	content := strings.TrimSpace(m.Content)
	if strings.Contains(content, " transfer via bot: ") {
		parts := strings.SplitN(content, " transfer via bot: ", 2)
		user := strings.TrimSpace(parts[0])
		if wMap != nil {
			if u, ok := wMap.LookupUser(user); ok {
				user = u
			}
		}
		return ForwardInfo{
			OriginalUser: user,
			Content:      strings.TrimSpace(parts[1]),
		}, true
	}
	if strings.HasSuffix(content, " transfer via bot") {
		user := strings.TrimSuffix(content, " transfer via bot")
		user = strings.TrimSpace(user)
		if wMap != nil {
			if u, ok := wMap.LookupUser(user); ok {
				user = u
			}
		}
		return ForwardInfo{
			OriginalUser: user,
			Content:      "",
		}, true
	}

	// 3. Fallback: if sender is the bot wallet address
	if m.BotAddress != "" && m.Sender == m.BotAddress {
		return ForwardInfo{
			OriginalUser: "Bot (trung gian)",
			Content:      m.Content,
		}, true
	}

	return ForwardInfo{}, false
}

// MovementText formats the announcement for a transaction.
func MovementText(m Movement, decimals int, loc *time.Location, wallets ...*UserWalletMap) string {
	sign, verb := "+", "Nhận vào quỹ"
	targetLabel := "Người gửi"
	targetAddr := m.Sender
	if !m.Incoming {
		sign, verb = "−", "Chi từ quỹ" // Unicode minus \u2212
		targetLabel = "Người nhận"
		targetAddr = m.Recipient
	}

	wMap := DefaultWallets
	if len(wallets) > 0 && wallets[0] != nil {
		wMap = wallets[0]
	}

	t := m.At
	if loc != nil {
		t = t.In(loc)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s%s VNĐ\n", verb, sign, FormatVND(m.VND(decimals))))

	// Check if this incoming transaction was forwarded via bot
	if m.Incoming {
		if fwd, ok := ParseForwardInfo(m, wMap); ok {
			sb.WriteString(fmt.Sprintf("Người gửi: %s (qua bot)\n", fwd.OriginalUser))
			if fwd.Content != "" {
				sb.WriteString(fmt.Sprintf("Nội dung: %s\n", fwd.Content))
			}
			sb.WriteString(t.Format("15:04 02/01/2006"))
			return sb.String()
		}
	}

	if targetAddr != "" {
		displayName := targetAddr
		if wMap != nil {
			if u, ok := wMap.LookupUser(targetAddr); ok {
				displayName = u
			}
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n", targetLabel, displayName))
	}

	if m.Content != "" {
		sb.WriteString(fmt.Sprintf("Nội dung: %s\n", m.Content))
	}

	sb.WriteString(t.Format("15:04 02/01/2006"))
	return sb.String()
}
