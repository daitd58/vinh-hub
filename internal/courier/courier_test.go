package courier

import (
	"context"
	"errors"
	"testing"

	"vinh-hub/internal/outbox"
)

type fakeSender struct {
	sent   []string
	failOn string
}

func (f *fakeSender) SendToThread(_ context.Context, _, _, body string) error {
	if f.failOn != "" && body == f.failOn {
		return errors.New("delivery failed")
	}
	f.sent = append(f.sent, body)
	return nil
}

func TestCourierDeliverSuccess(t *testing.T) {
	box := outbox.NewMemoryOutbox()
	sender := &fakeSender{}
	c := &Courier{
		Outbox: box,
		Sender: sender,
	}

	ctx := context.Background()
	_ = box.Enqueue(ctx, outbox.Item{ThreadID: "t1", Body: "msg1"})
	_ = box.Enqueue(ctx, outbox.Item{ThreadID: "t1", Body: "msg2"})

	sent, failed, err := c.Deliver(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 2 || failed != 0 {
		t.Errorf("sent=%d, failed=%d, want 2 sent and 0 failed", sent, failed)
	}
	if len(sender.sent) != 2 {
		t.Errorf("sender recorded %d messages, want 2", len(sender.sent))
	}

	// Verify outbox is drained
	pending, _ := box.Pending(ctx, 10)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending items, got %d", len(pending))
	}
}

func TestCourierDeliverRetryOnFailure(t *testing.T) {
	box := outbox.NewMemoryOutbox()
	sender := &fakeSender{failOn: "fail-msg"}
	c := &Courier{
		Outbox: box,
		Sender: sender,
	}

	ctx := context.Background()
	_ = box.Enqueue(ctx, outbox.Item{ThreadID: "t1", Body: "fail-msg"})

	sent, failed, err := c.Deliver(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 0 || failed != 1 {
		t.Errorf("sent=%d, failed=%d, want 0 sent and 1 failed", sent, failed)
	}

	// Should still be in pending outbox with attempt 1
	pending, _ := box.Pending(ctx, 10)
	if len(pending) != 1 || pending[0].Attempts != 1 {
		t.Errorf("item should be pending with 1 attempt")
	}
}
