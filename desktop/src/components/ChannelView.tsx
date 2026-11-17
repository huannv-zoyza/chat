import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type DragEvent } from "react";
import type { Attachment, Channel, Message } from "../lib/api";
import { channelMessages, type PendingMessage } from "../state/chat";
import { useChat } from "../state/ChatProvider";
import { dayKey, displayName, formatDay, formatFull, formatTime } from "../lib/format";
import { Composer, type ComposerHandle } from "./Composer";
import { Attachments, ImageViewer, LocalFiles, MessageText } from "./MessageParts";
import { Avatar, Icon } from "./ui";
import { LinkPreviews } from "./LinkPreview";
import { MessageActionSheet, MessageToolbar, ReactionBar, ReactionPicker, ThreadSummaryLink, useLongPress } from "./MessageActions";
import { COMPACT_QUERY, useMediaQuery } from "../lib/useMedia";
import { ChannelSigil } from "./ChannelDialogs";

const GROUP_WINDOW_MS = 5 * 60 * 1000;

type Props = {
  channel: Channel;
  membersOpen: boolean;
  onToggleMembers: () => void;
  onOpenSettings: () => void;
  onInvite: () => void;
  /** Có trên điện thoại: quay về danh sách kênh. */
  onBack?: () => void;
};

export function ChannelView({ channel, membersOpen, onToggleMembers, onOpenSettings, onInvite, onBack }: Props) {
  const { state, reconnectNow } = useChat();
  const disconnected = state.status === "reconnecting" || state.status === "offline";
  const composer = useRef<ComposerHandle>(null);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);

  // Kéo thả file vào bất kỳ đâu trong khung kênh.
  const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer.types).includes("Files");
  const onDragEnter = (e: DragEvent) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    dragDepth.current += 1;
    setDragging(true);
  };
  const onDragLeave = (e: DragEvent) => {
    if (!hasFiles(e)) return;
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) setDragging(false);
  };
  const onDrop = (e: DragEvent) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    dragDepth.current = 0;
    setDragging(false);
    composer.current?.addFiles(Array.from(e.dataTransfer.files));
  };

  return (
    <section
      className="room"
      aria-label={`Kênh ${channel.name}`}
      onDragEnter={onDragEnter}
      onDragOver={(e) => hasFiles(e) && e.preventDefault()}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      <header className="room-header">
        {onBack && (
          <button className="icon-btn back-btn" onClick={onBack} aria-label="Quay lại danh sách kênh">
            <Icon name="arrowLeft" size={22} />
          </button>
        )}
        <div className="room-heading">
          <h2 className="room-title">
            <span className="room-sigil">
              <ChannelSigil channel={channel} size={24} />
            </span>
            {channel.name}
          </h2>
          {channel.description && <p className="room-desc">{channel.description}</p>}
        </div>
        <div className="room-actions">
          <button
            className={`chip${membersOpen ? " is-on" : ""}`}
            onClick={onToggleMembers}
            aria-pressed={membersOpen}
            aria-label={`Thành viên (${channel.member_count})`}
            title="Thành viên"
          >
            <Icon name="users" size={16} />
            {channel.member_count}
          </button>
          <button className="chip" onClick={onInvite} title="Mời người vào kênh" aria-label="Mời người vào kênh">
            <Icon name="userPlus" size={16} />
            <span className="btn-label">Mời</span>
          </button>
          <button className="icon-btn" onClick={onOpenSettings} title="Cài đặt kênh" aria-label="Cài đặt kênh">
            <Icon name="settings" />
          </button>
        </div>
      </header>

      {disconnected && (
        <div className="banner" role="status">
          <span>
            {state.status === "offline"
              ? "Máy đang mất mạng. Tin nhắn sẽ được gửi khi có mạng trở lại."
              : "Mất kết nối tới máy chủ, đang thử kết nối lại…"}
          </span>
          {state.status === "reconnecting" && (
            <button className="link-btn" onClick={reconnectNow}>
              Thử ngay
            </button>
          )}
        </div>
      )}

      <MessageList channel={channel} />
      <TypingIndicator channelId={channel.id} />
      <Composer ref={composer} channel={channel} />

      {dragging && (
        <div className="drop-overlay" aria-hidden>
          <div className="drop-card">
            <Icon name="upload" size={28} />
            <strong>Thả file để gửi vào #{channel.name}</strong>
            <span>Tối đa 10 file, mỗi file 20 MB</span>
          </div>
        </div>
      )}
    </section>
  );
}

/* ---------- Danh sách tin nhắn ---------- */

export type Row =
  | { kind: "day"; key: string; label: string }
  | { kind: "msg"; key: string; msg: Message; grouped: boolean }
  | { kind: "pending"; key: string; p: PendingMessage; grouped: boolean };

export function buildRows(items: Message[], pending: PendingMessage[], meId: number): Row[] {
  const rows: Row[] = [];
  let prevUser: number | null = null;
  let prevTime = 0;
  let prevDay = "";

  const push = (userId: number, iso: string, make: (grouped: boolean) => Row) => {
    const day = dayKey(iso);
    if (day !== prevDay) {
      rows.push({ kind: "day", key: "d" + day, label: formatDay(iso) });
      prevDay = day;
      prevUser = null;
    }
    const t = new Date(iso).getTime();
    const grouped = prevUser === userId && t - prevTime < GROUP_WINDOW_MS;
    rows.push(make(grouped));
    prevUser = userId;
    prevTime = t;
  };

  for (const m of items) push(m.user_id, m.created_at, (grouped) => ({ kind: "msg", key: "m" + m.id, msg: m, grouped }));
  for (const p of pending)
    push(meId, p.createdAt, (grouped) => ({ kind: "pending", key: "p" + p.clientId, p, grouped }));
  return rows;
}

function MessageList({ channel }: { channel: Channel }) {
  const { state, loadOlder, retryMessage, discardMessage } = useChat();
  const cm = channelMessages(state, channel.id);
  const { openThread } = useChat();
  const pending = useMemo(
    () => state.pending.filter((p) => p.channelId === channel.id && !p.parentId),
    [state.pending, channel.id],
  );
  const rows = useMemo(() => buildRows(cm.items, pending, state.me.id), [cm.items, pending, state.me.id]);
  const seen = useMemo(
    () => seenByMessage(cm.items, state.reads[channel.id], state.me.id),
    [cm.items, state.reads, channel.id, state.me.id],
  );
  const [viewing, setViewing] = useState<Attachment | null>(null);

  const scrollRef = useRef<HTMLDivElement>(null);
  const topRef = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const [showJump, setShowJump] = useState(false);
  const prev = useRef<{ channelId: number; firstKey?: string; lastKey?: string; height: number } | null>(null);

  const firstKey = rows.find((r) => r.kind !== "day")?.key;
  const last = rows[rows.length - 1];
  const lastKey = last?.key;
  const lastIsMine = last?.kind === "pending" || (last?.kind === "msg" && last.msg.user_id === state.me.id);

  // Giữ vị trí cuộn hợp lý: mở kênh -> xuống cuối; tải tin cũ -> giữ nguyên chỗ đang đọc;
  // tin mới -> chỉ tự cuộn nếu đang ở cuối (hoặc tin của mình), không thì hiện nút "Tin mới".
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const p = prev.current;
    if (!p || p.channelId !== channel.id) {
      el.scrollTop = el.scrollHeight;
      atBottom.current = true;
      setShowJump(false);
    } else if (p.firstKey !== firstKey && p.lastKey === lastKey) {
      el.scrollTop += el.scrollHeight - p.height;
    } else if (p.lastKey !== lastKey) {
      if (atBottom.current || lastIsMine) el.scrollTop = el.scrollHeight;
      else setShowJump(true);
    } else if (atBottom.current && el.scrollHeight !== p.height) {
      // nội dung cuối thay đổi chiều cao (dòng "đã xem", cảm xúc…): đang ở cuối thì bám theo
      el.scrollTop = el.scrollHeight;
    }
    prev.current = { channelId: channel.id, firstKey, lastKey, height: el.scrollHeight };
  });

  // Ảnh không rõ kích thước (webp…) tải xong làm danh sách cao lên: đang ở cuối thì bám theo.
  // Sự kiện "load" không nổi bọt nên phải bắt ở pha capture bằng listener gốc.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onLoad = () => {
      if (atBottom.current) el.scrollTop = el.scrollHeight;
    };
    el.addEventListener("load", onLoad, true);
    return () => el.removeEventListener("load", onLoad, true);
  }, []);

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    if (atBottom.current) setShowJump(false);
    if (prev.current) prev.current.height = el.scrollHeight;
  };

  // Cuộn tới gần đầu danh sách thì tải tin cũ hơn.
  useEffect(() => {
    const el = topRef.current;
    const root = scrollRef.current;
    if (!el || !root || !cm.loaded || !cm.hasMore) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) void loadOlder(channel.id);
      },
      { root, rootMargin: "300px 0px 0px 0px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [channel.id, cm.loaded, cm.hasMore, cm.items.length, loadOlder]);

  const jumpToBottom = () => {
    const el = scrollRef.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    setShowJump(false);
  };

  const owner = state.users[channel.owner_id];
  const me = state.users[state.me.id] ?? state.me;

  return (
    <div className="messages-wrap">
      <div
        className="messages"
        ref={scrollRef}
        onScroll={onScroll}
        role="log"
        aria-live="polite"
        aria-relevant="additions"
      >
        <div ref={topRef} />
        {!cm.loaded ? (
          <p className="messages-loading">Đang tải tin nhắn…</p>
        ) : (
          <>
            {cm.hasMore ? (
              <p className="messages-loading">{cm.loadingOlder ? "Đang tải tin cũ hơn…" : ""}</p>
            ) : (
              <div className="channel-intro">
                <span className="channel-intro-mark">
                  <ChannelSigil channel={channel} size={44} />
                </span>
                <h3>
                  Đây là khởi đầu của {channel.is_private ? "" : "#"}
                  {channel.name}
                </h3>
                <p>
                  {channel.description || "Kênh chưa có mô tả."}
                  {owner && <> Kênh do {displayName(owner)} tạo.</>}
                  {channel.is_private && " Đây là kênh riêng tư: chỉ người có link mời mới vào được."}
                </p>
              </div>
            )}

            {rows.map((r) => {
              if (r.kind === "day")
                return (
                  <div key={r.key} className="day-divider" role="separator">
                    <span>{r.label}</span>
                  </div>
                );
              if (r.kind === "msg") {
                const user = state.users[r.msg.user_id] ?? r.msg.user;
                const readers = seen.get(r.msg.id);
                return (
                  <Fragment key={r.key}>
                    <MessageRow
                      mine={r.msg.user_id === state.me.id}
                      mentioned={!!r.msg.mentions?.includes(state.me.id)}
                      grouped={r.grouped}
                      name={displayName(user)}
                      avatarUser={user}
                      time={r.msg.created_at}
                      content={r.msg.content}
                      attachments={r.msg.attachments}
                      onOpenImage={setViewing}
                      message={r.msg}
                      onOpenThread={() => openThread(r.msg.id)}
                    />
                    {readers && <SeenBy userIds={readers} />}
                  </Fragment>
                );
              }
              return (
                <MessageRow
                  key={r.key}
                  mine
                  grouped={r.grouped}
                  name={displayName(me)}
                  avatarUser={me}
                  time={r.p.createdAt}
                  content={r.p.content}
                  pending={r.p}
                  onRetry={() => retryMessage(r.p.clientId)}
                  onDiscard={() => discardMessage(r.p.clientId)}
                />
              );
            })}
          </>
        )}
      </div>

      {showJump && (
        <button className="jump-btn" onClick={jumpToBottom}>
          <Icon name="arrowDown" size={16} />
          Tin mới
        </button>
      )}

      {viewing && <ImageViewer attachment={viewing} onClose={() => setViewing(null)} />}
    </div>
  );
}

/**
 * Mỗi thành viên (trừ mình) hiện ở dưới tin mới nhất họ đã xem, như Messenger.
 * Bỏ qua: người đã xem tới tin cũ hơn đoạn đang tải, và người gửi tin cuối cùng
 * (chính họ viết tin đó, hiện avatar dưới tin của họ là thừa).
 */
function seenByMessage(items: Message[], reads: Record<number, number> | undefined, meId: number) {
  const out = new Map<number, number[]>();
  if (!reads || items.length === 0) return out;
  const last = items[items.length - 1];
  for (const [uidStr, pos] of Object.entries(reads)) {
    const uid = Number(uidStr);
    if (uid === meId) continue;
    // tin mới nhất trong dòng chính có id <= pos (tìm nhị phân, items đã sắp theo id)
    let lo = 0;
    let hi = items.length - 1;
    let found = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (items[mid].id <= pos) {
        found = mid;
        lo = mid + 1;
      } else hi = mid - 1;
    }
    if (found < 0) continue; // chưa xem tin nào, hoặc chỉ xem tới đoạn cũ hơn chưa tải
    const m = items[found];
    if (m === last && m.user_id === uid) continue;
    const list = out.get(m.id);
    if (list) list.push(uid);
    else out.set(m.id, [uid]);
  }
  return out;
}

const SEEN_MAX = 5;

function SeenBy({ userIds }: { userIds: number[] }) {
  const { state } = useChat();
  const [open, setOpen] = useState(false);
  const names = userIds.map((id) => displayName(state.users[id]));
  const label = `Đã xem: ${names.join(", ")}`;
  const shown = userIds.slice(0, SEEN_MAX);
  const more = userIds.length - shown.length;
  return (
    <div className="seen-by" aria-live="off">
      {open && <span className="seen-names">{label}</span>}
      <button
        type="button"
        className="seen-avatars"
        title={label}
        aria-label={label}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {shown.map((id) => (
          <Avatar key={id} user={state.users[id]} size={16} />
        ))}
        {more > 0 && <span className="seen-more">+{more}</span>}
      </button>
    </div>
  );
}

type RowProps = {
  mine: boolean;
  mentioned?: boolean;
  grouped: boolean;
  name: string;
  avatarUser: Parameters<typeof Avatar>[0]["user"];
  time: string;
  content: string;
  attachments?: Attachment[];
  onOpenImage?: (a: Attachment) => void;
  pending?: PendingMessage;
  onRetry?: () => void;
  onDiscard?: () => void;
  /** Tin đã gửi: có thì hiện cảm xúc + thao tác. */
  message?: Message;
  /** Có ở dòng chính: mở luồng trả lời của tin này. */
  onOpenThread?: () => void;
};

export function MessageRow(props: RowProps) {
  const { mine, mentioned, grouped, name, avatarUser, time, content, attachments, pending, message } = props;
  const { state } = useChat();
  const compact = useMediaQuery(COMPACT_QUERY);
  const [pickerAnchor, setPickerAnchor] = useState<HTMLElement | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const longPress = useLongPress(() => setSheetOpen(true), !!message && compact);
  const failed = pending?.status === "failed";
  const uploading = pending && pending.status === "sending" && pending.files?.length;
  const cls = [
    "msg",
    grouped && "msg-grouped",
    mine && "msg-mine",
    mentioned && "msg-mentioned",
    pending && "msg-pending",
    failed && "msg-failed",
    pickerAnchor && "has-popover",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <article className={cls} {...longPress}>
      <div className="msg-gutter">
        {grouped ? (
          <time className="msg-gutter-time" dateTime={time} title={formatFull(time)}>
            {formatTime(time)}
          </time>
        ) : (
          <Avatar user={avatarUser} size={36} />
        )}
      </div>
      <div className="msg-main">
        {!grouped && (
          <header className="msg-head">
            <span className="msg-user" style={{ color: avatarUser ? `var(--u${(avatarUser.id % 6) + 1})` : undefined }}>
              {name}
            </span>
            <time className="msg-time" dateTime={time} title={formatFull(time)}>
              {formatTime(time)}
            </time>
          </header>
        )}
        {content && <MessageText content={content} meId={state.me.id} />}
        {content && message && <LinkPreviews content={content} />}
        {attachments && attachments.length > 0 && props.onOpenImage && (
          <Attachments attachments={attachments} onOpenImage={props.onOpenImage} />
        )}
        {pending?.files && pending.files.length > 0 && <LocalFiles files={pending.files} />}
        {message && <ReactionBar message={message} onAdd={setPickerAnchor} />}
        {message && props.onOpenThread && <ThreadSummaryLink message={message} onOpen={props.onOpenThread} />}

        {pending &&
          (failed ? (
            <p className="msg-state msg-state-failed">
              Chưa gửi được. {pending.error}{" "}
              <button className="link-btn" onClick={props.onRetry}>
                Gửi lại
              </button>{" "}
              <button className="link-btn" onClick={props.onDiscard}>
                Bỏ tin này
              </button>
            </p>
          ) : uploading ? (
            <div className="upload-progress" role="progressbar" aria-valuenow={Math.round((pending.progress ?? 0) * 100)}>
              <span className="upload-bar">
                <span style={{ width: `${Math.round((pending.progress ?? 0) * 100)}%` }} />
              </span>
              <span className="msg-state">
                {(pending.progress ?? 0) >= 1 ? "Đang xử lý…" : `Đang tải lên ${Math.round((pending.progress ?? 0) * 100)}%`}
              </span>
            </div>
          ) : (
            <p className="msg-state">Đang gửi…</p>
          ))}
      </div>

      {message && !compact && <MessageToolbar message={message} onPick={setPickerAnchor} onReply={props.onOpenThread} />}
      {message && pickerAnchor && (
        <ReactionPicker messageId={message.id} anchor={pickerAnchor} onClose={() => setPickerAnchor(null)} />
      )}
      {message && sheetOpen && (
        <MessageActionSheet message={message} onClose={() => setSheetOpen(false)} onReply={props.onOpenThread} />
      )}
    </article>
  );
}

/* ---------- Đang gõ ---------- */

function TypingIndicator({ channelId }: { channelId: number }) {
  const { state } = useChat();
  const ids = Object.keys(state.typing[channelId] ?? {}).map(Number);
  const names = ids.map((id) => displayName(state.users[id]));

  let text = "";
  if (names.length === 1) text = `${names[0]} đang gõ…`;
  else if (names.length === 2) text = `${names[0]} và ${names[1]} đang gõ…`;
  else if (names.length > 2) text = "Nhiều người đang gõ…";

  return (
    <div className="typing" aria-live="polite">
      {text && (
        <>
          <span className="typing-dots" aria-hidden>
            <i />
            <i />
            <i />
          </span>
          {text}
        </>
      )}
    </div>
  );
}
