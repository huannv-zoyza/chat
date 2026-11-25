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
  action: "water" | "bug" | "steal" | "dog";
  crop?: string;
  xu?: number;
};

export type FarmView = {
  user: PublicUser;
  me: boolean;
  level: number;
  xp: number;
  level_xp: number;
  next_level_xp: number | null;
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
  gained?: { xu: number; xp: number; level_up?: number; items?: Record<string, number> };
  dog?: boolean;
  // phần 2: kho, chăn nuôi, đơn hàng, mở rộng đất (máy chủ cũ không có: để tuỳ chọn)
  items?: Item[];
  storage?: { cap: number; used: number; next_upgrade?: { price: number } | null; stock: Record<string, number> };
  buildings?: Building[];
  animal_kinds?: AnimalKind[];
  animals?: Animal[];
  orders?: Order[];
  expansion?: { tier: number; plots: number; next?: { plots: number; price: number; level: number } | null };
};

export type Item = {
  id: string;
  name: string;
  emoji: string;
  price: number;
  kind: "crop" | "product";
  level: number;
  /** Giá mua 1 đơn vị ở chợ (nông sản làm thức ăn); không có: không bán ở chợ. */
  buy_price?: number;
};

export type Building = {
  id: "coop" | "pasture";
  name: string;
  built: boolean;
  cap: number;
  price: number;
  level: number;
  upgrade_price?: number | null;
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

/** "5 phút", "2 giờ", "1 giờ 30 phút". */
export function formatGrow(sec: number) {
  const h = Math.floor(sec / 3600);
  const m = Math.round((sec % 3600) / 60);
  if (h && m) return `${h} giờ ${m} phút`;
  if (h) return `${h} giờ`;
  return `${m} phút`;
}
