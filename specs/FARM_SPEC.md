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
