# Farm: cấp không giới hạn + đặc quyền theo cấp

## Mục đích
Hiện nông trại dừng ở cấp 10 (`LevelXP` 10 mốc, UI hiện "Tối đa"), nên người chơi lâu năm không còn
động lực cày. Người dùng muốn: cấp không giới hạn, cấp càng cao càng cần nhiều XP, và cấp cao
có nhiều đặc quyền hơn: trồng cây, thu hoạch, bán hàng… nhanh hơn và được nhiều hơn.

## Mục tiêu / không làm / anti-goal
- Có: lên cấp mãi. Khoảng XP giữa hai cấp tăng dần. Đặc quyền mạnh dần theo cấp, đều có trần.
- Có: người chơi thấy đặc quyền hiện tại và đặc quyền ở cấp kế tiếp.
- Không: đổi cấp mở khoá cây, con vật, công trình, hay thưởng xu khi lên cấp (`20*lv`).
  Không đổi `LevelXP` cấp 1..10, để không ai bị tụt cấp.
- Anti-goal: đặc quyền tạo vòng lặp kiếm lời vô hạn, ví dụ mua ở chợ rồi bán lại có lãi. Cũng không
  để thời gian về 0 hay số lượng tăng vô tận.

## Các cách đã cân nhắc
1. Bảng `LevelXP` dài, ví dụ 100 cấp: vẫn có trần, bị loại.
2. **Công thức (chọn):** giữ 10 mốc cũ. Từ cấp 11 trở đi, khoảng XP = khoảng trước × 1.15, làm tròn
   tới 10. Đặc quyền là hàm của cấp, có trần.
3. Đặc quyền dạng cây kỹ năng cho người chơi tự chọn: quá lớn so với yêu cầu, bị loại.

## Thiết kế (quyết định của tôi)
**Đường cong XP:** `gap(10) = 1900 − 1400 = 500`. Với n ≥ 11: `gap(n) = round10(gap(n−1) × 1.15)`,
và `XP(n) = XP(n−1) + gap(n)`. Hàm `levelXP(n)` (tổng XP để đạt cấp n) và `levelFor(xp)` không có
trần, nhưng phải an toàn với int64 (dừng khi vượt `math.MaxInt64/2`).

**Đặc quyền** (`farmPerks(level)`):
| Đặc quyền | Công thức | Trần |
|---|---|---|
| Cây lớn nhanh hơn `grow_pct` | 2% × (lv − 1) | 50% (cấp 26) |
| Vật nuôi làm nhanh hơn `animal_pct` | 1% × (lv − 1) | 30% (cấp 31) |
| Thu hoạch thêm `harvest_bonus` | +1 đơn vị/vụ mỗi 5 cấp (lv/5) | +5 (cấp 25) |
| Bán giá cao hơn `sell_pct` | 2% × (lv − 1) | 40% (cấp 21) |

- Giá mua ở chợ là 150% giá bán cơ bản, nên bán thêm tối đa 40% vẫn thấp hơn giá mua: không có lãi khi mua đi bán lại.
- `sell_pct` chỉ áp dụng cho nông sản (`crop`) và sản phẩm chăn nuôi (`product`), **không bao giờ** cho món chế biến (`goods`), vốn bán đúng giá gốc. Lý do: chặn vòng lặp mua nguyên liệu ở chợ → chế biến → bán (salad, bắp rang, cơm nắm có lợi nhuận gốc bằng 0, cộng 40% là có lãi).
- Đặc quyền cố ý nhân lợi nhuận sản xuất của chính mình (hạt giống → thu hoạch → bán, thức ăn → sản phẩm). Đây là phần thưởng của cấp cao, bị giới hạn bởi số ô đất, sức chứa kho và thời gian lớn. Đảm bảo không có lãi chỉ áp dụng cho việc mua đi bán lại ở chợ.
- Tưới nước vẫn rút ngắn theo `GrowSec/10` gốc (không theo `grow_pct`).
- Áp dụng:
  - lúc **gieo**: `ready = now + GrowSec × (100 − grow_pct) / 100`;
  - lúc **cho vật nuôi ăn**, mọi đường: cho từng con, cho tất cả, chăm cả chuồng;
  - lúc **chủ vườn thu hoạch**: cộng `harvest_bonus` vào số đơn vị (người trộm không được hưởng);
  - lúc **bán ở kho** (`FarmSell`): `price × (100 + sell_pct) / 100`, làm tròn xuống, chỉ cho `crop`/`product` (`goods` bán giá gốc). Giao đơn hàng không đổi.
- Đặc quyền tính theo cấp **tại thời điểm hành động**: cây đã gieo không đổi giờ chín.

**API:** `FarmView` thêm `perks: {grow_pct, animal_pct, harvest_bonus, sell_pct}` và `next_perks`
(đặc quyền ở cấp kế tiếp, cùng dạng). `next_level_xp` luôn có giá trị, không còn null.

**UI desktop:**
- Thanh cấp bỏ chữ "Tối đa".
- Bấm huy hiệu cấp mở bảng đặc quyền: hiện mỗi dòng giá trị bây giờ → giá trị ở cấp sau (ví dụ `🌱 Cây lớn nhanh hơn 18% → 20%`), dòng nào đã chạm trần thì ghi "tối đa".
- Cửa hàng hạt hiện thời gian đã trừ `grow_pct`. Kho hiện giá bán đã cộng `sell_pct`.
- Toast lên cấp nhắc đặc quyền mới.

**Test:** đường cong (giữ đúng 10 mốc cũ, `gap(11) = 580`, tăng đơn điệu, cấp 200 không tràn số), các
trần đặc quyền, gieo nhanh hơn, vật nuôi nhanh hơn, thu hoạch thêm, người trộm không hưởng bonus, bán
giá cao hơn, và kiểm tra không có lãi khi mua đi bán lại ở mọi cấp.
