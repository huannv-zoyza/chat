// Hình vẽ của Trạm Vũ Trụ trên canvas: nhân vật, bản đồ, trạm làm việc.
// Toạ độ là đơn vị thế giới (cùng hệ với game/station.go).
import type { StStation, StationMap, StRoom } from "./station";

export const INK = "#0b1220"; // viền hoạt hình

/* ---------- màu ---------- */

function hex(c: string): [number, number, number] {
  const n = parseInt(c.slice(1), 16);
  return [n >> 16, (n >> 8) & 255, n & 255];
}

/** Pha màu với trắng (k > 0) hoặc đen (k < 0). */
export function tint(c: string, k: number): string {
  const [r, g, b] = hex(c);
  const t = k > 0 ? 255 : 0;
  const a = Math.abs(k);
  const m = (v: number) => Math.round(v + (t - v) * a);
  return `rgb(${m(r)}, ${m(g)}, ${m(b)})`;
}

export function alpha(c: string, a: number): string {
  const [r, g, b] = hex(c);
  return `rgba(${r}, ${g}, ${b}, ${a})`;
}

/* =========================================================
   Nhân vật: phi hành gia chibi, mũ kính tròn thấy mặt
   ========================================================= */

export type Pose = {
  color: string;
  /** 1: quay phải, -1: quay trái */
  facing?: number;
  /** pha bước chân (radian), tăng khi đang đi */
  step?: number;
  moving?: boolean;
  dead?: boolean;
  ghost?: boolean;
  now?: number;
};

/** Vẽ nhân vật, gốc (0, 0) là điểm giữa hai chân chạm sàn trừ 28 (giữa người). Cao ~70 đơn vị. */
export function drawCharacter(ctx: CanvasRenderingContext2D, p: Pose) {
  const now = p.now ?? 0;
  const f = p.facing ?? 1;
  const step = p.step ?? 0;
  const walk = p.moving ? Math.sin(step) : 0;
  const bob = p.moving ? Math.abs(Math.cos(step)) * -2 : Math.sin(now / 600) * 0.8;
  const c = p.color;
  const dark = tint(c, -0.42);
  const mid = tint(c, -0.15);
  const light = tint(c, 0.35);
  ctx.save();
  ctx.lineJoin = "round";
  ctx.lineCap = "round";

  if (p.dead) {
    drawFallen(ctx, p);
    ctx.restore();
    return;
  }

  // bóng đổ
  if (!p.ghost) {
    ctx.fillStyle = "rgba(0, 0, 0, 0.32)";
    ctx.beginPath();
    ctx.ellipse(0, 31, 21, 6.5, 0, 0, Math.PI * 2);
    ctx.fill();
  }

  ctx.scale(f, 1);
  ctx.translate(0, bob);
  ctx.strokeStyle = INK;
  ctx.lineWidth = 3;

  if (p.ghost) {
    // hồn ma: không chân, đuôi lượn sóng
    ctx.fillStyle = mid;
    ctx.beginPath();
    ctx.moveTo(-15, 6);
    for (let i = 0; i <= 6; i++) {
      const x = -15 + i * 5;
      ctx.lineTo(x, 26 + Math.sin(now / 160 + i) * 3 + (i % 2 ? 4 : 0));
    }
    ctx.lineTo(15, 6);
    ctx.closePath();
    ctx.fill();
    ctx.stroke();
  } else {
    // chân + giày (lệch nhau khi đi)
    for (const side of [-1, 1]) {
      const lift = walk * side * 4;
      const x = side * 7.5;
      ctx.fillStyle = mid;
      ctx.beginPath();
      ctx.roundRect(x - 5.5, 12 + Math.min(0, lift), 11, 13, 4);
      ctx.fill();
      ctx.stroke();
      ctx.fillStyle = "#e9eef7";
      ctx.beginPath();
      ctx.roundRect(x - 7 + (side > 0 ? 1 : 0), 22 + Math.min(0, lift), 14, 8, 4);
      ctx.fill();
      ctx.stroke();
      ctx.fillStyle = "#9fb0c8";
      ctx.fillRect(x - 5, 27 + Math.min(0, lift), 10, 1.6);
    }
  }

  // tay sau
  drawArm(ctx, -1, walk, dark);

  // thân bộ đồ
  const g = ctx.createLinearGradient(-16, -4, 16, 22);
  g.addColorStop(0, light);
  g.addColorStop(0.55, c);
  g.addColorStop(1, mid);
  ctx.fillStyle = g;
  ctx.beginPath();
  ctx.roundRect(-16, -6, 32, 26, [12, 12, 9, 9]);
  ctx.fill();
  ctx.stroke();
  // khoá kéo + túi ngực + đai
  ctx.strokeStyle = alpha("#0b1220", 0.35);
  ctx.lineWidth = 1.6;
  ctx.beginPath();
  ctx.moveTo(1, 0);
  ctx.lineTo(1, 13);
  ctx.stroke();
  ctx.fillStyle = dark;
  ctx.beginPath();
  ctx.roundRect(-15, 12, 30, 5, 2);
  ctx.fill();
  ctx.fillStyle = "#ffd76a";
  ctx.beginPath();
  ctx.roundRect(-3, 11.5, 7, 6, 1.5);
  ctx.fill();
  ctx.fillStyle = "#e9eef7";
  ctx.beginPath();
  ctx.roundRect(5, 1, 7, 6, 1.5);
  ctx.fill();
  ctx.fillStyle = "#5ee1ff";
  ctx.fillRect(6.5, 2.5, 4, 1.4);
  ctx.strokeStyle = INK;
  ctx.lineWidth = 3;

  // tay trước
  drawArm(ctx, 1, walk, c);

  // cổ áo
  ctx.fillStyle = "#e9eef7";
  ctx.beginPath();
  ctx.ellipse(0, -6, 13, 4.5, 0, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();

  // mũ: vỏ theo màu áo
  const hy = -24;
  const hg = ctx.createRadialGradient(-7, hy - 9, 3, 0, hy, 22);
  hg.addColorStop(0, light);
  hg.addColorStop(1, mid);
  ctx.fillStyle = hg;
  ctx.beginPath();
  ctx.arc(0, hy, 20, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  // đèn mũ
  ctx.fillStyle = "#ffe48a";
  ctx.beginPath();
  ctx.roundRect(-21, hy - 6, 5, 9, 2);
  ctx.fill();
  ctx.lineWidth = 2;
  ctx.stroke();
  ctx.lineWidth = 3;

  // kính tròn (lệch về hướng nhìn)
  const gx = 3;
  const glass = ctx.createRadialGradient(gx - 4, hy - 6, 2, gx, hy, 16);
  glass.addColorStop(0, "#2d4a78");
  glass.addColorStop(1, "#101b33");
  ctx.fillStyle = glass;
  ctx.beginPath();
  ctx.arc(gx, hy + 1, 14.5, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();

  // mặt
  ctx.save();
  ctx.beginPath();
  ctx.arc(gx, hy + 1, 13, 0, Math.PI * 2);
  ctx.clip();
  ctx.fillStyle = "#ffd8b5";
  ctx.beginPath();
  ctx.arc(gx + 2, hy + 4, 10.5, 0, Math.PI * 2);
  ctx.fill();
  // tóc mái
  ctx.fillStyle = "#3a2a24";
  ctx.beginPath();
  ctx.ellipse(gx + 1, hy - 8.5, 12, 5, -0.15, 0, Math.PI * 2);
  ctx.fill();
  // mắt (chớp mắt thỉnh thoảng)
  const blink = now > 0 && Math.sin(now / 1300 + c.charCodeAt(2)) > 0.985; // chân dung (now = 0) không chớp
  ctx.fillStyle = "#1b1f2e";
  for (const ex of [gx - 1.5, gx + 6.5]) {
    if (blink) ctx.fillRect(ex - 2.5, hy + 2, 5, 1.6);
    else {
      ctx.beginPath();
      ctx.ellipse(ex, hy + 2.5, 2.6, 3.4, 0, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#fff";
      ctx.beginPath();
      ctx.arc(ex + 0.9, hy + 1.3, 1.1, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#1b1f2e";
    }
  }
  ctx.fillStyle = "rgba(255, 120, 130, 0.5)";
  for (const ex of [gx - 4.5, gx + 10]) {
    ctx.beginPath();
    ctx.ellipse(ex, hy + 6.5, 2.2, 1.4, 0, 0, Math.PI * 2);
    ctx.fill();
  }
  ctx.strokeStyle = "#7a3b2e";
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  ctx.arc(gx + 2.5, hy + 6.5, 2.2, 0.2 * Math.PI, 0.8 * Math.PI);
  ctx.stroke();
  ctx.restore();

  // phản chiếu trên kính
  ctx.strokeStyle = "rgba(255, 255, 255, 0.75)";
  ctx.lineWidth = 2.6;
  ctx.beginPath();
  ctx.arc(gx, hy + 1, 10.5, Math.PI * 1.08, Math.PI * 1.42);
  ctx.stroke();
  ctx.fillStyle = "rgba(255, 255, 255, 0.85)";
  ctx.beginPath();
  ctx.arc(gx + 8, hy - 7, 1.6, 0, Math.PI * 2);
  ctx.fill();
  ctx.restore();
}

function drawArm(ctx: CanvasRenderingContext2D, side: number, walk: number, color: string) {
  const swing = -walk * side * 0.45;
  ctx.save();
  ctx.translate(side * 15, 0);
  ctx.rotate(swing + side * 0.18);
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.roundRect(-5, -2, 10, 17, 5);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = "#e9eef7";
  ctx.beginPath();
  ctx.arc(0, 15, 4.6, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  ctx.restore();
}

/** Người đã chết: nằm nghiêng, mắt chéo, mũ nứt. */
function drawFallen(ctx: CanvasRenderingContext2D, p: Pose) {
  const c = p.color;
  const mid = tint(c, -0.15);
  ctx.fillStyle = "rgba(0, 0, 0, 0.35)";
  ctx.beginPath();
  ctx.ellipse(0, 14, 40, 10, 0, 0, Math.PI * 2);
  ctx.fill();
  ctx.strokeStyle = INK;
  ctx.lineWidth = 3;
  ctx.lineJoin = "round";
  // thân nằm ngang
  ctx.fillStyle = c;
  ctx.beginPath();
  ctx.roundRect(-12, -4, 34, 20, 9);
  ctx.fill();
  ctx.stroke();
  for (const y of [-1, 9]) {
    ctx.fillStyle = mid;
    ctx.beginPath();
    ctx.roundRect(20, y, 14, 9, 4);
    ctx.fill();
    ctx.stroke();
  }
  // mũ
  ctx.fillStyle = tint(c, 0.2);
  ctx.beginPath();
  ctx.arc(-22, 4, 16, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = "#16233f";
  ctx.beginPath();
  ctx.arc(-24, 5, 11, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  ctx.strokeStyle = "#e9eef7";
  ctx.lineWidth = 2;
  for (const ex of [-28, -20]) {
    ctx.beginPath();
    ctx.moveTo(ex - 2.5, 2);
    ctx.lineTo(ex + 2.5, 7);
    ctx.moveTo(ex + 2.5, 2);
    ctx.lineTo(ex - 2.5, 7);
    ctx.stroke();
  }
  // vết nứt kính
  ctx.strokeStyle = "rgba(255,255,255,0.7)";
  ctx.lineWidth = 1.3;
  ctx.beginPath();
  ctx.moveTo(-30, -3);
  ctx.lineTo(-26, 0);
  ctx.lineTo(-28, 3);
  ctx.stroke();
}

/* =========================================================
   Bản đồ
   ========================================================= */

type Theme = { accent: string; floor: string; props?: (ctx: CanvasRenderingContext2D, r: StRoom, now: number) => void };

const FLOOR = "#24324a";
const WALL = "#0f1828";
const WALL_FACE_TOP = "#3e587d";
const WALL_FACE_BOTTOM = "#253651";

function panel(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, fill: string, r = 6) {
  ctx.fillStyle = fill;
  ctx.beginPath();
  ctx.roundRect(x, y, w, h, r);
  ctx.fill();
  ctx.strokeStyle = INK;
  ctx.lineWidth = 3;
  ctx.stroke();
}

function screen(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, color: string, now: number) {
  panel(ctx, x, y, w, h, "#1a2840", 5);
  const g = ctx.createLinearGradient(x, y, x, y + h);
  g.addColorStop(0, alpha(color, 0.9));
  g.addColorStop(1, alpha(color, 0.35));
  ctx.fillStyle = g;
  ctx.fillRect(x + 5, y + 5, w - 10, h - 10);
  ctx.fillStyle = alpha("#ffffff", 0.55);
  const t = (now / 900) % 1;
  for (let i = 0; i < 3; i++) ctx.fillRect(x + 9, y + 9 + i * 7, (w - 22) * (0.4 + ((i * 0.37 + t) % 0.6)), 2.4);
}

function plant(ctx: CanvasRenderingContext2D, x: number, y: number) {
  panel(ctx, x - 14, y, 28, 22, "#8a5a3c", 5);
  ctx.fillStyle = "#3fae6a";
  for (const [dx, dy, r] of [
    [-9, -6, 11],
    [8, -8, 12],
    [0, -18, 12],
  ]) {
    ctx.beginPath();
    ctx.arc(x + dx, y + dy, r, 0, Math.PI * 2);
    ctx.fill();
    ctx.strokeStyle = INK;
    ctx.lineWidth = 2.5;
    ctx.stroke();
  }
}

function crate(ctx: CanvasRenderingContext2D, x: number, y: number, s: number) {
  panel(ctx, x, y, s, s, "#b98a55", 4);
  ctx.strokeStyle = "#7d5631";
  ctx.lineWidth = 3;
  ctx.beginPath();
  ctx.moveTo(x + 6, y + 6);
  ctx.lineTo(x + s - 6, y + s - 6);
  ctx.moveTo(x + s - 6, y + 6);
  ctx.lineTo(x + 6, y + s - 6);
  ctx.stroke();
}

const THEMES: Record<string, Theme> = {
  hub: {
    accent: "#ffb648",
    floor: "#26344d",
    props: (ctx, r) => {
      plant(ctx, r.x + 40, r.y + r.h - 50);
      plant(ctx, r.x + r.w - 40, r.y + r.h - 50);
    },
  },
  bridge: {
    accent: "#5ec8ff",
    floor: "#223552",
    props: (ctx, r, now) => {
      // cửa sổ nhìn ra vũ trụ trên tường bắc
      ctx.fillStyle = "#050a18";
      ctx.beginPath();
      ctx.roundRect(r.x + 60, r.y + 4, r.w - 120, 26, 6);
      ctx.fill();
      ctx.fillStyle = "#cfe2ff";
      for (let i = 0; i < 14; i++) ctx.fillRect(r.x + 70 + ((i * 97) % (r.w - 140)), r.y + 9 + ((i * 7) % 16), 2, 2);
      for (const x of [r.x + 40, r.x + 130, r.x + r.w - 120]) screen(ctx, x, r.y + 48, 80, 46, "#5ec8ff", now + x);
    },
  },
  engine: {
    accent: "#ff8a3d",
    floor: "#2b2f45",
    props: (ctx, r, now) => {
      for (const y of [r.y + 200, r.y + r.h - 140]) {
        panel(ctx, r.x + 14, y, 120, 120, "#3a4660", 18);
        const glow = 0.55 + 0.35 * Math.sin(now / 260 + y);
        const g = ctx.createRadialGradient(r.x + 74, y + 60, 6, r.x + 74, y + 60, 46);
        g.addColorStop(0, `rgba(255, 190, 90, ${glow})`);
        g.addColorStop(1, "rgba(255, 110, 40, 0.15)");
        ctx.fillStyle = g;
        ctx.beginPath();
        ctx.arc(r.x + 74, y + 60, 40, 0, Math.PI * 2);
        ctx.fill();
        ctx.strokeStyle = INK;
        ctx.lineWidth = 3;
        ctx.stroke();
      }
    },
  },
  medbay: {
    accent: "#5fe0b5",
    floor: "#22394a",
    props: (ctx, r) => {
      for (const x of [r.x + r.w - 150, r.x + r.w - 270]) {
        panel(ctx, x, r.y + r.h - 130, 90, 110, "#dfe7f2", 10);
        panel(ctx, x + 12, r.y + r.h - 122, 66, 26, "#a8c8f0", 8);
      }
      ctx.fillStyle = alpha("#5fe0b5", 0.25);
      ctx.fillRect(r.x + r.w / 2 - 40, r.y + r.h / 2 - 12, 80, 24);
      ctx.fillRect(r.x + r.w / 2 - 12, r.y + r.h / 2 - 40, 24, 80);
    },
  },
  power: {
    accent: "#ffd23f",
    floor: "#2a3047",
    props: (ctx, r, now) => {
      for (let i = 0; i < 4; i++) {
        const x = r.x + 160 + i * 70;
        panel(ctx, x, r.y + 40, 54, 70, "#3a4660", 5);
        for (let j = 0; j < 3; j++) {
          ctx.fillStyle = Math.sin(now / 200 + i * 3 + j) > 0 ? "#ffd23f" : "#5a4a1a";
          ctx.fillRect(x + 10 + j * 13, r.y + 52, 7, 7);
        }
      }
      ctx.strokeStyle = "#151d2c";
      ctx.lineWidth = 6;
      ctx.beginPath();
      ctx.moveTo(r.x + 190, r.y + 110);
      ctx.bezierCurveTo(r.x + 200, r.y + 220, r.x + 330, r.y + 180, r.x + 360, r.y + 300);
      ctx.stroke();
    },
  },
  lab: {
    accent: "#b48cff",
    floor: "#2a2c4a",
    props: (ctx, r) => {
      panel(ctx, r.x + 140, r.y + r.h - 120, 220, 70, "#3a4660", 10);
      const colors = ["#b48cff", "#5fe0b5", "#ff7eb6", "#ffd23f"];
      colors.forEach((c, i) => {
        ctx.fillStyle = alpha(c, 0.85);
        ctx.beginPath();
        ctx.roundRect(r.x + 160 + i * 50, r.y + r.h - 108, 26, 40, [4, 4, 12, 12]);
        ctx.fill();
        ctx.strokeStyle = INK;
        ctx.lineWidth = 2.5;
        ctx.stroke();
      });
      panel(ctx, r.x + r.w - 110, r.y + 250, 70, 150, alpha("#b48cff", 0.35), 35);
    },
  },
  storage: {
    accent: "#e6b06e",
    floor: "#2b3044",
    props: (ctx, r) => {
      crate(ctx, r.x + r.w - 100, r.y + 50, 70);
      crate(ctx, r.x + r.w - 165, r.y + 64, 56);
      crate(ctx, r.x + 30, r.y + r.h - 120, 80);
      crate(ctx, r.x + 115, r.y + r.h - 80, 50);
    },
  },
  oxygen: {
    accent: "#56e0e6",
    floor: "#21384a",
    props: (ctx, r) => {
      for (let i = 0; i < 3; i++) {
        panel(ctx, r.x + 40 + i * 70, r.y + 50, 50, 120, "#9fe8ee", 22);
        ctx.fillStyle = alpha("#ffffff", 0.5);
        ctx.fillRect(r.x + 52 + i * 70, r.y + 66, 6, 80);
      }
      plant(ctx, r.x + r.w - 60, r.y + r.h - 50);
      plant(ctx, r.x + r.w - 120, r.y + r.h - 50);
    },
  },
  comms: {
    accent: "#ff7eb6",
    floor: "#2c2d47",
    props: (ctx, r, now) => {
      screen(ctx, r.x + 50, r.y + 50, 120, 70, "#ff7eb6", now);
      screen(ctx, r.x + 180, r.y + 50, 90, 70, "#5ec8ff", now + 500);
      // chảo ăng-ten
      const cx = r.x + r.w - 110;
      const cy = r.y + r.h - 110;
      panel(ctx, cx - 10, cy, 20, 60, "#3a4660", 4);
      ctx.fillStyle = "#dfe7f2";
      ctx.beginPath();
      ctx.ellipse(cx, cy, 52, 26, -0.4 + Math.sin(now / 2000) * 0.15, 0, Math.PI * 2);
      ctx.fill();
      ctx.strokeStyle = INK;
      ctx.lineWidth = 3;
      ctx.stroke();
    },
  },
};

export function roomAccent(id: string): string {
  return THEMES[id]?.accent ?? "#8fb3e8";
}

type Seg = { x: number; y: number; w: number; h: number };

/** Phần hành lang nằm ngoài các phòng (để vẽ mặt tường không chồng vào trong phòng). */
function hallOpen(map: StationMap, h: Seg): Seg {
  let { x, y, w, h: hh } = h;
  const horizontal = w > hh;
  for (const r of map.rooms) {
    const ox = x < r.x + r.w && x + w > r.x;
    const oy = y < r.y + r.h && y + hh > r.y;
    if (!ox || !oy) continue;
    if (horizontal) {
      if (r.x <= x) {
        const e = r.x + r.w;
        w -= e - x;
        x = e;
      } else w = r.x - x;
    } else if (r.y <= y) {
      const e = r.y + r.h;
      hh -= e - y;
      y = e;
    } else hh = r.y - y;
  }
  return { x, y, w, h: hh };
}

/** Vẽ toàn bộ sàn, tường, đồ đạc. view: vùng thế giới đang hiện (để bỏ qua phòng ngoài màn hình). */
export function drawWorld(ctx: CanvasRenderingContext2D, map: StationMap, view: Seg, now: number, dark: boolean) {
  const vis = (s: Seg, pad = 80) =>
    s.x < view.x + view.w + pad && s.x + s.w > view.x - pad && s.y < view.y + view.h + pad && s.y + s.h > view.y - pad;
  const halls = map.halls.map((h) => ({ x: h.X, y: h.Y, w: h.W, h: h.H }));
  const T = 16; // độ dày tường

  // 1. khối tường bao ngoài
  ctx.fillStyle = WALL;
  for (const h of halls) if (vis(h)) ctx.fillRect(h.x - T, h.y - T, h.w + 2 * T, h.h + 2 * T);
  for (const r of map.rooms) {
    if (!vis(r)) continue;
    ctx.beginPath();
    ctx.roundRect(r.x - T, r.y - T, r.w + 2 * T, r.h + 2 * T, 26);
    ctx.fill();
  }
  ctx.strokeStyle = "#4b6a96";
  ctx.lineWidth = 3;
  for (const h of halls) if (vis(h)) ctx.strokeRect(h.x - T, h.y - T, h.w + 2 * T, h.h + 2 * T);
  for (const r of map.rooms) {
    if (!vis(r)) continue;
    ctx.beginPath();
    ctx.roundRect(r.x - T, r.y - T, r.w + 2 * T, r.h + 2 * T, 26);
    ctx.stroke();
  }

  // 2. sàn phòng + mặt tường bắc (tạo chiều sâu)
  for (const r of map.rooms) {
    if (!vis(r)) continue;
    const th = THEMES[r.id] ?? { accent: "#8fb3e8", floor: FLOOR };
    ctx.fillStyle = th.floor;
    ctx.beginPath();
    ctx.roundRect(r.x, r.y, r.w, r.h, 16);
    ctx.fill();
    floorTiles(ctx, r, alpha("#ffffff", 0.035));
    const face = ctx.createLinearGradient(0, r.y, 0, r.y + 36);
    face.addColorStop(0, WALL_FACE_TOP);
    face.addColorStop(1, WALL_FACE_BOTTOM);
    ctx.fillStyle = face;
    ctx.beginPath();
    ctx.roundRect(r.x, r.y, r.w, 36, [16, 16, 0, 0]);
    ctx.fill();
    // dải đèn màu của phòng chạy dọc chân tường
    ctx.fillStyle = alpha(th.accent, dark ? 0.25 : 0.75);
    ctx.fillRect(r.x + 18, r.y + 34, r.w - 36, 3);
  }

  // 3. sàn hành lang phủ lên để mở cửa giữa phòng và hành lang
  for (const h of halls) {
    if (!vis(h)) continue;
    ctx.fillStyle = FLOOR;
    ctx.fillRect(h.x, h.y, h.w, h.h);
    const o = hallOpen(map, h);
    if (h.w > h.h) {
      const face = ctx.createLinearGradient(0, o.y, 0, o.y + 26);
      face.addColorStop(0, WALL_FACE_TOP);
      face.addColorStop(1, WALL_FACE_BOTTOM);
      ctx.fillStyle = face;
      ctx.fillRect(o.x, o.y, o.w, 26);
      // đèn dẫn đường giữa sàn
      ctx.fillStyle = alpha("#8fd3ff", dark ? 0.1 : 0.28);
      for (let x = o.x + 20; x < o.x + o.w - 10; x += 60) ctx.fillRect(x, o.y + o.h / 2 + 8, 26, 4);
    } else {
      ctx.fillStyle = alpha("#8fd3ff", dark ? 0.1 : 0.28);
      for (let y = o.y + 20; y < o.y + o.h - 10; y += 60) ctx.fillRect(o.x + o.w / 2 - 2, y, 4, 26);
    }
    // vạch cảnh báo ở hai đầu cửa
    hazard(ctx, h, o);
  }

  // 4. ánh đèn trần, tên phòng sơn trên sàn, đồ đạc
  ctx.save();
  ctx.globalCompositeOperation = "lighter";
  for (const r of map.rooms) {
    if (!vis(r)) continue;
    const th = THEMES[r.id];
    const cx = r.x + r.w / 2;
    const cy = r.y + r.h / 2;
    const g = ctx.createRadialGradient(cx, cy, 10, cx, cy, Math.max(r.w, r.h) * 0.6);
    g.addColorStop(0, alpha(th?.accent ?? "#8fb3e8", dark ? 0.04 : 0.13));
    g.addColorStop(1, "rgba(0,0,0,0)");
    ctx.fillStyle = g;
    ctx.fillRect(r.x, r.y, r.w, r.h);
  }
  ctx.restore();

  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.font = "700 34px 'Chakra Petch', 'Be Vietnam Pro', sans-serif";
  for (const r of map.rooms) {
    if (!vis(r)) continue;
    ctx.fillStyle = alpha(THEMES[r.id]?.accent ?? "#8fb3e8", 0.2);
    ctx.fillText(r.name.toLocaleUpperCase("vi"), r.x + r.w / 2, r.y + r.h - 40);
  }
  for (const r of map.rooms) if (vis(r)) THEMES[r.id]?.props?.(ctx, r, now);
}

function floorTiles(ctx: CanvasRenderingContext2D, r: Seg, color: string) {
  ctx.strokeStyle = color;
  ctx.lineWidth = 2;
  ctx.beginPath();
  for (let x = r.x + 60; x < r.x + r.w - 10; x += 60) {
    ctx.moveTo(x, r.y + 36);
    ctx.lineTo(x, r.y + r.h - 4);
  }
  for (let y = r.y + 36 + 60; y < r.y + r.h - 10; y += 60) {
    ctx.moveTo(r.x + 4, y);
    ctx.lineTo(r.x + r.w - 4, y);
  }
  ctx.stroke();
}

function hazard(ctx: CanvasRenderingContext2D, h: Seg, o: Seg) {
  if (o.w <= 0 || o.h <= 0) return;
  const ends =
    h.w > h.h
      ? [o.x, o.x + o.w - 10].map((x) => ({ x, y: o.y, w: 10, h: o.h }))
      : [o.y, o.y + o.h - 10].map((y) => ({ x: o.x, y, w: o.w, h: 10 }));
  ctx.fillStyle = "rgba(255, 196, 70, 0.5)";
  for (const e of ends) {
    ctx.save();
    ctx.beginPath();
    ctx.rect(e.x, e.y, e.w, e.h);
    ctx.clip();
    for (let k = -120; k < 120; k += 16) {
      ctx.beginPath();
      ctx.moveTo(e.x + k, e.y);
      ctx.lineTo(e.x + k + 8, e.y);
      ctx.lineTo(e.x + k + 108, e.y + 100);
      ctx.lineTo(e.x + k + 100, e.y + 100);
      ctx.fill();
    }
    ctx.restore();
  }
}

/* ---------- chuông khẩn cấp ---------- */

export function drawButton(ctx: CanvasRenderingContext2D, x: number, y: number, now: number, active: boolean) {
  // bàn tròn
  ctx.fillStyle = "rgba(0,0,0,0.3)";
  ctx.beginPath();
  ctx.ellipse(x, y + 18, 92, 30, 0, 0, Math.PI * 2);
  ctx.fill();
  const g = ctx.createLinearGradient(x, y - 70, x, y + 60);
  g.addColorStop(0, "#5b7399");
  g.addColorStop(1, "#33445f");
  ctx.fillStyle = g;
  ctx.beginPath();
  ctx.ellipse(x, y, 88, 62, 0, 0, Math.PI * 2);
  ctx.fill();
  ctx.strokeStyle = INK;
  ctx.lineWidth = 3.5;
  ctx.stroke();
  ctx.fillStyle = "#2a3850";
  ctx.beginPath();
  ctx.ellipse(x, y - 4, 62, 42, 0, 0, Math.PI * 2);
  ctx.fill();
  // nút đỏ dưới vòm kính
  const pulse = active ? 0.5 + 0.5 * Math.sin(now / 280) : 0.2;
  ctx.fillStyle = `rgba(255, 70, 90, ${0.18 + pulse * 0.25})`;
  ctx.beginPath();
  ctx.arc(x, y - 10, 34, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = "#c22a43";
  ctx.beginPath();
  ctx.ellipse(x, y - 4, 22, 14, 0, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = "#ff4d6d";
  ctx.beginPath();
  ctx.ellipse(x, y - 10, 22, 14, 0, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = "rgba(255,255,255,0.55)";
  ctx.beginPath();
  ctx.ellipse(x - 8, y - 14, 7, 3, -0.3, 0, Math.PI * 2);
  ctx.fill();
  ctx.strokeStyle = "rgba(200, 230, 255, 0.55)";
  ctx.lineWidth = 2.5;
  ctx.beginPath();
  ctx.arc(x, y - 6, 30, Math.PI * 1.05, Math.PI * 1.95);
  ctx.stroke();
}

/* ---------- trạm làm việc ---------- */

export function drawStation(
  ctx: CanvasRenderingContext2D,
  s: StStation,
  now: number,
  state: "idle" | "task" | "alarm",
  accent: string,
) {
  const { x, y } = s;
  const hot = state !== "idle";
  const glowColor = state === "alarm" ? "#ff4d6d" : "#ffc94d";
  const pulse = 0.5 + 0.5 * Math.sin(now / 260);
  if (hot) {
    const g = ctx.createRadialGradient(x, y, 10, x, y, 70);
    g.addColorStop(0, alpha(glowColor, 0.35 + pulse * 0.2));
    g.addColorStop(1, alpha(glowColor, 0));
    ctx.fillStyle = g;
    ctx.beginPath();
    ctx.arc(x, y, 70, 0, Math.PI * 2);
    ctx.fill();
  }
  // bệ máy
  ctx.fillStyle = "rgba(0,0,0,0.3)";
  ctx.beginPath();
  ctx.ellipse(x, y + 26, 30, 8, 0, 0, Math.PI * 2);
  ctx.fill();
  panel(ctx, x - 26, y - 22, 52, 46, "#3a4a66", 9);
  panel(ctx, x - 19, y - 30, 38, 26, "#16223a", 6);
  ctx.fillStyle = alpha(hot ? glowColor : accent, hot ? 0.9 : 0.55);
  ctx.fillRect(x - 14, y - 25, 28, 16);
  glyph(ctx, s.kind, x, y + 6);
  if (hot) {
    // mũi tên nhún nhảy phía trên
    const by = y - 52 - Math.abs(Math.sin(now / 220)) * 8;
    ctx.fillStyle = glowColor;
    ctx.strokeStyle = INK;
    ctx.lineWidth = 2.5;
    ctx.beginPath();
    ctx.moveTo(x - 10, by - 10);
    ctx.lineTo(x + 10, by - 10);
    ctx.lineTo(x, by + 2);
    ctx.closePath();
    ctx.fill();
    ctx.stroke();
  }
}

function glyph(ctx: CanvasRenderingContext2D, kind: StStation["kind"], x: number, y: number) {
  ctx.strokeStyle = "#dfe9f8";
  ctx.fillStyle = "#dfe9f8";
  ctx.lineWidth = 2.6;
  ctx.beginPath();
  switch (kind) {
    case "wires":
      ctx.moveTo(x - 12, y - 4);
      ctx.bezierCurveTo(x - 4, y - 4, x + 4, y + 6, x + 12, y + 6);
      ctx.moveTo(x - 12, y + 6);
      ctx.bezierCurveTo(x - 4, y + 6, x + 4, y - 4, x + 12, y - 4);
      ctx.stroke();
      break;
    case "hold":
      for (let i = 0; i < 4; i++) ctx.rect(x - 12 + i * 6.5, y + 6 - i * 3, 4, 3 + i * 3);
      ctx.fill();
      break;
    case "code":
      for (let i = 0; i < 3; i++) for (let j = 0; j < 2; j++) ctx.rect(x - 10 + i * 8, y - 4 + j * 7, 4, 4);
      ctx.fill();
      break;
    case "align":
      ctx.moveTo(x - 13, y + 1);
      ctx.lineTo(x + 13, y + 1);
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(x + 3, y + 1, 4, 0, Math.PI * 2);
      ctx.fill();
      break;
    case "scan":
      ctx.arc(x, y + 1, 8, 0, Math.PI * 2);
      ctx.moveTo(x + 3, y + 1);
      ctx.arc(x, y + 1, 3, 0, Math.PI * 2);
      ctx.stroke();
      break;
    case "fix":
      ctx.moveTo(x - 9, y + 8);
      ctx.lineTo(x + 5, y - 6);
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(x + 7, y - 7, 5, 0, Math.PI * 2);
      ctx.stroke();
      break;
  }
}

/* ---------- nền sao ---------- */

export type Star = { x: number; y: number; r: number; a: number };

export function makeStars(n: number): Star[] {
  return Array.from({ length: n }, () => ({
    x: Math.random(),
    y: Math.random(),
    r: Math.random() * 1.6 + 0.4,
    a: Math.random() * 0.6 + 0.3,
  }));
}

export function drawSpace(ctx: CanvasRenderingContext2D, W: number, H: number, ox: number, oy: number, stars: Star[]) {
  const bg = ctx.createRadialGradient(W * 0.7, H * 0.2, 0, W * 0.5, H * 0.5, Math.max(W, H));
  bg.addColorStop(0, "#1a1440");
  bg.addColorStop(0.45, "#0a1028");
  bg.addColorStop(1, "#040611");
  ctx.fillStyle = bg;
  ctx.fillRect(0, 0, W, H);
  for (const s of stars) {
    const x = (((s.x * W - ox * s.r * 0.06) % W) + W) % W;
    const y = (((s.y * H - oy * s.r * 0.06) % H) + H) % H;
    ctx.fillStyle = `rgba(220, 232, 255, ${s.a})`;
    ctx.fillRect(x, y, s.r, s.r);
  }
}
