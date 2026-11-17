import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import { type Channel, MAX_FILE_SIZE, MAX_FILES, type PublicUser } from "../lib/api";
import { displayName } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { isTouchDevice } from "../lib/useMedia";
import { EmojiPicker } from "./EmojiPicker";
import { FileCard, formatSize, useObjectUrls } from "./MessageParts";
import { Avatar, Icon } from "./ui";

const MAX_LEN = 4000;

// Giữ bản nháp (chữ + file) khi chuyển kênh.
// Khoá: "c<id kênh>" cho dòng chính, "t<id tin gốc>" cho ô trả lời trong luồng.
const drafts = new Map<string, { text: string; files: File[] }>();

// "@" ở đầu hoặc sau khoảng trắng/dấu câu, ngay trước con trỏ.
const MENTION_QUERY_RE = /(?:^|[^\p{L}\p{N}_.@])@([\p{L}\p{N}_.]{0,32})$/u;

// So khớp không dấu: "quyen" khớp "Quyên".
const fold = (s: string) =>
  s
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "D")
    .toLowerCase();

type Suggestion = { kind: "all" } | { kind: "user"; user: PublicUser };

export type ComposerHandle = { addFiles: (files: File[]) => void };

type ComposerProps = {
  channel: Channel;
  /** Có: ô trả lời trong luồng của tin này. */
  parentId?: number;
};

export const Composer = forwardRef<ComposerHandle, ComposerProps>(function Composer({ channel, parentId }, handle) {
  const draftKey = parentId ? `t${parentId}` : `c${channel.id}`;
  const { state, sendMessage, notifyTyping, loadMembers } = useChat();
  const [text, setText] = useState(() => drafts.get(draftKey)?.text ?? "");
  const [files, setFiles] = useState<File[]>(() => drafts.get(draftKey)?.files ?? []);
  const [fileError, setFileError] = useState<string | null>(null);
  const [emojiOpen, setEmojiOpen] = useState(false);
  const [mention, setMention] = useState<{ query: string; start: number } | null>(null);
  const [active, setActive] = useState(0);
  const ref = useRef<HTMLTextAreaElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  // Vị trí con trỏ cần đặt sau khi chèn @tên/emoji. Đặt ngay sau render (useLayoutEffect),
  // trước khi trình duyệt xử lý phím tiếp theo; đặt trễ hơn thì chữ gõ nhanh sẽ rơi sai chỗ.
  const pendingCaret = useRef<number | null>(null);

  /* ---------- Bản nháp theo kênh ---------- */

  useEffect(() => {
    const d = drafts.get(draftKey);
    setText(d?.text ?? "");
    setFiles(d?.files ?? []);
    setFileError(null);
    setMention(null);
    setEmojiOpen(false);
    ref.current?.focus();
  }, [draftKey]);

  useEffect(() => {
    drafts.set(draftKey, { text, files });
  }, [draftKey, text, files]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 220)}px`;
    if (pendingCaret.current !== null) {
      el.focus();
      el.setSelectionRange(pendingCaret.current, pendingCaret.current);
      pendingCaret.current = null;
    }
  }, [text]);

  // Cần danh sách thành viên để gợi ý @tên.
  useEffect(() => {
    if (!state.members[channel.id]) loadMembers(channel.id).catch(() => {});
  }, [channel.id, state.members, loadMembers]);

  /* ---------- File ---------- */

  const addFiles = useCallback((incoming: File[]) => {
    if (incoming.length === 0) return;
    const tooBig = incoming.filter((f) => f.size > MAX_FILE_SIZE);
    const ok = incoming.filter((f) => f.size <= MAX_FILE_SIZE);
    setFiles((cur) => {
      const next = [...cur, ...ok];
      if (next.length > MAX_FILES) {
        setFileError(`Mỗi tin gửi tối đa ${MAX_FILES} file, đã bỏ bớt ${next.length - MAX_FILES} file.`);
        return next.slice(0, MAX_FILES);
      }
      setFileError(
        tooBig.length
          ? `${tooBig.map((f) => `“${f.name}”`).join(", ")} lớn hơn ${formatSize(MAX_FILE_SIZE)}, không gửi được.`
          : null,
      );
      return next;
    });
    ref.current?.focus();
  }, []);

  useImperativeHandle(handle, () => ({ addFiles }), [addFiles]);

  const removeFile = (i: number) => {
    setFiles((cur) => cur.filter((_, j) => j !== i));
    setFileError(null);
  };

  /* ---------- @nhắc tên ---------- */

  const suggestions = useMemo<Suggestion[]>(() => {
    if (!mention) return [];
    const q = fold(mention.query);
    const members = (state.members[channel.id] ?? [])
      .map((id) => state.users[id])
      .filter((u): u is PublicUser => !!u && u.id !== state.me.id);
    const scored = members
      .map((u) => {
        const un = fold(u.username);
        const dn = fold(displayName(u));
        const rank = un.startsWith(q) || dn.startsWith(q) ? 0 : un.includes(q) || dn.includes(q) ? 1 : -1;
        return { u, rank };
      })
      .filter((x) => x.rank >= 0)
      .sort((a, b) => a.rank - b.rank || displayName(a.u).localeCompare(displayName(b.u), "vi"))
      .slice(0, 6)
      .map(({ u }) => ({ kind: "user", user: u }) as Suggestion);
    if ("all".startsWith(q)) scored.push({ kind: "all" });
    return scored;
  }, [mention, state.members, state.users, state.me.id, channel.id]);

  const updateMention = (value: string, caret: number) => {
    const m = value.slice(0, caret).match(MENTION_QUERY_RE);
    if (m) {
      setMention({ query: m[1], start: caret - m[1].length - 1 });
      setActive(0);
    } else setMention(null);
  };

  const insertAtCaret = (insert: string, replaceFrom?: number) => {
    const el = ref.current;
    const caret = el?.selectionStart ?? text.length;
    const from = replaceFrom ?? (el?.selectionStart ?? text.length);
    const to = replaceFrom !== undefined ? caret : (el?.selectionEnd ?? text.length);
    const next = text.slice(0, from) + insert + text.slice(to);
    pendingCaret.current = from + insert.length;
    update(next);
  };

  const pickSuggestion = (s: Suggestion) => {
    if (!mention) return;
    insertAtCaret(`@${s.kind === "all" ? "all" : s.user.username} `, mention.start);
    setMention(null);
  };

  /* ---------- Soạn & gửi ---------- */

  const update = (v: string) => {
    setText(v);
    if (v.trim()) notifyTyping(channel.id);
  };

  const trimmed = text.trim();
  const tooLong = [...trimmed].length > MAX_LEN;
  const canSend = (trimmed.length > 0 || files.length > 0) && !tooLong;

  const submit = () => {
    if (!canSend) return;
    sendMessage(channel.id, trimmed, files.length ? files : undefined, parentId);
    setText("");
    setFiles([]);
    setFileError(null);
    setMention(null);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (mention && suggestions.length > 0) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const d = e.key === "ArrowDown" ? 1 : -1;
        setActive((i) => (i + d + suggestions.length) % suggestions.length);
        return;
      }
      if ((e.key === "Enter" || e.key === "Tab") && !e.shiftKey) {
        e.preventDefault();
        pickSuggestion(suggestions[active]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMention(null);
        return;
      }
    }
    // Điện thoại: Enter xuống dòng như mọi app chat, gửi bằng nút.
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing && !isTouchDevice()) {
      e.preventDefault();
      submit();
    }
  };

  const thumbs = useObjectUrls(files);

  return (
    <form
      className="composer"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      {mention && suggestions.length > 0 && (
        <ul className="mention-menu" role="listbox" aria-label="Nhắc tên">
          {suggestions.map((s, i) => (
            <li
              key={s.kind === "all" ? "all" : s.user.id}
              role="option"
              aria-selected={i === active}
              className="mention-option"
              onMouseDown={(e) => {
                e.preventDefault(); // giữ focus ở ô soạn
                pickSuggestion(s);
              }}
              onMouseEnter={() => setActive(i)}
            >
              {s.kind === "all" ? (
                <>
                  <span className="mention-all-icon" aria-hidden>
                    @
                  </span>
                  <span className="mention-option-name">all</span>
                  <span className="mention-option-sub">Báo cho cả kênh</span>
                </>
              ) : (
                <>
                  <Avatar user={s.user} size={24} online={!!state.online[s.user.id]} />
                  <span className="mention-option-name">{displayName(s.user)}</span>
                  <span className="mention-option-sub">@{s.user.username}</span>
                </>
              )}
            </li>
          ))}
        </ul>
      )}

      {emojiOpen && (
        <EmojiPicker
          onPick={(e) => insertAtCaret(e)}
          onClose={() => setEmojiOpen(false)}
        />
      )}

      <div className={`composer-box${tooLong ? " is-invalid" : ""}`}>
        {files.length > 0 && (
          <div className="composer-files">
            {files.map((f, i) =>
              thumbs[i] ? (
                <span key={i} className="composer-thumb" title={`${f.name} (${formatSize(f.size)})`}>
                  <img src={thumbs[i]!} alt={f.name} />
                  <button type="button" onClick={() => removeFile(i)} aria-label={`Bỏ ${f.name}`}>
                    <Icon name="x" size={12} />
                  </button>
                </span>
              ) : (
                <FileCard key={i} name={f.name} size={f.size} onRemove={() => removeFile(i)} />
              ),
            )}
          </div>
        )}

        <div className="composer-row">
          <button
            type="button"
            className="tool-btn"
            onClick={() => fileInput.current?.click()}
            title="Đính kèm file hoặc ảnh"
            aria-label="Đính kèm file hoặc ảnh"
          >
            <Icon name="paperclip" />
          </button>
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              addFiles(Array.from(e.target.files ?? []));
              e.target.value = ""; // chọn lại cùng file vẫn kích hoạt onChange
            }}
          />

          <textarea
            ref={ref}
            rows={1}
            value={text}
            onChange={(e) => {
              update(e.target.value);
              updateMention(e.target.value, e.target.selectionStart);
            }}
            onClick={(e) => updateMention(text, e.currentTarget.selectionStart)}
            onKeyDown={onKeyDown}
            onBlur={() => setTimeout(() => setMention(null), 100)}
            onPaste={(e) => {
              const pasted = Array.from(e.clipboardData.files);
              if (pasted.length) {
                e.preventDefault();
                // ảnh chụp màn hình dán vào thường tên "image.png": đặt tên rõ hơn
                addFiles(
                  pasted.map((f) =>
                    f.name === "image.png"
                      ? new File([f], `anh-dan-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.png`, {
                          type: f.type,
                        })
                      : f,
                  ),
                );
              }
            }}
            placeholder={parentId ? "Trả lời trong luồng…" : `Nhắn vào ${channel.is_private ? "" : "#"}${channel.name}`}
            aria-label={parentId ? "Trả lời trong luồng" : `Nhắn vào kênh ${channel.name}`}
            aria-autocomplete="list"
          />

          <button
            type="button"
            className={`tool-btn${emojiOpen ? " is-on" : ""}`}
            onClick={() => setEmojiOpen((v) => !v)}
            title="Chèn biểu tượng cảm xúc"
            aria-label="Chèn biểu tượng cảm xúc"
            aria-expanded={emojiOpen}
          >
            <Icon name="smile" />
          </button>
          <button className="send-btn" type="submit" disabled={!canSend} aria-label="Gửi" title="Gửi (Enter)">
            <Icon name="send" />
          </button>
        </div>
      </div>

      <p className={`composer-hint${fileError ? " is-error" : ""}`}>
        {fileError ??
          (tooLong
            ? `Tin nhắn dài tối đa ${MAX_LEN} ký tự (đang có ${[...trimmed].length}).`
            : state.status === "open"
              ? "Enter để gửi, Shift + Enter xuống dòng, @ để nhắc tên, kéo thả hoặc dán ảnh để gửi kèm"
              : "Đang mất kết nối, tin nhắn sẽ được gửi khi kết nối lại")}
      </p>
    </form>
  );
});
