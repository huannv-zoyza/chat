# Farm: bắt sâu tất cả + tự động bắt sâu

## Mục đích
Đây là phần tiếp theo của tính năng tưới tất cả (PR #14). Người dùng đã duyệt đề xuất "tự bắt sâu theo
cùng cách". Còn sâu lúc thu hoạch thì vụ mất 30% sản lượng. Hiện chủ vườn phải bấm từng ô có sâu.

## Mục tiêu / không làm
- Có: nút **🐛 Bắt sâu tất cả** trên vườn của mình. "⚡ Làm hết" và "🤖 Tự động" tự bắt sâu.
- Không: bắt sâu tất cả trên vườn hàng xóm. Bắt giúp có thưởng, nên vẫn phải bấm tay.
- Không đổi luật: chỉ bắt được sâu đã lộ ra (`bugVisible`). Chủ tự bắt vẫn tính nhiệm vụ "bug" như
  khi bấm tay, và không có thưởng xu/XP.

## Thiết kế
- Tách phần lõi của `FarmBug` thành op-helper `catchBug(actorID, p, now)`, gồm kiểm tra, cập nhật và
  bump. Phần thưởng cho người giúp vẫn chỉ nằm trong `FarmBug`.
- `POST /api/farm/bug {all: true}`: chỉ cho vườn của mình. Có `user_id` khác mình thì trả 400
  `ErrFarmBugAllOwn` ("Chỉ bắt sâu tất cả được ở vườn của mình.").
  - Bắt sâu mọi ô có `idx < plot_count` đang có sâu lộ ra.
  - Kết quả: `Gained.Bugs int` (`json:"bugs,omitempty"`).
  - Không có ô nào thì trả `ErrFarmNothingBug` ("Không có sâu nào để bắt.").
- `/auto`: bước **"Bắt sâu"** chạy **đầu tiên**, trước khi thu hoạch, để ô chín có sâu được bắt trước
  rồi mới thu đủ 100%. `ErrFarmNothingBug` là im lặng. Thêm `AutoReport.Bugs` (`json:"bugs"`) và tính
  vào điều kiện "có việc".
  - Thứ tự mới: bắt sâu → thu hoạch → gieo → tưới → chăm chuồng → vườn cây → thu lò → làm đầy lò.
- Client:
  - Nút "🐛 Bắt sâu tất cả" chỉ ở vườn của mình, hiện khi có ô `bug` trong số ô đã mở. Toast: "Đã bắt N con sâu 🐛".
  - `autoPending` thêm điều kiện có ô `bug`. `autoSummary` thêm "bắt N sâu" ở đầu.
  - Kiểu dữ liệu: `bugs?`.
- `FARM_SPEC.md`: cập nhật §14 và danh sách API.
- Test:
  - store: bắt tất cả chỉ ô có sâu lộ ra, gọi lần hai thì báo lỗi, tăng nhiệm vụ "bug";
  - `/auto`: ô chín có sâu thu đủ sản lượng, không bị −30%;
  - controller: chặn khi `user_id` là hàng xóm; route cũ giữ nguyên.
