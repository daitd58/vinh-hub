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
	"github.com/quangledang23/mezon-sdk-go/api"

	"vinh-hub/internal/fund"
	"vinh-hub/internal/sport"
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
	SportChannelID   string
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
	sportMgr   *sport.Manager
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

	c.On(mezon.EventTokenSend, func(p any) {
		if ev, ok := p.(*api.TokenSentEvent); ok {
			b.logger().Info("mezon token transfer received via websocket",
				"sender_id", ev.GetSenderId(),
				"sender_name", ev.GetSenderName(),
				"amount", ev.GetAmount(),
				"note", ev.GetNote(),
				"tx_id", ev.GetTransactionId(),
			)
		}
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

	sportChanID := b.cfg.SportChannelID
	if sportChanID != "" && sportChanID != chanID && sportChanID != thID {
		if ch, err := b.client.Channels.Fetch(sportChanID); err == nil && ch != nil {
			clanID := "0"
			if ch.Clan != nil {
				clanID = ch.Clan.ID
			}
			_, _ = b.client.Socket().JoinChat(clanID, ch.ID, ch.ChannelType, !ch.IsPrivate)
			b.logger().Info("joined configured sport channel", "sport_channel_id", sportChanID)
		}
	}
}

// SetSportManager attaches the sport/badminton manager to the bot.
func (b *Bot) SetSportManager(mgr *sport.Manager) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sportMgr = mgr
}

// SendToChannel sends a message with optional mentions directly to a channel.
func (b *Bot) SendToChannel(ctx context.Context, channelID string, text string, opts *mezon.SendOptions) error {
	ch, err := b.client.Channels.Fetch(channelID)
	if err != nil {
		return fmt.Errorf("fetch channel %s: %w", channelID, err)
	}
	clanID := "0"
	if ch.Clan != nil {
		clanID = ch.Clan.ID
	}
	_, _ = b.client.Socket().JoinChat(clanID, ch.ID, ch.ChannelType, !ch.IsPrivate)

	if _, err := ch.Send(mezon.Text(text), opts); err != nil {
		return fmt.Errorf("send to channel %s: %w", channelID, err)
	}
	return nil
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

	b.mu.RLock()
	sportMgr := b.sportMgr
	sportChanID := b.cfg.SportChannelID
	b.mu.RUnlock()

	// Record sport channel messages for badminton attendance & AI summary
	if sportMgr != nil && m.ChannelID == sportChanID {
		sportMgr.RecordMessage(m.ChannelID, m.ID, m.SenderID, m.Username, text)
	}

	cmd := strings.ToLower(text)
	switch {
	case cmd == "*quy" || cmd == "*fund" || cmd == "/quy" || cmd == "!quy":
		b.handleFundCommand(m)
	case cmd == "*help" || cmd == "/help" || cmd == "!help":
		b.handleHelpCommand(m)
	case cmd == "*diemdanh" || cmd == "*vinhhub diemdanh" || strings.HasPrefix(cmd, "*diemdanh"):
		b.handleDiemDanhCommand(m)
	case cmd == "*nhaclich" || cmd == "*vinhhub nhaclich":
		b.handleNhacLichCommand(m)
	case cmd == "*summary" || cmd == "*vinhhub summary":
		b.handleSummaryCommand(m)
	}
}

func (b *Bot) handleDiemDanhCommand(m *mezon.ChannelMessage) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng điểm danh chưa được cấu hình.")
		return
	}
	if err := sportMgr.TriggerDiemDanh(context.Background(), time.Now()); err != nil {
		b.logger().Error("trigger diemdanh failed", "err", err)
		_ = b.reply(m, fmt.Sprintf("Lỗi gửi điểm danh: %v", err))
		return
	}
	_ = b.reply(m, "Đã gửi thông báo điểm danh cho buổi đánh tiếp theo! 🏸")
}

func (b *Bot) handleNhacLichCommand(m *mezon.ChannelMessage) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}
	if err := sportMgr.TriggerReminder(context.Background(), time.Now()); err != nil {
		b.logger().Error("trigger nhaclich failed", "err", err)
		_ = b.reply(m, fmt.Sprintf("Lỗi nhắc lịch: %v", err))
		return
	}
	_ = b.reply(m, "Đã gửi nhắc lịch kèm tổng kết điểm danh! 🏸")
}

func (b *Bot) handleSummaryCommand(m *mezon.ChannelMessage) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}
	sum := sportMgr.GetSummary(context.Background())
	_ = b.reply(m, sum)
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
	help := "🤖 **Mezon Bot Hub**\n\n" +
		"💰 **Quỹ & Tài chính:**\n" +
		"• `*quy`: Xem số dư hiện tại của quỹ\n\n" +
		"🏸 **Cầu lông & Thể thao:**\n" +
		"• `*diemdanh`: Gửi thông báo điểm danh cho buổi đánh cầu tiếp theo\n" +
		"• `*nhaclich`: Nhắc lịch trước giờ đánh & AI tóm tắt ai đi ai nghỉ, sân nào\n" +
		"• `*summary`: Xem nhanh danh sách điểm danh và sân bãi hiện tại\n\n" +
		"• `*help`: Hiển thị hướng dẫn này"
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
