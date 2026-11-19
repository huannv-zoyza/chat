// Trạm Vũ Trụ: kiểu dữ liệu (khớp game/station.go) và kho trạng thái màn chơi.
// Frame tới ~15 lần/giây nên không đi qua reducer chung (sẽ vẽ lại cả app);
// màn chơi tự đăng ký nhận qua stationStore.

export type Rect = { X: number; Y: number; W: number; H: number };
export type StRoom = { id: string; name: string; x: number; y: number; w: number; h: number };
export type StStation = {
  id: string;
  name: string;
  room: string;
  kind: "wires" | "hold" | "code" | "align" | "scan" | "fix";
  x: number;
  y: number;
};
export type StationMap = {
  w: number;
  h: number;
  rooms: StRoom[];
  halls: Rect[];
  stations: StStation[];
  button: [number, number];
  spawn: [number, number];
  radius: number;
  speed: number;
};

export type StTask = { station: string; done: boolean };
export type StFramePlayer = { id: number; x: number; y: number; color: number; ghost?: boolean; traitor?: boolean };
export type StChat = { from: number; text: string; dead?: boolean };
export type StMeeting = {
  caller: number;
  body?: number;
  vote_in: number;
  voted: number[];
  my_vote?: number;
  chat: StChat[];
  alive: number[];
  ejected?: number;
  tie?: boolean;
  votes?: Record<string, number>;
  was_traitor?: boolean;
};
export type StFrame = {
  phase: "intro" | "play" | "meeting" | "eject" | "ended" | "";
  ends_in?: number;
  me: {
    x: number;
    y: number;
    color: number;
    traitor: boolean;
    ghost: boolean;
    correction: number;
    tasks: StTask[];
    using?: string;
    kill_in: number;
    emergencies: number;
    vision: number;
  };
  players: StFramePlayer[];
  bodies: { id: number; x: number; y: number }[];
  task_done: number;
  task_all: number;
  sab?: "power" | "oxygen";
  sab_in?: number;
  sab_ready?: number;
  meeting?: StMeeting;
  allies?: number[];
  colors: Record<string, number>;
};

/** Thẻ trong kênh chat (phần công khai của View). */
export type StationState = {
  phase?: string;
  task_done: number;
  task_all: number;
  dead: number[];
  traitors?: number[];
  colors: Record<string, number>;
  meetings: number;
  traitor_count: number;
  opts: {
    traitors: number;
    kill_cooldown: number;
    tasks: number;
    discuss: number;
    vote: number;
    emergencies: number;
    hide_ejects: boolean;
  };
};

/** 12 màu áo phi hành gia (đủ tương phản trên nền tối). */
export const ST_COLORS = [
  { fill: "#e5484d", name: "Đỏ" },
  { fill: "#3e8ef7", name: "Lam" },
  { fill: "#2fbf71", name: "Lục" },
  { fill: "#f5b82e", name: "Vàng" },
  { fill: "#a46bf5", name: "Tím" },
  { fill: "#f2f2f2", name: "Trắng" },
  { fill: "#ff8a3d", name: "Cam" },
  { fill: "#ff6fb5", name: "Hồng" },
  { fill: "#3fd0d4", name: "Ngọc" },
  { fill: "#8a5a3c", name: "Nâu" },
  { fill: "#b5e655", name: "Chanh" },
  { fill: "#6b7a8f", name: "Xám" },
];

export const RANGE = { use: 120, kill: 120, report: 160, button: 130 };

function inRect(x: number, y: number, r: { x: number; y: number; w: number; h: number }, inset: number) {
  return x >= r.x + inset && x <= r.x + r.w - inset && y >= r.y + inset && y <= r.y + r.h - inset;
}

/** Giống walkable() bên Go: hình tròn của người chơi nằm trọn trong phòng hoặc hành lang. */
export function walkable(m: StationMap, x: number, y: number): boolean {
  const r = m.radius;
  for (const room of m.rooms) if (inRect(x, y, room, r)) return true;
  for (const h of m.halls) if (inRect(x, y, { x: h.X, y: h.Y, w: h.W, h: h.H }, r)) return true;
  return false;
}

export function roomOf(m: StationMap, x: number, y: number): StRoom | undefined {
  return m.rooms.find((room) => inRect(x, y, room, 0));
}

export function roomAt(m: StationMap, x: number, y: number): string {
  return roomOf(m, x, y)?.name ?? "Hành lang";
}

/* ---------- Tìm đường (bấm vào bản đồ / nhiệm vụ là tự đi tới) ---------- */

export type Pt = { x: number; y: number };
const CELL = 20;
type Grid = { cols: number; rows: number; open: Uint8Array };
const grids = new WeakMap<StationMap, Grid>();

function gridOf(m: StationMap): Grid {
  let g = grids.get(m);
  if (g) return g;
  const cols = Math.ceil(m.w / CELL);
  const rows = Math.ceil(m.h / CELL);
  const open = new Uint8Array(cols * rows);
  for (let j = 0; j < rows; j++)
    for (let i = 0; i < cols; i++) open[j * cols + i] = walkable(m, (i + 0.5) * CELL, (j + 0.5) * CELL) ? 1 : 0;
  g = { cols, rows, open };
  grids.set(m, g);
  return g;
}

/** Đi thẳng từ a tới b được trọn đường (lấy mẫu mỗi 8 đơn vị). */
export function clearLine(m: StationMap, a: Pt, b: Pt): boolean {
  const n = Math.max(1, Math.ceil(Math.hypot(b.x - a.x, b.y - a.y) / 8));
  for (let k = 1; k <= n; k++) {
    const t = k / n;
    if (!walkable(m, a.x + (b.x - a.x) * t, a.y + (b.y - a.y) * t)) return false;
  }
  return true;
}

/** Ô đi được gần (x, y) nhất, tìm theo vòng vuông lan dần ra. */
function nearestCell(g: Grid, x: number, y: number, maxR: number): number {
  const ci = Math.floor(x / CELL);
  const cj = Math.floor(y / CELL);
  for (let r = 0; r <= maxR; r++) {
    let best = -1;
    let bestD = Infinity;
    for (let j = cj - r; j <= cj + r; j++)
      for (let i = ci - r; i <= ci + r; i++) {
        if (Math.max(Math.abs(i - ci), Math.abs(j - cj)) !== r) continue;
        if (i < 0 || j < 0 || i >= g.cols || j >= g.rows || !g.open[j * g.cols + i]) continue;
        const d = Math.hypot((i + 0.5) * CELL - x, (j + 0.5) * CELL - y);
        if (d < bestD) {
          bestD = d;
          best = j * g.cols + i;
        }
      }
    if (best >= 0) return best;
  }
  return -1;
}

/** Điểm đi được gần (x, y) nhất (bấm trúng tường thì đi tới sát tường). */
export function snapWalkable(m: StationMap, x: number, y: number): Pt | null {
  if (walkable(m, x, y)) return { x, y };
  const g = gridOf(m);
  const c = nearestCell(g, x, y, 15);
  return c < 0 ? null : { x: ((c % g.cols) + 0.5) * CELL, y: (Math.floor(c / g.cols) + 0.5) * CELL };
}

/** Đường đi từ a tới b (các điểm rẽ, không gồm a). null nếu không tới được. */
export function findPath(m: StationMap, a: Pt, b: Pt): Pt[] | null {
  if (walkable(m, b.x, b.y) && clearLine(m, a, b)) return [b];
  const g = gridOf(m);
  const start = nearestCell(g, a.x, a.y, 3);
  const goal = nearestCell(g, b.x, b.y, 15);
  if (start < 0 || goal < 0) return null;
  // BFS 8 hướng (không cắt góc tường), sau đó làm mượt bằng đường thẳng
  const prev = new Int32Array(g.cols * g.rows).fill(-1);
  const queue = new Int32Array(g.cols * g.rows);
  let head = 0;
  let tail = 0;
  prev[start] = start;
  queue[tail++] = start;
  while (head < tail) {
    const c = queue[head++];
    if (c === goal) break;
    const ci = c % g.cols;
    const cj = Math.floor(c / g.cols);
    for (let dj = -1; dj <= 1; dj++)
      for (let di = -1; di <= 1; di++) {
        if (!di && !dj) continue;
        const i = ci + di;
        const j = cj + dj;
        if (i < 0 || j < 0 || i >= g.cols || j >= g.rows) continue;
        const n = j * g.cols + i;
        if (!g.open[n] || prev[n] >= 0) continue;
        if (di && dj && (!g.open[cj * g.cols + i] || !g.open[j * g.cols + ci])) continue;
        prev[n] = c;
        queue[tail++] = n;
      }
  }
  if (prev[goal] < 0) return null;
  const pts: Pt[] = [];
  for (let c = goal; c !== start; c = prev[c])
    pts.push({ x: ((c % g.cols) + 0.5) * CELL, y: (Math.floor(c / g.cols) + 0.5) * CELL });
  pts.reverse();
  if (walkable(m, b.x, b.y) && (pts.length === 0 || clearLine(m, pts[pts.length - 1], b))) pts.push(b);
  const out: Pt[] = [];
  let from = a;
  let k = 0;
  while (k < pts.length) {
    let far = k;
    while (far + 1 < pts.length && clearLine(m, from, pts[far + 1])) far++;
    out.push(pts[far]);
    from = pts[far];
    k = far + 1;
  }
  return out;
}

type Snap = { t: number; x: number; y: number };

/** Kho trạng thái màn chơi: ván đang mở, bản đồ, frame mới nhất, lịch sử vị trí để nội suy. */
class StationStore {
  openId: number | null = null;
  map: StationMap | null = null;
  frame: StFrame | null = null;
  frameAt = 0;
  /** Vị trí gần đây của người khác (để vẽ mượt giữa hai frame). */
  trails = new Map<number, Snap[]>();
  private version = 0;
  private listeners = new Set<() => void>();
  private send: ((req: Record<string, unknown>) => boolean) | null = null;

  setSender(fn: ((req: Record<string, unknown>) => boolean) | null) {
    this.send = fn;
  }

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => void this.listeners.delete(fn);
  };
  getVersion = () => this.version;

  private changed() {
    this.version++;
    for (const l of this.listeners) l();
  }

  open(messageId: number) {
    if (this.openId === messageId) return;
    if (this.openId !== null) this.send?.({ type: "game.unwatch", message_id: this.openId });
    this.openId = messageId;
    this.map = null;
    this.frame = null;
    this.trails.clear();
    this.send?.({ type: "game.watch", message_id: messageId });
    this.changed();
  }

  close() {
    if (this.openId !== null) this.send?.({ type: "game.unwatch", message_id: this.openId });
    this.openId = null;
    this.frame = null;
    this.changed();
  }

  /** Sau khi kết nối lại: đăng ký nhận frame lại. */
  resubscribe() {
    if (this.openId !== null) this.send?.({ type: "game.watch", message_id: this.openId });
  }

  sendPos(x: number, y: number) {
    if (this.openId !== null) this.send?.({ type: "game.pos", message_id: this.openId, data: { x, y } });
  }

  onMap(messageId: number, map: StationMap) {
    if (messageId !== this.openId) return;
    this.map = map;
    this.changed();
  }

  onFrame(messageId: number, frame: StFrame) {
    if (messageId !== this.openId) return;
    const now = performance.now();
    const seen = new Set<number>();
    for (const p of frame.players) {
      seen.add(p.id);
      const list = this.trails.get(p.id) ?? [];
      list.push({ t: now, x: p.x, y: p.y });
      if (list.length > 6) list.shift();
      this.trails.set(p.id, list);
    }
    for (const id of [...this.trails.keys()]) if (!seen.has(id)) this.trails.delete(id);
    this.frame = frame;
    this.frameAt = now;
    this.changed();
  }

  /** Vị trí hiển thị của người khác: lùi 110ms và nội suy giữa hai frame quanh thời điểm đó. */
  position(id: number, now: number): { x: number; y: number } | null {
    const list = this.trails.get(id);
    if (!list || list.length === 0) return null;
    const t = now - 110;
    for (let i = list.length - 1; i > 0; i--) {
      const a = list[i - 1];
      const b = list[i];
      if (a.t <= t && t <= b.t) {
        const k = (t - a.t) / Math.max(1, b.t - a.t);
        return { x: a.x + (b.x - a.x) * k, y: a.y + (b.y - a.y) * k };
      }
    }
    const last = list[list.length - 1];
    return { x: last.x, y: last.y };
  }
}

export const stationStore = new StationStore();
