#!/usr/bin/env bash
# ==============================================================================
# Script: create-spec.sh
# Mục đích: Tạo nhanh file đặc tả kỹ thuật chuẩn (SPEC.md) trong thư mục specs/
# Sử dụng: ./scripts/create-spec.sh <feature_slug> ["Tiêu đề tính năng"]
# Ví dụ:   ./scripts/create-spec.sh caro-tournament "Hệ Thống Giải Đấu Cờ Caro"
# ==============================================================================

set -euo pipefail

# Xác định thư mục gốc của repository
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SPECS_DIR="$REPO_ROOT/specs"

mkdir -p "$SPECS_DIR"

if [ $# -lt 1 ]; then
  echo "❌ Lỗi: Thiếu tham số tên tính năng."
  echo "👉 Cú pháp: $0 <feature_name> [\"Tiêu đề tính năng\"]"
  echo "   Ví dụ:   $0 caro-tournament \"Hệ Thống Giải Đấu Cờ Caro\""
  exit 1
fi

RAW_NAME="$1"
TITLE="${2:-$RAW_NAME}"

# Chuẩn hoá tên file:
# 1. Bỏ đuôi .md hoặc _SPEC.md nếu người dùng lỡ gõ
CLEAN_NAME=$(echo "$RAW_NAME" | sed -E 's/\.(md|spec\.md)//I' | sed -E 's/_SPEC$//I')
# 2. Thay thế khoảng trắng và dấu gạch nối '-' thành dấu gạch dưới '_'
NORMALIZED=$(echo "$CLEAN_NAME" | tr '[:lower:]' '[:upper:]' | tr '-' '_' | tr ' ' '_')
SPEC_FILENAME="${NORMALIZED}_SPEC.md"
TARGET_FILE="$SPECS_DIR/$SPEC_FILENAME"

if [ -f "$TARGET_FILE" ]; then
  echo "⚠️ Cảnh báo: File $TARGET_FILE đã tồn tại!"
  echo "Vui lòng chọn tên khác hoặc chỉnh sửa trực tiếp file hiện có."
  exit 1
fi

TODAY=$(date +"%Y-%m-%d")

cat <<EOF > "$TARGET_FILE"
# Đặc tả kỹ thuật (SPEC): $TITLE

Tài liệu này đặc tả chi tiết kiến trúc, mô hình dữ liệu, luồng tương tác thời gian thực, quy tắc nghiệp vụ và kế hoạch triển khai cho tính năng **$TITLE** trong hệ thống chat \`tidebird/chat\`.

---

## 1. Mục tiêu & Phạm vi (Goals & Scope)

### 1.1. Bối cảnh & Vấn đề giải quyết
- Mô tả lý do cần phát triển tính năng này.
- Điểm bất tiện hoặc thiếu sót hiện tại mà người dùng đang gặp phải.

### 1.2. Mục tiêu chính (Core Goals)
- [ ] Mục tiêu 1: Cung cấp trải nghiệm...
- [ ] Mục tiêu 2: Đảm bảo hiệu năng và không gây ảnh hưởng đến...
- [ ] Mục tiêu 3: Tích hợp liền mạch với hệ thống WebSocket hiện có.

### 1.3. Ngoài phạm vi (Out of Scope v1 - Dành cho v2)
- Các tính năng nâng cao chưa làm trong phiên bản này.

---

## 2. Kiến trúc & Luồng hoạt động (Architecture & Flow)

### 2.1. Biểu đồ Sequence Diagram (Mermaid)

\`\`\`mermaid
sequenceDiagram
    autonumber
    actor User as Người dùng
    participant UI as Giao diện Frontend (React)
    participant API as Server API (Controller Go)
    participant WS as WebSocket Hub
    participant DB as SQLite (Store)

    User->>UI: Thao tác kích hoạt tính năng
    UI->>API: Gọi API REST hoặc gửi WebSocket Action
    API->>DB: Truy vấn / Cập nhật dữ liệu
    API->>WS: Broadcast sự kiện cập nhật
    WS-->>UI: Nhận sự kiện thời gian thực
    UI-->>User: Cập nhật giao diện mượt mà
\`\`\`

### 2.2. Vòng đời Trạng thái (State Diagram)

\`\`\`mermaid
stateDiagram-v2
    [*] --> KhoiTao: Bắt đầu
    KhoiTao --> DangHoatDong: Kích hoạt
    DangHoatDong --> HoanThanh: Kết thúc
    HoanThanh --> [*]
\`\`\`

---

## 3. Mô hình dữ liệu & API Contracts

### 3.1. Backend Data Models (Go)
\`\`\`go
// Cấu trúc dữ liệu trong package model hoặc controller
type ${NORMALIZED}Data struct {
    ID        int64     \`json:"id"\`
    ChannelID int64     \`json:"channel_id"\`
    CreatedAt time.Time \`json:"created_at"\`
}
\`\`\`

### 3.2. REST Endpoints & WebSocket Events
- **GET /api/channels/{id}/...**: Lấy danh sách...
- **POST /api/channels/{id}/...**: Khởi tạo...
- **WebSocket Event**: \`type: "${RAW_NAME}.updated"\`

---

## 4. Quy tắc nghiệp vụ & Trường hợp biên (Domain Rules & Edge Cases)

| Mã | Tình huống (Edge Case) | Cách xử lý chuẩn |
|---|---|---|
| **EC-01** | Người dùng mất kết nối mạng khi đang thao tác | Tự động thử lại khi WebSocket reconnect. |
| **EC-02** | Xung đột đồng thời nhiều người thao tác | Kiểm tra version / lock phía server Go. |
| **EC-03** | Dữ liệu rỗng hoặc không tồn tại | Trả về thông báo lỗi thân thiện, không làm sập UI. |

---

## 5. Thiết kế Giao diện Người dùng (UI / UX)

### 5.1. Bố cục Component
- Vị trí hiển thị trên giao diện: \`desktop/src/components/...\`
- Tương thích Responsive (Desktop & Mobile Compact Mode).

### 5.2. Hiệu ứng & Phản hồi Âm thanh
- Sử dụng hiệu ứng chuyển động CSS mượt mà.
- Phản hồi âm thanh qua Web Audio API (\`desktop/src/lib/sound.ts\`) nếu cần.

---

## 6. Tích hợp Thống kê / Bảng xếp hạng (Leaderboard & Stats)
*(Nếu tính năng có liên quan đến game hoặc thành tích cá nhân)*

---

## 7. Kế hoạch triển khai từng bước (Implementation Plan)

### Giai đoạn 1: Backend Core & Data Store
- [ ] Thiết kế struct và migration DB SQLite (nếu có).
- [ ] Viết API handler trong controller Go.
- [ ] Viết unit test backend đạt coverage cao.

### Giai đoạn 2: Frontend State & UI Components
- [ ] Khai báo kiểu TypeScript trong \`desktop/src/lib/\`.
- [ ] Xây dựng UI component trong \`desktop/src/components/\`.
- [ ] Kết nối WebSocket listener và ChatProvider.

### Giai đoạn 3: Kiểm thử Toàn diện & Hoàn thiện
- [ ] Kiểm tra typecheck \`pnpm build\`.
- [ ] Kiểm tra backend test \`go test ./...\`.
- [ ] Cập nhật tài liệu và kiểm thử thực tế.
EOF

chmod +x "$TARGET_FILE" || true

echo "✅ Đã tạo thành công file SPEC mới:"
echo "👉 File: specs/$SPEC_FILENAME"
echo "👉 Đường dẫn tuyệt đối: $TARGET_FILE"
echo ""
echo "💡 Bước tiếp theo: Mở file trên để điền chi tiết nội dung thiết kế kỹ thuật."
