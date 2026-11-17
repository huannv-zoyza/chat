import { useLayoutEffect, useRef, useState, type KeyboardEvent } from "react";
import { ApiError, type Message } from "../lib/api";
import { displayName } from "../lib/format";
import { isTouchDevice } from "../lib/useMedia";
import { useChat } from "../state/ChatProvider";
import { Dialog } from "./ui";
import { embedSummary } from "./Embeds";
import { e2ee } from "../lib/e2ee";

const MAX_RUNES = 4000;

function autosize(t: HTMLTextAreaElement) {
  t.style.height = "auto";
  t.style.height = `${t.scrollHeight}px`;
}

/** Sửa tin ngay tại chỗ: Enter lưu (máy tính), Esc huỷ. */
export function MessageEditor({ message }: { message: Message }) {
  const { editMessage, dispatch } = useChat();
  const [text, setText] = useState(message.content);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);

  useLayoutEffect(() => {
    const t = ref.current;
    if (!t) return;
    autosize(t);
    t.focus();
    t.setSelectionRange(t.value.length, t.value.length);
  }, []);

  const cancel = () => dispatch({ type: "edit.start", messageId: null });

  const save = async () => {
    const v = text.trim();
    if (v === message.content.trim()) return cancel();
    if (!v && !message.attachments?.length) {
      setError("Nội dung đang trống. Muốn bỏ tin này thì hãy xoá.");
      return;
    }
    if ([...v].length > MAX_RUNES) {
      setError("Tin nhắn dài tối đa 4000 ký tự.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await editMessage(message.id, v);
      cancel();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không lưu được, thử lại.");
      setBusy(false);
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      cancel();
    } else if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing && !isTouchDevice()) {
      e.preventDefault();
      void save();
    }
  };

  return (
    <div className="msg-editor">
      <textarea
        ref={ref}
        value={text}
        rows={1}
        aria-label="Sửa tin nhắn"
        onChange={(e) => {
          setText(e.target.value);
          autosize(e.target);
        }}
        onKeyDown={onKeyDown}
        disabled={busy}
      />
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      <div className="msg-editor-bar">
        <span className="msg-editor-hint">{isTouchDevice() ? "Sửa xong bấm Lưu" : "Esc để huỷ · Enter để lưu"}</span>
        <button type="button" className="btn btn-sm" onClick={cancel} disabled={busy}>
          Huỷ
        </button>
        <button type="button" className="btn btn-sm btn-primary" onClick={() => void save()} disabled={busy}>
          {busy ? "Đang lưu…" : "Lưu"}
        </button>
      </div>
    </div>
  );
}

/** Hỏi lại trước khi xoá (xoá với mọi người, không hoàn tác). */
export function ConfirmDeleteDialog({ message, onClose }: { message: Message; onClose: () => void }) {
  const { deleteMessage, state } = useChat();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const someoneElse = message.user_id !== state.me.id;
  const author = displayName(state.users[message.user_id] ?? message.user);
  const preview =
    message.content.length > 160
      ? message.content.slice(0, 160) + "…"
      : message.content || (message.embed ? embedSummary(message.embed) : "(tin chỉ có file đính kèm)");

  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      await deleteMessage(message.id);
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không xoá được, thử lại.");
      setBusy(false);
    }
  };

  return (
    <Dialog title="Xoá tin nhắn?" onClose={onClose}>
      <div className="dialog-form">
        <p className="muted">
          {someoneElse ? `Bạn đang xoá tin của ${author} bằng quyền chủ kênh. ` : ""}
          Tin nhắn và file đính kèm sẽ bị xoá với mọi người, không khôi phục được.
        </p>
        <blockquote className="delete-preview">{preview}</blockquote>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Huỷ
          </button>
          <button type="button" className="btn btn-danger" onClick={() => void confirm()} disabled={busy} autoFocus>
            {busy ? "Đang xoá…" : "Xoá tin nhắn"}
          </button>
        </div>
      </div>
    </Dialog>
  );
}

/** Tin nhắn cuối cùng của mình (chưa xoá) trong danh sách, để bấm ↑ là sửa như Slack. */
export function lastOwnMessage(items: Message[], meId: number): Message | undefined {
  for (let i = items.length - 1; i >= 0; i--) {
    const m = items[i];
    if (m.user_id !== meId || m.deleted_at || m.embed) continue;
    if (m.cipher) {
      const dec = e2ee.decrypt(m);
      if (!dec.ok || dec.payload.embed) continue;
    }
    return m;
  }
  return undefined;
}
