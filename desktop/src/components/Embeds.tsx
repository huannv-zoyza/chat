import type { CSSProperties } from "react";
import type { Embed } from "../lib/api";
import { findSticker, stickerLabel, type Sticker } from "../lib/stickers";

/** Một sticker. size: "msg" (trong tin nhắn) hoặc "pick" (ô trong bảng chọn). */
export function StickerView({ sticker, size = "msg" }: { sticker: Sticker; size?: "msg" | "pick" }) {
  if (sticker.kind === "emoji") {
    return (
      <span className={`sticker sticker-emoji sticker-${size}`} role="img" aria-label={`Sticker ${sticker.label}`}>
        {sticker.emoji}
      </span>
    );
  }
  const style = {
    "--st-bg": sticker.bg,
    "--st-fg": sticker.fg ?? "#fff",
    "--st-tilt": `${sticker.tilt}deg`,
  } as CSSProperties;
  return (
    <span
      className={`sticker sticker-text sticker-${size}`}
      style={style}
      role="img"
      aria-label={`Sticker ${sticker.text}`}
    >
      <span className="sticker-text-emoji" aria-hidden>
        {sticker.emoji}
      </span>
      <span className="sticker-text-label" aria-hidden>
        {sticker.text}
      </span>
    </span>
  );
}

const GIF_MAX_W = 320;
const GIF_MAX_H = 240;

/** Sticker hoặc GIF trong tin nhắn. */
export function EmbedView({ embed }: { embed: Embed }) {
  if (embed.type === "sticker") {
    const s = findSticker(embed.id);
    if (!s) return <p className="msg-deleted">Sticker không còn trong bộ của app.</p>;
    return (
      <div className="msg-sticker" title={stickerLabel(s)}>
        <StickerView sticker={s} />
      </div>
    );
  }
  if (embed.type === "gif" && embed.url) {
    const w = embed.width ?? 200;
    const h = embed.height ?? 200;
    const scale = Math.min(1, GIF_MAX_W / w, GIF_MAX_H / h);
    return (
      <figure className="msg-gif" style={{ width: Math.round(w * scale), aspectRatio: `${w} / ${h}` }}>
        <img src={embed.url} alt={embed.title || "GIF"} loading="lazy" referrerPolicy="no-referrer" />
        <span className="gif-badge" aria-hidden>
          GIF
        </span>
      </figure>
    );
  }
  return null;
}

/** Mô tả ngắn cho thông báo / xem trước: "đã gửi một sticker", ... */
export function embedSummary(embed: Embed): string {
  if (embed.type === "gif") return "Đã gửi một GIF";
  const s = findSticker(embed.id);
  return s ? `Đã gửi sticker “${stickerLabel(s)}”` : "Đã gửi một sticker";
}
