import { useEffect, useMemo, useState } from "react";
import { ApiError, type Channel, type Embed, type Message, type MessageLinkInfo } from "../lib/api";
import { channelLabel, channelTitle, dmPeerId, isDM } from "../lib/channel";
import { e2ee, useE2EE } from "../lib/e2ee";
import { copyText, displayName } from "../lib/format";
import type { GameView } from "../lib/api";
import { useChat } from "../state/ChatProvider";
import { ChannelSigil } from "./ChannelDialogs";
import { Avatar, Dialog, FormError, Icon } from "./ui";

/* =========================================================
   Chia sẻ tin nhắn: bản sao, thẻ liên kết tới tin gốc, link …/m/<id>, chia sẻ ra ngoài
   ========================================================= */

const KIND_LABEL: Record<string, { icon: string; label: string }> = {
  message: { icon: "💬", label: "Tin nhắn" },
  game: { icon: "🎮", label: "Trò chơi" },
  poll: { icon: "📊", label: "Bình chọn" },
  image: { icon: "🖼️", label: "Ảnh" },
  file: { icon: "📎", label: "File" },
  sticker: { icon: "😺", label: "Sticker" },
  gif: { icon: "🎞️", label: "GIF" },
  call: { icon: "📞", label: "Cuộc gọi" },
};

/** Tin có thể gửi dạng bản sao (game, bình chọn, cuộc gọi thì không chép được). */
function copyable(m: Message) {
  return !m.poll && !["game", "call"].includes(m.embed?.type ?? "");
}

export function ShareDialog({ message, onClose }: { message: Message; onClose: () => void }) {
  const { state, api, shareMessage } = useChat();
  useE2EE();
  const source = state.channels[message.channel_id];
  const fromDM = !!source && isDM(source);
  const canCopy = copyable(message) && (!message.cipher || e2ee.decrypt(message).ok);
  const canLink = !fromDM;
  const [mode, setMode] = useState<"copy" | "link">(canCopy && !(message.embed?.type === "share") ? "copy" : "link");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState<number | null>(null);
  const [done, setDone] = useState<number[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);

  const text = useMemo(() => {
    if (message.cipher) {
      const dec = e2ee.decrypt(message);
      return dec.ok ? (dec.payload.text ?? "") : "";
    }
    return message.content;
  }, [message]);
  const link = api.messageUrl(message.embed?.type === "share" ? Number(message.embed.id) : message.id);
  const canNativeShare = typeof navigator.share === "function";

  const targets = useMemo(() => {
    const q = query.trim().toLowerCase();
    return Object.values(state.channels)
      .map((c) => ({ c, title: channelTitle(c, state.users, state.me.id) }))
      .filter(({ title }) => !q || title.toLowerCase().includes(q))
      .sort((a, b) => Number(isDM(a.c)) - Number(isDM(b.c)) || a.title.localeCompare(b.title, "vi"));
  }, [state.channels, state.users, state.me.id, query]);

  const send = async (c: Channel) => {
    setBusy(c.id);
    setError(null);
    try {
      await shareMessage(message, c.id, mode);
      setDone((d) => [...d, c.id]);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không chia sẻ được.");
    } finally {
      setBusy(null);
    }
  };

  const [copyFailed, setCopyFailed] = useState(false);
  const copy = async (what: "link" | "text") => {
    const ok = await copyText(what === "link" ? link : text);
    setCopied(ok ? what : null);
    setCopyFailed(!ok);
    if (ok) setTimeout(() => setCopied(null), 1800);
  };

  const nativeShare = async () => {
    try {
      await navigator.share({ title: "Go Chat", text: text || undefined, url: canLink ? link : undefined });
    } catch {
      /* người dùng huỷ */
    }
  };

  const who = message.user_id === state.me.id ? "Bạn" : displayName(state.users[message.user_id] ?? message.user);

  return (
    <Dialog title="Chia sẻ tin nhắn" onClose={onClose}>
      <div className="share-source">
        <Avatar user={state.users[message.user_id] ?? message.user} size={28} />
        <div>
          <b>
            {who}
            {source && !fromDM ? <span> · #{source.name}</span> : null}
          </b>
          <ShareSummary message={message} text={text} />
        </div>
      </div>

      <div className="share-link">
        <input
          readOnly
          value={link}
          aria-label="Liên kết tới tin nhắn"
          onFocus={(e) => e.currentTarget.select()}
          onClick={(e) => e.currentTarget.select()}
        />
        <button type="button" className="btn btn-sm btn-primary" onClick={() => void copy("link")}>
          <Icon name={copied === "link" ? "check" : "link"} size={15} />
          {copied === "link" ? "Đã sao chép" : "Sao chép liên kết"}
        </button>
      </div>
      {copyFailed && (
        <p className="share-note is-warn" role="alert">
          Trình duyệt không cho tự sao chép (trang đang mở bằng http). Bấm vào ô link ở trên rồi nhấn Ctrl+C (điện
          thoại: nhấn giữ → Sao chép).
        </p>
      )}
      <div className="share-quick" role="group" aria-label="Chia sẻ nhanh">
        {text && (
          <button type="button" className="btn btn-sm" onClick={() => void copy("text")}>
            <Icon name="copy" size={15} />
            {copied === "text" ? "Đã sao chép" : "Sao chép nội dung"}
          </button>
        )}
        {canNativeShare && (
          <button type="button" className="btn btn-sm" onClick={() => void nativeShare()}>
            <Icon name="external" size={15} />
            Chia sẻ ra ngoài…
          </button>
        )}
      </div>
      {fromDM && (
        <p className="share-note">Tin nhắn riêng: liên kết chỉ mở được với hai người trong cuộc trò chuyện.</p>
      )}

      {canCopy && canLink && (
        <div className="share-mode" role="radiogroup" aria-label="Gửi dưới dạng">
          {(
            [
              ["copy", "Bản sao", "Gửi lại nội dung, ảnh, file như tin mới"],
              ["link", "Thẻ liên kết", "Thẻ trỏ về tin gốc, xem được bối cảnh"],
            ] as const
          ).map(([id, label, hint]) => (
            <label key={id} className={`share-mode-opt${mode === id ? " is-on" : ""}`}>
              <input type="radio" name="share-mode" checked={mode === id} onChange={() => setMode(id)} />
              <b>{label}</b>
              <small>{hint}</small>
            </label>
          ))}
        </div>
      )}
      {!canCopy && canLink && (
        <p className="share-note">
          {message.embed?.type === "game"
            ? "Gửi thẻ mời vào phòng chơi: ai ở trong kênh gốc bấm là vào chơi được."
            : "Gửi dạng thẻ liên kết trỏ về tin gốc."}
        </p>
      )}
      {!canCopy && !canLink && <FormError>Tin này không chia sẻ sang nơi khác được.</FormError>}

      {(canCopy || canLink) && (
        <>
          <input
            className="search"
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Tìm kênh hoặc người để gửi tới"
            aria-label="Tìm nơi gửi tới"
          />
          <FormError>{error}</FormError>
          <ul className="people-list share-targets">
            {targets.map(({ c, title }) => {
              const sent = done.includes(c.id);
              const peer = isDM(c) ? dmPeerId(c, state.me.id) : null;
              return (
                <li key={c.id}>
                  <div className="person">
                    {peer !== null ? (
                      <Avatar user={state.users[peer]} size={32} />
                    ) : (
                      <span className="forward-sigil">
                        <ChannelSigil channel={c} size={16} />
                      </span>
                    )}
                    <span className="person-text">
                      <span className="person-name">{isDM(c) ? title : channelLabel(c, state.users, state.me.id)}</span>
                      <span className="person-sub">{isDM(c) ? "Tin nhắn riêng" : `${c.member_count} thành viên`}</span>
                    </span>
                    <button
                      type="button"
                      className={`btn btn-sm${sent ? "" : " btn-primary"}`}
                      disabled={busy !== null || sent}
                      onClick={() => void send(c)}
                    >
                      {sent ? "Đã gửi" : busy === c.id ? "Đang gửi…" : "Gửi"}
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </Dialog>
  );
}

function ShareSummary({ message, text }: { message: Message; text: string }) {
  let label = text;
  if (message.poll) label = `📊 Bình chọn: ${message.poll.question}`;
  else if (message.embed?.type === "game") label = `🎮 Phòng chơi ${message.embed.title ?? ""}`;
  else if (message.embed?.type === "share") label = `↗ ${message.embed.text || "Tin được chia sẻ"}`;
  else if (message.embed?.type === "sticker") label = "Sticker";
  else if (message.embed?.type === "gif") label = "GIF";
  else if (!label && message.attachments?.length)
    label = `📎 ${message.attachments.map((a) => a.file_name).join(", ")}`;
  return <p className="share-summary">{label.length > 220 ? label.slice(0, 220) + "…" : label || "(tin nhắn)"}</p>;
}

/* ---------- Thẻ chia sẻ / thẻ link tới tin ---------- */

// mỗi tin chỉ hỏi máy chủ một lần trong 30 giây (nhiều thẻ cùng trỏ một tin)
const linkCache = new Map<number, { at: number; p: Promise<MessageLinkInfo> }>();

function useLinkInfo(id: number) {
  const { api } = useChat();
  const [info, setInfo] = useState<MessageLinkInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    let alive = true;
    let c = linkCache.get(id);
    if (!c || Date.now() - c.at > 30_000 || nonce > 0) {
      c = { at: Date.now(), p: api.messageLink(id) };
      linkCache.set(id, c);
    }
    c.p.then(
      (r) => alive && (setInfo(r), setError(null)),
      (err) => alive && setError(err instanceof ApiError ? err.message : "Không tải được tin gốc."),
    );
    return () => {
      alive = false;
    };
  }, [api, id, nonce]);
  return { info, error, reload: () => setNonce((n) => n + 1) };
}

function gameStatus(g: GameView): string {
  if (g.status === "lobby") return `Đang chờ người chơi · ${g.players.length}/${g.max_players}`;
  if (g.status === "playing") return `Đang chơi · ${g.players.length} người`;
  return g.result ? `Đã kết thúc · ${g.result}` : "Đã kết thúc";
}

/**
 * Thẻ trỏ tới tin gốc. embed: bản tóm tắt lúc chia sẻ (có thể không có khi chỉ là link dán vào tin).
 * Trạng thái hiện tại (game đang chờ mấy người, bình chọn bao nhiêu phiếu…) lấy trực tiếp nếu mình xem được.
 */
export function ShareCard({ id, embed }: { id: number; embed?: Embed }) {
  const { state, api, dispatch, openMessageLink } = useChat();
  const { info, error, reload } = useLinkInfo(id);
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState<string | null>(null);
  const live = info?.member ? info.message : undefined;

  const kind =
    embed?.kind ??
    (live?.poll ? "poll" : live?.embed?.type === "game" ? "game" : live?.attachments?.length ? "file" : "message");
  const k = KIND_LABEL[kind] ?? KIND_LABEL.message;
  const channelName = info?.channel.name ?? embed?.channel;
  const author = embed?.author ?? (live ? displayName(state.users[live.user_id] ?? live.user) : undefined);
  const title = embed?.title || (live?.embed?.type === "game" ? live.embed.title : undefined) || k.label;
  let text = embed?.text ?? "";
  if (live && !live.cipher) {
    if (live.poll) text = live.poll.question;
    else if (live.content) text = live.content.replace(/\n*_— [^\n]*_\s*$/, "");
  }
  if (text.length > 300) text = text.slice(0, 300) + "…";

  let status: string | null = null;
  if (live?.game) status = gameStatus(live.game);
  else if (live?.poll) status = `${live.poll.voters} người đã bình chọn${live.poll.closed_at ? " · đã kết thúc" : ""}`;

  const primary =
    kind === "game"
      ? live?.game?.status === "lobby"
        ? "Vào phòng chơi"
        : "Xem ván chơi"
      : kind === "poll"
        ? "Bình chọn"
        : "Xem tin gốc";

  const join = async () => {
    if (!info) return;
    setJoining(true);
    setJoinError(null);
    try {
      const ch = await api.joinChannel(info.channel.id);
      dispatch({ type: "channel.upsert", channel: { ...ch, unread_count: 0 } });
      linkCache.delete(id);
      reload();
      await openMessageLink(id);
    } catch (err) {
      setJoinError(err instanceof ApiError ? err.message : "Không tham gia được kênh.");
    } finally {
      setJoining(false);
    }
  };

  const preview = embed?.preview ?? live?.attachments?.find((a) => /^image\//.test(a.content_type))?.url;

  return (
    <div className={`share-card share-${kind}${error ? " is-gone" : ""}`}>
      <p className="share-from">
        <Icon name="forward" size={13} />
        {channelName ? `Chia sẻ từ #${channelName}` : "Tin được chia sẻ"}
        {author ? ` · ${author}` : ""}
      </p>
      <div className="share-body">
        {preview && !error && <img className="share-thumb" src={api.fileUrl({ url: preview }, false)} alt="" />}
        <div className="share-main">
          <p className="share-title">
            <span aria-hidden>{k.icon}</span> {title}
          </p>
          {text && <p className="share-text">{text}</p>}
          {status && <p className="share-status">{status}</p>}
        </div>
      </div>
      {error ? (
        <p className="share-note">{error}</p>
      ) : info && !info.member ? (
        <div className="share-actions">
          <span className="share-note">Bạn chưa ở trong #{info.channel.name}.</span>
          <button type="button" className="btn btn-sm btn-primary" disabled={joining} onClick={() => void join()}>
            {joining ? "Đang tham gia…" : "Tham gia kênh"}
          </button>
          {joinError && <FormError>{joinError}</FormError>}
        </div>
      ) : (
        <div className="share-actions">
          <button
            type="button"
            className="btn btn-sm btn-primary"
            disabled={!info}
            onClick={() => void openMessageLink(id)}
          >
            {primary}
          </button>
        </div>
      )}
    </div>
  );
}

/* ---------- Hộp thoại khi mở link mà chưa xem được tin ---------- */

export function MessageLinkDialog() {
  const { state, api, dispatch, openMessageLink } = useChat();
  const prompt = state.linkPrompt;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (!prompt) return null;
  const close = () => dispatch({ type: "link.prompt", prompt: null });
  const info = prompt.info;

  const join = async () => {
    if (!info) return;
    setBusy(true);
    setError(null);
    try {
      const ch = await api.joinChannel(info.channel.id);
      dispatch({ type: "channel.upsert", channel: { ...ch, unread_count: 0 } });
      close();
      linkCache.delete(prompt.id);
      await openMessageLink(prompt.id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không tham gia được kênh.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title="Mở tin nhắn" onClose={close}>
      {prompt.error ? (
        <p>{prompt.error}</p>
      ) : info ? (
        <p>
          Tin này nằm trong kênh <b>#{info.channel.name}</b>
          {info.channel.member_count ? ` (${info.channel.member_count} thành viên)` : ""}. Tham gia kênh để xem?
        </p>
      ) : null}
      <FormError>{error}</FormError>
      <div className="dialog-actions">
        <button type="button" className="btn" onClick={close}>
          Đóng
        </button>
        {info && !info.member && (
          <button type="button" className="btn btn-primary" disabled={busy} onClick={() => void join()}>
            {busy ? "Đang tham gia…" : "Tham gia và xem"}
          </button>
        )}
      </div>
    </Dialog>
  );
}
