import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type TouchEvent } from "react";
import type { Message } from "../lib/api";
import { copyText, displayName, formatDay, formatTime } from "../lib/format";
import { COMPACT_QUERY, useMediaQuery } from "../lib/useMedia";
import { useChat } from "../state/ChatProvider";
import { EmojiPicker, rememberEmoji } from "./EmojiPicker";
import { Avatar, Icon } from "./ui";

/** Cảm xúc hay dùng, hiện sẵn khi rê chuột/nhấn giữ tin nhắn. */
export const QUICK_REACTIONS = ["👍", "❤️", "😂", "😮", "😢", "🎉"];

/* ---------- Chip cảm xúc dưới tin ---------- */

export function ReactionBar({ message, onAdd }: { message: Message; onAdd: (anchor: HTMLElement) => void }) {
  const { state, toggleReaction } = useChat();
  const reactions = message.reactions ?? [];
  if (reactions.length === 0) return null;

  return (
    <div className="reactions" role="group" aria-label="Cảm xúc">
      {reactions.map((r) => {
        const mine = r.user_ids.includes(state.me.id);
        const names = r.user_ids.map((id) => (id === state.me.id ? "Bạn" : displayName(state.users[id])));
        return (
          <button
            key={r.emoji}
            type="button"
            className={`reaction${mine ? " is-mine" : ""}`}
            aria-pressed={mine}
            title={`${names.join(", ")} đã thả ${r.emoji}`}
            aria-label={`${r.emoji} ${r.user_ids.length}, ${mine ? "bấm để bỏ" : "bấm để thả"}`}
            onClick={() => toggleReaction(message.id, r.emoji)}
          >
            <span className="reaction-emoji">{r.emoji}</span>
            <span className="reaction-count">{r.user_ids.length}</span>
          </button>
        );
      })}
      <button
        type="button"
        className="reaction reaction-add"
        aria-label="Thả cảm xúc khác"
        title="Thả cảm xúc khác"
        onClick={(e) => onAdd(e.currentTarget)}
      >
        <Icon name="smilePlus" size={16} />
      </button>
    </div>
  );
}

/* ---------- Tóm tắt luồng dưới tin gốc ---------- */

export function ThreadSummaryLink({ message, onOpen }: { message: Message; onOpen: () => void }) {
  const { state } = useChat();
  const t = message.thread;
  if (!t || t.reply_count === 0) return null;
  const last = formatDay(t.last_reply_at) === "Hôm nay" ? formatTime(t.last_reply_at) : formatDay(t.last_reply_at);

  return (
    <button type="button" className="thread-summary" onClick={onOpen}>
      <span className="thread-avatars" aria-hidden>
        {t.participant_ids.map((id) => (
          <Avatar key={id} user={state.users[id]} size={20} />
        ))}
      </span>
      <span className="thread-count">{t.reply_count} trả lời</span>
      <span className="thread-last">Trả lời gần nhất {last}</span>
      <span className="thread-open-hint">Xem luồng</span>
    </button>
  );
}

/* ---------- Thanh thao tác khi rê chuột (máy tính) ---------- */

type ToolbarProps = {
  message: Message;
  onPick: (anchor: HTMLElement) => void;
  onReply?: () => void;
};

export function MessageToolbar({ message, onPick, onReply }: ToolbarProps) {
  const { toggleReaction } = useChat();
  return (
    <div className="msg-toolbar" role="toolbar" aria-label="Thao tác với tin nhắn">
      {QUICK_REACTIONS.slice(0, 4).map((e) => (
        <button
          key={e}
          type="button"
          className="toolbar-btn toolbar-emoji"
          title={`Thả ${e}`}
          aria-label={`Thả ${e}`}
          onClick={() => toggleReaction(message.id, e)}
        >
          {e}
        </button>
      ))}
      <button
        type="button"
        className="toolbar-btn"
        title="Thả cảm xúc khác"
        aria-label="Thả cảm xúc khác"
        onClick={(ev) => onPick(ev.currentTarget)}
      >
        <Icon name="smilePlus" size={17} />
      </button>
      {onReply && (
        <button type="button" className="toolbar-btn" title="Trả lời trong luồng" aria-label="Trả lời trong luồng" onClick={onReply}>
          <Icon name="thread" size={17} />
        </button>
      )}
    </div>
  );
}

/* ---------- Bảng chọn cảm xúc nổi, đặt cạnh nút vừa bấm ---------- */

export function ReactionPicker({
  messageId,
  anchor,
  onClose,
}: {
  messageId: number;
  anchor: HTMLElement;
  onClose: () => void;
}) {
  const { toggleReaction } = useChat();
  const compact = useMediaQuery(COMPACT_QUERY);
  const [style, setStyle] = useState<CSSProperties>({ visibility: "hidden" });
  const ref = useRef<HTMLDivElement>(null);

  // Đặt phía trên nút nếu đủ chỗ, không thì phía dưới; luôn nằm trong màn hình.
  useLayoutEffect(() => {
    if (compact) return setStyle({});
    const el = ref.current?.firstElementChild as HTMLElement | null;
    const r = anchor.getBoundingClientRect();
    const w = el?.offsetWidth ?? 328;
    const h = el?.offsetHeight ?? 320;
    const left = Math.max(8, Math.min(r.right - w, window.innerWidth - w - 8));
    const top = r.top - h - 6 > 8 ? r.top - h - 6 : Math.min(r.bottom + 6, window.innerHeight - h - 8);
    setStyle({ top, left });
  }, [anchor, compact]);

  return (
    <div ref={ref}>
      <EmojiPicker
        className="is-floating"
        style={style}
        onPick={(e) => {
          toggleReaction(messageId, e);
          onClose();
        }}
        onClose={onClose}
      />
    </div>
  );
}

/* ---------- Nhấn giữ (điện thoại) ---------- */

/** Trả về các handler touch: giữ ~450ms mà không kéo thì gọi onLongPress. */
export function useLongPress(onLongPress: () => void, enabled: boolean) {
  const timer = useRef<number | undefined>(undefined);
  const start = useRef<{ x: number; y: number } | null>(null);
  const fired = useRef(false);
  const cancel = () => {
    window.clearTimeout(timer.current);
    start.current = null;
  };
  useEffect(() => cancel, []);
  if (!enabled) return {};
  return {
    onTouchStart: (e: TouchEvent) => {
      const t = e.touches[0];
      start.current = { x: t.clientX, y: t.clientY };
      fired.current = false;
      timer.current = window.setTimeout(() => {
        start.current = null;
        fired.current = true;
        navigator.vibrate?.(10);
        onLongPress();
      }, 450);
    },
    onTouchMove: (e: TouchEvent) => {
      const t = e.touches[0];
      if (start.current && Math.hypot(t.clientX - start.current.x, t.clientY - start.current.y) > 10) cancel();
    },
    onTouchEnd: (e: TouchEvent) => {
      cancel();
      // Nhấc tay sau khi đã mở bảng thao tác: chặn cú click trình duyệt tự sinh,
      // nếu không nó rơi vào nền mờ và đóng bảng ngay lập tức.
      if (fired.current) e.preventDefault();
    },
    onTouchCancel: cancel,
    // Android mở menu chuột phải khi nhấn giữ: chặn để không đè lên bảng thao tác.
    onContextMenu: (e: React.MouseEvent) => e.preventDefault(),
  };
}

/** Bảng thao tác trượt lên khi nhấn giữ tin trên điện thoại. */
export function MessageActionSheet({
  message,
  onClose,
  onReply,
}: {
  message: Message;
  onClose: () => void;
  onReply?: () => void;
}) {
  const { toggleReaction, state } = useChat();
  const [picking, setPicking] = useState(false);
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);

  const react = (e: string) => {
    rememberEmoji(e);
    toggleReaction(message.id, e);
    onClose();
  };
  const mine = (e: string) => !!message.reactions?.find((r) => r.emoji === e)?.user_ids.includes(state.me.id);

  return (
    <dialog
      ref={ref}
      className="dialog action-sheet"
      aria-label="Thao tác với tin nhắn"
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => e.target === ref.current && onClose()}
    >
      <div className="dialog-body">
        {picking ? (
          <EmojiPicker className="is-inline" onPick={react} onClose={() => setPicking(false)} />
        ) : (
          <>
            <div className="sheet-reactions">
              {QUICK_REACTIONS.map((e) => (
                <button
                  key={e}
                  type="button"
                  className={`sheet-emoji${mine(e) ? " is-mine" : ""}`}
                  aria-label={`Thả ${e}`}
                  onClick={() => react(e)}
                >
                  {e}
                </button>
              ))}
              <button type="button" className="sheet-emoji" aria-label="Cảm xúc khác" onClick={() => setPicking(true)}>
                <Icon name="smilePlus" size={22} />
              </button>
            </div>
            <div className="sheet-actions">
              {onReply && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    onClose();
                    onReply();
                  }}
                >
                  <Icon name="thread" />
                  Trả lời trong luồng
                </button>
              )}
              {message.content && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    void copyText(message.content);
                    onClose();
                  }}
                >
                  <Icon name="copy" />
                  Sao chép nội dung
                </button>
              )}
              <button type="button" className="sheet-action" onClick={onClose}>
                <Icon name="x" />
                Đóng
              </button>
            </div>
          </>
        )}
      </div>
    </dialog>
  );
}
