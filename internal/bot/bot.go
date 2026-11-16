package bot

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mezon "github.com/quangledang23/mezon-sdk-go"
	"github.com/quangledang23/mezon-sdk-go/api"

	"vinh-hub/internal/fund"
	"vinh-hub/internal/sport"
	"vinh-hub/internal/vinhhub"
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
	SportClanID      string
	SportChannelID   string
	SportCong1EmojiID string
	SportTru1EmojiID  string
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
	vinhHubMgr *vinhhub.Manager
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

	c.On(mezon.EventMessageReaction, func(p any) {
		if ev, ok := p.(*api.MessageReaction); ok {
			b.handleMessageReaction(ev)
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

// SetVinhHubManager attaches the VinhHub manager and sets up the socket CDN signer.
func (b *Bot) SetVinhHubManager(mgr *vinhhub.Manager) {
	b.mu.Lock()
	b.vinhHubMgr = mgr
	b.mu.Unlock()
	if mgr != nil {
		mgr.SetSigner(func(rawURL string) (string, error) {
			return vinhhub.SignCDNURL(b.client, rawURL)
		})
	}
}

// SendToChannel sends a message with optional mentions directly to a channel,
// converting markdown bold tags (**) into Mezon `mk` spans and ensuring role mentions are highlighted.
func (b *Bot) SendToChannel(ctx context.Context, channelID string, text string, opts *mezon.SendOptions) (string, error) {
	ch, err := b.client.Channels.Fetch(channelID)
	if err != nil {
		return "", fmt.Errorf("fetch channel %s: %w", channelID, err)
	}

	clanID := b.cfg.SportClanID
	if clanID == "" && ch.Clan != nil {
		clanID = ch.Clan.ID
	}
	if clanID == "" {
		clanID = "0"
	}

	// Ensure clan is attached to channel so WriteChatMessage sends the proper ClanId for role mentions
	if clanID != "0" && (ch.Clan == nil || ch.Clan.ID == "" || ch.Clan.ID == "0") {
		if clan, ok := b.client.Clans.Get(clanID); ok && clan != nil {
			ch.Clan = clan
		}
	}

	_, _ = b.client.Socket().JoinChat(clanID, ch.ID, ch.ChannelType, !ch.IsPrivate)

	cleanText, mk := ParseMarkdownSpans(text)

	// If there are mentions, update their offsets based on cleanText
	if opts != nil && len(opts.Mentions) > 0 {
		for i := range opts.Mentions {
			tag := ""
			if opts.Mentions[i].Rolename != "" {
				tag = "@" + opts.Mentions[i].Rolename
			} else if opts.Mentions[i].Username != "" {
				tag = "@" + opts.Mentions[i].Username
			}
			if tag != "" {
				if s, e, ok := mezon.MentionSpan(cleanText, tag); ok {
					opts.Mentions[i].S = s
					opts.Mentions[i].E = e
				}
			}
		}
	}

	var content mezon.Content
	if len(mk) > 0 {
		content = map[string]any{
			"t":  cleanText,
			"mk": mk,
		}
	} else {
		content = mezon.Text(cleanText)
	}

	sentMsg, err := ch.Send(content, opts)
	if err != nil {
		return "", fmt.Errorf("send to channel %s: %w", channelID, err)
	}
	if sentMsg != nil {
		return sentMsg.ID, nil
	}
	return "", nil
}

// ReactToMessage adds a reaction (e.g. :cong1: or :tru1:) to a message in the channel.
func (b *Bot) ReactToMessage(ctx context.Context, channelID, messageID, emoji string) error {
	ch, err := b.client.Channels.Fetch(channelID)
	if err != nil {
		return err
	}

	clanID := b.cfg.SportClanID
	if clanID == "" && ch.Clan != nil {
		clanID = ch.Clan.ID
	}
	if clanID == "" {
		clanID = "0"
	}

	// Ensure clan is attached to channel so socket operations have proper clan context
	if clanID != "0" && (ch.Clan == nil || ch.Clan.ID == "" || ch.Clan.ID == "0") {
		if clan, ok := b.client.Clans.Get(clanID); ok && clan != nil {
			ch.Clan = clan
		}
	}

	// Must join channel before reacting
	_, _ = b.client.Socket().JoinChat(clanID, ch.ID, ch.ChannelType, !ch.IsPrivate)

	mode := int(mezon.ConvertChannelTypeToChannelMode(ch.ChannelType))
	isPublic := !ch.IsPrivate

	emojiID := "0"
	cleanEmoji := strings.ToLower(strings.Trim(emoji, ":"))
	if strings.Contains(cleanEmoji, "cong1") || cleanEmoji == "+1" {
		emojiID = b.cfg.SportCong1EmojiID
		if emojiID == "" {
			emojiID = "7239659508431615752"
		}
		emoji = ":cong1:"
	} else if strings.Contains(cleanEmoji, "tru1") || cleanEmoji == "-1" {
		emojiID = b.cfg.SportTru1EmojiID
		if emojiID == "" {
			emojiID = "7294564276005201529"
		}
		emoji = ":tru1:"
	}

	data := mezon.ReactMessageData{
		ClanID:       clanID,
		ChannelID:    channelID,
		ChannelType:  ch.ChannelType,
		Mode:         mode,
		IsPublic:     isPublic,
		MessageID:    messageID,
		EmojiID:      emojiID,
		Emoji:        emoji,
		Count:        1,
		ActionDelete: false,
	}

	_, err = b.client.Socket().WriteMessageReaction(data)
	return err
}

func (b *Bot) handleMessageReaction(ev *api.MessageReaction) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	sportChan := b.cfg.SportChannelID
	botID := b.cfg.BotID
	cong1ID := b.cfg.SportCong1EmojiID
	tru1ID := b.cfg.SportTru1EmojiID
	b.mu.RUnlock()

	if sportMgr == nil {
		return
	}

	chanID := strconv.FormatInt(ev.ChannelId, 10)
	if sportChan != "" && chanID != sportChan {
		return
	}

	senderID := strconv.FormatInt(ev.SenderId, 10)
	// Ignore reactions from the bot itself (e.g. initial seeding)
	if botID != "" && senderID == botID {
		return
	}

	msgID := strconv.FormatInt(ev.MessageId, 10)
	senderName := ev.SenderName
	if senderName == "" {
		if u, err := b.client.Users.Fetch(senderID); err == nil && u != nil && u.Username != "" {
			senderName = u.Username
		} else {
			senderName = senderID
		}
	}

	emojiVal := ev.Emoji
	if ev.EmojiId != 0 {
		emojiIDStr := strconv.FormatInt(ev.EmojiId, 10)
		if emojiIDStr == cong1ID || ev.EmojiId == 7239659508431615752 {
			emojiVal = ":cong1:"
		} else if emojiIDStr == tru1ID || ev.EmojiId == 7294564276005201529 {
			emojiVal = ":tru1:"
		}
	}

	// Check if this reaction is on the active split/money bill message
	if sportMgr.IsCurrentBillMessage(msgID) {
		cleanEmoji := strings.ToLower(strings.Trim(emojiVal, ":"))
		isPaidEmoji := cleanEmoji == "white_check_mark" || cleanEmoji == "check" || cleanEmoji == "✅" ||
			cleanEmoji == "heavy_check_mark" || cleanEmoji == "✔" || cleanEmoji == "ballot_box_with_check" || cleanEmoji == "☑" ||
			cleanEmoji == "💵" || cleanEmoji == "👍" || cleanEmoji == "+1" || cleanEmoji == "moneybag" || cleanEmoji == "money_with_wings" || cleanEmoji == "ok_hand" || cleanEmoji == "👌"
		if isPaidEmoji {
			if !ev.Action {
				if item, ok := sportMgr.MarkPaidByUserIDOrName(senderID, senderName); ok {
					b.logger().Info("marked user as paid via reaction", "user", item.Username, "msg_id", msgID)
				}
			} else {
				if _, _, err := sportMgr.MarkUnpaidBatch([]string{senderName}); err == nil {
					b.logger().Info("unmarked user paid via reaction removal", "user", senderName, "msg_id", msgID)
				}
			}
			return
		}
	}

	sportMgr.RecordReaction(chanID, msgID, senderID, senderName, emojiVal, ev.Action, int(ev.Count))
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
	case cmd == "*vinhhub" || cmd == "!vinhhub" || cmd == "/vinhhub":
		b.handleVinhHubCommand(m)
	case strings.HasPrefix(cmd, "*register vinhhub") || strings.HasPrefix(cmd, "!register vinhhub") || strings.HasPrefix(cmd, "/register vinhhub") ||
		strings.HasPrefix(cmd, "*vinhhub register") || strings.HasPrefix(cmd, "!vinhhub register") || strings.HasPrefix(cmd, "/vinhhub register"):
		b.handleRegisterVinhHubCommand(m, text)
	case cmd == "*delete vinhhub" || cmd == "!delete vinhhub" || cmd == "/delete vinhhub" ||
		cmd == "*vinhhub delete" || cmd == "!vinhhub delete" || cmd == "/vinhhub delete":
		b.handleDeleteVinhHubCommand(m)
	case cmd == "*diemdanh" || cmd == "*vinhhub diemdanh" || strings.HasPrefix(cmd, "*diemdanh"):
		b.handleDiemDanhCommand(m)
	case cmd == "*nhaclich" || cmd == "*vinhhub nhaclich":
		b.handleNhacLichCommand(m)
	case cmd == "*summary" || cmd == "*vinhhub summary":
		b.handleSummaryCommand(m)
	case strings.HasPrefix(cmd, "*split") || strings.HasPrefix(cmd, "!split") || strings.HasPrefix(cmd, "/split"):
		b.handleSplitCommand(m, text)
	case strings.HasPrefix(cmd, "*money") || strings.HasPrefix(cmd, "!money") || strings.HasPrefix(cmd, "/money") ||
		strings.HasPrefix(cmd, "*chiatien") || strings.HasPrefix(cmd, "!chiatien") || strings.HasPrefix(cmd, "/chiatien"):
		b.handleMoneyCommand(m, text)
	case cmd == "*paid" || strings.HasPrefix(cmd, "*paid ") || cmd == "!paid" || strings.HasPrefix(cmd, "!paid ") || cmd == "/paid" || strings.HasPrefix(cmd, "/paid "):
		b.handlePaidCommand(m, text)
	case cmd == "*unpaid" || strings.HasPrefix(cmd, "*unpaid ") || cmd == "!unpaid" || strings.HasPrefix(cmd, "!unpaid ") || cmd == "/unpaid" || strings.HasPrefix(cmd, "/unpaid "):
		b.handleUnpaidCommand(m, text)
	case cmd == "*bxh" || strings.HasPrefix(cmd, "*bxh ") || cmd == "!bxh" || strings.HasPrefix(cmd, "!bxh ") || cmd == "/bxh" || strings.HasPrefix(cmd, "/bxh ") ||
		cmd == "*stats" || strings.HasPrefix(cmd, "*stats ") || cmd == "!stats" || strings.HasPrefix(cmd, "!stats ") ||
		cmd == "*top" || strings.HasPrefix(cmd, "*top "):
		b.handleBXHCommand(m, text)
	case strings.HasPrefix(cmd, "*register qr") || strings.HasPrefix(cmd, "!register qr") || strings.HasPrefix(cmd, "/register qr"):
		b.handleRegisterQRCommand(m, text)
	}
}

func (b *Bot) handleVinhHubCommand(m *mezon.ChannelMessage) {
	b.mu.RLock()
	vinhHubMgr := b.vinhHubMgr
	b.mu.RUnlock()

	if vinhHubMgr == nil {
		_ = b.reply(m, "Tính năng VinhHub chưa được cấu hình.")
		return
	}

	info, err := vinhHubMgr.GetImageInfo()
	if err != nil {
		b.logger().Error("failed to get vinhhub image info", "err", err)
		_ = b.reply(m, fmt.Sprintf("⚠️ Không thể tải ảnh VinhHub: %v", err))
		return
	}

	opts := &mezon.SendOptions{
		Attachments: []mezon.Attachment{
			{
				Filename: info.Filename,
				Filetype: info.Filetype,
				URL:      info.URL,
				Width:    info.Width,
				Height:   info.Height,
				Size:     info.Size,
			},
		},
	}

	if err := b.replyWithOptions(m, "", opts); err != nil {
		b.logger().Error("failed to send vinhhub image", "err", err, "url", info.URL)
		_ = b.reply(m, fmt.Sprintf("⚠️ Gửi ảnh VinhHub thất bại: %v", err))
	}
}

func (b *Bot) handleRegisterVinhHubCommand(m *mezon.ChannelMessage, rawText string) {
	b.mu.RLock()
	vinhHubMgr := b.vinhHubMgr
	b.mu.RUnlock()

	if vinhHubMgr == nil {
		_ = b.reply(m, "Tính năng VinhHub chưa được cấu hình.")
		return
	}

	parts := strings.Fields(rawText)
	var urlToRegister string
	if len(parts) >= 3 {
		urlToRegister = parts[2]
	}

	if urlToRegister == "" {
		_ = b.reply(m, "⚠️ Vui lòng cung cấp link ảnh: `*register vinhhub <link_ảnh>`")
		return
	}

	sender := m.Username
	if sender == "" {
		sender = m.SenderID
	}

	if err := vinhHubMgr.Register(urlToRegister, sender); err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
		return
	}

	_ = b.reply(m, fmt.Sprintf("✅ Đã cập nhật ảnh VinhHub thành công!\nLink ảnh: %s", urlToRegister))
}

func (b *Bot) handleDeleteVinhHubCommand(m *mezon.ChannelMessage) {
	b.mu.RLock()
	vinhHubMgr := b.vinhHubMgr
	b.mu.RUnlock()

	if vinhHubMgr == nil {
		_ = b.reply(m, "Tính năng VinhHub chưa được cấu hình.")
		return
	}

	if err := vinhHubMgr.Delete(); err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ Xoá cấu hình ảnh VinhHub thất bại: %v", err))
		return
	}

	_ = b.reply(m, "✅ Đã xoá ảnh tuỳ chỉnh và khôi phục ảnh menu VinhHub mặc định!")
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

func (b *Bot) handleSplitCommand(m *mezon.ChannelMessage, rawText string) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}

	parts := strings.Fields(rawText)
	if len(parts) < 2 {
		_ = b.reply(m, "⚠️ Vui lòng nhập chi phí cần chia.\nVí dụ:\n• `*split 360k + 26*3` (tiền sân 360k + 3 quả cầu giá 26k/quả)\n• `*split 360k san 45k cau`\n• `*split 450k`")
		return
	}

	expr := strings.TrimSpace(rawText[len(parts[0]):])
	bill, breakdown, err := sportMgr.CreateSplitBill(expr)
	if err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
		return
	}

	title := "🏸 **[CHIA TIỀN CẦU LÔNG"
	if bill.MatchDay != "" && bill.MatchDate != "" {
		title += fmt.Sprintf(" - %s (%s)", strings.ToUpper(bill.MatchDay), bill.MatchDate)
	}
	title += "]**"

	var sb strings.Builder
	sb.WriteString(title + "\n\n")

	sb.WriteString("📊 **Chi tiết chi phí:**\n")
	sb.WriteString(fmt.Sprintf("• 🏟️ Tiền sân: **%s VNĐ**\n", fund.FormatVND(breakdown.CourtFee)))
	if breakdown.ShuttleFee > 0 {
		if breakdown.ShuttleCount > 0 {
			sb.WriteString(fmt.Sprintf("• 🏸 Tiền cầu (%d quả x %s): **%s VNĐ**\n",
				breakdown.ShuttleCount, fund.FormatVND(breakdown.ShuttlePrice), fund.FormatVND(breakdown.ShuttleFee)))
		} else {
			sb.WriteString(fmt.Sprintf("• 🏸 Tiền cầu: **%s VNĐ**\n", fund.FormatVND(breakdown.ShuttleFee)))
		}
	}
	sb.WriteString(fmt.Sprintf("💰 **Tổng chi phí:** **%s VNĐ**\n", fund.FormatVND(breakdown.TotalFee)))
	sb.WriteString(fmt.Sprintf("👥 Số người tham gia: **%d người** (**%d slot**)\n", len(bill.Items), bill.TotalSlots))
	sb.WriteString(fmt.Sprintf("💵 Đơn giá: **%s VNĐ/slot** *(đã làm tròn)*\n\n", fund.FormatVND(bill.SlotPrice)))

	sb.WriteString("📋 **Danh sách thanh toán:**\n")
	var mentions []mezon.Mention
	var sortedItems []*sport.BillItem
	for _, item := range bill.Items {
		sortedItems = append(sortedItems, item)
	}
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].Username < sortedItems[j].Username
	})

	for _, item := range sortedItems {
		slotText := fmt.Sprintf("%d slot", item.Slots)
		tag := "@" + item.Username
		sb.WriteString(fmt.Sprintf("• %s (%s): **%s VNĐ**\n", tag, slotText, fund.FormatVND(item.Amount)))
		mentions = append(mentions, mezon.Mention{
			UserID:   item.UserID,
			Username: item.Username,
		})
	}

	sb.WriteString("\n👉 Mọi người quét mã QR bên dưới để thanh toán nhé!\n*(React ✅ vào tin nhắn này hoặc gõ `*paid` sau khi đã chuyển khoản)* 🏸🔥")

	qrURL := sportMgr.GetPaymentQR()
	signedQRURL := qrURL
	if b.client != nil {
		if s, err := vinhhub.SignCDNURL(b.client, qrURL); err == nil && s != "" {
			signedQRURL = s
		}
	}

	opts := &mezon.SendOptions{
		Mentions: mentions,
	}
	if signedQRURL != "" {
		opts.Attachments = []mezon.Attachment{
			{
				Filename: "",
				Filetype: "image/png",
				URL:      signedQRURL,
				Width:    947,
				Height:   2048,
			},
		}
	}

	sentMsgID, err := b.replyWithOptionsGetID(m, sb.String(), opts)
	if err != nil {
		b.logger().Error("failed to send split message", "err", err)
		_ = b.reply(m, fmt.Sprintf("⚠️ Gửi bảng chia tiền thất bại: %v", err))
		return
	}
	if sentMsgID != "" {
		sportMgr.SetCurrentBillMessageID(sentMsgID)
	}
}

func (b *Bot) handleMoneyCommand(m *mezon.ChannelMessage, rawText string) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}

	parts := strings.Fields(rawText)
	if len(parts) >= 3 && strings.ToLower(parts[1]) == "qr" {
		newQR := parts[2]
		sportMgr.SetPaymentQR(newQR)
		_ = b.reply(m, fmt.Sprintf("✅ Đã cập nhật mã QR thanh toán thành công!\nLink: %s", newQR))
		return
	}

	if len(parts) < 2 {
		_ = b.reply(m, "⚠️ Vui lòng nhập số tiền cần chia, ví dụ: `*money 45k` hoặc `*money 45000`")
		return
	}

	unitAmount, err := sport.ParseMoneyAmount(parts[1])
	if err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
		return
	}

	bill, err := sportMgr.CreateMoneyBill(unitAmount)
	if err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
		return
	}

	title := "🏸 **[CHIA TIỀN CẦU LÔNG"
	if bill.MatchDay != "" && bill.MatchDate != "" {
		title += fmt.Sprintf(" - %s (%s)", strings.ToUpper(bill.MatchDay), bill.MatchDate)
	}
	title += "]**"

	var sb strings.Builder
	sb.WriteString(title + "\n\n")
	sb.WriteString(fmt.Sprintf("📊 Tổng kết buổi đánh: **%d người** (**%d slot**)\n", len(bill.Items), bill.TotalSlots))
	sb.WriteString(fmt.Sprintf("💵 Số tiền mỗi slot: **%s VNĐ**\n", fund.FormatVND(bill.SlotPrice)))
	sb.WriteString(fmt.Sprintf("💰 Tổng cộng: **%s VNĐ**\n\n", fund.FormatVND(bill.TotalAmount)))
	sb.WriteString("📋 **Danh sách thanh toán:**\n")

	var mentions []mezon.Mention
	var sortedItems []*sport.BillItem
	for _, item := range bill.Items {
		sortedItems = append(sortedItems, item)
	}
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].Username < sortedItems[j].Username
	})

	for _, item := range sortedItems {
		slotText := fmt.Sprintf("%d slot", item.Slots)
		tag := "@" + item.Username
		sb.WriteString(fmt.Sprintf("• %s (%s): **%s VNĐ**\n", tag, slotText, fund.FormatVND(item.Amount)))

		mentions = append(mentions, mezon.Mention{
			UserID:   item.UserID,
			Username: item.Username,
		})
	}

	sb.WriteString("\n👉 Mọi người quét mã QR bên dưới để thanh toán nhé!\n*(React ✅ vào tin nhắn này hoặc gõ `*paid` sau khi đã chuyển khoản)* 🏸🔥")

	qrURL := sportMgr.GetPaymentQR()
	signedQRURL := qrURL
	if b.client != nil {
		if s, err := vinhhub.SignCDNURL(b.client, qrURL); err == nil && s != "" {
			signedQRURL = s
		}
	}

	opts := &mezon.SendOptions{
		Mentions: mentions,
	}

	if signedQRURL != "" {
		opts.Attachments = []mezon.Attachment{
			{
				Filename: "",
				Filetype: "image/png",
				URL:      signedQRURL,
				Width:    947,
				Height:   2048,
			},
		}
	}

	sentMsgID, err := b.replyWithOptionsGetID(m, sb.String(), opts)
	if err != nil {
		b.logger().Error("failed to send money split message", "err", err)
		_ = b.reply(m, fmt.Sprintf("⚠️ Gửi bảng chia tiền thất bại: %v", err))
		return
	}
	if sentMsgID != "" {
		sportMgr.SetCurrentBillMessageID(sentMsgID)
	}
}

func (b *Bot) handlePaidCommand(m *mezon.ChannelMessage, rawText string) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}

	parts := strings.Fields(rawText)
	args := parts[1:]

	if len(args) > 0 {
		paid, notFound, totalPaid, totalCost, paidCount, totalCount, err := sportMgr.MarkPaidBatch(args)
		if err != nil {
			_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
			return
		}

		var sb strings.Builder
		if len(paid) > 0 {
			var names []string
			for _, p := range paid {
				names = append(names, "@"+p.Username)
			}
			sb.WriteString(fmt.Sprintf("✅ **Đã ghi nhận đóng tiền:** %s 💵\n\n", strings.Join(names, ", ")))
		}
		if len(notFound) > 0 {
			sb.WriteString(fmt.Sprintf("⚠️ *Không tìm thấy trong hoá đơn:* %s\n\n", strings.Join(notFound, ", ")))
		}
		sb.WriteString(fmt.Sprintf("📊 **Tiến độ thu tiền:** %d/%d người (%s / %s VNĐ)",
			paidCount, totalCount, fund.FormatVND(totalPaid), fund.FormatVND(totalCost)))
		_ = b.reply(m, sb.String())
		return
	}

	// No args -> Show payment dashboard
	bill, paid, unpaid, totalPaid, totalCost, err := sportMgr.GetBillStatus()
	if err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 **TIẾN ĐỘ THU TIỀN CẦU LÔNG**\n")
	if bill.MatchDay != "" && bill.MatchDate != "" {
		sb.WriteString(fmt.Sprintf("🏸 Buổi đánh: **%s (%s)**\n", bill.MatchDay, bill.MatchDate))
	}
	sb.WriteString(fmt.Sprintf("💰 Tổng chi phí: **%s VNĐ** (%d slot • **%s VNĐ/slot**)\n",
		fund.FormatVND(totalCost), bill.TotalSlots, fund.FormatVND(bill.SlotPrice)))
	sb.WriteString(fmt.Sprintf("💵 Đã thu: **%s VNĐ** (%d/%d người)\n",
		fund.FormatVND(totalPaid), len(paid), len(bill.Items)))

	remaining := totalCost - totalPaid
	if remaining < 0 {
		remaining = 0
	}
	sb.WriteString(fmt.Sprintf("⏳ Còn thiếu: **%s VNĐ** (%d/%d người)\n\n",
		fund.FormatVND(remaining), len(unpaid), len(bill.Items)))

	if len(paid) > 0 {
		sb.WriteString(fmt.Sprintf("✅ **Đã thanh toán (%d người):**\n", len(paid)))
		for _, item := range paid {
			sb.WriteString(fmt.Sprintf("• @%s (%d slot - %s VNĐ)\n", item.Username, item.Slots, fund.FormatVND(item.Amount)))
		}
		sb.WriteString("\n")
	}

	if len(unpaid) > 0 {
		sb.WriteString(fmt.Sprintf("⏳ **Chưa thanh toán (%d người):**\n", len(unpaid)))
		for _, item := range unpaid {
			sb.WriteString(fmt.Sprintf("• @%s (%d slot - %s VNĐ)\n", item.Username, item.Slots, fund.FormatVND(item.Amount)))
		}
	} else {
		sb.WriteString("🎉 *Toàn bộ thành viên đã hoàn tất đóng tiền! Cảm ơn cả nhà!* 🏸❤️")
	}

	_ = b.reply(m, sb.String())
}

func (b *Bot) handleUnpaidCommand(m *mezon.ChannelMessage, rawText string) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}

	parts := strings.Fields(rawText)
	args := parts[1:]

	if len(args) > 0 {
		unmarked, notFound, err := sportMgr.MarkUnpaidBatch(args)
		if err != nil {
			_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
			return
		}

		var sb strings.Builder
		if len(unmarked) > 0 {
			var names []string
			for _, u := range unmarked {
				names = append(names, "@"+u.Username)
			}
			sb.WriteString(fmt.Sprintf("↩️ Đã chuyển %s về trạng thái **CHƯA thanh toán**.\n", strings.Join(names, ", ")))
		}
		if len(notFound) > 0 {
			sb.WriteString(fmt.Sprintf("⚠️ *Không tìm thấy trong hoá đơn:* %s", strings.Join(notFound, ", ")))
		}
		_ = b.reply(m, sb.String())
		return
	}

	// No args -> Remind unpaid participants with QR code & mentions
	bill, unpaid, totalUnpaid, err := sportMgr.GetUnpaidList()
	if err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
		return
	}

	if len(unpaid) == 0 {
		_ = b.reply(m, "🎉 **Toàn bộ anh em đã hoàn tất đóng tiền!**\nCảm ơn cả nhà nhiều nhé! 🏸❤️")
		return
	}

	var sb strings.Builder
	sb.WriteString("📢 **NHẮC THANH TOÁN TIỀN CẦU LÔNG** 🏸\n")
	if bill.MatchDay != "" && bill.MatchDate != "" {
		sb.WriteString(fmt.Sprintf("Buổi đánh: **%s (%s)**\n", bill.MatchDay, bill.MatchDate))
	}
	sb.WriteString(fmt.Sprintf("Đơn giá: **%s VNĐ/slot**\n\n", fund.FormatVND(bill.SlotPrice)))

	sb.WriteString(fmt.Sprintf("👉 Danh sách còn **%d người** chưa thanh toán:\n", len(unpaid)))
	var mentions []mezon.Mention
	for _, item := range unpaid {
		tag := "@" + item.Username
		sb.WriteString(fmt.Sprintf("• %s (%d slot): **%s VNĐ**\n", tag, item.Slots, fund.FormatVND(item.Amount)))
		mentions = append(mentions, mezon.Mention{
			UserID:   item.UserID,
			Username: item.Username,
		})
	}

	sb.WriteString(fmt.Sprintf("\n💰 Tổng còn thiếu: **%s VNĐ**\n", fund.FormatVND(totalUnpaid)))
	sb.WriteString("👉 Mọi người vui lòng quét mã QR bên dưới để thanh toán sớm nhé! 🔥")

	qrURL := sportMgr.GetPaymentQR()
	signedQRURL := qrURL
	if b.client != nil {
		if s, err := vinhhub.SignCDNURL(b.client, qrURL); err == nil && s != "" {
			signedQRURL = s
		}
	}

	opts := &mezon.SendOptions{
		Mentions: mentions,
	}

	if signedQRURL != "" {
		opts.Attachments = []mezon.Attachment{
			{
				Filename: "",
				Filetype: "image/png",
				URL:      signedQRURL,
				Width:    947,
				Height:   2048,
			},
		}
	}

	if err := b.replyWithOptions(m, sb.String(), opts); err != nil {
		b.logger().Error("failed to send unpaid reminder message", "err", err)
		_ = b.reply(m, fmt.Sprintf("⚠️ Gửi nhắc thanh toán thất bại: %v", err))
	}
}

func (b *Bot) handleBXHCommand(m *mezon.ChannelMessage, rawText string) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}

	parts := strings.Fields(rawText)
	monthInput := ""
	if len(parts) >= 2 {
		monthInput = parts[1]
	}

	report, err := sportMgr.GetLeaderboardText(monthInput, time.Now())
	if err != nil {
		_ = b.reply(m, fmt.Sprintf("⚠️ %v", err))
		return
	}

	_ = b.reply(m, report)
}

func (b *Bot) handleRegisterQRCommand(m *mezon.ChannelMessage, rawText string) {
	b.mu.RLock()
	sportMgr := b.sportMgr
	b.mu.RUnlock()

	if sportMgr == nil {
		_ = b.reply(m, "Tính năng thể thao chưa được cấu hình.")
		return
	}

	parts := strings.Fields(rawText)
	if len(parts) < 3 {
		_ = b.reply(m, "⚠️ Vui lòng cung cấp link QR: `*register qr <link_ảnh_qr>`")
		return
	}

	newQR := parts[2]
	sportMgr.SetPaymentQR(newQR)
	_ = b.reply(m, fmt.Sprintf("✅ Đã cập nhật mã QR thanh toán thành công!\nLink: %s", newQR))
}

func (b *Bot) handleHelpCommand(m *mezon.ChannelMessage) {
	help := "🤖 **Mezon Bot Hub**\n\n" +
		"💰 **Quỹ & Tài chính:**\n" +
		"• `*quy`: Xem số dư hiện tại của quỹ\n\n" +
		"🏪 **VinhHub (Energy Hub):**\n" +
		"• `*vinhhub`: Xem ảnh menu VinhCorner Energy Hub\n" +
		"• `*register vinhhub <link>`: Cập nhật ảnh menu mới\n" +
		"• `*delete vinhhub`: Khôi phục ảnh menu mặc định\n\n" +
		"🏸 **Cầu lông & Thể thao:**\n" +
		"• `*diemdanh`: Gửi thông báo điểm danh cho buổi đánh cầu tiếp theo\n" +
		"• `*nhaclich`: Nhắc lịch trước giờ đánh & AI tóm tắt ai đi ai nghỉ, sân nào\n" +
		"• `*summary`: Xem nhanh danh sách điểm danh và sân bãi hiện tại\n" +
		"• `*split <công_thức>`: Chia tiền sân + cầu (ví dụ: `*split 360k + 26*3` hoặc `*split 360k san 45k cau`)\n" +
		"• `*money <số_tiền>`: Chia tiền theo slot cố định (ví dụ: `*money 45k`)\n" +
		"• `*paid [@user...]`: Đánh dấu ai đã đóng tiền hoặc xem tiến độ thu tiền\n" +
		"• `*unpaid [@user...]`: Nhắc nhở người chưa đóng kèm mã QR hoặc hủy đánh dấu nhầm\n" +
		"• `*bxh [tháng]`: Bảng xếp hạng \"Chiến thần quật cầu\" & \"Chúa sủi\" (ví dụ: `*bxh` hoặc `*bxh 11/2026`)\n" +
		"• `*register qr <link>`: Cập nhật mã QR thanh toán mới\n\n" +
		"• `*help`: Hiển thị hướng dẫn này"
	_ = b.reply(m, help)
}

var reBold = regexp.MustCompile(`\*\*(.+?)\*\*`)

// ParseMarkdownSpans removes `**...**` markers and converts them into Mezon `mk` bold spans.
func ParseMarkdownSpans(text string) (string, []map[string]any) {
	if !strings.Contains(text, "**") {
		return text, nil
	}

	var clean strings.Builder
	var spans []map[string]any
	last := 0

	indices := reBold.FindAllStringSubmatchIndex(text, -1)
	for _, idx := range indices {
		clean.WriteString(text[last:idx[0]])

		startU16 := mezon.UTF16Len(clean.String())
		inner := text[idx[2]:idx[3]]
		clean.WriteString(inner)
		endU16 := mezon.UTF16Len(clean.String())

		spans = append(spans, map[string]any{
			"type": "b",
			"s":    startU16,
			"e":    endU16,
		})

		last = idx[1]
	}

	clean.WriteString(text[last:])
	return clean.String(), spans
}

func (b *Bot) replyWithOptionsGetID(m *mezon.ChannelMessage, text string, opts *mezon.SendOptions) (string, error) {
	if opts == nil {
		opts = &mezon.SendOptions{}
	}
	if opts.TopicID == "" && m.TopicID != "" {
		opts.TopicID = m.TopicID
	}
	if len(opts.References) == 0 && m.ID != "" {
		opts.References = []mezon.MessageRef{{
			MessageRefID:    m.ID,
			MessageSenderID: m.SenderID,
		}}
	}

	targetID := m.ChannelID
	if m.TopicID != "" {
		targetID = m.TopicID
	}

	msgID, err := b.SendToChannel(context.Background(), targetID, text, opts)
	if err != nil && targetID != m.ChannelID && m.ChannelID != "" {
		// Fallback to parent channel with TopicID
		msgID, err = b.SendToChannel(context.Background(), m.ChannelID, text, opts)
	}
	return msgID, err
}

func (b *Bot) replyWithOptions(m *mezon.ChannelMessage, text string, opts *mezon.SendOptions) error {
	_, err := b.replyWithOptionsGetID(m, text, opts)
	return err
}

func (b *Bot) reply(m *mezon.ChannelMessage, text string) error {
	return b.replyWithOptions(m, text, nil)
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
