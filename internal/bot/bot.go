package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	mezon "github.com/quangledang23/mezon-sdk-go"

	"vinh-hub/internal/fund"
)

// Config configures the Mezon bot.
type Config struct {
	BotID            string
	Token            string
	Host             string
	Port             string
	UseSSL           bool
	DefaultChannelID string
	DefaultThreadID  string
	ThreadName       string
	FundReader       fund.BalanceReader
	Timezone         *time.Location
	Log              *slog.Logger
}

// Bot wraps the Mezon client and provides thread-aware message delivery.
type Bot struct {
	client     *mezon.MezonClient
	cfg        Config
	mu         sync.RWMutex
	threadID   string
	channelID  string
	fundReader fund.BalanceReader
}

// New creates and initializes a Mezon bot instance.
func New(cfg Config) (*Bot, error) {
	if cfg.BotID == "" {
		return nil, fmt.Errorf("bot ID is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("bot token is required")
	}

	host := cfg.Host
	if host == "" {
		host = mezon.DefaultHost
	}
	port := cfg.Port
	if port == "" {
		port = mezon.DefaultPort
	}

	clientCfg := mezon.ClientConfig{
		BotID:  cfg.BotID,
		Token:  cfg.Token,
		Host:   host,
		Port:   port,
		UseSSL: &cfg.UseSSL,
	}

	c, err := mezon.NewMezonClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("create mezon client: %w", err)
	}

	b := &Bot{
		client:     c,
		cfg:        cfg,
		threadID:   cfg.DefaultThreadID,
		channelID:  cfg.DefaultChannelID,
		fundReader: cfg.FundReader,
	}

	c.OnReady(func() {
		b.logger().Info("mezon bot ready", "bot_id", cfg.BotID, "host", host)
	})

	c.OnChannelMessage(func(m *mezon.ChannelMessage) {
		b.handleMessage(m)
	})

	return b, nil
}

// Start logs into the Mezon network and sets up the socket connection.
func (b *Bot) Start() error {
	if err := b.client.Login(); err != nil {
		return fmt.Errorf("mezon login: %w", err)
	}
	b.logger().Info("logged into mezon successfully")

	// Ensure destination thread or channel is joined
	b.ensureJoined()

	return nil
}

// Close disconnects the bot from Mezon.
func (b *Bot) Close() {
	if b.client != nil {
		b.client.Close()
	}
}

// ensureJoined joins target channel and thread so messages and events can flow.
func (b *Bot) ensureJoined() {
	b.mu.RLock()
	chanID := b.channelID
	thID := b.threadID
	b.mu.RUnlock()

	if thID != "" {
		if ch, err := b.client.Channels.Fetch(thID); err == nil && ch != nil {
			clanID := "0"
			if ch.Clan != nil {
				clanID = ch.Clan.ID
			}
			_, _ = b.client.Socket().JoinChat(clanID, ch.ID, ch.ChannelType, !ch.IsPrivate)
			b.logger().Info("joined configured thread", "thread_id", thID)
		}
	}

	if chanID != "" {
		if ch, err := b.client.Channels.Fetch(chanID); err == nil && ch != nil {
			clanID := "0"
			if ch.Clan != nil {
				clanID = ch.Clan.ID
			}
			_, _ = b.client.Socket().JoinChat(clanID, ch.ID, ch.ChannelType, !ch.IsPrivate)
			b.logger().Info("joined configured channel", "channel_id", chanID)
		}
	}
}

// SendToThread sends a message to a thread or fallback channel.
func (b *Bot) SendToThread(ctx context.Context, channelID, threadID, text string) error {
	b.mu.RLock()
	if threadID == "" {
		threadID = b.threadID
	}
	if channelID == "" {
		channelID = b.channelID
	}
	b.mu.RUnlock()

	content := mezon.Text(text)

	// Case 1: Thread ID is provided directly.
	if threadID != "" {
		// Attempt to fetch thread as a channel
		threadChan, err := b.client.Channels.Fetch(threadID)
		if err == nil && threadChan != nil {
			clanID := "0"
			if threadChan.Clan != nil {
				clanID = threadChan.Clan.ID
			}
			_, _ = b.client.Socket().JoinChat(clanID, threadChan.ID, threadChan.ChannelType, !threadChan.IsPrivate)

			_, sendErr := threadChan.Send(content, &mezon.SendOptions{TopicID: threadID})
			if sendErr == nil {
				return nil
			}
			b.logger().Warn("sending directly to thread channel failed; trying via parent channel",
				"thread_id", threadID, "err", sendErr)
		}

		// If fetching or sending to thread directly failed, try sending via parent channel
		if channelID != "" {
			parentChan, err := b.client.Channels.Fetch(channelID)
			if err == nil && parentChan != nil {
				clanID := "0"
				if parentChan.Clan != nil {
					clanID = parentChan.Clan.ID
				}
				_, _ = b.client.Socket().JoinChat(clanID, parentChan.ID, parentChan.ChannelType, !parentChan.IsPrivate)

				_, sendErr := parentChan.Send(content, &mezon.SendOptions{TopicID: threadID})
				if sendErr == nil {
					return nil
				}
				return fmt.Errorf("send to parent channel %s with topic %s: %w", channelID, threadID, sendErr)
			}
		}
	}

	// Case 2: No thread ID, but thread name and channel ID are configured: try to find or create thread
	if threadID == "" && b.cfg.ThreadName != "" && channelID != "" {
		parentChan, err := b.client.Channels.Fetch(channelID)
		if err == nil && parentChan != nil && parentChan.Clan != nil {
			// Create thread under parent channel
			createdDesc, createErr := parentChan.Clan.CreateChannel(mezon.CreateChannelData{
				Label:    b.cfg.ThreadName,
				Type:     int(mezon.ChannelTypeThread),
				ParentID: channelID,
			})
			if createErr == nil && createdDesc != nil && createdDesc.ChannelId != 0 {
				newThreadID := strconv.FormatInt(createdDesc.ChannelId, 10)
				b.mu.Lock()
				b.threadID = newThreadID
				b.mu.Unlock()
				b.logger().Info("created new thread for fund feed", "thread_id", newThreadID, "label", b.cfg.ThreadName)
				return b.SendToThread(ctx, channelID, newThreadID, text)
			}
			b.logger().Warn("could not auto-create thread", "err", createErr)
		}
	}

	// Case 3: Send directly to channel
	if channelID != "" {
		ch, err := b.client.Channels.Fetch(channelID)
		if err != nil {
			return fmt.Errorf("fetch channel %s: %w", channelID, err)
		}
		clanID := "0"
		if ch.Clan != nil {
			clanID = ch.Clan.ID
		}
		_, _ = b.client.Socket().JoinChat(clanID, ch.ID, ch.ChannelType, !ch.IsPrivate)

		if _, err := ch.Send(content, nil); err != nil {
			return fmt.Errorf("send to channel %s: %w", channelID, err)
		}
		return nil
	}

	return fmt.Errorf("neither thread nor channel destination is available")
}

// handleMessage inspects inbound messages for commands like *quy.
func (b *Bot) handleMessage(m *mezon.ChannelMessage) {
	if m == nil || m.SenderID == b.cfg.BotID {
		return
	}

	text := strings.TrimSpace(m.ContentText())
	switch strings.ToLower(text) {
	case "*quy", "*fund", "/quy", "!quy":
		b.handleFundCommand(m)
	case "*help", "/help", "!help":
		b.handleHelpCommand(m)
	}
}

func (b *Bot) handleFundCommand(m *mezon.ChannelMessage) {
	if b.fundReader == nil {
		_ = b.reply(m, "Quỹ chưa được cấu hình tra cứu số dư tự động.")
		return
	}

	bal, err := b.fundReader.Balance(context.Background())
	if err != nil {
		b.logger().Warn("failed to read fund balance for command", "err", err)
		_ = b.reply(m, "Hiện không thể truy vấn số dư quỹ từ hệ thống. Vui lòng thử lại sau.")
		return
	}

	report := FormatBalanceReport(bal, b.cfg.Timezone)
	_ = b.reply(m, report)
}

func (b *Bot) handleHelpCommand(m *mezon.ChannelMessage) {
	help := "🤖 **Mezon Fund Bot**\n" +
		"• `*quy`: Xem số dư hiện tại của quỹ\n" +
		"• `*help`: Hiển thị hướng dẫn này\n\n" +
		"Bot tự động cập nhật mọi biến động nhận/chi của quỹ vào luồng này theo thời gian thực."
	_ = b.reply(m, help)
}

func (b *Bot) reply(m *mezon.ChannelMessage, text string) error {
	content := mezon.Text(text)
	channelID := m.ChannelID
	topicID := m.TopicID

	// Reply to same thread or channel
	if topicID != "" {
		return b.SendToThread(context.Background(), channelID, topicID, text)
	}

	ch, err := b.client.Channels.Fetch(channelID)
	if err != nil {
		return err
	}
	_, err = ch.Send(content, &mezon.SendOptions{
		References: []mezon.MessageRef{{
			MessageRefID:    m.ID,
			MessageSenderID: m.SenderID,
		}},
	})
	return err
}

// FormatBalanceReport formats a balance report for interactive commands.
func FormatBalanceReport(b fund.Balance, loc *time.Location) string {
	t := b.At
	if loc != nil {
		t = t.In(loc)
	}

	return fmt.Sprintf("Thông tin Quỹ\n• Quỹ hiện có: %s VNĐ\nCập nhật lúc: %s",
		fund.FormatVND(b.BalanceVND),
		t.Format("15:04 02/01/2006"),
	)
}

func (b *Bot) logger() *slog.Logger {
	if b.cfg.Log != nil {
		return b.cfg.Log
	}
	return slog.Default()
}
