package fund

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeSource struct {
	balance *big.Int
	err     error
	calls   int
}

func (f *fakeSource) WalletBalance(context.Context, string) (*big.Int, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.balance, nil
}

func micro(vnd int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(vnd), big.NewInt(1_000_000))
}

func TestFundBalanceRead(t *testing.T) {
	source := &fakeSource{balance: micro(46_000_000)}
	r := New(Options{Source: source, Wallet: "w"})

	balance, err := r.Balance(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if balance.BalanceVND != 46_000_000 {
		t.Errorf("balance = %d, want 46000000", balance.BalanceVND)
	}
}

func TestTheBalanceIsCached(t *testing.T) {
	source := &fakeSource{balance: micro(500_000_000)}
	now := time.Now()
	r := New(Options{
		Source: source,
		Wallet: "w",
		TTL:    time.Minute,
		Now:    func() time.Time { return now },
	})

	for range 5 {
		if _, err := r.Balance(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if source.calls != 1 {
		t.Errorf("read the source %d times, want 1", source.calls)
	}

	now = now.Add(2 * time.Minute)
	if _, err := r.Balance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if source.calls != 2 {
		t.Errorf("source was not refreshed after TTL: %d calls", source.calls)
	}
}

func TestAStaleFigureSurvivesAnIndexerFailure(t *testing.T) {
	source := &fakeSource{balance: micro(700_000_000)}
	now := time.Now()
	r := New(Options{
		Source: source,
		Wallet: "w",
		TTL:    time.Minute,
		Now:    func() time.Time { return now },
	})

	first, err := r.Balance(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	source.err = errors.New("indexer down")
	now = now.Add(2 * time.Minute)

	again, err := r.Balance(context.Background())
	if err != nil {
		t.Fatalf("cached figure should have been returned: %v", err)
	}
	if again.BalanceVND != first.BalanceVND {
		t.Errorf("stale figure = %d, want earlier %d", again.BalanceVND, first.BalanceVND)
	}
	if !again.At.Equal(first.At) {
		t.Error("stale figure timestamp was incorrectly updated")
	}
}

func TestAFirstFailureIsReported(t *testing.T) {
	r := New(Options{
		Source: &fakeSource{err: errors.New("indexer down")},
		Wallet: "w",
	})
	if _, err := r.Balance(context.Background()); err == nil {
		t.Error("failed first read should return error")
	}
}

func TestNoWalletMeansNoReader(t *testing.T) {
	if r := New(Options{Source: &fakeSource{}}); r != nil {
		t.Error("Reader was built without a wallet")
	}
	var missing *Reader
	if _, err := missing.Balance(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("got %v, want ErrNotConfigured", err)
	}
}

func TestIndexerHTTPCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1337/wallets/addr1/detail" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"address":"addr1","balance":"2500000000"}}`))
	}))
	defer srv.Close()

	indexer := NewIndexer(srv.URL, "1337", 6, srv.Client())
	raw, err := indexer.WalletBalance(context.Background(), "addr1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw.String() != "2500000000" {
		t.Errorf("raw balance = %s, want 2500000000", raw.String())
	}
	vnd := ToVND(raw, 6)
	if vnd != 2500 {
		t.Errorf("ToVND = %d, want 2500", vnd)
	}
}

func TestReaderFreshBalanceBypassesCache(t *testing.T) {
	source := &fakeSource{balance: micro(10_000_000)}
	r := New(Options{
		Source: source,
		Wallet: "w",
		TTL:    time.Hour,
	})

	b1, err := r.Balance(context.Background())
	if err != nil || b1.BalanceVND != 10_000_000 {
		t.Fatalf("first read = %v, %v", b1, err)
	}
	if source.calls != 1 {
		t.Fatalf("expected 1 call, got %d", source.calls)
	}

	// Update source balance
	source.balance = micro(20_000_000)

	// Balance() returns cached figure
	b2, err := r.Balance(context.Background())
	if err != nil || b2.BalanceVND != 10_000_000 {
		t.Fatalf("cached read = %v, %v", b2, err)
	}
	if source.calls != 1 {
		t.Fatalf("expected still 1 call, got %d", source.calls)
	}

	// FreshBalance() bypasses cache and reads new balance
	b3, err := r.FreshBalance(context.Background())
	if err != nil || b3.BalanceVND != 20_000_000 {
		t.Fatalf("fresh read = %v, %v", b3, err)
	}
	if source.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", source.calls)
	}

	// Subsequent Balance() now sees updated cache
	b4, err := r.Balance(context.Background())
	if err != nil || b4.BalanceVND != 20_000_000 {
		t.Fatalf("subsequent cached read = %v, %v", b4, err)
	}
}

func TestReaderInvalidateAndSetCached(t *testing.T) {
	source := &fakeSource{balance: micro(5_000_000)}
	r := New(Options{
		Source: source,
		Wallet: "w",
		TTL:    time.Hour,
	})

	b, _ := r.Balance(context.Background())
	if b.BalanceVND != 5_000_000 {
		t.Fatalf("want 5000000, got %d", b.BalanceVND)
	}

	// SetCached manually
	r.SetCached(12_345_678)
	b, _ = r.Balance(context.Background())
	if b.BalanceVND != 12_345_678 {
		t.Fatalf("want 12345678, got %d", b.BalanceVND)
	}

	// Invalidate clears cache
	r.Invalidate()
	source.balance = micro(9_000_000)
	b, _ = r.Balance(context.Background())
	if b.BalanceVND != 9_000_000 {
		t.Fatalf("want 9000000 after invalidate, got %d", b.BalanceVND)
	}
}
