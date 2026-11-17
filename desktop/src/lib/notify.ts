import { isTauri } from "./platform";
import { load, save } from "./storage";

/** Người dùng bật/tắt trong Cài đặt > Ứng dụng. Mặc định bật. */
export const getNotifyPref = () => load<boolean>("notifyMentions", true);
export const setNotifyPref = (v: boolean) => save("notifyMentions", v);

/**
 * Thông báo hệ thống có dùng được không:
 *  - app desktop (Tauri): luôn được (plugin notification),
 *  - trình duyệt: chỉ trên https/localhost (Notification API cần "secure context").
 *    Mở bản web qua http://<IP LAN> thì không có, vẫn còn tô sáng + badge trong app.
 */
export function notificationsSupported() {
  return isTauri || ("Notification" in window && window.isSecureContext);
}

/** Xin quyền hiện thông báo; trả về true nếu được phép. */
export async function ensureNotifyPermission(): Promise<boolean> {
  try {
    if (isTauri) {
      const n = await import("@tauri-apps/plugin-notification");
      if (await n.isPermissionGranted()) return true;
      return (await n.requestPermission()) === "granted";
    }
    if (!notificationsSupported()) return false;
    if (Notification.permission === "granted") return true;
    if (Notification.permission === "denied") return false;
    return (await Notification.requestPermission()) === "granted";
  } catch (err) {
    console.warn("không xin được quyền thông báo:", err);
    return false;
  }
}

/** Hiện thông báo hệ thống. onClick chỉ hoạt động trên trình duyệt. */
export async function showNotification(title: string, body: string, onClick?: () => void) {
  if (!getNotifyPref() || !(await ensureNotifyPermission())) return;
  try {
    if (isTauri) {
      const n = await import("@tauri-apps/plugin-notification");
      n.sendNotification({ title, body });
      return;
    }
    const notif = new Notification(title, { body, icon: "/favicon.svg", tag: title });
    notif.onclick = () => {
      window.focus();
      onClick?.();
      notif.close();
    };
  } catch (err) {
    console.warn("không hiện được thông báo:", err);
  }
}

/** Mở link bằng trình duyệt mặc định (app desktop) hoặc tab mới (bản web). */
export async function openExternal(url: string) {
  if (isTauri) {
    const { openUrl } = await import("@tauri-apps/plugin-opener");
    await openUrl(url);
    return;
  }
  window.open(url, "_blank", "noopener");
}
