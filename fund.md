# The fund feed

The bot watches the fund wallet on the chain and posts every transaction into or out
of it in a Mezon channel/thread, one post per transaction:

```
Nhận vào quỹ +80.000 VNĐ
Người gửi: dai.trinhduc
Nội dung: sua chua
15:22 11/09/2026
Quỹ hiện có 46.000.000 VNĐ
```

Money leaving the wallet posts as `Chi từ quỹ −2.000.000 VNĐ`, including `Người nhận:` and `Nội dung:`.
The bot resolves the sender/recipient address against the configured `users => wallet addresses` map (`wallets.json` / `USER_WALLETS_PATH`). If the wallet address is not mapped, the address is shown directly.

This page covers how the feed works, how to set it up, and how to tell whether it
is running. For `*quy`, the command that reports the balance on demand.

---

## 1. How a transaction reaches the channel

```
 mmn node (gRPC)                   fund.Stream                      notifications table            Courier                 Mezon
 dong.mezon.ai:443                 internal/fund/stream.go          (the outbox)                   internal/notify         channel
┌──────────────────────┐  every  ┌────────────────────────────┐   ┌───────────────────────────┐  every  ┌───────────────┐  ┌──────────────────┐
│ SubscribeTransaction │  event  │ 1. drop HeartBeat           │   │ kind      = fund_activity │  30 s   │ send pending  │  │ MEZON_FUND_      │
│ Status — the whole   │ ──────▶ │ 2. keep CONFIRMED/FINALIZED │──▶│ channel   = fund channel  │ ──────▶ │ messages,     │─▶│ CHANNEL_ID       │
│ chain, unfiltered    │         │ 3. keep sender or recipient │   │ dedupe    = fund:<txhash> │         │ up to 5 tries │  │                  │
└──────────────────────┘         │    = the campaign wallet    │   │ body      = post text     │         └───────────────┘  └──────────────────┘
                                 │ 4. read the balance line    │   └───────────────────────────┘
                                 └────────────────────────────┘
```

1. **Subscribe.** `fund.Stream` opens `TxService.SubscribeTransactionStatus` on
   the mmn node through `mmn-sdk`. The subscription cannot be filtered, so the
   node sends every transaction on the chain.
2. **Filter.** The stream keeps an event only if all of these hold:
   - it is not a `HeartBeat` (the node's keep-alive, with no addresses and no amount);
   - its status is `CONFIRMED` or `FINALIZED`. A `PENDING` transaction may still
     fail, and a `FAILED` one moved no money;
   - the campaign wallet (`DONATION_WALLET_ADDRESS`) is the recipient (money in)
     or the sender (money out);
   - it has a transaction hash and a readable amount.
3. **Balance line.** Just before queueing, the stream reads the current balance
   and appends `Quỹ hiện có … VNĐ` (see § 3). If the balance cannot be read, the
   post goes out without that line.
4. **Queue.** The post is written to the `notifications` outbox with
   `kind = 'fund_activity'` and `dedupe_key = 'fund:<tx hash>'`. A unique index
   on `dedupe_key` (migration 0011) means one transaction becomes one post. The
   node reports each transaction several times (PENDING, CONFIRMED, FINALIZED,
   often within one second), and a reconnect may deliver it again.
5. **Deliver.** The courier, which also delivers results and announcements,
   sends pending messages every 30 seconds. A send that fails is retried on the
   next sweep. After 5 failed attempts the message is marked `failed` and stays
   visible on the admin site.

The stream never sends to Mezon itself; it only queues. So if the channel is
briefly unreachable, the feed is late, not lost.

---

## 2. Configuration

All settings are environment variables, read from `/opt/scholarship/env` in
production and from `.env` locally.

| Variable | Needed for | Meaning |
|---|---|---|
| `MMN_NODE_ENDPOINT` | the feed | The mmn node's gRPC address: `dong.mezon.ai:443`. **Use the hostname, not the IP.** The certificate is `*.mezon.ai` and has no IP SANs, so connecting to `103.214.9.101` fails verification. Blank turns the feed off. |
| `MMN_NODE_TLS` | the feed | `true` by default. Set `false` only for a node served without TLS. |
| `DONATION_WALLET_ADDRESS` | the feed and the balance | The campaign wallet. Transactions from or to this address are posted. |
| `MEZON_FUND_CHANNEL_ID` | the feed | The channel the posts go to. Blank falls back to `MEZON_ANNOUNCE_CHANNEL_ID`. |
| `MMN_TOKEN_DECIMALS` | amounts | The token's precision, default `6`. The raw amount is divided by 10^decimals to get whole đồng. |
| `MEZON_MAINNET_API_URL` | the balance line | The indexer's REST base, `https://dong.mezon.ai/indexer-api`. Blank drops the balance line (and `*quy`). |
| `MMN_CHAIN_ID` | the balance line | The chain id in the indexer's paths, default `1337`. |
| `SCHOOL_FUND_RESERVED_VND` | the balance line | The school-building share, subtracted from the wallet. See § 3. |

**When the feed runs.** It starts with the bot, inside the `scholarship`
service, only if `MMN_NODE_ENDPOINT`, `DONATION_WALLET_ADDRESS` and a fund
channel (its own or the announce channel) are all set. It does not run under
`-api-only`, because that mode has no bot and no courier.

**The bot has to be able to post in the channel.** It must be a member of the
clan with permission to send messages in `MEZON_FUND_CHANNEL_ID`. Otherwise every
post fails at delivery and ends up `failed` after five attempts (§ 4).

**Changing a setting** means editing the env file and restarting:

```bash
sudoedit /opt/scholarship/env
sudo systemctl restart scholarship
```

To **turn the feed off**, blank `MMN_NODE_ENDPOINT` and restart. Posts already
queued are still delivered (§ 5).

---

## 3. The balance line

`Quỹ hiện có` is what the scholarship may spend, not what the wallet holds:

```
scholarship balance = wallet balance − SCHOOL_FUND_RESERVED_VND   (never below 0)
```

The campaign also funds a school-building programme, and
`SCHOOL_FUND_RESERVED_VND` is the share set aside for it. The wallet balance
already excludes earlier withdrawals, so do not reserve money that has been paid
out.

The figure comes from the **indexer** (REST), not the node, through the same
reader as `*quy` and the admin site's fund card. Two things follow from that:

- **It may lag the movement it follows.** Readings are cached for five minutes.
  If `*quy` or the admin site read the balance shortly before a transfer, the
  post shows the balance from before it. The indexer may also be a few seconds
  behind the node. The movement line is always right. The balance line is right
  as of when it was read.
- **It is read live, never computed.** Adding each movement to a remembered total
  would drift as soon as one movement was missed, and the feed can miss
  movements (§ 5). The chain is the ledger.

If the indexer is down, the post is still sent, without the balance line. After
the first successful read, a failed read reuses the last cached figure.

---

## 4. Is it working?

### The service log

```bash
journalctl -u scholarship --since today | grep -i fund
```

| Log line | Meaning |
|---|---|
| `watching the fund node=… channel=…` | Connected and subscribed. Printed at start and after every reconnect. |
| `fund movement announced hash=… incoming=… vnd=…` | A transaction was queued for the channel. |
| `fund stream ended; reconnecting … in=10s` | The connection closed (node restart, proxy timeout). Ordinary; the stream dials again after 10 s. |
| `fund stream went silent; reconnecting after=2m0s` | Nothing, not even a heartbeat, for two minutes. The connection is treated as dead and redialled. The node heartbeats about every 30 s, so silence means a broken connection, not a quiet chain. |
| `could not queue a fund movement` | The database refused the write. The movement is not posted. |
| `could not read the fund balance for the activity post` | The indexer failed. The post goes out without the balance line. |
| `could not deliver notification … kind=fund_activity` | Mezon refused the send. It is retried, up to 5 attempts. |

If there is no `watching the fund` line since the last restart, the feed is
not configured: check the three settings in § 2.

### The admin site

**Vận hành → Tin nhắn** lists the outbox. Fund posts show as **Biến động quỹ**,
with their text and status. Filter by *Gửi lỗi* to see failed ones and why. An
admin can press **Gửi lại** once the cause is fixed, for example after adding
the bot to the channel.

### The database

```sql
-- The last ten fund posts, and whether they went out.
SELECT id, status, attempts, created_at, sent_at, last_error, body
  FROM notifications
 WHERE kind = 'fund_activity'
 ORDER BY id DESC
 LIMIT 10;
```

---

## 5. What it guarantees, and what it does not

| | |
|---|---|
| **Once per transaction** | Yes. The dedupe key `fund:<tx hash>` is unique in the database, so status updates and reconnects cannot repeat a post. |
| **Both directions** | Yes. Money in and money out, no minimum. |
| **Survives Mezon being briefly down** | Yes. Posts wait in the outbox and are retried, up to 5 attempts, 30 s apart. |
| **Survives the node restarting** | Yes. The stream reconnects after 10 s, or after 2 minutes of silence. |
| **Transactions while disconnected** | **No, they are lost.** The node streams from the moment of subscribing and nothing asks it for history. This is accepted: the feed is a notice board, and `*quy` reads the true balance from the chain, so a missed post loses a line of news, not money or a number. |
| **The sender's note** | Posted under `Nội dung:` when provided. |

**Removing the producer does not remove what it produced.** Turning the feed off
stops new posts, but ones already queued are still delivered. To drop them:

```sql
UPDATE notifications SET status = 'failed'
 WHERE kind = 'fund_activity' AND status = 'pending';
```

---

## 6. Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| Nothing posts, and the log has no `watching the fund` | Feed not configured, or the service runs `-api-only` | Set `MMN_NODE_ENDPOINT`, `DONATION_WALLET_ADDRESS` and a fund channel; restart. |
| `connect to …` or TLS errors | Endpoint given as an IP, or TLS off for a TLS node | Use `dong.mezon.ai:443` with `MMN_NODE_TLS=true`. |
| `watching the fund` appears, but a known transfer did not post | It landed while the stream was reconnecting (§ 5), or it was to or from a different wallet | Check the reconnect lines around that time, and compare the address with `DONATION_WALLET_ADDRESS`. |
| Posts are queued but `failed` | The bot cannot post in the channel, or the channel id is wrong | Fix the membership or permission (or the id and restart), then **Gửi lại** on the admin site. |
| Amounts off by a power of ten | Wrong `MMN_TOKEN_DECIMALS` | Set it to the token's precision (6) and restart. Posts already sent keep the wrong figure. |
| No `Quỹ hiện có` line | Indexer not configured or unreachable | Check `MEZON_MAINNET_API_URL`; the log has `could not read the fund balance…`. |
| The balance line looks too low or too high | Cache or indexer lag (§ 3), or a wrong `SCHOOL_FUND_RESERVED_VND` | Compare with `*quy` a few minutes later. Check the reserve. |
| `reconnecting` every two minutes | Heartbeats are not reaching the service (proxy, firewall) | Check outbound gRPC to `dong.mezon.ai:443` stays open. |

---

## 7. Trying it locally

There is no simulated trigger. The feed needs a real transaction on the chain.

1. In `.env`, set `MMN_NODE_ENDPOINT=dong.mezon.ai:443`, set
   `DONATION_WALLET_ADDRESS` to a wallet you can send to (not the real
   campaign's), and set `MEZON_FUND_CHANNEL_ID` to a test channel.
2. Run the bot: `make run`. Wait for `watching the fund` in its output.
3. Send a small amount to that wallet. The post should appear within about 30 s,
   the courier's interval.

The behaviour itself is covered by unit tests, which need no node:

```bash
go test ./internal/fund/
```

These cover: both directions posted; one post per transaction across statuses;
only confirmed or finalized transactions; other wallets and heartbeats ignored;
a malformed event skipped; a queue failure not stopping the stream; the silence
timeout; the balance line, and posting without it.

---

## 8. Where the code is

| File | What it holds |
|---|---|
| `internal/fund/stream.go` | `Stream`: subscribe, filter, reconnect, silence timeout, post text (`MovementText`) |
| `internal/fund/fund.go` | `Reader` and `Indexer`: the balance, the reserve, the five-minute cache |
| `cmd/scholarship/main.go` | Wiring: starts the stream beside the courier; the `outbox` adapter that maps a duplicate to `fund.ErrAlreadyQueued` |
| `internal/notify/notify.go` | `Courier`: delivers the outbox every 30 s |
| `internal/store/notification.go` | The outbox, `NotifyFund = "fund_activity"`, `MaxNotificationAttempts = 5` |
| `migrations/0011_notification_dedupe.sql` | The unique `dedupe_key` index that keeps one transaction to one post |
| `internal/config/config.go` | The environment variables in § 2 |

