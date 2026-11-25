import { useEffect, useState, type FormEvent } from "react";
import { ApiError, type Channel, type Integration } from "../lib/api";
import { copyText } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, EmptyState, FormError, Icon } from "./ui";

const errMsg = (err: unknown) => (err instanceof ApiError ? err.message : "Có lỗi xảy ra, thử lại sau.");

/** Bot tích hợp: tài khoản tên đuôi "_bot". */
export const isIntegrationBot = (u: { username?: string } | undefined) => !!u?.username?.endsWith("_bot");

/**
 * Quản lý bot của kênh (chủ kênh): tạo bot, lấy link webhook để dịch vụ ngoài gửi tin vào kênh,
 * đặt URL để bot trả lời khi được @nhắc tên, đổi mã, xoá.
 */
export function IntegrationsDialog({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const { api } = useChat();
  const [list, setList] = useState<Integration[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  // link webhook vừa tạo / vừa đổi mã: chỉ xem được lúc này
  const [fresh, setFresh] = useState<{ id: number; path: string } | null>(null);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    api
      .listIntegrations(channel.id)
      .then(setList)
      .catch((err) => setError(errMsg(err)));
  }, [api, channel.id]);

  const replace = (it: Integration) => setList((l) => (l ?? []).map((x) => (x.id === it.id ? { ...x, ...it } : x)));

  return (
    <Dialog title="Bot & tích hợp" onClose={onClose} wide className="dialog-top">
      <p className="integ-intro">
        Bot đăng tin vào <b>{channel.is_private ? "" : "#"}{channel.name}</b> khi một dịch vụ bên ngoài (CI/CD, n8n, cron,
        script…) gọi link webhook của nó, và có thể trả lời khi ai đó <b>@nhắc tên</b> bot.
      </p>
      <FormError>{error}</FormError>
      {!list && !error && <p className="muted">Đang tải…</p>}

      {list && list.length === 0 && !creating && (
        <EmptyState icon="bot" title="Kênh chưa có bot nào">
          Tạo một bot để nhận thông báo deploy, đơn hàng, cảnh báo máy chủ… ngay trong kênh.
        </EmptyState>
      )}

      {list && list.length > 0 && (
        <ul className="integ-list">
          {list.map((it) => (
            <IntegrationItem
              key={it.id}
              channel={channel}
              it={it}
              freshPath={fresh?.id === it.id ? fresh.path : null}
              onChanged={replace}
              onRotated={(path) => setFresh({ id: it.id, path })}
              onDeleted={() => setList((l) => (l ?? []).filter((x) => x.id !== it.id))}
            />
          ))}
        </ul>
      )}

      {creating ? (
        <CreateIntegration
          channel={channel}
          onCancel={() => setCreating(false)}
          onCreated={(it) => {
            setList((l) => [...(l ?? []), it]);
            if (it.hook_path) setFresh({ id: it.id, path: it.hook_path });
            setCreating(false);
          }}
        />
      ) : (
        list && (
          <div className="dialog-actions dialog-actions-start">
            <button type="button" className="btn btn-primary" onClick={() => setCreating(true)}>
              <Icon name="plus" size={16} />
              Thêm bot
            </button>
          </div>
        )
      )}
    </Dialog>
  );
}

function CreateIntegration({
  channel,
  onCreated,
  onCancel,
}: {
  channel: Channel;
  onCreated: (it: Integration) => void;
  onCancel: () => void;
}) {
  const { api } = useChat();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onCreated(await api.createIntegration(channel.id, name.trim(), url.trim()));
    } catch (err) {
      setError(errMsg(err));
      setBusy(false);
    }
  };

  return (
    <form className="integ-create" onSubmit={submit}>
      <label className="field">
        <span>Tên bot</span>
        <input
          autoFocus
          data-autofocus
          value={name}
          maxLength={40}
          placeholder="Ví dụ: CI Bot, Đơn hàng, Cảnh báo server"
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <label className="field">
        <span>
          URL trả lời khi được @nhắc tên <small>(không bắt buộc)</small>
        </span>
        <input
          value={url}
          inputMode="url"
          spellCheck={false}
          placeholder="https://bot-cua-ban.example.com/gochat"
          onChange={(e) => setUrl(e.target.value)}
        />
        <small>Bỏ trống nếu bot chỉ cần đăng thông báo vào kênh.</small>
      </label>
      <FormError>{error}</FormError>
      <div className="dialog-actions">
        <button type="button" className="btn" onClick={onCancel}>
          Huỷ
        </button>
        <button className="btn btn-primary" disabled={busy || !name.trim()}>
          {busy ? "Đang tạo…" : "Tạo bot"}
        </button>
      </div>
    </form>
  );
}

function IntegrationItem({
  channel,
  it,
  freshPath,
  onChanged,
  onRotated,
  onDeleted,
}: {
  channel: Channel;
  it: Integration;
  freshPath: string | null;
  onChanged: (it: Integration) => void;
  onRotated: (path: string) => void;
  onDeleted: () => void;
}) {
  const { api, state, serverUrl } = useChat();
  const [open, setOpen] = useState(!!freshPath);
  const [url, setUrl] = useState(it.outgoing_url);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<"rotate" | "delete" | null>(null);
  const [showSecret, setShowSecret] = useState(false);
  useEffect(() => {
    if (freshPath) setOpen(true);
  }, [freshPath]);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    setNote(null);
    try {
      await fn();
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
      setConfirm(null);
    }
  };
  const copy = async (text: string, what: string) => setNote((await copyText(text)) ? `Đã sao chép ${what}.` : null);

  const bot = state.users[it.bot_user_id] ?? {
    id: it.bot_user_id,
    username: it.bot_username,
    display_name: it.bot_name,
    avatar: null,
    status_text: "",
  };
  const hookUrl = freshPath ? new URL(freshPath, serverUrl || window.location.origin).toString() : null;

  return (
    <li className={`integ-item${open ? " is-open" : ""}`}>
      <button type="button" className="integ-head" onClick={() => setOpen(!open)} aria-expanded={open}>
        <Avatar user={bot} size={34} />
        <span className="integ-name">
          <b>
            {it.bot_name} <span className="bot-tag is-integration">BOT</span>
          </b>
          <small>
            @{it.bot_username}
            {it.outgoing_url ? " · trả lời khi được nhắc" : " · chỉ đăng tin"}
          </small>
        </span>
        <Icon name={open ? "chevronUp" : "chevronDown"} size={16} />
      </button>

      {open && (
        <div className="integ-body">
          {hookUrl ? (
            <div className="integ-hook">
              <p className="integ-hook-title">
                <Icon name="link" size={15} />
                Link webhook (chỉ hiện lần này, hãy lưu lại)
              </p>
              <div className="share-link">
                <input readOnly value={hookUrl} aria-label="Link webhook" onFocus={(e) => e.currentTarget.select()} />
                <button type="button" className="btn btn-sm btn-primary" onClick={() => void copy(hookUrl, "link")}>
                  <Icon name="copy" size={15} />
                  Sao chép
                </button>
              </div>
              <p className="integ-label">Thử ngay:</p>
              <pre className="integ-code">{`curl -X POST '${hookUrl}' \\\n  -H 'Content-Type: application/json' \\\n  -d '{"text": "Xin chào từ ${it.bot_name} 👋"}'`}</pre>
            </div>
          ) : (
            <p className="integ-help">
              Link webhook chỉ hiện một lần lúc tạo. Mất link thì bấm <b>Đổi mã</b> để lấy link mới (link cũ ngừng hoạt động).
            </p>
          )}

          <form
            className="integ-url"
            onSubmit={(e) => {
              e.preventDefault();
              void run(async () => {
                onChanged(await api.updateIntegration(channel.id, it.id, url.trim()));
                setNote(url.trim() ? "Đã lưu URL trả lời." : "Đã tắt trả lời khi được nhắc.");
              });
            }}
          >
            <label className="field">
              <span>URL trả lời khi được @{it.bot_username}</span>
              <div className="share-link">
                <input
                  value={url}
                  inputMode="url"
                  spellCheck={false}
                  placeholder="Bỏ trống: bot chỉ đăng tin"
                  onChange={(e) => setUrl(e.target.value)}
                />
                <button className="btn btn-sm" disabled={busy || url.trim() === it.outgoing_url}>
                  Lưu
                </button>
              </div>
            </label>
          </form>

          {it.signing_secret && (
            <div className="integ-secret">
              <p className="integ-label">
                Khoá ký (bên nhận kiểm tra header <code>X-GoChat-Signature</code>)
              </p>
              <div className="share-link">
                <input
                  readOnly
                  type={showSecret ? "text" : "password"}
                  value={it.signing_secret}
                  aria-label="Khoá ký"
                  onFocus={(e) => e.currentTarget.select()}
                />
                <button type="button" className="btn btn-sm" onClick={() => setShowSecret(!showSecret)}>
                  {showSecret ? "Ẩn" : "Hiện"}
                </button>
                <button type="button" className="btn btn-sm" onClick={() => void copy(it.signing_secret, "khoá ký")}>
                  Sao chép
                </button>
              </div>
            </div>
          )}

          <details className="integ-doc">
            <summary>Bot nhận gì và trả lời thế nào?</summary>
            <p>
              Khi ai đó nhắn <code>@{it.bot_username} …</code>, máy chủ gửi <code>POST</code> JSON tới URL trên:
            </p>
            <pre className="integ-code">{`{
  "type": "mention",
  "timestamp": 1732500000,
  "channel": { "id": ${channel.id}, "name": "${channel.name}" },
  "message": { "id": 123, "text": "@${it.bot_username} deploy bản mới", "parent_id": null },
  "user": { "id": 2, "username": "nam", "display_name": "Nam" },
  "bot": { "id": ${it.bot_user_id}, "username": "${it.bot_username}" }
}`}</pre>
            <p>
              Trả về <code>{`{"text": "…"}`}</code> thì bot đăng câu đó ngay dưới tin được nhắc. Chữ ký:{" "}
              <code>X-GoChat-Signature: sha256=HMAC_SHA256(khoá ký, timestamp + "." + body)</code>, với timestamp lấy
              từ header <code>X-GoChat-Timestamp</code>.
            </p>
          </details>

          <FormError>{error}</FormError>
          {note && (
            <p className="integ-note" role="status">
              {note}
            </p>
          )}
          <div className="integ-actions">
            {confirm === "rotate" ? (
              <>
                <span className="integ-confirm">Link cũ sẽ ngừng hoạt động.</span>
                <button
                  type="button"
                  className="btn btn-sm btn-primary"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const r = await api.rotateIntegrationToken(channel.id, it.id);
                      if (r.hook_path) onRotated(r.hook_path);
                    })
                  }
                >
                  Đổi mã
                </button>
                <button type="button" className="btn btn-sm" onClick={() => setConfirm(null)}>
                  Thôi
                </button>
              </>
            ) : confirm === "delete" ? (
              <>
                <span className="integ-confirm">Bot rời kênh, tin cũ của bot vẫn giữ.</span>
                <button
                  type="button"
                  className="btn btn-sm btn-danger"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      await api.deleteIntegration(channel.id, it.id);
                      onDeleted();
                    })
                  }
                >
                  Xoá bot
                </button>
                <button type="button" className="btn btn-sm" onClick={() => setConfirm(null)}>
                  Thôi
                </button>
              </>
            ) : (
              <>
                <button type="button" className="btn btn-sm" onClick={() => setConfirm("rotate")}>
                  <Icon name="refresh" size={15} />
                  Đổi mã
                </button>
                <button type="button" className="btn btn-sm btn-danger-outline" onClick={() => setConfirm("delete")}>
                  <Icon name="trash" size={15} />
                  Xoá
                </button>
              </>
            )}
          </div>
        </div>
      )}
    </li>
  );
}
