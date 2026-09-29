package outbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrAlreadyQueued is returned when an item with the same dedupe key was queued before.
var ErrAlreadyQueued = errors.New("already queued")

// MaxAttempts is the maximum delivery tries before marking an item failed.
const MaxAttempts = 5

// Status constants
const (
	StatusPending = "pending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

// Item is an outgoing notification in the outbox.
type Item struct {
	ID        int64
	Kind      string
	ChannelID string
	ThreadID  string
	Body      string
	DedupeKey string
	Status    string
	Attempts  int
	LastError string
	CreatedAt time.Time
	SentAt    *time.Time
}

// Outbox represents the storage queue for outgoing messages.
type Outbox interface {
	Enqueue(ctx context.Context, item Item) error
	Pending(ctx context.Context, limit int) ([]*Item, error)
	MarkSent(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, cause string) error
}

// MemoryOutbox is an in-memory, thread-safe implementation of Outbox.
type MemoryOutbox struct {
	mu         sync.Mutex
	nextID     int64
	seenDedupe map[string]struct{}
	items      map[int64]*Item
	order      []int64
}

// NewMemoryOutbox creates a new in-memory outbox.
func NewMemoryOutbox() *MemoryOutbox {
	return &MemoryOutbox{
		nextID:     1,
		seenDedupe: make(map[string]struct{}),
		items:      make(map[int64]*Item),
	}
}

// Enqueue adds an item to the outbox if dedupe key hasn't been seen.
func (m *MemoryOutbox) Enqueue(ctx context.Context, item Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if item.ChannelID == "" && item.ThreadID == "" {
		return fmt.Errorf("item has no destination channel or thread")
	}

	if item.DedupeKey != "" {
		if _, exists := m.seenDedupe[item.DedupeKey]; exists {
			return ErrAlreadyQueued
		}
		m.seenDedupe[item.DedupeKey] = struct{}{}
	}

	item.ID = m.nextID
	m.nextID++
	if item.Status == "" {
		item.Status = StatusPending
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}

	m.items[item.ID] = &item
	m.order = append(m.order, item.ID)
	return nil
}

// Pending returns up to limit items that are pending.
func (m *MemoryOutbox) Pending(ctx context.Context, limit int) ([]*Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if limit <= 0 {
		limit = 50
	}

	var res []*Item
	for _, id := range m.order {
		it := m.items[id]
		if it != nil && it.Status == StatusPending {
			copied := *it
			res = append(res, &copied)
			if len(res) >= limit {
				break
			}
		}
	}
	return res, nil
}

// MarkSent marks an item as delivered.
func (m *MemoryOutbox) MarkSent(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	it, ok := m.items[id]
	if !ok {
		return fmt.Errorf("item %d not found", id)
	}
	it.Status = StatusSent
	now := time.Now()
	it.SentAt = &now
	return nil
}

// MarkFailed increments attempt count and marks failed after MaxAttempts.
func (m *MemoryOutbox) MarkFailed(ctx context.Context, id int64, cause string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	it, ok := m.items[id]
	if !ok {
		return fmt.Errorf("item %d not found", id)
	}
	it.Attempts++
	it.LastError = cause
	if it.Attempts >= MaxAttempts {
		it.Status = StatusFailed
	} else {
		it.Status = StatusPending
	}
	return nil
}
