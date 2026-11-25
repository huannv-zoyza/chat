import { useEffect, useState } from "react";
import { ApiError, type PublicUser } from "../lib/api";
import { displayName } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, FormError } from "./ui";

/** Chọn một người để nhắn riêng (tìm theo tên hoặc @username). */
export function NewDMDialog({ onClose, onOpened }: { onClose: () => void; onOpened: () => void }) {
  const { api, state, openDM } = useChat();
  const [query, setQuery] = useState("");
  const [list, setList] = useState<PublicUser[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [opening, setOpening] = useState<number | null>(null);

  // Gõ xong ~200ms mới tìm, tránh gọi server mỗi phím.
  useEffect(() => {
    let alive = true;
    const t = window.setTimeout(() => {
      api
        .searchUsers(query.trim())
        .then((users) => alive && setList(users))
        .catch((err) => alive && setError(err instanceof ApiError ? err.message : "Không tải được danh sách."));
    }, 200);
    return () => {
      alive = false;
      window.clearTimeout(t);
    };
  }, [api, query]);

  const open = async (u: PublicUser) => {
    setOpening(u.id);
    setError(null);
    try {
      await openDM(u.id);
      onClose();
      onOpened();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không mở được cuộc trò chuyện.");
      setOpening(null);
    }
  };

  const connected = state.status === "open";
  // Người khác lên trước, mình ở cuối ("Ghi chú cho mình").
  const people = list
    ? [...list.filter((u) => u.id !== state.me.id), ...list.filter((u) => u.id === state.me.id)]
    : null;

  return (
    <Dialog title="Tin nhắn mới" onClose={onClose}>
      <input
        className="search"
        autoFocus data-autofocus
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Tìm theo tên hoặc @username"
        aria-label="Tìm người để nhắn"
      />
      <FormError>{error}</FormError>
      {!people && !error && <p className="muted">Đang tải…</p>}
      {people && people.length === 0 && <p className="muted">Không tìm thấy ai khớp “{query}”.</p>}
      {people && people.length > 0 && (
        <ul className="people-list">
          {people.map((u) => {
            const user = state.users[u.id] ?? u;
            const self = u.id === state.me.id;
            return (
              <li key={u.id}>
                <button
                  type="button"
                  className="person"
                  onClick={() => void open(u)}
                  disabled={opening !== null}
                  aria-label={self ? "Ghi chú cho chính mình" : `Nhắn cho ${displayName(user)}`}
                >
                  <Avatar user={user} size={36} online={connected && !!state.online[u.id]} />
                  <span className="person-text">
                    <span className="person-name">
                      {displayName(user)}
                      {self && <span className="member-tag">bạn</span>}
                    </span>
                    <span className="person-sub">
                      @{user.username}
                      {user.status_text && ` · ${user.status_text}`}
                    </span>
                  </span>
                  <span className="person-action">{opening === u.id ? "Đang mở…" : self ? "Ghi chú" : "Nhắn tin"}</span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </Dialog>
  );
}
