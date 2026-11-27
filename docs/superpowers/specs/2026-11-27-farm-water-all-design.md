# Farm: tưới tất cả + tự động tưới

## Mục đích
Người dùng: "thêm features tự động tưới nước tất cả, không cần phải click thủ công". Hiện chủ vườn
phải bấm từng ô đang lớn để tưới (mỗi vụ chủ được tưới 1 lần, rút `GrowSec/10`). Có nhiều ô thì rất
mệt. Phần 5 (PR #13) đã có "⚡ Làm hết" và "🤖 Tự động", nhưng hai nút này chưa tưới.

## Mục tiêu / không làm / anti-goal
- Có: nút **💧 Tưới tất cả** trên nông trại của mình. Nút tưới mọi ô đang lớn mà chủ còn tưới được.
- Có: "⚡ Làm hết" và chế độ "🤖 Tự động" tự tưới luôn, không cần cài đặt gì thêm. Tưới không tốn xu.
- Không: tưới tất cả trên vườn hàng xóm. Tưới giúp có thưởng xu, XP và điểm thân mật, nên vẫn phải bấm
  tay để giữ tính xã giao. Không tự bắt sâu (có thể làm phần sau).
- Không đổi luật tưới: tối đa 3 lần mỗi vụ, mỗi người 1 lần, `ready = max(now, ready − GrowSec/10)`.
  Chủ tự tưới vẫn không có thưởng và không tính nhiệm vụ "water".

## Các cách đã cân nhắc
1. Client gọi `/api/farm/water` cho từng ô: tốn N request, dễ đụng `busy`, bị loại.
2. **Chọn:** server làm `FarmWaterAll` trong một giao dịch, và `/auto` có thêm một bước tưới.
   Dùng chung helper với `FarmWater` để giữ đúng luật.
3. Cài đặt bật/tắt tưới trong `/auto`: tưới miễn phí, không có mặt xấu, nên bỏ.

## Thiết kế
**Server**
- Tách phần lõi của `FarmWater` thành op-helper `waterPlot(actorID, p, now)`. Helper này kiểm tra
  growing, `waters < 3` và `addAct`, rồi cập nhật `ready_at` và `waters`.
- `POST /api/farm/water {all: true}`: chỉ áp dụng cho nông trại của chính mình. `user_id` khác mình thì
  báo lỗi. Route `{user_id, plot}` cũ giữ nguyên.
  - Tưới mọi ô có `idx < plot_count` đang lớn, `waters < 3` và chủ chưa tưới.
  - Kết quả: `Gained.Watered int` (`json:"watered,omitempty"`).
  - Không có ô nào → `ErrFarmNothingWater` ("Không có cây nào cần tưới.").
- `/auto`: thêm bước **"Tưới"** ngay sau bước gieo, để cây vừa gieo cũng được tưới. `ErrFarmNothingWater`
  là im lặng. Thêm `AutoReport.Watered` (`json:"watered"`) và đưa vào điều kiện "không làm được gì".

**Client**
- Banner có nút **💧 Tưới tất cả** khi ở vườn của mình và có ít nhất một ô `growing` với `can_water`.
  Toast: "Đã tưới N ô 💧".
- `autoPending`: thêm điều kiện "có ô growing với `can_water`". `autoSummary` thêm "tưới N ô".

**Spec / test**
- `FARM_SPEC.md`: thêm vào §14 (tự động) và mục API.
- Test store:
  - tưới tất cả: chỉ ô đang lớn và chưa tưới, rút đúng thời gian, gọi lần hai thì báo lỗi, ô đã có 3
    lượt tưới thì bỏ qua;
  - `/auto` có tưới, kể cả ô vừa gieo trong cùng lượt.
- Test controller cho route mới.
