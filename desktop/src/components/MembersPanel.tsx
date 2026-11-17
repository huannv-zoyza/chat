import { useEffect, useMemo, useState } from "react";
import type { Channel } from "../lib/api";
import { useChat } from "../state/ChatProvider";
import { displayName } from "../lib/format";
import { Avatar, Icon } from "./ui";

export function MembersPanel({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const { state, loadMembers } = useChat();
  const [error, setError] = useState<string | null>(null);
  const ids = state.members[channel.id];

  useEffect(() => {
    setError(null);
    loadMembers(channel.id).catch(() => setError("Không tải được danh sách thành viên."));
  }, [channel.id, channel.member_count, loadMembers]);

  // Đang mất kết nối thì không biết ai online, không hiện chấm xanh cũ.
  const connected = state.status === "open";
  const isOnline = (id: number) => connected && !!state.online[id];

  // Người online lên trước, rồi theo tên.
  const members = useMemo(() => {
    if (!ids) return null;
    return ids
      .map((id) => state.users[id])
      .filter(Boolean)
      .sort((a, b) => {
        const oa = state.online[a.id] ? 0 : 1;
        const ob = state.online[b.id] ? 0 : 1;
        return oa - ob || displayName(a).localeCompare(displayName(b), "vi");
      });
  }, [ids, state.users, state.online]);

  const onlineCount = members?.filter((m) => isOnline(m.id)).length ?? 0;

  return (
    <aside className="members" aria-label="Thành viên kênh">
      <header className="members-header">
        <h3>Thành viên</h3>
        <button className="icon-btn" onClick={onClose} aria-label="Đóng danh sách thành viên">
          <Icon name="x" />
        </button>
      </header>

      {error && <p className="members-note">{error}</p>}
      {!members && !error && <p className="members-note">Đang tải…</p>}

      {members && (
        <>
          <p className="members-note">
            {connected ? `${onlineCount} / ${members.length} người đang trực tuyến` : `${members.length} thành viên`}
          </p>
          <ul className="member-list">
            {members.map((m) => {
              const online = isOnline(m.id);
              return (
                <li key={m.id} className={`member${online ? "" : " is-away"}`}>
                  <Avatar user={m} size={32} online={online} />
                  <span className="member-text">
                    <span className="member-name">
                      <span className="member-name-text">{displayName(m)}</span>
                      {m.id === state.me.id && <span className="member-tag">bạn</span>}
                      {m.id === channel.owner_id && <span className="member-tag">chủ kênh</span>}
                    </span>
                    <span className="member-status">{m.status_text || "@" + m.username}</span>
                  </span>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </aside>
  );
}
