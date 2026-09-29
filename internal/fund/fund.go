package fund

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Balance reports what the fund currently holds.
type Balance struct {
	// BalanceVND is the fund's current balance in whole VNĐ.
	BalanceVND int64
	// At is when the wallet balance was read.
	At time.Time
}

// Source reads a wallet's balance in the chain's smallest unit.
type Source interface {
	WalletBalance(ctx context.Context, address string) (*big.Int, error)
}

// FreshBalancer can query the live balance bypassing cached values and update cache.
type FreshBalancer interface {
	BalanceReader
	FreshBalance(ctx context.Context) (Balance, error)
	Invalidate()
	SetCached(balVND int64)
}

// ErrNotConfigured reports that no fund wallet address was configured.
var ErrNotConfigured = errors.New("chưa cấu hình địa chỉ ví của quỹ")

// Indexer reads balances from the Mezon Đồng indexer REST API.
type Indexer struct {
	BaseURL  string
	ChainID  string
	Decimals int
	HTTP     *http.Client
}

// NewIndexer builds a Source communicating with the indexer REST API.
func NewIndexer(baseURL, chainID string, decimals int, client *http.Client) *Indexer {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &Indexer{
		BaseURL:  strings.TrimSuffix(baseURL, "/"),
		ChainID:  chainID,
		Decimals: decimals,
		HTTP:     client,
	}
}

// WalletBalance fetches the raw balance from the indexer API.
func (i *Indexer) WalletBalance(ctx context.Context, address string) (*big.Int, error) {
	endpoint := fmt.Sprintf("%s/%s/wallets/%s/detail",
		i.BaseURL, url.PathEscape(i.ChainID), url.PathEscape(address))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build wallet request: %w", err)
	}
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read wallet %s: %w", address, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer returned %s for wallet %s", resp.Status, address)
	}

	var body struct {
		Data struct {
			Address string `json:"address"`
			Balance string `json:"balance"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode wallet %s: %w", address, err)
	}
	if body.Data.Balance == "" {
		return nil, fmt.Errorf("indexer returned no balance for wallet %s", address)
	}

	raw, ok := new(big.Int).SetString(body.Data.Balance, 10)
	if !ok {
		return nil, fmt.Errorf("wallet %s has unreadable balance %q", address, body.Data.Balance)
	}
	return raw, nil
}

// Options configure a Reader.
type Options struct {
	Source   Source
	Wallet   string
	Decimals int
	TTL      time.Duration
	Now      func() time.Time
}

// Reader reads and caches the fund balance.
type Reader struct {
	source   Source
	wallet   string
	decimals int
	ttl      time.Duration
	now      func() time.Time

	mu     sync.Mutex
	cached *Balance
}

// New builds a Reader, or returns nil if source or wallet is not provided.
func New(opts Options) *Reader {
	if opts.Source == nil || opts.Wallet == "" {
		return nil
	}
	if opts.Decimals <= 0 {
		opts.Decimals = 6
	}
	if opts.TTL <= 0 {
		opts.TTL = 10 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Reader{
		source:   opts.Source,
		wallet:   opts.Wallet,
		decimals: opts.Decimals,
		ttl:      opts.TTL,
		now:      opts.Now,
	}
}

// Balance returns the current balance, reusing cached values within TTL.
func (r *Reader) Balance(ctx context.Context) (Balance, error) {
	if r == nil {
		return Balance{}, ErrNotConfigured
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cached != nil && r.now().Sub(r.cached.At) < r.ttl {
		return *r.cached, nil
	}

	raw, err := r.source.WalletBalance(ctx, r.wallet)
	if err != nil {
		// A stale reading beats nothing: return previously cached reading if available
		if r.cached != nil {
			return *r.cached, nil
		}
		return Balance{}, err
	}

	b := Balance{
		BalanceVND: ToVND(raw, r.decimals),
		At:         r.now(),
	}

	r.cached = &b
	return b, nil
}

// FreshBalance queries the source directly, bypassing any cached balance,
// and updates the cache with the new reading.
func (r *Reader) FreshBalance(ctx context.Context) (Balance, error) {
	if r == nil {
		return Balance{}, ErrNotConfigured
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	raw, err := r.source.WalletBalance(ctx, r.wallet)
	if err != nil {
		if r.cached != nil {
			return *r.cached, nil
		}
		return Balance{}, err
	}

	b := Balance{
		BalanceVND: ToVND(raw, r.decimals),
		At:         r.now(),
	}

	r.cached = &b
	return b, nil
}

// Invalidate clears the cached balance.
func (r *Reader) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = nil
}

// SetCached overrides the cached balance with a known value.
func (r *Reader) SetCached(balVND int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached = &Balance{
		BalanceVND: balVND,
		At:         r.now(),
	}
}

// ToVND converts a raw chain balance to whole đồng.
func ToVND(raw *big.Int, decimals int) int64 {
	if raw == nil || raw.Sign() < 0 {
		return 0
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	whole := new(big.Int).Div(raw, scale)
	if !whole.IsInt64() || whole.Int64() > math.MaxInt64 {
		return 0
	}
	return whole.Int64()
}

// FormatVND formats an integer with dot separators (Vietnamese standard).
func FormatVND(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
