import type { PublicUser } from "./api";

const timeFmt = new Intl.DateTimeFormat("vi-VN", { hour: "2-digit", minute: "2-digit" });
const dayFmt = new Intl.DateTimeFormat("vi-VN", { weekday: "long", day: "numeric", month: "long" });
const dayYearFmt = new Intl.DateTimeFormat("vi-VN", { day: "numeric", month: "long", year: "numeric" });
const fullFmt = new Intl.DateTimeFormat("vi-VN", { dateStyle: "full", timeStyle: "short" });

export const formatTime = (iso: string) => timeFmt.format(new Date(iso));
export const formatFull = (iso: string) => fullFmt.format(new Date(iso));

export function dayKey(iso: string) {
  const d = new Date(iso);
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}

/** "Hôm nay", "Hôm qua", "Thứ Hai, 16 tháng 11", hoặc kèm năm nếu khác năm nay. */
export function formatDay(iso: string) {
  const d = new Date(iso);
  const now = new Date();
  const startOf = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((startOf(now) - startOf(d)) / 86_400_000);
  if (diff === 0) return "Hôm nay";
  if (diff === 1) return "Hôm qua";
  const s = d.getFullYear() === now.getFullYear() ? dayFmt.format(d) : dayYearFmt.format(d);
  return s.charAt(0).toUpperCase() + s.slice(1);
}

export function displayName(u: Pick<PublicUser, "display_name" | "username"> | undefined) {
  if (!u) return "Người dùng đã rời";
  return u.display_name || u.username;
}

export function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  const first = [...parts[0]][0] ?? "";
  const last = parts.length > 1 ? ([...parts[parts.length - 1]][0] ?? "") : "";
  return (first + last).toUpperCase();
}

/** Mỗi người một màu cố định (theo id) để dễ nhận ra trong cuộc trò chuyện. */
export function userHue(id: number) {
  return `var(--u${(id % 6) + 1})`;
}

/**
 * Id ngẫu nhiên để khớp tin đang gửi với tin server xác nhận.
 * Không dùng crypto.randomUUID: hàm đó chỉ có trong secure context (https/localhost),
 * nên sẽ lỗi khi mở bản web qua http://<IP LAN>. getRandomValues thì có ở mọi nơi.
 */
export function newClientId() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * Sao chép chữ vào clipboard. navigator.clipboard chỉ có trên https/localhost,
 * nên mở bản web qua http://<IP LAN> phải dùng cách cũ (execCommand).
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* thử cách dưới */
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  ta.remove();
  return ok;
}
