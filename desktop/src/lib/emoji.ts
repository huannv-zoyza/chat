/**
 * Hình emoji vẽ sẵn (Microsoft Fluent Emoji 3D, MIT) cho nông trại: emoji hệ thống mỗi máy vẽ
 * một kiểu (iPhone vẽ 🪙 màu bạc trông như cục đá), nên nông trại dùng chung một bộ hình.
 * Tải thêm hình: scripts/fetch-farm-emoji.py. Emoji chưa có hình thì vẫn hiện chữ như cũ.
 */
const files = import.meta.glob("../assets/emoji/*.webp", {
  eager: true,
  query: "?url",
  import: "default",
}) as Record<string, string>;

const URLS = new Map<string, string>();
for (const [path, url] of Object.entries(files)) {
  URLS.set(path.slice(path.lastIndexOf("/") + 1, -".webp".length), url);
}

/** Mã tệp: các codepoint hex nối "-", bỏ FE0F (giống scripts/fetch-farm-emoji.py). */
function code(e: string): string {
  return [...e]
    .filter((c) => c !== "\ufe0f")
    .map((c) => c.codePointAt(0)!.toString(16))
    .join("-");
}

/** Đường dẫn hình của emoji (null nếu chưa có hình). */
export function emojiUrl(e: string | undefined | null): string | null {
  if (!e) return null;
  return URLS.get(code(e.trim())) ?? null;
}

/** Tách chữ thành đoạn chữ thường và emoji (giữ nguyên thứ tự). */
// prettier-ignore
export const EMOJI_RE = /\p{Extended_Pictographic}\ufe0f?(?:\u200d\p{Extended_Pictographic}\ufe0f?)*/gu;
export function splitEmoji(text: string): { text: string; emoji: boolean }[] {
  const out: { text: string; emoji: boolean }[] = [];
  let last = 0;
  for (const m of text.matchAll(EMOJI_RE)) {
    if (m.index! > last)
      out.push({ text: text.slice(last, m.index), emoji: false });
    out.push({ text: m[0], emoji: true });
    last = m.index! + m[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last), emoji: false });
  return out;
}
