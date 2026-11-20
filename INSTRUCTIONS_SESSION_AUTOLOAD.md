# Hướng dẫn Cấu hình Antigravity Tự Động Load Dự Án Khi Tạo Session Mới

Tài liệu này hướng dẫn chi tiết các cách để Google Antigravity (CLI, IDE, hoặc Antigravity 2.0 Desktop) **tự động nạp ngữ cảnh dự án (project context, kiến trúc, quy tắc, trạng thái hiện tại)** ngay khi bạn mở một session mới mà không cần phải gõ lại prompt thủ công.

---

## Mục Lục
1. [Cơ Chế Hoạt Động Của Antigravity](#1-cơ-chế-hoạt-động-của-antigravity)
2. [Cách 1: Sử Dụng `GEMINI.md` / `AGENTS.md` (Khuyến Nghị - Đơn Giản Nhất)](#2-cách-1-sử-dụng-geminimd--agentsmd-khuyến-nghị---đơn-giản-nhất)
3. [Cách 2: Sử Dụng Lifecycle Hook `SessionStart` (Nạp Ngữ Cảnh Động)](#3-cách-2-sử-dụng-lifecycle-hook-sessionstart-nạp-ngữ-cảnh-động)
4. [Cách 3: Sử Dụng Rules Với `trigger: always_on`](#4-cách-3-sử-dụng-rules-với-trigger-always_on)
5. [Mẫu Thiết Lập Sẵn Cho Dự Án Này (`tidebird/chat`)](#5-mẫu-thiết-lập-sẵn-cho-dự-án-này-tidebirdchat)
6. [Cách Kiểm Tra & Xác Minh](#6-cách-kiểm-tra--xác-minh)

---

## 1. Cơ Chế Hoạt Động Của Antigravity

Khi bạn mở một session làm việc mới trong một thư mục dự án, Antigravity thực hiện các bước sau:
1. **Quét thư mục gốc và thư mục làm việc hiện tại (CWD)** để tìm các file chỉ dẫn tự động: `GEMINI.md`, `AGENTS.md`, hoặc `.agents/rules/*.md`.
2. **Kích hoạt sự kiện vòng đời (Lifecycle Event)**: Nếu có khai báo `SessionStart` trong `.agents/hooks.json`, Antigravity sẽ thực thi script khởi tạo và nhận dữ liệu đưa vào context.
3. **Nạp vào Context Window**: Toàn bộ chỉ dẫn này sẽ được đưa vào phần system context hoặc lượt tương tác đầu tiên, giúp AI nắm rõ bạn đang làm dự án gì, cấu trúc code ra sao, và cần tuân thủ quy tắc nào.

---

## 2. Cách 1: Sử Dụng `GEMINI.md` / `AGENTS.md` (Khuyến Nghị - Đơn Giản Nhất)

Đây là chuẩn tự nhiên và ổn định nhất của Antigravity.

### Vị Trí File
Tạo file `GEMINI.md` (hoặc `AGENTS.md`) ngay tại **thư mục gốc** của dự án:
```
my-project/
├── GEMINI.md          <-- Đặt ở đây
├── .agents/
├── main.go
└── ...
```

### Nguyên Lý
Mỗi khi session mới được khởi tạo hoặc agent chạy tác vụ trong thư mục này, Antigravity **tự động đọc và đưa toàn bộ nội dung file này vào context** mà bạn không cần phải làm gì thêm.

### Cấu Trúc File Mẫu: `GEMINI.md`

```markdown
# Project Overview & Context

Dự án này là hệ thống Chat thời gian thực kết hợp WebRTC và Game (Cờ tướng, Cờ vua) viết bằng Go và Web/Desktop frontend.

## 1. Kiến Trúc & Ngăn Xếp Công Nghệ (Tech Stack)
- **Backend**: Go (1.21+), Gorilla WebSocket, Pion WebRTC.
- **Database**: SQLite / PostgreSQL / Redis.
- **Frontend / Client**: TypeScript, Tailwind CSS, Desktop app (Wails / Electron).
- **Core Modules**:
  - `controller/`: Điều hướng HTTP API & WebSocket routing.
  - `game/`: Logic các bàn cờ và luật chơi (Xiangqi, Chess).
  - `rtc/`: Peer-to-peer WebRTC signaling và room management.
  - `store/`: Lưu trữ dữ liệu và trạng thái phiên đăng nhập.

## 2. Quy Tắc Khởi Động Session Mới (Session Startup Protocol)
Khi một session mới bắt đầu hoặc trong lượt phản hồi đầu tiên:
1. Kiểm tra trạng thái git hiện tại (`git status`, nhánh đang làm việc).
2. Tóm tắt nhanh các tác vụ còn dang dở nếu user yêu cầu tiếp tục công việc.
3. Luôn đọc kỹ các file SPEC trước khi sửa code game (`specs/XIANGQI_GAME_SPEC.md`, `specs/CHESS_GAME_SPEC.md`).

## 3. Quy Ước Lập Trình (Coding Standards)
- Mọi logic game mới phải có unit test đi kèm (`*_test.go`).
- Chạy `make test` hoặc `go test ./...` trước khi hoàn tất thay đổi.
- Giữ phong cách trả lời ngắn gọn, sử dụng tiếng Việt khi trao đổi với user.
```

### Kỹ Thuật Nâng Cao: Nhúng File Phụ Với `@[label](path)`
Nếu file `GEMINI.md` quá dài, bạn có thể tách nhỏ và nhúng bằng cú pháp Antigravity:
```markdown
## Tài Liệu Thiết Kế Game
@[Xiangqi Spec](specs/XIANGQI_GAME_SPEC.md)
@[Chess Spec](specs/CHESS_GAME_SPEC.md)
```
> [!NOTE]
> - Giới hạn dung lượng: Mỗi file rule tối đa **24 KB** (sau khi mở rộng cú pháp `@[...]`).
> - Tổng ngân sách rules: Antigravity cấp **20.000 tokens** dành riêng cho rules.

---

## 3. Cách 2: Sử Dụng Lifecycle Hook `SessionStart` (Nạp Ngữ Cảnh Động)

Nếu bạn muốn mỗi khi tạo session mới, AI **tự động kiểm tra git, môi trường máy, docker containers hoặc các task Jira/Linear** rồi inject vào chat, hãy dùng **Lifecycle Hooks**.

### Bước 1: Tạo cấu hình `.agents/hooks.json`

Tạo file `.agents/hooks.json`:
```json
{
  "project-init-hook": {
    "enabled": true,
    "SessionStart": [
      {
        "type": "command",
        "command": "./.agents/hooks/on_session_start.sh",
        "timeout": 15
      }
    ]
  }
}
```

### Bước 2: Tạo script `.agents/hooks/on_session_start.sh`

Tạo file script thực thi `.agents/hooks/on_session_start.sh`:
```bash
#!/usr/bin/env bash
set -e

# Đọc context được Antigravity truyền qua stdin (JSON: conversationId, cwd, ...)
INPUT_JSON=$(cat)
CWD=$(echo "$INPUT_JSON" | grep -o '"cwd":"[^"]*' | cut -d'"' -f4)
[ -z "$CWD" ] && CWD=$(pwd)

cd "$CWD"

# Thu thập thông tin runtime động
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
GIT_STATUS_SHORT=$(git status -s 2>/dev/null | head -n 10 || echo "clean")
RECENT_COMMITS=$(git log -n 3 --oneline 2>/dev/null || echo "no commits")

# Chuẩn bị nội dung hiển thị cho User và Agent
SYSTEM_MSG="[AUTO-INIT] Project Tidebird Chat loaded. Current Branch: $CURRENT_BRANCH. Recent commits:\n$RECENT_COMMITS\nChanged files:\n$GIT_STATUS_SHORT"

# Xuất kết quả JSON ra stdout theo đúng Contract của Antigravity
cat <<EOF
{
  "injectSteps": [
    {
      "systemMessage": {
        "systemMessage": "$SYSTEM_MSG",
        "metadata": {
          "user_markdown": "### 🚀 Dự Án Đã Được Tự Động Nạp\n- **Nhánh Git**: \`$CURRENT_BRANCH\`\n- **Trạng thái**: Tự động đồng bộ ngữ cảnh sẵn sàng làm việc."
        }
      }
    }
  ]
}
EOF
```

Nhớ cấp quyền thực thi:
```bash
chmod +x .agents/hooks/on_session_start.sh
```

### Kết quả:
Ngay khi bạn tạo một session chat mới, hook này sẽ tự chạy ngầm, in ra banner chào mừng trên UI và nạp toàn bộ thông tin git branch, file đang sửa vào đầu não của Antigravity!

---

## 4. Cách 3: Sử Dụng Rules Với `trigger: always_on`

Bạn cũng có thể tạo file rule chuyên biệt trong thư mục `.agents/rules/`:

Tạo file `.agents/rules/project_context.md`:
```markdown
---
trigger: always_on
description: Tự động nạp kiến trúc và quy tắc dự án cho mọi session
---

# Project Context: Tidebird Chat
... (nội dung quy tắc tương tự GEMINI.md)
```

Khi đặt `trigger: always_on`, Antigravity đảm bảo rule này luôn hiện diện trong bộ nhớ ngữ cảnh của mọi turn trò chuyện.

---

## 5. Mẫu Thiết Lập Sẵn Cho Dự Án Này (`tidebird/chat`)

Để áp dụng ngay cho dự án hiện tại:

1. **Tạo file `GEMINI.md` tại thư mục gốc**:
   ```bash
   touch GEMINI.md
   ```
2. **Khai báo các file cốt lõi** như `main.go`, `Makefile`, các thư mục `game/`, `controller/`, `rtc/`.
3. Khi khởi động session mới trên Antigravity CLI (`agy`), Antigravity IDE hoặc Antigravity 2.0 Desktop, trợ lý AI sẽ nhận diện ngay lập tức mà bạn không cần phải giải thích lại "đây là dự án gì".

---

## 6. Cách Kiểm Tra & Xác Minh

Sau khi thiết lập một trong các cách trên:
1. Mở một session mới (bấm **New Conversation** trên Desktop/IDE, hoặc gõ `agy` trong terminal).
2. Đặt câu hỏi thử nghiệm:
   > *"Dự án này là gì và cấu trúc chính gồm những module nào?"*
3. Nếu Antigravity trả lời chính xác ngay từ đầu mà không cần quét lại toàn bộ file từ đầu, hệ thống đã nạp thành công!
