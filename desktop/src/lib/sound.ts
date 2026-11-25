import { load, save } from "./storage";

export const getSoundMentionsPref = () => load<boolean>("soundMentions", true);
export const setSoundMentionsPref = (v: boolean) => save("soundMentions", v);

export const getSoundMessagesPref = () => load<boolean>("soundMessages", true);
export const setSoundMessagesPref = (v: boolean) => save("soundMessages", v);

let audioCtx: AudioContext | null = null;
let lastMessageSoundTime = 0;

function getAudioContext(): AudioContext | null {
  if (typeof window === "undefined") return null;
  const AudioContextClass =
    window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
  if (!AudioContextClass) return null;
  if (!audioCtx) {
    try {
      audioCtx = new AudioContextClass();
    } catch {
      return null;
    }
  }
  if (audioCtx.state === "suspended") {
    void audioCtx.resume().catch(() => {});
  }
  return audioCtx;
}

// Unlock AudioContext ngay khi người dùng có thao tác đầu tiên trên trang
if (typeof window !== "undefined") {
  const unlock = () => {
    if (audioCtx && audioCtx.state === "suspended") {
      void audioCtx.resume().catch(() => {});
    }
    window.removeEventListener("pointerdown", unlock);
    window.removeEventListener("keydown", unlock);
  };
  window.addEventListener("pointerdown", unlock, { passive: true });
  window.addEventListener("keydown", unlock, { passive: true });
}

/**
 * Phát tiếng chuông hai nốt du dương (G5 -> C6) khi có người nhắc tên, tin riêng DM hoặc trả lời thread.
 */
export function playMentionSound(force = false) {
  if (!force && !getSoundMentionsPref()) return;
  const ctx = getAudioContext();
  if (!ctx) return;

  try {
    const now = ctx.currentTime;

    // Nốt 1: G5 (783.99 Hz)
    const o1 = ctx.createOscillator();
    const g1 = ctx.createGain();
    o1.type = "sine";
    o1.frequency.setValueAtTime(783.99, now);
    g1.gain.setValueAtTime(0.001, now);
    g1.gain.exponentialRampToValueAtTime(0.18, now + 0.02);
    g1.gain.exponentialRampToValueAtTime(0.001, now + 0.15);
    o1.connect(g1).connect(ctx.destination);
    o1.start(now);
    o1.stop(now + 0.16);

    // Nốt 2: C6 (1046.50 Hz) - độ ngân vang cao hơn
    const o2 = ctx.createOscillator();
    const g2 = ctx.createGain();
    o2.type = "sine";
    o2.frequency.setValueAtTime(1046.5, now + 0.08);
    g2.gain.setValueAtTime(0.001, now + 0.08);
    g2.gain.exponentialRampToValueAtTime(0.22, now + 0.1);
    g2.gain.exponentialRampToValueAtTime(0.0001, now + 0.38);
    o2.connect(g2).connect(ctx.destination);
    o2.start(now + 0.08);
    o2.stop(now + 0.4);
  } catch (err) {
    console.warn("không phát được âm thanh mention:", err);
  }
}

/**
 * Phát tiếng pop/bloop ngắn nhẹ khi có tin nhắn mới thường (có throttle chống dồn âm).
 */
export function playMessageSound(force = false) {
  if (!force && !getSoundMessagesPref()) return;
  const nowMs = Date.now();
  if (!force && nowMs - lastMessageSoundTime < 150) return;
  lastMessageSoundTime = nowMs;

  const ctx = getAudioContext();
  if (!ctx) return;

  try {
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();

    osc.type = "sine";
    // Pitch sweep từ 480 Hz xuống 240 Hz
    osc.frequency.setValueAtTime(480, now);
    osc.frequency.exponentialRampToValueAtTime(240, now + 0.06);

    gain.gain.setValueAtTime(0.001, now);
    gain.gain.exponentialRampToValueAtTime(0.12, now + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.07);

    osc.connect(gain).connect(ctx.destination);
    osc.start(now);
    osc.stop(now + 0.08);
  } catch (err) {
    console.warn("không phát được âm thanh tin nhắn:", err);
  }
}

/**
 * Phát tiếng gõ quân cờ khi đặt quân xuống bàn (âm thanh tiếng gỗ đanh gọn).
 */
export function playPiecePlace() {
  const ctx = getAudioContext();
  if (!ctx) return;
  try {
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();

    osc.type = "triangle";
    osc.frequency.setValueAtTime(320, now);
    osc.frequency.exponentialRampToValueAtTime(110, now + 0.05);

    gain.gain.setValueAtTime(0.001, now);
    gain.gain.exponentialRampToValueAtTime(0.18, now + 0.008);
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.06);

    osc.connect(gain).connect(ctx.destination);
    osc.start(now);
    osc.stop(now + 0.07);
  } catch {
    // ignore
  }
}

/**
 * Phát tiếng gõ mạnh khi ăn quân cờ đối phương.
 */
export function playPieceCapture() {
  const ctx = getAudioContext();
  if (!ctx) return;
  try {
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();

    osc.type = "sine";
    osc.frequency.setValueAtTime(440, now);
    osc.frequency.exponentialRampToValueAtTime(90, now + 0.08);

    gain.gain.setValueAtTime(0.001, now);
    gain.gain.exponentialRampToValueAtTime(0.28, now + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.09);

    osc.connect(gain).connect(ctx.destination);
    osc.start(now);
    osc.stop(now + 0.1);
  } catch {
    // ignore
  }
}

/**
 * Phát âm thanh chúc mừng chiến thắng (Triumphant Victory Fanfare)
 * khi ván đấu kết thúc (chiếu bí hoặc có người thắng).
 */
export function playVictorySound() {
  const ctx = getAudioContext();
  if (!ctx) return;
  try {
    const now = ctx.currentTime;

    // 1. Tiếng chiêng/trống dội nhẹ ở đầu (Decisive strike)
    const strikeOsc = ctx.createOscillator();
    const strikeGain = ctx.createGain();
    strikeOsc.type = "triangle";
    strikeOsc.frequency.setValueAtTime(130.81, now); // C3
    strikeOsc.frequency.exponentialRampToValueAtTime(65.41, now + 0.25);
    strikeGain.gain.setValueAtTime(0.001, now);
    strikeGain.gain.exponentialRampToValueAtTime(0.25, now + 0.015);
    strikeGain.gain.exponentialRampToValueAtTime(0.0001, now + 0.35);
    strikeOsc.connect(strikeGain).connect(ctx.destination);
    strikeOsc.start(now);
    strikeOsc.stop(now + 0.38);

    // 2. Chuỗi nốt kèn đồng / chuông khải hoàn (C5 -> E5 -> G5)
    const arpeggio = [
      { f: 523.25, t: 0.08, d: 0.14, g: 0.18 }, // C5
      { f: 659.25, t: 0.20, d: 0.14, g: 0.20 }, // E5
      { f: 783.99, t: 0.32, d: 0.16, g: 0.22 }, // G5
    ];

    arpeggio.forEach((n) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "triangle";
      osc.frequency.setValueAtTime(n.f, now + n.t);

      gain.gain.setValueAtTime(0.001, now + n.t);
      gain.gain.exponentialRampToValueAtTime(n.g, now + n.t + 0.015);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + n.t + n.d);

      osc.connect(gain).connect(ctx.destination);
      osc.start(now + n.t);
      osc.stop(now + n.t + n.d + 0.02);
    });

    // 3. Hợp âm ngân vang cực đại ở cao trào (C6 kết hợp E5 + G5 + C5)
    const climaxTime = 0.46;
    const chord = [
      { f: 1046.50, type: "sine" as OscillatorType, g: 0.28, d: 0.9 }, // C6 (ngân cao sáng)
      { f: 783.99, type: "triangle" as OscillatorType, g: 0.18, d: 0.75 }, // G5
      { f: 659.25, type: "triangle" as OscillatorType, g: 0.16, d: 0.75 }, // E5
      { f: 523.25, type: "sine" as OscillatorType, g: 0.16, d: 0.8 }, // C5
    ];

    chord.forEach((n) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = n.type;
      osc.frequency.setValueAtTime(n.f, now + climaxTime);

      gain.gain.setValueAtTime(0.001, now + climaxTime);
      gain.gain.exponentialRampToValueAtTime(n.g, now + climaxTime + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + climaxTime + n.d);

      osc.connect(gain).connect(ctx.destination);
      osc.start(now + climaxTime);
      osc.stop(now + climaxTime + n.d + 0.05);
    });
  } catch (err) {
    console.warn("không phát được âm thanh chiến thắng:", err);
  }
}

/**
 * Phát tiếng xu rơi leng keng khi nhận thưởng, điểm danh hoặc nhận chuyển khoản Xu.
 */
export function playCoinSound() {
  const ctx = getAudioContext();
  if (!ctx) return;
  try {
    const now = ctx.currentTime;
    const notes = [
      { f: 1318.5, t: 0, d: 0.14, g: 0.14 }, // E6
      { f: 1760.0, t: 0.07, d: 0.24, g: 0.16 }, // A6
    ];
    for (const n of notes) {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.setValueAtTime(n.f, now + n.t);
      gain.gain.setValueAtTime(0.001, now + n.t);
      gain.gain.exponentialRampToValueAtTime(n.g, now + n.t + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + n.t + n.d);
      osc.connect(gain).connect(ctx.destination);
      osc.start(now + n.t);
      osc.stop(now + n.t + n.d + 0.02);
    }
  } catch {
    // ignore
  }
}
