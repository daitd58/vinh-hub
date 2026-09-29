package courier

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"vinh-hub/internal/outbox"
)

// Sender delivers a message to a thread or channel.
type Sender interface {
	SendToThread(ctx context.Context, channelID, threadID, body string) error
}

// Courier regularly sweeps the outbox and delivers pending messages.
type Courier struct {
	Outbox   outbox.Outbox
	Sender   Sender
	Log      *slog.Logger
	Batch    int
	Interval time.Duration
}

// Deliver sends each pending message once.
func (c *Courier) Deliver(ctx context.Context) (sent int, failed int, err error) {
	batch := c.Batch
	if batch <= 0 {
		batch = 50
	}

	pending, err := c.Outbox.Pending(ctx, batch)
	if err != nil {
		return 0, 0, fmt.Errorf("load pending notifications: %w", err)
	}

	for _, item := range pending {
		if c.Sender == nil {
			err := fmt.Errorf("no sender configured")
			_ = c.Outbox.MarkFailed(ctx, item.ID, err.Error())
			failed++
			continue
		}

		if err := c.Sender.SendToThread(ctx, item.ChannelID, item.ThreadID, item.Body); err != nil {
			c.logger().Warn("could not deliver notification",
				"id", item.ID,
				"kind", item.Kind,
				"attempts", item.Attempts+1,
				"err", err)
			_ = c.Outbox.MarkFailed(ctx, item.ID, err.Error())
			failed++
			continue
		}

		if err := c.Outbox.MarkSent(ctx, item.ID); err != nil {
			c.logger().Error("delivered message but failed to mark sent", "id", item.ID, "err", err)
		}
		sent++
	}

	return sent, failed, nil
}

// Run executes Deliver on an interval until the context is cancelled.
func (c *Courier) Run(ctx context.Context) {
	interval := c.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial sweep on startup
	if sent, failed, err := c.Deliver(ctx); err == nil && (sent > 0 || failed > 0) {
		c.logger().Info("initial outbox sweep complete", "sent", sent, "failed", failed)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sent, failed, err := c.Deliver(ctx)
			if err != nil {
				c.logger().Error("courier sweep failed", "err", err)
				continue
			}
			if sent > 0 || failed > 0 {
				c.logger().Info("notifications delivered", "sent", sent, "failed", failed)
			}
		}
	}
}

func (c *Courier) logger() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}
