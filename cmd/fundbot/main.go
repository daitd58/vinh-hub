package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"vinh-hub/internal/bot"
	"vinh-hub/internal/config"
	"vinh-hub/internal/courier"
	"vinh-hub/internal/fund"
	"vinh-hub/internal/outbox"
	"vinh-hub/internal/sport"
	"vinh-hub/internal/vinhhub"
)

// outboxAdapter adapts outbox.Outbox to fund.Queue.
type outboxAdapter struct {
	box outbox.Outbox
}

func (a *outboxAdapter) Enqueue(ctx context.Context, kind, channelID, threadID, body, dedupeKey string) error {
	return a.box.Enqueue(ctx, outbox.Item{
		Kind:      kind,
		ChannelID: channelID,
		ThreadID:  threadID,
		Body:      body,
		DedupeKey: dedupeKey,
	})
}

func main() {
	var (
		flagQuy     bool
		flagTestMsg string
		flagDryRun  bool
	)

	flag.BoolVar(&flagQuy, "quy", false, "fetch and print the current fund balance, then exit")
	flag.StringVar(&flagTestMsg, "test-msg", "", "send a test message to the configured thread/channel, then exit")
	flag.BoolVar(&flagDryRun, "dry-run", false, "start in dry-run mode (without connecting to Mezon or node)")
	flag.Parse()

	cfg := config.Load()

	// Logger setup
	logLevel := slog.LevelInfo
	if cfg.App.LogLevel == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	if cfg.DotEnvPath != "" {
		logger.Info("loaded environment configuration", "file", cfg.DotEnvPath)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initialize fund balance reader with primary node source and indexer fallback
	var fundReader *fund.Reader
	var fundIndexer *fund.Indexer
	if cfg.Fund.Wallet != "" {
		var src fund.Source
		var nodeSrc *fund.NodeSource
		var indexerSrc fund.Source

		if cfg.Fund.NodeEndpoint != "" {
			nodeSrc = fund.NewNodeSource(cfg.Fund.NodeEndpoint, cfg.Fund.NodeTLS)
			defer nodeSrc.Close()
		}
		if cfg.Fund.ApiURL != "" {
			fundIndexer = fund.NewIndexer(cfg.Fund.ApiURL, cfg.Fund.ChainID, cfg.Fund.Decimals, nil)
			indexerSrc = fundIndexer
		}

		if nodeSrc != nil && indexerSrc != nil {
			src = &fund.FallbackSource{
				Primary:   nodeSrc,
				Secondary: indexerSrc,
			}
		} else if nodeSrc != nil {
			src = nodeSrc
		} else if indexerSrc != nil {
			src = indexerSrc
		}

		if src != nil {
			fundReader = fund.New(fund.Options{
				Source:   src,
				Wallet:   cfg.Fund.Wallet,
				Decimals: cfg.Fund.Decimals,
				TTL:      10 * time.Second,
			})
		}
	}

	// -quy flag: inspect fund balance
	if flagQuy {
		if fundReader == nil {
			logger.Error("fund reader is not configured: check DONATION_WALLET_ADDRESS and MEZON_MAINNET_API_URL")
			os.Exit(1)
		}
		bal, err := fundReader.Balance(ctx)
		if err != nil {
			logger.Error("could not read fund balance", "err", err)
			os.Exit(1)
		}
		fmt.Println(bot.FormatBalanceReport(bal, cfg.Fund.Timezone))
		return
	}

	if flagDryRun {
		logger.Info("running in dry-run mode; skipping external network connections")
		if fundReader != nil {
			if bal, err := fundReader.Balance(ctx); err == nil {
				logger.Info("fund balance read successfully",
					"balance_vnd", bal.BalanceVND)
			}
		}
		return
	}

	if err := cfg.Validate(); err != nil {
		logger.Error("configuration validation failed", "err", err)
		os.Exit(1)
	}

	wallets, err := fund.LoadUserWalletMap(cfg.Fund.WalletsPath)
	if err != nil {
		logger.Warn("could not load user wallet mapping file; using defaults", "err", err)
		wallets = fund.DefaultWallets
	} else {
		logger.Info("loaded user wallet map", "count", len(wallets.All()))
	}

	// Initialize Mezon bot
	mezonBot, err := bot.New(bot.Config{
		BotID:            cfg.Mezon.BotID,
		Token:            cfg.Mezon.Token,
		Host:             cfg.Mezon.Host,
		Port:             cfg.Mezon.Port,
		UseSSL:           cfg.Mezon.UseSSL,
		DefaultChannelID: cfg.Mezon.ChannelID,
		DefaultThreadID:  cfg.Mezon.ThreadID,
		ThreadName:       cfg.Mezon.ThreadName,
		SportClanID:      cfg.Sport.ClanID,
		SportChannelID:   cfg.Sport.ChannelID,
		SportCong1EmojiID: cfg.Sport.Cong1EmojiID,
		SportTru1EmojiID:  cfg.Sport.Tru1EmojiID,
		SportCheckEmojiID: cfg.Sport.CheckEmojiID,
		FundReader:       fundReader,
		FundIndexer:      fundIndexer,
		FundWallet:       cfg.Fund.Wallet,
		Wallets:          wallets,
		Timezone:         cfg.Fund.Timezone,
		Log:              logger,
	})
	if err != nil {
		logger.Error("initialize mezon bot failed", "err", err)
		os.Exit(1)
	}
	defer mezonBot.Close()

	if err := mezonBot.Start(); err != nil {
		logger.Error("mezon bot login failed", "err", err)
		os.Exit(1)
	}

	// -test-msg flag: send a single test message and exit
	if flagTestMsg != "" {
		logger.Info("sending test message to thread/channel", "msg", flagTestMsg)
		if err := mezonBot.SendToThread(ctx, cfg.Mezon.ChannelID, cfg.Mezon.ThreadID, flagTestMsg); err != nil {
			logger.Error("send test message failed", "err", err)
			os.Exit(1)
		}
		logger.Info("test message sent successfully")
		return
	}

	// Setup outbox and courier
	box := outbox.NewMemoryOutbox()
	cr := &courier.Courier{
		Outbox:   box,
		Sender:   mezonBot,
		Log:      logger,
		Interval: cfg.App.CourierInterval,
	}

	var wg sync.WaitGroup

	// Run Courier
	wg.Add(1)
	go func() {
		defer wg.Done()
		cr.Run(ctx)
	}()
	logger.Info("courier started", "interval", cfg.App.CourierInterval)

	// Run Badminton & Sport Scheduler
	if cfg.Sport.Enabled {
		sportMgr := sport.NewManager(sport.Config{
			Enabled:       cfg.Sport.Enabled,
			ClanID:        cfg.Sport.ClanID,
			ChannelID:     cfg.Sport.ChannelID,
			RoleID:        cfg.Sport.RoleID,
			RoleName:      cfg.Sport.RoleName,
			StatePath:     cfg.Sport.StatePath,
			OpenAIKey:     cfg.Sport.OpenAIKey,
			OpenAIBaseURL: cfg.Sport.OpenAIBaseURL,
			OpenAIModel:   cfg.Sport.OpenAIModel,
			GeminiKey:     cfg.Sport.GeminiKey,
			PaymentQR:     cfg.Sport.PaymentQR,
			Timezone:      cfg.Fund.Timezone,
			Log:           logger,
		}, mezonBot)

		mezonBot.SetSportManager(sportMgr)

		wg.Add(1)
		go func() {
			defer wg.Done()
			sportMgr.Run(ctx)
		}()
		logger.Info("badminton manager initialized and running",
			"channel_id", cfg.Sport.ChannelID, "role", cfg.Sport.RoleName)
	}

	// Initialize VinhHub (Energy Hub) Manager
	vinhHubMgr := vinhhub.NewManager(cfg.VinhHub.DefaultImage, cfg.VinhHub.StatePath, logger)
	mezonBot.SetVinhHubManager(vinhHubMgr)
	logger.Info("vinhhub manager initialized", "default_image", cfg.VinhHub.DefaultImage, "state_path", cfg.VinhHub.StatePath)

	// Run Fund Stream
	if cfg.Fund.NodeEndpoint != "" && cfg.Fund.Wallet != "" {
		destDesc := cfg.Mezon.ThreadID
		if destDesc == "" {
			destDesc = cfg.Mezon.ChannelID
		}

		// Initialize Bot Intermediary Forwarder:
		// Priority 1: If BOT_PRIVATE_KEY is explicitly configured -> Key mode (TxTypeTransferByKey)
		// Priority 2: Native Mezon ZK Forwarder (no private key needed, uses bot ID & bot token like mezon-sdk JS)
		var forwarder *fund.Forwarder
		botWalletAddr := cfg.BotWallet.Address
		if cfg.BotWallet.PrivateKey != "" {
			fwd, err := fund.NewForwarder(
				cfg.Fund.NodeEndpoint,
				cfg.Fund.NodeTLS,
				cfg.Fund.Wallet,
				cfg.BotWallet.PrivateKey,
				botWalletAddr,
				wallets,
				logger,
			)
			if err != nil {
				logger.Error("failed to initialize key-based bot forwarder", "err", err)
			} else {
				forwarder = fwd
				defer forwarder.Close()
				botWalletAddr = forwarder.BotAddress()
				logger.Info("bot forwarder initialized (Key mode)", "bot_wallet", botWalletAddr, "fund_wallet", cfg.Fund.Wallet)
			}
		} else if cfg.Mezon.BotID != "" && cfg.Mezon.Token != "" {
			zkFwd, err := fund.NewZKForwarder(
				cfg.Fund.NodeEndpoint,
				cfg.Fund.NodeTLS,
				"", // default ZK API: https://dong.mezon.ai/zk-api/prove
				cfg.Mezon.Host,
				cfg.Mezon.BotID,
				cfg.Mezon.Token,
				cfg.Fund.Wallet,
				botWalletAddr,
				wallets,
				logger,
			)
			if err != nil {
				logger.Error("failed to initialize native ZK bot forwarder", "err", err)
			} else {
				forwarder = zkFwd
				defer forwarder.Close()
				botWalletAddr = forwarder.BotAddress()
				logger.Info("bot forwarder initialized (Native Mezon ZK mode - no private key required)",
					"bot_wallet", botWalletAddr, "fund_wallet", cfg.Fund.Wallet)
			}
		} else if botWalletAddr != "" {
			logger.Warn("BOT_WALLET_ADDRESS is set, but auto-forwarding credentials are missing",
				"bot_wallet", botWalletAddr)
		}

		stream := &fund.Stream{
			Endpoint:   cfg.Fund.NodeEndpoint,
			UseTLS:     cfg.Fund.NodeTLS,
			Address:    cfg.Fund.Wallet,
			BotAddress: botWalletAddr,
			Forwarder:  forwarder,
			ChannelID:  cfg.Mezon.ChannelID,
			ThreadID:   cfg.Mezon.ThreadID,
			Decimals:   cfg.Fund.Decimals,
			Queue:      &outboxAdapter{box: box},
			Log:        logger,
			Wallets:    wallets,
			Timezone:   cfg.Fund.Timezone,
		}
		if fundReader != nil {
			stream.Balance = fundReader
			if initBal, err := fundReader.FreshBalance(ctx); err == nil {
				stream.SetLastBalance(initBal.BalanceVND)
				logger.Info("initialized fund stream with current balance", "balance_vnd", initBal.BalanceVND)
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			stream.Run(ctx)
		}()
		logger.Info("fund stream worker launched", "endpoint", cfg.Fund.NodeEndpoint, "destination", destDesc)
	}

	logger.Info("fund bot is running; press Ctrl+C to stop")
	<-ctx.Done()
	logger.Info("shutting down fund bot...")
	wg.Wait()
	logger.Info("all workers stopped cleanly")
}
