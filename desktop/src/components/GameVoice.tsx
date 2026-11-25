// Thoại trong ván Ma Sói: nút vào/rời thoại, bật/tắt micro, câu giải thích lúc này ai nghe được mình,
// và thanh nhỏ nổi trên màn hình chat khi đang trong thoại mà đã rời màn hình làng.
import { createPortal } from "react-dom";
import type { GameView } from "../lib/api";
import { gameVoice, useGameVoice } from "../lib/gamevoice";
import type { WerewolfMe, WerewolfState } from "../lib/games";
import { useVillageId, village } from "../lib/village";
import { findMessage } from "../state/chat";
import { useChat } from "../state/ChatProvider";
import { Icon } from "./ui";

/** Lúc này ai nghe được mình nói (theo luật thoại máy chủ gửi về). */
export function voiceNote(game: GameView): string {
  const v = game.voice;
  if (!v) return "";
  if (game.status !== "playing") return "Mọi người trong ván nghe nhau";
  const s = game.state as WerewolfState | undefined;
  const me = game.me as WerewolfMe | undefined;
  const night = s?.phase === "night";
  if (me && !me.alive) {
    if (s?.phase === "hunter" && v.talk.length > 1) return "🏹 Trăn trối: cả làng nghe bạn";
    return "💀 Bạn đã chết: chỉ người chết nghe bạn";
  }
  if (night) return v.talk.length ? "🐺 Ban đêm: chỉ bầy Sói nghe nhau" : "🌙 Ban đêm: bạn đang ngủ, micro tắt";
  return "☀️ Ban ngày: cả làng nghe nhau";
}

/** Nút thoại trên thanh trên cùng của màn hình làng. */
export function VoiceButtons({ messageId, game, meId }: { messageId: number; game: GameView; meId: number }) {
  const vs = useGameVoice();
  if (!game.voice) return null;
  const a = vs.active?.messageId === messageId ? vs.active : undefined;
  const inRoom = vs.rooms[messageId]?.length ?? 0;
  if (!a) {
    return (
      <button
        type="button"
        className="village-btn village-voice-join"
        onClick={() => void gameVoice.join(messageId, game, meId)}
        title="Nói chuyện bằng micro với người chơi trong ván (ban đêm chỉ Sói nghe nhau)"
      >
        <Icon name="mic" size={16} />
        <span className="village-btn-label">Vào thoại{inRoom ? ` · ${inRoom}` : ""}</span>
      </button>
    );
  }
  const silent = a.rules.talk.length === 0;
  return (
    <>
      <button
        type="button"
        className={`village-btn village-mic${a.muted ? " is-off" : ""}${silent ? " is-silent" : ""}${
          a.speaking.includes(meId) ? " is-speaking" : ""
        }`}
        onClick={() => gameVoice.toggleMute()}
        aria-pressed={!a.muted}
        aria-label={a.muted ? "Bật micro" : "Tắt micro"}
        title={voiceNote(game)}
      >
        <Icon name={a.muted || silent ? "micOff" : "mic"} size={16} />
        <span className="village-btn-label">{a.muted ? "Mic tắt" : "Mic"}</span>
      </button>
      <button type="button" className="village-btn" onClick={() => gameVoice.leave()} aria-label="Rời thoại">
        <Icon name="phoneOff" size={16} />
        <span className="village-btn-label">Rời thoại</span>
      </button>
    </>
  );
}

/** Dòng trạng thái thoại trong bảng điều khiển dưới cùng. */
export function VoiceStatus({ messageId, game }: { messageId: number; game: GameView }) {
  const vs = useGameVoice();
  const a = vs.active?.messageId === messageId ? vs.active : undefined;
  if (vs.error && !a) {
    return (
      <p className="village-voice-note is-error">
        {vs.error}{" "}
        <button type="button" className="link-btn" onClick={() => gameVoice.dismissError()}>
          Ẩn
        </button>
      </p>
    );
  }
  if (!a) return null;
  const others = Object.keys(a.peers).length;
  return (
    <p className="village-voice-note">
      <Icon name={a.muted ? "micOff" : "mic"} size={14} /> {voiceNote(game)}
      <small> · {others ? `${others + 1} người trong thoại` : "chưa ai khác vào thoại"}</small>
    </p>
  );
}

/** Đang trong thoại của ván nhưng đã rời màn hình làng: thanh nổi để bật/tắt micro, quay lại, rời thoại. */
export function GameVoiceBar() {
  const vs = useGameVoice();
  const villageId = useVillageId();
  const { state } = useChat();
  const a = vs.active;
  if (!a || villageId === a.messageId) return null;
  const game = findMessage(state, a.messageId)?.game;
  const silent = a.rules.talk.length === 0;
  return createPortal(
    <div className="gv-bar" role="region" aria-label="Thoại Ma Sói">
      <span className={`gv-dot${a.speaking.length ? " is-live" : ""}`} aria-hidden />
      <span className="gv-text">
        <b>Thoại Ma Sói</b>
        <small>{game ? voiceNote(game) : `${Object.keys(a.peers).length + 1} người`}</small>
      </span>
      <button
        type="button"
        className={`gv-btn${a.muted || silent ? " is-off" : ""}`}
        onClick={() => gameVoice.toggleMute()}
        aria-label={a.muted ? "Bật micro" : "Tắt micro"}
      >
        <Icon name={a.muted || silent ? "micOff" : "mic"} size={16} />
      </button>
      {game?.kind === "werewolf" && (
        <button type="button" className="gv-btn" onClick={() => village.open(a.messageId)} aria-label="Vào làng">
          🔥
        </button>
      )}
      <button type="button" className="gv-btn is-leave" onClick={() => gameVoice.leave()} aria-label="Rời thoại">
        <Icon name="phoneOff" size={16} />
      </button>
    </div>,
    document.body,
  );
}
