import { useMemo } from "react";
import { useChat } from "../state/ChatProvider";
import type { SocketStatus } from "../lib/socket";
import { displayName } from "../lib/format";
import { Avatar, Icon } from "./ui";
import { ChannelSigil } from "./ChannelDialogs";
import { channelTitle, dmPeerId, isDM } from "../lib/channel";

const STATUS_LABEL: Record<SocketStatus, string> = {
  idle: "Đang kết nối",
  connecting: "Đang kết nối",
  open: "Đã kết nối",
  reconnecting: "Đang kết nối lại",
  offline: "Mất mạng",
  unauthorized: "Hết phiên",
  closed: "Đã ngắt",
};

type Props = {
  /** Gọi sau khi chọn kênh (trên điện thoại: chuyển sang màn chat). */
  onOpenChannel: () => void;
  onCreateChannel: () => void;
  onBrowseChannels: () => void;
  onJoinByLink: () => void;
  onOpenSettings: () => void;
  onNewDM: () => void;
  onSearch: () => void;
  onReminders: () => void;
};

export function Sidebar({
  onOpenChannel,
  onCreateChannel,
  onBrowseChannels,
  onJoinByLink,
  onOpenSettings,
  onNewDM,
  onSearch,
  onReminders,
}: Props) {
  const { state, selectChannel } = useChat();
  const channels = useMemo(
    () =>
      Object.values(state.channels)
        .filter((c) => !isDM(c))
        .sort((a, b) => a.name.localeCompare(b.name, "vi")),
    [state.channels],
  );
  const dms = useMemo(
    () =>
      Object.values(state.channels)
        .filter(isDM)
        .map((c) => ({ c, title: channelTitle(c, state.users, state.me.id), peer: dmPeerId(c, state.me.id)! }))
        .sort((a, b) => a.title.localeCompare(b.title, "vi")),
    [state.channels, state.users, state.me.id],
  );
  const me = state.users[state.me.id] ?? state.me;
  const connected = state.status === "open";
  const dueCount = state.reminders?.filter((r) => r.fired_at).length ?? 0;
  const upcomingCount = state.reminders?.filter((r) => !r.fired_at).length ?? 0;

  return (
    <nav className="sidebar" aria-label="Kênh">
      <header className="sidebar-top">
        <span className="brand">Go Chat</span>
        <button
          className="icon-btn icon-btn-ink sidebar-search"
          onClick={onSearch}
          title="Tìm tin nhắn (Ctrl+K)"
          aria-label="Tìm tin nhắn"
        >
          <Icon name="search" size={17} />
        </button>
        <span className={`conn conn-${state.status}`} title={STATUS_LABEL[state.status]}>
          <span className="conn-dot" />
          <span className="sr-only">{STATUS_LABEL[state.status]}</span>
        </span>
      </header>

      <div className="sidebar-section">
        <div className="sidebar-heading">
          <h3>Kênh</h3>
          <button className="icon-btn icon-btn-ink" onClick={onCreateChannel} title="Tạo kênh" aria-label="Tạo kênh">
            <Icon name="plus" size={16} />
          </button>
        </div>

        {channels.length === 0 ? (
          <p className="sidebar-empty">Bạn chưa ở kênh nào.</p>
        ) : (
          <ul className="channel-list">
            {channels.map((c) => {
              const active = c.id === state.activeId;
              const unread = !active && c.unread_count > 0;
              const mentions = !active ? (c.mention_count ?? 0) : 0;
              return (
                <li key={c.id}>
                  <button
                    className={`channel${unread ? " has-unread" : ""}`}
                    aria-current={active ? "page" : undefined}
                    onClick={() => {
                      selectChannel(c.id);
                      onOpenChannel();
                    }}
                  >
                    <span className="channel-hash">
                      <ChannelSigil channel={c} size={13} />
                    </span>
                    <span className="channel-name">{c.name}</span>
                    {mentions > 0 ? (
                      <span className="badge badge-mention" aria-label={`${mentions} lần nhắc tên bạn`}>
                        @{mentions > 99 ? "99+" : mentions}
                      </span>
                    ) : (
                      unread && (
                        <span className="badge" aria-label={`${c.unread_count} tin chưa đọc`}>
                          {c.unread_count > 99 ? "99+" : c.unread_count}
                        </span>
                      )
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        )}

        <button className="sidebar-action" onClick={onBrowseChannels}>
          <Icon name="compass" size={16} />
          Khám phá kênh
        </button>
        <button className="sidebar-action" onClick={onJoinByLink}>
          <Icon name="link" size={16} />
          Tham gia bằng link mời
        </button>
        <button className="sidebar-action" onClick={onReminders}>
          <Icon name="clock" size={16} />
          Nhắc việc
          {dueCount > 0 ? (
            <span className="badge badge-mention" aria-label={`${dueCount} nhắc việc đến giờ`}>
              {dueCount}
            </span>
          ) : (
            upcomingCount > 0 && <span className="sidebar-action-count">{upcomingCount}</span>
          )}
        </button>

        <div className="sidebar-heading sidebar-heading-dm">
          <h3>Tin nhắn riêng</h3>
          <button className="icon-btn icon-btn-ink" onClick={onNewDM} title="Tin nhắn mới" aria-label="Tin nhắn mới">
            <Icon name="plus" size={16} />
          </button>
        </div>
        {dms.length === 0 ? (
          <button className="sidebar-action" onClick={onNewDM}>
            <Icon name="send" size={16} />
            Nhắn riêng cho ai đó
          </button>
        ) : (
          <ul className="channel-list">
            {dms.map(({ c, title, peer }) => {
              const active = c.id === state.activeId;
              const unread = !active && c.unread_count > 0;
              return (
                <li key={c.id}>
                  <button
                    className={`channel channel-dm${unread ? " has-unread" : ""}`}
                    aria-current={active ? "page" : undefined}
                    onClick={() => {
                      selectChannel(c.id);
                      onOpenChannel();
                    }}
                  >
                    <span className="channel-hash">
                      <Avatar
                        user={state.users[peer]}
                        size={20}
                        online={peer !== state.me.id ? connected && !!state.online[peer] : undefined}
                      />
                    </span>
                    <span className="channel-name">{title}</span>
                    {unread && (
                      <span className="badge" aria-label={`${c.unread_count} tin chưa đọc`}>
                        {c.unread_count > 99 ? "99+" : c.unread_count}
                      </span>
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <button className="me-card" onClick={onOpenSettings} title="Cài đặt tài khoản">
        <Avatar user={me} size={34} online={state.status === "open"} />
        <span className="me-text">
          <span className="me-name">{displayName(me)}</span>
          <span className="me-status">{me.status_text || "@" + me.username}</span>
        </span>
        <Icon name="settings" size={16} />
      </button>
    </nav>
  );
}
