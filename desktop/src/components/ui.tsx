import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import type { PublicUser } from "../lib/api";
import { displayName, initials, userHue } from "../lib/format";

/* ---------- Avatar ---------- */

/** Địa chỉ server, để ghép với ảnh tải lên server (đường dẫn tương đối "/api/avatars/…"). */
export const ServerUrlContext = createContext("");

export function assetUrl(src: string, serverUrl: string) {
  return src.startsWith("/") ? serverUrl + src : src;
}

export function Avatar({ user, size = 36, online }: { user: PublicUser | undefined; size?: number; online?: boolean }) {
  const [broken, setBroken] = useState(false);
  const serverUrl = useContext(ServerUrlContext);
  const name = displayName(user);
  const src = user?.avatar && !broken ? assetUrl(user.avatar, serverUrl) : null;

  useEffect(() => setBroken(false), [user?.avatar]);

  return (
    <span className="avatar" style={{ width: size, height: size, fontSize: size * 0.38 }} aria-hidden>
      {src ? (
        <img src={src} alt="" onError={() => setBroken(true)} referrerPolicy="no-referrer" />
      ) : (
        <span className="avatar-initials" style={{ background: user ? userHue(user.id) : "var(--muted)" }}>
          {initials(name)}
        </span>
      )}
      {online !== undefined && <span className={`presence-dot${online ? " is-online" : ""}`} />}
    </span>
  );
}

/* ---------- Dialog ---------- */

type DialogProps = {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
};

/** Dùng <dialog> gốc: có sẵn khoá focus, đóng bằng Esc, nền mờ. */
export function Dialog({ title, onClose, children, wide }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);

  return (
    <dialog
      ref={ref}
      className={`dialog${wide ? " dialog-wide" : ""}`}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        // bấm ra ngoài khung (vào nền mờ) để đóng
        if (e.target === ref.current) onClose();
      }}
      // Hộp thoại có thể nằm trong một tin nhắn (cây React): đừng để chạm/nhấn giữ lan ra tin
      // nhắn bên ngoài (sẽ mở bảng thao tác của tin).
      onTouchStart={(e) => e.stopPropagation()}
      onTouchMove={(e) => e.stopPropagation()}
      onTouchEnd={(e) => e.stopPropagation()}
      onContextMenu={(e) => e.stopPropagation()}
      aria-labelledby="dialog-title"
    >
      <div className="dialog-body">
        <header className="dialog-header">
          <h2 id="dialog-title">{title}</h2>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Đóng">
            <Icon name="x" />
          </button>
        </header>
        {children}
      </div>
    </dialog>
  );
}

/* ---------- Icon ---------- */

const paths: Record<string, ReactNode> = {
  hash: <path d="M5 9h14M5 15h14M10 3 8 21M16 3l-2 18" />,
  plus: <path d="M12 5v14M5 12h14" />,
  x: <path d="M6 6l12 12M18 6 6 18" />,
  users: (
    <>
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2.5 20c.8-3.5 3.4-5.5 6.5-5.5s5.7 2 6.5 5.5M16 4.8a3.5 3.5 0 0 1 0 6.4M18 14.8c1.8.8 3 2.6 3.5 5.2" />
    </>
  ),
  settings: (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z" />
    </>
  ),
  compass: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m15.5 8.5-2 5-5 2 2-5 5-2Z" />
    </>
  ),
  send: <path d="M4 12 20 4l-4 16-4-7-8-1ZM12 13l8-9" />,
  refresh: <path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7" />,
  arrowDown: <path d="M12 5v14M6 13l6 6 6-6" />,
  arrowLeft: <path d="M19 12H5M11 6l-6 6 6 6" />,
  play: <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />,
  smilePlus: (
    <>
      <path d="M20.5 11.5A8.5 8.5 0 1 1 12.5 3.5" />
      <path d="M8.5 14.5a4.5 4.5 0 0 0 7 0M9 9.5h.01M15 9.5h.01M19 2.5v6M16 5.5h6" />
    </>
  ),
  thread: (
    <>
      <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12Z" />
      <path d="M8.5 10.5h7M8.5 13.5h4.5" />
    </>
  ),
  paperclip: <path d="m21 11.5-8.6 8.6a5.5 5.5 0 0 1-7.8-7.8l8.6-8.6a3.7 3.7 0 0 1 5.2 5.2l-8.6 8.6a1.8 1.8 0 0 1-2.6-2.6l8-8" />,
  smile: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M8.5 14.5a4.5 4.5 0 0 0 7 0M9 9.5h.01M15 9.5h.01" />
    </>
  ),
  file: (
    <>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5Z" />
      <path d="M14 3v5h5" />
    </>
  ),
  download: <path d="M12 4v11M7 10l5 5 5-5M5 20h14" />,
  external: <path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />,
  upload: <path d="M12 20V9M7 14l5-5 5 5M5 4h14" />,
  lock: (
    <>
      <rect x="5" y="11" width="14" height="10" rx="2" />
      <path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </>
  ),
  userPlus: (
    <>
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2.5 20c.8-3.5 3.4-5.5 6.5-5.5s5.7 2 6.5 5.5M19 8v6M16 11h6" />
    </>
  ),
  link: <path d="M10 14a4.5 4.5 0 0 0 6.4 0l3.2-3.2a4.5 4.5 0 0 0-6.4-6.4l-1 1M14 10a4.5 4.5 0 0 0-6.4 0l-3.2 3.2a4.5 4.5 0 0 0 6.4 6.4l1-1" />,
  camera: (
    <>
      <path d="M4 8.5A1.5 1.5 0 0 1 5.5 7h2l1.5-2h6l1.5 2h2A1.5 1.5 0 0 1 20 8.5v9a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 17.5v-9Z" />
      <circle cx="12" cy="12.5" r="3.5" />
    </>
  ),
  sticker: (
    <>
      <path d="M20 12.5V7a3 3 0 0 0-3-3H7a3 3 0 0 0-3 3v10a3 3 0 0 0 3 3h5.5L20 12.5Z" />
      <path d="M12.5 20v-4.5a3 3 0 0 1 3-3H20M8.5 13.5a4 4 0 0 0 5 1M9 9.5h.01M14.5 9.5h.01" />
    </>
  ),
  poll: <path d="M5 20V10M12 20V4M19 20v-7M3 20h18" />,
  check: <path d="m5 12.5 4.5 4.5L19 7" strokeWidth="2.4" />,
  reply: <path d="M9 14 4 9l5-5M4 9h10.5a5.5 5.5 0 0 1 0 11H11" />,
  forward: <path d="m15 14 5-5-5-5M20 9H9.5a5.5 5.5 0 0 0 0 11H13" />,
  pin: <path d="M9 4h6l-1 6 3 3H7l3-3-1-6ZM12 13v7" />,
  more: (
    <>
      <circle cx="5.5" cy="12" r="1.3" fill="currentColor" />
      <circle cx="12" cy="12" r="1.3" fill="currentColor" />
      <circle cx="18.5" cy="12" r="1.3" fill="currentColor" />
    </>
  ),
  search: (
    <>
      <circle cx="11" cy="11" r="6.5" />
      <path d="m20 20-4.2-4.2" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </>
  ),
  pencil: <path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16v4ZM13.5 6.5l4 4" />,
  trash: <path d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3" />,
  copy: (
    <>
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </>
  ),
};

export function Icon({ name, size = 18 }: { name: keyof typeof paths | string; size?: number }) {
  return (
    <svg
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      {paths[name]}
    </svg>
  );
}

/* ---------- Lỗi biểu mẫu ---------- */

export function FormError({ children }: { children: ReactNode }) {
  if (!children) return null;
  return (
    <p className="form-error" role="alert">
      {children}
    </p>
  );
}
