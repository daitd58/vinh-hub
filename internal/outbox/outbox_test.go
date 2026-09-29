package outbox

import (
	"context"
	"errors"
	"testing"
)

func TestOutboxDeduplication(t *testing.T) {
	o := NewMemoryOutbox()
	ctx := context.Background()

	item1 := Item{
		Kind:      "fund_activity",
		ChannelID: "c1",
		ThreadID:  "t1",
		Body:      "msg 1",
		DedupeKey: "fund:tx1",
	}

	if err := o.Enqueue(ctx, item1); err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}

	// Second enqueue with same dedupe key must return ErrAlreadyQueued
	err := o.Enqueue(ctx, item1)
	if !errors.Is(err, ErrAlreadyQueued) {
		t.Fatalf("got err=%v, want ErrAlreadyQueued", err)
	}
}

func TestOutboxPendingAndRetries(t *testing.T) {
	o := NewMemoryOutbox()
	ctx := context.Background()

	item := Item{
		Kind:      "fund_activity",
		ThreadID:  "t1",
		Body:      "test",
		DedupeKey: "fund:tx2",
	}
	if err := o.Enqueue(ctx, item); err != nil {
		t.Fatal(err)
	}

	pending, err := o.Pending(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending len=%d, err=%v", len(pending), err)
	}
	id := pending[0].ID

	// Mark failed up to MaxAttempts
	for i := 1; i < MaxAttempts; i++ {
		if err := o.MarkFailed(ctx, id, "fail"); err != nil {
			t.Fatal(err)
		}
		items, _ := o.Pending(ctx, 10)
		if len(items) != 1 {
			t.Fatalf("item should still be pending after retry %d", i)
		}
		if items[0].Attempts != i {
			t.Errorf("attempts=%d, want %d", items[0].Attempts, i)
		}
	}

	// Reaching MaxAttempts marks it failed
	if err := o.MarkFailed(ctx, id, "permanent error"); err != nil {
		t.Fatal(err)
	}
	items, _ := o.Pending(ctx, 10)
	if len(items) != 0 {
		t.Errorf("item should no longer be pending after %d attempts", MaxAttempts)
	}
}

func TestOutboxMarkSent(t *testing.T) {
	o := NewMemoryOutbox()
	ctx := context.Background()

	item := Item{ChannelID: "c1", Body: "hello"}
	_ = o.Enqueue(ctx, item)

	items, _ := o.Pending(ctx, 10)
	if len(items) != 1 {
		t.Fatal("expected 1 item")
	}

	if err := o.MarkSent(ctx, items[0].ID); err != nil {
		t.Fatal(err)
	}

	itemsAfter, _ := o.Pending(ctx, 10)
	if len(itemsAfter) != 0 {
		t.Errorf("sent items should not be pending")
	}
}
