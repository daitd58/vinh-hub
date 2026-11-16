package vinhhub

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// State records persisted custom VinhHub configuration.
type State struct {
	CustomURL string    `json:"custom_url,omitempty"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	Filetype  string    `json:"filetype,omitempty"`
	Size      int       `json:"size,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

// ImageInfo contains metadata and signed URL needed to render the attachment full-size in Mezon.
type ImageInfo struct {
	URL      string
	Filename string
	Filetype string
	Width    int
	Height   int
	Size     int
	IsCustom bool
}

// SignerFunc is a function that returns a fresh signed URL for Mezon CDN or other URLs.
type SignerFunc func(rawURL string) (string, error)

// Manager manages the VinhHub menu image, custom overrides, and CDN signature generation.
type Manager struct {
	defaultURL string
	statePath  string
	signer     SignerFunc
	mu         sync.RWMutex
	state      State
	log        *slog.Logger
}

// NewManager creates a Manager and loads any existing persisted state from disk.
func NewManager(defaultURL, statePath string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		defaultURL: strings.TrimSpace(defaultURL),
		statePath:  statePath,
		log:        logger,
	}

	if err := m.load(); err != nil && !os.IsNotExist(err) {
		m.log.Warn("could not load vinhhub state; starting with default image", "path", statePath, "err", err)
	}

	return m
}

// SetSigner updates the CDN signing function.
func (m *Manager) SetSigner(signer SignerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.signer = signer
}

// GetRawImageURL returns the active image URL and a boolean indicating whether it is a custom URL.
func (m *Manager) GetRawImageURL() (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.state.CustomURL != "" {
		return m.state.CustomURL, true
	}
	return m.defaultURL, false
}

// GetImageURL returns the signed image URL ready for sending or viewing.
func (m *Manager) GetImageURL() (string, error) {
	info, err := m.GetImageInfo()
	if err != nil {
		return "", err
	}
	return info.URL, nil
}

// GetImageInfo returns the full image metadata with dimensions and signature.
func (m *Manager) GetImageInfo() (*ImageInfo, error) {
	m.mu.RLock()
	rawURL := m.state.CustomURL
	isCustom := rawURL != ""
	if !isCustom {
		rawURL = m.defaultURL
	}
	width := m.state.Width
	height := m.state.Height
	filetype := m.state.Filetype
	size := m.state.Size
	signer := m.signer
	m.mu.RUnlock()

	if rawURL == "" {
		return nil, fmt.Errorf("no vinhhub image configured")
	}

	signedURL := rawURL
	if signer != nil {
		var err error
		signedURL, err = signer(rawURL)
		if err != nil {
			return nil, err
		}
	}

	// Default dimensions for VinhHub menu (1290x960 natural resolution)
	if width <= 0 || height <= 0 {
		width = 1290
		height = 960
	}
	if filetype == "" {
		filetype = "image/png"
		lower := strings.ToLower(rawURL)
		if strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") {
			filetype = "image/jpeg"
		}
	}

	return &ImageInfo{
		URL:      signedURL,
		Filename: "", // empty filename tells Mezon to render directly as inline image, matching KOMU
		Filetype: filetype,
		Width:    width,
		Height:   height,
		Size:     size,
		IsCustom: isCustom,
	}, nil
}

func probeImageDimensions(imgURL string) (width int, height int, filetype string, size int, err error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", imgURL, nil)
	if err != nil {
		return 0, 0, "", 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, "", 0, fmt.Errorf("http status %d", resp.StatusCode)
	}

	contentLength := int(resp.ContentLength)
	cfg, format, err := image.DecodeConfig(resp.Body)
	if err != nil {
		return 0, 0, "", contentLength, err
	}
	mime := "image/" + format
	if format == "jpeg" {
		mime = "image/jpeg"
	}
	return cfg.Width, cfg.Height, mime, contentLength, nil
}

// Register validates, probes dimensions, and saves a new custom image URL.
func (m *Manager) Register(rawURL, updatedBy string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("link ảnh không được để trống")
	}

	u, err := url.ParseRequestURI(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("link ảnh không hợp lệ (cần bắt đầu bằng http:// hoặc https://)")
	}

	width, height, filetype, size := 1290, 960, "image/png", 0
	urlToProbe := trimmed
	m.mu.RLock()
	signer := m.signer
	m.mu.RUnlock()
	if signer != nil {
		if s, err := signer(trimmed); err == nil && s != "" {
			urlToProbe = s
		}
	}

	if w, h, mime, sz, err := probeImageDimensions(urlToProbe); err == nil && w > 0 && h > 0 {
		width = w
		height = h
		filetype = mime
		size = sz
	} else if strings.HasSuffix(strings.ToLower(trimmed), ".jpg") || strings.HasSuffix(strings.ToLower(trimmed), ".jpeg") {
		filetype = "image/jpeg"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.CustomURL = trimmed
	m.state.Width = width
	m.state.Height = height
	m.state.Filetype = filetype
	m.state.Size = size
	m.state.UpdatedAt = time.Now()
	m.state.UpdatedBy = strings.TrimSpace(updatedBy)

	if err := m.saveLocked(); err != nil {
		return fmt.Errorf("lưu cấu hình thất bại: %w", err)
	}

	m.log.Info("registered new vinhhub image", "url", trimmed, "width", width, "height", height, "by", updatedBy)
	return nil
}

// Delete resets the image URL to the default image and clears custom state.
func (m *Manager) Delete() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state = State{}

	if err := m.saveLocked(); err != nil {
		return fmt.Errorf("xoá cấu hình thất bại: %w", err)
	}

	m.log.Info("reset vinhhub image to default", "default_url", m.defaultURL)
	return nil
}

func (m *Manager) load() error {
	if m.statePath == "" {
		return nil
	}
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		return err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	m.mu.Lock()
	m.state = s
	m.mu.Unlock()
	return nil
}

func (m *Manager) saveLocked() error {
	if m.statePath == "" {
		return nil
	}

	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.statePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	tmpFile := m.statePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpFile, m.statePath)
}
