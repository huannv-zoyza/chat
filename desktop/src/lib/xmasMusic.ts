import { getAudioContext } from "./sound";
import { load, save } from "./storage";

/*
 * Nhạc Noel cho nông trại: giai điệu Jingle Bells (dân gian, không bản quyền) bằng tiếng chuông tổng hợp
 * WebAudio, nhỏ, lặp lại. Mặc định tắt; người chơi bật bằng nút 🎵, nhớ lựa chọn.
 */

const PREF = "farmXmasMusic";
export const getXmasMusicPref = () => load<boolean>(PREF, false);
export const setXmasMusicPref = (v: boolean) => save(PREF, v);

// [nốt MIDI, số phách] — 0 là lặng
const E = 64, G = 67, C = 60, D = 62, F = 65;
const TUNE: [number, number][] = [
  [E, 1], [E, 1], [E, 2], [E, 1], [E, 1], [E, 2], [E, 1], [G, 1], [C, 1.5], [D, 0.5], [E, 4],
  [F, 1], [F, 1], [F, 1.5], [F, 0.5], [F, 1], [E, 1], [E, 1], [E, 0.5], [E, 0.5],
  [E, 1], [D, 1], [D, 1], [E, 1], [D, 2], [G, 2],
  [E, 1], [E, 1], [E, 2], [E, 1], [E, 1], [E, 2], [E, 1], [G, 1], [C, 1.5], [D, 0.5], [E, 4],
  [F, 1], [F, 1], [F, 1.5], [F, 0.5], [F, 1], [E, 1], [E, 1], [E, 0.5], [E, 0.5],
  [G, 1], [G, 1], [F, 1], [D, 1], [C, 4], [0, 4],
];
const BEAT = 0.3; // giây mỗi phách
const VOLUME = 0.05;

const hz = (midi: number) => 440 * 2 ** ((midi - 69) / 12);

/** Tiếng chuông: sóng sin + bồi âm, tắt dần. */
function bell(ctx: AudioContext, out: AudioNode, midi: number, at: number, len: number) {
  for (const [mul, gain] of [
    [1, 1],
    [2, 0.35],
    [3.01, 0.15],
  ] as const) {
    const o = ctx.createOscillator();
    const g = ctx.createGain();
    o.type = "sine";
    o.frequency.value = hz(midi + 12) * mul;
    g.gain.setValueAtTime(0.0001, at);
    g.gain.exponentialRampToValueAtTime(gain, at + 0.01);
    g.gain.exponentialRampToValueAtTime(0.0001, at + Math.max(0.4, len * 1.6));
    o.connect(g).connect(out);
    o.start(at);
    o.stop(at + Math.max(0.5, len * 1.7));
  }
}

let master: GainNode | null = null;
let timer: number | null = null;

/** Phát lặp giai điệu tới khi stopXmasMusic. */
export function startXmasMusic() {
  if (timer !== null) return;
  const ctx = getAudioContext();
  if (!ctx) return;
  master = ctx.createGain();
  master.gain.value = VOLUME;
  master.connect(ctx.destination);
  const loopLen = TUNE.reduce((s, [, b]) => s + b, 0) * BEAT;
  const play = () => {
    if (!master) return;
    let t = ctx.currentTime + 0.1;
    for (const [n, b] of TUNE) {
      if (n) bell(ctx, master, n, t, b * BEAT);
      t += b * BEAT;
    }
  };
  play();
  timer = window.setInterval(play, loopLen * 1000);
}

export function stopXmasMusic() {
  if (timer !== null) window.clearInterval(timer);
  timer = null;
  if (master) {
    const m = master;
    master = null;
    try {
      m.gain.setTargetAtTime(0, m.context.currentTime, 0.05); // tắt êm, rồi ngắt
      window.setTimeout(() => m.disconnect(), 300);
    } catch {
      m.disconnect();
    }
  }
}
