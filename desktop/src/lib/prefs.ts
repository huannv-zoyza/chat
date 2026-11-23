/**
 * Cấu hình giao diện riêng của người dùng, lưu trên máy chủ (bảng user_prefs) để dùng chung mọi
 * thiết bị. Có bản sao trong localStorage (theo từng tài khoản) để mở app là áp dụng ngay, không
 * chờ mạng; tải xong từ máy chủ thì bản máy chủ thắng.
 */
import { useSyncExternalStore } from "react";
import type { Api } from "./api";
import { load, save } from "./storage";

export type Prefs = {
  /** Độ rộng khung luồng trên máy tính (px). Không có = mặc định của giao diện. */
  thread_width?: number;
};

/** Giới hạn khớp với máy chủ (controller/prefs.go). */
export const THREAD_WIDTH_MIN = 320;
export const THREAD_WIDTH_MAX = 900;

let userId: number | null = null;
let prefs: Prefs = {};
const listeners = new Set<() => void>();
let saveTimer: number | undefined;
let pending: Partial<Record<keyof Prefs, number | null>> = {};

const key = () => `prefs:${userId}`;

function set(next: Prefs) {
  prefs = next;
  if (userId !== null) save(key(), prefs);
  listeners.forEach((fn) => fn());
}

/** Gọi khi đăng nhập xong: lấy bản đã lưu trên máy rồi đồng bộ với máy chủ. */
export function initPrefs(api: Api, uid: number) {
  if (userId !== uid) {
    userId = uid;
    pending = {};
    set(load<Prefs>(key(), {}));
  }
  api
    .getPrefs()
    .then((p) => {
      // thay đổi chưa kịp gửi lên vẫn giữ (vd. vừa kéo xong thì bản tải về tới)
      const merged: Prefs = { ...p };
      for (const [k, v] of Object.entries(pending)) {
        if (v === null) delete merged[k as keyof Prefs];
        else merged[k as keyof Prefs] = v;
      }
      set(merged);
    })
    .catch(() => {
      /* offline: dùng bản trên máy */
    });
}

/** Đổi một/vài khoá; gửi lên máy chủ sau 400 ms (gộp các lần đổi liên tiếp). null = về mặc định. */
export function setPrefs(api: Api, patch: Partial<Record<keyof Prefs, number | null>>) {
  const next: Prefs = { ...prefs };
  for (const [k, v] of Object.entries(patch)) {
    if (v === null) delete next[k as keyof Prefs];
    else next[k as keyof Prefs] = v;
  }
  set(next);
  pending = { ...pending, ...patch };
  window.clearTimeout(saveTimer);
  saveTimer = window.setTimeout(() => {
    const body = pending;
    pending = {};
    api.updatePrefs(body).catch(() => {
      /* lỗi mạng: lần sau đổi sẽ gửi lại; bản trên máy vẫn đúng */
    });
  }, 400);
}

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => listeners.delete(fn);
};

export function usePrefs(): Prefs {
  return useSyncExternalStore(subscribe, () => prefs);
}
