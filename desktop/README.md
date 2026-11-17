# Go Chat desktop

App desktop (Tauri v2 + React) cho server go-chat.

## Cần có

- Node + pnpm
- Rust: toolchain được chỉ định trong `rust-toolchain.toml`, rustup tự dùng đúng bản
- Linux: `libwebkit2gtk-4.1-dev`, `libsoup-3.0-dev`. Để đóng gói `.deb`/AppImage cần thêm
  `librsvg2-dev` và `libssl-dev`

## Chạy

```sh
# 1. Server (thư mục gốc repo), mặc định cổng 4000, DB chat.db
go run .

# 2. App desktop
cd desktop
pnpm install
pnpm tauri dev
```

Lần đầu mở app, chọn "Tạo tài khoản". Máy chủ mặc định là `localhost:4000`, bấm "Đổi" để trỏ sang máy khác.

Không cần Rust vẫn thử được giao diện trên trình duyệt: `pnpm dev` rồi mở http://localhost:1420.

## Build

Bản build giao diện nằm ở `../web/ui/dist`, dùng chung cho hai nơi:
server Go nhúng vào binary (bản web), và Tauri đóng gói thành app.
Từ thư mục gốc repo:

```sh
make server    # giao diện + server Go -> ./go-chat
make desktop   # .deb + AppImage trong desktop/src-tauri/target/release/bundle
```

Khi chạy dưới dạng web do server phục vụ, app tự dùng địa chỉ trang đang mở làm máy chủ
và ẩn ô "Máy chủ" (xem `src/lib/platform.ts`).

## Cấu trúc

```
src/
  lib/api.ts        REST client + kiểu dữ liệu (khớp package model bên Go)
  lib/socket.ts     ChatSocket: kết nối websocket, tự kết nối lại, heartbeat, hàng đợi tin
  state/chat.ts     reducer: kênh, tin nhắn, tin đang gửi, online, đang gõ, thành viên
  state/ChatProvider.tsx  nối socket + REST vào reducer, cung cấp hành động cho UI
  components/       màn đăng nhập, sidebar, khung chat, thành viên, các hộp thoại
```

### ChatSocket xử lý những gì

- **Xác thực**: token gửi qua `?token=`. Server đóng với mã `4001` khi token sai/hết hạn hoặc
  phiên bị đăng xuất (kể cả từ máy khác, khi đổi mật khẩu) → app quay về màn đăng nhập.
- **Kết nối lại**: exponential backoff 0.5s → 30s có jitter; kết nối lại ngay khi máy có mạng trở lại.
- **Heartbeat**: im lặng 25s thì gửi `ping`, 10s không có phản hồi thì coi kết nối đã chết và kết nối lại.
- **Gửi tin khi mất kết nối**: tin được xếp hàng và tự gửi khi kết nối lại.
- **Xác nhận tin**: mỗi tin có `client_id`; server phát lại `message.new` kèm `client_id` là thành công.
  Lỗi từ server, mất kết nối trước khi được xác nhận, hoặc quá 15s → tin hiện "Chưa gửi được" với nút Gửi lại.
- **Lỡ tin**: sau khi kết nối lại, tải lại danh sách kênh (số chưa đọc) và trang tin mới nhất của kênh đang mở.

## API server

Mọi endpoint trừ đăng ký/đăng nhập cần header `Authorization: Bearer <token>`.
Lỗi trả về `{"error": "<câu hiển thị cho người dùng>"}`.

| Method | Đường dẫn | Mô tả |
|---|---|---|
| POST | `/api/auth/register` | `{username, email, password, display_name}` → `{token, user}` |
| POST | `/api/auth/login` | `{login, password}` (login là username hoặc email) → `{token, user}` |
| POST | `/api/auth/logout` | Huỷ phiên hiện tại, đóng websocket của phiên |
| GET | `/api/me` | Thông tin tài khoản |
| PATCH | `/api/me` | `{display_name?, status_text?, avatar?}` |
| POST | `/api/me/avatar` | multipart `file` (PNG/JPEG/GIF/WebP ≤ 5 MB) → đặt làm ảnh đại diện |
| GET | `/api/avatars/{key}` | ảnh đại diện đã tải lên (công khai) |
| GET | `/api/users?q=` | Tìm người dùng (để nhắn riêng) |
| POST | `/api/dms` | `{user_id}` → mở/tạo cuộc trò chuyện riêng (kênh `kind: "dm"`) |
| GET | `/api/gifs?q=&offset=` | Tìm GIF qua GIPHY (`q` rỗng: thịnh hành); `enabled: false` nếu server chưa có key |
| PATCH | `/api/messages/{id}` | `{content}` sửa tin của mình |
| DELETE | `/api/messages/{id}` | Xoá tin (người viết, hoặc chủ kênh) |
| PUT | `/api/me/password` | `{current_password, new_password}`; đăng xuất các thiết bị khác |
| GET | `/api/channels` | Kênh đã tham gia, kèm `unread_count`, `member_count` |
| GET | `/api/channels/discover` | Kênh chưa tham gia |
| POST | `/api/channels` | `{name, description}` tạo kênh (người tạo là chủ kênh) |
| GET/PATCH/DELETE | `/api/channels/{id}` | Xem / sửa / xoá kênh (sửa, xoá: chỉ chủ kênh) |
| POST | `/api/channels/{id}/join`, `/leave` | Tham gia / rời kênh |
| GET | `/api/channels/{id}/members` | Thành viên (chỉ thành viên xem được) |
| GET | `/api/channels/{id}/messages?before=&limit=` | Lịch sử, cũ → mới, kèm `has_more` |
| POST | `/api/channels/{id}/messages` | Gửi tin kèm file (multipart: `content`, `client_id`, `files` ×≤10, mỗi file ≤20 MB) |
| GET | `/api/unfurl?url=` | Xem trước link: `{kind: page/image/video/audio/youtube/none, title, description, site_name, image, embed_url}`. Server chặn địa chỉ nội bộ (chống SSRF) |
| GET | `/api/messages/{id}/thread` | Tin gốc + toàn bộ trả lời trong luồng |
| POST | `/api/messages/{id}/reactions` | `{emoji}`: thả/bỏ cảm xúc (bấm lại để bỏ), trả về danh sách mới |
| GET | `/api/files/{key}[?download=1]` | Tải file; ảnh png/jpeg/gif/webp xem trực tiếp, loại khác luôn tải về |

Tin nhắn có thêm `attachments` (file/ảnh), `mentions` (id người được nhắc), `reactions`
(`[{emoji, user_ids}]`), `parent_id`/`parent_user_id` (nếu là trả lời trong luồng) và `thread`
(`{reply_count, last_reply_at, participant_ids}` ở tin gốc). Lịch sử kênh chỉ trả tin ở dòng chính;
gửi trả lời bằng `message.send` kèm `parent_id` (hoặc field `parent_id` khi upload file). Kênh có
`mention_count` (số tin chưa đọc nhắc tên mình). `@username` và `@all` trong nội dung được server
nhận diện (chỉ tính thành viên kênh, không tính người gửi).

## Websocket `/ws?token=<token>`

Client gửi:

```json
{ "type": "message.send", "channel_id": 1, "content": "...", "client_id": "uuid" }
{ "type": "message.send", "channel_id": 1, "content": "", "client_id": "uuid", "embed": { "type": "sticker", "id": "text/oke" } }
{ "type": "typing", "channel_id": 1 }
{ "type": "read", "channel_id": 1, "message_id": 42 }
{ "type": "ping" }
```

Server gửi (danh sách đầy đủ ở `controller/events.go`): `ready`, `message.new`, `typing`, `presence`,
`user.updated`, `channel.joined`, `channel.left`, `channel.updated`, `channel.deleted`,
`member.joined`, `member.left`, `reaction.updated`, `thread.updated`, `read.updated`, `message.updated`, `error`, `pong`.

Mỗi kết nối nhận tin của **mọi** kênh mình tham gia, nên số chưa đọc cập nhật tức thì.
