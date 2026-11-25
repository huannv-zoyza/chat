// Màn hình làng Ma Sói đang mở (id tin chứa ván), dùng chung giữa thẻ game và Workspace.
import { useSyncExternalStore } from "react";

let openId: number | null = null;
const subs = new Set<() => void>();
const emit = () => subs.forEach((f) => f());

export const village = {
  open(messageId: number) {
    if (openId === messageId) return;
    openId = messageId;
    emit();
  },
  close() {
    if (openId === null) return;
    openId = null;
    emit();
  },
  subscribe(f: () => void) {
    subs.add(f);
    return () => {
      subs.delete(f);
    };
  },
  get: () => openId,
};

export function useVillageId() {
  return useSyncExternalStore(village.subscribe, village.get);
}

// Mỗi ván chỉ tự mở làng một lần khi vừa bắt đầu (đóng rồi thì thôi, bấm "Vào làng" để mở lại).
const autoOpened = new Set<number>();
export function autoOpenVillage(messageId: number) {
  if (autoOpened.has(messageId)) return;
  autoOpened.add(messageId);
  village.open(messageId);
}
