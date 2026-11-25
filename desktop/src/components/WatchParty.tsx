import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ApiError, type WatchPartyItem, type WatchPartyState } from "../lib/api";
import { displayName } from "../lib/format";
import { load } from "../lib/storage";
import { save } from "../lib/storage";
import { loadYouTubeAPI, type YTPlayerInstance } from "../lib/youtubePlayer";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, FormError, Icon } from "./ui";

function formatSec(sec: number): string {
  const s = Math.max(0, Math.floor(sec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${String(r).padStart(2, "0")}`;
  return `${m}:${String(r).padStart(2, "0")}`;
}

function expectedPlaybackSec(wp: WatchPartyState, clockOffsetMs: number): number {
  if (!wp.is_playing || !wp.anchor_updated_ms) return Math.max(0, wp.anchor_pos_sec);
  const serverNow = Date.now() + clockOffsetMs;
  const elapsed = Math.max(0, (serverNow - wp.anchor_updated_ms) / 1000);
  return Math.max(0, wp.anchor_pos_sec + elapsed);
}

/* ---------- Banner trong ChannelView (phải bấm "Vào nghe" mới phát) ---------- */

export function WatchPartyBanner({ channelId }: { channelId: number }) {
  const { state, dispatch, loadWatchParty, joinWatchParty, leaveWatchParty } = useChat();
  const wp = state.watchParties[channelId];
  const isOpen = state.watchPartyOpenChannelId === channelId;

  useEffect(() => {
    void loadWatchParty(channelId);
  }, [channelId, loadWatchParty]);

  const hasContent = !!wp && (!!wp.current_item || wp.queue.length > 0 || wp.listener_count > 0);
  if (!hasContent && !isOpen) return null;

  const cur = wp?.current_item;
  const joined = !!wp?.joined;

  return (
    <div className={`wp-banner${joined ? " is-joined" : ""}`} role="region" aria-label="Phòng nghe nhạc và xem chung">
      <div className="wp-banner-left">
        {cur?.thumbnail_url ? (
          <img src={cur.thumbnail_url} alt="" className="wp-banner-thumb" />
        ) : (
          <span className="wp-banner-icon" aria-hidden>
            <Icon name="music" size={18} />
          </span>
        )}
        <div className="wp-banner-text">
          <div className="wp-banner-title">
            {cur ? (
              <>
                <span className={`wp-dot${wp?.is_playing ? " is-live" : ""}`} aria-hidden />
                <strong>{cur.title}</strong>
              </>
            ) : (
              <strong>Phòng nghe nhạc &amp; xem chung</strong>
            )}
          </div>
          <div className="wp-banner-meta">
            <span>{wp?.listener_count ?? 0} đang nghe</span>
            <span>·</span>
            <span>{wp?.queue.length ?? 0} bài chờ</span>
            {!joined && cur && (
              <>
                <span>·</span>
                <span className="wp-join-hint">Bấm "Vào nghe" để nghe và xem cùng mọi người</span>
              </>
            )}
          </div>
        </div>
      </div>

      <div className="wp-banner-actions">
        {!joined ? (
          <button
            type="button"
            className="btn btn-primary btn-sm"
            onClick={() => {
              joinWatchParty(channelId);
              // điện thoại: không bung bảng hàng đợi đè lên khung chat, thanh phát nhỏ là đủ
              if (window.innerWidth > 720) dispatch({ type: "watchParty.open", channelId });
            }}
          >
            <Icon name="play" size={14} />
            Vào nghe
          </button>
        ) : (
          <button
            type="button"
            className="btn btn-sm"
            onClick={() => leaveWatchParty(channelId)}
            title="Rời phòng nghe chung"
          >
            <Icon name="logOut" size={14} />
            Rời
          </button>
        )}
        <button
          type="button"
          className="btn btn-sm btn-secondary"
          onClick={() => dispatch({ type: "watchParty.open", channelId: isOpen ? null : channelId })}
        >
          {isOpen ? "Thu gọn" : "Hàng đợi"}
        </button>
      </div>
    </div>
  );
}

/* ---------- Panel Hàng đợi & Điều khiển cộng tác ---------- */

export function WatchPartyPanel({ channelId }: { channelId: number }) {
  const { api, state, dispatch, joinWatchParty } = useChat();
  const wp = state.watchParties[channelId];
  const [urlInput, setUrlInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const [editUrl, setEditUrl] = useState("");
  const [dragId, setDragId] = useState<number | null>(null);
  const [posNow, setPosNow] = useState(0);
  const [showHistory, setShowHistory] = useState(false);

  useEffect(() => {
    if (!wp) return;
    const sync = () => setPosNow(expectedPlaybackSec(wp, state.watchPartyClockOffsetMs));
    sync();
    if (!wp.is_playing) return;
    const t = window.setInterval(sync, 500);
    return () => window.clearInterval(t);
  }, [wp, state.watchPartyClockOffsetMs]);

  if (!wp || state.watchPartyOpenChannelId !== channelId) return null;

  const cur = wp.current_item;
  const joined = wp.joined;

  const handleAdd = async (e: FormEvent) => {
    e.preventDefault();
    const u = urlInput.trim();
    if (!u) return;
    setBusy(true);
    setError(null);
    try {
      const res = await api.addWatchPartyItem(channelId, { url: u });
      dispatch({ type: "watchParty.set", state: res.state });
      setUrlInput("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không thêm được bài.");
    } finally {
      setBusy(false);
    }
  };

  const control = async (
    action: "play" | "pause" | "seek" | "next" | "prev" | "loop",
    extra?: { position_sec?: number; loop_mode?: string },
  ) => {
    setError(null);
    try {
      const next = await api.controlWatchParty(channelId, { action, ...extra });
      dispatch({ type: "watchParty.set", state: next });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Thao tác thất bại.");
    }
  };

  const startEdit = (item: WatchPartyItem) => {
    setEditingId(item.id);
    setEditTitle(item.title);
    setEditUrl(item.url);
    setError(null);
  };

  const saveEdit = async (item: WatchPartyItem) => {
    if (!editTitle.trim()) {
      setError("Tiêu đề không được để trống.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const payload: { title?: string; url?: string } = { title: editTitle.trim() };
      if (editUrl.trim() && editUrl.trim() !== item.url) {
        payload.url = editUrl.trim();
      }
      const next = await api.updateWatchPartyItem(channelId, item.id, payload);
      dispatch({ type: "watchParty.set", state: next });
      setEditingId(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không sửa được bài.");
    } finally {
      setBusy(false);
    }
  };

  const removeItem = async (itemId: number) => {
    setBusy(true);
    setError(null);
    try {
      const next = await api.deleteWatchPartyItem(channelId, itemId);
      dispatch({ type: "watchParty.set", state: next });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không xoá được bài.");
    } finally {
      setBusy(false);
    }
  };

  const moveItem = async (index: number, delta: -1 | 1) => {
    const target = index + delta;
    if (target < 0 || target >= wp.queue.length) return;
    const ids = wp.queue.map((q) => q.id);
    [ids[index], ids[target]] = [ids[target], ids[index]];
    try {
      const next = await api.reorderWatchPartyQueue(channelId, ids);
      dispatch({ type: "watchParty.set", state: next });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không đổi được thứ tự.");
    }
  };

  const onDropOn = async (targetId: number) => {
    if (dragId === null || dragId === targetId) {
      setDragId(null);
      return;
    }
    const ids = wp.queue.map((q) => q.id);
    const fromIdx = ids.indexOf(dragId);
    const toIdx = ids.indexOf(targetId);
    setDragId(null);
    if (fromIdx < 0 || toIdx < 0) return;
    ids.splice(fromIdx, 1);
    ids.splice(toIdx, 0, dragId);
    try {
      const next = await api.reorderWatchPartyQueue(channelId, ids);
      dispatch({ type: "watchParty.set", state: next });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không sắp xếp được hàng đợi.");
    }
  };

  const nextLoop = wp.loop_mode === "off" ? "queue" : wp.loop_mode === "queue" ? "single" : "off";
  const loopLabel = wp.loop_mode === "single" ? "Lặp 1 bài" : wp.loop_mode === "queue" ? "Lặp danh sách" : "Không lặp";

  return (
    <section className="wp-panel" aria-label="Bảng điều khiển Nghe nhạc và Hàng đợi">
      <div className="wp-panel-head">
        <div className="wp-panel-title">
          <span className="wp-panel-name">
            <Icon name="music" size={16} />
            Nghe nhạc &amp; xem chung
          </span>
          <span className="wp-listener-pills">
            {wp.listeners.slice(0, 6).map((u) => (
              <span key={u.id} title={displayName(state.users[u.id] ?? u)}>
                <Avatar user={state.users[u.id] ?? u} size={20} />
              </span>
            ))}
            <small>{wp.listener_count} đang nghe</small>
          </span>
        </div>
        <button
          type="button"
          className="icon-btn"
          onClick={() => dispatch({ type: "watchParty.open", channelId: null })}
          title="Thu gọn"
          aria-label="Thu gọn bảng nghe nhạc"
        >
          <Icon name="x" size={16} />
        </button>
      </div>

      <div className="wp-panel-grid">
        {/* Cột trái: Trình trạng hiện tại & Điều khiển */}
        <div className="wp-now-card">
          {cur ? (
            <>
              <div className="wp-now-head">
                <img src={cur.thumbnail_url} alt="" className="wp-now-thumb" />
                <div className="wp-now-info">
                  <div className="wp-now-badge">{wp.is_playing ? "Đang phát" : "Đang tạm dừng"}</div>
                  <div className="wp-now-title">{cur.title}</div>
                  <div className="wp-now-sub">
                    {cur.author ? `${cur.author} · ` : ""}
                    Thêm bởi {displayName(cur.added_by)}
                  </div>
                </div>
              </div>

              {!joined ? (
                <div className="wp-join-gate">
                  <p>Bạn chưa vào phòng. Vào để xem video và nghe cùng lúc với mọi người.</p>
                  <button type="button" className="btn btn-primary" onClick={() => joinWatchParty(channelId)}>
                    <Icon name="play" size={14} />
                    Vào nghe
                  </button>
                </div>
              ) : (
                <div className="wp-controls-box">
                  <div className="wp-seek-row">
                    <span>{formatSec(posNow)}</span>
                    <input
                      type="range"
                      min={0}
                      max={Math.max(cur.duration_sec || 300, Math.ceil(posNow) + 10)}
                      value={Math.floor(posNow)}
                      onChange={(e) => void control("seek", { position_sec: Number(e.target.value) })}
                      aria-label="Tua thời gian phát"
                    />
                    <span>{cur.duration_sec > 0 ? formatSec(cur.duration_sec) : "--:--"}</span>
                  </div>

                  <div className="wp-ctrl-buttons">
                    <button
                      type="button"
                      className="btn btn-sm wp-icon-btn"
                      onClick={() => void control("prev")}
                      title="Bài trước"
                      aria-label="Bài trước"
                    >
                      <Icon name="skipPrev" size={16} />
                    </button>
                    <button
                      type="button"
                      className="btn btn-sm btn-primary"
                      onClick={() => void control(wp.is_playing ? "pause" : "play")}
                    >
                      <Icon name={wp.is_playing ? "pause" : "play"} size={14} />
                      {wp.is_playing ? "Tạm dừng" : "Phát"}
                    </button>
                    <button type="button" className="btn btn-sm" onClick={() => void control("next")} title="Bài tiếp theo">
                      <Icon name="skipNext" size={14} />
                      Qua bài
                    </button>
                    <button
                      type="button"
                      className="btn btn-sm btn-secondary"
                      onClick={() => void control("loop", { loop_mode: nextLoop })}
                      title="Đổi chế độ lặp"
                    >
                      <Icon name="repeat" size={14} />
                      {loopLabel}
                    </button>
                  </div>
                </div>
              )}

              <div className="wp-now-edit-row">
                <button type="button" className="link-btn" onClick={() => startEdit(cur)}>
                  Sửa bài đang phát
                </button>
                <button type="button" className="link-btn" onClick={() => void removeItem(cur.id)}>
                  Xoá khỏi hàng đợi
                </button>
              </div>
              {editingId === cur.id && (
                <div className="wp-edit-inline">
                  <input value={editTitle} onChange={(e) => setEditTitle(e.target.value)} placeholder="Tên bài hát" />
                  <input value={editUrl} onChange={(e) => setEditUrl(e.target.value)} placeholder="Link YouTube" />
                  <div className="wp-edit-actions">
                    <button type="button" className="btn btn-xs btn-primary" disabled={busy} onClick={() => void saveEdit(cur)}>
                      Lưu
                    </button>
                    <button type="button" className="btn btn-xs" onClick={() => setEditingId(null)}>
                      Huỷ
                    </button>
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className="wp-empty-now">
              <p>Chưa có bài hát nào. Hãy dán link YouTube ở bên cạnh để bắt đầu hàng đợi!</p>
              {!joined && (
                <button type="button" className="btn btn-sm btn-primary" onClick={() => joinWatchParty(channelId)}>
                  Vào nghe luôn
                </button>
              )}
            </div>
          )}
        </div>

        {/* Cột phải: Hàng đợi cộng tác */}
        <div className="wp-queue-card">
          <form className="wp-add-form" onSubmit={(e) => void handleAdd(e)}>
            <input
              type="url"
              value={urlInput}
              onChange={(e) => setUrlInput(e.target.value)}
              placeholder="Dán link YouTube để đẩy vào cuối hàng đợi..."
              aria-label="Link YouTube"
            />
            <button type="submit" className="btn btn-primary btn-sm" disabled={busy || !urlInput.trim()}>
              + Thêm
            </button>
          </form>
          <FormError>{error}</FormError>

          <div className="wp-queue-head">
            <span>Hàng đợi ({wp.queue.length}) · kéo thả hoặc bấm mũi tên để đổi thứ tự</span>
            {wp.queue.length > 0 && (
              <button
                type="button"
                className="link-btn"
                onClick={() => {
                  void api.clearWatchPartyQueue(channelId, false).then((st) => dispatch({ type: "watchParty.set", state: st }));
                }}
              >
                Xoá hàng chờ
              </button>
            )}
          </div>

          {wp.queue.length === 0 ? (
            <p className="wp-queue-empty">Hàng đợi đang trống.</p>
          ) : (
            <ul className="wp-queue-list">
              {wp.queue.map((item, idx) => (
                <li
                  key={item.id}
                  className={`wp-queue-item${dragId === item.id ? " is-dragging" : ""}`}
                  draggable
                  onDragStart={() => setDragId(item.id)}
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={() => void onDropOn(item.id)}
                  onDragEnd={() => setDragId(null)}
                >
                  <span className="wp-drag-handle" title="Kéo thả sắp xếp">
                    ⠿
                  </span>
                  <span className="wp-q-idx">#{idx + 1}</span>
                  <img src={item.thumbnail_url} alt="" className="wp-q-thumb" />
                  <div className="wp-q-body">
                    {editingId === item.id ? (
                      <div className="wp-edit-inline">
                        <input value={editTitle} onChange={(e) => setEditTitle(e.target.value)} placeholder="Tên bài" />
                        <input value={editUrl} onChange={(e) => setEditUrl(e.target.value)} placeholder="Link YouTube" />
                        <div className="wp-edit-actions">
                          <button type="button" className="btn btn-xs btn-primary" disabled={busy} onClick={() => void saveEdit(item)}>
                            Lưu
                          </button>
                          <button type="button" className="btn btn-xs" onClick={() => setEditingId(null)}>
                            Huỷ
                          </button>
                        </div>
                      </div>
                    ) : (
                      <>
                        <div className="wp-q-title" title={item.title}>
                          {item.title}
                        </div>
                        <div className="wp-q-sub">
                          {item.author ? `${item.author} · ` : ""}
                          {displayName(item.added_by)}
                        </div>
                      </>
                    )}
                  </div>
                  {editingId !== item.id && (
                    <div className="wp-q-actions">
                      <button
                        type="button"
                        className="icon-btn"
                        disabled={idx === 0}
                        onClick={() => void moveItem(idx, -1)}
                        title="Đưa lên"
                        aria-label="Đưa lên"
                      >
                        <Icon name="arrowUp" size={15} />
                      </button>
                      <button
                        type="button"
                        className="icon-btn"
                        disabled={idx === wp.queue.length - 1}
                        onClick={() => void moveItem(idx, 1)}
                        title="Đưa xuống"
                        aria-label="Đưa xuống"
                      >
                        <Icon name="arrowDown" size={15} />
                      </button>
                      <button
                        type="button"
                        className="icon-btn"
                        onClick={() => startEdit(item)}
                        title="Sửa bài"
                        aria-label="Sửa bài"
                      >
                        <Icon name="pencil" size={15} />
                      </button>
                      <button
                        type="button"
                        className="icon-btn"
                        onClick={() => void removeItem(item.id)}
                        title="Xoá bài"
                        aria-label="Xoá bài"
                      >
                        <Icon name="trash" size={15} />
                      </button>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}

          {wp.history.length > 0 && (
            <div className="wp-history">
              <button type="button" className="link-btn" onClick={() => setShowHistory((v) => !v)}>
                {showHistory ? "Ẩn bài đã phát" : `Đã phát gần đây (${wp.history.length})`}
              </button>
              {showHistory && (
                <ul className="wp-history-list">
                  {wp.history.map((h) => (
                    <li key={h.id}>
                      <span className="wp-h-title">{h.title}</span>
                      <button
                        type="button"
                        className="link-btn"
                        onClick={() => {
                          void api.addWatchPartyItem(channelId, { url: h.url, title: h.title }).then((res) => {
                            dispatch({ type: "watchParty.set", state: res.state });
                          });
                        }}
                      >
                        + Thêm lại
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

/* ---------- Hộp thoại mở nhanh từ Composer (+) ---------- */

export function AddWatchPartyDialog({ channelId, onClose }: { channelId: number; onClose: () => void }) {
  const { api, dispatch, joinWatchParty } = useChat();
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [announce, setAnnounce] = useState(true);
  const [autoJoin, setAutoJoin] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await api.addWatchPartyItem(channelId, {
        url: url.trim(),
        title: title.trim() || undefined,
        announce_in_chat: announce,
      });
      dispatch({ type: "watchParty.set", state: res.state });
      if (autoJoin && !res.state.joined) {
        joinWatchParty(channelId);
      }
      if (window.innerWidth > 720) dispatch({ type: "watchParty.open", channelId });
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không thêm được link YouTube.");
      setBusy(false);
    }
  };

  return (
    <Dialog title="Nghe nhạc & xem YouTube chung" onClose={onClose}>
      <form className="dialog-form" onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>Link bài hát hoặc video YouTube</span>
          <input
            type="url"
            autoFocus
            required
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://www.youtube.com/watch?v=... hoặc https://youtu.be/..."
          />
        </label>
        <label className="field">
          <span>Tên hiển thị tuỳ chọn (bỏ trống sẽ tự lấy từ YouTube)</span>
          <input
            type="text"
            maxLength={140}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Ví dụ: Nhạc chill làm việc..."
          />
        </label>
        <label className="check-field">
          <input type="checkbox" checked={announce} onChange={(e) => setAnnounce(e.target.checked)} />
          <span>
            Gửi thẻ mời vào kênh chat
            <small>Để mọi người thấy và bấm vào nghe cùng.</small>
          </span>
        </label>
        <label className="check-field">
          <input type="checkbox" checked={autoJoin} onChange={(e) => setAutoJoin(e.target.checked)} />
          <span>
            Vào nghe luôn
            <small>Người khác tự bấm "Vào nghe" khi muốn nghe cùng.</small>
          </span>
        </label>
        <FormError>{error}</FormError>
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Huỷ
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy || !url.trim()}>
            {busy ? "Đang thêm..." : "Thêm vào hàng đợi"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}

/* ---------- Trình phát YouTube IFrame nổi toàn cục (chỉ mount khi Joined) ---------- */

export function WatchPartyPlayerDock() {
  const { api, state, dispatch, selectChannel, leaveWatchParty, watchPartyEnded, watchPartySyncDuration } = useChat();
  const joinedParty = useMemo(
    () => Object.values(state.watchParties).find((wp) => wp.joined),
    [state.watchParties],
  );
  // điện thoại: mặc định chỉ nghe, video to sẽ che hết khung chat
  const [showVideo, setShowVideo] = useState(() => load("wpShowVideo", window.innerWidth > 720));
  const [volume, setVolume] = useState(() => load("wpVolume", 80));
  const [playerErr, setPlayerErr] = useState<string | null>(null);
  const mountRef = useRef<HTMLDivElement>(null);
  const playerRef = useRef<YTPlayerInstance | null>(null);
  const loadedVideoRef = useRef<string | null>(null);
  const latestPartyRef = useRef<WatchPartyState | undefined>(undefined);
  const latestOffsetRef = useRef(0);
  latestPartyRef.current = joinedParty;
  latestOffsetRef.current = state.watchPartyClockOffsetMs;

  useEffect(() => {
    save("wpShowVideo", showVideo);
  }, [showVideo]);

  useEffect(() => {
    save("wpVolume", volume);
    playerRef.current?.setVolume(volume);
  }, [volume]);

  // Khởi tạo / đồng bộ YouTube Player chỉ khi có phòng đã Join và có current_item
  const channelId = joinedParty?.channel_id;
  const currentItem = joinedParty?.current_item;
  const currentVideoId = currentItem?.video_id ?? null;
  const currentItemId = currentItem?.id ?? null;

  useEffect(() => {
    if (!joinedParty || !currentVideoId || !mountRef.current) {
      if (playerRef.current) {
        playerRef.current.destroy();
        playerRef.current = null;
        loadedVideoRef.current = null;
      }
      setPlayerErr(null);
      return;
    }

    let cancelled = false;
    setPlayerErr(null);

    const syncPlayerState = (p: YTPlayerInstance, wp: WatchPartyState) => {
      const expected = expectedPlaybackSec(wp, latestOffsetRef.current);
      if (loadedVideoRef.current !== wp.current_item?.video_id && wp.current_item) {
        loadedVideoRef.current = wp.current_item.video_id;
        if (wp.is_playing) p.loadVideoById(wp.current_item.video_id, expected);
        else p.cueVideoById(wp.current_item.video_id, expected);
        return;
      }
      const curT = p.getCurrentTime?.() ?? 0;
      if (Math.abs(curT - expected) > 1.8) {
        p.seekTo(expected, true);
      }
      const st = p.getPlayerState?.();
      if (wp.is_playing && st !== 1 && st !== 3) {
        p.playVideo();
      } else if (!wp.is_playing && st === 1) {
        p.pauseVideo();
      }
    };

    if (playerRef.current) {
      syncPlayerState(playerRef.current, joinedParty);
      return;
    }

    void loadYouTubeAPI()
      .then((YT) => {
        if (cancelled || !mountRef.current) return;
        const el = document.createElement("div");
        mountRef.current.innerHTML = "";
        mountRef.current.appendChild(el);
        loadedVideoRef.current = currentVideoId;
        const startPos = expectedPlaybackSec(joinedParty, latestOffsetRef.current);
        const p = new YT.Player(el, {
          width: "100%",
          height: "100%",
          host: "https://www.youtube-nocookie.com",
          videoId: currentVideoId,
          playerVars: {
            autoplay: joinedParty.is_playing ? 1 : 0,
            start: Math.floor(startPos),
            playsinline: 1,
            rel: 0,
            modestbranding: 1,
            origin: window.location.origin,
          },
          events: {
            onReady: (ev) => {
              if (cancelled) return;
              ev.target.setVolume(volume);
              const latest = latestPartyRef.current;
              if (latest) {
                const dur = Math.floor(ev.target.getDuration?.() ?? 0);
                if (dur > 0 && latest.current_item && latest.current_item.duration_sec === 0) {
                  watchPartySyncDuration(latest.channel_id, latest.current_item.id, dur);
                }
                if (latest.is_playing) ev.target.playVideo();
                else ev.target.pauseVideo();
              }
            },
            onStateChange: (ev) => {
              if (cancelled) return;
              const latest = latestPartyRef.current;
              if (!latest?.current_item) return;
              if (ev.data === YT.PlayerState.ENDED) {
                watchPartyEnded(latest.channel_id, latest.current_item.id);
              } else if (ev.data === YT.PlayerState.PLAYING) {
                const dur = Math.floor(ev.target.getDuration?.() ?? 0);
                if (dur > 0 && latest.current_item.duration_sec === 0) {
                  watchPartySyncDuration(latest.channel_id, latest.current_item.id, dur);
                }
              }
            },
            onError: (ev) => {
              if (cancelled) return;
              if (ev.data === 101 || ev.data === 150 || ev.data === 100) {
                setPlayerErr("Video này chặn nhúng phát ngoài YouTube. Hãy mở trực tiếp hoặc qua bài khác.");
              } else {
                setPlayerErr("Không phát được video YouTube này.");
              }
            },
          },
        });
        playerRef.current = p;
      })
      .catch(() => {
        if (!cancelled) setPlayerErr("Không tải được trình phát YouTube.");
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channelId, currentVideoId, currentItemId]);

  // Đồng bộ play/pause/seek khi state của phòng thay đổi
  useEffect(() => {
    const p = playerRef.current;
    if (!p || !joinedParty?.current_item) return;
    if (loadedVideoRef.current !== joinedParty.current_item.video_id) return;
    const expected = expectedPlaybackSec(joinedParty, state.watchPartyClockOffsetMs);
    const curT = p.getCurrentTime?.() ?? 0;
    if (Math.abs(curT - expected) > 1.8) {
      p.seekTo(expected, true);
    }
    const st = p.getPlayerState?.();
    if (joinedParty.is_playing && st !== 1 && st !== 3) {
      p.playVideo();
    } else if (!joinedParty.is_playing && st === 1) {
      p.pauseVideo();
    }
  }, [joinedParty, state.watchPartyClockOffsetMs]);

  // Dọn dẹp player khi leave hoàn toàn
  useEffect(() => {
    if (!joinedParty && playerRef.current) {
      playerRef.current.destroy();
      playerRef.current = null;
      loadedVideoRef.current = null;
    }
  }, [joinedParty]);

  if (!joinedParty) return null;

  const chName = state.channels[joinedParty.channel_id]?.name ?? `Kênh #${joinedParty.channel_id}`;
  const cur = joinedParty.current_item;

  return (
    <aside className={`wp-dock${showVideo && cur ? " has-video" : ""}`} aria-label="Trình phát nhạc chung">
      <div className={`wp-dock-video${showVideo && cur ? "" : " is-hidden"}`}>
        <div ref={mountRef} className="wp-dock-iframe-wrap" />
        {playerErr && (
          <div className="wp-dock-error">
            <span>{playerErr}</span>
            {cur && (
              <div style={{ display: "flex", gap: "0.4rem", marginTop: "0.35rem" }}>
                <a href={cur.url} target="_blank" rel="noopener noreferrer" className="btn btn-xs btn-secondary">
                  Mở YouTube
                </a>
                <button
                  type="button"
                  className="btn btn-xs btn-primary"
                  onClick={() => void api.controlWatchParty(joinedParty.channel_id, { action: "next" })}
                >
                  Qua bài
                </button>
              </div>
            )}
          </div>
        )}
      </div>

      <div className="wp-dock-bar">
        <button
          type="button"
          className="wp-dock-title-btn"
          onClick={() => {
            selectChannel(joinedParty.channel_id);
            dispatch({ type: "watchParty.open", channelId: joinedParty.channel_id });
          }}
          title={`Mở phòng nghe chung tại #${chName}`}
        >
          <Icon name="music" size={16} />
          <span className="wp-dock-text">
            <strong>{cur ? cur.title : "Đang chờ bài mới"}</strong>
            <small>
              #{chName} · {joinedParty.listener_count} đang nghe
            </small>
          </span>
        </button>

        <div className="wp-dock-ctrls">
          {cur && (
            <>
              <button
                type="button"
                className="icon-btn"
                onClick={() =>
                  void api
                    .controlWatchParty(joinedParty.channel_id, { action: joinedParty.is_playing ? "pause" : "play" })
                    .then((st) => dispatch({ type: "watchParty.set", state: st }))
                }
                title={joinedParty.is_playing ? "Tạm dừng" : "Phát"}
                aria-label={joinedParty.is_playing ? "Tạm dừng" : "Phát"}
              >
                <Icon name={joinedParty.is_playing ? "pause" : "play"} size={16} />
              </button>
              <button
                type="button"
                className="icon-btn"
                onClick={() =>
                  void api
                    .controlWatchParty(joinedParty.channel_id, { action: "next" })
                    .then((st) => dispatch({ type: "watchParty.set", state: st }))
                }
                title="Qua bài"
                aria-label="Qua bài"
              >
                <Icon name="skipNext" size={16} />
              </button>
              <button
                type="button"
                className="icon-btn"
                onClick={() => setShowVideo((v) => !v)}
                title={showVideo ? "Thu gọn video (chỉ nghe)" : "Hiện video"}
                aria-label={showVideo ? "Thu gọn video" : "Hiện video"}
              >
                <Icon name={showVideo ? "shrink" : "video"} size={16} />
              </button>
            </>
          )}
          <input
            type="range"
            className="wp-dock-vol"
            min={0}
            max={100}
            value={volume}
            onChange={(e) => setVolume(Number(e.target.value))}
            title={`Âm lượng: ${volume}%`}
            aria-label="Âm lượng trình phát"
          />
          <button
            type="button"
            className="icon-btn"
            onClick={() => leaveWatchParty(joinedParty.channel_id)}
            title="Rời phòng nghe chung"
            aria-label="Rời phòng nghe chung"
          >
            <Icon name="x" size={15} />
          </button>
        </div>
      </div>
    </aside>
  );
}
