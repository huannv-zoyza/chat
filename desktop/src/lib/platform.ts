/** Đang chạy trong app desktop Tauri. */
export const isTauri = "__TAURI_INTERNALS__" in window;

/**
 * Giao diện web do chính server Go phục vụ (mở http://<ip>:<port>/ trên trình duyệt):
 * máy chủ luôn là trang đang mở, người dùng không cần (và không nên) đổi.
 * Khi chạy `pnpm dev` (Vite, cổng 1420) thì vẫn cho nhập máy chủ.
 */
export const servedByServer = !isTauri && !import.meta.env.DEV;

const INVITE_KEY = "pendingInvite";

/**
 * Bản web mở bằng link mời (…/invite/<mã>): ghi nhớ mã (kể cả khi phải đăng nhập/đăng ký trước)
 * và đưa thanh địa chỉ về "/" để tải lại trang không mở hộp thoại lần nữa.
 */
export function captureInviteFromUrl() {
  const m = window.location.pathname.match(/^\/invite\/([0-9a-f]{24})\/?$/i);
  if (!m) return;
  try {
    sessionStorage.setItem(INVITE_KEY, m[1].toLowerCase());
  } catch {
    /* bỏ qua */
  }
  window.history.replaceState(null, "", "/");
}

/** Mã mời đang chờ, nếu có (chỉ đọc, không xoá: an toàn khi React gọi khởi tạo 2 lần). */
export function peekPendingInvite(): string | null {
  try {
    return sessionStorage.getItem(INVITE_KEY);
  } catch {
    return null;
  }
}

export function clearPendingInvite() {
  try {
    sessionStorage.removeItem(INVITE_KEY);
  } catch {
    /* bỏ qua */
  }
}

const MESSAGE_KEY = "pendingMessage";

/** Bản web mở bằng link tới tin (…/m/<id>): ghi nhớ id (kể cả khi phải đăng nhập trước) rồi đưa địa chỉ về "/". */
export function captureMessageFromUrl() {
  const m = window.location.pathname.match(/^\/m\/(\d{1,15})\/?$/);
  if (!m) return;
  try {
    sessionStorage.setItem(MESSAGE_KEY, m[1]);
  } catch {
    /* bỏ qua */
  }
  window.history.replaceState(null, "", "/");
}

/** Lấy (và xoá) id tin đang chờ mở. */
export function takePendingMessage(): number | null {
  try {
    const v = sessionStorage.getItem(MESSAGE_KEY);
    sessionStorage.removeItem(MESSAGE_KEY);
    return v ? Number(v) : null;
  } catch {
    return null;
  }
}
