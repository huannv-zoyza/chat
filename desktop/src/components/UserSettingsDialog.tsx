import { useState, type FormEvent } from "react";
import { ApiError } from "../lib/api";
import { applyTheme, getTheme, type ThemePref } from "../lib/theme";
import { servedByServer } from "../lib/platform";
import {
  ensureNotifyPermission,
  getNotifyPref,
  notificationsSupported,
  setNotifyPref,
  showNotification,
} from "../lib/notify";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, FormError } from "./ui";

type Tab = "profile" | "account" | "app";

const TABS: { id: Tab; label: string }[] = [
  { id: "profile", label: "Hồ sơ" },
  { id: "account", label: "Tài khoản" },
  { id: "app", label: "Ứng dụng" },
];

const errMsg = (err: unknown) => (err instanceof ApiError ? err.message : "Có lỗi xảy ra, thử lại sau.");

export function UserSettingsDialog({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("profile");

  return (
    <Dialog title="Cài đặt" onClose={onClose} wide>
      <div className="settings">
        <div className="tabs" role="tablist" aria-label="Mục cài đặt">
          {TABS.map((t) => (
            <button
              key={t.id}
              role="tab"
              id={`tab-${t.id}`}
              aria-selected={tab === t.id}
              aria-controls={`panel-${t.id}`}
              className="tab"
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>
        <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`} className="tab-panel">
          {tab === "profile" && <ProfileTab />}
          {tab === "account" && <AccountTab />}
          {tab === "app" && <AppTab />}
        </div>
      </div>
    </Dialog>
  );
}

/* ---------- Hồ sơ ---------- */

function ProfileTab() {
  const { api, dispatch, state } = useChat();
  const me = state.me;
  const [displayName, setDisplayName] = useState(me.display_name);
  const [statusText, setStatusText] = useState(me.status_text);
  const [avatar, setAvatar] = useState(me.avatar ?? "");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  const preview = { ...me, display_name: displayName || me.username, avatar: avatar.trim() || null };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const user = await api.updateMe({ display_name: displayName, status_text: statusText, avatar });
      dispatch({ type: "me.updated", me: user });
      setSaved(true);
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="dialog-form" onSubmit={submit}>
      <div className="profile-preview">
        <Avatar user={preview} size={56} />
        <div>
          <div className="profile-preview-name">{preview.display_name}</div>
          <div className="muted">{statusText || "@" + me.username}</div>
        </div>
      </div>

      <label className="field">
        <span>Tên hiển thị</span>
        <input value={displayName} maxLength={64} onChange={(e) => setDisplayName(e.target.value)} />
      </label>
      <label className="field">
        <span>Trạng thái</span>
        <input
          value={statusText}
          maxLength={140}
          onChange={(e) => setStatusText(e.target.value)}
          placeholder="Ví dụ: Đang họp tới 15h"
        />
      </label>
      <label className="field">
        <span>Ảnh đại diện (đường dẫn ảnh)</span>
        <input
          value={avatar}
          onChange={(e) => setAvatar(e.target.value)}
          placeholder="https://…"
          spellCheck={false}
        />
        <small>Để trống để dùng chữ cái đầu của tên.</small>
      </label>

      <FormError>{error}</FormError>
      <div className="dialog-actions">
        {saved && (
          <span className="saved-note" role="status">
            Đã lưu hồ sơ.
          </span>
        )}
        <button className="btn btn-primary" disabled={busy}>
          {busy ? "Đang lưu…" : "Lưu hồ sơ"}
        </button>
      </div>
    </form>
  );
}

/* ---------- Tài khoản ---------- */

function AccountTab() {
  const { api, state, onLogout } = useChat();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaved(false);
    if (next !== confirm) return setError("Hai lần nhập mật khẩu mới không khớp.");
    setBusy(true);
    try {
      await api.changePassword({ current_password: current, new_password: next });
      setCurrent("");
      setNext("");
      setConfirm("");
      setSaved(true);
    } catch (err) {
      setError(errMsg(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="dialog-form">
      <dl className="info-list">
        <dt>Tên đăng nhập</dt>
        <dd>@{state.me.username}</dd>
        <dt>Email</dt>
        <dd>{state.me.email}</dd>
      </dl>

      <form className="dialog-form" onSubmit={submit}>
        <h3 className="section-title">Đổi mật khẩu</h3>
        <label className="field">
          <span>Mật khẩu hiện tại</span>
          <input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        </label>
        <label className="field">
          <span>Mật khẩu mới</span>
          <input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
          <small>Ít nhất 8 ký tự. Các thiết bị khác sẽ bị đăng xuất.</small>
        </label>
        <label className="field">
          <span>Nhập lại mật khẩu mới</span>
          <input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </label>
        <FormError>{error}</FormError>
        <div className="dialog-actions">
          {saved && (
            <span className="saved-note" role="status">
              Đã đổi mật khẩu.
            </span>
          )}
          <button className="btn btn-primary" disabled={busy || !current || !next}>
            Đổi mật khẩu
          </button>
        </div>
      </form>

      <section className="danger-zone">
        <h3>Đăng xuất</h3>
        <p>Đăng xuất khỏi Go Chat trên máy này.</p>
        <button type="button" className="btn btn-danger-outline" onClick={onLogout}>
          Đăng xuất
        </button>
      </section>
    </div>
  );
}

/* ---------- Ứng dụng ---------- */

const THEMES: { id: ThemePref; label: string }[] = [
  { id: "system", label: "Theo hệ thống" },
  { id: "light", label: "Sáng" },
  { id: "dark", label: "Tối" },
];

function AppTab() {
  const { serverUrl } = useChat();
  const [theme, setTheme] = useState<ThemePref>(getTheme);
  const [notify, setNotify] = useState(getNotifyPref);
  const [notifyMsg, setNotifyMsg] = useState<string | null>(null);
  const supported = notificationsSupported();

  const toggleNotify = async (on: boolean) => {
    setNotify(on);
    setNotifyPref(on);
    setNotifyMsg(null);
    if (on && !(await ensureNotifyPermission())) {
      setNotifyMsg("Hệ thống chưa cho phép hiện thông báo. Hãy bật quyền thông báo cho Go Chat.");
    }
  };

  return (
    <div className="dialog-form">
      <fieldset className="field">
        <legend>Giao diện</legend>
        <div className="segmented">
          {THEMES.map((t) => (
            <label key={t.id} className={`segment${theme === t.id ? " is-on" : ""}`}>
              <input
                type="radio"
                name="theme"
                value={t.id}
                checked={theme === t.id}
                onChange={() => {
                  setTheme(t.id);
                  applyTheme(t.id);
                }}
              />
              {t.label}
            </label>
          ))}
        </div>
      </fieldset>

      <fieldset className="field">
        <legend>Thông báo</legend>
        {supported ? (
          <>
            <label className="check">
              <input type="checkbox" checked={notify} onChange={(e) => void toggleNotify(e.target.checked)} />
              Báo khi có người nhắc tên tôi (@tên hoặc @all)
            </label>
            <div>
              <button
                type="button"
                className="btn"
                disabled={!notify}
                onClick={() => void showNotification("Go Chat", "Thông báo đang hoạt động.")}
              >
                Gửi thử thông báo
              </button>
            </div>
            {notifyMsg && <small className="form-error">{notifyMsg}</small>}
          </>
        ) : (
          <small>
            Trình duyệt chỉ cho hiện thông báo khi mở Go Chat qua HTTPS hoặc localhost. Bạn vẫn thấy tin nhắc tên được tô
            sáng và đếm riêng ở danh sách kênh; muốn có thông báo hệ thống, hãy dùng app desktop.
          </small>
        )}
      </fieldset>

      <dl className="info-list">
        <dt>Máy chủ</dt>
        <dd>{serverUrl}</dd>
      </dl>
      {!servedByServer && (
        <p className="muted">Muốn đổi máy chủ, hãy đăng xuất rồi chọn máy chủ khác ở màn đăng nhập.</p>
      )}
    </div>
  );
}
