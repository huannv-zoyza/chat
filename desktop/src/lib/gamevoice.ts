/**
 * Thoại trong ván (Ma Sói): kênh thoại riêng của từng ván, chỉ người chơi, chỉ có tiếng.
 * Kiểu mesh như gọi nhóm (controller/gamevoice.go chuyển tín hiệu): người mới vào gửi offer cho
 * mọi người đang có mặt.
 *
 * Ai nghe được ai do máy chủ tính theo luật của trò (game.voice: talk/hear, đổi theo pha):
 * - máy mình chỉ gửi tiếng micro tới người trong `talk` (người khác nhận im lặng, kể cả khi họ
 *   sửa app của họ cũng không nghe được mình);
 * - tiếng của người ngoài `hear` bị tắt ở máy mình (phòng khi máy họ đổi pha chậm hơn).
 *
 * Đang ở trong cuộc gọi nhóm của kênh thì micro bên đó tạm tắt và tiếng của những người chơi
 * bên đó cũng tắt, để mọi lời nói trong ván chỉ đi qua kênh thoại của ván.
 */
import { useSyncExternalStore } from "react";
import { insecureMediaMessage, type Api, type GameView, type PublicUser } from "./api";
import type { GroupCallController } from "./groupcall";
import type { SignalData } from "./socket";

export type VoiceRules = { talk: number[]; hear: number[] };
export type VoiceParticipant = { peer_id: string; user: PublicUser };

export type VoicePeer = {
  peerId: string;
  user: PublicUser;
  state: RTCPeerConnectionState | "new";
};

export type GameVoiceState = {
  /** Người đang trong thoại của từng ván (theo id tin chứa ván). */
  rooms: Record<number, VoiceParticipant[]>;
  active?: {
    messageId: number;
    selfId?: string;
    peers: Record<string, VoicePeer>;
    muted: boolean;
    rules: VoiceRules;
    /** Người (id) đang nói, kể cả mình. */
    speaking: number[];
  };
  error?: string;
};

export type GameVoiceEvent =
  | { type: "gamevoice.state"; message_id: number; participants?: VoiceParticipant[] }
  | { type: "gamevoice.welcome"; message_id: number; self: string; participants?: VoiceParticipant[] }
  | { type: "gamevoice.signal"; message_id: number; from: string; from_user: PublicUser; data: SignalData }
  | { type: "gamevoice.error"; message_id: number; message: string };

type Send = (req: Record<string, unknown>) => boolean;

type Conn = {
  pc: RTCPeerConnection;
  userId: number;
  sender?: RTCRtpSender;
  audio: HTMLAudioElement;
  stream: MediaStream;
  analyser?: AnalyserNode;
  source?: MediaStreamAudioSourceNode;
  pendingIce: RTCIceCandidateInit[];
  queue: Promise<void>;
  /** Số lần đã thử nối lại liên tiếp (về 0 khi nối được). */
  restarts: number;
  restartTimer?: number;
};

/** Thử nối lại tối đa bấy nhiêu lần liên tiếp khi đường nối giữa hai người bị đứt. */
const MAX_RESTARTS = 5;

const SPEAK_LEVEL = 0.035;

class GameVoiceController {
  private state: GameVoiceState = { rooms: {} };
  private listeners = new Set<() => void>();
  private conns = new Map<string, Conn>();
  private api: Api | null = null;
  private send: Send = () => false;
  private groups: GroupCallController | null = null;
  private iceServers: RTCIceServer[] = [];
  private mic: MediaStream | null = null;
  private meId = 0;
  private players: number[] = [];
  private audioCtx: AudioContext | null = null;
  private micAnalyser: AnalyserNode | null = null;
  private meter: number | undefined;

  configure(api: Api, send: Send, groups: GroupCallController) {
    this.api = api;
    this.send = send;
    this.groups = groups;
  }

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => {
      this.listeners.delete(fn);
    };
  };
  getSnapshot = () => this.state;
  private set(patch: Partial<GameVoiceState>) {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((fn) => fn());
  }
  private setActive(patch: Partial<NonNullable<GameVoiceState["active"]>>) {
    if (!this.state.active) return;
    this.set({ active: { ...this.state.active, ...patch } });
  }

  /* ---------- Hành động ---------- */

  /** Vào thoại của ván (mỗi lúc chỉ ở một ván). */
  async join(messageId: number, game: GameView, meId: number) {
    if (!this.api) return;
    if (this.state.active?.messageId === messageId) return;
    if (this.state.active) this.leave();
    if (!game.voice) return;
    if (!navigator.mediaDevices?.getUserMedia) {
      this.set({ error: await insecureMediaMessage(this.api) });
      return;
    }
    let mic: MediaStream;
    try {
      mic = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
        video: false,
      });
    } catch {
      this.set({ error: "Không mở được micro. Hãy cho phép trang dùng micro." });
      return;
    }
    if (this.state.active) {
      mic.getTracks().forEach((t) => t.stop());
      return;
    }
    this.iceServers = await this.api
      .rtcConfig()
      .then((r) => r.ice_servers)
      .catch(() => this.iceServers);
    this.mic = mic;
    this.meId = meId;
    this.players = game.players.map((p) => p.id);
    this.set({
      error: undefined,
      active: { messageId, peers: {}, muted: false, rules: game.voice, speaking: [] },
    });
    this.startMeter();
    this.holdGroupCall();
    if (!this.send({ type: "gamevoice.join", message_id: messageId })) {
      this.cleanup("Đang mất kết nối tới máy chủ.");
    }
  }

  leave() {
    const a = this.state.active;
    if (!a) return;
    this.send({ type: "gamevoice.leave", message_id: a.messageId });
    this.cleanup();
  }

  toggleMute() {
    const a = this.state.active;
    if (!a || !this.mic) return;
    this.mic.getAudioTracks().forEach((t) => (t.enabled = a.muted));
    this.setActive({ muted: !a.muted });
  }

  dismissError() {
    this.set({ error: undefined });
  }

  /** Ván vừa đổi (game.updated): cập nhật ai nghe ai, ai còn là người chơi. */
  onGame(messageId: number, game: GameView) {
    const a = this.state.active;
    if (!a || a.messageId !== messageId || !game.voice) return;
    this.players = game.players.map((p) => p.id);
    this.setActive({ rules: game.voice });
    this.applyRules();
    this.holdGroupCall();
  }

  /* ---------- Luật nghe/nói ---------- */

  private applyRules() {
    const a = this.state.active;
    if (!a) return;
    const talk = new Set(a.rules.talk);
    const hear = new Set(a.rules.hear);
    const track = this.mic?.getAudioTracks()[0] ?? null;
    for (const c of this.conns.values()) {
      const out = talk.has(c.userId) ? track : null;
      if (c.sender && c.sender.track !== out) void c.sender.replaceTrack(out).catch(() => {});
      c.audio.muted = !hear.has(c.userId);
    }
  }

  /** Tạm tắt micro và tiếng người chơi trong cuộc gọi nhóm của kênh (nếu đang ở đó). */
  private holdGroupCall() {
    this.groups?.setGameHold(this.state.active ? this.players.filter((id) => id !== this.meId) : null);
  }

  /* ---------- Kết nối với từng người ---------- */

  private conn(peerId: string, user: PublicUser): Conn {
    const have = this.conns.get(peerId);
    if (have) return have;
    const a = this.state.active!;
    const messageId = a.messageId;
    const pc = new RTCPeerConnection({ iceServers: this.iceServers });
    const stream = new MediaStream();
    const audio = new Audio();
    audio.autoplay = true;
    const c: Conn = { pc, userId: user.id, audio, stream, pendingIce: [], queue: Promise.resolve(), restarts: 0 };
    pc.ontrack = (e) => {
      if (!stream.getTracks().includes(e.track)) stream.addTrack(e.track);
      audio.srcObject = stream;
      void audio.play().catch(() => {});
      this.watchLevel(c);
    };
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        this.send({ type: "gamevoice.signal", message_id: messageId, to: peerId, data: { candidate: e.candidate.toJSON() } });
      }
    };
    pc.onconnectionstatechange = () => {
      const st = pc.connectionState;
      this.updatePeer(peerId, { state: st });
      window.clearTimeout(c.restartTimer);
      if (st === "connected") c.restarts = 0;
      // Đứt (đổi Wi-Fi, mạng chập chờn...): nối lại bằng ICE restart thay vì bỏ hẳn người này.
      // "disconnected" hay tự hồi lại nên chờ một chút; "failed" thì nối lại ngay.
      if (st === "failed") this.restartIce(peerId, c);
      else if (st === "disconnected") c.restartTimer = window.setTimeout(() => this.restartIce(peerId, c), 3000);
    };
    this.conns.set(peerId, c);
    this.setActive({ peers: { ...a.peers, [peerId]: { peerId, user, state: "new" } } });
    return c;
  }

  /** Kênh tiếng hai chiều; gửi micro hay im lặng tuỳ luật. */
  private attachSender(c: Conn, transceiver: RTCRtpTransceiver) {
    transceiver.direction = "sendrecv";
    c.sender = transceiver.sender;
    this.applyRules();
  }

  /**
   * Nối lại đường đã đứt tới peerId. Chỉ một bên chủ động (bên có mã phiên nhỏ hơn) để hai bên
   * không cùng gửi offer một lúc; bên kia trả lời như mọi offer khác.
   */
  private restartIce(peerId: string, c: Conn) {
    const self = this.state.active?.selfId;
    if (!self || self > peerId || this.conns.get(peerId) !== c) return;
    if (c.restarts >= MAX_RESTARTS) return;
    c.restarts++;
    c.queue = c.queue.then(async () => {
      if (this.conns.get(peerId) !== c || c.pc.signalingState !== "stable") return;
      const st = c.pc.connectionState;
      if (st !== "failed" && st !== "disconnected") return;
      try {
        await c.pc.setLocalDescription(await c.pc.createOffer({ iceRestart: true }));
        this.signal(peerId, { sdp: c.pc.localDescription!.toJSON() });
      } catch {
        /* thử lại ở lần đổi trạng thái sau */
      }
    });
    // offer/answer xong mà vẫn chưa nối được thì thử lại
    c.restartTimer = window.setTimeout(() => {
      const st = c.pc.connectionState;
      if (st === "failed" || st === "disconnected") this.restartIce(peerId, c);
    }, 8000);
  }

  private updatePeer(peerId: string, patch: Partial<VoicePeer>) {
    const a = this.state.active;
    const p = a?.peers[peerId];
    if (!a || !p) return;
    this.setActive({ peers: { ...a.peers, [peerId]: { ...p, ...patch } } });
  }

  private dropPeer(peerId: string) {
    const c = this.conns.get(peerId);
    if (c) {
      window.clearTimeout(c.restartTimer);
      c.pc.close();
      c.audio.srcObject = null;
      c.source?.disconnect();
      this.conns.delete(peerId);
    }
    const a = this.state.active;
    if (a?.peers[peerId]) {
      const { [peerId]: _gone, ...rest } = a.peers;
      this.setActive({ peers: rest });
    }
  }

  private cleanup(error?: string) {
    for (const id of [...this.conns.keys()]) this.dropPeer(id);
    this.mic?.getTracks().forEach((t) => t.stop());
    this.mic = null;
    window.clearInterval(this.meter);
    this.meter = undefined;
    this.micAnalyser = null;
    void this.audioCtx?.close().catch(() => {});
    this.audioCtx = null;
    this.set({ active: undefined, ...(error ? { error } : {}) });
    this.holdGroupCall();
  }

  /* ---------- Ai đang nói ---------- */

  private ctx(): AudioContext | null {
    if (!this.audioCtx) {
      try {
        this.audioCtx = new AudioContext();
      } catch {
        return null;
      }
    }
    return this.audioCtx;
  }

  private analyser(stream: MediaStream): { node: AnalyserNode; source: MediaStreamAudioSourceNode } | null {
    const ctx = this.ctx();
    if (!ctx || !stream.getAudioTracks().length) return null;
    const source = ctx.createMediaStreamSource(stream);
    const node = ctx.createAnalyser();
    node.fftSize = 512;
    source.connect(node);
    return { node, source };
  }

  private watchLevel(c: Conn) {
    if (c.analyser) return;
    const an = this.analyser(c.stream);
    if (an) {
      c.analyser = an.node;
      c.source = an.source;
    }
  }

  private startMeter() {
    if (this.mic) this.micAnalyser = this.analyser(this.mic)?.node ?? null;
    const buf = new Float32Array(512);
    const level = (n: AnalyserNode) => {
      n.getFloatTimeDomainData(buf);
      let sum = 0;
      for (const v of buf) sum += v * v;
      return Math.sqrt(sum / buf.length);
    };
    this.meter = window.setInterval(() => {
      const a = this.state.active;
      if (!a) return;
      void this.audioCtx?.resume().catch(() => {});
      const speaking: number[] = [];
      // mình: chỉ tính là đang nói khi có người nghe được
      if (!a.muted && a.rules.talk.length && this.micAnalyser && level(this.micAnalyser) > SPEAK_LEVEL) speaking.push(this.meId);
      const hear = new Set(a.rules.hear);
      for (const c of this.conns.values()) {
        if (hear.has(c.userId) && c.analyser && level(c.analyser) > SPEAK_LEVEL) speaking.push(c.userId);
      }
      if (speaking.join() !== a.speaking.join()) this.setActive({ speaking });
    }, 200);
  }

  /* ---------- Sự kiện ---------- */

  async onEvent(ev: GameVoiceEvent) {
    const a = this.state.active;
    switch (ev.type) {
      case "gamevoice.state": {
        const rooms = { ...this.state.rooms };
        if (ev.participants?.length) rooms[ev.message_id] = ev.participants;
        else delete rooms[ev.message_id];
        this.set({ rooms });
        if (a && a.messageId === ev.message_id) {
          const present = new Set((ev.participants ?? []).map((p) => p.peer_id));
          for (const id of Object.keys(a.peers)) if (!present.has(id)) this.dropPeer(id);
        }
        return;
      }
      case "gamevoice.welcome": {
        if (!a || a.messageId !== ev.message_id) return;
        this.setActive({ selfId: ev.self });
        for (const p of ev.participants ?? []) {
          const c = this.conn(p.peer_id, p.user);
          try {
            this.attachSender(c, c.pc.addTransceiver("audio", { direction: "sendrecv" }));
            await c.pc.setLocalDescription(await c.pc.createOffer());
            this.signal(p.peer_id, { sdp: c.pc.localDescription!.toJSON() });
          } catch {
            this.dropPeer(p.peer_id);
          }
        }
        return;
      }
      case "gamevoice.signal": {
        if (!a || a.messageId !== ev.message_id) return;
        const c = this.conn(ev.from, ev.from_user);
        const data = ev.data;
        c.queue = c.queue.then(async () => {
          if (this.conns.get(ev.from) !== c) return;
          try {
            if (data.sdp) {
              await c.pc.setRemoteDescription(data.sdp);
              if (data.sdp.type === "offer") {
                const t = c.pc.getTransceivers().find((x) => x.receiver.track.kind === "audio");
                if (t) this.attachSender(c, t);
                await c.pc.setLocalDescription(await c.pc.createAnswer());
                this.signal(ev.from, { sdp: c.pc.localDescription!.toJSON() });
              }
              for (const ice of c.pendingIce.splice(0)) await c.pc.addIceCandidate(ice).catch(() => {});
            } else if (data.candidate) {
              if (c.pc.remoteDescription) await c.pc.addIceCandidate(data.candidate).catch(() => {});
              else c.pendingIce.push(data.candidate);
            }
          } catch {
            this.dropPeer(ev.from);
          }
        });
        return;
      }
      case "gamevoice.error":
        if (a && a.messageId === ev.message_id) this.cleanup(ev.message);
        else this.set({ error: ev.message });
        return;
    }
  }

  private signal(peerId: string, data: SignalData) {
    const a = this.state.active;
    if (a) this.send({ type: "gamevoice.signal", message_id: a.messageId, to: peerId, data });
  }

  /** Mất kết nối máy chủ: rời thoại cho sạch (vào lại bằng nút). */
  onDisconnected() {
    if (this.state.active) this.cleanup("Mất kết nối tới máy chủ, đã rời thoại của ván.");
  }
}

export const gameVoice = new GameVoiceController();

export function useGameVoice(): GameVoiceState {
  return useSyncExternalStore(gameVoice.subscribe, gameVoice.getSnapshot);
}
