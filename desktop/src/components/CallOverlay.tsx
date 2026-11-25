import { useEffect, useRef, useState } from "react";
import { useCallState } from "../lib/calls";
import { useGroupCall } from "../lib/groupcall";
import { canShareScreen } from "../lib/screenshare";
import { fullscreenElement, toggleFullscreen } from "../lib/fullscreen";
import { displayName } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { Avatar, Icon, Linkified } from "./ui";

function useTimer(startedAt?: number) {
  const [, tick] = useState(0);
  useEffect(() => {
    if (!startedAt) return;
    const t = window.setInterval(() => tick((x) => x + 1), 1000);
    return () => window.clearInterval(t);
  }, [startedAt]);
  if (!startedAt) return "";
  const s = Math.floor((Date.now() - startedAt) / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
}

function Media({
  stream,
  muted,
  className,
  onClick,
  onDoubleClick,
  title,
}: {
  stream?: MediaStream;
  muted?: boolean;
  className?: string;
  onClick?: () => void;
  onDoubleClick?: () => void;
  title?: string;
}) {
  const ref = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    if (ref.current && ref.current.srcObject !== (stream ?? null)) ref.current.srcObject = stream ?? null;
  }, [stream]);
  return (
    <video
      ref={ref}
      className={className}
      autoPlay
      playsInline
      muted={muted}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      title={title}
    />
  );
}

/** Màn hình cuộc gọi (phủ lên app). */
export function CallOverlay() {
  const { calls, state } = useChat();
  const call = useCallState(calls);
  const timer = useTimer(call.phase === "active" ? call.startedAt : undefined);
  // đổi chỗ khung lớn / khung nhỏ (vd. xem to màn hình mình đang chia sẻ)
  const [swap, setSwap] = useState(false);
  const overlayRef = useRef<HTMLDivElement>(null);
  const [full, setFull] = useState(false);
  useEffect(() => {
    const on = () => setFull(!!fullscreenElement() && fullscreenElement() === overlayRef.current);
    document.addEventListener("fullscreenchange", on);
    document.addEventListener("webkitfullscreenchange", on);
    return () => {
      document.removeEventListener("fullscreenchange", on);
      document.removeEventListener("webkitfullscreenchange", on);
    };
  }, []);
  useEffect(() => {
    if (call.phase === "idle" || call.phase === "ended") setSwap(false);
  }, [call.phase]);
  if (!calls || call.phase === "idle") return null;

  const peer = (call.peer && state.users[call.peer.id]) ?? call.peer;
  const name = displayName(peer);
  // gọi thoại: chỉ hiện hình khi người kia chia sẻ màn hình
  const remoteVideo = !!call.remote?.getVideoTracks().length && (call.video || !!call.remoteScreen);
  const localVideo = !!call.local?.getVideoTracks().length && !call.camOff;
  const inCall = call.phase === "connecting" || call.phase === "active";
  const mineShown = !!call.screen || (localVideo && (inCall || call.phase === "outgoing"));
  // chỉ đổi chỗ được khi cả hai bên đều có hình
  const swapped = swap && inCall && remoteVideo && mineShown;
  const goFull = () => overlayRef.current && toggleFullscreen(overlayRef.current);

  const status =
    call.phase === "incoming"
      ? call.video
        ? "Cuộc gọi video đến"
        : "Cuộc gọi thoại đến"
      : call.phase === "outgoing"
        ? "Đang đổ chuông…"
        : call.phase === "connecting"
          ? "Đang kết nối…"
          : call.phase === "active"
            ? timer
            : call.message;

  return (
    <div
      ref={overlayRef}
      className={`call-overlay${remoteVideo && inCall ? " has-video" : ""}`}
      role="dialog"
      aria-label={`Cuộc gọi với ${name}`}
    >
      {/* âm thanh/hình của người kia: luôn gắn vào thẻ video (kể cả gọi thoại) để phát tiếng */}
      {inCall && (
        <Media
          stream={call.remote}
          className={!remoteVideo ? "call-remote is-audio" : swapped ? "call-local is-peer" : "call-remote"}
          onClick={swapped ? () => setSwap(false) : undefined}
          onDoubleClick={!swapped && remoteVideo ? goFull : undefined}
          title={swapped ? `Bấm để xem ${name} ở khung lớn` : remoteVideo ? "Bấm đúp: toàn màn hình" : undefined}
        />
      )}

      {call.notice && (
        <p className="call-notice" role="alert">
          {call.notice}
        </p>
      )}

      {!(remoteVideo && inCall) && (
        <div className="call-center">
          <span className={`call-avatar${call.phase === "incoming" || call.phase === "outgoing" ? " is-ringing" : ""}`}>
            <Avatar user={peer} size={96} />
          </span>
          <h2 className="call-name">{name}</h2>
          <p className="call-status">
            <Linkified text={status} />
          </p>
        </div>
      )}
      {remoteVideo && inCall && (
        <div className="call-top">
          <strong>{name}</strong> <span>{call.remoteScreen ? `đang chia sẻ màn hình · ${status}` : status}</span>
        </div>
      )}

      {mineShown && (
        <Media
          stream={call.screen ?? call.local}
          muted
          className={
            swapped
              ? `call-remote${call.screen ? "" : " is-mirror"}`
              : `call-local${call.screen ? " is-screen" : ""}`
          }
          onClick={remoteVideo && inCall && !swapped ? () => setSwap(true) : undefined}
          title={remoteVideo && inCall && !swapped ? "Bấm để xem khung này to" : undefined}
        />
      )}

      <div className="call-controls">
        {call.phase === "incoming" ? (
          <>
            <button type="button" className="call-btn is-danger" onClick={() => calls.decline()} aria-label="Từ chối">
              <Icon name="phoneOff" size={26} />
              <span>Từ chối</span>
            </button>
            {call.video && (
              <button
                type="button"
                className="call-btn is-ok"
                onClick={() => void calls.accept(true)}
                aria-label="Nghe bằng video"
              >
                <Icon name="video" size={26} />
                <span>Video</span>
              </button>
            )}
            <button
              type="button"
              className="call-btn is-ok"
              onClick={() => void calls.accept(false)}
              aria-label="Nghe máy"
            >
              <Icon name="phone" size={26} />
              <span>{call.video ? "Chỉ tiếng" : "Nghe"}</span>
            </button>
          </>
        ) : call.phase === "ended" ? (
          <button type="button" className="call-btn" onClick={() => calls.dismiss()} aria-label="Đóng">
            <Icon name="x" size={24} />
            <span>Đóng</span>
          </button>
        ) : (
          <>
            <button
              type="button"
              className={`call-btn${call.muted ? " is-off" : ""}`}
              onClick={() => calls.toggleMute()}
              aria-pressed={call.muted}
              aria-label={call.muted ? "Bật micro" : "Tắt micro"}
            >
              <Icon name={call.muted ? "micOff" : "mic"} size={24} />
              <span>{call.muted ? "Đang tắt tiếng" : "Micro"}</span>
            </button>
            {!!call.local?.getVideoTracks().length && (
              <button
                type="button"
                className={`call-btn${call.camOff ? " is-off" : ""}`}
                onClick={() => calls.toggleCamera()}
                aria-pressed={call.camOff}
                aria-label={call.camOff ? "Bật camera" : "Tắt camera"}
              >
                <Icon name={call.camOff ? "videoOff" : "video"} size={24} />
                <span>Camera</span>
              </button>
            )}
            {call.phase === "active" && (remoteVideo || mineShown) && (
              <button
                type="button"
                className={`call-btn${full ? " is-on" : ""}`}
                onClick={goFull}
                aria-pressed={full}
                aria-label={full ? "Thoát toàn màn hình" : "Toàn màn hình"}
              >
                <Icon name={full ? "shrink" : "expand"} size={24} />
                <span>{full ? "Thu lại" : "Toàn màn hình"}</span>
              </button>
            )}
            {call.phase === "active" && canShareScreen() && (
              <button
                type="button"
                className={`call-btn${call.screen ? " is-on" : ""}`}
                onClick={() => void calls.toggleScreen()}
                aria-pressed={!!call.screen}
                aria-label={call.screen ? "Dừng chia sẻ màn hình" : "Chia sẻ màn hình"}
              >
                <Icon name={call.screen ? "screenOff" : "screen"} size={24} />
                <span>{call.screen ? "Dừng chia sẻ" : "Màn hình"}</span>
              </button>
            )}
            <button
              type="button"
              className="call-btn is-danger"
              onClick={() => calls.hangup()}
              aria-label="Kết thúc cuộc gọi"
            >
              <Icon name="phoneOff" size={26} />
              <span>{call.phase === "outgoing" ? "Huỷ" : "Kết thúc"}</span>
            </button>
          </>
        )}
      </div>
    </div>
  );
}

/** Nút gọi trên đầu cuộc trò chuyện riêng. */
export function CallButtons({ channelId, peerId }: { channelId: number; peerId: number }) {
  const { calls, state, groupCalls } = useChat();
  const call = useCallState(calls);
  const inGroup = !!useGroupCall(groupCalls).active;
  const busy = inGroup || (call.phase !== "idle" && call.phase !== "ended");
  const peer = state.users[peerId];
  if (!calls || !peer || peerId === state.me.id) return null;
  return (
    <>
      <button
        className="icon-btn"
        disabled={busy}
        onClick={() => void calls.start(channelId, peer, false)}
        title="Gọi thoại"
        aria-label={`Gọi thoại cho ${displayName(peer)}`}
      >
        <Icon name="phone" />
      </button>
      <button
        className="icon-btn"
        disabled={busy}
        onClick={() => void calls.start(channelId, peer, true)}
        title="Gọi video"
        aria-label={`Gọi video cho ${displayName(peer)}`}
      >
        <Icon name="video" />
      </button>
    </>
  );
}

/** Dòng nhật ký cuộc gọi trong tin nhắn riêng. */
export function CallLog({
  outcome,
  video,
  duration,
  channelId,
  callerId,
  participants,
}: {
  participants?: number;
  outcome: string;
  video?: boolean;
  duration?: number;
  channelId: number;
  callerId: number;
}) {
  const { calls, state } = useChat();
  const ch = state.channels[channelId];
  const peerId = ch?.dm_members?.find((u) => u.id !== state.me.id)?.id;
  const mine = callerId === state.me.id;
  const kind = video ? "video" : "thoại";
  const fmt = (s: number) => (s >= 60 ? `${Math.floor(s / 60)} phút ${s % 60} giây` : `${s} giây`);
  const text =
    outcome === "group"
      ? `Cuộc gọi ${kind} nhóm · ${fmt(duration ?? 0)}${participants ? ` · ${participants} người` : ""}`
      : outcome === "completed"
        ? `Cuộc gọi ${kind} · ${fmt(duration ?? 0)}`
        : outcome === "declined"
          ? mine
            ? `Cuộc gọi ${kind} bị từ chối`
            : `Bạn đã từ chối cuộc gọi ${kind}`
          : mine
            ? `Cuộc gọi ${kind} không được trả lời`
            : `Cuộc gọi ${kind} nhỡ`;
  const missed = outcome !== "completed" && outcome !== "group";
  return (
    <div className={`call-log${missed && !mine ? " is-missed" : ""}`}>
      <span className="call-log-icon">
        <Icon name={video ? "video" : "phone"} size={16} />
      </span>
      <span>{text}</span>
      {calls && peerId && state.users[peerId] && (
        <button
          type="button"
          className="link-btn"
          onClick={() => void calls.start(channelId, state.users[peerId], !!video)}
        >
          Gọi lại
        </button>
      )}
    </div>
  );
}
