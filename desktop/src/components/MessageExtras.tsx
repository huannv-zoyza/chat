import { useEffect, useState } from "react";
import { ApiError, type Channel, type Message, type QuotedMessage } from "../lib/api";
import { channelLabel } from "../lib/channel";
import { e2ee, useE2EE } from "../lib/e2ee";
import { displayName, formatDay, formatTime } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { embedSummary } from "./Embeds";
import { Avatar, Dialog, FormError, Icon } from "./ui";

/* ---------- Xem trước ngắn gọn một tin (dùng cho trích dẫn, ghim, tìm kiếm) ---------- */

type Previewable = Pick<QuotedMessage, "id" | "user_id" | "content" | "cipher" | "embed" | "deleted"> & {
  attachments?: number | unknown[];
  poll?: string | { question: string };
};

export function previewText(m: Previewable, channelId: number): string {
  if (m.deleted) return "Tin nhắn đã bị xoá";
  let text = m.content;
  let embed = m.embed;
  let files = Array.isArray(m.attachments) ? m.attachments.length : (m.attachments ?? 0);
  if (m.cipher) {
    const dec = e2ee.decrypt({ id: m.id, channel_id: channelId, user_id: m.user_id, cipher: m.cipher });
    if (!dec.ok) return "🔒 Tin nhắn được mã hoá";
    text = dec.payload.text ?? "";
    embed = dec.payload.embed;
    files = dec.payload.files?.length ?? 0;
  }
  if (text) return text;
  const poll = typeof m.poll === "string" ? m.poll : m.poll?.question;
  if (poll) return `📊 ${poll}`;
  if (embed) return embedSummary(embed);
  if (files) return `📎 ${files} file đính kèm`;
  return "";
}

const msgPreview = (m: Message) =>
  previewText({ ...m, deleted: !!m.deleted_at, attachments: m.attachments, poll: m.poll }, m.channel_id);

/** Bản trích tin được trả lời (phía trên nội dung tin). Bấm để nhảy tới tin gốc. */
export function QuoteBlock({ quote, channelId }: { quote: QuotedMessage; channelId: number }) {
  const { state, jumpToMessage } = useChat();
  useE2EE();
  const user = state.users[quote.user_id];
  return (
    <button
      type="button"
      className={`quote${quote.deleted ? " is-deleted" : ""}`}
      onClick={() =>
        !quote.deleted && void jumpToMessage({ id: quote.id, channel_id: channelId, parent_id: quote.parent_id })
      }
      title="Xem tin được trả lời"
    >
      <span className="quote-who">
        <Icon name="reply" size={13} />
        {quote.user_id === state.me.id ? "Bạn" : displayName(user)}
      </span>
      <span className="quote-text">{previewText(quote, channelId)}</span>
    </button>
  );
}

/** Thanh "Đang trả lời …" phía trên ô soạn. */
export function ReplyBar({ message, onCancel }: { message: Message; onCancel: () => void }) {
  const { state } = useChat();
  useE2EE();
  return (
    <div className="reply-bar">
      <Icon name="reply" size={15} />
      <span className="reply-bar-text">
        Đang trả lời{" "}
        <strong>
          {message.user_id === state.me.id ? "chính bạn" : displayName(state.users[message.user_id] ?? message.user)}
        </strong>
        <span className="reply-bar-preview">{msgPreview(message)}</span>
      </span>
      <button type="button" className="icon-btn" onClick={onCancel} aria-label="Huỷ trả lời" title="Huỷ trả lời (Esc)">
        <Icon name="x" size={15} />
      </button>
    </div>
  );
}

/* ---------- Danh sách tin ghim ---------- */

export function PinsDialog({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const { state, loadPins, pinMessage, jumpToMessage } = useChat();
  useE2EE();
  const [error, setError] = useState<string | null>(null);
  const pins = state.pins[channel.id];
  useEffect(() => {
    loadPins(channel.id).catch(() => setError("Không tải được tin ghim."));
  }, [channel.id, loadPins]);

  return (
    <Dialog title="Tin đã ghim" onClose={onClose}>
      <FormError>{error}</FormError>
      {!pins && !error && <p className="muted">Đang tải…</p>}
      {pins && pins.length === 0 && (
        <p className="muted">Chưa có tin nào được ghim. Rê chuột (hoặc nhấn giữ) một tin, chọn “Ghim”.</p>
      )}
      {pins && pins.length > 0 && (
        <ul className="pin-list">
          {pins.map((m) => (
            <li key={m.id} className="pin-item">
              <button
                type="button"
                className="pin-open"
                onClick={() => {
                  onClose();
                  void jumpToMessage(m);
                }}
              >
                <Avatar user={state.users[m.user_id] ?? m.user} size={30} />
                <span className="pin-text">
                  <span className="pin-meta">
                    <strong>{displayName(state.users[m.user_id] ?? m.user)}</strong> · {formatDay(m.created_at)}{" "}
                    {formatTime(m.created_at)}
                  </span>
                  <span className="pin-preview">{msgPreview(m)}</span>
                  {m.pinned_by ? (
                    <span className="pin-by">
                      Ghim bởi {m.pinned_by === state.me.id ? "bạn" : displayName(state.users[m.pinned_by])}
                    </span>
                  ) : null}
                </span>
              </button>
              <button
                type="button"
                className="icon-btn"
                title="Bỏ ghim"
                aria-label="Bỏ ghim"
                onClick={() => void pinMessage(m.id, false).catch(() => setError("Không bỏ ghim được."))}
              >
                <Icon name="x" size={15} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}

/* ---------- Tìm kiếm ---------- */

function Highlight({ text, query }: { text: string; query: string }) {
  // tô đậm các từ khớp (so không dấu, như máy chủ tìm)
  const norm = (s: string) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/[đĐ]/g, "d").toLowerCase();
  const words = norm(query)
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean);
  if (!words.length) return <>{text}</>;
  const n = norm(text);
  const marks: [number, number][] = [];
  for (const w of words) {
    let i = n.indexOf(w);
    while (i >= 0) {
      marks.push([i, i + w.length]);
      i = n.indexOf(w, i + w.length);
    }
  }
  if (!marks.length || n.length !== text.length) return <>{text}</>;
  marks.sort((a, b) => a[0] - b[0]);
  const out: React.ReactNode[] = [];
  let last = 0;
  marks.forEach(([s, e], k) => {
    if (s < last) return;
    out.push(text.slice(last, s), <mark key={k}>{text.slice(s, e)}</mark>);
    last = e;
  });
  out.push(text.slice(last));
  return <>{out}</>;
}

export function SearchDialog({ onClose, onJumped }: { onClose: () => void; onJumped: () => void }) {
  const { api, state, jumpToMessage } = useChat();
  const [query, setQuery] = useState("");
  const [onlyHere, setOnlyHere] = useState(false);
  const [results, setResults] = useState<Message[] | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const here = state.activeId !== null ? state.channels[state.activeId] : undefined;

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setResults(null);
      return;
    }
    let alive = true;
    setLoading(true);
    const t = window.setTimeout(() => {
      api
        .search(q, onlyHere && here ? here.id : 0)
        .then((r) => {
          if (!alive) return;
          setResults(r.messages);
          setHasMore(r.has_more);
          setError(null);
        })
        .catch((err) => alive && setError(err instanceof ApiError ? err.message : "Không tìm được."))
        .finally(() => alive && setLoading(false));
    }, 250);
    return () => {
      alive = false;
      window.clearTimeout(t);
    };
  }, [api, query, onlyHere, here]);

  const more = async () => {
    if (!results?.length) return;
    const r = await api.search(query.trim(), onlyHere && here ? here.id : 0, results[results.length - 1].id);
    setResults([...results, ...r.messages]);
    setHasMore(r.has_more);
  };

  return (
    <Dialog title="Tìm tin nhắn" onClose={onClose} wide>
      <input
        className="search"
        autoFocus
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Gõ từ khoá (không cần dấu: “pho bo” tìm được “phở bò”)"
        aria-label="Từ khoá"
      />
      <div className="search-opts">
        {here && (
          <label className="check-field">
            <input type="checkbox" checked={onlyHere} onChange={(e) => setOnlyHere(e.target.checked)} />
            <span>Chỉ trong {channelLabel(here, state.users, state.me.id)}</span>
          </label>
        )}
        <span className="muted search-note">
          <Icon name="lock" size={13} /> Tin nhắn riêng đã mã hoá không tìm được (máy chủ không đọc được).
        </span>
      </div>
      <FormError>{error}</FormError>
      {loading && !results && <p className="muted">Đang tìm…</p>}
      {results && results.length === 0 && !loading && <p className="muted">Không có tin nào khớp “{query}”.</p>}
      {results && results.length > 0 && (
        <ul className="search-results">
          {results.map((m) => {
            const ch = state.channels[m.channel_id];
            const user = state.users[m.user_id] ?? m.user;
            return (
              <li key={m.id}>
                <button
                  type="button"
                  className="search-hit"
                  onClick={() => {
                    onClose();
                    onJumped();
                    void jumpToMessage(m);
                  }}
                >
                  <Avatar user={user} size={30} />
                  <span className="search-hit-body">
                    <span className="search-hit-meta">
                      <strong>{displayName(user)}</strong>
                      {ch && <span>{channelLabel(ch, state.users, state.me.id)}</span>}
                      {m.parent_id && <span>trong luồng</span>}
                      <span>
                        {formatDay(m.created_at)} {formatTime(m.created_at)}
                      </span>
                    </span>
                    <span className="search-hit-text">
                      <Highlight text={m.content} query={query} />
                    </span>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
      {hasMore && (
        <button type="button" className="btn btn-block" onClick={() => void more()}>
          Xem thêm kết quả
        </button>
      )}
    </Dialog>
  );
}
