import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Attachment, Channel } from "../lib/api";
import { displayName } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { buildRows, MessageRow } from "./ChannelView";
import { Composer } from "./Composer";
import { ImageViewer } from "./MessageParts";
import { Icon } from "./ui";

type Props = {
  channel: Channel;
  parentId: number;
  onClose: () => void;
};

/** Panel bên phải: tin gốc, các trả lời và ô trả lời. Trên điện thoại phủ toàn màn hình. */
export function ThreadPanel({ channel, parentId, onClose }: Props) {
  const { state, openThread, retryMessage, discardMessage } = useChat();
  const thread = state.threads[parentId];
  const [viewing, setViewing] = useState<Attachment | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);

  // Mở bằng thông báo hoặc kết nối lại: luồng có thể chưa được tải.
  useEffect(() => {
    if (!state.threads[parentId]) openThread(parentId);
  }, [parentId, state.threads, openThread]);

  const pending = useMemo(() => state.pending.filter((p) => p.parentId === parentId), [state.pending, parentId]);
  const rows = useMemo(
    () => (thread ? buildRows(thread.replies, pending, state.me.id).filter((r) => r.kind !== "day") : []),
    [thread, pending, state.me.id],
  );

  // Trả lời mới: tự cuộn xuống nếu đang ở cuối.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [rows.length]);

  const parent = thread?.parent;
  const parentUser = parent ? (state.users[parent.user_id] ?? parent.user) : undefined;
  const replyCount = thread?.replies.length ?? 0;
  const where = `${channel.is_private ? "" : "#"}${channel.name}`;

  return (
    <aside className="thread-panel" aria-label="Luồng trả lời">
      <header className="thread-header">
        <div>
          <h3>Luồng</h3>
          <p className="muted">{where}</p>
        </div>
        <button className="icon-btn" onClick={onClose} aria-label="Đóng luồng">
          <Icon name="x" />
        </button>
      </header>

      <div
        className="thread-scroll"
        ref={scrollRef}
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
        }}
      >
        {!parent ? (
          <p className="messages-loading">Đang tải luồng…</p>
        ) : (
          <>
            <MessageRow
              mine={parent.user_id === state.me.id}
              mentioned={!!parent.mentions?.includes(state.me.id)}
              grouped={false}
              name={displayName(parentUser)}
              avatarUser={parentUser}
              time={parent.created_at}
              content={parent.content}
              attachments={parent.attachments}
              onOpenImage={setViewing}
              message={parent}
            />
            <div className="thread-divider" role="separator">
              <span>{replyCount > 0 ? `${replyCount} trả lời` : "Chưa có trả lời"}</span>
            </div>
            {rows.map((r) => {
              if (r.kind === "msg") {
                const user = state.users[r.msg.user_id] ?? r.msg.user;
                return (
                  <MessageRow
                    key={r.key}
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
                  />
                );
              }
              if (r.kind !== "pending") return null;
              const me = state.users[state.me.id] ?? state.me;
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

      {parent && <Composer channel={channel} parentId={parentId} />}
      {viewing && <ImageViewer attachment={viewing} onClose={() => setViewing(null)} />}
    </aside>
  );
}
