package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigLoadDefaults(t *testing.T) {
	// Clear relevant env vars
	os.Clearenv()

	cfg := Load()
	if cfg.Fund.NodeEndpoint != "dong.mezon.ai:443" {
		t.Errorf("NodeEndpoint = %s, want dong.mezon.ai:443", cfg.Fund.NodeEndpoint)
	}
	if !cfg.Fund.NodeTLS {
		t.Errorf("NodeTLS should be true by default")
	}
	if cfg.Fund.Decimals != 6 {
		t.Errorf("Decimals = %d, want 6", cfg.Fund.Decimals)
	}
	if cfg.App.CourierInterval != 30*time.Second {
		t.Errorf("CourierInterval = %v, want 30s", cfg.App.CourierInterval)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty config")
	}

	cfg.Fund.Wallet = "addr123"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error without Mezon credentials")
	}

	cfg.Mezon.BotID = "bot1"
	cfg.Mezon.Token = "token1"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error without destination channel/thread")
	}

	cfg.Mezon.ThreadID = "thread-123"
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfigBotWallet(t *testing.T) {
	os.Clearenv()
	t.Setenv("BOT_WALLET_ADDRESS", "bot_addr_123")
	t.Setenv("BOT_PRIVATE_KEY", "bot_priv_key_456")

	cfg := Load()
	if cfg.BotWallet.Address != "bot_addr_123" {
		t.Errorf("BotWallet.Address = %s, want bot_addr_123", cfg.BotWallet.Address)
	}
	if cfg.BotWallet.PrivateKey != "bot_priv_key_456" {
		t.Errorf("BotWallet.PrivateKey = %s, want bot_priv_key_456", cfg.BotWallet.PrivateKey)
	}
}
