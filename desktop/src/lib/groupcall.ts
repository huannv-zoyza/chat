/**
 * Gọi nhóm trong kênh (tối đa 8 người, kiểu mesh): mỗi máy giữ một RTCPeerConnection tới
 * từng người còn lại. Người mới vào gửi offer cho tất cả người đang có mặt; người đang có mặt
 * chỉ trả lời. Máy chủ chỉ chuyển tín hiệu (controller/groupcalls.go).
 */
import { useSyncExternalStore } from "react";
import { insecureMediaMessage, type Api, type PublicUser } from "./api";
import { Ringer } from "./calls";

export type GroupParticipant = { peer_id: string; user: PublicUser; video: boolean };
export type GroupRoom = { channel_id: number; started_at?: string; participants: GroupParticipant[] };

export type GroupPeer = {
  peerId: string;
  user: PublicUser;
  stream: MediaStream;
  state: RTCPeerConnectionState | "new";
};

export type GroupState = {
  /** Cuộc gọi nhóm đang diễn ra ở các kênh mình tham gia. */
  rooms: Record<number, GroupRoom>;
  /** Cuộc gọi mình đang tham gia. */
  active?: {
    channelId: number;
    selfId?: string;
    joinedAt: number;
    peers: Record<string, GroupPeer>;
    local?: MediaStream;
    muted: boolean;
    camOff: boolean;
    minimized: boolean;
    /** Người mở cuộc gọi: số người đang online trong kênh đã được mời. */
    invited?: number;
  };
  /** Lời mời khi có người bắt đầu gọi nhóm. */
  invite?: { channelId: number; by: PublicUser; video: boolean };
  error?: string;
};

type Send = (req: Record<string, unknown>) => boolean;

type GroupEvent =
  | { type: "groupcall.state"; channel_id: number; started_at?: string; participants?: GroupParticipant[] }
  | { type: "groupcall.started"; channel_id: number; by: PublicUser; video?: boolean }
  | { type: "groupcall.welcome"; channel_id: number; self: string; participants?: GroupParticipant[]; invited?: number }
  | {
      type: "groupcall.signal";
      channel_id: number;
      from: string;
      from_user: PublicUser;
      data: { sdp?: RTCSessionDescriptionInit; candidate?: RTCIceCandidateInit };
    }
  | { type: "groupcall.error"; channel_id: number; message: string };

/** Người gửi offer mà thiếu track nào thì thêm kênh chỉ-nhận cho loại đó. */
export function recvOnlyIfMissing(pc: RTCPeerConnection, local?: MediaStream) {
  if (!local?.getVideoTracks().length) pc.addTransceiver("video", { direction: "recvonly" });
  if (!local?.getAudioTracks().length) pc.addTransceiver("audio", { direction: "recvonly" });
}

export class GroupCallController {
  private state: GroupState = { rooms: {} };
  private listeners = new Set<() => void>();
  private pcs = new Map<string, RTCPeerConnection>();
  private pendingIce = new Map<string, RTCIceCandidateInit[]>();
  private iceServers: RTCIceServer[] | null = null;
  private ringer = new Ringer();
  private ringTimer: number | undefined;

  constructor(
    private api: Api,
    private send: Send,
    private notify: (title: string, body: string, channelId: number) => void,
    /** Đang trong cuộc gọi 1-1 thì không vào gọi nhóm. */
    private busyElsewhere: () => boolean,
  ) {}

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };
  getSnapshot = () => this.state;
  private set(patch: Partial<GroupState>) {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((fn) => fn());
  }
  private setActive(patch: Partial<NonNullable<GroupState["active"]>>) {
    if (!this.state.active) return;
    this.set({ active: { ...this.state.active, ...patch } });
  }

  get inCall() {
    return !!this.state.active;
  }

  /* ---------- Hành động ---------- */

  async join(channelId: number, video: boolean) {
    if (this.state.active || this.busyElsewhere()) return;
    if (!navigator.mediaDevices?.getUserMedia) {
      this.set({ error: await insecureMediaMessage(this.api) });
      return;
    }
    let local: MediaStream;
    try {
      local = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true },
        video,
      });
    } catch {
      try {
        local = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
      } catch {
        this.set({ error: "Không mở được micro. Hãy cho phép trang dùng micro." });
        return;
      }
    }
    // lấy mới mỗi lần vào: tài khoản TURN tạm có hạn
    this.iceServers = await this.api
      .rtcConfig()
      .then((r) => r.ice_servers)
      .catch(() => this.iceServers ?? []);
    this.ringer.stop();
    this.set({
      invite: undefined,
      error: undefined,
      active: { channelId, joinedAt: Date.now(), peers: {}, local, muted: false, camOff: false, minimized: false },
    });
    if (!this.send({ type: "groupcall.join", channel_id: channelId, video: local.getVideoTracks().length > 0 })) {
      this.cleanup("Đang mất kết nối tới máy chủ.");
    }
  }

  leave() {
    const a = this.state.active;
    if (!a) return;
    this.send({ type: "groupcall.leave", channel_id: a.channelId });
    this.cleanup();
  }

  toggleMute() {
    const a = this.state.active;
    if (!a) return;
    a.local?.getAudioTracks().forEach((t) => (t.enabled = a.muted));
    this.setActive({ muted: !a.muted });
  }

  toggleCamera() {
    const a = this.state.active;
    if (!a) return;
    a.local?.getVideoTracks().forEach((t) => (t.enabled = a.camOff));
    this.setActive({ camOff: !a.camOff });
  }

  minimize(v: boolean) {
    this.setActive({ minimized: v });
  }

  dismissInvite() {
    this.ringer.stop();
    this.set({ invite: undefined });
  }

  dismissError() {
    this.set({ error: undefined });
  }

  /* ---------- Kết nối với từng người ---------- */

  private peer(peerId: string, user: PublicUser): RTCPeerConnection {
    let pc = this.pcs.get(peerId);
    if (pc) return pc;
    const a = this.state.active!;
    pc = new RTCPeerConnection({ iceServers: this.iceServers ?? [] });
    const stream = new MediaStream();
    const channelId = a.channelId;
    pc.ontrack = (e) => {
      stream.addTrack(e.track);
      this.updatePeer(peerId, { stream });
    };
    pc.onicecandidate = (e) => {
      if (e.candidate) {
        this.send({
          type: "groupcall.signal",
          channel_id: channelId,
          to: peerId,
          data: { candidate: e.candidate.toJSON() },
        });
      }
    };
    pc.onconnectionstatechange = () => this.updatePeer(peerId, { state: pc!.connectionState });
    a.local?.getTracks().forEach((t) => pc!.addTrack(t, a.local!));
    this.pcs.set(peerId, pc);
    this.setActive({ peers: { ...a.peers, [peerId]: { peerId, user, stream, state: "new" } } });
    return pc;
  }

  private updatePeer(peerId: string, patch: Partial<GroupPeer>) {
    const a = this.state.active;
    const p = a?.peers[peerId];
    if (!a || !p) return;
    this.setActive({ peers: { ...a.peers, [peerId]: { ...p, ...patch } } });
  }

  private dropPeer(peerId: string) {
    this.pcs.get(peerId)?.close();
    this.pcs.delete(peerId);
    this.pendingIce.delete(peerId);
    const a = this.state.active;
    if (a?.peers[peerId]) {
      const { [peerId]: _gone, ...rest } = a.peers;
      this.setActive({ peers: rest });
    }
  }

  private cleanup(error?: string) {
    for (const id of [...this.pcs.keys()]) this.dropPeer(id);
    this.state.active?.local?.getTracks().forEach((t) => t.stop());
    this.set({ active: undefined, ...(error ? { error } : {}) });
  }

  /* ---------- Sự kiện ---------- */

  setRooms(rooms: GroupRoom[]) {
    const map: Record<number, GroupRoom> = {};
    for (const r of rooms) map[r.channel_id] = r;
    this.set({ rooms: map });
  }

  async onEvent(ev: GroupEvent) {
    const a = this.state.active;
    switch (ev.type) {
      case "groupcall.state": {
        const participants = ev.participants ?? [];
        const rooms = { ...this.state.rooms };
        if (participants.length) rooms[ev.channel_id] = { ...ev, participants };
        else delete rooms[ev.channel_id];
        this.set({ rooms });
        if (!participants.length && this.state.invite?.channelId === ev.channel_id) {
          this.ringer.stop();
          this.set({ invite: undefined });
        }
        // người đã rời: đóng kết nối tới họ
        if (a && a.channelId === ev.channel_id) {
          const present = new Set(participants.map((p) => p.peer_id));
          for (const id of Object.keys(a.peers)) if (!present.has(id)) this.dropPeer(id);
        }
        return;
      }
      case "groupcall.started": {
        if (a || this.busyElsewhere()) return;
        this.set({ invite: { channelId: ev.channel_id, by: ev.by, video: !!ev.video } });
        // đổ chuông ~20 giây (dừng khi tham gia / bỏ qua / cuộc gọi kết thúc)
        this.ringer.start("incoming");
        window.clearTimeout(this.ringTimer);
        this.ringTimer = window.setTimeout(() => this.ringer.stop(), 20000);
        this.notify("📞 Cuộc gọi nhóm", `${ev.by.display_name || ev.by.username} vừa bắt đầu gọi nhóm`, ev.channel_id);
        return;
      }
      case "groupcall.welcome": {
        if (!a || a.channelId !== ev.channel_id) return;
        this.setActive({ selfId: ev.self, invited: ev.invited });
        // người mới vào: gửi offer cho từng người đang có mặt
        for (const p of ev.participants ?? []) {
          const pc = this.peer(p.peer_id, p.user);
          try {
            // mình không bật camera/micro vẫn phải xin NHẬN hình/tiếng của người kia
            recvOnlyIfMissing(pc, a.local);
            await pc.setLocalDescription(await pc.createOffer());
            this.send({
              type: "groupcall.signal",
              channel_id: ev.channel_id,
              to: p.peer_id,
              data: { sdp: pc.localDescription!.toJSON() },
            });
          } catch {
            this.dropPeer(p.peer_id);
          }
        }
        return;
      }
      case "groupcall.signal": {
        if (!a || a.channelId !== ev.channel_id) return;
        const pc = this.peer(ev.from, ev.from_user);
        try {
          if (ev.data.sdp) {
            await pc.setRemoteDescription(ev.data.sdp);
            if (ev.data.sdp.type === "offer") {
              await pc.setLocalDescription(await pc.createAnswer());
              this.send({
                type: "groupcall.signal",
                channel_id: ev.channel_id,
                to: ev.from,
                data: { sdp: pc.localDescription!.toJSON() },
              });
            }
            for (const c of this.pendingIce.get(ev.from)?.splice(0) ?? []) await pc.addIceCandidate(c).catch(() => {});
          } else if (ev.data.candidate) {
            if (pc.remoteDescription) await pc.addIceCandidate(ev.data.candidate).catch(() => {});
            else this.pendingIce.set(ev.from, [...(this.pendingIce.get(ev.from) ?? []), ev.data.candidate]);
          }
        } catch {
          this.dropPeer(ev.from);
        }
        return;
      }
      case "groupcall.error":
        if (a && a.channelId === ev.channel_id) this.cleanup(ev.message);
        else this.set({ error: ev.message });
        return;
    }
  }

  /** Mất kết nối máy chủ: không còn nhận tín hiệu, rời cuộc gọi cho sạch. */
  onDisconnected() {
    if (this.state.active) this.cleanup("Mất kết nối tới máy chủ, đã rời cuộc gọi nhóm.");
  }
}

const EMPTY_STATE: GroupState = { rooms: {} };
const EMPTY = { subscribe: () => () => {}, getSnapshot: () => EMPTY_STATE };
export function useGroupCall(ctrl: GroupCallController | null): GroupState {
  const c = ctrl ?? EMPTY;
  return useSyncExternalStore(c.subscribe, c.getSnapshot);
}
