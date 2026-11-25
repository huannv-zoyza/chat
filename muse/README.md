# muse — cầu nối muse.ai, phơi ra API tương thích OpenAI

Nói chuyện thẳng với máy ảo Muse của tài khoản bạn qua đúng giao thức mà app desktop Muse4AI dùng:
REST của `muse.ai` → đánh thức máy ảo → WebSocket `hatch.metaaivm.com` → bắt tay **Noise XX** → HTTP
đóng gói protobuf bên trong đường hầm.

Không thêm dependency nào: `crypto/ecdh` của thư viện chuẩn lo X25519, `github.com/coder/websocket` đã
có sẵn trong go.mod.

## Chạy

```bash
export MUSE_COOKIE='datr=...; c_user=...; xs=...'   # copy từ trình duyệt đã đăng nhập muse.ai
export MUSE_API_KEY='sk-tuy-ban-dat'                # khoá bảo vệ API này
go run ./cmd/museapi
```

Lấy cookie: mở `muse.ai` đã đăng nhập → DevTools → tab Network → chọn một request bất kỳ tới `muse.ai`
→ Request Headers → copy nguyên giá trị `Cookie`.

| Biến | Mặc định | Ý nghĩa |
|---|---|---|
| `MUSE_COOKIE` | — | **Bắt buộc.** Cookie phiên muse.ai |
| `MUSE_API_KEY` | trống | Khoá Bearer bảo vệ API; trống = không xác thực |
| `MUSE_ADDR` | `127.0.0.1:8099` | Địa chỉ lắng nghe |
| `MUSE_PROXY` | — | Proxy đi ra, ví dụ `http://127.0.0.1:8888` |
| `MUSE_ORIGIN` | `https://muse.ai` | |
| `MUSE_GATEWAY` | `wss://hatch.metaaivm.com/v1/noise` | |
| `MUSE_UA` | Chrome trên Linux | Nên giống trình duyệt đã lấy cookie |

## Gọi thử

```bash
curl http://127.0.0.1:8099/v1/chat/completions \
  -H "Authorization: Bearer $MUSE_API_KEY" -H 'content-type: application/json' \
  -d '{"model":"muse","messages":[{"role":"user","content":"2+2 bằng mấy?"}]}'

# stream (SSE đúng khuôn OpenAI)
curl -N http://127.0.0.1:8099/v1/chat/completions \
  -H "Authorization: Bearer $MUSE_API_KEY" -H 'content-type: application/json' \
  -d '{"model":"muse","stream":true,"messages":[{"role":"user","content":"kể chuyện ngắn"}]}'

# sinh ảnh → b64_json
curl http://127.0.0.1:8099/v1/images/generations \
  -H "Authorization: Bearer $MUSE_API_KEY" -H 'content-type: application/json' \
  -d '{"prompt":"ấm trà gốm đỏ trên bàn trắng","n":1}'
```

Dùng được thẳng với SDK OpenAI:

```python
from openai import OpenAI
c = OpenAI(base_url="http://127.0.0.1:8099/v1", api_key="sk-tuy-ban-dat")
print(c.chat.completions.create(model="muse", messages=[{"role":"user","content":"chào"}]).choices[0].message.content)
```

## Nối tiếp hội thoại

API của OpenAI vốn không trạng thái, còn Muse thì nhớ hội thoại. Cầu nối xử lý cả hai:

- **Không gửi header `X-Muse-Session`** → mở hội thoại Muse mới, gửi phẳng toàn bộ `messages` để đủ ngữ cảnh.
- **Gửi `X-Muse-Session: <id>`** → chỉ gửi tin nhắn user cuối, Muse tự nhớ phần trước.

Mọi phản hồi đều trả lại `X-Muse-Session` để bạn dùng cho lượt sau.

## Dùng như thư viện

```go
c, _ := muse.New(muse.Config{Cookie: os.Getenv("MUSE_COOKIE")})
defer c.Close()
ans, err := c.Ask(ctx, []map[string]any{muse.TextItem("vẽ con mèo")}, muse.AskOptions{
    OnDelta: func(d string) { fmt.Print(d) },
})
// ans.Text, ans.Files (đường dẫn trong workspace), ans.SessionID
ctype, data, _ := c.FetchFile(ctx, ans.Files[0].Path, 32<<20)
```

## Kiểm thử

```bash
go test -race ./muse/
```

15 test, không cần tài khoản Muse thật:

- **`TestNoiseVectorsMatchTypeScript`** đối chiếu từng byte với bản TypeScript gốc (vector sinh bằng
  chính mã TS chạy trên Node): hằng tên giao thức, `mixHash`, `hkdf`, `encryptAndHash`.
- **`TestHandshakeRoundTrip`** bắt tay trọn vẹn initiator ↔ responder, kiểm cả hai chiều khoá.
- **`TestNonceLayout`** nonce 12 byte, bộ đếm uint64 big-endian ở offset 4.
- `fakegw_test.go` dựng **gateway giả nói đúng giao thức thật** (WebSocket + Noise vai responder +
  đóng khung protobuf), nhờ đó test được toàn tuyến: đánh thức máy ảo → hội thoại → SSE → sinh ảnh.
- **`TestLargePayloadChunking`** gửi ~180 KB để chắc chắn phần cắt/ghép mảnh đúng.
- **`TestWakeAcceptsReadyAndLive`** `vm_state` nhận `running`/`live`/`ready`, không phân biệt hoa thường,
  và coi giá trị rỗng/thiếu là đang chạy.

## Tích hợp vào Ask AI của go-chat

Đã nối sẵn. Chỉ cần đặt `MUSE_COOKIE` rồi chạy `go-chat` như thường:

```bash
export MUSE_COOKIE='datr=...; c_user=...; xs=...'
export MUSE_UA='<user-agent của trình duyệt đã copy cookie>'   # nên đặt, xem mục Cookie
./go-chat
# Muse bật: cầu nối nội bộ tại http://127.0.0.1:38xxx/v1 (chỉ loopback)
# Trợ lý AI (@ai): chat [groq gemini muse], ...
```

Không có `MUSE_COOKIE` thì không có gì thay đổi — chuỗi nhà cung cấp giữ nguyên như trước.

**Cách nối (một tiến trình).** `muse.StartLoopback` mở một listener trên `127.0.0.1` với cổng do hệ
điều hành cấp, ngay trong tiến trình `go-chat`, rồi `main.go` nối `ai.Muse(baseURL, key)` vào cuối
`aiChat.Providers`. Cầu nối **không** gắn vào router công khai của go-chat: làm vậy là phơi tài khoản
Muse ra cho mọi người tới được máy chủ chat. Khoá Bearer sinh ngẫu nhiên mỗi lần khởi động và chỉ nằm
trong bộ nhớ, trao thẳng cho provider.

**Muse đứng cuối chuỗi** vì mỗi lượt phải đánh thức máy ảo nên chậm hơn hẳn Groq/Gemini. Các bên trước
lỗi hết thì mới tới nó.

**`NoTools: true`** — Muse không nhận `tools` kiểu OpenAI, nên khi chạy agent nó bị bỏ qua, đúng cơ chế
sẵn có của `ai`. **Chưa khai `Vision`**: Muse đọc được ảnh nhưng cầu nối chưa chuyển phần `image_url`
của OpenAI thành `ImageItem`.

> Lưu ý: `ai.NewChat` đặt `HTTP.Timeout = 3 phút`. Lượt chat của Muse thường kịp, nhưng một yêu cầu
> nặng (dựng video, xử lý tệp lớn) có thể vượt. Cần thì nâng timeout đó khi bật Muse.

## Hai điều phải biết (kiểm chứng trên tài khoản thật, 21/11/2026)

**1. `sec-fetch-*` là bắt buộc.** Thiếu ba header Fetch Metadata thì `/api/hatch/token` và
`/api/hatch/noise-notary-token` trả `403 {"error":"Forbidden"}` — trong khi `/api/session` vẫn 200,
nên rất dễ tưởng là cookie hỏng. Thêm `sec-fetch-dest: empty`, `sec-fetch-mode: cors`,
`sec-fetch-site: same-origin` là 403 → 200 ngay. Bản desktop không bao giờ gặp vì nó chạy trong trang
Chromium thật, trình duyệt tự gắn. Đã xử lý sẵn trong `session.go`.

**2. `vm_state` thật trả về là `"UNKNOWN"`.** Kèm `vm_state_source: "live"`, trong khi web app của Muse
vẫn nối và chat bình thường. `VMRunning` vì thế là **danh sách chặn** (chỉ `suspended`/`stopped`/… mới
cần đánh thức), không phải danh sách cho phép — xem ghi chú trong `session.go`.

## Kiểm tra nhanh một cookie

```bash
MUSE_COOKIE='...' MUSE_UA='...' go run ./cmd/musetry
# → đang kết nối gateway…
# ✓ gateway mở sau 2.3s
# → gửi câu hỏi…
# 4
# ✓ trả lời sau 4.3s   text = "4"
```

Lỗi sẽ nói rõ bước hỏng: `session` / `wake` / `token` / `socket` / `handshake` / `challenge`.

## Khác biệt có chủ ý so với bản desktop

1. **`vm_state` khoan dung.** Bản desktop (`driver.ts`) so khớp cứng `=== "RUNNING"` nên bỏ sót
   `ready`/`live` — chính lỗi làm mọi việc kẹt ở "Máy ảo Muse chưa khởi động xong". Ở đây chỉ có một
   hàm `VMRunning` duy nhất, dùng chung mọi nơi.
2. **Ack chắc chắn hơn.** Bản TS chỉ ghi bản ghi cuối khi dòng đó thiếu `\n`; ở đây luôn giữ bản ghi
   mới nhất nên không bao giờ hụt ack của `/chat/stream`.
3. **Không có ngân sách 90 giây cắt ngang.** Desktop cắt kết nối ở 90s để đẩy việc sang tài khoản khác;
   ở đây chờ đủ `VMWakeWait` (90s) cho máy ảo thức rồi mới chạy tiếp, không bỏ cuộc giữa chừng.

## Chưa làm

- Đính kèm tệp lớn qua workspace (`/fs/*` upload) — hiện chỉ có ảnh/tệp nội tuyến base64.
- Kéo dài video (`extend`) và Vibes.
- Đếm token thật trong `usage` (đang trả 0).
