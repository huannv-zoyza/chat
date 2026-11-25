import type { CSSProperties, ReactNode } from "react";
import { ShareCard } from "./Share";
import type { Embed } from "../lib/api";
import { findSticker, stickerLabel, type Sticker } from "../lib/stickers";
import { CallLog } from "./CallOverlay";
import { useChat } from "../state/ChatProvider";
import { displayName } from "../lib/format";
import { throwItem } from "../lib/fun";

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
export function EmbedView({
  embed,
  channelId,
  userId,
  messageId,
  actor,
}: {
  embed: Embed;
  channelId?: number;
  userId?: number;
  messageId?: number;
  /** Tên người làm, hiện đầu dòng trêu nhau (dòng đó không có tiêu đề riêng). */
  actor?: ReactNode;
}) {
  if (embed.type === "throw" || embed.type === "nickname")
    return <FunLine embed={embed} messageId={messageId} actor={actor} />;
  if (embed.type === "share" && /^\d+$/.test(embed.id)) return <ShareCard id={Number(embed.id)} embed={embed} />;
  if (embed.type === "call") {
    return (
      <CallLog
        outcome={embed.id}
        video={embed.video}
        duration={embed.duration}
        channelId={channelId ?? 0}
        callerId={userId ?? 0}
        participants={embed.participants}
      />
    );
  }
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

/** Dòng trêu nhau: "🩴 ném dép vào Nam", "đặt biệt danh cho Nam là “Thánh Lag”". */
function FunLine({ embed, messageId, actor }: { embed: Embed; messageId?: number; actor?: ReactNode }) {
  const { state, dispatch } = useChat();
  const target = embed.target_id ? state.users[embed.target_id] : undefined;
  const isMe = embed.target_id === state.me.id;
  const name = isMe ? "bạn" : target ? displayName(target) : embed.author || "ai đó";
  if (embed.type === "throw") {
    const item = throwItem(embed.id);
    // lượt ném vừa xảy ra khi đang mở kênh: món đồ bay vào dòng này rồi bẹp dí
    const fresh = messageId !== undefined && !!state.freshThrows[messageId];
    return (
      <p className={`msg-fun${fresh ? ` is-fresh throw-${embed.id}` : ""}`}>
        <span
          className="msg-fun-icon"
          aria-hidden
          onAnimationEnd={(ev) => {
            if (fresh && ev.animationName === "fun-splat") dispatch({ type: "throw.done", messageId: messageId! });
          }}
        >
          {item?.icon ?? "🎯"}
        </span>
        {actor}
        {actor && " "}ném {item?.name ?? "đồ"} vào <strong>{name}</strong>
      </p>
    );
  }
  const nick = embed.text;
  return (
    <p className="msg-fun">
      <span className="msg-fun-icon" aria-hidden>
        🏷️
      </span>
      {nick ? (
        <>
          {actor}
          {actor && " "}đặt biệt danh cho <strong>{name}</strong> là “{nick}”
        </>
      ) : (
        <>
          {actor}
          {actor && " "}xoá biệt danh của <strong>{name}</strong>
        </>
      )}
    </p>
  );
}

/** Mô tả ngắn cho thông báo / xem trước: "đã gửi một sticker", ... */
export function embedSummary(embed: Embed): string {
  if (embed.type === "throw") return `${throwItem(embed.id)?.icon ?? "🎯"} ném ${throwItem(embed.id)?.name ?? "đồ"} vào ${embed.author ?? "ai đó"}`;
  if (embed.type === "nickname")
    return embed.text ? `🏷️ đặt biệt danh cho ${embed.author} là “${embed.text}”` : `🏷️ xoá biệt danh của ${embed.author}`;
  if (embed.type === "game") return `🎮 ${embed.title ?? "Trò chơi"}`;
  if (embed.type === "share") return `↗ Đã chia sẻ: ${embed.text || embed.title || "một tin nhắn"}`;
  if (embed.type === "call") return embed.id === "completed" ? "📞 Cuộc gọi" : "📞 Cuộc gọi nhỡ";
  if (embed.type === "gif") return "Đã gửi một GIF";
  const s = findSticker(embed.id);
  return s ? `Đã gửi sticker “${stickerLabel(s)}”` : "Đã gửi một sticker";
}
