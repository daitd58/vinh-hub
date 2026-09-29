package fund

import (
	"context"
	"errors"
	"math/big"
	"testing"
)

func TestFallbackSourcePrimarySuccess(t *testing.T) {
	primary := &fakeSource{balance: big.NewInt(100)}
	secondary := &fakeSource{balance: big.NewInt(200)}

	fb := &FallbackSource{
		Primary:   primary,
		Secondary: secondary,
	}

	bal, err := fb.WalletBalance(context.Background(), "w")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bal.Int64() != 100 {
		t.Errorf("balance = %d, want 100", bal.Int64())
	}
	if primary.calls != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls)
	}
	if secondary.calls != 0 {
		t.Errorf("secondary calls = %d, want 0", secondary.calls)
	}
}

func TestFallbackSourceSecondaryWhenPrimaryFails(t *testing.T) {
	primary := &fakeSource{err: errors.New("node down")}
	secondary := &fakeSource{balance: big.NewInt(200)}

	fb := &FallbackSource{
		Primary:   primary,
		Secondary: secondary,
	}

	bal, err := fb.WalletBalance(context.Background(), "w")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bal.Int64() != 200 {
		t.Errorf("balance = %d, want 200", bal.Int64())
	}
	if primary.calls != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls)
	}
	if secondary.calls != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.calls)
	}
}

func TestFallbackSourceBothFail(t *testing.T) {
	primary := &fakeSource{err: errors.New("node down")}
	secondary := &fakeSource{err: errors.New("indexer down")}

	fb := &FallbackSource{
		Primary:   primary,
		Secondary: secondary,
	}

	_, err := fb.WalletBalance(context.Background(), "w")
	if err == nil {
		t.Fatal("expected error when both fail, got nil")
	}
}
