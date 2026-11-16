package sport

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUserRegistry_ResolutionAndAliases(t *testing.T) {
	reg := NewUserRegistry("")

	// Test default profile resolution
	if name := reg.ResolveDisplayName("", "son.nguyentruong1"); name != "Sơn Nguyễn Trường" {
		t.Errorf("expected 'Sơn Nguyễn Trường', got %q", name)
	}

	// Test default alias resolution
	if name := reg.ResolveDisplayName("", "son.nt"); name != "Sơn Nguyễn Trường" {
		t.Errorf("expected 'Sơn Nguyễn Trường' for alias 'son.nt', got %q", name)
	}

	if name := reg.ResolveDisplayName("", "sơn nt"); name != "Sơn Nguyễn Trường" {
		t.Errorf("expected 'Sơn Nguyễn Trường' for alias 'sơn nt', got %q", name)
	}

	if name := reg.ResolveDisplayName("", "dai.trinhduc"); name != "Trịnh Đức Đại" {
		t.Errorf("expected 'Trịnh Đức Đại', got %q", name)
	}

	if name := reg.ResolveDisplayName("", "dai.td"); name != "Trịnh Đức Đại" {
		t.Errorf("expected 'Trịnh Đức Đại' for alias 'dai.td', got %q", name)
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

	// There should only be 1 attendee in Going, named "Sơn Nguyễn Trường"
	if len(summary.Going) != 1 {
		t.Fatalf("expected exactly 1 attendee (deduplicated), got %d: %v", len(summary.Going), summary.Going)
	}

	if summary.Going[0] != "Sơn Nguyễn Trường" {
		t.Errorf("expected attendee 'Sơn Nguyễn Trường', got %q", summary.Going[0])
	}
}
