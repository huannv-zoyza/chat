// Chế độ tự động của nông trại: cài đặt lưu theo người dùng và việc "còn gì để làm".
import { load, save } from "./storage";
import { liveState, treeLive, type FarmView } from "./farm";

export type FarmAutoSettings = {
  on: boolean;
  crop?: string;
  kitchen_recipe?: string;
  juicer_recipe?: string;
  buy_feed: boolean;
};

export const DEFAULT_AUTO: FarmAutoSettings = { on: false, buy_feed: false };

const key = (userId: number) => `farm-auto:${userId}`;

const str = (v: unknown) => (typeof v === "string" && v ? v : undefined);

/** Đọc cài đặt; thiếu hoặc hỏng thì dùng mặc định (không bao giờ tự bật mua thức ăn). */
export function loadAuto(userId: number): FarmAutoSettings {
  const raw = load<unknown>(key(userId), null);
  if (!raw || typeof raw !== "object") return { ...DEFAULT_AUTO };
  const o = raw as Record<string, unknown>;
  return {
    on: o.on === true,
    crop: str(o.crop),
    kitchen_recipe: str(o.kitchen_recipe),
    juicer_recipe: str(o.juicer_recipe),
    buy_feed: o.buy_feed === true,
  };
}

export function saveAuto(userId: number, s: FarmAutoSettings) {
  save(key(userId), s);
}

/** Còn việc để làm không (lạc quan: máy chủ vẫn có thể báo "Chưa có việc gì để làm."). */
export function autoPending(view: FarmView, now: number, s: FarmAutoSettings): boolean {
  const stock = view.storage?.stock ?? {};
  for (const p of view.plots) {
    if (p.index >= view.plot_count) continue;
    const st = liveState(p, now).state;
    if (st === "ripe") return true;
    if (p.bug && p.can_bug) return true;
    if (st === "growing" && p.can_water) return true;
    if ((st === "empty" || st === "withered") && s.crop) return true;
  }
  for (const a of view.animals ?? []) {
    if (
      a.state === "ready" ||
      (a.state === "producing" && !!a.ready_at && new Date(a.ready_at).getTime() <= now)
    )
      return true;
    if (a.state === "hungry") {
      const k = view.animal_kinds?.find((x) => x.id === a.kind);
      if (s.buy_feed || (k && (stock[k.feed] ?? 0) >= k.feed_qty)) return true;
    }
  }
  if ((view.trees ?? []).some((t) => treeLive(t, now).ready)) return true;
  if ((view.jobs ?? []).some((j) => new Date(j.ready_at).getTime() <= now)) return true;
  const recipes = view.recipes ?? [];
  for (const b of ["kitchen", "juicer"] as const) {
    const id = b === "kitchen" ? s.kitchen_recipe : s.juicer_recipe;
    const bld = view.buildings?.find((x) => x.id === b);
    const r = id ? recipes.find((x) => x.id === id && (x.building ?? "kitchen") === b) : undefined;
    if (!bld?.built || !r) continue;
    const used = (view.jobs ?? []).filter((j) => (j.building ?? "kitchen") === b).length;
    if (bld.cap - used > 0 && r.inputs.every((i) => (stock[i.item] ?? 0) >= i.qty)) return true;
  }
  return false;
}

/** Số ô trống (đã mở, chưa gieo) cho nút "Gieo tất cả". */
export function emptyPlots(view: FarmView, now: number): number {
  return view.plots.filter((p) => p.index < view.plot_count && liveState(p, now).state === "empty").length;
}

/** Số ô của mình có sâu đã lộ, cho nút "Bắt sâu tất cả". */
export function buggyPlots(view: FarmView): number {
  return view.plots.filter((p) => p.index < view.plot_count && p.bug).length;
}

/** Số ô của mình đang lớn và còn tưới được, cho nút "Tưới tất cả". */
export function waterablePlots(view: FarmView, now: number): number {
  return view.plots.filter(
    (p) => p.index < view.plot_count && p.can_water && liveState(p, now).state === "growing",
  ).length;
}

/** Lời báo một lượt tự động, chỉ liệt kê phần khác 0. */
export function autoSummary(a: NonNullable<NonNullable<FarmView["gained"]>["auto"]>, prefix: string): string {
  const parts: string[] = [];
  if (a.bugs) parts.push(`bắt ${a.bugs} sâu`);
  if (a.harvested) parts.push(`thu ${a.harvested} ô`);
  if (a.planted) parts.push(`gieo ${a.planted}`);
  if (a.watered) parts.push(`tưới ${a.watered} ô`);
  if (a.fed) parts.push(`cho ${a.fed} con ăn`);
  if (a.fruits) parts.push(`hái ${a.fruits} quả`);
  if (a.collected) parts.push(`thu ${a.collected} món`);
  if (a.crafted) parts.push(`nấu ${a.crafted} món`);
  let t = parts.join(", ");
  if (a.spent > 0) t += `${t ? ", " : ""}tiêu ${a.spent} xu`;
  t = t || "Xong";
  return `${prefix} ${t.charAt(0).toUpperCase()}${t.slice(1)}`;
}
