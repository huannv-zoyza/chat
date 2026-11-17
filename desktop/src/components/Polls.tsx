import { useRef, useState, type FormEvent } from "react";
import { ApiError, type Message, type Poll } from "../lib/api";
import { displayName } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, FormError, Icon } from "./ui";

const MAX_OPTIONS = 10;

/** Lựa chọn của mình: bình chọn công khai thì suy ra từ voter_ids, ẩn danh thì dùng my_votes. */
function myVotes(poll: Poll, meId: number): number[] {
  if (!poll.anonymous) return poll.options.filter((o) => o.voter_ids?.includes(meId)).map((o) => o.id);
  return poll.my_votes ?? [];
}

/* ---------- Bình chọn trong tin nhắn ---------- */

export function PollView({ message }: { message: Message }) {
  const { state, vote, closePoll } = useChat();
  const poll = message.poll!;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [voters, setVoters] = useState(false);
  const mine = myVotes(poll, state.me.id);
  const closed = !!poll.closed_at;
  const isOwner = message.user_id === state.me.id;
  const top = Math.max(0, ...poll.options.map((o) => o.votes));

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không gửi được, thử lại.");
    } finally {
      setBusy(false);
    }
  };

  const pick = (id: number) => {
    if (closed || busy) return;
    let next: number[];
    if (poll.multiple) next = mine.includes(id) ? mine.filter((x) => x !== id) : [...mine, id];
    else next = mine.includes(id) ? [] : [id]; // bấm lại đáp án đang chọn để rút phiếu
    void run(() => vote(message.id, next));
  };

  const meta = [
    poll.multiple ? "Chọn nhiều đáp án" : "Chọn một đáp án",
    poll.anonymous && "Ẩn danh",
    closed && "Đã kết thúc",
  ].filter(Boolean);

  return (
    <div className={`poll${closed ? " is-closed" : ""}`}>
      <div className="poll-head">
        <span className="poll-icon" aria-hidden>
          <Icon name="poll" size={16} />
        </span>
        <div>
          <p className="poll-question">{poll.question}</p>
          <p className="poll-meta">{meta.join(" · ")}</p>
        </div>
      </div>

      <ul className="poll-options" role={poll.multiple ? "group" : "radiogroup"} aria-label={poll.question}>
        {poll.options.map((o) => {
          const chosen = mine.includes(o.id);
          const pct = poll.voters > 0 ? Math.round((o.votes / poll.voters) * 100) : 0;
          const leading = closed && o.votes > 0 && o.votes === top;
          return (
            <li key={o.id}>
              <button
                type="button"
                className={`poll-option${chosen ? " is-chosen" : ""}${leading ? " is-leading" : ""}`}
                role={poll.multiple ? "checkbox" : "radio"}
                aria-checked={chosen}
                disabled={closed || busy}
                onClick={() => pick(o.id)}
              >
                <span className="poll-bar" style={{ width: `${pct}%` }} aria-hidden />
                <span className={`poll-check${poll.multiple ? " is-box" : ""}`} aria-hidden>
                  {chosen && <Icon name="check" size={13} />}
                </span>
                <span className="poll-text">{o.text}</span>
                {!poll.anonymous && o.voter_ids && o.voter_ids.length > 0 && (
                  <span className="poll-avatars" aria-hidden>
                    {o.voter_ids.slice(0, 3).map((id) => (
                      <Avatar key={id} user={state.users[id]} size={18} />
                    ))}
                  </span>
                )}
                <span className="poll-count">
                  {o.votes > 0 ? `${pct}%` : ""}
                  <span className="sr-only">, {o.votes} phiếu</span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>

      <div className="poll-foot">
        {!poll.anonymous && poll.voters > 0 ? (
          <button type="button" className="link-btn" onClick={() => setVoters(true)}>
            {poll.voters} người đã bình chọn
          </button>
        ) : (
          <span>{poll.voters > 0 ? `${poll.voters} người đã bình chọn` : "Chưa có ai bình chọn"}</span>
        )}
        {isOwner && (
          <button
            type="button"
            className="link-btn"
            disabled={busy}
            onClick={() => void run(() => closePoll(message.id, !closed))}
          >
            {closed ? "Mở lại bình chọn" : "Kết thúc bình chọn"}
          </button>
        )}
      </div>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
      {voters && <PollVotersDialog poll={poll} onClose={() => setVoters(false)} />}
    </div>
  );
}

function PollVotersDialog({ poll, onClose }: { poll: Poll; onClose: () => void }) {
  const { state } = useChat();
  return (
    <Dialog title="Ai đã chọn gì" onClose={onClose}>
      <div className="poll-voters">
        {poll.options.map((o) => (
          <section key={o.id}>
            <h4>
              {o.text} <span>{o.votes} phiếu</span>
            </h4>
            {o.voter_ids && o.voter_ids.length > 0 ? (
              <ul>
                {o.voter_ids.map((id) => (
                  <li key={id}>
                    <Avatar user={state.users[id]} size={26} />
                    {id === state.me.id ? "Bạn" : displayName(state.users[id])}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="muted">Chưa có ai chọn.</p>
            )}
          </section>
        ))}
      </div>
    </Dialog>
  );
}

/* ---------- Tạo bình chọn ---------- */

export function CreatePollDialog({ channelId, onClose }: { channelId: number; onClose: () => void }) {
  const { createPoll } = useChat();
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState(["", ""]);
  const [multiple, setMultiple] = useState(false);
  const [anonymous, setAnonymous] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const optionRefs = useRef<(HTMLInputElement | null)[]>([]);

  const filled = options.map((o) => o.trim()).filter(Boolean);

  const setOption = (i: number, v: string) => {
    setOptions((list) => {
      const next = list.map((o, j) => (j === i ? v : o));
      // gõ vào ô cuối thì tự thêm ô trống mới (tối đa 10)
      if (i === next.length - 1 && v.trim() && next.length < MAX_OPTIONS) next.push("");
      return next;
    });
  };

  const removeOption = (i: number) => setOptions((list) => (list.length <= 2 ? list : list.filter((_, j) => j !== i)));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!question.trim()) return setError("Hãy nhập câu hỏi.");
    if (filled.length < 2) return setError("Cần ít nhất 2 lựa chọn.");
    const lower = filled.map((o) => o.toLowerCase());
    if (new Set(lower).size !== lower.length) return setError("Có hai lựa chọn giống nhau.");
    setBusy(true);
    try {
      await createPoll(channelId, { question: question.trim(), options: filled, multiple, anonymous });
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không tạo được bình chọn.");
      setBusy(false);
    }
  };

  return (
    <Dialog title="Tạo bình chọn" onClose={onClose}>
      <form className="dialog-form" onSubmit={submit}>
        <label className="field">
          <span>Câu hỏi</span>
          <input
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder="Ví dụ: Trưa nay ăn gì?"
            maxLength={300}
            autoFocus
          />
        </label>
        <fieldset className="field poll-edit-options">
          <legend>Lựa chọn</legend>
          {options.map((o, i) => (
            <div key={i} className="poll-edit-row">
              <input
                ref={(el) => {
                  optionRefs.current[i] = el;
                }}
                value={o}
                onChange={(e) => setOption(i, e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    optionRefs.current[i + 1]?.focus();
                  }
                }}
                placeholder={`Lựa chọn ${i + 1}`}
                maxLength={100}
                aria-label={`Lựa chọn ${i + 1}`}
              />
              {options.length > 2 && (
                <button
                  type="button"
                  className="icon-btn"
                  onClick={() => removeOption(i)}
                  aria-label={`Bỏ lựa chọn ${i + 1}`}
                >
                  <Icon name="x" size={16} />
                </button>
              )}
            </div>
          ))}
          <small>Tối đa {MAX_OPTIONS} lựa chọn. Gõ vào ô cuối sẽ tự thêm ô mới.</small>
        </fieldset>
        <label className="check-field">
          <input type="checkbox" checked={multiple} onChange={(e) => setMultiple(e.target.checked)} />
          <span>
            Cho chọn nhiều đáp án
            <small>Mỗi người được chọn nhiều lựa chọn cùng lúc.</small>
          </span>
        </label>
        <label className="check-field">
          <input type="checkbox" checked={anonymous} onChange={(e) => setAnonymous(e.target.checked)} />
          <span>
            Bình chọn ẩn danh
            <small>Mọi người chỉ thấy số phiếu, không thấy ai chọn gì. Không đổi được sau khi tạo.</small>
          </span>
        </label>
        <FormError>{error}</FormError>
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Huỷ
          </button>
          <button className="btn btn-primary" disabled={busy}>
            {busy ? "Đang tạo…" : "Tạo bình chọn"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
