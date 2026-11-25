/**
 * Chia sẻ màn hình trong cuộc gọi (1-1 và nhóm): thay nguồn hình gửi đi của kết nối WebRTC,
 * đàm phán lại SDP khi cần (cuộc gọi chỉ có tiếng chưa có kênh hình để gửi).
 *
 * Báo cho máy bên kia bằng cờ `screen` trong dữ liệu tín hiệu (máy chủ chuyển nguyên văn).
 */

/** Trình duyệt / app có chia sẻ màn hình được không (điện thoại thường không). */
export function canShareScreen(): boolean {
  return typeof navigator !== "undefined" && typeof navigator.mediaDevices?.getDisplayMedia === "function";
}

/** Hỏi người dùng chọn màn hình / cửa sổ / tab. Trả null nếu người dùng huỷ. */
export async function captureScreen(): Promise<MediaStream | null> {
  if (!canShareScreen()) throw new Error("Trình duyệt / app này chưa hỗ trợ chia sẻ màn hình.");
  let stream: MediaStream;
  try {
    stream = await navigator.mediaDevices.getDisplayMedia({
      video: { frameRate: { ideal: 15, max: 30 } },
      audio: false,
    });
  } catch (err) {
    // NotAllowedError: bấm Huỷ trong hộp chọn màn hình (hoặc hệ điều hành chặn)
    if (err instanceof DOMException && err.name === "NotAllowedError") return null;
    throw new Error("Không chia sẻ được màn hình.");
  }
  const track = stream.getVideoTracks()[0];
  if (!track) {
    stream.getTracks().forEach((t) => t.stop());
    return null;
  }
  // ưu tiên độ nét (chữ, code) hơn độ mượt
  if ("contentHint" in track) track.contentHint = "detail";
  return stream;
}

/**
 * Đặt nguồn hình gửi đi: track (màn hình / camera) hoặc null (thôi gửi hình).
 * Trả true nếu phải đàm phán lại SDP (thêm kênh hình hoặc đổi chiều gửi/nhận).
 */
export async function setVideoSource(
  pc: RTCPeerConnection,
  track: MediaStreamTrack | null,
  stream?: MediaStream,
): Promise<boolean> {
  const tr = pc.getTransceivers().find((t) => t.receiver.track?.kind === "video" && t.currentDirection !== "stopped");
  if (!tr) {
    if (!track) return false;
    pc.addTrack(track, ...(stream ? [stream] : []));
    return true;
  }
  await tr.sender.replaceTrack(track);
  const sending = tr.direction === "sendrecv" || tr.direction === "sendonly";
  if (track && !sending) {
    tr.direction = tr.direction === "inactive" ? "sendonly" : "sendrecv";
    return true;
  }
  if (!track && sending) {
    tr.direction = tr.direction === "sendonly" ? "inactive" : "recvonly";
    return true;
  }
  return false;
}

/**
 * Đàm phán lại SDP sau khi đã kết nối, theo kiểu "perfect negotiation": nếu hai bên cùng gửi
 * offer một lúc thì bên "lịch sự" (polite) nhường, nhận offer của bên kia rồi gửi lại thay đổi của mình.
 */
export class Renegotiator {
  private makingOffer = false;
  private ready = false;
  /** Số lần cần đàm phán (tăng mỗi lần request) / đã gửi offer / bên kia đã trả lời. */
  private wanted = 0;
  private sent = 0;
  private acked = 0;
  private chain: Promise<void> = Promise.resolve();

  constructor(
    private pc: RTCPeerConnection,
    private polite: boolean,
    private sendSdp: (sdp: RTCSessionDescriptionInit) => void,
  ) {}

  /**
   * Xử lý tín hiệu nhận được lần lượt từng cái: cái sau chờ cái trước áp xong, nếu không
   * thì offer đến ngay sau answer bị tưởng là đụng độ (đang chờ answer áp xong) và bị bỏ qua.
   */
  serial(fn: () => Promise<void>): Promise<void> {
    const p = this.chain.then(fn);
    this.chain = p.catch(() => {});
    return p;
  }

  /** Gọi khi kết nối lần đầu xong; trước đó mọi yêu cầu được để dành. */
  markReady() {
    this.ready = true;
    this.maybeNegotiate();
  }

  /** Có thay đổi cần báo bên kia (setVideoSource trả true). */
  request() {
    this.wanted++;
    this.maybeNegotiate();
  }

  /**
   * Trước khi áp SDP nhận được: true = bỏ qua. Bỏ offer đụng độ (bên không nhường), và answer
   * cho offer mình đã rút lại (đã nhường bên kia; thay đổi của mình sẽ được gửi lại sau).
   */
  shouldIgnore(desc: RTCSessionDescriptionInit): boolean {
    if (desc.type === "answer") return this.pc.signalingState !== "have-local-offer";
    const collision = desc.type === "offer" && (this.makingOffer || this.pc.signalingState !== "stable");
    return collision && !this.polite;
  }

  /** Sau khi áp xong SDP nhận được (và đã trả lời nếu là offer). */
  afterRemote(desc: RTCSessionDescriptionInit) {
    if (desc.type === "answer") this.acked = this.sent;
    this.maybeNegotiate();
  }

  private maybeNegotiate() {
    if (!this.ready || this.wanted <= this.acked || this.makingOffer) return;
    if (this.pc.signalingState !== "stable") return; // chạy lại ở afterRemote
    void this.negotiate();
  }

  private async negotiate() {
    this.makingOffer = true;
    const target = this.wanted;
    try {
      const offer = await this.pc.createOffer();
      if (this.pc.signalingState !== "stable") return;
      await this.pc.setLocalDescription(offer);
      this.sent = target;
      this.sendSdp(this.pc.localDescription!.toJSON());
    } catch {
      /* bị offer của bên kia chen ngang (đã rollback): làm lại sau afterRemote */
    } finally {
      this.makingOffer = false;
    }
  }
}
