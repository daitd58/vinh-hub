package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration values.
type Config struct {
	Fund  FundConfig
	Mezon MezonConfig
	App   AppConfig

	DotEnvPath string
	DotEnvErr  error
}

// FundConfig holds settings for blockchain node and fund balance reader.
type FundConfig struct {
	NodeEndpoint string
	NodeTLS      bool
	Wallet       string
	Decimals     int
	ApiURL       string
	ChainID      string
	WalletsPath  string
	Timezone     *time.Location
}

// MezonConfig holds settings for connecting to Mezon and target destination.
type MezonConfig struct {
	BotID      string
	Token      string
	Host       string
	Port       string
	UseSSL     bool
	ChannelID  string
	ThreadID   string
	ThreadName string
}

// AppConfig holds runtime settings like delivery intervals and logger level.
type AppConfig struct {
	CourierInterval time.Duration
	LogLevel        string
}

// Load reads settings from the environment, reading a .env file if one exists.
func Load() *Config {
	cfg := &Config{}

	// Load local .env if present
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(".env"); err != nil {
			cfg.DotEnvErr = fmt.Errorf("read .env: %w", err)
		} else {
			cfg.DotEnvPath = ".env"
		}
	}

	// Fund
	cfg.Fund.NodeEndpoint = env("MMN_NODE_ENDPOINT", "dong.mezon.ai:443")
	cfg.Fund.NodeTLS = boolEnv("MMN_NODE_TLS", true)
	cfg.Fund.Wallet = strings.TrimSpace(env("DONATION_WALLET_ADDRESS", ""))
	cfg.Fund.Decimals = intEnv("MMN_TOKEN_DECIMALS", 6)
	cfg.Fund.ApiURL = env("MEZON_MAINNET_API_URL", "https://dong.mezon.ai/indexer-api")
	cfg.Fund.ChainID = env("MMN_CHAIN_ID", "1337")
	cfg.Fund.WalletsPath = env("USER_WALLETS_PATH", "wallets.json")

	tzName := env("TIMEZONE", "Asia/Ho_Chi_Minh")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.FixedZone("ICT", 7*3600) // Default to GMT+7 Vietnam Time
	}
	cfg.Fund.Timezone = loc

	// Mezon
	cfg.Mezon.BotID = strings.TrimSpace(env("MEZON_BOT_ID", ""))
	cfg.Mezon.Token = strings.TrimSpace(env("MEZON_TOKEN", ""))
	cfg.Mezon.Host = env("MEZON_HOST", "gw.mezon.ai")
	cfg.Mezon.Port = env("MEZON_PORT", "443")
	cfg.Mezon.UseSSL = boolEnv("MEZON_USE_SSL", true)

	// Channel can be specified via MEZON_FUND_CHANNEL_ID or MEZON_CHANNEL_ID
	chanID := env("MEZON_FUND_CHANNEL_ID", "")
	if chanID == "" {
		chanID = env("MEZON_CHANNEL_ID", "")
	}
	cfg.Mezon.ChannelID = strings.TrimSpace(chanID)
	cfg.Mezon.ThreadID = strings.TrimSpace(env("MEZON_THREAD_ID", ""))
	cfg.Mezon.ThreadName = strings.TrimSpace(env("MEZON_THREAD_NAME", "Biến động quỹ"))

	// App
	intervalSec := intEnv("COURIER_INTERVAL", 30)
	cfg.App.CourierInterval = time.Duration(intervalSec) * time.Second
	cfg.App.LogLevel = env("LOG_LEVEL", "info")

	return cfg
}

// Validate checks essential configurations.
func (c *Config) Validate() error {
	if c.Fund.Wallet == "" {
		return fmt.Errorf("DONATION_WALLET_ADDRESS is required")
	}
	if c.Mezon.BotID == "" {
		return fmt.Errorf("MEZON_BOT_ID is required")
	}
	if c.Mezon.Token == "" {
		return fmt.Errorf("MEZON_TOKEN is required")
	}
	if c.Mezon.ChannelID == "" && c.Mezon.ThreadID == "" {
		return fmt.Errorf("at least one of MEZON_CHANNEL_ID or MEZON_THREAD_ID is required")
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func intEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func int64Env(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
