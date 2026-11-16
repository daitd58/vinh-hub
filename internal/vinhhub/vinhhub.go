package vinhhub

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	UpdatedBy string    `json:"updated_by,omitempty"`
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
	m.mu.RLock()
	rawURL := m.state.CustomURL
	if rawURL == "" {
		rawURL = m.defaultURL
	}
	signer := m.signer
	m.mu.RUnlock()

	if rawURL == "" {
		return "", fmt.Errorf("no vinhhub image configured")
	}

	if signer != nil {
		return signer(rawURL)
	}
	return rawURL, nil
}

// Register validates and saves a new custom image URL.
func (m *Manager) Register(rawURL, updatedBy string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("link ảnh không được để trống")
	}

	u, err := url.ParseRequestURI(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("link ảnh không hợp lệ (cần bắt đầu bằng http:// hoặc https://)")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.CustomURL = trimmed
	m.state.UpdatedAt = time.Now()
	m.state.UpdatedBy = strings.TrimSpace(updatedBy)

	if err := m.saveLocked(); err != nil {
		return fmt.Errorf("lưu cấu hình thất bại: %w", err)
	}

	m.log.Info("registered new vinhhub image", "url", trimmed, "by", updatedBy)
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
