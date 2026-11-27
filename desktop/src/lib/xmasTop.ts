import { useEffect, useState } from "react";
import { farmXmasEvents, type XmasTopRow } from "./farm";

/*
 * Top kẹo Noel (huy hiệu 🎄 cạnh tên trong chat). Bộ nhớ đệm chung cho mọi dòng tin: tải lại sau 5 phút
 * hoặc khi có tin Giáng sinh (farm.xmas). Ngoài sự kiện máy chủ trả mảng rỗng nên không hiện gì.
 */

const TTL = 5 * 60_000;
let cache: { at: number; ids: Set<number> } | null = null;
let loading = false;
const subs = new Set<() => void>();

function refresh(load: () => Promise<XmasTopRow[]>) {
  if (loading) return;
  loading = true;
  load()
    .then((rows) => {
      cache = { at: Date.now(), ids: new Set(rows.map((r) => r.user.id)) };
      subs.forEach((f) => f());
    })
    .catch(() => {
      cache = { at: Date.now(), ids: cache?.ids ?? new Set() }; // lỗi: thử lại sau TTL
    })
    .finally(() => {
      loading = false;
    });
}

farmXmasEvents.on(() => {
  if (cache) cache.at = 0;
});

export function useXmasTop(load: () => Promise<XmasTopRow[]>): Set<number> {
  const [, rerender] = useState(0);
  useEffect(() => {
    const f = () => rerender((n) => n + 1);
    subs.add(f);
    if (!cache || Date.now() - cache.at > TTL) refresh(load);
    return () => {
      subs.delete(f);
    };
  }, [load]);
  return cache?.ids ?? EMPTY;
}

const EMPTY = new Set<number>();
