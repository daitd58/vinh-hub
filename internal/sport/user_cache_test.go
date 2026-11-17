package sport

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUserRegistry_ResolutionAndAliases(t *testing.T) {
	reg := NewUserRegistry("")

	// Test keeping username as-is (no mapping to real names)
	if name := reg.ResolveDisplayName("", "son.nguyentruong1"); name != "son.nguyentruong1" {
		t.Errorf("expected 'son.nguyentruong1', got %q", name)
	}

	// Test alias resolution to canonical username
	if name := reg.ResolveDisplayName("", "son.nt"); name != "son.nguyentruong1" {
		t.Errorf("expected 'son.nguyentruong1' for alias 'son.nt', got %q", name)
	}

	if name := reg.ResolveDisplayName("", "sơn nt"); name != "son.nguyentruong1" {
		t.Errorf("expected 'son.nguyentruong1' for alias 'sơn nt', got %q", name)
	}

	if name := reg.ResolveDisplayName("", "dai.trinhduc"); name != "dai.trinhduc" {
		t.Errorf("expected 'dai.trinhduc', got %q", name)
	}

	if name := reg.ResolveDisplayName("", "dai.td"); name != "dai.trinhduc" {
		t.Errorf("expected 'dai.trinhduc' for alias 'dai.td', got %q", name)
	}

	// Test registering new profile
	reg.Register("123456", "new.user", "Clan Hero", "Hero Display")
	if name := reg.ResolveDisplayName("123456", "new.user"); name != "Clan Hero" {
		t.Errorf("expected 'Clan Hero', got %q", name)
	}
	if name := reg.ResolveDisplayName("", "new.user"); name != "Clan Hero" {
		t.Errorf("expected 'Clan Hero' by username, got %q", name)
	}

	// Test fallback for unknown user
	if name := reg.ResolveDisplayName("", "unknown.user"); name != "unknown.user" {
		t.Errorf("expected 'unknown.user', got %q", name)
	}
}

func TestBadminton_DeduplicationOfSonNT(t *testing.T) {
	mgr := NewManager(Config{
		StatePath: filepath.Join(t.TempDir(), "test_badminton.json"),
	}, nil)

	// User son.nguyentruong1 reacts :cong1:
	mgr.RecordReaction("1840689219983380480", "msg1", "uid_son", "son.nguyentruong1", ":cong1:", false, 1)

	// In chat, someone writes from or mentions son.nt with +1
	mgr.RecordMessage("1840689219983380480", "msg2", "uid_son_alias", "son.nt", "+1 nha mn")

	summary := mgr.SummarizeMessages(context.Background(), mgr.state.Messages)

	// There should only be 1 attendee in Going, named "son.nguyentruong1"
	if len(summary.Going) != 1 {
		t.Fatalf("expected exactly 1 attendee (deduplicated), got %d: %v", len(summary.Going), summary.Going)
	}

	if summary.Going[0] != "son.nguyentruong1" {
		t.Errorf("expected attendee 'son.nguyentruong1', got %q", summary.Going[0])
	}
}
