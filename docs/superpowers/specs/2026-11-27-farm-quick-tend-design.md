# Farm: chăm nhanh cả chuồng (thu sản phẩm + cho ăn + tự mua thức ăn)

## Mục đích
Người chơi đang phải bấm từng con vật để thu trứng/sữa/lông và cho ăn. Muốn bấm một lần
cho cả chuồng. Thiếu thức ăn thì tự mua ở Chợ thay vì bị đẩy sang tab Kho.

## Mục tiêu / không làm
- Có: mỗi chuồng (chuồng gà `coop`, đồng cỏ `pasture`) có **một nút** "chăm cả chuồng".
- Có: thiếu thức ăn thì tự mua phần còn thiếu, theo giá `BuyPrice` của Chợ.
- Không: thay đổi giá, thời gian sản xuất hay XP. Không làm "auto" chạy nền. Bấm từng con vẫn giữ nguyên như cũ.
- Không được (anti-goal): trừ xu mà con vật không được ăn, hoặc tiêu xu mà người chơi không thấy trước.

## Các cách đã cân nhắc
1. **Một nút chung trên banner**, dùng lại API `{all:true}` có sẵn: đơn giản nhưng không gắn với chuồng.
2. **Mỗi chuồng một nút** (người dùng chọn): nút nằm ngay trên chuồng trong FarmWorld, server lọc theo chuồng.
3. Tự mua ở client (gọi buy rồi feed): bị loại vì không nguyên tử, dễ bị trừ xu mà không cho ăn được.

## Thiết kế
**Server:** thêm endpoint `POST /api/farm/buildings/{id}/tend` (`id` = `coop` | `pasture`).
Mọi bước chạy trong **một transaction** `farmTx`:
1. Thu sản phẩm của các con trong chuồng đã `ready` (giống `FarmCollect` all nhưng lọc theo
   `Kind.Building`), giới hạn theo chỗ trống trong kho. Con nào đã thu thì chuyển sang đói.
2. Cho các con đang đói trong chuồng ăn, kể cả những con vừa thu ở bước 1. Thức ăn lấy từ kho trước.
3. Nếu kho không đủ: mua phần thiếu theo `BuyPrice` và **đưa thẳng cho con vật, không qua kho**,
   để kho đầy cũng không chặn việc mua. Chỉ mua nếu đủ cấp (`it.Level`) và đủ xu. Hết xu
   thì cho ăn được tới đâu hay tới đó, con nào chưa được ăn thì báo lại.
4. Tách phần logic thu và cho ăn dùng chung với `FarmCollect`/`FarmFeed` để không chép code.
5. Kết quả trả về: số món thu được (`Gained`), số con đã ăn, số xu đã tiêu kèm món đã mua, và cảnh báo
   nếu còn con đói. Có thể mở rộng `FarmResult`. Nếu không làm được gì cả thì trả lỗi rõ ràng,
   ví dụ "Chuồng không có gì để thu hay cho ăn" hoặc "Không đủ xu mua thức ăn".
6. Ghi vào ví như khi mua ở chợ, ví dụ "Mua 3 Cà rốt cho vật nuôi".

**Client:**
- `api.farmTend(building)`.
- Trong `FarmWorld`, khi chuồng đã xây và đang xem nông trại của mình, nút nổi trên chuồng (giống
  `BuildSign`) chỉ hiện khi có việc. Nhãn tính sẵn ở client từ `view`, ví dụ
  `🥚 Thu 3 · 🍽️ Cho ăn 5 · mua 2🌽 30🪙`. Như vậy người chơi thấy trước số xu sẽ tiêu.
- Bấm thì chạy `act(...)`, toast tóm tắt kết quả và có âm thanh thu hoạch như hiện có.

**Test:** test store cho từng trường hợp: chỉ thu, chỉ cho ăn, thu xong cho ăn luôn, tự mua khi thiếu,
thiếu xu (cho ăn một phần, không tiêu quá số xu), chưa đủ cấp mua, kho đầy mà vẫn mua được,
chuồng kia không bị ảnh hưởng, và nông trại người khác thì không được làm. Thêm test controller cho route mới.

## Để builder tự quyết
Vị trí và kiểu dáng nút trên chuồng, câu chữ trong toast, và tên field trả về.
