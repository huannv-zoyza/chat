# Go Chat

Chat theo kênh cho mạng nội bộ: server Go + SQLite, giao diện React dùng được qua trình duyệt
hoặc cài thành app desktop (Tauri).

## Triển khai trong mạng LAN

### 1. Chạy server (bắt buộc, trên một máy)

```sh
make server          # build giao diện web + server -> ./go-chat (một file duy nhất)
./go-chat            # cổng 4000, dữ liệu lưu ở chat.db
```

Tuỳ chọn: `-addr :8080` đổi cổng, `-db /duong/dan/chat.db` đổi nơi lưu dữ liệu,
`-uploads /duong/dan/uploads` đổi nơi lưu file/ảnh người dùng gửi (mặc định `uploads/`).
Sao lưu = copy `chat.db` và thư mục `uploads/`.

Mở cổng nếu máy có firewall (chỉ cho mạng LAN):

```sh
sudo ufw allow from 192.168.30.0/24 to any port 4000 proto tcp
```

Xem IP của máy server: `hostname -I` (ví dụ `192.168.30.36`).

### 2. Người dùng vào chat — chọn một trong hai cách

**Cách A, trình duyệt (không cần cài):** mở `http://192.168.30.36:4000`.

**Cách B, app desktop:** cài file trong `desktop/src-tauri/target/release/bundle/` (tạo bằng `make desktop`):

| Máy | File | Cài |
|---|---|---|
| Ubuntu/Debian | `deb/Go Chat_0.1.0_amd64.deb` | `sudo apt install "./Go Chat_0.1.0_amd64.deb"` |
| Linux khác | `appimage/Go Chat_0.1.0_amd64.AppImage` | `chmod +x` rồi chạy trực tiếp |
| Windows / macOS | — | phải build trên chính máy Windows/Mac đó: `make desktop` |

Mở app, ở màn đăng nhập bấm **Đổi** cạnh "Máy chủ", nhập `192.168.30.36:4000`.

### Chạy thường trực (tự khởi động lại)

```sh
make install-service   # cài go-chat thành dịch vụ systemd của user và chạy luôn
make deploy            # sau khi sửa code: build lại + khởi động lại dịch vụ
make logs              # xem log
```

Dịch vụ tự chạy lại sau 3 giây nếu process lỗi/bị kill, và tự chạy khi máy khởi động
(cần `loginctl enable-linger`, máy này đã bật). Đừng chạy thêm `./go-chat` bằng tay
cùng lúc, sẽ tranh cổng 4000.

### Đưa ra internet qua Cloudflare Tunnel

Dùng tài khoản Cloudflare đã đăng nhập sẵn trong `~/.cloudflared` (tên miền quyenpv.site),
tạo một tunnel riêng cho chat, không đụng tới tunnel khác:

```sh
cloudflared tunnel create chat
make install-tunnel TUNNEL_NAME=chat TUNNEL_HOST=chat.quyenpv.site   # ghi ~/.cloudflared/go-chat.yml + cài dịch vụ
make route-dns      TUNNEL_NAME=chat TUNNEL_HOST=chat.quyenpv.site   # trỏ DNS về đúng tunnel
```

Đừng chạy `cloudflared tunnel route dns ...` trần: `~/.cloudflared/config.yml` có sẵn
`tunnel: agent-review` nên cloudflared sẽ trỏ DNS nhầm sang tunnel đó. `make route-dns`
luôn truyền `--config` riêng và id tunnel.

Tunnel/tên miền khác: `make install-tunnel TUNNEL_NAME=<tên-tunnel> TUNNEL_HOST=<tên-miền>`
(route DNS đúng tên miền đó trước; tên miền đã có bản ghi thì thêm `--overwrite-dns`). Tunnel chạy trên chính máy này, nên khi máy tắt/treo thì
bên ngoài không vào được cho tới khi máy chạy lại; cả server lẫn tunnel tự khởi động lại.

### Cập nhật phiên bản

- Bản web: `make server`, khởi động lại `./go-chat`; người dùng tải lại trang là có bản mới.
- App desktop: `make desktop` rồi gửi lại file cài.

## Phát triển

```sh
go run .                          # server (cổng 4000)
cd desktop && pnpm dev            # giao diện trên http://localhost:1420, tự tải lại khi sửa code
cd desktop && pnpm tauri dev      # hoặc chạy trong cửa sổ app desktop
make test                         # test Go
```

Chi tiết API, giao thức websocket và cấu trúc frontend: [desktop/README.md](desktop/README.md).

## Tính năng

- Kênh: tạo, khám phá, tham gia/rời, chủ kênh đổi tên/mô tả hoặc xoá.
- Tin nhắn: lịch sử, số chưa đọc, đang gõ, online/offline, emoji.
- Gửi file và ảnh: nút đính kèm, kéo thả, hoặc dán ảnh (Ctrl+V). Tối đa 10 file/tin, 20 MB/file (video, âm thanh tới 95 MB). Ổ đĩa máy chủ còn dưới 1 GB trống thì tạm không nhận file.
- Link: bấm được, tự hiện thẻ xem trước (tiêu đề, mô tả, ảnh); link YouTube, video, âm thanh phát ngay trong app.
  File video/âm thanh tải lên cũng phát được trong tin.
- Thả cảm xúc: rê chuột vào tin (điện thoại: nhấn giữ) để thả 👍 ❤️ 😂…; bấm lại để bỏ.
- Trả lời theo luồng: "Trả lời trong luồng" mở panel bên phải; dòng chính chỉ hiện "N trả lời".
- Sửa / xoá tin nhắn của mình (rê chuột hoặc nhấn giữ; bấm ↑ ở ô soạn trống để sửa tin gần nhất). Chủ kênh xoá được tin của người khác. Tin bị xoá còn lại dòng "Tin nhắn đã bị xoá", file đính kèm bị xoá khỏi server.
- Trả lời trích dẫn (nút ↩ trên tin): tin mới kèm bản trích, bấm để nhảy tới tin gốc. Ghim tin (menu ⋯ hoặc nhấn giữ): nút 📌 trên đầu kênh liệt kê tin ghim. Chia sẻ tin (nút ↪ trên tin, menu ⋯ hoặc nhấn giữ): gửi sang kênh / tin nhắn riêng khác dạng bản sao (chữ, ảnh, file, sticker) hoặc thẻ liên kết tới tin gốc (game, bình chọn, câu trả lời AI hay bất kỳ tin nào: thẻ hiện trạng thái trực tiếp và nút "Xem tin gốc" / "Vào phòng chơi"; người chưa ở kênh công khai được mời tham gia, kênh riêng tư thì không lộ nội dung); "Sao chép liên kết" (…/m/<id>, mở là nhảy tới tin, dán vào chat hiện thành thẻ); "Chia sẻ ra ngoài" (điện thoại); phòng game có nút "Mời bạn bè".
- Tìm kiếm (🔍 ở thanh bên hoặc Ctrl+K): tìm toàn văn không cần dấu ("da lat" ra "Đà Lạt"), lọc theo kênh, bấm kết quả để nhảy tới tin (tự tải tin cũ). Tin nhắn riêng đã mã hoá không tìm được.
- Nhắc việc: menu ⋯ của tin → "Nhắc tôi về tin này" (20 phút, 1 giờ, chiều nay, sáng mai, thứ Hai tới hoặc tự chọn giờ, kèm ghi chú). Tới giờ có thông báo; mục "Nhắc việc" ở thanh bên để xem / hoãn / xong.
- Bình chọn: nút "+" trên ô soạn tin của kênh → "Bình chọn". 2–10 lựa chọn, tuỳ chọn nhiều đáp án / ẩn danh; bấm để bỏ phiếu, bấm lại để rút; kết quả cập nhật realtime; người tạo kết thúc hoặc mở lại được; có thể đặt hạn chót tự kết thúc và cho mọi người thêm lựa chọn. Không có trong tin nhắn riêng (đã mã hoá đầu cuối).
- Sticker & GIF: nút sticker cạnh nút emoji. 40 sticker có sẵn (câu cửa miệng tiếng Việt + emoji cỡ lớn), thêm sticker mới trong `desktop/src/lib/stickers.ts`. Tìm GIF qua GIPHY: lấy API key miễn phí tại https://developers.giphy.com rồi chạy `make giphy-key KEY=<key>` (key lưu ở `.env` cạnh server, không vào repo). Chưa có key vẫn gửi được file GIF từ máy.
- Mã hoá đầu cuối cho tin nhắn riêng (giống Messenger): chữ, file/ảnh, sticker/GIF được mã hoá trên máy người gửi (X25519 + XChaCha20-Poly1305, libsodium), server chỉ lưu bản mã. Khoá bí mật được sao lưu lên server dưới dạng đã khoá bằng mã PIN (Argon2id) để đăng nhập máy khác vẫn đọc được lịch sử; quên PIN thì tạo khoá mới và mất tin mã hoá cũ. Nút 🔒 ở đầu cuộc trò chuyện hiện mã an toàn để hai bên so khớp. Kênh nhóm chưa mã hoá đầu cuối; tin mã hoá không có xem trước link.
- Gọi điện trong LAN: trình duyệt chỉ cho micro/camera trên https, nên server mở thêm **https://<IP máy chủ>:4443** (chứng chỉ tự ký tạo sẵn trong `tls/`, lần đầu bấm "Nâng cao → Tiếp tục"). Mở bằng http thì app báo kèm link https này. Server cũng chạy sẵn **TURN** (cổng 3478 UDP/TCP, chuyển tiếp qua UDP 49160–49200) để hai máy trong LAN không nối thẳng được (mạng chặn multicast / cách ly wifi) vẫn gọi được qua máy chủ. Nếu máy chủ bật tường lửa: `sudo ufw allow 4443/tcp && sudo ufw allow 3478 && sudo ufw allow 49160:49200/udp`. Tắt: `-tls-addr "" -turn-addr ""`.
- Gọi nhóm trong kênh (tối đa 8 người, kiểu mesh): nút 📞 / 🎥 trên đầu kênh; người trong kênh nhận lời mời "Tham gia", kênh đang gọi có dải "Đang có cuộc gọi nhóm" và biểu tượng ở thanh bên. Thu nhỏ màn hình gọi để vẫn nhắn tin. Hết người thì có nhật ký (thời lượng, số người).
- Chia sẻ màn hình khi đang gọi (1-1 hoặc nhóm): nút "Màn hình" trong lúc gọi, chọn cả màn hình / một cửa sổ / một tab. Màn hình thay cho hình camera; gọi thoại vẫn chia sẻ được. Bấm "Dừng chia sẻ" (hoặc nút dừng của trình duyệt) để quay lại camera. Cần trình duyệt máy tính (Chrome, Edge, Firefox); điện thoại chưa hỗ trợ.
- Bản web tự nhắc "Có phiên bản mới — Tải lại" khi máy chủ được deploy bản giao diện mới (tab cũ không biết tính năng mới như gọi điện).
- Gọi thoại / video 1-1 (WebRTC): nút 📞 / 🎥 ở đầu tin nhắn riêng. Máy chủ chỉ làm tổng đài (đổ chuông, nghe, cúp, chuyển tín hiệu); âm thanh và hình đi thẳng giữa hai máy, mã hoá DTLS-SRTP. Cuộc gọi xong / nhỡ có nhật ký trong tin nhắn, kèm nút "Gọi lại". Cần https (tunnel) hoặc app desktop: trình duyệt chặn micro/camera trên http thường (http://IP-LAN). Hai máy khác mạng mà nhà mạng chặn kết nối trực tiếp thì cần thêm máy chủ TURN: đặt `ICE_SERVERS='[{"urls":["stun:stun.cloudflare.com:3478"]},{"urls":["turn:turn.example.com:3478"],"username":"u","credential":"p"}]'` trong `.env` rồi `make deploy` (mặc định chỉ có STUN của Cloudflare).
- Trợ lý AI trong kênh: gõ `@ai` kèm câu hỏi (kể cả trong luồng), bot "Trợ lý AI" trả lời ngay trong kênh, chữ hiện dần, hiện tiến trình (đang đọc tin, xem ảnh, tìm web…) và ghi cuối câu trả lời đã đọc những gì. Trước khi trả lời, AI tự quyết định cần ngữ cảnh gì: đọc toàn bộ tin trong khoảng thời gian được hỏi ("tóm tắt 1 ngày", "hôm qua", "tuần này"…, tối đa 600 tin, gồm cả trả lời trong luồng), tìm lại tin cũ trong kênh (chỉ kênh đang hỏi), xem ảnh (mô tả được lưu lại cho lần sau), đọc file chữ gửi kèm (txt, md, csv, json, code…), tìm web (DuckDuckGo; thêm `make ai-key PROVIDER=tavily|brave` để tốt hơn) và đọc trang kết quả. Giá cả / số liệu chỉ được nêu khi có nguyên văn trong kết quả web, kèm nguồn. Ngữ cảnh lớn tự chuyển sang model ngữ cảnh dài (Pollinations có key) vì Groq miễn phí chỉ nhận ~8.000 token/phút. "Trả lời tin này": trả lời trích dẫn rồi gõ `@ai`. Gõ `@ai vẽ …` (thêm `--ngang` / `--doc` để đổi khung) hoặc nút "+" → "Tạo ảnh AI" để tạo ảnh, AI tự dịch mô tả tiếng Việt sang tiếng Anh cho máy vẽ. Vẽ lại từ ảnh gốc (đổi phong cách, biến thành nhân vật hoạt hình, thêm chi tiết…): gửi ảnh kèm tin `@ai vẽ …` / `@ai biến …`, hoặc trả lời trích dẫn một tin có ảnh rồi gõ như vậy, hoặc chọn "Ảnh gốc" trong hộp thoại (cần key Pollinations hoặc model ảnh OpenRouter; ảnh gốc được gửi tới dịch vụ AI). Chỉ trong kênh (tin nhắn riêng mã hoá đầu cuối, máy chủ không đọc được). Mỗi người tối đa 8 câu hỏi và 4 ảnh mỗi phút.
  - Không cần cấu hình vẫn chạy (Pollinations miễn phí, không key; ảnh bị giới hạn 1 ảnh/45 giây và chất lượng vừa phải). Thêm key để tốt hơn, bên nào có key thì dùng trước, lỗi / hết lượt thì tự chuyển sang bên sau: chat `make ai-key PROVIDER=gemini|groq|openrouter KEY=…`, vẽ ảnh `make ai-key PROVIDER=together KEY=…` hoặc `PROVIDER=cloudflare KEY=<token> ACCOUNT=<account id>` (đều có gói miễn phí, link lấy key ghi trong Makefile). Model chat được tự chọn từ danh sách của nhà cung cấp; đặt `GEMINI_MODEL`, `GROQ_MODEL`, `OPENROUTER_MODEL` trong `.env` để chỉ định. `AI_DISABLE=1` tắt AI, `AI_NO_POLLINATIONS=1` bỏ dịch vụ không key.
  - Lưu ý riêng tư: khi có người gõ `@ai`, câu hỏi và các tin gần đây của kênh đó được gửi tới nhà cung cấp AI.
- Trò chơi trong kênh: nút "+" trên ô soạn tin → "Chơi game". Máy chủ làm quản trò (chia vai bí mật, đếm giờ, chuyển pha), ván hiện ngay trong khung chat và cập nhật realtime; tới lượt mình mà đang ở kênh khác thì có thông báo.
  - **Trạm Vũ Trụ** (4–12 người, thời gian thực): đi lại trên bản đồ trạm (W A S D / phím mũi tên, điện thoại có cần gạt), thuyền viên làm nhiệm vụ bằng các trò nhỏ (nối dây, nhập mã, căn chỉnh, giữ nút, quét), kẻ phản bội giết người (có hồi chiêu) và phá hoại (cắt điện, xả oxy). Thấy xác thì báo hoặc bấm chuông ở sảnh để họp: thảo luận trong cuộc họp rồi bỏ phiếu đẩy một người ra ngoài. Người chết thành hồn ma, đi xuyên tường, vẫn làm nhiệm vụ. Máy chủ chỉ gửi cho mỗi người những gì người đó nhìn thấy, và kiểm tra tốc độ / tường nên không hack đi xuyên tường được. Bản đồ và nhân vật là thiết kế riêng (`game/station.go`, `desktop/src/components/Station.tsx`).
  - **Ma Sói** (4–16 người): Sói, Dân làng, Tiên tri, Bảo vệ, Phù thủy, Thợ săn; đêm hành động bí mật, ngày thảo luận trong kênh rồi bỏ phiếu treo cổ. Chọn thời gian ngày/đêm, tuỳ chọn giấu vai người chết.
  - **Ai là gián điệp** (4–12 người): gián điệp nhận từ khoá gần giống, lần lượt mô tả rồi bỏ phiếu loại. Thêm cặp từ trong `game/spy.go`.
  - **Bầu cua tôm cá** (2–10 người): cược bằng xu ảo trong ví. Vào bàn là mang toàn bộ xu thành chip, hết ván chip tự về ví. Mỗi lượt 20 giây đặt chip vào 6 con, cái lắc 3 xúc xắc; con ra k mặt thì ăn k lần. Cái xoay vòng theo thứ tự vào phòng (1/3/5 lượt mỗi người); cái không đủ trả là cháy túi, phần thiếu thành nợ ngân hàng và mất cái ngay.
  - **Cờ caro** 15×15 (luật chặn hai đầu, 3 phút/nước) và **Oẳn tù tì** (1/3/5 ván): 2 người, chơi được cả trong tin nhắn riêng.
  - Vai / từ khoá chỉ gửi cho đúng người đó, thẻ úp sẵn để người ngồi cạnh không nhìn trộm. Ván đang dở vẫn chạy tiếp khi khởi động lại server.
- Ví xu ảo: 🪙 ở góc dưới thanh bên. Mỗi người khởi đầu 1.000 xu, mỗi ngày bấm nhận 100 xu (giờ Việt Nam), còn dưới 100 xu thì vay ngân hàng 500 (thành nợ), có nút trả nợ. Bảng Đại gia / Con nợ theo từng kênh. Xu chỉ để chơi, không đổi ra gì.
- Tin nhắn riêng 1-1: mục "Tin nhắn riêng" ở thanh bên (nút +), hoặc nút nhắn ở danh sách thành viên. Tự nhắn cho mình để ghi chú.
- Ảnh đại diện: chọn ảnh từ máy (tự cắt vuông, thu nhỏ 256×256) hoặc dán link.
- Đã xem: avatar nhỏ góc phải dưới tin mới nhất mà mỗi người đã xem (như Messenger); bấm vào để xem tên.
  Ai trả lời tin của bạn thì có thông báo.
- Nhắc tên: gõ `@` để chọn người, `@all` để gọi cả kênh. Người được nhắc thấy tin tô sáng,
  badge `@` ở kênh và thông báo hệ thống (app desktop; bản web chỉ có thông báo khi chạy HTTPS).

## Lưu ý

- File gửi lên được tải qua đường dẫn ngẫu nhiên khó đoán, không cần đăng nhập (để thẻ `<img>` hiển thị
  được). Ai có đường dẫn thì mở được file, nên đừng chia sẻ link file ra ngoài.
- Server dùng HTTP thường: mật khẩu và tin nhắn không được mã hoá trên đường truyền. Ổn trong mạng
  nội bộ tin cậy; nếu cần bảo mật hơn, đặt server sau reverse proxy có HTTPS (ví dụ Caddy).
- IP máy server có thể đổi nếu router cấp tự động (DHCP); nên đặt IP tĩnh hoặc giữ IP trong router.
