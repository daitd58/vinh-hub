package vinhhub

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
)

func TestParseCDNChannel(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantID    int64
		wantValid bool
	}{
		{
			name:      "valid mezon cdn url",
			rawURL:    "https://cdn.mezon.ai/1994479556801000/2121182274450886656_SG_Energy_Hub_3.png?1794795387-WzdD7%2B%2BoP9%2F%2FyK5Q%2BnpIXfGobTpgQyfZ8sasb74aXCc%3D",
			wantID:    1843176854228307968,
			wantValid: true,
		},
		{
			name:      "external url",
			rawURL:    "https://i.imgur.com/someimage.png",
			wantID:    0,
			wantValid: false,
		},
		{
			name:      "cdn url invalid hex length",
			rawURL:    "https://cdn.mezon.ai/123/image.png",
			wantID:    0,
			wantValid: false,
		},
		{
			name:      "invalid url",
			rawURL:    "://invalid-url",
			wantID:    0,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotValid := ParseCDNChannel(tt.rawURL)
			if gotValid != tt.wantValid {
				t.Fatalf("ParseCDNChannel() valid = %v, want %v", gotValid, tt.wantValid)
			}
			if gotID != tt.wantID {
				t.Fatalf("ParseCDNChannel() channelID = %d, want %d", gotID, tt.wantID)
			}
		})
	}
}

func TestFileNameFromURL(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		wantName string
	}{
		{
			name:     "standard cdn url",
			rawURL:   "https://cdn.mezon.ai/1994479556801000/2121182274450886656_SG_Energy_Hub_3.png?1794795387-abc",
			wantName: "2121182274450886656_SG_Energy_Hub_3.png",
		},
		{
			name:     "empty or root url",
			rawURL:   "https://cdn.mezon.ai/",
			wantName: "vinhhub_menu.png",
		},
		{
			name:     "plain file",
			rawURL:   "https://example.com/menu.jpg",
			wantName: "menu.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FileNameFromURL(tt.rawURL)
			if got != tt.wantName {
				t.Fatalf("FileNameFromURL() = %s, want %s", got, tt.wantName)
			}
		})
	}
}

func TestManagerRegisterAndDelete(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "vinhhub_state.json")
	defaultURL := "https://cdn.mezon.ai/1994479556801000/default.png"

	mgr := NewManager(defaultURL, statePath, nil)

	// Check default
	raw, isCustom := mgr.GetRawImageURL()
	if isCustom || raw != defaultURL {
		t.Fatalf("expected default URL %s (isCustom=false), got %s (%v)", defaultURL, raw, isCustom)
	}

	// Register invalid
	if err := mgr.Register("", "user1"); err == nil {
		t.Fatal("expected error registering empty URL")
	}
	if err := mgr.Register("ftp://foo/bar.png", "user1"); err == nil {
		t.Fatal("expected error registering non-http URL")
	}
	if err := mgr.Register("not-a-valid-url", "user1"); err == nil {
		t.Fatal("expected error registering malformed URL")
	}

	// Register valid
	customURL := "https://cdn.mezon.ai/1994479556801000/custom.png"
	if err := mgr.Register(customURL, "admin"); err != nil {
		t.Fatalf("unexpected error registering URL: %v", err)
	}

	raw, isCustom = mgr.GetRawImageURL()
	if !isCustom || raw != customURL {
		t.Fatalf("expected custom URL %s, got %s (%v)", customURL, raw, isCustom)
	}

	// Ensure persisted to disk and reloads cleanly
	mgrReloaded := NewManager(defaultURL, statePath, nil)
	raw, isCustom = mgrReloaded.GetRawImageURL()
	if !isCustom || raw != customURL {
		t.Fatalf("expected reloaded custom URL %s, got %s (%v)", customURL, raw, isCustom)
	}

	// Test Delete
	if err := mgrReloaded.Delete(); err != nil {
		t.Fatalf("unexpected error deleting custom URL: %v", err)
	}

	raw, isCustom = mgrReloaded.GetRawImageURL()
	if isCustom || raw != defaultURL {
		t.Fatalf("expected reset to default URL %s, got %s (%v)", defaultURL, raw, isCustom)
	}

	// Ensure disk state reloads default
	mgrAfterDelete := NewManager(defaultURL, statePath, nil)
	raw, isCustom = mgrAfterDelete.GetRawImageURL()
	if isCustom || raw != defaultURL {
		t.Fatalf("expected disk state after delete to be default, got %s (%v)", raw, isCustom)
	}
}

func TestManagerGetImageURLWithSigner(t *testing.T) {
	defaultURL := "https://cdn.mezon.ai/1994479556801000/default.png"
	mgr := NewManager(defaultURL, "", nil)

	mgr.SetSigner(func(u string) (string, error) {
		parsed, err := url.Parse(u)
		if err != nil {
			return "", err
		}
		parsed.RawQuery = "sig=fresh_123"
		return parsed.String(), nil
	})

	signed, err := mgr.GetImageURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "https://cdn.mezon.ai/1994479556801000/default.png?sig=fresh_123"
	if signed != expected {
		t.Fatalf("GetImageURL() = %s, want %s", signed, expected)
	}

	// Test signer error propagation
	mgr.SetSigner(func(u string) (string, error) {
		return "", fmt.Errorf("mock signing failed")
	})
	if _, err := mgr.GetImageURL(); err == nil {
		t.Fatal("expected signer error to propagate")
	}
}

func TestGetImageInfoAndProbe(t *testing.T) {
	// Setup test HTTP server serving a 300x150 test PNG
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 300, 150))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, img)
	}))
	defer ts.Close()

	defaultURL := "https://cdn.mezon.ai/1994479556801000/default.png"
	mgr := NewManager(defaultURL, "", nil)

	// Default info checks
	info, err := mgr.GetImageInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Width != 1290 || info.Height != 960 {
		t.Errorf("expected default dimensions 1290x960, got %dx%d", info.Width, info.Height)
	}
	if info.Filetype != "image/png" {
		t.Errorf("expected filetype image/png, got %s", info.Filetype)
	}
	if info.Filename != "" {
		t.Errorf("expected empty filename for inline preview, got %s", info.Filename)
	}

	// Register probed image from test server
	if err := mgr.Register(ts.URL+"/custom.png", "tester"); err != nil {
		t.Fatalf("register probed image failed: %v", err)
	}

	customInfo, err := mgr.GetImageInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if customInfo.Width != 300 || customInfo.Height != 150 {
		t.Errorf("expected probed dimensions 300x150, got %dx%d", customInfo.Width, customInfo.Height)
	}
	if customInfo.Filetype != "image/png" {
		t.Errorf("expected probed filetype image/png, got %s", customInfo.Filetype)
	}
}
