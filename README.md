# Mezon Fund Feed Bot (`vinh-hub`)

A real-time Mezon bot that monitors wallet transactions on the **Mezon Đồng** blockchain (`mmn`) and posts balance notifications directly into a designated **Mezon thread** (or channel), following the architecture specified in [`fund.md`](file:///Users/daitrinh/Projects/vinh-hub/fund.md).

## Features

- **Live Blockchain Stream**: Connects via gRPC to the Mezon node (`dong.mezon.ai:443`) using [`github.com/mezonai/mmn-sdk/go-sdk`](https://github.com/mezonai/mmn-sdk/go-sdk) to subscribe to real-time transactions (`TxService.SubscribeTransactionStatus`).
- **Transaction Filtering & Attribution**:
  - Automatically filters incoming and outgoing transactions for the configured fund wallet (`DONATION_WALLET_ADDRESS`).
  - Ignores `HeartBeat` events, unconfirmed/failed transactions, and other unrelated wallets.
  - Resolves sender and recipient wallet addresses to Mezon usernames via [`wallets.json`](file:///Users/daitrinh/Projects/vinh-hub/wallets.json).
  - Displays the transaction content / note (`Nội dung: ...`) when provided.
- **Accurate Balance Reporting**:
  - Fetches the current fund balance from the Mezon Đồng Indexer REST API (`MEZON_MAINNET_API_URL`).
  - Caches readings for 5 minutes and falls back to stale readings if the indexer is temporarily unreachable.
- **Thread-Aware Mezon Bot**:
  - Connects to Mezon gateway using [`github.com/quangledang23/mezon-sdk-go`](https://github.com/quangledang23/mezon-sdk-go).
  - Posts updates directly into a target **Thread** (`MEZON_THREAD_ID`), or auto-creates/finds a thread under a parent channel (`MEZON_CHANNEL_ID`).
  - Interactive commands: users can ask `*quy` or `*fund` inside the thread/channel to check the live balance.
- **Reliable Outbox & Courier**:
  - Transactions are deduplicated via unique dedupe keys (`fund:<txhash>`), ensuring status updates (`CONFIRMED`, `FINALIZED`) and reconnects never produce duplicate posts.
  - Delivery runs on a configurable interval (default 30 seconds) with exponential backoff / retry up to 5 attempts before marking as failed.
- **Fault Tolerant**:
  - Automatic reconnection after connection drops (10s delay).
  - Heartbeat / silence monitoring (reconnects if silence exceeds 2 minutes).

---

## Message Format

When money arrives in the fund wallet:
```text
Nhận vào quỹ +80.000 VNĐ
Người gửi: dai.trinhduc
Nội dung: sua chua
15:22 11/09/2026
Quỹ hiện có 46.000.000 VNĐ
```

When money leaves the wallet:
```text
Chi từ quỹ −2.000.000 VNĐ
Người nhận: dai.trinhduc
Nội dung: mua đồ
15:22 11/09/2026
Quỹ hiện có 46.000.000 VNĐ
```

When checking balance on demand with `*quy`:
```text
Thông tin Quỹ
• Quỹ hiện có: 2.736.348 VNĐ
Cập nhật lúc: 09:53 28/09/2026
```

---

## Architecture Flow

```
   mmn node (gRPC)              fund.Stream                Outbox Queue               Courier                  Mezon Bot
  dong.mezon.ai:443      internal/fund/stream.go     internal/outbox/outbox.go  internal/courier/courier.go  internal/bot/bot.go
┌──────────────────┐    ┌───────────────────────────┐   ┌───────────────────────┐    ┌────────────────────┐   ┌──────────────────────────┐
│ SubscribeTx      │───▶│ 1. Drop HeartBeat         │──▶│ kind   = fund_activity│───▶│ Deliver pending    │──▶│ Sends to Thread:         │
│ Status stream    │    │ 2. Filter CONFIRMED/FIN   │   │ dedupe = fund:<txhash>│    │ messages every 30s │   │ • MEZON_THREAD_ID        │
└──────────────────┘    │ 3. Match DONATION_WALLET  │   │ body   = post content │    │ (up to 5 retries)  │   │ • Or Thread in Channel   │
                        │ 4. Read Indexer Balance   │   └───────────────────────┘    └────────────────────┘   └──────────────────────────┘
                        └───────────────────────────┘
```

---

## Configuration

Settings are loaded from `.env` in the project root:

| Variable | Required | Description |
|---|---|---|
| `MEZON_BOT_ID` | **Yes** | Your bot's Mezon application ID |
| `MEZON_TOKEN` | **Yes** | Your bot's Mezon authentication token |
| `MEZON_CHANNEL_ID` | Optional* | Parent channel ID in your Mezon clan |
| `MEZON_THREAD_ID` | Optional* | Target thread ID to deliver fund posts (*at least one of Channel or Thread ID is required) |
| `MEZON_THREAD_NAME` | No | Name of thread to create if `MEZON_THREAD_ID` is blank (default: `Biến động quỹ`) |
| `DONATION_WALLET_ADDRESS`| **Yes** | Fund wallet address to monitor on the blockchain |
| `MMN_NODE_ENDPOINT` | No | gRPC node endpoint, default `dong.mezon.ai:443` |
| `MMN_NODE_TLS` | No | Enable TLS for gRPC node, default `true` |
| `MMN_TOKEN_DECIMALS` | No | Token decimals, default `6` |
| `MEZON_MAINNET_API_URL` | No | Indexer REST URL, default `https://dong.mezon.ai/indexer-api` |
| `MMN_CHAIN_ID` | No | Chain ID for indexer, default `1337` |
| `TIMEZONE` | No | Timezone for post timestamps, default `Asia/Ho_Chi_Minh` |
| `COURIER_INTERVAL` | No | Interval in seconds between outbox delivery sweeps, default `30` |

---

## Getting Started

### 1. Copy Environment Configuration
```bash
cp .env.example .env
```
Edit `.env` and fill in your `MEZON_BOT_ID`, `MEZON_TOKEN`, `DONATION_WALLET_ADDRESS`, and target `MEZON_THREAD_ID` (or `MEZON_CHANNEL_ID`).

### 2. Run Tests
```bash
make test
```

### 3. Build & Run
```bash
make build
make run
```

### 4. Utility Commands
- Query fund balance directly:
  ```bash
  make quy
  # or: ./bin/fundbot -quy
  ```
- Send a test message to the configured thread/channel:
  ```bash
  make test-msg
  # or: ./bin/fundbot -test-msg "Hello from fund bot"
  ```
- Dry run:
  ```bash
  ./bin/fundbot -dry-run
  ```

---

## Interactive Commands in Mezon

When the bot is running in your channel or thread:
- Type `*quy` or `*fund`: The bot queries the Mezon indexer live and responds with:
  ```text
  Thông tin Quỹ
  • Quỹ hiện có: 2.736.348 VNĐ
  Cập nhật lúc: 09:53 28/09/2026
  ```
- Type `*help`: The bot displays help and available commands.
