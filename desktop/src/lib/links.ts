/** Tách link http(s) trong nội dung tin. */

const URL_RE = /\bhttps?:\/\/[^\s<>"'`]+/gi;

/** Bỏ dấu câu dính ở cuối link ("xem https://a.vn/x." -> "https://a.vn/x"); giữ ")" nếu link có "(" đi kèm. */
function trimTrailing(url: string): string {
  let u = url.replace(/[.,;:!?'"…]+$/, "");
  while (u.endsWith(")") && (u.match(/\(/g)?.length ?? 0) < (u.match(/\)/g)?.length ?? 0)) u = u.slice(0, -1);
  return u.replace(/[.,;:!?'"…]+$/, "");
}

export type TextPart = { kind: "text"; text: string } | { kind: "link"; url: string };

export function splitLinks(content: string): TextPart[] {
  const parts: TextPart[] = [];
  let last = 0;
  for (const m of content.matchAll(URL_RE)) {
    const url = trimTrailing(m[0]);
    const start = m.index!;
    if (start > last) parts.push({ kind: "text", text: content.slice(last, start) });
    parts.push({ kind: "link", url });
    last = start + url.length;
  }
  if (last < content.length) parts.push({ kind: "text", text: content.slice(last) });
  return parts;
}

/** Các link khác nhau trong tin (tối đa max), để hiện thẻ xem trước. */
export function extractLinks(content: string, max = 3): string[] {
  const out: string[] = [];
  for (const p of splitLinks(content)) {
    if (p.kind === "link" && !out.includes(p.url)) out.push(p.url);
    if (out.length >= max) break;
  }
  return out;
}
