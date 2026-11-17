import { useEffect, useState, type FormEvent } from "react";
import { ApiError, type Channel, parseInviteCode } from "../lib/api";
import { useChat } from "../state/ChatProvider";
import { copyText, displayName, formatDay } from "../lib/format";
import { Dialog, FormError, Icon } from "./ui";

const errMsg = (err: unknown) => (err instanceof ApiError ? err.message : "Có lỗi xảy ra, thử lại sau.");

/** Xem trước tên kênh giống cách server chuẩn hoá: chữ thường, khoảng trắng thành "-". */
function previewName(s: string) {
  return s.trim().toLowerCase().replace(/^#+/, "").split(/\s+/).filter(Boolean).join("-");
}

/** Ký hiệu đứng trước tên kênh: # cho kênh công khai, ổ khoá cho kênh riêng tư. */
export function ChannelSigil({ channel, size = 16 }: { channel: Pick<Channel, "is_private">; size?: number }) {
  return channel.is_private ? (
    <span className="sigil-lock" aria-label="Kênh riêng tư">
      <Icon name="lock" size={size} />
    </span>
  ) : (
    <span aria-hidden>#</span>
  );
}

/* ---------- Chọn công khai / riêng tư ---------- */

function VisibilityField({ value, onChange }: { value: boolean; onChange: (isPrivate: boolean) => void }) {
  return (
    <fieldset className="field">
      <legend>Ai được vào kênh</legend>
      <div className="choice-list">
        <label className={`choice${!value ? " is-on" : ""}`}>
          <input type="radio" name="visibility" checked={!value} onChange={() => onChange(false)} />
          <span className="choice-icon" aria-hidden>
            #
          </span>
          <span>
            <strong>Công khai</strong>
            <small>Mọi người thấy kênh trong “Khám phá kênh” và tự tham gia được.</small>
          </span>
        </label>
        <label className={`choice${value ? " is-on" : ""}`}>
          <input type="radio" name="visibility" checked={value} onChange={() => onChange(true)} />
          <span className="choice-icon" aria-hidden>
            <Icon name="lock" size={16} />
          </span>
          <span>
            <strong>Riêng tư</strong>
            <small>Kênh bị ẩn với người ngoài, chỉ ai có link mời mới vào được.</small>
          </span>
        </label>
      </div>
    </fieldset>
  );
}

/* ---------- Tạo kênh ---------- */

export function CreateChannelDialog({ onClose }: { onClose: () => void }) {
  const { api, dispatch } = useChat();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [isPrivate, setIsPrivate] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const preview = previewName(name);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!preview) return setError("Cần đặt tên cho kênh.");
    setBusy(true);
    setError(null);
    try {
      const ch = await api.createChannel({ name, description, is_private: isPrivate });
      dispatch({ type: "channel.upsert", channel: ch });
      dispatch({ type: "channel.select", channelId: ch.id });
      onClose();
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  };

  return (
    <Dialog title="Tạo kênh" onClose={onClose}>
      <form className="dialog-form" onSubmit={submit}>
        <label className="field">
          <span>Tên kênh</span>
          <div className="input-prefix">
            <span aria-hidden>{isPrivate ? <Icon name="lock" size={16} /> : "#"}</span>
            <input autoFocus value={name} maxLength={40} onChange={(e) => setName(e.target.value)} placeholder="du-an-moi" />
          </div>
          {name && preview !== name.trim() && <small>Sẽ được lưu là {preview}</small>}
        </label>
        <label className="field">
          <span>Mô tả (không bắt buộc)</span>
          <textarea
            rows={2}
            maxLength={500}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Kênh này dùng để làm gì?"
          />
        </label>
        <VisibilityField value={isPrivate} onChange={setIsPrivate} />
        <FormError>{error}</FormError>
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose}>
            Huỷ
          </button>
          <button className="btn btn-primary" disabled={busy}>
            {busy ? "Đang tạo…" : "Tạo kênh"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}

/* ---------- Khám phá kênh (chỉ kênh công khai) ---------- */

export function BrowseChannelsDialog({
  onClose,
  onCreate,
  onJoinByLink,
}: {
  onClose: () => void;
  onCreate: () => void;
  onJoinByLink: () => void;
}) {
  const { api, dispatch } = useChat();
  const [list, setList] = useState<Channel[] | null>(null);
  const [query, setQuery] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [joining, setJoining] = useState<number | null>(null);

  useEffect(() => {
    api
      .discover()
      .then(setList)
      .catch((err) => setError(errMsg(err)));
  }, [api]);

  const join = async (c: Channel) => {
    setJoining(c.id);
    setError(null);
    try {
      const ch = await api.joinChannel(c.id);
      dispatch({ type: "channel.upsert", channel: { ...ch, unread_count: 0 } });
      dispatch({ type: "channel.select", channelId: ch.id });
      onClose();
    } catch (err) {
      setError(errMsg(err));
      setJoining(null);
    }
  };

  const q = query.trim().toLowerCase();
  const shown = list?.filter((c) => !q || c.name.includes(q) || c.description.toLowerCase().includes(q));

  return (
    <Dialog title="Khám phá kênh" onClose={onClose} wide>
      <input
        className="search"
        autoFocus
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Tìm theo tên hoặc mô tả"
        aria-label="Tìm kênh"
      />
      <FormError>{error}</FormError>

      {!list && !error && <p className="muted">Đang tải…</p>}

      {list && list.length === 0 && (
        <div className="empty-block">
          <p>Bạn đã ở trong mọi kênh công khai hiện có.</p>
          <button
            className="btn btn-primary"
            onClick={() => {
              onClose();
              onCreate();
            }}
          >
            Tạo kênh mới
          </button>
        </div>
      )}

      {shown && shown.length > 0 && (
        <ul className="browse-list">
          {shown.map((c) => (
            <li key={c.id} className="browse-item">
              <div className="browse-text">
                <span className="browse-name">#{c.name}</span>
                <span className="browse-meta">{c.member_count} thành viên</span>
                {c.description && <p className="browse-desc">{c.description}</p>}
              </div>
              <button className="btn btn-primary" onClick={() => join(c)} disabled={joining !== null}>
                {joining === c.id ? "Đang vào…" : "Tham gia"}
              </button>
            </li>
          ))}
        </ul>
      )}
      {shown && list && list.length > 0 && shown.length === 0 && <p className="muted">Không có kênh nào khớp “{query}”.</p>}

      <p className="browse-footnote">
        <Icon name="lock" size={14} />
        Kênh riêng tư không hiện ở đây.{" "}
        <button
          type="button"
          className="link-btn"
          onClick={() => {
            onClose();
            onJoinByLink();
          }}
        >
          Có link mời?
        </button>
      </p>
    </Dialog>
  );
}

/* ---------- Link mời ---------- */

/** Ô hiển thị link mời + nút sao chép; chủ kênh có thêm nút tạo link mới. */
function InviteLinkBox({ channel }: { channel: Channel }) {
  const { api, dispatch, state } = useChat();
  const [copied, setCopied] = useState(false);
  const [confirmReset, setConfirmReset] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const isOwner = channel.owner_id === state.me.id;
  const link = channel.invite_code ? api.inviteLink(channel.invite_code) : "";

  const copy = async () => {
    setCopied(await copyText(link));
    window.setTimeout(() => setCopied(false), 2000);
  };

  const reset = async () => {
    setError(null);
    try {
      const ch = await api.regenerateInvite(channel.id);
      dispatch({ type: "channel.upsert", channel: ch, keepUnread: true });
      setConfirmReset(false);
    } catch (err) {
      setError(errMsg(err));
    }
  };

  if (!link) return null;
  return (
    <div className="field">
      <span>Link mời</span>
      <div className="invite-box">
        <input readOnly value={link} onFocus={(e) => e.currentTarget.select()} aria-label="Link mời" />
        <button type="button" className="btn btn-primary" onClick={copy}>
          <Icon name={copied ? "send" : "copy"} size={16} />
          {copied ? "Đã chép" : "Sao chép"}
        </button>
      </div>
      <small>
        {channel.is_private
          ? "Ai có link này đều vào được kênh. Chỉ gửi cho người bạn muốn mời."
          : "Gửi link này để mời người khác vào kênh."}{" "}
        Mở trên trình duyệt sẽ vào bản web; trong app desktop chọn “Tham gia bằng link mời” rồi dán link.
      </small>
      {isOwner &&
        (confirmReset ? (
          <div className="dialog-actions dialog-actions-start">
            <button type="button" className="btn btn-danger" onClick={reset}>
              Tạo link mới
            </button>
            <button type="button" className="btn" onClick={() => setConfirmReset(false)}>
              Giữ link cũ
            </button>
            <small>Link cũ sẽ không dùng được nữa.</small>
          </div>
        ) : (
          <div>
            <button type="button" className="link-btn" onClick={() => setConfirmReset(true)}>
              Lỡ để lộ link? Tạo link mới
            </button>
          </div>
        ))}
      <FormError>{error}</FormError>
    </div>
  );
}

export function InviteDialog({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  return (
    <Dialog title={`Mời người vào ${channel.is_private ? "" : "#"}${channel.name}`} onClose={onClose}>
      <div className="dialog-form">
        <InviteLinkBox channel={channel} />
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose}>
            Xong
          </button>
        </div>
      </div>
    </Dialog>
  );
}

/** Dán link (hoặc mã) mời → xem trước kênh → tham gia. */
export function JoinByLinkDialog({ initialCode, onClose }: { initialCode?: string; onClose: () => void }) {
  const { api, dispatch } = useChat();
  const [input, setInput] = useState(initialCode ?? "");
  const [preview, setPreview] = useState<{ channel: Channel; is_member: boolean } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const code = parseInviteCode(input);

  // Dán link hợp lệ là tự xem trước luôn.
  useEffect(() => {
    setPreview(null);
    setError(null);
    if (!code) return;
    let cancelled = false;
    api
      .invitePreview(code)
      .then((p) => !cancelled && setPreview(p))
      .catch((err) => !cancelled && setError(errMsg(err)));
    return () => {
      cancelled = true;
    };
  }, [api, code]);

  const open = (ch: Channel) => {
    dispatch({ type: "channel.upsert", channel: { ...ch, unread_count: 0 } });
    dispatch({ type: "channel.select", channelId: ch.id });
    onClose();
  };

  const join = async (e: FormEvent) => {
    e.preventDefault();
    if (!code) return setError("Dán link mời dạng …/invite/<mã>.");
    if (preview?.is_member) return open(preview.channel);
    setBusy(true);
    try {
      open(await api.joinByInvite(code));
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  };

  return (
    <Dialog title="Tham gia bằng link mời" onClose={onClose}>
      <form className="dialog-form" onSubmit={join}>
        <label className="field">
          <span>Link mời</span>
          <input
            autoFocus
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="http://…/invite/…"
            spellCheck={false}
          />
          {input && !code && <small>Link chưa đúng dạng. Link mời có dạng …/invite/ và 24 ký tự.</small>}
        </label>

        {preview && (
          <div className="invite-preview">
            <span className="invite-preview-sigil">
              <ChannelSigil channel={preview.channel} size={20} />
            </span>
            <div>
              <strong>{preview.channel.name}</strong>
              <span className="muted">
                {preview.channel.is_private ? "Kênh riêng tư" : "Kênh công khai"} · {preview.channel.member_count}{" "}
                thành viên
              </span>
              {preview.channel.description && <p>{preview.channel.description}</p>}
            </div>
          </div>
        )}

        <FormError>{error}</FormError>
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose}>
            Huỷ
          </button>
          <button className="btn btn-primary" disabled={!preview || busy}>
            {preview?.is_member ? "Mở kênh" : busy ? "Đang vào…" : "Tham gia"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}

/* ---------- Cài đặt kênh ---------- */

export function ChannelSettingsDialog({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const { api, dispatch, state } = useChat();
  const isOwner = channel.owner_id === state.me.id;
  const [name, setName] = useState(channel.name);
  const [description, setDescription] = useState(channel.description);
  const [isPrivate, setIsPrivate] = useState(channel.is_private);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const owner = state.users[channel.owner_id];

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const ch = await api.updateChannel(channel.id, { name, description, is_private: isPrivate });
      dispatch({ type: "channel.upsert", channel: ch, keepUnread: true });
      onClose();
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  };

  const run = async (action: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await action();
      dispatch({ type: "channel.removed", channelId: channel.id });
      onClose();
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  };

  return (
    <Dialog title={`Cài đặt ${channel.is_private ? "" : "#"}${channel.name}`} onClose={onClose} wide>
      {isOwner ? (
        <form className="dialog-form" onSubmit={save}>
          <label className="field">
            <span>Tên kênh</span>
            <div className="input-prefix">
              <span aria-hidden>{isPrivate ? <Icon name="lock" size={16} /> : "#"}</span>
              <input value={name} maxLength={40} onChange={(e) => setName(e.target.value)} />
            </div>
          </label>
          <label className="field">
            <span>Mô tả</span>
            <textarea rows={2} maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} />
          </label>
          <VisibilityField value={isPrivate} onChange={setIsPrivate} />
          <FormError>{error}</FormError>
          <div className="dialog-actions">
            <button type="button" className="btn" onClick={onClose}>
              Huỷ
            </button>
            <button className="btn btn-primary" disabled={busy}>
              Lưu thay đổi
            </button>
          </div>

          <section className="settings-section">
            <InviteLinkBox channel={channel} />
          </section>

          <section className="danger-zone">
            <h3>Xoá kênh</h3>
            <p>Toàn bộ tin nhắn và file của kênh sẽ bị xoá vĩnh viễn với mọi thành viên.</p>
            {confirmDelete ? (
              <div className="dialog-actions dialog-actions-start">
                <button
                  type="button"
                  className="btn btn-danger"
                  disabled={busy}
                  onClick={() => run(() => api.deleteChannel(channel.id))}
                >
                  Xoá vĩnh viễn
                </button>
                <button type="button" className="btn" onClick={() => setConfirmDelete(false)}>
                  Giữ lại kênh
                </button>
              </div>
            ) : (
              <button type="button" className="btn btn-danger-outline" onClick={() => setConfirmDelete(true)}>
                Xoá kênh…
              </button>
            )}
          </section>
        </form>
      ) : (
        <div className="dialog-form">
          <dl className="info-list">
            <dt>Loại kênh</dt>
            <dd>{channel.is_private ? "Riêng tư, chỉ vào bằng link mời" : "Công khai"}</dd>
            <dt>Mô tả</dt>
            <dd>{channel.description || "Chưa có mô tả."}</dd>
            <dt>Người tạo</dt>
            <dd>{owner ? displayName(owner) : "—"}</dd>
            <dt>Ngày tạo</dt>
            <dd>{formatDay(channel.created_at)}</dd>
            <dt>Thành viên</dt>
            <dd>{channel.member_count} người</dd>
          </dl>
          <InviteLinkBox channel={channel} />
          <FormError>{error}</FormError>
          <div className="dialog-actions">
            <button type="button" className="btn" onClick={onClose}>
              Đóng
            </button>
            <button
              type="button"
              className="btn btn-danger-outline"
              disabled={busy}
              onClick={() => run(() => api.leaveChannel(channel.id))}
            >
              Rời kênh
            </button>
          </div>
        </div>
      )}
    </Dialog>
  );
}
