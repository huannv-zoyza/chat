import { useCallback, useEffect, useMemo, useState } from "react";
import { ApiError, createApi, type User } from "./lib/api";
import { load, remove, save } from "./lib/storage";
import { AuthScreen } from "./components/AuthScreen";
import { Workspace } from "./components/Workspace";
import { ChatProvider } from "./state/ChatProvider";
import { servedByServer } from "./lib/platform";

type Session = { serverUrl: string; token: string };

// Bản web do server phục vụ: máy chủ chính là trang đang mở.
const DEFAULT_SERVER = servedByServer ? window.location.origin : "http://localhost:4000";

function loadSession(): Session | null {
  const s = load<Session | null>("session", null);
  // Bản web: bỏ phiên của máy chủ khác (ví dụ server đổi IP/cổng).
  if (s && servedByServer && s.serverUrl !== window.location.origin) return null;
  return s;
}

export default function App() {
  const [session, setSession] = useState<Session | null>(loadSession);
  const [me, setMe] = useState<User | null>(null);
  const [bootError, setBootError] = useState<string | null>(null);
  // Lưu vào storage để thông báo (ví dụ "phiên hết hạn") không mất khi tải lại app.
  const [notice, setNotice] = useState<string | null>(() => load("authNotice", null));

  const endSession = useCallback((message: string | null) => {
    remove("session");
    if (message) save("authNotice", message);
    else remove("authNotice");
    setSession(null);
    setMe(null);
    setBootError(null);
    setNotice(message);
  }, []);

  const onUnauthorized = useCallback(
    () => endSession("Phiên đăng nhập đã hết hạn, hãy đăng nhập lại."),
    [endSession],
  );

  const api = useMemo(
    () => (session ? createApi({ serverUrl: session.serverUrl, token: session.token, onUnauthorized }) : null),
    [session, onUnauthorized],
  );

  const logout = useCallback(() => {
    api?.logout().catch(() => {}); // không chờ: đăng xuất trên máy này luôn thành công
    endSession(null);
  }, [api, endSession]);

  // Mở app với phiên đã lưu: kiểm tra token còn hợp lệ.
  const boot = useCallback(() => {
    if (!api) return;
    setBootError(null);
    api
      .me()
      .then(setMe)
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) return; // onUnauthorized đã xử lý
        setBootError(err instanceof ApiError ? err.message : "Không tải được thông tin tài khoản.");
      });
  }, [api]);

  useEffect(() => {
    if (session && !me) boot();
  }, [session, me, boot]);

  if (!session || !api) {
    return (
      <AuthScreen
        initialServer={
          servedByServer
            ? DEFAULT_SERVER
            : (load<{ serverUrl: string } | null>("lastServer", null)?.serverUrl ?? DEFAULT_SERVER)
        }
        fixedServer={servedByServer}
        notice={notice}
        onAuthenticated={(serverUrl, token, user) => {
          const s = { serverUrl, token };
          save("session", s);
          save("lastServer", { serverUrl });
          remove("authNotice");
          setNotice(null);
          setMe(user);
          setSession(s);
        }}
      />
    );
  }

  if (!me) {
    return (
      <main className="boot">
        {bootError ? (
          <div className="boot-card">
            <h1>Chưa kết nối được</h1>
            <p>{bootError}</p>
            <div className="dialog-actions dialog-actions-start">
              <button className="btn btn-primary" onClick={boot}>
                Thử lại
              </button>
              <button className="btn" onClick={() => endSession(null)}>
                Đăng nhập máy chủ khác
              </button>
            </div>
          </div>
        ) : (
          <p className="muted">Đang mở Go Chat…</p>
        )}
      </main>
    );
  }

  return (
    <ChatProvider
      key={session.token}
      api={api}
      serverUrl={session.serverUrl}
      token={session.token}
      me={me}
      onLogout={logout}
      onUnauthorized={onUnauthorized}
    >
      <Workspace />
    </ChatProvider>
  );
}
