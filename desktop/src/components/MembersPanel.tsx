import { useEffect, useMemo, useState } from "react";
import { ApiError, type Channel } from "../lib/api";
import { isDM } from "../lib/channel";
import { useChat } from "../state/ChatProvider";
import { displayName } from "../lib/format";
import { NICKNAME_MAX, THROW_ITEMS } from "../lib/fun";
import { Avatar, Icon } from "./ui";

export function MembersPanel({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const { state, api, loadMembers, openDM } = useChat();
  const [error, setError] = useState<string | null>(null);
  const ids = state.members[channel.id];
  // Chủ kênh mời người khác ra: bấm lần đầu hỏi lại ngay trên dòng đó.
  const canKick = !isDM(channel) && channel.owner_id === state.me.id;
  const [confirmKick, setConfirmKick] = useState<number | null>(null);
  const [kicking, setKicking] = useState(false);
  useEffect(() => setConfirmKick(null), [channel.id]);
  // Trêu nhau: mở dải ném đồ / đặt biệt danh ngay dưới dòng người đó.
  const nicks = state.nicknames[channel.id];
  const [funOpen, setFunOpen] = useState<number | null>(null);
  const [nickDraft, setNickDraft] = useState("");
  const [funBusy, setFunBusy] = useState(false);
  useEffect(() => setFunOpen(null), [channel.id]);
  const openFun = (id: number) => {
    setFunOpen(funOpen === id ? null : id);
    setNickDraft(nicks?.[id] ?? "");
    setError(null);
  };
  const fun = async (fn: () => Promise<unknown>, fail: string, close = false) => {
    setFunBusy(true);
    setError(null);
    try {
      await fn();
      if (close) setFunOpen(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : fail);
    } finally {
      setFunBusy(false);
    }
  };

  const kick = async (userId: number) => {
    setKicking(true);
    setError(null);
    try {
      await api.kickMember(channel.id, userId);
      setConfirmKick(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không mời được người này ra khỏi kênh.");
    } finally {
      setKicking(false);
    }
  };

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
              if (confirmKick === m.id) {
                return (
                  <li key={m.id} className="member member-kick-confirm">
                    <span className="member-text">
                      Mời <strong>{displayName(m)}</strong> ra khỏi kênh?
                    </span>
                    <span className="member-kick-actions">
                      <button
                        type="button"
                        className="btn btn-sm btn-danger"
                        disabled={kicking}
                        onClick={() => void kick(m.id)}
                        autoFocus
                      >
                        Mời ra
                      </button>
                      <button type="button" className="btn btn-sm" disabled={kicking} onClick={() => setConfirmKick(null)}>
                        Huỷ
                      </button>
                    </span>
                  </li>
                );
              }
              return (
                <li key={m.id} className={`member${online ? "" : " is-away"}`}>
                  <Avatar user={m} size={32} online={online} />
                  <span className="member-text">
                    <span className="member-name">
                      <span className="member-name-text">{nicks?.[m.id] || displayName(m)}</span>
                      {m.id === state.me.id && <span className="member-tag">bạn</span>}
                      {m.id === channel.owner_id && <span className="member-tag">chủ kênh</span>}
                    </span>
                    <span className="member-status">
                      {nicks?.[m.id] ? displayName(m) + " · " : ""}
                      {m.status_text || "@" + m.username}
                    </span>
                  </span>
                  {m.id !== state.me.id && (
                    <button
                      type="button"
                      className="icon-btn member-dm"
                      title={`Nhắn riêng cho ${displayName(m)}`}
                      aria-label={`Nhắn riêng cho ${displayName(m)}`}
                      onClick={() => void openDM(m.id).catch(() => setError("Không mở được cuộc trò chuyện."))}
                    >
                      <Icon name="send" size={16} />
                    </button>
                  )}
                  <button
                    type="button"
                    className={`icon-btn member-fun${funOpen === m.id ? " is-open" : ""}`}
                    title={m.id === state.me.id ? "Đặt biệt danh cho mình" : `Trêu ${displayName(m)}`}
                    aria-label={m.id === state.me.id ? "Đặt biệt danh cho mình" : `Trêu ${displayName(m)}`}
                    aria-expanded={funOpen === m.id}
                    onClick={() => openFun(m.id)}
                  >
                    <span aria-hidden>😈</span>
                  </button>
                  {canKick && m.id !== state.me.id && (
                    <button
                      type="button"
                      className="icon-btn member-kick"
                      title={`Mời ${displayName(m)} ra khỏi kênh`}
                      aria-label={`Mời ${displayName(m)} ra khỏi kênh`}
                      onClick={() => setConfirmKick(m.id)}
                    >
                      <Icon name="x" size={16} />
                    </button>
                  )}
                  {funOpen === m.id && (
                    <div className="member-fun-panel">
                      {m.id !== state.me.id && (
                        <div className="member-throws" role="group" aria-label={`Ném đồ vào ${displayName(m)}`}>
                          {THROW_ITEMS.map((t) => (
                            <button
                              key={t.id}
                              type="button"
                              className="member-throw"
                              title={`Ném ${t.name}`}
                              aria-label={`Ném ${t.name} vào ${displayName(m)}`}
                              disabled={funBusy}
                              onClick={() => void fun(() => api.throwItem(channel.id, m.id, t.id), "Ném trượt rồi, thử lại.")}
                            >
                              {t.icon}
                            </button>
                          ))}
                        </div>
                      )}
                      <form
                        className="member-nick"
                        onSubmit={(e) => {
                          e.preventDefault();
                          void fun(() => api.setNickname(channel.id, m.id, nickDraft), "Không đặt được biệt danh.", true);
                        }}
                      >
                        <input
                          value={nickDraft}
                          maxLength={NICKNAME_MAX}
                          placeholder="Biệt danh trong kênh…"
                          aria-label={`Biệt danh của ${displayName(m)}`}
                          onChange={(e) => setNickDraft(e.target.value)}
                        />
                        <button type="submit" className="btn btn-sm btn-primary" disabled={funBusy}>
                          Lưu
                        </button>
                        {!!nicks?.[m.id] && (
                          <button
                            type="button"
                            className="btn btn-sm"
                            disabled={funBusy}
                            onClick={() => void fun(() => api.setNickname(channel.id, m.id, ""), "Không xoá được biệt danh.", true)}
                          >
                            Xoá
                          </button>
                        )}
                      </form>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        </>
      )}
    </aside>
  );
}
