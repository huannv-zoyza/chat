/**
 * Mã hoá đầu cuối cho tin nhắn riêng (giống "cuộc trò chuyện được mã hoá" của Messenger).
 *
 *  - Mỗi người có một cặp khoá X25519. Khoá công khai đưa lên server; khoá bí mật chỉ nằm
 *    trên máy, và một bản sao lưu được khoá bằng mã PIN (Argon2id -> XChaCha20-Poly1305)
 *    để đăng nhập máy khác vẫn đọc được lịch sử. Server không có PIN nên không mở được.
 *  - Tin trong cuộc trò chuyện A–B: khoá chung = X25519(khoá bí mật của mình, khoá công khai
 *    người kia) (crypto_box_beforenm), mã hoá XChaCha20-Poly1305 với dữ liệu kèm
 *    "gochat:v1:<kênh>:<người gửi>", nên server không thể chuyển tin sang kênh khác hay đổi
 *    người gửi.
 *  - File: mỗi file một khoá ngẫu nhiên; khoá + tên + loại file nằm trong tin đã mã hoá.
 *
 * libsodium (WASM, chạy được cả trên http LAN không có crypto.subtle) chỉ được tải khi cần.
 */
import { useSyncExternalStore } from "react";
import { ApiError, type Api, type Embed } from "./api";
import { load, remove, save } from "./storage";

type Sodium = typeof import("libsodium-wrappers-sumo");

let sodium: Sodium | null = null;
let sodiumLoading: Promise<Sodium> | null = null;

export function loadSodium(): Promise<Sodium> {
  sodiumLoading ??= import("libsodium-wrappers-sumo").then(async (m) => {
    const s = ((m as { default?: Sodium }).default ?? m) as Sodium;
    await s.ready;
    sodium = s;
    return s;
  });
  return sodiumLoading;
}

const S = () => {
  if (!sodium) throw new Error("libsodium chưa sẵn sàng");
  return sodium;
};
const b64 = (x: Uint8Array) => S().to_base64(x, S().base64_variants.ORIGINAL);
const unb64 = (x: string) => S().from_base64(x, S().base64_variants.ORIGINAL);

/* ---------- Kiểu dữ liệu ---------- */

/** File đính kèm đã mã hoá: thông tin thật + khoá để giải mã (chỉ nằm trong tin mã hoá). */
export type EncFile = { name: string; type: string; size: number; w?: number; h?: number; k: string; n: string };

/** Nội dung thật của một tin mã hoá. */
export type Payload = { text?: string; embed?: Embed; files?: EncFile[] };

type Envelope = { v: 1; s: string; r: string; n: string; c: string };

type Backup = { v: 1; kdf: "argon2id13"; ops: number; mem: number; salt: string; nonce: string; ct: string };

type Identity = { pub: Uint8Array; priv: Uint8Array; pubB64: string };

export type E2EEStatus =
  | "off" // chưa đăng nhập
  | "loading"
  | "none" // tài khoản chưa có khoá: cần tạo mã PIN
  | "locked" // có khoá trên server nhưng máy này chưa mở: cần nhập mã PIN
  | "ready"
  | "error"; // không hỏi được server

export type DecryptResult = { ok: true; payload: Payload } | { ok: false; reason: "locked" | "failed" };

// Argon2id: 3 lượt, 64 MB RAM (~0,2 s). Đủ chậm để dò PIN tốn kém, đủ nhanh để không phải chờ.
const PW_OPS = 3;
const PW_MEM = 64 * 1024 * 1024;

const ad = (channelId: number, senderId: number) => `gochat:v1:${channelId}:${senderId}`;

/* ---------- Kiểm tra nội dung giải mã (server không kiểm tra hộ được nữa) ---------- */

const GIPHY_HOST = /^(media[0-9]*|i)\.giphy\.com$/;
const SAFE_INLINE = /^(image\/(png|jpeg|gif|webp)|video\/[\w.+-]+|audio\/[\w.+-]+)$/;

function cleanPayload(raw: unknown): Payload | null {
  if (!raw || typeof raw !== "object") return null;
  const p = raw as Record<string, unknown>;
  const out: Payload = {};
  if (typeof p.text === "string") out.text = p.text.slice(0, 4000);
  if (p.embed && typeof p.embed === "object") {
    const e = p.embed as Embed;
    if (e.type === "sticker" && typeof e.id === "string" && /^[a-z0-9]{1,20}\/[a-z0-9-]{1,40}$/.test(e.id)) {
      out.embed = { type: "sticker", id: e.id };
    } else if (e.type === "gif" && typeof e.url === "string") {
      try {
        const u = new URL(e.url);
        if (u.protocol === "https:" && GIPHY_HOST.test(u.hostname)) {
          out.embed = {
            type: "gif",
            id: String(e.id ?? ""),
            url: e.url,
            width: Number(e.width) || 200,
            height: Number(e.height) || 200,
            title: typeof e.title === "string" ? e.title.slice(0, 200) : "",
          };
        }
      } catch {
        /* bỏ qua GIF sai */
      }
    }
  }
  if (Array.isArray(p.files)) {
    out.files = p.files.slice(0, 10).flatMap((f): EncFile[] => {
      if (!f || typeof f !== "object") return [];
      const x = f as Record<string, unknown>;
      if (typeof x.k !== "string" || typeof x.n !== "string") return [];
      const type = typeof x.type === "string" ? x.type.toLowerCase() : "";
      return [
        {
          name: typeof x.name === "string" ? x.name.slice(0, 200) : "file",
          // Loại file do người gửi khai: chỉ cho hiển thị ngay những loại an toàn,
          // còn lại (html, svg, …) coi như file nhị phân để tải về.
          type: SAFE_INLINE.test(type) ? type : "application/octet-stream",
          size: Number(x.size) || 0,
          w: Number(x.w) || undefined,
          h: Number(x.h) || undefined,
          k: x.k,
          n: x.n,
        },
      ];
    });
  }
  return out;
}

/* ---------- Kho khoá (một bản cho cả app) ---------- */

class E2EEStore {
  private snap: { status: E2EEStatus; version: number } = { status: "off", version: 0 };
  private listeners = new Set<() => void>();
  private api: Api | null = null;
  private meId = 0;
  private storageKey = "";
  private identity: Identity | null = null;
  private serverKey: { public_key: string; backup?: string } | null = null;
  private peers = new Map<number, string | null>(); // userId -> khoá công khai (null: chưa có khoá)
  private peerLoading = new Map<number, Promise<string | null>>();
  private shared = new Map<string, Uint8Array>();
  private cache = new Map<number, { cipher: string; result: DecryptResult }>();

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };
  getSnapshot = () => this.snap;

  private set(status: E2EEStatus) {
    this.snap = { status, version: this.snap.version + 1 };
    this.listeners.forEach((fn) => fn());
  }
  private bump() {
    this.set(this.snap.status);
  }

  get status() {
    return this.snap.status;
  }

  /** Chờ đến khi biết trạng thái (hết "loading"), tối đa ms. */
  settled(ms = 8000): Promise<E2EEStatus> {
    if (this.snap.status !== "loading") return Promise.resolve(this.snap.status);
    return new Promise((resolve) => {
      const done = () => {
        if (this.snap.status === "loading") return;
        off();
        window.clearTimeout(t);
        resolve(this.snap.status);
      };
      const off = this.subscribe(done);
      const t = window.setTimeout(() => {
        off();
        resolve(this.snap.status);
      }, ms);
    });
  }
  get publicKey() {
    return this.identity?.pubB64 ?? null;
  }

  /** Gọi khi đăng nhập xong. */
  async start(api: Api, meId: number, serverUrl: string) {
    this.api = api;
    this.meId = meId;
    this.storageKey = `e2ee:${serverUrl}:${meId}`;
    this.identity = null;
    this.peers.clear();
    this.peerLoading.clear();
    this.shared.clear();
    this.cache.clear();
    this.set("loading");
    try {
      await loadSodium();
      this.serverKey = await api.myKey().catch((err) => {
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      });
    } catch (err) {
      console.error("e2ee:", err);
      this.set("error");
      return;
    }
    if (api !== this.api) return; // đã đăng xuất / đổi tài khoản trong lúc chờ
    const local = load<{ pub: string; priv: string } | null>(this.storageKey, null);
    if (!this.serverKey) {
      remove(this.storageKey);
      this.set("none");
    } else if (local && local.pub === this.serverKey.public_key) {
      this.useIdentity(unb64(local.priv));
      this.set("ready");
    } else {
      // khoá trên server đã đổi (tạo lại ở máy khác) hoặc máy này chưa mở: cần PIN
      remove(this.storageKey);
      this.set("locked");
    }
  }

  stop() {
    this.api = null;
    this.identity = null;
    this.serverKey = null;
    this.cache.clear();
    this.shared.clear();
    this.set("off");
  }

  /** Đăng xuất: xoá khoá bí mật khỏi máy này (đăng nhập lại cần mã PIN). */
  forget() {
    if (this.storageKey) remove(this.storageKey);
    this.stop();
  }

  private useIdentity(priv: Uint8Array) {
    const pub = S().crypto_scalarmult_base(priv);
    this.identity = { pub, priv, pubB64: b64(pub) };
    this.peers.set(this.meId, this.identity.pubB64);
    save(this.storageKey, { pub: this.identity.pubB64, priv: b64(priv) });
    this.cache.clear();
    this.shared.clear();
  }

  private wrap(priv: Uint8Array, pub: Uint8Array, pin: string): string {
    const s = S();
    const salt = s.randombytes_buf(s.crypto_pwhash_SALTBYTES);
    const key = s.crypto_pwhash(32, pin, salt, PW_OPS, PW_MEM, s.crypto_pwhash_ALG_ARGON2ID13);
    const nonce = s.randombytes_buf(24);
    const ct = s.crypto_aead_xchacha20poly1305_ietf_encrypt(priv, pub, null, nonce, key);
    const backup: Backup = {
      v: 1,
      kdf: "argon2id13",
      ops: PW_OPS,
      mem: PW_MEM,
      salt: b64(salt),
      nonce: b64(nonce),
      ct: b64(ct),
    };
    return JSON.stringify(backup);
  }

  /** Lần đầu: tạo khoá, khoá bản sao lưu bằng PIN. replace: tạo khoá mới thay khoá cũ (quên PIN). */
  async setup(pin: string, replace = false) {
    const s = await loadSodium();
    const kp = s.crypto_box_keypair();
    const backup = this.wrap(kp.privateKey, kp.publicKey, pin);
    const res = await this.api!.putMyKey({ public_key: b64(kp.publicKey), backup, replace });
    this.serverKey = res;
    this.useIdentity(kp.privateKey);
    this.set("ready");
  }

  /** Máy mới: mở khoá bằng PIN. Sai PIN thì ném lỗi. */
  async unlock(pin: string) {
    const s = await loadSodium();
    const key = this.serverKey ?? (await this.api!.myKey());
    this.serverKey = key;
    let b: Backup;
    try {
      b = JSON.parse(key.backup ?? "");
    } catch {
      throw new Error("Bản sao lưu khoá bị hỏng. Hãy tạo khoá mới.");
    }
    const pub = unb64(key.public_key);
    const k = s.crypto_pwhash(32, pin, unb64(b.salt), b.ops, b.mem, s.crypto_pwhash_ALG_ARGON2ID13);
    let priv: Uint8Array;
    try {
      priv = s.crypto_aead_xchacha20poly1305_ietf_decrypt(null, unb64(b.ct), pub, unb64(b.nonce), k);
    } catch {
      throw new Error("Mã PIN không đúng.");
    }
    if (b64(s.crypto_scalarmult_base(priv)) !== key.public_key) throw new Error("Khoá không khớp. Hãy tạo khoá mới.");
    this.useIdentity(priv);
    this.set("ready");
  }

  /** Đổi mã PIN (giữ nguyên khoá, chỉ khoá lại bản sao lưu). */
  async changePin(pin: string) {
    if (!this.identity) throw new Error("Chưa mở khoá.");
    const backup = this.wrap(this.identity.priv, this.identity.pub, pin);
    this.serverKey = await this.api!.putMyKey({ public_key: this.identity.pubB64, backup });
    this.bump();
  }

  /** Sự kiện key.updated từ server. */
  onKeyUpdated(userId: number, publicKey: string) {
    if (userId === this.meId) {
      if (this.identity && this.identity.pubB64 !== publicKey) {
        // khoá vừa được tạo lại ở máy khác: khoá ở máy này hết hiệu lực
        remove(this.storageKey);
        this.identity = null;
        this.serverKey = null;
        this.cache.clear();
        this.set("locked");
      } else if (!this.identity && this.snap.status === "none") {
        this.serverKey = null;
        this.set("locked");
      }
      return;
    }
    this.peers.set(userId, publicKey);
    this.peerLoading.delete(userId);
    this.bump();
  }

  /* ---------- Khoá của người khác ---------- */

  /** undefined: chưa biết (đang hỏi server); null: người đó chưa thiết lập mã hoá. */
  peerKeyCached(userId: number): string | null | undefined {
    return this.peers.get(userId);
  }

  peerKey(userId: number): Promise<string | null> {
    if (this.peers.has(userId)) return Promise.resolve(this.peers.get(userId)!);
    let p = this.peerLoading.get(userId);
    if (!p) {
      p = this.api!.userKey(userId)
        .then((k) => k.public_key)
        .catch((err) => {
          if (err instanceof ApiError && err.status === 404) return null;
          this.peerLoading.delete(userId);
          throw err;
        })
        .then((k) => {
          this.peers.set(userId, k);
          this.bump();
          return k;
        });
      this.peerLoading.set(userId, p);
    }
    return p;
  }

  /* ---------- Mã hoá / giải mã ---------- */

  private sharedKey(otherPubB64: string): Uint8Array {
    let k = this.shared.get(otherPubB64);
    if (!k) {
      k = S().crypto_box_beforenm(unb64(otherPubB64), this.identity!.priv);
      this.shared.set(otherPubB64, k);
    }
    return k;
  }

  /** Mã hoá nội dung tin gửi cho người có khoá công khai theirPub. */
  encrypt(payload: Payload, channelId: number, theirPub: string): string {
    if (!this.identity) throw new Error("Chưa mở khoá mã hoá.");
    const s = S();
    const nonce = s.randombytes_buf(24);
    const ct = s.crypto_aead_xchacha20poly1305_ietf_encrypt(
      JSON.stringify(payload),
      ad(channelId, this.meId),
      null,
      nonce,
      this.sharedKey(theirPub),
    );
    const env: Envelope = { v: 1, s: this.identity.pubB64, r: theirPub, n: b64(nonce), c: b64(ct) };
    return JSON.stringify(env);
  }

  /** Giải mã tin (có bộ nhớ đệm, gọi thoải mái trong lúc render). */
  decrypt(m: { id: number; channel_id: number; user_id: number; cipher?: string }): DecryptResult {
    if (!m.cipher) return { ok: false, reason: "failed" };
    if (!this.identity || !sodium) return { ok: false, reason: "locked" };
    const hit = this.cache.get(m.id);
    if (hit && hit.cipher === m.cipher) return hit.result;
    let result: DecryptResult;
    try {
      const env = JSON.parse(m.cipher) as Envelope;
      const mine = this.identity.pubB64;
      if (env.v !== 1 || (env.s !== mine && env.r !== mine)) throw new Error("not for this key");
      const other = env.s === mine ? env.r : env.s;
      const pt = S().crypto_aead_xchacha20poly1305_ietf_decrypt(
        null,
        unb64(env.c),
        ad(m.channel_id, m.user_id),
        unb64(env.n),
        this.sharedKey(other),
      );
      const payload = cleanPayload(JSON.parse(S().to_string(pt)));
      result = payload ? { ok: true, payload } : { ok: false, reason: "failed" };
    } catch {
      result = { ok: false, reason: "failed" };
    }
    this.cache.set(m.id, { cipher: m.cipher, result });
    return result;
  }

  /** Mã hoá một file bằng khoá ngẫu nhiên riêng. */
  async encryptFile(file: File): Promise<{ blob: Blob; meta: EncFile }> {
    const s = await loadSodium();
    const key = s.crypto_aead_xchacha20poly1305_ietf_keygen();
    const nonce = s.randombytes_buf(24);
    const plain = new Uint8Array(await file.arrayBuffer());
    const ct = s.crypto_aead_xchacha20poly1305_ietf_encrypt(plain, null, null, nonce, key);
    const dims = await imageSize(file);
    return {
      blob: new Blob([ct as BlobPart], { type: "application/octet-stream" }),
      meta: { name: file.name, type: file.type, size: file.size, ...dims, k: b64(key), n: b64(nonce) },
    };
  }

  async decryptFile(data: ArrayBuffer, meta: EncFile): Promise<Blob> {
    const s = await loadSodium();
    const pt = s.crypto_aead_xchacha20poly1305_ietf_decrypt(
      null,
      new Uint8Array(data),
      null,
      unb64(meta.n),
      unb64(meta.k),
    );
    return new Blob([pt as BlobPart], { type: meta.type });
  }

  /**
   * Mã an toàn của cặp (mình, người kia): 60 chữ số, hai bên thấy giống nhau.
   * So khớp (gặp trực tiếp, gọi điện) để chắc server không tráo khoá.
   */
  safetyNumber(peerId: number, peerPub: string): string {
    if (!this.identity) return "";
    const s = S();
    const parts = [
      [this.meId, this.identity.pubB64],
      [peerId, peerPub],
    ].sort((a, b) => Number(a[0]) - Number(b[0]));
    const h = s.crypto_generichash(64, `gochat-safety-v1|${parts.map((p) => p.join(":")).join("|")}`);
    const groups: string[] = [];
    for (let i = 0; i < 12; i++) {
      const v = ((h[i * 4] << 24) | (h[i * 4 + 1] << 16) | (h[i * 4 + 2] << 8) | h[i * 4 + 3]) >>> 0;
      groups.push(String(v % 100000).padStart(5, "0"));
    }
    return groups.join(" ");
  }
}

async function imageSize(file: File): Promise<{ w?: number; h?: number }> {
  if (!/^image\/(png|jpeg|gif|webp)$/.test(file.type)) return {};
  try {
    const bmp = await createImageBitmap(file);
    const out = { w: bmp.width, h: bmp.height };
    bmp.close();
    return out;
  } catch {
    return {};
  }
}

export const e2ee = new E2EEStore();

/** Theo dõi trạng thái mã hoá (render lại khi mở khoá, có khoá người khác, ...). */
export function useE2EE() {
  return useSyncExternalStore(e2ee.subscribe, e2ee.getSnapshot);
}
