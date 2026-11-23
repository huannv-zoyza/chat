/**
 * Gọi thoại / video 1-1 bằng WebRTC. Máy chủ chỉ chuyển tín hiệu (xem controller/calls.go);
 * âm thanh, hình ảnh đi thẳng giữa hai máy (DTLS-SRTP), hoặc qua TURN nếu mạng chặn.
 *
 * Người gọi: invite -> (đổ chuông) -> accepted -> tạo offer -> nhận answer.
 * Người nghe: incoming -> accept -> nhận offer -> gửi answer.
 */
import { useSyncExternalStore } from "react";
import { insecureMediaMessage, type Api, type PublicUser } from "./api";
import { captureScreen, Renegotiator, setVideoSource } from "./screenshare";
import type { CallRequest, ServerEvent, SignalData } from "./socket";

export type CallPhase = "idle" | "outgoing" | "incoming" | "connecting" | "active" | "ended";

export type CallState = {
  phase: CallPhase;
  callId?: string;
  channelId?: number;
  peer?: PublicUser;
  /** Cuộc gọi video (người gọi chọn). */
  video: boolean;
  local?: MediaStream;
  remote?: MediaStream;
  muted: boolean;
  camOff: boolean;
  /** Màn hình mình đang chia sẻ. */
  screen?: MediaStream;
  /** Người kia đang chia sẻ màn hình. */
  remoteScreen?: boolean;
  /** Báo ngắn trong lúc gọi (vd. không chia sẻ được màn hình). */
  notice?: string;
  startedAt?: number;
  /** Câu báo khi kết thúc / lỗi. */
  message?: string;
};

const IDLE: CallState = { phase: "idle", video: false, muted: false, camOff: false };

const REASONS: Record<string, string> = {
  hangup: "Cuộc gọi đã kết thúc",
  declined: "Người kia đã từ chối",
  no_answer: "Không ai nghe máy",
  busy: "Người kia đang bận cuộc gọi khác",
  offline: "Người kia đang offline",
  disconnected: "Mất kết nối",
  answered_elsewhere: "Đã nghe máy trên thiết bị khác",
  gone: "Cuộc gọi không còn",
  invalid: "Không gọi được",
};

function newCallId() {
  const b = new Uint8Array(12);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
}

/** Chuông: hai nốt ngắn lặp lại (không cần file âm thanh). */
export class Ringer {
  private ctx: AudioContext | null = null;
  private timer: number | undefined;
  start(kind: "incoming" | "outgoing") {
    this.stop();
    try {
      this.ctx = new AudioContext();
    } catch {
      return;
    }
    const play = () => {
      const ctx = this.ctx;
      if (!ctx) return;
      const notes = kind === "incoming" ? [880, 660] : [440];
      notes.forEach((f, i) => {
        const o = ctx.createOscillator();
        const g = ctx.createGain();
        o.frequency.value = f;
        g.gain.value = kind === "incoming" ? 0.12 : 0.05;
        o.connect(g).connect(ctx.destination);
        const t = ctx.currentTime + i * 0.22;
        o.start(t);
        o.stop(t + (kind === "incoming" ? 0.18 : 0.9));
      });
    };
    play();
    this.timer = window.setInterval(play, kind === "incoming" ? 1800 : 3000);
  }
  stop() {
    window.clearInterval(this.timer);
    void this.ctx?.close().catch(() => {});
    this.ctx = null;
  }
}

export class CallController {
  private state: CallState = IDLE;
  private listeners = new Set<() => void>();
  private pc: RTCPeerConnection | null = null;
  private pendingIce: RTCIceCandidateInit[] = [];
  private iceServers: RTCIceServer[] | null = null;
  private ringer = new Ringer();
  private clearTimer: number | undefined;
  private noticeTimer: number | undefined;
  private neg: Renegotiator | null = null;
  /** Mình là người gọi (bên còn lại "nhường" khi hai bên cùng đàm phán lại). */
  private isCaller = false;

  constructor(
    private api: Api,
    private send: (req: CallRequest) => boolean,
    private notify: (title: string, body: string) => void,
  ) {}

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };
  getSnapshot = () => this.state;

  private set(patch: Partial<CallState>) {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((fn) => fn());
  }

  private async media(video: boolean): Promise<MediaStream> {
    if (!navigator.mediaDevices?.getUserMedia) throw new Error(await insecureMediaMessage(this.api));
    try {
      return await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true },
        video,
      });
    } catch (err) {
      if (video) {
        // không có / không cho dùng camera: gọi bằng âm thanh
        return await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
      }
      throw new Error(
        err instanceof DOMException && err.name === "NotAllowedError"
          ? "Bạn chưa cho phép dùng micro."
          : "Không mở được micro.",
      );
    }
  }

  private async peer(): Promise<RTCPeerConnection> {
    // lấy mới mỗi cuộc gọi: tài khoản TURN tạm có hạn
    this.iceServers = await this.api
      .rtcConfig()
      .then((r) => r.ice_servers)
      .catch(() => this.iceServers ?? []);
    const pc = new RTCPeerConnection({ iceServers: this.iceServers });
    const remote = new MediaStream();
    this.set({ remote });
    pc.ontrack = (e) => {
      e.streams[0]?.getTracks().forEach((t) => remote.addTrack(t));
      if (!e.streams[0]) remote.addTrack(e.track);
      this.set({ remote });
    };
    pc.onicecandidate = (e) => {
      if (e.candidate && this.state.callId) {
        this.send({ type: "call.signal", call_id: this.state.callId, data: { candidate: e.candidate.toJSON() } });
      }
    };
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === "connected") this.neg?.markReady();
      if (pc.connectionState === "connected" && this.state.phase !== "active") {
        this.set({ phase: "active", startedAt: Date.now() });
      } else if (pc.connectionState === "failed") {
        this.hangup("Không kết nối được tới máy bên kia (mạng chặn kết nối trực tiếp, cần máy chủ TURN).");
      }
    };
    this.state.local?.getTracks().forEach((t) => pc.addTrack(t, this.state.local!));
    this.neg = new Renegotiator(pc, !this.isCaller, (sdp) => this.signal({ sdp }));
    this.pc = pc;
    return pc;
  }

  /** Gửi tín hiệu cho người kia; SDP luôn kèm cờ đang chia sẻ màn hình. */
  private signal(data: SignalData) {
    if (!this.state.callId) return;
    const withScreen = data.sdp ? { ...data, screen: !!this.state.screen } : data;
    this.send({ type: "call.signal", call_id: this.state.callId, data: withScreen });
  }

  /* ---------- Hành động người dùng ---------- */

  async start(channelId: number, peer: PublicUser, video: boolean) {
    if (this.state.phase !== "idle" && this.state.phase !== "ended") return;
    window.clearTimeout(this.clearTimer);
    const callId = newCallId();
    this.isCaller = true;
    this.set({ ...IDLE, phase: "outgoing", callId, channelId, peer, video });
    try {
      const local = await this.media(video);
      if (this.state.callId !== callId) return local.getTracks().forEach((t) => t.stop()); // đã huỷ
      this.set({ local, video: video && local.getVideoTracks().length > 0 });
      if (!this.send({ type: "call.invite", call_id: callId, channel_id: channelId, video })) {
        throw new Error("Đang mất kết nối tới máy chủ.");
      }
      this.ringer.start("outgoing");
    } catch (err) {
      this.finish(err instanceof Error ? err.message : "Không gọi được.");
    }
  }

  async accept(withVideo: boolean) {
    const s = this.state;
    if (s.phase !== "incoming" || !s.callId) return;
    this.ringer.stop();
    this.set({ phase: "connecting" });
    try {
      const local = await this.media(withVideo);
      this.set({ local });
      await this.peer();
      if (!this.send({ type: "call.accept", call_id: s.callId })) throw new Error("Đang mất kết nối tới máy chủ.");
    } catch (err) {
      this.send({ type: "call.decline", call_id: s.callId });
      this.finish(err instanceof Error ? err.message : "Không nghe máy được.");
    }
  }

  decline() {
    if (this.state.phase !== "incoming" || !this.state.callId) return;
    this.send({ type: "call.decline", call_id: this.state.callId });
    this.finish();
  }

  hangup(message?: string) {
    if (this.state.callId && this.state.phase !== "ended" && this.state.phase !== "idle") {
      this.send({ type: "call.hangup", call_id: this.state.callId });
    }
    this.finish(message ?? REASONS.hangup);
  }

  toggleMute() {
    const muted = !this.state.muted;
    this.state.local?.getAudioTracks().forEach((t) => (t.enabled = !muted));
    this.set({ muted });
  }

  toggleCamera() {
    const camOff = !this.state.camOff;
    this.state.local?.getVideoTracks().forEach((t) => (t.enabled = !camOff));
    this.set({ camOff });
  }

  /** Bật / tắt chia sẻ màn hình (thay cho hình camera nếu đang gọi video). */
  async toggleScreen() {
    if (this.state.screen) return this.stopScreen();
    const pc = this.pc;
    if (this.state.phase !== "active" || !pc) return;
    let screen: MediaStream | null;
    try {
      screen = await captureScreen();
    } catch (err) {
      this.flash(err instanceof Error ? err.message : "Không chia sẻ được màn hình.");
      return;
    }
    if (!screen) return;
    if (this.pc !== pc || this.state.phase !== "active" || this.state.screen) {
      return screen.getTracks().forEach((t) => t.stop());
    }
    const track = screen.getVideoTracks()[0];
    // bấm "Dừng chia sẻ" của trình duyệt
    track.onended = () => void this.stopScreen();
    try {
      if (await setVideoSource(pc, track, screen)) this.neg?.request();
    } catch {
      screen.getTracks().forEach((t) => t.stop());
      this.flash("Không chia sẻ được màn hình.");
      return;
    }
    this.set({ screen });
    this.signal({ screen: true });
  }

  async stopScreen() {
    const screen = this.state.screen;
    if (!screen) return;
    screen.getTracks().forEach((t) => {
      t.onended = null;
      t.stop();
    });
    this.set({ screen: undefined });
    const pc = this.pc;
    if (pc) {
      // quay lại camera (nếu có), không thì thôi gửi hình
      const cam = this.state.local?.getVideoTracks()[0] ?? null;
      if (await setVideoSource(pc, cam, this.state.local).catch(() => false)) this.neg?.request();
    }
    this.signal({ screen: false });
  }

  private flash(notice: string) {
    this.set({ notice });
    window.clearTimeout(this.noticeTimer);
    this.noticeTimer = window.setTimeout(() => this.set({ notice: undefined }), 4000);
  }

  dismiss() {
    if (this.state.phase === "ended") this.set(IDLE);
  }

  /* ---------- Sự kiện từ máy chủ ---------- */

  async onEvent(ev: ServerEvent) {
    const s = this.state;
    switch (ev.type) {
      case "call.incoming": {
        if (s.phase !== "idle" && s.phase !== "ended") {
          // đang bận cuộc khác trên thiết bị này
          this.send({ type: "call.decline", call_id: ev.call_id });
          return;
        }
        window.clearTimeout(this.clearTimer);
        this.isCaller = false;
        this.set({
          ...IDLE,
          phase: "incoming",
          callId: ev.call_id,
          channelId: ev.channel_id,
          peer: ev.from,
          video: ev.video,
        });
        this.ringer.start("incoming");
        this.notify(
          ev.video ? "📹 Cuộc gọi video đến" : "📞 Cuộc gọi đến",
          `${ev.from.display_name || ev.from.username} đang gọi cho bạn`,
        );
        return;
      }
      case "call.accepted": {
        if (ev.call_id !== s.callId || s.phase !== "outgoing") return;
        this.ringer.stop();
        this.set({ phase: "connecting" });
        try {
          const pc = await this.peer();
          // máy mình không có camera: vẫn nhận hình của người kia trong cuộc gọi video
          if (s.video && !this.state.local?.getVideoTracks().length) pc.addTransceiver("video", { direction: "recvonly" });
          const offer = await pc.createOffer();
          await pc.setLocalDescription(offer);
          this.signal({ sdp: pc.localDescription!.toJSON() });
        } catch {
          this.hangup("Không thiết lập được cuộc gọi.");
        }
        return;
      }
      case "call.signal": {
        const pc = this.pc;
        const neg = this.neg;
        if (ev.call_id !== s.callId || !pc || !neg) return;
        if (typeof ev.data.screen === "boolean" && ev.data.screen !== !!s.remoteScreen) {
          this.set({ remoteScreen: ev.data.screen });
        }
        const data = ev.data;
        await neg.serial(async () => {
          if (this.pc !== pc) return; // cuộc gọi đã kết thúc
          try {
            const sdp = data.sdp;
            if (sdp) {
              if (neg.shouldIgnore(sdp)) return;
              await pc.setRemoteDescription(sdp);
              if (sdp.type === "offer") {
                const answer = await pc.createAnswer();
                await pc.setLocalDescription(answer);
                this.signal({ sdp: pc.localDescription!.toJSON() });
              }
              for (const c of this.pendingIce.splice(0)) await pc.addIceCandidate(c).catch(() => {});
              neg.afterRemote(sdp);
            } else if (data.candidate) {
              if (pc.remoteDescription) await pc.addIceCandidate(data.candidate).catch(() => {});
              else this.pendingIce.push(data.candidate);
            }
          } catch {
            if (this.pc === pc) this.hangup("Không thiết lập được cuộc gọi.");
          }
        });
        return;
      }
      case "call.ended": {
        if (ev.call_id !== s.callId || s.phase === "idle" || s.phase === "ended") return;
        // tự mình cúp / từ chối thì đã hiện thông báo rồi
        this.finish(REASONS[ev.reason] ?? "Cuộc gọi đã kết thúc", ev.reason === "answered_elsewhere");
        return;
      }
    }
  }

  /** Mất kết nối tới máy chủ giữa chừng. */
  onDisconnected() {
    if (this.state.phase === "outgoing" || this.state.phase === "incoming") this.finish(REASONS.disconnected);
  }

  private finish(message?: string, quiet = false) {
    this.ringer.stop();
    this.pc?.close();
    this.pc = null;
    this.neg = null;
    this.pendingIce = [];
    this.state.local?.getTracks().forEach((t) => t.stop());
    this.state.screen?.getTracks().forEach((t) => {
      t.onended = null;
      t.stop();
    });
    window.clearTimeout(this.noticeTimer);
    if (quiet) {
      this.set(IDLE);
      return;
    }
    this.set({
      phase: "ended",
      message,
      local: undefined,
      remote: undefined,
      screen: undefined,
      remoteScreen: false,
      notice: undefined,
    });
    window.clearTimeout(this.clearTimer);
    this.clearTimer = window.setTimeout(() => this.dismiss(), 2500);
  }
}

const IDLE_CTRL = { subscribe: () => () => {}, getSnapshot: () => IDLE };

export function useCallState(ctrl: CallController | null): CallState {
  const c = ctrl ?? IDLE_CTRL;
  return useSyncExternalStore(c.subscribe, c.getSnapshot);
}
