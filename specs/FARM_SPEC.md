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
XP cần để **lên** cấp n+1 (tổng tích luỹ): `[0, 30, 80, 160, 280, 450, 680, 1000, 1400, 1900]`
(cấp 1 = 0 XP, cấp 10 tối đa). Lên cấp thưởng `20 * cấp_mới` xu (qua `earn`).

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

type FarmView = {
  user: PublicUser;
  me: boolean;                   // đang xem nông trại của mình
  level: number; xp: number;
  level_xp: number;              // XP mốc cấp hiện tại
  next_level_xp: number | null;  // null khi tối đa
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
