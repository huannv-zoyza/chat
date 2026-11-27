// Nông trại: kiểu dữ liệu (khớp specs/FARM_SPEC.md), trạng thái mở/đóng màn hình nông trại,
// và kênh báo "farm.updated" từ websocket tới màn hình đang mở.
import { useSyncExternalStore } from "react";
import type { PublicUser } from "./api";

export type Crop = {
  id: string;
  name: string;
  emoji: string;
  seed: number;
  grow_sec: number;
  sell: number;
  xp: number;
  level: number;
};

export type PlotState = "empty" | "growing" | "ripe" | "withered";

export type Plot = {
  index: number;
  state: PlotState;
  crop?: string;
  planted_at?: string;
  ready_at?: string;
  wither_at?: string;
  progress: number;
  bug: boolean;
  waters: number;
  stolen: number;
  steals: number;
  can_water: boolean;
  can_bug: boolean;
  can_steal: boolean;
  can_harvest: boolean;
};

export type Quest = {
  id: string;
  title: string;
  goal: number;
  progress: number;
  xu: number;
  xp: number;
  done: boolean;
  claimed: boolean;
};

export type FarmLog = {
  at: string;
  actor: PublicUser;
  action:
    | "water"
    | "bug"
    | "steal"
    | "dog"
    | "propose"
    | "couple"
    | "breakup"
    | "gift";
  crop?: string;
  xu?: number;
};

/** Đặc quyền theo cấp, khớp farmPerks ở máy chủ. */
export type Perks = {
  grow_pct: number;
  animal_pct: number;
  harvest_bonus: number;
  sell_pct: number;
};

export type FarmView = {
  user: PublicUser;
  me: boolean;
  level: number;
  xp: number;
  level_xp: number;
  /** Luôn có ở máy chủ mới; null (máy chủ cũ) hoặc >= MAX_SAFE_INTEGER nghĩa là không có cấp kế. */
  next_level_xp: number | null;
  perks?: Perks;
  next_perks?: Perks;
  plots: Plot[];
  plot_count: number;
  max_plots: number;
  next_plot?: { price: number; level: number } | null;
  dog_until: string | null;
  crops: Crop[];
  now: string;
  wallet?: { balance: number; debt: number };
  quests?: Quest[];
  chest?: { ready: boolean; claimed: boolean; xu: number; xp: number };
  log?: FarmLog[];
  steals_left?: number;
  gained?: {
    xu: number;
    xp: number;
    level_up?: number;
    items?: Record<string, number>;
    /** điểm thân mật vừa cộng / cấp thân mật mới (kết đôi) */
    love?: number;
    love_up?: number;
    /** chăm nhanh cả chuồng: số con đã cho ăn, thức ăn tự mua, số con còn đói */
    fed?: number;
    bought?: Record<string, number>;
    hungry?: number;
    /** gieo tất cả: số ô vừa gieo */
    planted?: number;
    /** một lượt /auto */
    auto?: {
      harvested: number;
      planted: number;
      fed: number;
      fruits: number;
      crafted: number;
      collected: number;
      spent: number;
      skipped: string[];
    };
  };
  dog?: boolean;
  // phần 2: kho, chăn nuôi, đơn hàng, mở rộng đất (máy chủ cũ không có: để tuỳ chọn)
  items?: Item[];
  storage?: { cap: number; used: number; next_upgrade?: { price: number } | null; stock: Record<string, number> };
  buildings?: Building[];
  animal_kinds?: AnimalKind[];
  animals?: Animal[];
  orders?: Order[];
  expansion?: { tier: number; plots: number; next?: { plots: number; price: number; level: number } | null };
  // phần 3: xưởng chế biến (jobs ai xem cũng có, recipes chỉ chủ vườn)
  jobs?: Job[];
  recipes?: Recipe[];
  /** vùng cố định không xây đè được (chỉ vườn mình), để chọn chỗ đặt công trình */
  blocked?: Rect[];
  // kết đôi: couple ai xem cũng thấy; requests / sent chỉ vườn mình; state chỉ khi xem vườn người khác
  couple?: Couple;
  couple_requests?: CoupleRequest[];
  couple_sent?: CoupleRequest | null;
  couple_state?: "partner" | "can_propose" | "sent" | "incoming";
  // vườn cây ăn quả: trees và orchard_spots ai xem cũng có, tree_kinds chỉ vườn mình
  trees?: TreeView[];
  tree_kinds?: FruitTree[];
  orchard_spots?: number;
};

/** Một cây trong vườn: growing (đang lớn) | fruiting (đang ra quả) | ready (có quả để hái). */
export type TreeView = {
  spot: number;
  tree: string;
  state: "growing" | "fruiting" | "ready";
  /** RFC3339 */
  ready_at: string;
};

/** Cấp mở chỗ trồng cây: chỗ 0-2 cấp 5, 3-5 cấp 8, 6-8 cấp 12, 9-11 cấp 16 (khớp máy chủ). */
export const ORCHARD_SPOTS = 12;
export const orchardUnlockLevel = (spot: number) => [5, 8, 12, 16][Math.min(3, Math.floor(spot / 3))];
/** Chỗ trồng thứ i trên bản đồ (đơn vị ô): lưới 4 × 3. */
export const orchardSpotXY = (i: number): Spot => ({ x: 16 + 1.6 * (i % 4), y: 3 + 1.6 * Math.floor(i / 4) });

/** Cây đã có quả chưa (khớp máy chủ: ready, hoặc tới giờ ready_at) và còn bao lâu (ms, không âm). */
export function treeLive(t: TreeView, now: number): { ready: boolean; left: number } {
  const left = Math.max(0, new Date(t.ready_at).getTime() - now);
  return { ready: t.state === "ready" || left <= 0, left };
}

export type FruitTree = {
  id: string;
  name: string;
  emoji: string;
  fruit: string;
  fruit_name: string;
  fruit_emoji: string;
  price: number;
  level: number;
  mature_sec: number;
  cycle_sec: number;
  fruit_price: number;
  xp: number;
};

export type Couple = {
  partner: PublicUser;
  love: number;
  level: number;
  level_name: string;
  level_love: number;
  next_love: number | null;
  since: string;
  /** chỉ vườn mình: còn bao nhiêu lần tặng quà hôm nay */
  gifts_left?: number;
};

export type CoupleRequest = { user: PublicUser; at: string };

export type Spot = { x: number; y: number };
export type Rect = { x: number; y: number; w: number; d: number };

/** Cỡ bản đồ nông trại theo ô (khớp store/farm_place.go). */
export const FARM_MAP = { w: 24, h: 20 };

const overlaps = (a: Rect, b: Rect) =>
  a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.d && b.y < a.y + a.d;

/**
 * Công trình tự do id đặt ở spot có được không (cùng luật với máy chủ): null là được, không thì câu
 * báo lý do.
 */
export function spotProblem(
  view: FarmView,
  id: string,
  spot: Spot,
): string | null {
  const b = view.buildings?.find((x) => x.id === id);
  if (!b?.size) return "Công trình này không dời chỗ được";
  const r = { x: spot.x, y: spot.y, w: b.size.w, d: b.size.d };
  if (
    r.x < 0 ||
    r.y < 0 ||
    r.x + r.w > FARM_MAP.w ||
    r.y + r.d > FARM_MAP.h
  )
    return "Ra ngoài nông trại rồi";
  if ((view.blocked ?? []).some((z) => overlaps(r, z)))
    return "Vướng ruộng, chuồng, ao hoặc đường đi";
  const taken = (view.buildings ?? []).some(
    (o) =>
      o.id !== id &&
      o.built &&
      o.size &&
      o.spot &&
      overlaps(r, { ...o.spot, w: o.size.w, d: o.size.d }),
  );
  return taken ? "Đã có công trình khác ở đây" : null;
}

export type Recipe = {
  id: string;
  name: string;
  emoji: string;
  inputs: { item: string; qty: number }[];
  time_sec: number;
  price: number;
  xp: number;
  level: number;
  /** Công trình có lò làm món này. */
  building: "kitchen" | "juicer";
};

/** Một lò của xưởng hoặc máy ép nước đang làm / đã xong (lò trống không có trong danh sách). */
export type Job = {
  building: "kitchen" | "juicer";
  slot: number;
  recipe: string;
  ready_at: string;
  state: "working" | "ready";
};

export type Item = {
  id: string;
  name: string;
  emoji: string;
  price: number;
  kind: "crop" | "product" | "fruit" | "goods";
  level: number;
  /** Giá mua 1 đơn vị ở chợ (nông sản làm thức ăn); không có: không bán ở chợ. */
  buy_price?: number;
};

export type Building = {
  id: "coop" | "pasture" | "kitchen" | "juicer";
  name: string;
  built: boolean;
  cap: number;
  price: number;
  level: number;
  upgrade_price?: number | null;
  /** công trình tự do (xưởng): số ô chiếm và chỗ đang đặt */
  size?: { w: number; d: number };
  spot?: Spot;
};

export type AnimalKind = {
  id: string;
  name: string;
  emoji: string;
  building: "coop" | "pasture";
  price: number;
  level: number;
  feed: string;
  feed_qty: number;
  time_sec: number;
  product: string;
  xp: number;
};

export type Animal = {
  id: number;
  kind: string;
  state: "hungry" | "producing" | "ready";
  ready_at?: string;
  can_feed: boolean;
};

export type Order = {
  slot: number;
  id?: number;
  items?: { item: string; qty: number }[];
  xu?: number;
  xp?: number;
  next_at?: string;
  can_deliver?: boolean;
};

export type Neighbor = {
  user: PublicUser;
  level: number;
  xp: number;
  ripe: number;
  stealable: number;
  needs_help: number;
  dog: boolean;
  /** người thương của mình */
  partner?: boolean;
};

export type LeaderRow = { user: PublicUser; level: number; xp: number };

/* ---------- mở / đóng màn hình nông trại ---------- */

let open = false;
const subs = new Set<() => void>();
const notify = () => subs.forEach((f) => f());

export const farmScreen = {
  open() {
    if (open) return;
    open = true;
    notify();
  },
  close() {
    if (!open) return;
    open = false;
    notify();
  },
  subscribe(f: () => void) {
    subs.add(f);
    return () => {
      subs.delete(f);
    };
  },
  get: () => open,
};

export function useFarmOpen() {
  return useSyncExternalStore(farmScreen.subscribe, farmScreen.get);
}

/* ---------- sự kiện "farm.updated" ---------- */

const logSubs = new Set<(l: FarmLog) => void>();
export const farmEvents = {
  emit(l: FarmLog) {
    logSubs.forEach((f) => f(l));
  },
  on(f: (l: FarmLog) => void) {
    logSubs.add(f);
    return () => {
      logSubs.delete(f);
    };
  },
};

/* ---------- tiện ích ---------- */

/** "4:05", "1:02:09", "2 ngày". */
export function formatLeft(ms: number) {
  const s = Math.max(0, Math.ceil(ms / 1000));
  if (s >= 2 * 86400) return `${Math.floor(s / 86400)} ngày`;
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`;
}

/** Thời gian cây lớn sau đặc quyền (làm tròn xuống như máy chủ). */
export function effGrowSec(sec: number, p?: Perks) {
  return p ? Math.floor((sec * (100 - p.grow_pct)) / 100) : sec;
}

export function effAnimalSec(sec: number, p?: Perks) {
  return p ? Math.floor((sec * (100 - p.animal_pct)) / 100) : sec;
}

/** Chỉ nông sản, sản phẩm chăn nuôi và quả được thưởng giá bán; món chế biến (goods) giữ giá gốc. */
function sellPerks(kind: string, p?: Perks) {
  return kind === "goods" ? undefined : p;
}

/** Giá bán một cái sau đặc quyền (chỉ để hiển thị). */
export function effSellPrice(price: number, kind: string, p?: Perks) {
  p = sellPerks(kind, p);
  return p ? Math.floor((price * (100 + p.sell_pct)) / 100) : price;
}

/** Tổng tiền bán qty cái, tính trên cả lô như máy chủ. */
export function effSellTotal(price: number, kind: string, qty: number, p?: Perks) {
  p = sellPerks(kind, p);
  return p ? Math.floor((price * qty * (100 + p.sell_pct)) / 100) : price * qty;
}

/** Còn cấp kế tiếp không (máy chủ mới luôn có; int64 bão hoà coi như không). */
export function hasNextLevel(v: { next_level_xp: number | null }) {
  return v.next_level_xp != null && v.next_level_xp < Number.MAX_SAFE_INTEGER;
}

/** Các đặc quyền vừa tăng, để nối vào toast lên cấp. */
export function perkGains(prev?: Perks, cur?: Perks): string[] {
  if (!prev || !cur) return [];
  const out: string[] = [];
  if (cur.grow_pct > prev.grow_pct)
    out.push(`Cây lớn nhanh hơn ${cur.grow_pct}%`);
  if (cur.animal_pct > prev.animal_pct)
    out.push(`vật nuôi làm nhanh hơn ${cur.animal_pct}%`);
  if (cur.harvest_bonus > prev.harvest_bonus)
    out.push(`thu hoạch +${cur.harvest_bonus}/vụ`);
  if (cur.sell_pct > prev.sell_pct) out.push(`bán giá +${cur.sell_pct}%`);
  return out;
}

/** "5 phút", "2 giờ", "1 giờ 30 phút". */
export function formatGrow(sec: number) {
  const h = Math.floor(sec / 3600);
  const m = Math.round((sec % 3600) / 60);
  if (h && m) return `${h} giờ ${m} phút`;
  if (h) return `${h} giờ`;
  return `${m} phút`;
}

export type TendPlan = {
  collect: number;
  feed: number;
  buy: Record<string, number>;
  cost: number;
  hungryLeft: number;
};

/**
 * Dự tính "chăm nhanh" một chuồng, khớp máy chủ: thu con chín (trong giới hạn chỗ kho), rồi cho mọi con đói ăn,
 * lấy từ kho trước, thiếu thì mua ở chợ nếu đủ cấp và đủ xu. Không có gì để làm: null.
 */
export function tendPlan(
  view: FarmView,
  building: "coop" | "pasture",
  now: number,
): TendPlan | null {
  const kinds = new Map((view.animal_kinds ?? []).map((k) => [k.id, k]));
  const items = new Map((view.items ?? []).map((i) => [i.id, i]));
  const animals = (view.animals ?? []).filter(
    (a) => kinds.get(a.kind)?.building === building,
  );
  const isReady = (a: Animal) =>
    a.state === "ready" ||
    (a.state === "producing" &&
      !!a.ready_at &&
      new Date(a.ready_at).getTime() <= now);
  let room = view.storage ? view.storage.cap - view.storage.used : 0;
  const stock = { ...(view.storage?.stock ?? {}) };
  const balance = view.wallet?.balance;
  let collect = 0;
  let feed = 0;
  let hungryLeft = 0;
  let cost = 0;
  const buy: Record<string, number> = {};
  for (const a of animals) {
    let hungry = a.state === "hungry";
    if (isReady(a)) {
      if (room <= 0) continue;
      room--;
      collect++;
      hungry = true;
    }
    if (!hungry) continue;
    const k = kinds.get(a.kind);
    if (!k) continue;
    const have = stock[k.feed] ?? 0;
    const take = Math.min(have, k.feed_qty);
    const need = k.feed_qty - take;
    if (need > 0) {
      const it = items.get(k.feed);
      const price = it?.buy_price ?? 0;
      const add = price * need;
      if (
        !it ||
        price <= 0 ||
        view.level < it.level ||
        (balance !== undefined && cost + add > balance)
      ) {
        hungryLeft++;
        continue;
      }
      cost += add;
      buy[k.feed] = (buy[k.feed] ?? 0) + need;
    }
    stock[k.feed] = have - take;
    feed++;
  }
  if (collect === 0 && feed === 0) return null;
  return { collect, feed, buy, cost, hungryLeft };
}
