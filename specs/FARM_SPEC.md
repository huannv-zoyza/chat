# Nông trại & siết vay nợ

Mục tiêu: cho mọi người một cách **kiếm xu bằng công sức** thay vì vay ngân hàng vô hạn. Mọi luật
chơi chạy trên máy chủ (client chỉ hiển thị). Thời gian tính theo đồng hồ máy chủ, giờ VN cho "ngày".

## 1. Siết vay nợ (store/wallet.go)

| Luật | Cũ | Mới |
|---|---|---|
| Điều kiện vay | số dư < 100 | số dư < 100 **và không còn nợ** (`debt == 0`) |
| Mỗi lần vay | +500 xu, nợ +500 | **+300 xu, nợ +360** (lãi 20%) |
| Tần suất | không giới hạn | **1 lần / 3 ngày** (tính từ lần vay gần nhất trong `wallet_tx` kind `loan`) |
| Đang nợ mà kiếm được xu | giữ cả | **50% số xu kiếm từ nông trại và điểm danh tự trừ vào nợ** (không vượt số nợ) |

- Hằng số: `LoanAmount = 300`, `LoanDebt = 360`, `LoanCooldown = 72h`.
- Lỗi mới: `ErrLoanInDebt` ("Bạn còn nợ, trả hết rồi mới vay tiếp được."),
  `ErrLoanCooldown` (kèm thời điểm vay lại được; controller trả 400 với câu
  "Mỗi 3 ngày chỉ vay được một lần. Vay lại được lúc HH:mm ngày dd/MM.").
- `GET /api/wallet` trả thêm `loan_available_at` (RFC3339 hoặc null) và `streak` (xem dưới).
- Tự trừ nợ: hàm dùng chung `earn(ctx, tx, userID, kind, amount, note)`: cộng `amount` vào số dư;
  nếu `debt > 0` thì `repay = min(debt, amount/2)` chuyển ngay sang trả nợ (ghi thêm một dòng
  `wallet_tx` kind `auto_repay`, amount `-repay`, debt_delta `-repay`).

### Điểm danh liên tục
Bảng `wallets` thêm cột `streak INTEGER NOT NULL DEFAULT 0`. Điểm danh hôm nay mà hôm qua cũng điểm
danh thì `streak + 1`, không thì `streak = 1`. Thưởng theo ngày trong chuỗi (vòng 7 ngày):
`[100, 120, 140, 160, 180, 200, 300]` (ngày thứ 8 quay lại 100). `ClaimDaily` dùng `earn`.
`GET /api/wallet` trả `streak` (số ngày liên tục tính tới lần điểm danh gần nhất, 0 nếu đã đứt).

## 2. Nông trại

### Cây trồng (bảng cố định trong code, `farm.Crops`)

| id | Tên | emoji | Hạt (xu) | Thời gian | Bán (xu) | XP | Cấp mở |
|---|---|---|---|---|---|---|---|
| `rau_muong` | Rau muống | 🥬 | 10 | 5 phút | 14 | 2 | 1 |
| `ca_rot` | Cà rốt | 🥕 | 20 | 15 phút | 30 | 4 | 1 |
| `ca_chua` | Cà chua | 🍅 | 35 | 30 phút | 56 | 7 | 2 |
| `ngo` | Ngô | 🌽 | 50 | 1 giờ | 85 | 10 | 3 |
| `lua` | Lúa | 🌾 | 80 | 2 giờ | 145 | 16 | 4 |
| `dau_tay` | Dâu tây | 🍓 | 120 | 4 giờ | 230 | 25 | 5 |
| `dua_hau` | Dưa hấu | 🍉 | 200 | 8 giờ | 400 | 40 | 6 |
| `bi_ngo` | Bí ngô | 🎃 | 300 | 12 giờ | 640 | 60 | 8 |

### Cấp độ
XP tổng tích luỹ để đạt cấp n: cấp 1..10 theo bảng `[0, 30, 80, 160, 280, 450, 680, 1000, 1400, 1900]`.
Từ cấp 11 không giới hạn: `gap(10) = 500`, `gap(n) = round10(gap(n−1) × 1.15)`, `XP(n) = XP(n−1) + gap(n)`
(an toàn int64, dừng khi vượt `MaxInt64/2`). Lên cấp thưởng `20 * cấp_mới` xu (qua `earn`).

Đặc quyền theo cấp (`farmPerks(level)`), đều có trần:
| Đặc quyền | Công thức | Trần |
|---|---|---|
| Cây lớn nhanh hơn `grow_pct` | 2% × (lv − 1) | 50% (cấp 26) |
| Vật nuôi làm nhanh hơn `animal_pct` | 1% × (lv − 1) | 30% (cấp 31) |
| Thu hoạch thêm `harvest_bonus` | lv/5 đơn vị/vụ | +5 (cấp 25) |
| Bán giá cao hơn `sell_pct` | 2% × (lv − 1) | 40% (cấp 21) |

Tính theo cấp tại thời điểm hành động (cây đã gieo không đổi giờ chín). Giá bán kho: `floor(price × qty × (100 + sell_pct) / 100)`, chỉ cho nông sản (`crop`) và sản phẩm chăn nuôi (`product`); món chế biến (`goods`) bán đúng giá gốc để chặn vòng lặp mua ở chợ → chế biến → bán.
Đặc quyền cố ý nhân lợi nhuận sản xuất của chính mình (hạt giống → thu hoạch → bán, thức ăn → sản phẩm): đó là phần thưởng của cấp cao, bị giới hạn bởi ô đất, kho và thời gian lớn; đảm bảo không có lãi chỉ áp dụng cho mua đi bán lại ở chợ. Tưới nước vẫn rút ngắn theo `GrowSec/10` gốc.

### Ô đất
- Bắt đầu **6 ô**, mua thêm tối đa **12**. Giá ô thứ 7..12: `[200, 300, 450, 600, 800, 1000]`,
  cần cấp tối thiểu `[2, 3, 4, 5, 6, 7]`.

### Một vụ trồng
- **Trồng** (ô trống, đủ cấp, đủ xu): trừ tiền hạt (`wallet_tx` kind `farm`, note "Mua hạt Cà chua").
  `ready_at = planted_at + grow`. Lúc trồng gieo ngẫu nhiên 25% khả năng **có sâu**.
- **Sâu**: hiện ra khi tiến độ ≥ 50%. Còn sâu lúc thu hoạch thì sản lượng −30%.
  Ai cũng **bắt sâu** được (chủ hoặc người khác) → hết sâu. Người khác bắt sâu giúp: +3 xu, +1 XP.
- **Tưới nước**: mỗi vụ được tưới tối đa **3 lần** (chủ tối đa 1 lần trong số đó), mỗi lần
  trừ **10% tổng thời gian** còn lại phải chờ (dời `ready_at` sớm lên `grow/10`, không sớm hơn hiện tại).
  Chỉ tưới khi đang lớn. Người khác tưới giúp: +2 xu, +1 XP. Một người tưới một vụ tối đa 1 lần.
- **Chín** khi `now ≥ ready_at`. **Héo** khi `now ≥ ready_at + 12h` (mất trắng, phải **dọn**).
- **Thu hoạch** (chủ, đã chín, chưa héo): nhận `sell * (còn sâu ? 0.7 : 1) − stolen` xu (làm tròn xuống,
  tối thiểu 1) + XP của cây. Ô trống lại. Có `harvest all`.
- **Trộm** (người khác, đã chín, chưa héo): lấy `ceil(sell * 10%)` xu, ghi vào `stolen` của vụ.
  Mỗi vụ bị trộm tối đa **2 lần**, mỗi người trộm một vụ tối đa 1 lần, mỗi người trộm **tối đa 5 lần/ngày**
  (tổng mọi nông trại). Chủ có **chó giữ nhà** còn hạn thì 50% bị chó đuổi: kẻ trộm mất 10 xu
  (chuyển cho chủ; kẻ trộm không đủ 10 xu thì mất hết số đang có), không lấy được gì.
  Không trộm được nông trại của chính mình.
- **Chó giữ nhà**: mua 150 xu, hiệu lực 24h (mua tiếp thì cộng dồn từ lúc hết hạn hiện tại, tối đa 72h).
- **Dọn ô héo**: chủ bấm dọn, ô trống lại, không mất gì thêm.

### Nhiệm vụ hằng ngày
Mỗi người mỗi ngày (giờ VN) có **4 nhiệm vụ**, chọn cố định theo `hash(user_id, ngày)` từ kho dưới
(không lặp loại). Tiến độ đếm từ các hành động trong ngày.

| id | Mô tả (n = mục tiêu) | n | Thưởng xu | XP |
|---|---|---|---|---|
| `harvest` | Thu hoạch n vụ | 5 | 40 | 10 |
| `plant` | Gieo n hạt | 6 | 30 | 8 |
| `plant_rare` | Trồng n cây từ cấp 3 trở lên | 2 | 50 | 12 |
| `water` | Tưới nước giúp n người hàng xóm | 3 | 30 | 8 |
| `bug` | Bắt n con sâu | 2 | 30 | 8 |
| `steal` | Trộm thành công n lần | 2 | 25 | 6 |
| `earn` | Bán nông sản được n xu | 150 | 50 | 12 |
| `visit` | Ghé thăm n nông trại khác | 3 | 20 | 5 |

Hoàn thành cả 4 → nhận thêm **rương thưởng ngày**: 80 xu + 20 XP (một lần/ngày).
Nhận thưởng phải bấm (`claim`), không tự cộng.

### Nhật ký
Mỗi hành động của **người khác** lên nông trại của mình (tưới, bắt sâu, trộm, bị chó đuổi) ghi vào
nhật ký (giữ 50 dòng mới nhất mỗi người) và bắn sự kiện `farm.updated` tới chủ.

## 3. API (đều cần đăng nhập; JSON; lỗi `{ "error": "câu tiếng Việt" }`)

```
GET  /api/farm                     -> FarmView của mình (kèm quests, log, wallet)
GET  /api/farm/users/{userId}      -> FarmView nông trại người khác (không có quests/log; tính lượt "visit")
GET  /api/farm/neighbors           -> Neighbor[] (mọi người dùng thật trừ mình, bot, AI)
GET  /api/farm/leaderboard         -> LeaderRow[] (top 20 theo XP)
POST /api/farm/plant      {plot, crop}        -> FarmView
POST /api/farm/harvest    {plot} | {all:true} -> FarmView + "gained": {xu, xp}
POST /api/farm/clear      {plot}              -> FarmView
POST /api/farm/water      {user_id, plot}     -> FarmView (của nông trại vừa tác động)
POST /api/farm/water      {all: true}         -> FarmView + "gained": {watered} (tưới mọi ô của mình còn tưới được)
POST /api/farm/bug        {user_id, plot}     -> FarmView
POST /api/farm/steal      {user_id, plot}     -> FarmView + "gained" (âm nếu bị chó đuổi) + "dog": bool
POST /api/farm/buy-plot                       -> FarmView
POST /api/farm/buy-dog                        -> FarmView
POST /api/farm/quests/{questId}/claim         -> FarmView   (questId "chest" = rương thưởng ngày)
```

`user_id` ở water/bug: có thể là chính mình (chủ tự tưới/bắt sâu).

### Kiểu dữ liệu

```ts
type Crop = { id: string; name: string; emoji: string; seed: number; grow_sec: number;
              sell: number; xp: number; level: number };

type Plot = {
  index: number;                 // 0..plots-1
  state: "empty" | "growing" | "ripe" | "withered";
  crop?: string;                 // id cây
  planted_at?: string; ready_at?: string; wither_at?: string;   // RFC3339
  progress: number;              // 0..1 (ripe/withered = 1)
  bug: boolean;                  // đang có sâu (đã lộ ra)
  waters: number;                // số lần đã tưới vụ này
  stolen: number;                // số xu đã bị trộm khỏi vụ này
  steals: number;                // số lần bị trộm
  // cờ cho người đang xem:
  can_water: boolean; can_bug: boolean; can_steal: boolean; can_harvest: boolean;
};

type Perks = { grow_pct: number; animal_pct: number; harvest_bonus: number; sell_pct: number };

type FarmView = {
  user: PublicUser;
  me: boolean;                   // đang xem nông trại của mình
  level: number; xp: number;
  level_xp: number;              // XP mốc cấp hiện tại
  next_level_xp: number;         // luôn có; ở cấp không thể đạt là MaxInt64 (client coi như không có cấp kế)
  perks: Perks;                  // đặc quyền cấp hiện tại
  next_perks: Perks;             // đặc quyền cấp kế tiếp
  plots: Plot[];
  plot_count: number; max_plots: number;
  next_plot?: { price: number; level: number } | null;
  dog_until: string | null;
  crops: Crop[];                 // toàn bộ danh mục
  now: string;                   // giờ máy chủ, để client đếm ngược đúng
  wallet?: { balance: number; debt: number };          // chỉ khi me
  quests?: Quest[]; chest?: { ready: boolean; claimed: boolean; xu: number; xp: number };
  log?: LogEntry[];
  steals_left?: number;          // lượt trộm còn lại hôm nay của người xem
  gained?: { xu: number; xp: number; level_up?: number };  // kết quả hành động vừa làm
  dog?: boolean;                 // vừa bị chó đuổi
};

type Quest = { id: string; title: string; goal: number; progress: number;
               xu: number; xp: number; done: boolean; claimed: boolean };

type LogEntry = { at: string; actor: PublicUser; action: "water" | "bug" | "steal" | "dog";
                  crop?: string; xu?: number };

type Neighbor = { user: PublicUser; level: number; xp: number;
                  ripe: number; stealable: number; needs_help: number;  // số ô cần tưới / có sâu
                  dog: boolean };

type LeaderRow = { user: PublicUser; level: number; xp: number };
```

Sự kiện websocket tới chủ nông trại: `{ "type": "farm.updated", "log": LogEntry }`.

---

# Phần 2: Kho, chăn nuôi, đơn hàng, mở rộng đất

Tham khảo: Nông trại vui vẻ (mở rộng đất, cho vật nuôi ăn ra trứng/sữa), FarmVille (con vật cho sản phẩm
theo giờ, không chết, sẵn sàng thì có dấu báo; mở rộng đất bằng xu), Hay Day (nông sản vào kho, dùng làm
thức ăn, giao đơn hàng xe tải lấy xu; kho có sức chứa, nâng cấp được).

## 4. Kho

- Thu hoạch **không trả xu ngay** nữa: mỗi vụ cho `3` đơn vị nông sản vào kho (còn sâu: `2`).
  XP vẫn cộng lúc thu hoạch. Quest `harvest` vẫn đếm vụ; quest `earn` đếm xu **bán từ kho / giao đơn**.
- Giá bán một đơn vị nông sản = `ceil(sell / 3)` (bán 3 đơn vị ≈ giá cũ của cả vụ).
- **Trộm**: lấy `1` đơn vị vào kho kẻ trộm (thay vì xu); một vụ bị trộm tối đa 2 lần nên chủ còn ≥ 1.
  Thu hoạch vụ bị trộm: `3 - stolen` (còn sâu thì `2 - stolen`, tối thiểu 1). Cột `stolen` đổi nghĩa thành
  số đơn vị bị lấy. Chó đuổi: như cũ (kẻ trộm mất 10 xu cho chủ).
- Sức chứa kho: bắt đầu `60` đơn vị (mọi loại cộng lại); nâng cấp `+30` mỗi lần, giá `[300, 600, 1000, 1600, 2400]`
  (tối đa 210). Kho đầy thì không thu hoạch / thu sản phẩm / trộm được (lỗi "Kho đầy, bán bớt hoặc nâng cấp kho.").
- Bán: `POST /api/farm/sell {item, qty}` (qty ≤ số đang có) và `{all: true}` bán sạch mọi thứ.
  Tiền bán qua `earn` (đang nợ thì 50% tự trả nợ).

## 5. Chăn nuôi

Chuồng là công trình phải xây (mua) trước khi nuôi:

| id | Tên | Giá xây | Cấp cần | Sức chứa (cơ bản → nâng cấp) | Giá nâng cấp |
|---|---|---|---|---|---|
| `coop` | Chuồng gà | 200 | 2 | 4 → 8 | 400 |
| `pasture` | Đồng cỏ (bò, cừu, dê, heo) | 600 | 3 | 3 → 6 | 900 |

| id | Tên | emoji | Chuồng | Giá | Cấp | Ăn (từ kho) | Thời gian | Sản phẩm | Giá SP | XP |
|---|---|---|---|---|---|---|---|---|---|---|
| `ga` | Gà | 🐔 | coop | 80 | 2 | 1 `rau_muong` | 20 phút | `trung` Trứng 🥚 | 22 | 2 |
| `bo` | Bò | 🐄 | pasture | 300 | 3 | 1 `ngo` | 1 giờ | `sua` Sữa 🥛 | 75 | 6 |
| `de` | Dê | 🐐 | pasture | 400 | 5 | 1 `ca_chua` | 1.5 giờ | `pho_mai` Phô mai 🧀 | 95 | 8 |
| `cuu` | Cừu | 🐑 | pasture | 450 | 4 | 2 `ca_rot` | 2 giờ | `len` Len 🧶 | 110 | 10 |
| `heo` | Heo | 🐖 | pasture | 650 | 6 | 1 `dau_tay` | 3 giờ | `nam` Nấm truffle 🍄 | 220 | 15 |

- Con vật có 3 trạng thái: `hungry` (đói, chờ cho ăn) → `producing` (đã ăn, đang làm ra sản phẩm, có `ready_at`)
  → `ready` (sẵn sàng thu). Không chết, không héo. Mới mua thì `hungry`.
- **Cho ăn** (chủ): trừ thức ăn trong kho (thiếu thì lỗi "Thiếu <tên cây> trong kho để cho <tên con> ăn.").
  Có `feed all` cho mọi con đang đói mà đủ thức ăn.
- **Thu** (chủ): sản phẩm `1` vào kho, + XP, con vật về `hungry`. Có `collect all`.
- Người khác **cho ăn giúp** bằng thức ăn trong kho **của chủ**? Không. Hàng xóm chỉ xem được vật nuôi.
- Bán vật nuôi: không (đơn giản).

## 6. Đơn hàng (xe tải)

- Luôn có tối đa **3 đơn**. Mỗi đơn đòi 1–3 loại hàng (nông sản hoặc sản phẩm chăn nuôi **người chơi đã mở
  khoá theo cấp**), mỗi loại 1–4 đơn vị. Thưởng xu = `ceil(1.35 × tổng giá trị đơn vị)`, XP = `ceil(xu/10)`.
- Giao đơn (`POST /api/farm/orders/{id}/deliver`): đủ hàng thì trừ kho, cộng xu (qua `earn`) và XP. Ô đơn đó
  trống, **đơn mới sau 5 phút**. Huỷ đơn (`DELETE /api/farm/orders/{id}`): ô trống, đơn mới sau 15 phút.
- Sinh đơn ngẫu nhiên phía máy chủ (dùng RNG đã có), lưu DB.
- Quest mới `order`: "Giao n đơn hàng" (n = 2, thưởng 50 xu, 12 XP) vào kho quest.

## 7. Mở rộng đất

- Thay cho mua từng ô: **mở rộng theo bậc**. Số ô theo bậc: `[6, 9, 12, 16, 20]`, giá lên bậc kế:
  `[400, 1000, 2200, 4500]`, cấp cần: `[2, 4, 6, 8]`.
- Người đã mua lẻ ô trước đây: bậc hiện tại = bậc lớn nhất có số ô ≤ số ô đang có; bậc kế cho số ô của bậc kế.
  Ô đã có không bị mất.
- `max_plots` = 20.

## 8. API thêm / đổi

```
POST /api/farm/sell            {item, qty} | {all:true}        -> FarmView + gained {xu}
POST /api/farm/expand                                           -> FarmView   (thay /buy-plot; giữ /buy-plot làm bí danh)
POST /api/farm/storage/upgrade                                  -> FarmView
POST /api/farm/buildings/{id}           (xây coop | pasture)    -> FarmView
POST /api/farm/buildings/{id}/upgrade                           -> FarmView
POST /api/farm/animals         {kind}                           -> FarmView   (mua con vật)
POST /api/farm/animals/feed    {id} | {all:true}                -> FarmView
POST /api/farm/animals/collect {id} | {all:true}                -> FarmView + gained {xp}
POST /api/farm/orders/{id}/deliver                              -> FarmView + gained {xu, xp}
DELETE /api/farm/orders/{id}                                    -> FarmView
```

FarmView thêm (chỉ khi `me`, trừ `animals`, `buildings` và `expansion` thì có cả khi xem nhà người khác):

```ts
items: Item[];        // danh mục mọi món có thể vào kho: nông sản + sản phẩm chăn nuôi
storage: { cap: number; used: number; next_upgrade?: {price:number} | null; stock: Record<string, number> };
buildings: { id: "coop"|"pasture"; name: string; built: boolean; cap: number; price: number; level: number;
             upgrade_price?: number | null }[];
animal_kinds: AnimalKind[];
animals: Animal[];
orders: Order[];      // 3 ô; ô trống có next_at
expansion: { tier: number; plots: number; next?: { plots: number; price: number; level: number } | null };

type Item = { id: string; name: string; emoji: string; price: number; kind: "crop" | "product"; level: number };
type AnimalKind = { id: string; name: string; emoji: string; building: "coop"|"pasture"; price: number;
                    level: number; feed: string; feed_qty: number; time_sec: number; product: string; xp: number };
type Animal = { id: number; kind: string; state: "hungry"|"producing"|"ready"; ready_at?: string; can_feed: boolean };
type Order = { slot: number; id?: number; items?: {item:string; qty:number}[]; xu?: number; xp?: number;
               next_at?: string; can_deliver?: boolean };
```

`gained` có thêm `items?: Record<string, number>` khi vật vào kho (thu hoạch / thu sản phẩm / trộm).
`crops[].sell` giữ nguyên (giá cả vụ); giá đơn vị lấy từ `items[].price`.

# Phần 3: Xưởng chế biến

Tham khảo Hay Day (tiệm bánh, máy làm bơ...): nguyên liệu trong kho → món giá cao hơn, có thời gian chờ.

## 9. Xưởng chế biến

- Công trình `kitchen` "Xưởng chế biến" trong danh sách chuồng trại: giá xây **500**, cần **cấp 4**; có **2 lò**,
  nâng cấp (**1200** xu) lên **4 lò**. `buildings[].cap` của xưởng là số lò.
- Mỗi lò nấu một món một lúc. **Bắt đầu** (`POST /api/farm/craft {recipe}`): đủ cấp, đã xây xưởng, còn lò trống
  (lấy lò trống có số nhỏ nhất), đủ nguyên liệu trong kho thì trừ nguyên liệu ngay, lò bận tới `ready_at`.
  Thiếu thì lỗi "Thiếu 1 rau muống, 1 cà rốt trong kho để làm Salad rau." (liệt kê mọi thứ thiếu).
- **Lấy món** (`POST /api/farm/craft/collect {slot} | {all:true}`): món đã xong vào kho (1 đơn vị, cần chỗ
  trong kho; `all` lấy lần lượt tới khi kho đầy) và cộng XP của món. Lò trống lại.
- Món làm ra là `Item` kind `goods` (cùng id với món), bán ở kho theo `price`, không mua ở chợ được.
- Đơn hàng xe tải chỉ đòi món chế biến khi đã xây xưởng; mỗi đơn nhiều nhất **một** loại món, 1–2 món.

| id | Món | emoji | Nguyên liệu | Thời gian | Giá | XP | Cấp |
|---|---|---|---|---|---|---|---|
| `salad` | Salad rau | 🥗 | 2 rau muống, 1 cà rốt, 1 cà chua | 15 phút | 60 | 4 | 4 |
| `bap_rang` | Bắp rang | 🍿 | 2 ngô | 30 phút | 88 | 6 | 4 |
| `com_nam` | Cơm nắm | 🍙 | 2 lúa | 45 phút | 148 | 9 | 5 |
| `banh_flan` | Bánh flan | 🍮 | 2 trứng, 1 sữa | 1 giờ | 180 | 11 | 5 |
| `sinh_to` | Sinh tố dâu | 🥤 | 2 dâu tây, 1 sữa | 1,5 giờ | 345 | 18 | 6 |
| `pizza` | Pizza | 🍕 | 1 lúa, 2 cà chua, 1 phô mai | 2 giờ | 275 | 16 | 6 |
| `kem_dua` | Kem dưa hấu | 🍧 | 1 dưa hấu, 2 sữa | 2 giờ | 425 | 22 | 7 |
| `ao_len` | Áo len | 🧥 | 3 len | 3 giờ | 495 | 26 | 7 |
| `banh_bi_ngo` | Bánh bí ngô | 🥧 | 1 bí ngô, 2 trứng, 1 sữa | 4 giờ | 500 | 30 | 8 |
| `sup_nam` | Súp nấm | 🍲 | 1 nấm truffle, 1 sữa, 2 cà rốt | 3 giờ | 475 | 28 | 9 |

FarmView thêm: `jobs: {slot, recipe, ready_at, state: "working"|"ready"}[]` (luôn có, hàng xóm cũng thấy để vẽ
khói bếp) và `recipes: {id, name, emoji, inputs: {item, qty}[], time_sec, price, xp, level}[]` (chỉ khi `me`).

## 10. Chọn chỗ đặt công trình

- Bản đồ nông trại là lưới **24 × 20** ô. Công trình **tự do** (hiện có xưởng chế biến, chiếm **3 × 2** ô, chỗ mặc
  định ô `(12, 12)`) đặt được ở bất cứ đâu: không ra ngoài bản đồ, không chồng lên vùng cố định (`blocked`: ruộng
  cỡ lớn nhất, chuồng gà, đồng cỏ, nhà kho, vườn cây, ao, đường đá, xe tải, luống hoa...) và không chồng lên
  công trình tự do khác. Chuồng gà, đồng cỏ cố định chỗ.
- Xây: `POST /api/farm/buildings/{id} {x, y}` (không gửi chỗ thì đặt ở chỗ mặc định). Dời (miễn phí):
  `POST /api/farm/buildings/{id}/move {x, y}`. Lỗi: "Chỗ này ra ngoài nông trại rồi.", "Chỗ này vướng ruộng,
  chuồng, ao hoặc đường đi, chọn chỗ khác nhé.", "Chỗ này đã có công trình khác.", "Công trình này không dời chỗ
  được.". Chỗ không hợp lệ thì không trừ xu.
- `buildings[]` thêm `size: {w, d}` và `spot: {x, y}` cho công trình tự do (hàng xóm cũng thấy để vẽ đúng chỗ);
  FarmView thêm `blocked: {x, y, w, d}[]` (chỉ khi `me`) để giao diện tô xanh / đỏ lúc chọn chỗ.
- Giao diện: bấm xây hoặc "Đổi chỗ" thì hiện bản nháp công trình trên bản đồ; kéo bản nháp hoặc chạm chỗ khác để
  dời, rồi bấm "Xây ở đây" / "Đặt ở đây".

# Phần 4: Kết đôi

## 11. Kết đôi và độ thân mật

- Hai người dùng thật **độc thân** kết đôi được. Ghé vườn người kia, bấm "Ngỏ lời" (`POST /api/farm/couple/propose
  {user_id}`); mỗi người chỉ có **một** lời ngỏ đang chờ (ngỏ người mới thì thay lời cũ); người kia đã ngỏ lời
  với mình trước thì **thành đôi luôn**. Người nhận trả lời `POST /api/farm/couple/respond {user_id, accept}`.
  Rút lời ngỏ: `DELETE /api/farm/couple/propose`. Thành đôi thì mọi lời ngỏ liên quan tới hai người bị xoá.
- Mỗi người **một đôi**. Chia tay (`DELETE /api/farm/couple`): điểm thân mật mất hết, cả hai độc thân lại.
- **Điểm thân mật** (của cả cặp) tăng khi: tưới giúp người thương **+2**, bắt sâu giúp **+3**, ghé vườn người
  thương lần đầu trong ngày **+2**, tặng quà (`POST /api/farm/couple/gift {item, qty}`): chuyển 1–10 đơn vị một
  món trong kho mình sang kho người thương (kho người kia phải đủ chỗ), tối đa **3 lần/ngày**, điểm =
  `ceil(giá trị / 20)` trong khoảng 1..10.
- **Cấp thân mật** theo điểm `[0, 20, 60, 120, 200, 320, 480, 700, 1000]`: Mới quen, Cảm nắng, Hẹn hò, Thân thiết,
  Yêu nhau, Gắn bó, Tri kỷ, Vợ chồng son, Bách niên giai lão. Lên cấp n: **mỗi người** nhận `30 × n` xu.
- Không trộm được vườn người thương ("Ai nỡ trộm vườn người thương 💕").
- Nhật ký / farm.updated: `propose` (ai đó ngỏ lời), `couple` (thành đôi), `breakup`, `gift` (`crop` = món, `xu` =
  số lượng).
- FarmView: `couple: {partner, love, level, level_name, level_love, next_love, since, gifts_left?}` (ai xem cũng
  thấy; `gifts_left` chỉ chủ vườn); chủ vườn có thêm `couple_requests: {user, at}[]` và `couple_sent`; xem vườn
  người khác có `couple_state`: `partner` | `can_propose` | `sent` | `incoming` | không có (một trong hai đã có
  đôi). `gained` thêm `love`, `love_up`. `neighbors[].partner`.

# Phần 5: Vườn cây ăn quả, Máy ép nước, Tự động

Thiết kế: `docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md`. Phần này thêm nội dung cho cấp
5–18 và các nút "làm hết" cho người có nhiều ô, chuồng, cây, lò.

## 12. Vườn cây ăn quả

- Cây trồng **một lần, hái nhiều lần** (khác ruộng: không bị xoá sau khi thu). Bảng `farm_trees`
  (migration 000032): `id, user_id, spot, tree, planted_at, ready_at, cycles`, duy nhất `(user_id, spot)`.
  `ready_at` là giây unix cây có quả (lần đầu: lớn xong); `cycles` là số lần đã hái (0 = đang lớn).
- Có **12** chỗ trồng (`spot` 0..11, lưới 4×3, vùng `{15.5, 2.5, 6.5, 4.8}` thuộc `blocked` trên bản đồ). Số chỗ mở theo
  cấp: dưới cấp 5 khoá; cấp 5 → **3**; cấp 8 → **6**; cấp 12 → **9**; cấp 16 → **12**.
- **Trồng** (`POST /api/farm/orchard/plant {spot, tree}`): chỗ đã mở và còn trống, đủ cấp của cây, đủ xu (trừ giá
  cây giống). Cây ra quả lần đầu sau `mature_sec × (100 − grow_pct) / 100` (`grow_pct` theo cấp lúc trồng).
- **Hái** (`POST /api/farm/orchard/harvest {spot} | {all:true}`): mỗi cây chín cho `3 + harvest_bonus` quả vào kho
  và `xp` của cây; cây bắt đầu chu kỳ mới `cycle_sec × (100 − grow_pct) / 100`, `cycles` tăng 1. Hái một cây chưa
  có quả: "Cây chưa có quả."; chỗ không có cây: "Chỗ này chưa có cây."; `all` mà chưa cây nào chín: "Chưa có cây
  nào có quả để hái.". Kho không đủ chỗ cho cả một lượt quả của một cây thì "kho đầy"; `all` hái theo thứ tự chỗ
  tới khi hết chỗ trong kho (cây nào không vừa thì để lại).
- **Chặt** (`DELETE /api/farm/orchard/{spot}`): xoá cây, không hoàn tiền, để trồng loại khác.
- Trạng thái cây: `growing` (chưa ra quả lần nào), `fruiting` (đang chờ lần ra quả kế), `ready`.
- Quả là `Item` kind `fruit` (id quả = id cây), `Level` = cấp cây, không mua ở chợ, bán ở kho theo `price` có cộng
  `sell_pct`. Đơn hàng xe tải chỉ có quả loại nào khi chủ vườn đang có ít nhất một cây loại đó.

| id | Cây | Quả (emoji) | Cấp | Giá cây | Lớn | Chu kỳ | Giá quả | XP/lần hái |
|---|---|---|---|---|---|---|---|---|
| `cam` | Cây cam | Cam 🍊 | 5 | 300 | 2 giờ | 1 giờ | 18 | 4 |
| `tao` | Cây táo | Táo 🍎 | 7 | 500 | 3 giờ | 1,5 giờ | 26 | 6 |
| `chuoi` | Cây chuối | Chuối 🍌 | 9 | 700 | 4 giờ | 2 giờ | 34 | 8 |
| `xoai` | Cây xoài | Xoài 🥭 | 11 | 1000 | 6 giờ | 3 giờ | 48 | 11 |
| `dua` | Cây dừa | Dừa 🥥 | 14 | 1500 | 8 giờ | 4 giờ | 65 | 15 |
| `nho` | Cây nho | Nho 🍇 | 18 | 2200 | 10 giờ | 5 giờ | 90 | 20 |

FarmView thêm: `trees: {spot, tree, state, ready_at}[]` và `orchard_spots` (ai xem cũng thấy), `tree_kinds`
(danh mục trên, chỉ khi `me`: `id, name, emoji, fruit, fruit_name, fruit_emoji, price, level, mature_sec, cycle_sec,
fruit_price, xp`). Hái trả `gained: {xp, level_up?, items}`.

## 13. Máy ép nước

- Công trình `juicer` "Máy ép nước" (tự do, **2 × 2** ô, chỗ mặc định `(16, 13)`, dời được như xưởng, xem §10): giá xây
  **800**, cần **cấp 6**; có **2 lò**, nâng cấp (**1600** xu) lên **4 lò**. `buildings[].cap` là số lò.
- Mỗi lò thuộc một công trình: bảng `farm_jobs` có thêm cột `building` (mặc định `kitchen`), khoá chính
  `(user_id, building, slot)` (migration 000033 dựng lại bảng, giữ nguyên job xưởng đang chạy). `Recipe` thêm trường
  `building` (`kitchen` | `juicer`); nấu, thu, view đều theo công trình của món.
- **Nấu** (`POST /api/farm/craft {recipe, fill?}`): kiểm tra như §9 nhưng theo công trình của món. Chưa xây máy ép:
  "Cần xây Máy ép nước trước."; hết lò: "Các lò của Máy ép nước đều đang bận." (xưởng giữ lời cũ). Không `fill`: nấu ở lò
  trống số nhỏ nhất. `fill: true`: nấu liên tiếp ở mọi lò trống của công trình tới khi hết lò hoặc hết nguyên liệu;
  chỉ báo lỗi khi không bắt đầu được lò nào (lỗi như nấu một món).
- **Lấy món** (`POST /api/farm/craft/collect {building?, slot} | {all:true, building?}`): `building` là `kitchen` |
  `juicer`, giá trị khác báo "công trình không hợp lệ". Bỏ trống: lấy một lò thì mặc định xưởng; `all` thì mọi công
  trình. `all` lấy tới khi kho đầy.
- Món làm ra là `Item` kind `goods` (cùng id món), bán theo `price`, không cộng `sell_pct`. Đơn hàng chỉ đòi món của
  công trình đã xây; mỗi đơn vẫn nhiều nhất một loại `goods`.

| id | Món | emoji | Nguyên liệu | Thời gian | Giá | XP | Cấp |
|---|---|---|---|---|---|---|---|
| `nuoc_cam` | Nước cam | 🧃 | 3 cam | 20 phút | 85 | 8 | 6 |
| `nuoc_tao` | Nước táo | 🍏 | 3 táo | 30 phút | 120 | 10 | 7 |
| `sinh_to_chuoi` | Sinh tố chuối | 🧋 | 2 chuối, 1 sữa | 40 phút | 215 | 14 | 9 |
| `nuoc_xoai` | Nước xoài | 🫙 | 3 xoài | 1 giờ | 215 | 18 | 11 |
| `nuoc_thap_cam` | Nước ép thập cẩm | 🍹 | 1 cam, 1 táo, 1 xoài, 1 cà rốt | 2 giờ | 165 | 14 | 12 |
| `nuoc_dua` | Nước dừa | 🌴 | 2 dừa | 45 phút | 195 | 16 | 14 |
| `nuoc_nho` | Nước nho | 🍷 | 4 nho | 90 phút | 540 | 32 | 18 |

FarmView: `jobs[]` thêm `building`; `recipes[]` thêm `building` (chỉ khi `me`; gồm cả món xưởng ở §9 và món máy ép).

## 14. Tự động

Chỉ áp dụng cho nông trại của chính mình. Không có tác vụ nền trên máy chủ: tự động chỉ chạy khi app đang mở.

### Máy chủ

- **Gieo tất cả** `POST /api/farm/plant {all:true, crop}` (thay `{plot, crop}`): dọn mọi ô héo, gieo `crop` vào các ô trống
  theo thứ tự tới khi hết xu (thiếu xu thì gieo một phần). Cần đủ cấp. Không có ô trống: lỗi; không đủ xu cho ô nào:
  "không đủ xu". `gained: {planted, xu: -tiền hạt}`.
- **Tưới tất cả** `POST /api/farm/water {all:true}`: tưới mọi ô của chính mình đang lớn và `can_water`; `gained.watered` =
  số ô đã tưới. Không có ô nào cần tưới: lỗi "Không có cây nào cần tưới."
- **Hái tất cả** và **nấu đầy lò**: xem §12 (`orchard/harvest {all:true}`) và §13 (`craft {recipe, fill:true}`).
- **Làm hết** `POST /api/farm/auto`, body `{crop?, kitchen_recipe?, juicer_recipe?, buy_feed}`. Tất cả trong **một
  giao dịch**, theo thứ tự:
  1. thu hoạch mọi ô chín;
  2. gieo `crop` (nếu có; "không có ô trống" thì bỏ qua lặng lẽ);
  3. chăm chuồng gà rồi đồng cỏ (thu + cho ăn; `buy_feed` tắt: chỉ dùng thức ăn trong kho, không tốn xu; "không có gì
     để thu hay cho ăn" và "không có thức ăn" bỏ qua lặng lẽ);
  4. hái mọi cây chín còn vừa kho;
  5. thu mọi lò đã xong của xưởng và máy ép (kho đầy thì không thu, không báo lỗi);
  2b. tưới mọi ô đang lớn còn tưới được (không có ô nào thì bỏ qua lặng lẽ), `auto.watered` = số ô;
  6. lấp lò xưởng bằng `kitchen_recipe`, rồi lò máy ép bằng `juicer_recipe` (món phải thuộc đúng công trình, không thì
     "Món X không làm ở đây.").
- **SAVEPOINT mỗi bước**: bước gặp lỗi nghiệp vụ (`FarmError` hoặc không đủ xu) bị hoàn lại mọi ghi của riêng bước đó,
  lý do ghi vào `skipped` dạng `"<nhãn>: <lỗi>"` (nhãn: Thu hoạch, Gieo, Chuồng gà, Đồng cỏ, Vườn cây, Thu món, Xưởng,
  Máy ép), các bước khác vẫn chạy. Lỗi khác (DB) hoàn lại và dừng cả lượt.
- Kết quả: `gained` gộp mọi bước (`xu` = −tổng đã tiêu, `xp`, `level_up`, `items`, `fed`, `planted`...) và
  `gained.auto = {harvested, planted, watered, fed, fruits, crafted, collected, spent, skipped: string[]}`. `harvested` = số ô thu,
  `fruits` = số quả hái, `collected` = số sản phẩm chăn nuôi + món đã thu, `crafted` = số lò vừa bắt đầu, `spent` = xu đã
  trả cho hạt giống và thức ăn (số dương).
- Không có `harvested + planted + watered + fed + fruits + crafted + collected` nào thì lỗi `ErrFarmNothingAuto`: "Chưa có việc
  gì để làm." (cả giao dịch hoàn lại).

### Giao diện (`desktop/src/lib/farmAuto.ts`, `components/Farm.tsx`)

- Cài đặt `{on, crop?, kitchen_recipe?, juicer_recipe?, buy_feed}` lưu ở settings key `farm-auto:<userId>`; thiếu hoặc
  hỏng thì mặc định tắt (`buy_feed` mặc định **tắt**, không bao giờ tự bật mua thức ăn).
- `autoPending(view, now, settings)` ước lượng lạc quan còn việc không: ô chín, ô đang lớn còn tưới được, ô trống / héo khi đã chọn cây, vật nuôi
  sẵn sàng hoặc đói (có thức ăn trong kho hoặc `buy_feed`), cây có quả, lò xong, lò trống đủ nguyên liệu cho món đã chọn.
  Nút "⚡ Làm hết" (một chạm, gọi `/auto` rồi báo `autoSummary`, chỉ liệt kê phần khác 0) hiện khi `autoPending`; "🌱 Gieo
  tất cả" khi có ô trống (chưa chọn cây thì mở cửa hàng hạt); "💧 Tưới tất cả" khi `waterablePlots > 0`. `autoSummary`
  viết hoa chữ đầu, ví dụ "🤖 Gieo 4, tưới 4 ô".
- Công tắc "🤖 Tự động" (kèm popover cài đặt cây, món xưởng, món máy ép, mua thức ăn): khi bật và đang ở vườn **của mình**
  (không phải đi thăm), cứ **30 giây** kiểm tra `autoPending` và gọi `/auto` nếu còn việc; không chạy chồng khi đang bận.
  Gặp **lỗi** thì **tạm dừng 5 phút** và hiện toast một lần; riêng "Chưa có việc gì để làm." thì không hiện toast (nhưng
  vẫn tạm dừng).
