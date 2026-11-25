import { useEffect, useRef, useState, type CSSProperties } from "react";
import type { Channel, PublicUser } from "../lib/api";
import { useCallState } from "../lib/calls";
import { channelLabel } from "../lib/channel";
import { displayName } from "../lib/format";
import { useGroupCall } from "../lib/groupcall";
import { canShareScreen } from "../lib/screenshare";
import { useVillageId } from "../lib/village";
import { fullscreenElement, toggleFullscreen } from "../lib/fullscreen";
import { useChat } from "../state/ChatProvider";
import { Avatar, Icon, Linkified } from "./ui";

function Media({
  stream,
  muted,
  mirror,
  videoRef,
}: {
  stream?: MediaStream;
  muted?: boolean;
  mirror?: boolean;
  videoRef?: React.RefObject<HTMLVideoElement | null>;
}) {
  const own = useRef<HTMLVideoElement>(null);
  const ref = videoRef ?? own;
  useEffect(() => {
    if (ref.current && ref.current.srcObject !== (stream ?? null)) ref.current.srcObject = stream ?? null;
  }, [stream, ref]);
  return <video ref={ref} autoPlay playsInline muted={muted} className={mirror ? "is-mirror" : undefined} />;
}

function useClock(since?: number) {
  const [, tick] = useState(0);
  useEffect(() => {
    if (!since) return;
    const t = window.setInterval(() => tick((x) => x + 1), 1000);
    return () => window.clearInterval(t);
  }, [since]);
  if (!since) return "";
  const s = Math.max(0, Math.floor((Date.now() - since) / 1000));
  const h = Math.floor(s / 3600);
  const mm = String(Math.floor((s % 3600) / 60)).padStart(2, "0");
  const ss = String(s % 60).padStart(2, "0");
  return h ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}

function Tile({
  user,
  stream,
  self,
  camOff,
  state,
  screen,
  pinned,
  onPin,
}: {
  user?: PublicUser;
  stream?: MediaStream;
  self?: boolean;
  camOff?: boolean;
  state?: string;
  /** Ô này đang hiện màn hình được chia sẻ (không lật gương, không cắt hình). */
  screen?: boolean;
  /** Ô đang được ghim (khung lớn). */
  pinned?: boolean;
  onPin: () => void;
}) {
  // có hình khi có track video đang bật; video vẫn được gắn (ẩn) để phát tiếng
  const [, force] = useState(0);
  useEffect(() => {
    if (!stream) return;
    const on = () => force((x) => x + 1);
    // track có thể được thêm sau (đàm phán lại khi chia sẻ màn hình): gắn lại mỗi lần vẽ
    const tracks = stream.getTracks();
    stream.addEventListener("addtrack", on);
    tracks.forEach((t) => {
      t.addEventListener("mute", on);
      t.addEventListener("unmute", on);
    });
    return () => {
      stream.removeEventListener("addtrack", on);
      tracks.forEach((t) => {
        t.removeEventListener("mute", on);
        t.removeEventListener("unmute", on);
      });
    };
  });
  const tileRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const [full, setFull] = useState(false);
  useEffect(() => {
    const on = () => setFull(fullscreenElement() === tileRef.current);
    document.addEventListener("fullscreenchange", on);
    document.addEventListener("webkitfullscreenchange", on);
    return () => {
      document.removeEventListener("fullscreenchange", on);
      document.removeEventListener("webkitfullscreenchange", on);
    };
  }, []);
  const hasVideo = !!stream?.getVideoTracks().some((t) => t.enabled && !t.muted) && (screen || !camOff);
  const name = self ? "Bạn" : displayName(user);
  const fullscreen = () => tileRef.current && toggleFullscreen(tileRef.current, videoRef.current);
  return (
    <div
      ref={tileRef}
      className={`gc-tile${hasVideo ? " has-video" : ""}${screen ? " is-screen" : ""}${pinned ? " is-pinned" : ""}`}
      onClick={onPin}
      onDoubleClick={(e) => {
        e.preventDefault();
        if (hasVideo) fullscreen();
      }}
      title={pinned ? "Bấm để bỏ ghim · bấm đúp: toàn màn hình" : "Bấm để ghim lên khung lớn · bấm đúp: toàn màn hình"}
    >
      <Media stream={stream} muted={self} mirror={self && !screen} videoRef={videoRef} />
      {!hasVideo && (
        <div className="gc-tile-avatar">
          <Avatar user={user} size={72} />
        </div>
      )}
      <span className="gc-tile-name">
        {name}
        {screen && <em> · màn hình</em>}
        {state && state !== "connected" && state !== "new" && (
          <em> · {state === "failed" ? "lỗi kết nối" : "đang nối…"}</em>
        )}
        {state === "new" && <em> · đang nối…</em>}
      </span>
      <span className="gc-tile-tools" onClick={(e) => e.stopPropagation()}>
        <button
          type="button"
          className={`gc-tile-btn${pinned ? " is-on" : ""}`}
          onClick={onPin}
          aria-pressed={!!pinned}
          aria-label={pinned ? `Bỏ ghim ${name}` : `Ghim ${name} lên khung lớn`}
          title={pinned ? "Bỏ ghim" : "Ghim lên khung lớn"}
        >
          <Icon name="pin" size={16} />
        </button>
        {hasVideo && (
          <button
            type="button"
            className="gc-tile-btn"
            onClick={fullscreen}
            aria-label={full ? "Thoát toàn màn hình" : `Xem ${name} toàn màn hình`}
            title={full ? "Thoát toàn màn hình" : "Toàn màn hình"}
          >
            <Icon name={full ? "shrink" : "expand"} size={16} />
          </button>
        )}
      </span>
    </div>
  );
}

/** Màn hình gọi nhóm (to) hoặc thanh nổi khi thu nhỏ; thẻ mời; thông báo lỗi. */
export function GroupCallOverlay() {
  const { groupCalls: g, state, selectChannel } = useChat();
  const gs = useGroupCall(g);
  const a = gs.active;
  const clock = useClock(
    a ? (gs.rooms[a.channelId]?.started_at ? Date.parse(gs.rooms[a.channelId].started_at!) : a.joinedAt) : undefined,
  );
  // ghim: "self" | peerId | "grid" (người dùng đã bỏ ghim, kể cả ghim tự động) | null (tự động)
  const [userPin, setUserPin] = useState<string | null>(null);
  const sharing = a ? Object.values(a.peers).find((p) => p.screen)?.peerId : undefined;
  useEffect(() => setUserPin(null), [a?.channelId]);
  // có người mới chia sẻ màn hình: lại cho tự ghim người đó (trừ khi đang ghim tay một người)
  useEffect(() => setUserPin((v) => (v === "grid" ? null : v)), [sharing]);
  // Làng Ma Sói vừa mở (ván bắt đầu / bấm "Vào làng"): thu nhỏ cuộc gọi để không che làng.
  const inVillage = useVillageId() !== null;
  const active = !!a;
  useEffect(() => {
    if (inVillage && active) g?.minimize(true);
  }, [inVillage, active, g]);
  if (!g) return null;

  const invite = gs.invite;
  const inviteCh = invite ? state.channels[invite.channelId] : undefined;
  const ch = a ? state.channels[a.channelId] : undefined;
  const peers = a ? Object.values(a.peers) : [];
  const count = peers.length + (a ? 1 : 0);
  const label = ch ? channelLabel(ch, state.users, state.me.id) : "Cuộc gọi nhóm";
  const present = (id: string) => id === "self" || peers.some((p) => p.peerId === id);
  let pinId: string | null =
    userPin === "grid" ? null : userPin && present(userPin) ? userPin : (sharing ?? null);
  if (count < 2) pinId = null; // một mình trong cuộc gọi: không có gì để ghim
  const togglePin = (id: string) => setUserPin(pinId === id ? "grid" : id);

  return (
    <>
      {gs.error && (
        <div className="gc-toast is-error" role="alert">
          <span>
            <Linkified text={gs.error} />
          </span>
          <button type="button" className="icon-btn" onClick={() => g.dismissError()} aria-label="Đóng">
            <Icon name="x" size={15} />
          </button>
        </div>
      )}

      {invite && !a && (
        <div className="gc-toast" role="alertdialog" aria-label="Lời mời gọi nhóm">
          <Avatar user={state.users[invite.by.id] ?? invite.by} size={36} />
          <span className="gc-toast-text">
            <strong>{displayName(state.users[invite.by.id] ?? invite.by)}</strong> bắt đầu gọi{" "}
            {invite.video ? "video" : "thoại"} nhóm
            {inviteCh ? ` trong ${channelLabel(inviteCh, state.users, state.me.id)}` : ""}
          </span>
          <button
            type="button"
            className="btn btn-sm btn-primary"
            onClick={() => {
              selectChannel(invite.channelId);
              void g.join(invite.channelId, invite.video);
            }}
          >
            Tham gia
          </button>
          <button type="button" className="btn btn-sm" onClick={() => g.dismissInvite()}>
            Bỏ qua
          </button>
        </div>
      )}

      {a && (
        <div className={`gc-overlay${a.minimized ? " is-min" : ""}${inVillage ? " in-village" : ""}`} role="dialog" aria-label={`Gọi nhóm ${label}`}>
          <div className="gc-head">
            <strong>{label}</strong>
            <span>
              {clock} · {count} người
            </span>
          </div>
          <div
            className={`gc-grid n${Math.min(count, 9)}${pinId ? " is-pinned" : ""}`}
            style={{ "--strip-n": Math.max(1, count - 1) } as CSSProperties}
          >
            <Tile
              user={state.users[state.me.id] ?? state.me}
              stream={a.screen ?? a.local}
              self
              camOff={a.camOff}
              screen={!!a.screen}
              pinned={pinId === "self"}
              onPin={() => count > 1 && togglePin("self")}
            />
            {peers.map((p) => (
              <Tile
                key={p.peerId}
                user={state.users[p.user.id] ?? p.user}
                stream={p.stream}
                state={p.state}
                screen={p.screen}
                pinned={pinId === p.peerId}
                onPin={() => togglePin(p.peerId)}
              />
            ))}
          </div>
          {peers.length === 0 && (
            <p className="gc-wait">
              {a.invited === undefined
                ? "Đang chờ người khác tham gia…"
                : a.invited === 0
                  ? "Không ai khác trong kênh đang online nên chưa ai nhận được lời mời."
                  : `Đã mời ${a.invited} người đang online trong kênh, đang chờ tham gia…`}
            </p>
          )}
          <div className="gc-controls">
            <button
              type="button"
              className={`call-btn${a.muted || gs.gameHold ? " is-off" : ""}`}
              onClick={() => g.toggleMute()}
              disabled={gs.gameHold}
              aria-label={gs.gameHold ? "Micro đang dùng cho thoại Ma Sói" : a.muted ? "Bật micro" : "Tắt micro"}
              title={gs.gameHold ? "Đang trong thoại Ma Sói: micro ở đây tạm tắt, tiếng người chơi chỉ đi qua thoại của ván" : undefined}
            >
              <Icon name={a.muted || gs.gameHold ? "micOff" : "mic"} size={22} />
              <span>{gs.gameHold ? "Mic ở ván" : "Micro"}</span>
            </button>
            {!!a.local?.getVideoTracks().length && (
              <button
                type="button"
                className={`call-btn${a.camOff ? " is-off" : ""}`}
                onClick={() => g.toggleCamera()}
                aria-label={a.camOff ? "Bật camera" : "Tắt camera"}
              >
                <Icon name={a.camOff ? "videoOff" : "video"} size={22} />
                <span>Camera</span>
              </button>
            )}
            {canShareScreen() && (
              <button
                type="button"
                className={`call-btn${a.screen ? " is-on" : ""}`}
                onClick={() => void g.toggleScreen()}
                aria-pressed={!!a.screen}
                aria-label={a.screen ? "Dừng chia sẻ màn hình" : "Chia sẻ màn hình"}
              >
                <Icon name={a.screen ? "screenOff" : "screen"} size={22} />
                <span>{a.screen ? "Dừng chia sẻ" : "Màn hình"}</span>
              </button>
            )}
            <button
              type="button"
              className="call-btn"
              onClick={() => g.minimize(!a.minimized)}
              aria-label={a.minimized ? "Phóng to" : "Thu nhỏ"}
            >
              <Icon name={a.minimized ? "arrowUp" : "arrowDown"} size={22} />
              <span>{a.minimized ? "Mở" : "Thu nhỏ"}</span>
            </button>
            <button type="button" className="call-btn is-danger" onClick={() => g.leave()} aria-label="Rời cuộc gọi">
              <Icon name="phoneOff" size={24} />
              <span>Rời</span>
            </button>
          </div>
        </div>
      )}
    </>
  );
}

/** Nút gọi nhóm trên đầu kênh + dải "đang có cuộc gọi". */
export function GroupCallButtons({ channel }: { channel: Channel }) {
  const { groupCalls: g, calls } = useChat();
  const gs = useGroupCall(g);
  const one = useCallState(calls);
  if (!g) return null;
  const busy = !!gs.active || (one.phase !== "idle" && one.phase !== "ended");
  const room = gs.rooms[channel.id];
  return (
    <>
      <button
        className={`icon-btn${room ? " is-live" : ""}`}
        disabled={busy}
        onClick={() => void g.join(channel.id, false)}
        title={room ? "Tham gia cuộc gọi nhóm" : "Gọi thoại nhóm"}
        aria-label={room ? "Tham gia cuộc gọi nhóm" : "Gọi thoại nhóm"}
      >
        <Icon name="phone" />
      </button>
      <button
        className="icon-btn"
        disabled={busy}
        onClick={() => void g.join(channel.id, true)}
        title={room ? "Tham gia bằng video" : "Gọi video nhóm"}
        aria-label={room ? "Tham gia cuộc gọi nhóm bằng video" : "Gọi video nhóm"}
      >
        <Icon name="video" />
      </button>
    </>
  );
}

export function GroupCallBanner({ channel }: { channel: Channel }) {
  const { groupCalls: g, state } = useChat();
  const gs = useGroupCall(g);
  const room = gs.rooms[channel.id];
  if (!g || !room || gs.active?.channelId === channel.id) return null;
  const names = room.participants.map((p) =>
    p.user.id === state.me.id ? "Bạn" : displayName(state.users[p.user.id] ?? p.user),
  );
  return (
    <div className="gc-banner" role="status">
      <span className="gc-live-dot" aria-hidden />
      <span className="gc-banner-text">
        Đang có cuộc gọi nhóm · {names.slice(0, 3).join(", ")}
        {names.length > 3 ? ` và ${names.length - 3} người khác` : ""}
      </span>
      <button
        type="button"
        className="btn btn-sm btn-primary"
        disabled={!!gs.active}
        onClick={() => void g.join(channel.id, false)}
      >
        Tham gia
      </button>
    </div>
  );
}

/** Dải "có phiên bản mới" (bản web đang chạy cũ hơn máy chủ). */
export function UpdateBanner() {
  const { state } = useChat();
  if (!state.updateAvailable) return null;
  return (
    <div className="update-banner" role="status">
      <span>Đã có phiên bản mới của Go Chat (có tính năng mới như gọi điện).</span>
      <button type="button" className="btn btn-sm btn-primary" onClick={() => window.location.reload()}>
        Tải lại
      </button>
    </div>
  );
}
