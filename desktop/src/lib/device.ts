// Mã máy: chuỗi ngẫu nhiên sinh một lần rồi lưu trong máy, gửi kèm mọi request (header X-Device-ID).
// Server dùng để giới hạn số tài khoản tạo trên một máy và chặn chuyển xu giữa các tài khoản dùng
// chung máy (chống tạo tài khoản ảo gom xu). Không chứa thông tin gì về máy.
import { load, save } from "./storage";

const KEY = "deviceId";
let cached: string | null = null;

function randomId(): string {
  const bytes = new Uint8Array(16);
  // crypto.getRandomValues chạy được cả khi mở qua http trong LAN (randomUUID thì không).
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

export function deviceId(): string {
  if (cached) return cached;
  const stored = load<string | null>(KEY, null);
  if (typeof stored === "string" && /^[A-Za-z0-9-]{16,64}$/.test(stored)) {
    cached = stored;
  } else {
    cached = randomId();
    save(KEY, cached);
  }
  return cached;
}
