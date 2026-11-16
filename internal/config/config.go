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
	Fund      FundConfig
	Mezon     MezonConfig
	App       AppConfig
	BotWallet BotWalletConfig
	Sport     SportConfig
	VinhHub   VinhHubConfig

	DotEnvPath string
	DotEnvErr  error
}

// VinhHubConfig holds settings for the VinhHub energy hub menu.
type VinhHubConfig struct {
	DefaultImage string
	StatePath    string
}

// SportConfig holds settings for badminton attendance and match reminders.
type SportConfig struct {
	Enabled       bool
	ClanID        string
	ChannelID     string
	RoleID        string
	RoleName      string
	StatePath     string
	OpenAIKey     string
	OpenAIBaseURL string
	OpenAIModel   string
	GeminiKey     string
	Cong1EmojiID  string
	Tru1EmojiID   string
	PaymentQR     string
}

// BotWalletConfig holds settings for the intermediary bot wallet.
type BotWalletConfig struct {
	Address    string
	PrivateKey string
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

	// Bot Wallet (Intermediary)
	cfg.BotWallet.Address = strings.TrimSpace(env("BOT_WALLET_ADDRESS", ""))
	cfg.BotWallet.PrivateKey = strings.TrimSpace(env("BOT_PRIVATE_KEY", ""))

	// Sport (Badminton)
	cfg.Sport.Enabled = boolEnv("SPORT_ENABLED", true)
	cfg.Sport.ClanID = strings.TrimSpace(env("SPORT_CLAN_ID", "1779484504377790464"))
	cfg.Sport.ChannelID = strings.TrimSpace(env("SPORT_CHANNEL_ID", "1840689219983380480"))
	cfg.Sport.RoleID = strings.TrimSpace(env("SPORT_ROLE_ID", "1840693979155271680"))
	cfg.Sport.RoleName = strings.TrimSpace(env("SPORT_ROLE_NAME", "Vinh-CL"))
	cfg.Sport.StatePath = env("SPORT_STATE_PATH", "badminton_state.json")
	cfg.Sport.OpenAIKey = strings.TrimSpace(env("OPENAI_API_KEY", ""))
	cfg.Sport.OpenAIBaseURL = strings.TrimSpace(env("OPENAI_BASE_URL", "https://llm.mrdnd.dev/v1"))
	cfg.Sport.OpenAIModel = strings.TrimSpace(env("OPENAI_MODEL", "gpt-4o-mini"))
	cfg.Sport.GeminiKey = strings.TrimSpace(env("GEMINI_API_KEY", ""))
	cfg.Sport.Cong1EmojiID = strings.TrimSpace(env("SPORT_CONG1_EMOJI_ID", "7239659508431615752"))
	cfg.Sport.Tru1EmojiID = strings.TrimSpace(env("SPORT_TRU1_EMOJI_ID", "7294564276005201529"))
	cfg.Sport.PaymentQR = strings.TrimSpace(env("SPORT_PAYMENT_QR", "https://cdn.mezon.ai/198b7117db001000/2122180489040105472_pasted_image_1794802215552_0.png?1794803450-2Zo8db8MRVnVm9qwMzSilZzXa2%2F1v2WP%2FSKoY6TpJHI%3D"))

	// VinhHub (Energy Hub)
	cfg.VinhHub.DefaultImage = env("VINHHUB_DEFAULT_IMAGE", "https://cdn.mezon.ai/1994479556801000/2121182274450886656_SG_Energy_Hub_3.png?1794795387-WzdD7%2B%2BoP9%2F%2FyK5Q%2BnpIXfGobTpgQyfZ8sasb74aXCc%3D")
	cfg.VinhHub.StatePath = env("VINHHUB_STATE_PATH", "vinhhub_state.json")

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
