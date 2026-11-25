import { useState, type FormEvent } from "react";
import { ApiError, createApi, normalizeServerUrl, type User } from "../lib/api";
import { FormError } from "./ui";

type Mode = "login" | "register";

type Props = {
  initialServer: string;
  /** true: không cho đổi máy chủ (bản web do chính server phục vụ). */
  fixedServer?: boolean;
  notice?: string | null;
  onAuthenticated: (serverUrl: string, token: string, user: User) => void;
};

export function AuthScreen({ initialServer, fixedServer, notice, onAuthenticated }: Props) {
  const [mode, setMode] = useState<Mode>("login");
  const [server, setServer] = useState(initialServer);
  const [editServer, setEditServer] = useState(false);
  const [login, setLogin] = useState("");
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    const serverUrl = normalizeServerUrl(server);
    if (!serverUrl) {
      setEditServer(true);
      return setError("Địa chỉ máy chủ không hợp lệ, ví dụ: localhost:4000");
    }
    const api = createApi({ serverUrl });
    setBusy(true);
    try {
      const res =
        mode === "login"
          ? await api.login({ login: login.trim(), password })
          : await api.register({ username: username.trim(), email: email.trim(), password, display_name: displayName.trim() });
      onAuthenticated(serverUrl, res.token, res.user);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Có lỗi xảy ra, thử lại sau.");
      if (err instanceof ApiError && err.status === 0 && !fixedServer) setEditServer(true);
    } finally {
      setBusy(false);
    }
  };

  const switchMode = (m: Mode) => {
    setMode(m);
    setError(null);
  };

  return (
    <main className="auth">
      <section className="auth-brand" aria-hidden>
        <span className="auth-mark">#</span>
        <p className="auth-tagline">Trò chuyện theo kênh cho cả nhóm, ngay trên máy tính của bạn.</p>
      </section>

      <section className="auth-panel">
        <form className="auth-form" onSubmit={submit} noValidate>
          <h1 className="auth-title">{mode === "login" ? "Đăng nhập Go Chat" : "Tạo tài khoản"}</h1>

          {notice && <p className="auth-notice">{notice}</p>}

          {mode === "login" ? (
            <label className="field">
              <span>Tên đăng nhập hoặc email</span>
              <input autoFocus data-autofocus autoComplete="username" value={login} onChange={(e) => setLogin(e.target.value)} />
            </label>
          ) : (
            <>
              <label className="field">
                <span>Tên hiển thị</span>
                <input
                  autoFocus data-autofocus
                  value={displayName}
                  maxLength={64}
                  onChange={(e) => setDisplayName(e.target.value)}
                  placeholder="Ví dụ: Phạm Quyên"
                />
              </label>
              <label className="field">
                <span>Tên đăng nhập</span>
                <input
                  autoComplete="username"
                  value={username}
                  maxLength={32}
                  onChange={(e) => setUsername(e.target.value.toLowerCase())}
                  placeholder="quyenpv"
                  spellCheck={false}
                />
                <small>Chữ thường không dấu, số, dấu chấm hoặc gạch dưới.</small>
              </label>
              <label className="field">
                <span>Email</span>
                <input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />
              </label>
            </>
          )}

          <label className="field">
            <span>Mật khẩu</span>
            <input
              type="password"
              autoComplete={mode === "login" ? "current-password" : "new-password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            {mode === "register" && <small>Ít nhất 8 ký tự.</small>}
          </label>

          {fixedServer ? null : editServer ? (
            <label className="field">
              <span>Máy chủ</span>
              <input value={server} onChange={(e) => setServer(e.target.value)} spellCheck={false} />
            </label>
          ) : (
            <p className="auth-server">
              Máy chủ <strong>{server.replace(/^https?:\/\//, "")}</strong>
              <button type="button" className="link-btn" onClick={() => setEditServer(true)}>
                Đổi
              </button>
            </p>
          )}

          <FormError>{error}</FormError>

          <button className="btn btn-primary btn-block" type="submit" disabled={busy}>
            {busy ? "Đang xử lý…" : mode === "login" ? "Đăng nhập" : "Tạo tài khoản"}
          </button>

          <p className="auth-switch">
            {mode === "login" ? (
              <>
                Chưa có tài khoản?{" "}
                <button type="button" className="link-btn" onClick={() => switchMode("register")}>
                  Tạo tài khoản
                </button>
              </>
            ) : (
              <>
                Đã có tài khoản?{" "}
                <button type="button" className="link-btn" onClick={() => switchMode("login")}>
                  Đăng nhập
                </button>
              </>
            )}
          </p>
        </form>
      </section>
    </main>
  );
}
