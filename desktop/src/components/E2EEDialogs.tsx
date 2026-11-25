import { useEffect, useState, type FormEvent } from "react";
import { ApiError, type Channel } from "../lib/api";
import { dmPeerId } from "../lib/channel";
import { e2ee, useE2EE } from "../lib/e2ee";
import { displayName } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { Dialog, FormError, Icon } from "./ui";

const MIN_PIN = 6;

const errText = (err: unknown) =>
  err instanceof ApiError ? err.message : err instanceof Error ? err.message : "Có lỗi xảy ra, thử lại sau.";

export type PinMode = "setup" | "unlock" | "change" | "reset";

const TITLES: Record<PinMode, string> = {
  setup: "Bật mã hoá đầu cuối",
  unlock: "Nhập mã PIN",
  change: "Đổi mã PIN",
  reset: "Tạo khoá mã hoá mới",
};

/** Tạo / nhập / đổi mã PIN bảo vệ khoá mã hoá đầu cuối. */
export function PinDialog({ mode: initial, onClose }: { mode: PinMode; onClose: () => void }) {
  const [mode, setMode] = useState<PinMode>(initial);
  const [pin, setPin] = useState("");
  const [pin2, setPin2] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const needConfirm = mode !== "unlock";

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (pin.length < MIN_PIN) return setError(`Mã PIN cần ít nhất ${MIN_PIN} ký tự.`);
    if (needConfirm && pin !== pin2) return setError("Hai lần nhập mã PIN không khớp.");
    setBusy(true);
    // nhường 1 nhịp cho giao diện hiện "Đang xử lý…" trước khi Argon2 chạy (~0,2 s)
    await new Promise((r) => setTimeout(r, 30));
    try {
      if (mode === "setup") await e2ee.setup(pin);
      else if (mode === "reset") await e2ee.setup(pin, true);
      else if (mode === "change") await e2ee.changePin(pin);
      else await e2ee.unlock(pin);
      onClose();
    } catch (err) {
      setError(errText(err));
      setBusy(false);
    }
  };

  return (
    <Dialog title={TITLES[mode]} onClose={onClose}>
      <form className="dialog-form" onSubmit={submit}>
        <div className="e2ee-hero" aria-hidden>
          <Icon name="lock" size={26} />
        </div>
        {mode === "setup" && (
          <p className="muted">
            Tin nhắn riêng sẽ được mã hoá trên máy bạn trước khi gửi. Chỉ bạn và người nhận đọc được, kể cả máy chủ cũng
            không. Tạo mã PIN để khôi phục tin nhắn khi đăng nhập trên máy khác.
          </p>
        )}
        {mode === "unlock" && (
          <p className="muted">
            Tài khoản của bạn đã bật mã hoá đầu cuối. Nhập mã PIN để đọc và gửi tin nhắn được mã hoá trên máy này.
          </p>
        )}
        {mode === "change" && <p className="muted">Mã PIN mới dùng để khôi phục tin nhắn trên máy khác.</p>}
        {mode === "reset" && (
          <p className="e2ee-warn">
            <Icon name="lock" size={15} />
            Khoá mới sẽ thay khoá cũ: các tin nhắn đã mã hoá trước đây sẽ <strong>không đọc được nữa</strong> trên mọi
            máy. Chỉ làm khi bạn đã quên mã PIN.
          </p>
        )}
        <label className="field">
          <span>{mode === "unlock" ? "Mã PIN" : "Mã PIN mới"}</span>
          <input
            type="password"
            value={pin}
            onChange={(e) => setPin(e.target.value)}
            autoComplete={mode === "unlock" ? "current-password" : "new-password"}
            autoFocus data-autofocus
            minLength={MIN_PIN}
            maxLength={128}
          />
          {needConfirm && <small>Ít nhất {MIN_PIN} ký tự. PIN càng dài càng khó bị dò.</small>}
        </label>
        {needConfirm && (
          <label className="field">
            <span>Nhập lại mã PIN</span>
            <input
              type="password"
              value={pin2}
              onChange={(e) => setPin2(e.target.value)}
              autoComplete="new-password"
              maxLength={128}
            />
          </label>
        )}
        <FormError>{error}</FormError>
        <div className="dialog-actions">
          {mode === "unlock" && (
            <button type="button" className="link-btn e2ee-forgot" onClick={() => setMode("reset")} disabled={busy}>
              Quên mã PIN?
            </button>
          )}
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            {mode === "setup" || mode === "unlock" ? "Để sau" : "Huỷ"}
          </button>
          <button className={`btn ${mode === "reset" ? "btn-danger" : "btn-primary"}`} disabled={busy}>
            {busy
              ? "Đang xử lý…"
              : mode === "unlock"
                ? "Mở khoá"
                : mode === "reset"
                  ? "Tạo khoá mới"
                  : mode === "change"
                    ? "Đổi mã PIN"
                    : "Bật mã hoá"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}

/** Mã an toàn của cuộc trò chuyện riêng: hai bên so khớp để chắc không ai tráo khoá. */
export function SafetyDialog({ channel, onClose }: { channel: Channel; onClose: () => void }) {
  const { state } = useChat();
  const { status } = useE2EE();
  const peerId = dmPeerId(channel, state.me.id)!;
  const peer = state.users[peerId];
  const peerKey = usePeerKey(peerId);
  const self = peerId === state.me.id;
  const number = status === "ready" && peerKey ? e2ee.safetyNumber(peerId, peerKey) : "";

  return (
    <Dialog title="Mã hoá đầu cuối" onClose={onClose}>
      <div className="dialog-form">
        <div className="e2ee-hero" aria-hidden>
          <Icon name="lock" size={26} />
        </div>
        {number ? (
          <>
            <p className="muted">
              Tin nhắn, file, sticker trong cuộc trò chuyện này được mã hoá trên máy người gửi. Chỉ{" "}
              {self ? "bạn" : `bạn và ${displayName(peer)}`} đọc được, máy chủ chỉ lưu bản đã mã hoá.
            </p>
            {!self && (
              <>
                <p className="muted">
                  Để chắc chắn không ai chen vào giữa, hãy so mã an toàn dưới đây với {displayName(peer)} (gặp trực tiếp
                  hoặc gọi điện). Hai bên phải thấy cùng một dãy số.
                </p>
                <div className="safety-number" aria-label="Mã an toàn">
                  {number.split(" ").map((g, i) => (
                    <span key={i}>{g}</span>
                  ))}
                </div>
              </>
            )}
          </>
        ) : status !== "ready" ? (
          <p className="muted">Bạn chưa mở mã hoá đầu cuối trên máy này nên tin nhắn ở đây chưa được mã hoá.</p>
        ) : peerKey === null ? (
          <p className="muted">
            {displayName(peer)} chưa bật mã hoá đầu cuối. Tin nhắn sẽ được mã hoá ngay khi họ bật (mở một tin nhắn riêng
            và tạo mã PIN).
          </p>
        ) : (
          <p className="muted">Đang tải khoá…</p>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn btn-primary" onClick={onClose}>
            Đóng
          </button>
        </div>
      </div>
    </Dialog>
  );
}

/** Khoá công khai của userId: undefined = đang tải, null = chưa bật mã hoá. */
export function usePeerKey(userId: number | null): string | null | undefined {
  useE2EE();
  const cached = userId === null ? null : e2ee.peerKeyCached(userId);
  const ready = e2ee.status === "ready";
  useEffect(() => {
    if (userId !== null && ready && cached === undefined) void e2ee.peerKey(userId).catch(() => {});
  }, [userId, ready, cached]);
  return cached;
}

/** Tin nhắn riêng có đang được mã hoá không (cả hai đã có khoá). */
export function useDMEncryption(channel: Channel) {
  const { state } = useChat();
  const { status } = useE2EE();
  const peerId = dmPeerId(channel, state.me.id);
  const peerKey = usePeerKey(peerId);
  return { status, peerId, peerKey, encrypted: status === "ready" && !!peerKey };
}

let promptedThisSession = false;

/** Dải thông báo trên ô soạn của tin nhắn riêng khi CHƯA mã hoá được; tự mời bật mã hoá một lần. */
export function E2EEBanner({ channel }: { channel: Channel }) {
  const { state } = useChat();
  const { status, peerId, peerKey } = useDMEncryption(channel);
  const [dialog, setDialog] = useState<PinMode | null>(null);

  useEffect(() => {
    if (promptedThisSession) return;
    if (status === "none" || status === "locked") {
      promptedThisSession = true;
      setDialog(status === "none" ? "setup" : "unlock");
    }
  }, [status]);

  let body: React.ReactNode = null;
  if (status === "none") {
    body = (
      <>
        <span>Tin nhắn riêng của bạn chưa được mã hoá đầu cuối.</span>
        <button type="button" className="link-btn" onClick={() => setDialog("setup")}>
          Bật mã hoá
        </button>
      </>
    );
  } else if (status === "locked") {
    body = (
      <>
        <span>Nhập mã PIN để đọc và gửi tin nhắn được mã hoá trên máy này.</span>
        <button type="button" className="link-btn" onClick={() => setDialog("unlock")}>
          Nhập mã PIN
        </button>
      </>
    );
  } else if (status === "error") {
    body = <span>Không kiểm tra được mã hoá đầu cuối (mất kết nối?). Tin nhắn đang gửi dạng thường.</span>;
  } else if (status === "ready" && peerKey === null && peerId !== null) {
    body = (
      <span>
        {displayName(state.users[peerId])} chưa bật mã hoá đầu cuối, tin nhắn hiện gửi dạng thường. Sẽ tự mã hoá khi họ
        bật.
      </span>
    );
  }

  return (
    <>
      {body && (
        <div className="e2ee-banner" role="status">
          <Icon name="lock" size={15} />
          {body}
        </div>
      )}
      {dialog && <PinDialog mode={dialog} onClose={() => setDialog(null)} />}
    </>
  );
}

/** Mục "Mã hoá đầu cuối" trong Cài đặt > Tài khoản. */
export function E2EESettings() {
  const { status } = useE2EE();
  const [dialog, setDialog] = useState<PinMode | null>(null);
  const fp = e2ee.publicKey;
  return (
    <section className="settings-section">
      <h3>Mã hoá đầu cuối</h3>
      <p className="muted">
        {status === "ready"
          ? "Đang bật. Tin nhắn riêng được mã hoá trên máy bạn; mã PIN dùng để khôi phục trên máy khác."
          : status === "locked"
            ? "Tài khoản đã bật nhưng máy này chưa mở khoá."
            : status === "none"
              ? "Chưa bật. Tin nhắn riêng đang gửi dạng thường."
              : status === "loading"
                ? "Đang kiểm tra…"
                : "Không kiểm tra được trạng thái."}
      </p>
      {fp && (
        <p className="muted e2ee-fp">
          Khoá của bạn: <code>{fp.slice(0, 16)}…</code>
        </p>
      )}
      <div className="dialog-actions e2ee-actions">
        {status === "ready" && (
          <button type="button" className="btn" onClick={() => setDialog("change")}>
            Đổi mã PIN
          </button>
        )}
        {status === "none" && (
          <button type="button" className="btn btn-primary" onClick={() => setDialog("setup")}>
            Bật mã hoá
          </button>
        )}
        {status === "locked" && (
          <button type="button" className="btn btn-primary" onClick={() => setDialog("unlock")}>
            Nhập mã PIN
          </button>
        )}
      </div>
      {dialog && <PinDialog mode={dialog} onClose={() => setDialog(null)} />}
    </section>
  );
}
