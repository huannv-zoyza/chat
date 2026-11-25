import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type TouchEvent } from "react";
import type { Message, Reaction } from "../lib/api";
import { copyText, displayName, formatDay, formatTime } from "../lib/format";
import { COMPACT_QUERY, TOUCH_QUERY, useMediaQuery } from "../lib/useMedia";
import { findMessage } from "../state/chat";
import { useChat } from "../state/ChatProvider";
import { EmojiPicker, rememberEmoji } from "./EmojiPicker";
import { Avatar, Dialog, Icon } from "./ui";
import { useSheetDrag } from "../lib/sheet";

/** Cảm xúc hay dùng, hiện sẵn khi rê chuột/nhấn giữ tin nhắn. */
export const QUICK_REACTIONS = ["👍", "❤️", "😂", "😮", "😢", "🎉"];

/* ---------- Chip cảm xúc dưới tin ---------- */

export function ReactionBar({ message, onAdd }: { message: Message; onAdd: (anchor: HTMLElement) => void }) {
  const { state } = useChat();
  const [who, setWho] = useState<string | null>(null);
  const reactions = message.reactions ?? [];
  if (reactions.length === 0) return null;

  return (
    <div className="reactions" role="group" aria-label="Cảm xúc">
      {reactions.map((r) => (
        <ReactionChip key={r.emoji} message={message} reaction={r} meId={state.me.id} onShowWho={setWho} />
      ))}
      {who && <ReactionsDialog message={message} initial={who} onClose={() => setWho(null)} />}
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

/**
 * Một nút cảm xúc: chạm/bấm để thả hoặc gỡ. Xem ai đã thả: rê chuột (máy tính),
 * nhấn giữ (điện thoại) hoặc chuột phải.
 */
function ReactionChip({
  message,
  reaction: r,
  meId,
  onShowWho,
}: {
  message: Message;
  reaction: Reaction;
  meId: number;
  onShowWho: (emoji: string) => void;
}) {
  const { state, toggleReaction } = useChat();
  const touch = useMediaQuery(TOUCH_QUERY);
  const press: Partial<
    Record<"onTouchStart" | "onTouchMove" | "onTouchEnd" | "onTouchCancel", (e: TouchEvent) => void>
  > = useLongPress(() => onShowWho(r.emoji), touch);
  const mine = r.user_ids.includes(meId);
  const names = r.user_ids.map((id) => (id === meId ? "Bạn" : displayName(state.users[id])));
  // Không để nhấn giữ nút cảm xúc lan lên tin nhắn (sẽ mở luôn bảng thao tác của tin).
  const stop =
    <E extends React.SyntheticEvent>(fn?: (e: E) => void) =>
    (e: E) => {
      e.stopPropagation();
      fn?.(e);
    };
  return (
    <button
      type="button"
      className={`reaction${mine ? " is-mine" : ""}`}
      aria-pressed={mine}
      title={`${names.join(", ")} đã thả ${r.emoji}`}
      aria-label={`${r.emoji} ${r.user_ids.length}: ${names.join(", ")}. ${mine ? "Bấm để bỏ" : "Bấm để thả"}, nhấn giữ để xem ai đã thả`}
      onClick={() => toggleReaction(message.id, r.emoji)}
      onTouchStart={stop(press.onTouchStart)}
      onTouchMove={stop(press.onTouchMove)}
      onTouchEnd={stop(press.onTouchEnd)}
      onTouchCancel={stop(press.onTouchCancel)}
      onContextMenu={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onShowWho(r.emoji);
      }}
    >
      <span className="reaction-emoji">{r.emoji}</span>
      <span className="reaction-count">{r.user_ids.length}</span>
    </button>
  );
}

/** Danh sách ai đã thả cảm xúc nào (tab "Tất cả" + từng emoji). Chạm dòng của mình để gỡ. */
export function ReactionsDialog({
  message,
  initial,
  onClose,
}: {
  message: Message;
  initial: string;
  onClose: () => void;
}) {
  const { state, toggleReaction } = useChat();
  const [tab, setTab] = useState(initial);
  // lấy bản mới nhất của tin (cảm xúc có thể đổi trong lúc đang mở)
  const reactions = (findMessage(state, message.id) ?? message).reactions ?? [];
  useEffect(() => {
    if (reactions.length === 0) onClose();
  }, [reactions.length, onClose]);
  const total = reactions.reduce((n, r) => n + r.user_ids.length, 0);
  const rows = reactions
    .filter((r) => tab === "all" || r.emoji === tab)
    .flatMap((r) => r.user_ids.map((uid) => ({ uid, emoji: r.emoji })));
  // mình lên đầu, rồi theo tên
  rows.sort(
    (a, b) =>
      Number(b.uid === state.me.id) - Number(a.uid === state.me.id) ||
      displayName(state.users[a.uid]).localeCompare(displayName(state.users[b.uid]), "vi"),
  );

  return (
    <Dialog title="Cảm xúc" onClose={onClose}>
      <div className="who-tabs" role="tablist" aria-label="Lọc theo cảm xúc">
        <button
          type="button"
          role="tab"
          className="who-tab"
          aria-selected={tab === "all"}
          onClick={() => setTab("all")}
        >
          Tất cả <span>{total}</span>
        </button>
        {reactions.map((r) => (
          <button
            key={r.emoji}
            type="button"
            role="tab"
            className="who-tab"
            aria-selected={tab === r.emoji}
            onClick={() => setTab(r.emoji)}
          >
            <span className="who-tab-emoji">{r.emoji}</span> <span>{r.user_ids.length}</span>
          </button>
        ))}
      </div>
      <ul className="who-list">
        {rows.map(({ uid, emoji }) => {
          const me = uid === state.me.id;
          const user = state.users[uid];
          return (
            <li key={`${uid}-${emoji}`}>
              <button
                type="button"
                className="who-row"
                disabled={!me}
                onClick={() => me && toggleReaction(message.id, emoji)}
                aria-label={me ? `Bạn đã thả ${emoji}, chạm để gỡ` : `${displayName(user)} đã thả ${emoji}`}
              >
                <Avatar user={user} size={34} />
                <span className="who-text">
                  <span className="who-name">{me ? "Bạn" : displayName(user)}</span>
                  {me && <span className="who-sub">Chạm để gỡ</span>}
                </span>
                <span className="who-emoji">{emoji}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </Dialog>
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

/** Các thao tác có thể làm với một tin (thiếu = không được phép / không áp dụng). */
export type MessageActionSet = {
  onThread?: () => void; // trả lời trong luồng
  onQuote?: () => void; // trả lời trích dẫn
  onEdit?: () => void;
  onDelete?: () => void;
  onPin?: () => void;
  pinned?: boolean;
  onForward?: () => void;
  onRemind?: () => void; // nhắc tôi về tin này
};

type ToolbarProps = MessageActionSet & {
  message: Message;
  onPick: (anchor: HTMLElement) => void;
};

export function MessageToolbar({
  message,
  onPick,
  onThread,
  onQuote,
  onEdit,
  onDelete,
  onPin,
  pinned,
  onForward,
  onRemind,
}: ToolbarProps) {
  const { toggleReaction } = useChat();
  const [menu, setMenu] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!menu) return;
    const onDown = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setMenu(false);
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setMenu(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [menu]);
  const item = (label: string, icon: string, fn?: () => void, danger = false) =>
    fn && (
      <button
        type="button"
        role="menuitem"
        className={danger ? "is-danger" : undefined}
        aria-label={label}
        onClick={() => {
          setMenu(false);
          fn();
        }}
      >
        <Icon name={icon} size={16} />
        {label}
      </button>
    );
  const hasMenu = onPin || onForward || onEdit || onDelete || message.content;

  return (
    <div className={`msg-toolbar${menu ? " is-open" : ""}`} role="toolbar" aria-label="Thao tác với tin nhắn" ref={ref}>
      {QUICK_REACTIONS.slice(0, 3).map((e) => (
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
      {onQuote && (
        <button type="button" className="toolbar-btn" title="Trả lời" aria-label="Trả lời" onClick={onQuote}>
          <Icon name="reply" size={17} />
        </button>
      )}
      {onForward && (
        <button type="button" className="toolbar-btn" title="Chia sẻ" aria-label="Chia sẻ" onClick={onForward}>
          <Icon name="forward" size={17} />
        </button>
      )}
      {onThread && (
        <button
          type="button"
          className="toolbar-btn"
          title="Trả lời trong luồng"
          aria-label="Trả lời trong luồng"
          onClick={onThread}
        >
          <Icon name="thread" size={17} />
        </button>
      )}
      {hasMenu && (
        <button
          type="button"
          className="toolbar-btn"
          title="Thêm"
          aria-label="Thao tác khác"
          aria-haspopup="menu"
          aria-expanded={menu}
          onClick={() => setMenu((v) => !v)}
        >
          <Icon name="more" size={17} />
        </button>
      )}
      {menu && (
        <div className="more-menu" role="menu">
          {item(pinned ? "Bỏ ghim" : "Ghim tin nhắn", "pin", onPin)}
          {item("Chia sẻ", "forward", onForward)}
          {item("Nhắc tôi về tin này", "clock", onRemind)}
          {message.content && item("Sao chép nội dung", "copy", () => void copyText(message.content))}
          {item("Sửa tin nhắn", "pencil", onEdit)}
          {item("Xoá tin nhắn", "trash", onDelete, true)}
        </div>
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
  onThread: onReply,
  onQuote,
  onEdit,
  onDelete,
  onPin,
  pinned,
  onForward,
  onRemind,
  onShowReactions,
}: MessageActionSet & {
  message: Message;
  onClose: () => void;
  onShowReactions?: () => void;
}) {
  const { toggleReaction, state } = useChat();
  const [picking, setPicking] = useState(false);
  const ref = useRef<HTMLDialogElement>(null);
  useSheetDrag(ref, onClose);

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
      onTouchStart={(e) => e.stopPropagation()}
      onTouchMove={(e) => e.stopPropagation()}
      onTouchEnd={(e) => e.stopPropagation()}
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
              {onQuote && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    onClose();
                    onQuote();
                  }}
                >
                  <Icon name="reply" />
                  Trả lời
                </button>
              )}
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
              {onForward && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    onClose();
                    onForward();
                  }}
                >
                  <Icon name="forward" />
                  Chia sẻ
                </button>
              )}
              {onRemind && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    onClose();
                    onRemind();
                  }}
                >
                  <Icon name="clock" />
                  Nhắc tôi về tin này
                </button>
              )}
              {onPin && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    onClose();
                    onPin();
                  }}
                >
                  <Icon name="pin" />
                  {pinned ? "Bỏ ghim" : "Ghim tin nhắn"}
                </button>
              )}
              {onShowReactions && (message.reactions?.length ?? 0) > 0 && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    onClose();
                    onShowReactions();
                  }}
                >
                  <Icon name="smilePlus" />
                  Xem ai đã thả cảm xúc
                </button>
              )}
              {onEdit && (
                <button
                  type="button"
                  className="sheet-action"
                  onClick={() => {
                    onClose();
                    onEdit();
                  }}
                >
                  <Icon name="pencil" />
                  Sửa tin nhắn
                </button>
              )}
              {onDelete && (
                <button
                  type="button"
                  className="sheet-action sheet-danger"
                  onClick={() => {
                    onClose();
                    onDelete();
                  }}
                >
                  <Icon name="trash" />
                  Xoá tin nhắn
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
