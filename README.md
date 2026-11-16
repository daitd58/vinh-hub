# Mezon Fund Feed Bot (`vinh-hub`)

A real-time Mezon bot that monitors wallet transactions on the **Mezon Đồng** blockchain (`mmn`) and posts balance notifications directly into a designated **Mezon thread** (or channel), following the architecture specified in [`fund.md`](file:///Users/daitrinh/Projects/vinh-hub/fund.md).

## Features

- **Live Blockchain Stream**: Connects via gRPC to the Mezon node (`dong.mezon.ai:443`) using [`github.com/mezonai/mmn-sdk/go-sdk`](https://github.com/mezonai/mmn-sdk/go-sdk) to subscribe to real-time transactions (`TxService.SubscribeTransactionStatus`).
- **Transaction Filtering & Attribution**:
  - Automatically filters incoming and outgoing transactions for the configured fund wallet (`DONATION_WALLET_ADDRESS`).
  - **Bot Intermediary (Auto-Forward)**: Users can send tokens directly to the bot's wallet (`BOT_WALLET_ADDRESS`); the bot automatically forwards incoming tokens to the fund wallet on-chain and maps the original sender and message.
  - Ignores `HeartBeat` events, unconfirmed/failed transactions, and other unrelated wallets.
  - Resolves sender and recipient wallet addresses to Mezon usernames via [`wallets.json`](file:///Users/daitrinh/Projects/vinh-hub/wallets.json).
  - Displays the transaction content / note (`Nội dung: ...`) when provided.
- **Accurate Balance Reporting**:
  - Fetches the current fund balance from the Mezon Đồng Indexer REST API (`MEZON_MAINNET_API_URL`).
  - Caches readings for 5 minutes and falls back to stale readings if the indexer is temporarily unreachable.
- **Badminton Attendance & AI Summary Reminders (`@Vinh-CL`)**:
  - **Auto Điểm danh (Attendance Call)**: 09:00 sáng Thứ 2 (cho Thứ 3), Thứ 4 (cho Thứ 5), Thứ 6 (cho Thứ 7), tự động ping role `@Vinh-CL` (`1840693979155271680`).
  - **Nhắc lịch trước 1 tiếng linh hoạt theo giờ đặt sân**:
    - Thứ 3 & Thứ 5: Nhóm thường đặt sân lúc 19:00 hoặc 19:30 (hoặc chênh lệch 30 phút). Bot tự động đọc lịch sử chat để nhận diện giờ đặt sân thực tế và nhắc trước **1 tiếng** (18:00 nếu đá 19:00, 18:30 nếu đá 19:30).
    - Thứ 7: Thường đánh lúc 14:00 (hoặc 13:30), nhắc trước 1 tiếng (13:00 / 12:30).
  - **AI Message Summary (OpenAI / Gemini / NLP)**:
    - Thu thập tin nhắn từ kênh chat kể từ lúc điểm danh đến lúc nhắc lịch.
    - Hỗ trợ OpenAI-compatible API (`OPENAI_API_KEY`, `OPENAI_BASE_URL="https://llm.mrdnd.dev/v1"`, `OPENAI_MODEL="gpt-4o-mini"`) hoặc `GEMINI_API_KEY`, fallback về rule-based NLP parser nếu chưa cấu hình API key.
    - Tóm tắt danh sách người tham gia (`+1`, `đi`), vắng mặt (`off`, `bận`), và sân bãi (`Minh Hải`, `sân 1, 2`,...).
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

When money is sent via the bot intermediary (`a.nguyenvan transfer via bot: 1 xúc xích`):
```text
Nhận vào quỹ +20.000 VNĐ
Người gửi: a.nguyenvan (qua bot)
Nội dung: 1 xúc xích
15:22 16/11/2026
Quỹ hiện có 46.020.000 VNĐ
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

When auto-calling attendance (09:00 Mon/Wed/Fri):
```text
🏸 [ĐIỂM DANH CẦU LÔNG - THỨ BA (17/11)]
Chào cả nhà @Vinh-CL! Lịch đánh cầu lông tiếp theo diễn ra vào Thứ Ba (17/11/2026) lúc 18:00.

👉 Mọi người vào điểm danh xác nhận tham gia (+1 / đi / bận / off) trước 16:00 chiều để tiện chốt người và đặt sân nhé! 🔥
```

When reminding 1 hour before match (17:00 Tue/Thu, 13:00 Sat) with AI Summary:
```text
🏸 [NHẮC LỊCH ĐÁNH CẦU - THỨ BA (17/11)]
Chào @Vinh-CL, còn 1 tiếng nữa là đến giờ quật cầu rồi (⏰ 18:00)! 🏸

📋 TỔNG KẾT TÌNH HÌNH ĐIỂM DANH:
• ✅ Tham gia (6 người): dai.trinhduc, tai.cumanhtuan, nam.nguyen, ...
• ❌ Vắng mặt (2 người): hoang.nguyen, viet.le
• 🏟️ Sân bãi: sân 1 sân 2 nha mn (Minh Hải)

Chúc anh em một buổi giao lưu quật cầu thật cháy và nhiều tiếng cười! 🔥💪
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
| `BOT_WALLET_ADDRESS`| Optional | Intermediary bot wallet address (users can transfer here) |
| `BOT_PRIVATE_KEY`   | Optional | Private key or seed (hex or base58) to auto-forward from bot to fund |
| `MMN_NODE_ENDPOINT` | No | gRPC node endpoint, default `dong.mezon.ai:443` |
| `MMN_NODE_TLS` | No | Enable TLS for gRPC node, default `true` |
| `MMN_TOKEN_DECIMALS` | No | Token decimals, default `6` |
| `MEZON_MAINNET_API_URL` | No | Indexer REST URL, default `https://dong.mezon.ai/indexer-api` |
| `MMN_CHAIN_ID` | No | Chain ID for indexer, default `1337` |
| `TIMEZONE` | No | Timezone for post timestamps, default `Asia/Ho_Chi_Minh` |
| `COURIER_INTERVAL` | No | Interval in seconds between outbox delivery sweeps, default `30` |
| `SPORT_ENABLED` | No | Enable badminton attendance & reminder scheduler, default `true` |
| `SPORT_CLAN_ID` | No | Clan ID of KOMU, default `1779484504377790464` |
| `SPORT_CHANNEL_ID` | No | Sport channel ID (Vinh-sport-club), default `1840689219983380480` |
| `SPORT_ROLE_ID` | No | Role ID of `@Vinh-CL` to mention, default `1840693979155271680` |
| `SPORT_ROLE_NAME` | No | Role name to display, default `Vinh-CL` |
| `SPORT_STATE_PATH` | No | Path to persist attendance messages and session, default `badminton_state.json` |
| `OPENAI_API_KEY` | Optional | API key for OpenAI-compatible endpoint (used first for AI summary) |
| `OPENAI_BASE_URL` | Optional | Base URL for OpenAI-compatible endpoint, default `https://llm.mrdnd.dev/v1` |
| `OPENAI_MODEL` | Optional | Model name for OpenAI-compatible endpoint, default `gpt-4o-mini` |
| `GEMINI_API_KEY` | Optional | API key for Google Gemini (fallback if OpenAI key is blank) |

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
- `*quy` or `*fund`: The bot queries the Mezon indexer live and responds with the current fund balance.
- `*vinhhub`: Gửi ảnh menu VinhCorner Energy Hub (tự động tạo chữ ký Mezon CDN socket mới mỗi lần gửi để không bao giờ bị lỗi 403, thay thế hoàn toàn bot KOMU).
- `*register vinhhub <link_image>`: Cập nhật hoặc đổi link ảnh menu mới (lưu vào `vinhhub_state.json`).
- `*delete vinhhub`: Xoá ảnh tuỳ chỉnh và khôi phục ảnh menu mặc định.
- `*diemdanh` (hoặc `*vinhhub diemdanh`): Kích hoạt ngay thông báo điểm danh cho buổi đánh tiếp theo (ping role `@Vinh-CL`).
- `*nhaclich`: Gửi ngay tin nhắn nhắc lịch kèm AI tóm tắt danh sách tham gia, vắng mặt, và sân bãi.
- `*summary`: Xem nhanh tình hình điểm danh hiện tại mà không cần đợi cron.
- `*split <công_thức>`: Chia tiền sân + cầu linh hoạt (ví dụ: `*split 360k + 26*3`, `*split 360k + 26k*4`, `*split 360k san 45k cau`, `*split 450k`). Tự động tính tổng tiền, chia đều theo slot buổi đánh hôm trước, làm tròn lên 1.000đ, tag người tham gia và gửi mã QR thanh toán.
- `*money <số_tiền>`: Chia tiền theo đơn giá slot cố định (ví dụ: `*money 45k`).
- `*paid [ngày] [@user...]`:
  - `*paid`: Hiển thị dashboard tiến độ thu tiền buổi gần nhất (ai đã đóng, ai chưa đóng, tổng đã thu / còn thiếu).
  - `*paid 17/11` (hoặc `*paid 1711`): Xem dashboard tiến độ thu tiền của buổi ngày 17/11 trong lịch sử.
  - `*paid @user1 @user2`: Đánh dấu thành viên đã thanh toán buổi hiện tại.
  - `*paid 17/11 @user1`: Đánh dấu thành viên đã thanh toán cho buổi ngày 17/11.
  - *Mẹo*: Thành viên có thể react `:checked:`, `:check:` (custom emoji trong clan KOMU) hoặc `✅`, `💵`, `👍` trực tiếp vào tin nhắn chia tiền để bot tự động ghi nhận đã đóng tiền cho đúng buổi đó! Bot cũng tự động thả sẵn reaction `:checked:` để mọi người bấm 1 chạm.
- `*unpaid [ngày] [@user...]`:
  - `*unpaid`: Lọc danh sách những người chưa đóng tiền buổi gần nhất, tag trực tiếp để nhắc nhở và đính kèm mã QR thanh toán + cú pháp chuyển khoản.
  - `*unpaid 17/11`: Lọc danh sách và nhắc nhở những người chưa đóng tiền của buổi ngày 17/11.
  - `*unpaid @user1`: Chuyển trạng thái về chưa thanh toán (nếu lỡ mark nhầm).
  - `*unpaid 17/11 @user1`: Chuyển trạng thái về chưa thanh toán của buổi 17/11.
- **Phân biệt thanh toán giữa các buổi**:
  - *Qua chuyển khoản ngân hàng*: Bot tự động tạo và hướng dẫn cú pháp chuyển khoản chuẩn trong mọi hoá đơn và tin nhắn nhắc nhở: `CL <DDMM> <tên>` (Ví dụ: `CL 1711 dai.trinhduc`). Thủ quỹ chỉ cần nhìn nội dung sao kê là biết ngay thanh toán cho buổi nào và của ai.
  - *Qua tin nhắn Mezon*: Mỗi hoá đơn được lưu kèm `MessageID` riêng biệt (`BillHistory`). Thành viên thả reaction `:checked:` vào tin nhắn chia tiền của buổi nào thì bot sẽ ghi nhận chính xác cho buổi đó, không bao giờ bị ghi đè hay nhầm lẫn.
- `*bxh [tháng]` (hoặc `*stats`): Xem bảng xếp hạng tháng (ví dụ: `*bxh`, `*bxh 11/2026`). Thống kê tổng số buổi, Top "Chăm chỉ" trao danh hiệu `👑 Chiến thần quật cầu`, và Top "Chúa sủi" trao danh hiệu `🍗 Chúa sủi của tháng`. Bot cũng tự động cron chạy vào 20:00 ngày cuối tháng để thông báo vào kênh.
- `*money qr <link_qr>` (hoặc `*register qr <link_qr>`): Cập nhật mã QR thanh toán mới.
- `*help`: Hiển thị bảng hướng dẫn lệnh.

---

## Deploying on Ubuntu (systemd Service)

To run the bot as a background service on Ubuntu that automatically restarts on crashes and system reboots:

### 1. Build Linux Binary
You can build directly on macOS or CI:
```bash
make build-linux
```
This produces a standalone, statically linked executable: `bin/fundbot-linux-amd64`.

### 2. Quick Deploy with Script (on Ubuntu Server)
Clone or copy the repository to the Ubuntu server, then run:
```bash
sudo ./deploy/setup.sh
```

### 3. Manual Deployment Steps
If you prefer configuring manually:

1. **Copy binary and files to `/opt/fundbot`**:
   ```bash
   sudo mkdir -p /opt/fundbot
   sudo cp bin/fundbot-linux-amd64 /opt/fundbot/fundbot
   sudo chmod +x /opt/fundbot/fundbot
   sudo cp wallets.json /opt/fundbot/
   sudo cp .env /opt/fundbot/.env
   sudo chmod 600 /opt/fundbot/.env
   ```

2. **Install systemd unit**:
   Copy [`deploy/fundbot.service`](file:///Users/daitrinh/Projects/vinh-hub/deploy/fundbot.service) to `/etc/systemd/system/fundbot.service`:
   ```bash
   sudo cp deploy/fundbot.service /etc/systemd/system/fundbot.service
   ```

3. **Enable and start the service**:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable fundbot
   sudo systemctl start fundbot
   ```

4. **Useful Service Management Commands**:
   - Check status: `sudo systemctl status fundbot`
   - View real-time logs: `journalctl -u fundbot -f`
   - Restart bot: `sudo systemctl restart fundbot`
   - Stop bot: `sudo systemctl stop fundbot`

### 4. Updating the Bot on Server

To update the running bot without encountering `Text file busy` errors:
```bash
git pull origin main
make build-linux

# Use install to safely replace the active executable inode:
sudo install -m 755 bin/fundbot-linux-amd64 bin/fundbot
# (Hoặc nếu deploy tại /opt/fundbot: sudo install -m 755 bin/fundbot-linux-amd64 /opt/fundbot/fundbot)

sudo systemctl restart fundbot
sudo journalctl -u fundbot -f
```

