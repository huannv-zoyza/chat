import { useEffect, useState } from "react";
import { farmScreen, farmXmasPokes, useFarmOpen, type XmasPoke } from "../lib/farm";
import { ApiError } from "../lib/api";
import { load, save } from "../lib/storage";
import { useChat } from "../state/ChatProvider";
import { Icon } from "./ui";

/*
 * Tin Giáng sinh: tin chung cả nhóm (ai ném tuyết ai, treo / mở tất, nhặt quà…) và tin riêng của mình (bị ném tuyết, có tất mới, tất được mở, quà ở vườn bị nhặt). Hiện ở góc
 * màn hình cả khi đang ở chat để người chơi phản ứng ngay: ném trả, mở nông trại.
 */

type Item = XmasPoke & { id: number; done?: string; busy?: boolean };

const SHOW_MS = 9000;
const INBOX_MS = 25000; // thẻ "trong lúc bạn vắng mặt" để lâu hơn
const MAX = 3;
const INBOX_LINES = 6;
let seq = 0;

export function XmasNotice() {
  const { api, state } = useChat();
  const [items, setItems] = useState<Item[]>([]);
  const farmOpen = useFarmOpen(); // đang ở nông trại thì không cần nút "Xem vườn"
  // mốc tin Noel đã xem (theo người dùng, trên máy này): tin mới hơn mà lúc đó offline thì hiện khi mở app
  const seenKey = `farmXmasSeen.${state.me.id}`;

  const push = (p: XmasPoke, ms = SHOW_MS) => {
    const id = ++seq;
    setItems((xs) => [...xs, { ...p, id }].slice(-MAX));
    window.setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), ms);
  };

  useEffect(
    () =>
      farmXmasPokes.on((p) => {
        if (p.id && p.id > load<number>(seenKey, 0)) save(seenKey, p.id);
        push(p);
      }),
    [seenKey],
  );

  // mở app: lấy tin Noel đã lỡ. Lần đầu trên máy này thì chỉ lấy mốc, không dội tin cũ.
  useEffect(() => {
    let alive = true;
    const seen = load<number | null>(seenKey, null);
    api
      .farmXmasInbox(seen ?? -1)
      .then(({ items: news, last }) => {
        if (!alive) return;
        save(seenKey, Math.max(last, load<number>(seenKey, 0)));
        if (news.length === 0) return;
        const lines = news.slice(0, INBOX_LINES).map((n) => n.text);
        if (news.length > INBOX_LINES) lines.push(`… và ${news.length - INBOX_LINES} tin khác`);
        push({ kind: "inbox", text: "🎄 Trong lúc bạn vắng mặt:", lines }, INBOX_MS);
      })
      .catch(() => {}); // ngoài sự kiện / lỗi mạng: bỏ qua
    return () => {
      alive = false;
    };
  }, [api, seenKey]);

  const patch = (id: number, p: Partial<Item>) => setItems((xs) => xs.map((x) => (x.id === id ? { ...x, ...p } : x)));
  const close = (id: number) => setItems((xs) => xs.filter((x) => x.id !== id));

  const throwBack = async (it: Item) => {
    if (!it.from_id) return;
    patch(it.id, { busy: true });
    try {
      const f = await api.farmXmasSnowball(it.from_id);
      const s = f.gained?.snow;
      patch(it.id, {
        busy: false,
        done:
          s === "miss"
            ? "Trượt mất rồi! ⛄"
            : `Trúng rồi! ⛄ +${f.gained?.candy ?? 0} 🍬${s === "revenge" ? " (thưởng ném trả)" : ""}`,
      });
    } catch (e) {
      patch(it.id, { busy: false, done: e instanceof ApiError ? e.message : "Không ném được, thử lại sau." });
    }
  };

  if (items.length === 0) return null;
  return (
    <div className="xmas-notice" role="status" aria-live="polite">
      {items.map((it) => (
        <div key={it.id} className={`xmas-notice-item is-${it.kind}`}>
          <p>{it.done ?? it.text}</p>
          {it.lines && (
            <ul className="xmas-notice-lines">
              {it.lines.map((l, i) => (
                <li key={i}>{l}</li>
              ))}
            </ul>
          )}
          <div className="xmas-notice-actions">
            {it.kind === "snowball" && it.from_id && !it.done && (
              <button type="button" className="btn btn-sm btn-primary" disabled={it.busy} onClick={() => void throwBack(it)}>
                ⛄ Ném trả
              </button>
            )}
            {(it.kind === "sock" || it.kind === "drop_claimed" || it.kind === "inbox" || it.done) && !farmOpen && (
              <button
                type="button"
                className="btn btn-sm"
                onClick={() => {
                  farmScreen.open();
                  close(it.id);
                }}
              >
                Xem vườn
              </button>
            )}
            <button type="button" className="icon-btn" onClick={() => close(it.id)} aria-label="Đóng">
              <Icon name="x" size={14} />
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}
