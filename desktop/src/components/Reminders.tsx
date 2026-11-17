import { useState, type FormEvent } from "react";
import { ApiError, type Message, type Reminder } from "../lib/api";
import { displayName } from "../lib/format";
import { channelLabel } from "../lib/channel";
import { useE2EE } from "../lib/e2ee";
import { useChat } from "../state/ChatProvider";
import { previewText } from "./MessageExtras";
import { Dialog, FormError, Icon } from "./ui";

/* ---------- Thời gian ---------- */

const pad = (n: number) => String(n).padStart(2, "0");

/** Giá trị cho <input type="datetime-local"> theo giờ máy. */
export function toLocalInput(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** "15:30 hôm nay", "09:00 ngày mai", "09:00 T2 24/11". */
export function formatWhen(iso: string | Date) {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  const now = new Date();
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(d) - day(now)) / 86400000);
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (diff === 0) return `${time} hôm nay`;
  if (diff === 1) return `${time} ngày mai`;
  if (diff === -1) return `${time} hôm qua`;
  const wd = ["CN", "T2", "T3", "T4", "T5", "T6", "T7"][d.getDay()];
  return `${time} ${wd} ${d.getDate()}/${d.getMonth() + 1}${d.getFullYear() !== now.getFullYear() ? "/" + d.getFullYear() : ""}`;
}

/** "còn 2 giờ", "còn 3 ngày", "còn 5 phút". */
export function formatLeft(iso: string) {
  const ms = new Date(iso).getTime() - Date.now();
  if (ms <= 0) return "đã hết hạn";
  const m = Math.ceil(ms / 60000);
  if (m < 60) return `còn ${m} phút`;
  const h = Math.round(m / 60);
  if (h < 48) return `còn ${h} giờ`;
  return `còn ${Math.round(h / 24)} ngày`;
}

function at(hours: number, minutes = 0, addDays = 0) {
  const d = new Date();
  d.setDate(d.getDate() + addDays);
  d.setHours(hours, minutes, 0, 0);
  return d;
}

/** Các mốc nhắc nhanh. */
function presets(): { label: string; at: Date }[] {
  const now = Date.now();
  const nextMonday = (() => {
    const d = at(9);
    const add = (8 - d.getDay()) % 7 || 7;
    d.setDate(d.getDate() + add);
    return d;
  })();
  const list = [
    { label: "20 phút nữa", at: new Date(now + 20 * 60000) },
    { label: "1 giờ nữa", at: new Date(now + 3600000) },
    { label: "3 giờ nữa", at: new Date(now + 3 * 3600000) },
    { label: "Chiều nay (17:00)", at: at(17) },
    { label: "Sáng mai (09:00)", at: at(9, 0, 1) },
    { label: "Thứ Hai tới (09:00)", at: nextMonday },
  ];
  return list.filter((p) => p.at.getTime() > now + 60000);
}

/* ---------- Đặt nhắc ---------- */

export function RemindDialog({ message, onClose }: { message: Message; onClose: () => void }) {
  const { remind } = useChat();
  useE2EE();
  const [custom, setCustom] = useState(() => toLocalInput(new Date(Date.now() + 3600000)));
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const set = async (when: Date) => {
    if (when.getTime() < Date.now()) return setError("Thời gian nhắc đã qua.");
    setBusy(true);
    setError(null);
    try {
      await remind(message.id, when, note);
      setDone(formatWhen(when));
      window.setTimeout(onClose, 900);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không đặt được nhắc việc.");
      setBusy(false);
    }
  };

  const submitCustom = (e: FormEvent) => {
    e.preventDefault();
    const d = new Date(custom);
    if (isNaN(d.getTime())) return setError("Chọn ngày giờ hợp lệ.");
    void set(d);
  };

  return (
    <Dialog title="Nhắc tôi về tin này" onClose={onClose}>
      <form className="dialog-form" onSubmit={submitCustom}>
        <blockquote className="delete-preview forward-preview">
          {previewText({ ...message, deleted: !!message.deleted_at, poll: message.poll }, message.channel_id) ||
            "(tin nhắn)"}
        </blockquote>
        {done ? (
          <p className="saved-note remind-done" role="status">
            <Icon name="check" size={15} /> Sẽ nhắc bạn lúc {done}.
          </p>
        ) : (
          <>
            <div className="remind-presets">
              {presets().map((p) => (
                <button key={p.label} type="button" className="btn" disabled={busy} onClick={() => void set(p.at)}>
                  <span>{p.label}</span>
                  <small>{formatWhen(p.at)}</small>
                </button>
              ))}
            </div>
            <label className="field">
              <span>Hoặc chọn giờ</span>
              <input
                type="datetime-local"
                value={custom}
                min={toLocalInput(new Date())}
                onChange={(e) => setCustom(e.target.value)}
              />
            </label>
            <label className="field">
              <span>Ghi chú (không bắt buộc)</span>
              <input
                value={note}
                onChange={(e) => setNote(e.target.value)}
                maxLength={200}
                placeholder="Ví dụ: gửi file cho sếp"
              />
            </label>
            <FormError>{error}</FormError>
            <div className="dialog-actions">
              <button type="button" className="btn" onClick={onClose} disabled={busy}>
                Huỷ
              </button>
              <button className="btn btn-primary" disabled={busy}>
                Đặt nhắc
              </button>
            </div>
          </>
        )}
      </form>
    </Dialog>
  );
}

/* ---------- Danh sách nhắc việc ---------- */

export function RemindersDialog({ onClose, onJumped }: { onClose: () => void; onJumped: () => void }) {
  const { state, snoozeReminder, doneReminder, jumpToMessage } = useChat();
  useE2EE();
  const [error, setError] = useState<string | null>(null);
  const list = state.reminders;
  const due = (list ?? []).filter((r) => r.fired_at);
  const upcoming = (list ?? []).filter((r) => !r.fired_at);

  const run = (fn: () => Promise<void>) =>
    fn().catch((err) => setError(err instanceof ApiError ? err.message : "Có lỗi xảy ra."));

  const row = (r: Reminder) => {
    const m = r.message;
    const ch = m ? state.channels[m.channel_id] : undefined;
    return (
      <li key={r.id} className={`reminder${r.fired_at ? " is-due" : ""}`}>
        <div className="reminder-when">
          <Icon name="clock" size={15} />
          {r.fired_at ? "Đến giờ: " : ""}
          {formatWhen(r.remind_at)}
        </div>
        {r.note && <p className="reminder-note">{r.note}</p>}
        {m && (
          <p className="reminder-msg">
            <strong>{displayName(state.users[m.user_id] ?? m.user)}</strong>
            {ch && <span> · {channelLabel(ch, state.users, state.me.id)}</span>}:{" "}
            {previewText({ ...m, deleted: !!m.deleted_at, poll: m.poll }, m.channel_id) || "(tin nhắn)"}
          </p>
        )}
        <div className="reminder-actions">
          {m && (
            <button
              type="button"
              className="link-btn"
              onClick={() => {
                onClose();
                onJumped();
                void jumpToMessage(m);
              }}
            >
              Xem tin
            </button>
          )}
          <button
            type="button"
            className="link-btn"
            onClick={() => run(() => snoozeReminder(r.id, new Date(Date.now() + 3600000)))}
          >
            Hoãn 1 giờ
          </button>
          <button type="button" className="link-btn" onClick={() => run(() => snoozeReminder(r.id, at(9, 0, 1)))}>
            Sáng mai
          </button>
          <button type="button" className="link-btn reminder-done" onClick={() => run(() => doneReminder(r.id))}>
            {r.fired_at ? "Xong" : "Huỷ nhắc"}
          </button>
        </div>
      </li>
    );
  };

  return (
    <Dialog title="Nhắc việc" onClose={onClose}>
      <FormError>{error}</FormError>
      {!list && <p className="muted">Đang tải…</p>}
      {list && list.length === 0 && (
        <p className="muted">Chưa có nhắc việc nào. Rê chuột (hoặc nhấn giữ) một tin, chọn “Nhắc tôi về tin này”.</p>
      )}
      {due.length > 0 && (
        <>
          <h4 className="reminder-group">Đến giờ</h4>
          <ul className="reminder-list">{due.map(row)}</ul>
        </>
      )}
      {upcoming.length > 0 && (
        <>
          <h4 className="reminder-group">Sắp tới</h4>
          <ul className="reminder-list">{upcoming.map(row)}</ul>
        </>
      )}
    </Dialog>
  );
}
