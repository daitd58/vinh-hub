package fund

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserWalletMapLookup(t *testing.T) {
	m := NewUserWalletMap(map[string]string{
		"alice": "addr-alice-123",
		"bob":   "addr-bob-456",
	})

	if user, ok := m.LookupUser("addr-alice-123"); !ok || user != "alice" {
		t.Errorf("LookupUser failed: got %q, %v, want alice, true", user, ok)
	}

	if wallet, ok := m.LookupWallet("bob"); !ok || wallet != "addr-bob-456" {
		t.Errorf("LookupWallet failed: got %q, %v, want addr-bob-456, true", wallet, ok)
	}

	if _, ok := m.LookupUser("unknown"); ok {
		t.Error("expected unknown wallet to return false")
	}

	if _, ok := m.LookupWallet("unknown"); ok {
		t.Error("expected unknown user to return false")
	}
}

func TestDefaultWalletsHasKnownUsers(t *testing.T) {
	daiWallet, ok := DefaultWallets.LookupWallet("dai.trinhduc")
	if !ok || daiWallet != "DzSGqEnhbpdLnsAPqyhAD97UxTKeHWmCaMm3nENQasbW" {
		t.Errorf("expected dai.trinhduc to map to DzSGqEnhbpdLnsAPqyhAD97UxTKeHWmCaMm3nENQasbW, got %q, %v", daiWallet, ok)
	}

	user, ok := DefaultWallets.LookupUser("DzSGqEnhbpdLnsAPqyhAD97UxTKeHWmCaMm3nENQasbW")
	if !ok || user != "dai.trinhduc" {
		t.Errorf("expected reverse lookup to find dai.trinhduc, got %q, %v", user, ok)
	}
}

func TestLoadUserWalletMap(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "custom_wallets.json")

	content := `{"custom.user": "addr-custom-999"}`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	m, err := LoadUserWalletMap(filePath)
	if err != nil {
		t.Fatalf("LoadUserWalletMap failed: %v", err)
	}

	if wallet, ok := m.LookupWallet("custom.user"); !ok || wallet != "addr-custom-999" {
		t.Errorf("custom.user not loaded properly: got %q, %v", wallet, ok)
	}

	// Default users should still be present
	if _, ok := m.LookupWallet("dai.trinhduc"); !ok {
		t.Error("default user dai.trinhduc was missing after merging custom file")
	}
}
