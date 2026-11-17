import { useEffect, useRef, useState } from "react";
import { ApiError, type Embed, type GifResult } from "../lib/api";
import { load, save } from "../lib/storage";
import { STICKER_PACKS, findSticker, stickerLabel, type Sticker } from "../lib/stickers";
import { useChat } from "../state/ChatProvider";
import { StickerView } from "./Embeds";
import { Icon } from "./ui";

const RECENT_KEY = "recentStickers";
const RECENT_MAX = 12;

function rememberSticker(id: string) {
  const list = load<string[]>(RECENT_KEY, []).filter((x) => x !== id);
  save(RECENT_KEY, [id, ...list].slice(0, RECENT_MAX));
}

type Tab = "sticker" | "gif";

type Props = {
  onPick: (embed: Embed) => void;
  onClose: () => void;
  /** Chưa có GIPHY key: cho chọn file GIF từ máy. */
  onPickFile: () => void;
};

/** Bảng chọn sticker / GIF (popover trên ô soạn tin). Chọn là gửi luôn. */
export function StickerPicker({ onPick, onClose, onPickFile }: Props) {
  const [tab, setTab] = useState<Tab>(() => load<Tab>("stickerTab", "sticker"));
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => save("stickerTab", tab), [tab]);

  // Bấm ra ngoài hoặc Esc để đóng (như bảng emoji).
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    const t = window.setTimeout(() => document.addEventListener("mousedown", onDown));
    document.addEventListener("keydown", onKey, true);
    return () => {
      window.clearTimeout(t);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [onClose]);

  return (
    <div className="emoji-picker sticker-picker" ref={ref} role="dialog" aria-label="Gửi sticker hoặc GIF">
      <div className="sticker-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "sticker"}
          className="sticker-tab"
          onClick={() => setTab("sticker")}
        >
          Sticker
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "gif"}
          className="sticker-tab"
          onClick={() => setTab("gif")}
        >
          GIF
        </button>
      </div>
      {tab === "sticker" ? (
        <StickerTab
          onPick={(s) => {
            rememberSticker(s.id);
            onPick({ type: "sticker", id: s.id });
          }}
        />
      ) : (
        <GifTab onPick={onPick} onPickFile={onPickFile} />
      )}
    </div>
  );
}

function StickerTab({ onPick }: { onPick: (s: Sticker) => void }) {
  const recent = load<string[]>(RECENT_KEY, [])
    .map(findSticker)
    .filter((s): s is Sticker => !!s);
  const packs = [
    ...(recent.length ? [{ id: "recent", label: "Dùng gần đây", icon: "🕘", stickers: recent }] : []),
    ...STICKER_PACKS,
  ];
  const [packId, setPackId] = useState(packs[0].id);
  const pack = packs.find((p) => p.id === packId) ?? packs[0];

  return (
    <>
      <div className="emoji-tabs" role="tablist" aria-label="Bộ sticker">
        {packs.map((p) => (
          <button
            key={p.id}
            type="button"
            role="tab"
            aria-selected={p.id === pack.id}
            className="emoji-tab"
            title={p.label}
            onClick={() => setPackId(p.id)}
          >
            {p.icon}
          </button>
        ))}
      </div>
      <p className="emoji-group-label">{pack.label}</p>
      <div className="sticker-grid">
        {pack.stickers.map((s) => (
          <button
            key={s.id}
            type="button"
            className="sticker-btn"
            title={stickerLabel(s)}
            aria-label={`Gửi sticker ${stickerLabel(s)}`}
            onClick={() => onPick(s)}
          >
            <StickerView sticker={s} size="pick" />
          </button>
        ))}
      </div>
    </>
  );
}

function GifTab({ onPick, onPickFile }: { onPick: (e: Embed) => void; onPickFile: () => void }) {
  const { api } = useChat();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<GifResult[] | null>(null);
  const [enabled, setEnabled] = useState(true);
  const [next, setNext] = useState<number | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Gõ xong ~300ms mới tìm. Ô trống: GIF đang thịnh hành.
  useEffect(() => {
    let alive = true;
    setError(null);
    const t = window.setTimeout(() => {
      api
        .gifs(query.trim())
        .then((res) => {
          if (!alive) return;
          setEnabled(res.enabled);
          setResults(res.results);
          setNext(res.next_offset);
        })
        .catch((err) => alive && setError(err instanceof ApiError ? err.message : "Không tải được GIF."));
    }, 300);
    return () => {
      alive = false;
      window.clearTimeout(t);
    };
  }, [api, query]);

  const more = async () => {
    if (next === undefined || loadingMore) return;
    setLoadingMore(true);
    try {
      const res = await api.gifs(query.trim(), next);
      setResults((r) => [...(r ?? []), ...res.results.filter((x) => !r?.some((y) => y.id === x.id))]);
      setNext(res.next_offset);
    } catch {
      /* bỏ qua, người dùng cuộn lại để thử */
    } finally {
      setLoadingMore(false);
    }
  };

  if (!enabled) {
    return (
      <div className="gif-off">
        <p>
          Tìm GIF chưa được bật trên server này. Quản trị viên chạy <code>make giphy-key KEY=…</code> với API key GIPHY
          miễn phí để bật.
        </p>
        <button type="button" className="btn btn-sm" onClick={onPickFile}>
          <Icon name="paperclip" size={15} />
          Gửi file GIF từ máy
        </button>
      </div>
    );
  }

  return (
    <>
      <div className="gif-search">
        <input
          className="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Tìm GIF (tiếng Việt hoặc tiếng Anh)"
          aria-label="Tìm GIF"
          autoFocus
        />
      </div>
      {error && <p className="gif-note">{error}</p>}
      {!results && !error && <p className="gif-note">Đang tải…</p>}
      {results && results.length === 0 && <p className="gif-note">Không có GIF nào khớp “{query}”.</p>}
      {results && results.length > 0 && (
        <div
          className="gif-grid"
          onScroll={(e) => {
            const el = e.currentTarget;
            if (el.scrollHeight - el.scrollTop - el.clientHeight < 120) void more();
          }}
        >
          {results.map((g) => (
            <button
              key={g.id}
              type="button"
              className="gif-btn"
              title={g.title}
              aria-label={`Gửi GIF ${g.title || ""}`.trim()}
              onClick={() =>
                onPick({
                  type: "gif",
                  id: g.id,
                  url: g.url,
                  preview: g.preview,
                  width: g.width,
                  height: g.height,
                  title: g.title,
                })
              }
            >
              <img src={g.preview || g.url} alt="" loading="lazy" referrerPolicy="no-referrer" />
            </button>
          ))}
        </div>
      )}
      <p className="gif-attribution">Powered by GIPHY</p>
    </>
  );
}
