# Farm Phần 5: Vườn cây ăn quả, Máy ép nước, Nút tự động

## Mục đích
Người dùng muốn một bản cập nhật lớn: trồng cây ăn quả, ép nước trái cây, các tính năng đi kèm,
và các nút tự động. Bối cảnh: cấp đã không giới hạn (PR #12), nhưng nội dung mới chỉ mở tới cấp 9.
Phần này thêm nội dung cho cấp 5–18. Đồng thời, khi người chơi có nhiều ô, chuồng, cây và lò, bấm
từng thứ rất mệt, nên cần nút "làm hết" và chế độ tự động.

## Mục tiêu / không làm / anti-goal
- Có: cây ăn quả trồng **một lần, hái nhiều lần**. Quả vào kho, bán được, có trong đơn hàng, và là
  nguyên liệu cho máy ép.
- Có: công trình **Máy ép nước** có lò riêng và công thức nước ép riêng.
- Có: nút **Gieo tất cả**, **Hái tất cả**, **Nấu/ép đầy lò**, **⚡ Làm hết** (một chạm làm mọi
  việc) và công tắc **🤖 Tự động** (lặp lại "Làm hết" khi đang mở nông trại).
- Không: tự động chạy khi app đã đóng (không có job nền trên server). Không làm trộm quả, nhiệm vụ
  hằng ngày mới hay hàng xóm tưới cây ăn quả (để phần sau). Không đổi giá và thời gian của nội dung cũ.
- Anti-goal: tự động tiêu xu ngoài ý muốn. Chỉ mua thức ăn khi bật "tự mua thức ăn". Gieo hạt
  chỉ tiêu tiền hạt của loại cây người chơi đã chọn. Không có vòng lặp mua ở chợ → chế biến → bán có lãi.
  Không spam server khi tự động gặp lỗi.

## Các cách đã cân nhắc
1. Cây ăn quả là một loại "cây trồng" trên ô ruộng: phá vỡ logic vụ (một lần thu rồi xoá ô), bị loại.
2. **Vườn cây riêng (chọn):** bảng `farm_trees` riêng, có các chỗ trồng cố định. Vườn thay khu vườn
   trang trí đang có trên bản đồ. Logic giống vật nuôi: có chu kỳ, cho sản phẩm lặp lại.
3. Tự động chạy trên server theo lịch (cron): người chơi không thấy, tốn tài nguyên, khó kiểm soát
   tiền, bị loại. **Chọn:** server có endpoint `/auto` làm mọi việc trong một lần gọi, client lặp
   lại khi bật công tắc.

## Thiết kế

### A. Vườn cây ăn quả
- Có 12 chỗ trồng trên lưới 4×3, thay khu vườn trang trí ở góc trên phải. Chỗ thứ i (0..11):
  `x = 16 + 1.6·(i%4)`, `y = 3 + 1.6·⌊i/4⌋`.
- Ô bị chặn trên server mở rộng thành `{15.5, 2.5, 6.5, 4.8}`. Công trình người chơi đã đặt trước
  đó vẫn giữ nguyên chỗ (chỉ kiểm tra khi đặt hoặc dời).
- **Số chỗ mở theo cấp:** cấp 5 → 3, cấp 8 → 6, cấp 12 → 9, cấp 16 → 12. Dưới cấp 5 thì vườn khoá.
- **Danh mục cây** (`FruitTrees`). Thời gian ra quả chu kỳ đầu = thời gian lớn. Mỗi lần hái được
  `3 + harvest_bonus` quả.

| id cây | Quả (item id) | Tên quả | Cấp | Giá cây giống | Lớn | Chu kỳ | Giá quả | XP/lần hái |
|---|---|---|---|---|---|---|---|---|
| cam | cam 🍊 | Cam | 5 | 300 | 2h | 1h | 18 | 4 |
| tao | tao 🍎 | Táo | 7 | 500 | 3h | 1h30 | 26 | 6 |
| chuoi | chuoi 🍌 | Chuối | 9 | 700 | 4h | 2h | 34 | 8 |
| xoai | xoai 🥭 | Xoài | 11 | 1000 | 6h | 3h | 48 | 11 |
| dua | dua 🥥 | Dừa | 14 | 1500 | 8h | 4h | 65 | 15 |
| nho | nho 🍇 | Nho | 18 | 2200 | 10h | 5h | 90 | 20 |

- Quả là `Item` loại `"fruit"`, có `BuyPrice = 0` (không bán ở chợ). Quả được cộng `sell_pct` giống
  crop và product, vì là hàng tự sản xuất. `Item.Level` bằng cấp của cây.
- **Trạng thái cây:** `growing` (đang lớn, chưa ra quả lần nào), `fruiting` (đang ra quả),
  `ready` (có quả để hái).
  - Trồng: `ready_at = now + MatureSec × (100 − grow_pct)/100`, với `grow_pct` tính theo cấp lúc trồng.
  - Hái: quả vào kho, giới hạn theo chỗ trống. Lần hái này phải vừa đủ chỗ cho cả lượt, nếu không
    thì báo kho đầy. Sau khi hái: `ready_at = now + CycleSec × (100 − grow_pct)/100`, cộng XP.
- **Hành động:**
  - trồng cây vào chỗ trống: đủ cấp, đủ xu, chỗ đã mở;
  - hái một cây hoặc hái tất cả;
  - chặt cây (xoá, không hoàn tiền, để trồng loại khác).
- **Đơn hàng:** quả xuất hiện trong pool đơn hàng chỉ khi người chơi đang có ít nhất một cây loại đó.

### B. Máy ép nước
- Công trình `juicer` ("Máy ép nước"): giá 800, cấp 6, có 2 lò, nâng cấp lên 4 lò với giá 1600.
  Kích thước 2×2, đặt tự do như xưởng. Chỗ mặc định phải hợp lệ và không đè lên vườn cây.
- Công thức có thêm trường `Building` (`kitchen` | `juicer`). Mỗi lò thuộc về một công trình.
  Nấu, thu, view và đơn hàng đều làm việc theo từng công trình.
- Migration phải giữ nguyên các job xưởng đang có.
- **Công thức máy ép** (loại `goods`, không cộng `sell_pct`):

| id | Tên | Nguyên liệu | Thời gian | Giá | XP | Cấp |
|---|---|---|---|---|---|---|
| nuoc_cam | Nước cam | cam×3 | 20m | 85 | 8 | 6 |
| nuoc_tao | Nước táo | tao×3 | 30m | 120 | 10 | 7 |
| sinh_to_chuoi | Sinh tố chuối | chuoi×2, sua×1 | 40m | 215 | 14 | 9 |
| nuoc_xoai | Nước xoài | xoai×3 | 60m | 215 | 18 | 11 |
| nuoc_thap_cam | Nước ép thập cẩm | cam×1, tao×1, xoai×1, ca_rot×1 | 2h | 165 | 14 | 12 |
| nuoc_dua | Nước dừa | dua×2 | 45m | 195 | 16 | 14 |
| nuoc_nho | Nước nho | nho×4 | 90m | 540 | 32 | 18 |

- Mỗi món có emoji riêng, không trùng với item đã có.
- Đơn hàng có nước ép chỉ khi đã xây máy ép, giống món xưởng chỉ có khi đã xây xưởng. Mỗi đơn vẫn
  chỉ có tối đa một món `goods`.

### C. Tự động
**Server:**
- `POST /api/farm/plant {all: true, crop}`: dọn mọi ô héo, rồi gieo `crop` vào mọi ô trống, cho
  tới khi hết xu. Cần đủ cấp. Không gieo được ô nào thì báo lỗi.
- `POST /api/farm/orchard/harvest {all: true}`.
- `POST /api/farm/craft {recipe, fill: true}`: dùng công thức đó lấp mọi lò trống của công trình
  tương ứng, đến khi hết nguyên liệu.
- `POST /api/farm/auto` với body `{crop?, kitchen_recipe?, juicer_recipe?, buy_feed: bool}`. Chạy
  lần lượt các bước sau, mỗi bước có giao dịch riêng hoặc tất cả trong một giao dịch (builder chọn).
  Bước nào "không có gì để làm" hoặc gặp lỗi nghiệp vụ thì bỏ qua và ghi lại lý do. Lỗi DB thì dừng.
  1. thu hoạch mọi ô chín;
  2. gieo `crop`, nếu có;
  3. chăm chuồng gà rồi đồng cỏ (tự mua thức ăn khi `buy_feed`, không thì chỉ dùng thức ăn trong kho);
  4. hái cả vườn;
  5. thu mọi lò đã xong của xưởng và máy ép;
  6. lấp lò xưởng bằng `kitchen_recipe` và lò máy ép bằng `juicer_recipe`, nếu có.
- Kết quả: `Gained` gộp tất cả, cộng thêm
  `auto: {harvested, planted, fed, fruits, crafted, collected, spent, skipped: [lý do]}`.
- Không làm được gì thì trả `ErrFarmNothingAuto` ("Chưa có việc gì để làm.").
- Tất cả chỉ áp dụng cho nông trại của chính mình.

**Client:**
- Banner có nút **⚡ Làm hết**, chỉ hiện khi có việc để làm. `autoPending(view, now)` mô phỏng các
  điều kiện ở client.
- Công tắc **🤖 Tự động** kèm popover cài đặt: cây để gieo, món của xưởng, món của máy ép, tự mua
  thức ăn. Cài đặt lưu localStorage `farm-auto:<userId>`.
- Khi bật và đang mở nông trại của mình: cứ 30 giây kiểm tra `autoPending`; có việc thì gọi `/auto`.
  Gặp lỗi thì tạm dừng 5 phút và hiện toast một lần. Không chồng lệnh khi đang `busy`.
- Các nút nhanh tại chỗ:
  - "🌱 Gieo tất cả" khi có ô trống (dùng cây đã chọn, chưa chọn thì mở cửa hàng hạt);
  - biển "🧺 Hái N quả" trên vườn;
  - "Làm đầy lò" ở từng công thức trong bảng xưởng và bảng máy ép.

### D. UI vườn và máy ép
- **FarmWorld** vẽ 12 chỗ trồng cây:
  - chỗ khoá: biển "Mở ở cấp N";
  - chỗ trống: cọc đất, bấm để mở cửa hàng cây;
  - cây theo trạng thái: cây non, cây có quả, quả lơ lửng khi `ready`.
- Bấm cây `ready` để hái. Bấm cây chưa có quả để xem còn bao lâu. Có tuỳ chọn chặt cây, phải xác nhận.
- **Máy ép** vẽ như xưởng nhưng hình khác, đặt và dời chỗ được.
- **Tab mới "Vườn & Ép"**: cửa hàng cây giống và bảng máy ép, dùng lại bảng xưởng (tổng quát theo công trình).
- Hiển thị thời gian đã trừ đặc quyền và giá bán đã cộng đặc quyền như phần trước.

### E. Spec và test
- `specs/FARM_SPEC.md`: thêm "Phần 5" (§12 Vườn cây ăn quả, §13 Máy ép nước, §14 Tự động).
- Test store:
  - vườn: mở chỗ theo cấp, trồng, hái, kho đầy, chặt cây, đặc quyền thời gian và số quả, quả trong đơn hàng;
  - máy ép: migration giữ job cũ, lò riêng theo công trình, không có lãi khi mua ở chợ rồi ép;
  - gieo tất cả: thiếu xu thì gieo một phần, có dọn ô héo;
  - lấp lò;
  - `/auto`: mọi bước, `buy_feed` tắt không tiêu xu cho thức ăn, không có việc thì báo lỗi.
- Test controller cho mọi route mới. Test migration lên/xuống.
