import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type FormEvent,
  type MutableRefObject,
} from "react";
import { createPortal } from "react-dom";
import { ApiError, type GameView } from "../lib/api";
import {
  RANGE,
  ST_COLORS,
  roomAt,
  stationStore,
  walkable,
  type StFrame,
  type StStation,
  type StationMap,
  type StationState,
} from "../lib/station";
import {
  drawButton,
  drawCharacter,
  drawSpace,
  drawStation,
  drawWorld,
  makeStars,
  roomAccent,
  type Star,
} from "../lib/stationArt";
import { isTouchDevice } from "../lib/useMedia";
import "@fontsource/chakra-petch/vietnamese-600.css";
import "@fontsource/chakra-petch/vietnamese-700.css";
import "@fontsource/chakra-petch/latin-600.css";
import "@fontsource/chakra-petch/latin-700.css";
import { findMessage } from "../state/chat";
import { useChat } from "../state/ChatProvider";

/* =========================================================
   Màn chơi Trạm Vũ Trụ (toàn màn hình)
   ========================================================= */

const MIN_TIME: Record<StStation["kind"], number> = {
  wires: 1500,
  hold: 3000,
  code: 2000,
  align: 1000,
  scan: 6000,
  fix: 2500,
};

const colorOf = (i: number | undefined) => ST_COLORS[(i ?? 0) % ST_COLORS.length].fill;

function useStation() {
  useSyncExternalStore(stationStore.subscribe, stationStore.getVersion);
  return stationStore;
}

type Near = {
  use?: { kind: "station"; station: StStation } | { kind: "button" };
  report?: number;
  kill?: number;
};

export function StationOverlay() {
  const store = useStation();
  const { state, gameAction } = useChat();
  const id = store.openId;
  const msg = id !== null ? findMessage(state, id) : undefined;
  const game = msg?.game;
  if (id === null) return null;
  return createPortal(
    <StationScreen
      key={id}
      game={game}
      meId={state.me.id}
      act={(action, data) => gameAction(id, action, data)}
      onClose={() => stationStore.close()}
    />,
    document.body,
  );
}

function StationScreen({
  game,
  meId,
  act,
  onClose,
}: {
  game: GameView | undefined;
  meId: number;
  act: (action: string, data?: unknown) => Promise<void>;
  onClose: () => void;
}) {
  const store = useStation();
  const map = store.map;
  const frame = store.frame;
  const [near, setNear] = useState<Near>({});
  const [task, setTask] = useState<StStation | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [showMap, setShowMap] = useState(false);
  const [sabOpen, setSabOpen] = useState(false);
  // màn hình nhỏ: danh sách nhiệm vụ thu gọn sẵn để không che bản đồ
  const [tasksOpen, setTasksOpen] = useState(() => window.innerWidth > 720 && window.innerHeight > 500);
  const nameOf = useCallback((pid: number) => game?.players.find((p) => p.id === pid)?.name ?? "?", [game]);

  const run = useCallback(
    (action: string, data?: unknown) =>
      act(action, data).catch((err) => {
        setToast(err instanceof ApiError ? err.message : "Không gửi được, thử lại.");
        throw err;
      }),
    [act],
  );
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 2500);
    return () => clearTimeout(t);
  }, [toast]);

  // ván bị xoá
  useEffect(() => {
    if (game === undefined && store.map) onClose();
  }, [game, store.map, onClose]);

  const phase: StFrame["phase"] = game?.status === "ended" ? "ended" : (frame?.phase ?? "");
  const frozen = !!task || phase !== "play";

  const doUse = () => {
    const u = near.use;
    if (!u) return;
    if (u.kind === "button") void run("emergency").catch(() => {});
    else setTask(u.station);
  };
  const doReport = () => near.report && void run("report", { target: near.report }).catch(() => {});
  const doKill = () => near.kill && void run("kill", { target: near.kill }).catch(() => {});

  // phím tắt
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") return;
      if (e.key === "Escape") {
        if (task) return;
        if (showMap) setShowMap(false);
        else if (sabOpen) setSabOpen(false);
        return;
      }
      if (phase !== "play" || task) return;
      const k = e.key.toLowerCase();
      if (k === "e" || k === " ") {
        e.preventDefault();
        doUse();
      } else if (k === "r") doReport();
      else if (k === "q") doKill();
      else if (k === "m" || k === "tab") {
        e.preventDefault();
        setShowMap((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  if (!map || !frame || !game) {
    return (
      <div className="st-screen st-loading" role="dialog" aria-label="Trạm Vũ Trụ">
        <Portrait color={colorOf(0)} size={96} />
        <p>Đang kết nối tới trạm…</p>
        <button type="button" className="st-btn" onClick={onClose}>
          Về kênh chat
        </button>
      </div>
    );
  }

  const me = frame.me;
  const st = game.state as StationState | undefined;
  const pct = frame.task_all ? Math.round((frame.task_done / frame.task_all) * 100) : 0;
  const killCd = Math.min(1, me.kill_in / ((st?.opts.kill_cooldown ?? 25) * 1000));
  const sabCd = Math.min(1, (frame.sab_ready ?? 0) / 30000);
  const myDone = me.tasks.filter((t) => t.done).length;

  return (
    <div
      className={`st-screen${frame.sab === "oxygen" ? " is-alarm" : ""}${frame.sab === "power" ? " is-dark" : ""}`}
      role="dialog"
      aria-label="Trạm Vũ Trụ"
    >
      <StationCanvas map={map} meId={meId} nameOf={nameOf} frozen={frozen} onNear={setNear} />

      {/* thanh trên */}
      <div className="st-top">
        <div className="st-taskbar" title="Tiến độ nhiệm vụ của cả trạm">
          <div className="st-taskbar-label">
            <span>Tiến độ trạm</span>
            <b>{pct}%</b>
          </div>
          <div className="st-bar">
            <i style={{ width: `${pct}%` }} />
          </div>
        </div>
        <div className="st-top-actions">
          <button type="button" className="st-icon" onClick={() => setShowMap((v) => !v)} title="Bản đồ (M)">
            <StIcon name="map" />
          </button>
          <button type="button" className="st-icon" onClick={onClose} title="Thu nhỏ, vẫn ở trong ván">
            <StIcon name="min" />
          </button>
        </div>
      </div>

      {/* danh sách nhiệm vụ */}
      <aside className={`st-tasks${tasksOpen ? "" : " is-closed"}${me.traitor ? " is-bad" : ""}`}>
        <button
          type="button"
          className="st-tasks-head"
          onClick={() => setTasksOpen((v) => !v)}
          aria-expanded={tasksOpen}
        >
          <span>{me.traitor ? "Nhiệm vụ giả" : me.ghost ? "Việc còn dang dở" : "Nhiệm vụ"}</span>
          <b>
            {myDone}/{me.tasks.length}
          </b>
          <StIcon name={tasksOpen ? "up" : "down"} />
        </button>
        {tasksOpen && me.traitor && (
          <p className="st-bad-hint">Giả vờ làm việc. Giết thuyền viên, phá hoại, đừng để lộ.</p>
        )}
        {frame.sab && (
          <p className="st-sab-hint">
            {frame.sab === "power"
              ? "Mất điện. Tới bảng điện ở Phòng điện để sửa."
              : `Rò oxy. Tới van ở Trạm oxy trong ${Math.ceil((frame.sab_in ?? 0) / 1000)} giây.`}
          </p>
        )}
        <ul hidden={!tasksOpen}>
          {me.tasks.map((t) => {
            const s = map.stations.find((x) => x.id === t.station);
            return (
              <li key={t.station} className={t.done ? "is-done" : ""}>
                <i aria-hidden>{t.done ? <StIcon name="check" /> : null}</i>
                <span>
                  {s?.name}
                  <small>{roomAt(map, s?.x ?? 0, s?.y ?? 0)}</small>
                </span>
              </li>
            );
          })}
        </ul>
      </aside>

      {frame.sab === "oxygen" && (
        <div className="st-alarm" role="alert">
          <StIcon name="alarm" />
          Oxy cạn sau {Math.ceil((frame.sab_in ?? 0) / 1000)} giây
        </div>
      )}

      {/* nút hành động */}
      {phase === "play" && (
        <div className="st-actions">
          <ActButton
            kind="use"
            icon={near.use?.kind === "button" ? "alarm" : "hand"}
            label={near.use?.kind === "button" ? `Họp khẩn (${me.emergencies})` : "Dùng"}
            disabled={!near.use}
            onClick={doUse}
            keyHint="E"
          />
          {!me.ghost && (
            <ActButton
              kind="report"
              icon="report"
              label="Báo xác"
              disabled={!near.report}
              onClick={doReport}
              keyHint="R"
            />
          )}
          {me.traitor && (
            <div className="st-sab-wrap">
              <ActButton
                kind="sab"
                icon="bolt"
                label="Phá hoại"
                cooldown={frame.sab ? 1 : sabCd}
                count={frame.sab_ready && frame.sab_ready > 0 ? Math.ceil(frame.sab_ready / 1000) : undefined}
                disabled={!!frame.sab || (frame.sab_ready ?? 0) > 0}
                onClick={() => setSabOpen((v) => !v)}
              />
              {sabOpen && (
                <div className="st-sab-menu" role="menu">
                  {(
                    [
                      ["power", "Cắt điện", "Thuyền viên gần như không nhìn thấy gì cho tới khi sửa"],
                      ["oxygen", "Xả oxy", "45 giây không ai sửa là phe bạn thắng"],
                    ] as const
                  ).map(([kind, label, hint]) => (
                    <button
                      key={kind}
                      type="button"
                      role="menuitem"
                      onClick={() => {
                        setSabOpen(false);
                        void run("sabotage", { kind }).catch(() => {});
                      }}
                    >
                      <StIcon name={kind === "power" ? "bolt" : "alarm"} />
                      <span>
                        <b>{label}</b>
                        <small>{hint}</small>
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          {me.traitor && !me.ghost && (
            <ActButton
              kind="kill"
              icon="blade"
              label="Hạ gục"
              cooldown={killCd}
              count={me.kill_in > 0 ? Math.ceil(me.kill_in / 1000) : undefined}
              disabled={!near.kill || me.kill_in > 0}
              onClick={doKill}
              keyHint="Q"
            />
          )}
        </div>
      )}

      {toast && (
        <p className="st-toast" role="status">
          {toast}
        </p>
      )}

      {showMap && <MiniMap map={map} frame={frame} onClose={() => setShowMap(false)} />}

      {task && (
        <TaskGame
          station={task}
          run={run}
          onClose={(done) => {
            if (!done) void act("cancel_use").catch(() => {});
            setTask(null);
          }}
        />
      )}

      {phase === "intro" && <Intro frame={frame} game={game} meId={meId} />}
      {(phase === "meeting" || phase === "eject") && frame.meeting && (
        <Meeting frame={frame} game={game} meId={meId} nameOf={nameOf} run={run} />
      )}
      {phase === "ended" && <Ending game={game} st={st} meId={meId} nameOf={nameOf} onClose={onClose} />}
      {me.ghost && phase === "play" && (
        <p className="st-ghost-tag">Bạn đã thành hồn ma: đi xuyên tường được, hãy làm nốt nhiệm vụ.</p>
      )}
    </div>
  );
}

/* ---------- Icon, nút hành động, chân dung ---------- */

const ICONS: Record<string, string> = {
  hand: "M8 13V5.5a1.5 1.5 0 0 1 3 0V11M11 10.5V4a1.5 1.5 0 0 1 3 0v6.5M14 10.5V5.5a1.5 1.5 0 0 1 3 0V14c0 4-2.6 7-6.2 7-2.6 0-4.3-1.3-5.8-3.8L3.3 13a1.5 1.5 0 0 1 2.5-1.7L8 13.5",
  report: "M3 10v4h3l8 4.5V5.5L6 10H3ZM17.5 9.2a4 4 0 0 1 0 5.6M20 6.5a8 8 0 0 1 0 11",
  blade:
    "M20.5 3.5c-5 .8-9.6 4.7-12 8.5l3.5 3.5c3.8-2.4 7.7-7 8.5-12ZM8.5 12 6 14.5M12 15.5 9.5 18M5.5 13.5l5 5M3.5 20.5l3-3",
  bolt: "M13 2 4.5 13.5H11l-1 8.5 8.5-11.5H12l1-8.5Z",
  alarm: "M12 3v2M4.6 6.6 6 8M19.4 6.6 18 8M6.5 17.5a5.5 5.5 0 0 1 11 0V19h-11v-1.5ZM4 21h16",
  map: "M9 4 3 6.5v13.5l6-2.5 6 2.5 6-2.5V4l-6 2.5L9 4ZM9 4v13.5M15 6.5V20",
  min: "M5 12h14",
  check: "m5 12.5 4.5 4.5L19 7",
  up: "m6 15 6-6 6 6",
  down: "m6 9 6 6 6-6",
  chat: "M4 5h16v11H9l-5 4V5Z",
};

function StIcon({ name }: { name: string }) {
  return (
    <svg
      className="st-svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d={ICONS[name]} />
    </svg>
  );
}

function ActButton({
  kind,
  icon,
  label,
  disabled,
  onClick,
  cooldown,
  count,
  keyHint,
}: {
  kind: string;
  icon: string;
  label: string;
  disabled: boolean;
  onClick: () => void;
  cooldown?: number;
  count?: number;
  keyHint?: string;
}) {
  return (
    <button
      type="button"
      className={`st-act is-${kind}`}
      disabled={disabled}
      onClick={onClick}
      style={{ ["--cd" as string]: cooldown ?? 0 }}
      title={keyHint ? `${label} (${keyHint})` : label}
    >
      <span className="st-act-ring" aria-hidden />
      {count !== undefined ? <b className="st-act-count">{count}</b> : <StIcon name={icon} />}
      <span className="st-act-label">{label}</span>
      {keyHint && <kbd>{keyHint}</kbd>}
    </button>
  );
}

/** Chân dung nhân vật (dùng cùng hình vẽ với bản đồ). */
function Portrait({
  color,
  size,
  dead,
  ghost,
  facing = 1,
  bust,
}: {
  color: string;
  size: number;
  dead?: boolean;
  ghost?: boolean;
  facing?: number;
  /** chỉ nửa người trên (mũ + vai): rõ mặt khi khung nhỏ */
  bust?: boolean;
}) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const c = ref.current!;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    c.width = size * dpr;
    c.height = size * dpr;
    const ctx = c.getContext("2d")!;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, c.width, c.height);
    const k = (size * dpr) / (bust && !dead ? 50 : 96);
    const oy = bust && !dead ? 0.95 : dead ? 0.55 : 0.6;
    ctx.setTransform(k, 0, 0, k, (size * dpr) / 2, size * dpr * oy);
    ctx.globalAlpha = ghost ? 0.55 : 1;
    drawCharacter(ctx, { color, dead, ghost, facing, now: 0 });
  }, [color, size, dead, ghost, facing, bust]);
  return <canvas ref={ref} className="st-portrait" style={{ width: size, height: size }} aria-hidden />;
}

/* ---------- Canvas: bản đồ, người chơi, bóng tối ---------- */

function StationCanvas({
  map,
  meId,
  nameOf,
  frozen,
  onNear,
}: {
  map: StationMap;
  meId: number;
  nameOf: (id: number) => string;
  frozen: boolean;
  onNear: (n: Near) => void;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const keys = useRef(new Set<string>());
  const stick = useRef({ x: 0, y: 0 });
  const pos = useRef<{ x: number; y: number; correction: number } | null>(null);
  const live = useRef({ frozen, nameOf, onNear });
  live.current = { frozen, nameOf, onNear };
  // cần gạt: máy cảm ứng, hoặc ngay khi có lần chạm đầu tiên (laptop màn cảm ứng, trình giả lập)
  const [touch, setTouch] = useState(() => isTouchDevice() || navigator.maxTouchPoints > 0);
  useEffect(() => {
    if (touch) return;
    const on = (e: PointerEvent) => e.pointerType === "touch" && setTouch(true);
    window.addEventListener("pointerdown", on);
    return () => window.removeEventListener("pointerdown", on);
  }, [touch]);

  useEffect(() => {
    const down = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (el.tagName === "INPUT" || el.tagName === "TEXTAREA") return;
      keys.current.add(e.key.toLowerCase());
      if (e.key.startsWith("Arrow")) e.preventDefault();
    };
    const up = (e: KeyboardEvent) => keys.current.delete(e.key.toLowerCase());
    const blur = () => keys.current.clear();
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    window.addEventListener("blur", blur);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
      window.removeEventListener("blur", blur);
    };
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current!;
    const ctx = canvas.getContext("2d")!;
    let raf = 0;
    let last = performance.now();
    let lastSent = 0;
    let sentX = NaN;
    let sentY = NaN;
    let nearKey = "";
    const stars = makeStars(240);
    const anim: AnimMap = new Map();

    const loop = (now: number) => {
      raf = requestAnimationFrame(loop);
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      const f = stationStore.frame;
      if (!f) return;
      const me = f.me;

      // vị trí của mình: tự đi trên máy, máy chủ chỉnh lại khi cần
      let p = pos.current;
      if (!p || p.correction !== me.correction || f.phase !== "play") {
        p = pos.current = { x: me.x, y: me.y, correction: me.correction };
      }
      if (!live.current.frozen && f.phase === "play") {
        let dx = 0;
        let dy = 0;
        const k = keys.current;
        if (k.has("arrowleft") || k.has("a")) dx -= 1;
        if (k.has("arrowright") || k.has("d")) dx += 1;
        if (k.has("arrowup") || k.has("w")) dy -= 1;
        if (k.has("arrowdown") || k.has("s")) dy += 1;
        dx += stick.current.x;
        dy += stick.current.y;
        const len = Math.hypot(dx, dy);
        if (len > 0.05) {
          const step = (map.speed * dt) / Math.max(1, len);
          const nx = p.x + dx * step;
          const ny = p.y + dy * step;
          const ok = (x: number, y: number) =>
            me.ghost ? x > 0 && y > 0 && x < map.w && y < map.h : walkable(map, x, y);
          if (ok(nx, p.y)) p.x = nx;
          if (ok(p.x, ny)) p.y = ny;
        }
        if (now - lastSent > 66 && (p.x !== sentX || p.y !== sentY)) {
          stationStore.sendPos(p.x, p.y);
          sentX = p.x;
          sentY = p.y;
          lastSent = now;
        }
      }

      // những thứ ở gần (để bật nút Dùng / Báo xác / Giết)
      const near: Near = {};
      if (f.phase === "play") {
        if (f.sab) {
          const fix = map.stations.find((s) => s.id === (f.sab === "power" ? "fix-power" : "fix-oxygen"));
          if (fix && !me.ghost && Math.hypot(fix.x - p.x, fix.y - p.y) < RANGE.use)
            near.use = { kind: "station", station: fix };
        }
        if (!near.use) {
          let best = RANGE.use;
          for (const t of me.tasks) {
            if (t.done) continue;
            const s = map.stations.find((x) => x.id === t.station);
            const d = s ? Math.hypot(s.x - p.x, s.y - p.y) : Infinity;
            if (s && d < best) {
              best = d;
              near.use = { kind: "station", station: s };
            }
          }
        }
        if (
          !near.use &&
          !me.ghost &&
          !f.sab &&
          me.emergencies > 0 &&
          Math.hypot(map.button[0] - p.x, map.button[1] - p.y) < RANGE.button
        ) {
          near.use = { kind: "button" };
        }
        if (!me.ghost) {
          let best = RANGE.report;
          for (const b of f.bodies) {
            const d = Math.hypot(b.x - p.x, b.y - p.y);
            if (d < best) {
              best = d;
              near.report = b.id;
            }
          }
        }
        if (me.traitor && !me.ghost) {
          let best = RANGE.kill;
          for (const o of f.players) {
            if (o.ghost || o.traitor) continue;
            const q = stationStore.position(o.id, now) ?? o;
            const d = Math.hypot(q.x - p.x, q.y - p.y);
            if (d < best) {
              best = d;
              near.kill = o.id;
            }
          }
        }
      }
      const key = JSON.stringify([
        near.use?.kind === "station" ? near.use.station.id : near.use?.kind,
        near.report,
        near.kill,
      ]);
      if (key !== nearKey) {
        nearKey = key;
        live.current.onNear(near);
      }

      draw(ctx, canvas, map, f, p, now, meId, live.current.nameOf, stars, anim);
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
  }, [map, meId]);

  // khung vẽ theo kích thước màn hình (nét trên màn hình mật độ cao)
  useEffect(() => {
    const canvas = canvasRef.current!;
    const fit = () => {
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      canvas.width = Math.round(canvas.clientWidth * dpr);
      canvas.height = Math.round(canvas.clientHeight * dpr);
    };
    fit();
    const ro = new ResizeObserver(fit);
    ro.observe(canvas);
    return () => ro.disconnect();
  }, []);

  return (
    <>
      <canvas ref={canvasRef} className="st-canvas" aria-hidden />
      {touch && <Joystick stick={stick} />}
    </>
  );
}

type AnimMap = Map<number, { x: number; y: number; facing: number; step: number; moving: boolean }>;

/** Cập nhật hướng nhìn và nhịp bước chân từ vị trí mới. */
function animate(anim: AnimMap, id: number, x: number, y: number) {
  const a = anim.get(id) ?? { x, y, facing: 1, step: 0, moving: false };
  const dx = x - a.x;
  const d = Math.hypot(dx, y - a.y);
  a.moving = d > 0.4;
  if (a.moving) a.step += d * 0.11;
  if (Math.abs(dx) > 0.3) a.facing = dx > 0 ? 1 : -1;
  a.x = x;
  a.y = y;
  anim.set(id, a);
  return a;
}

function draw(
  ctx: CanvasRenderingContext2D,
  canvas: HTMLCanvasElement,
  map: StationMap,
  f: StFrame,
  me: { x: number; y: number },
  now: number,
  meId: number,
  nameOf: (id: number) => string,
  stars: Star[],
  anim: AnimMap,
) {
  const W = canvas.width;
  const H = canvas.height;
  // tỉ lệ theo kích thước màn hình tính bằng px CSS (điện thoại không bị thu quá nhỏ)
  const dpr = W / Math.max(1, canvas.clientWidth);
  const scale = dpr * Math.max(0.55, Math.min(1.35, Math.min(canvas.clientWidth, canvas.clientHeight) / 720));
  const dark = f.sab === "power" && !f.me.traitor && !f.me.ghost;

  ctx.setTransform(1, 0, 0, 1, 0, 0);
  drawSpace(ctx, W, H, me.x * scale, me.y * scale, stars);
  ctx.setTransform(scale, 0, 0, scale, W / 2 - me.x * scale, H / 2 - me.y * scale);
  const view = { x: me.x - W / 2 / scale, y: me.y - H / 2 / scale, w: W / scale, h: H / scale };
  drawWorld(ctx, map, view, now, dark);
  drawButton(ctx, map.button[0], map.button[1], now, !f.sab && f.me.emergencies > 0 && !f.me.ghost);

  // trạm làm việc
  const mine = new Set(f.me.tasks.filter((t) => !t.done).map((t) => t.station));
  const fixId = f.sab === "power" ? "fix-power" : f.sab === "oxygen" ? "fix-oxygen" : "";
  for (const s of map.stations) {
    if (s.kind === "fix" && s.id !== fixId) continue;
    const state = s.id === fixId ? "alarm" : mine.has(s.id) ? "task" : "idle";
    drawStation(ctx, s, now, state, roomAccent(s.room));
  }

  // xác
  for (const b of f.bodies) {
    ctx.save();
    ctx.translate(b.x, b.y);
    drawCharacter(ctx, { color: colorOf(f.colors[b.id]), dead: true });
    ctx.restore();
  }

  // người chơi (người khác nội suy giữa hai frame, mình đi theo máy)
  type Pod = { id: number; x: number; y: number; color: number; ghost?: boolean; ally?: boolean };
  const pods: Pod[] = [];
  for (const o of f.players) {
    const q = stationStore.position(o.id, now) ?? o;
    pods.push({ id: o.id, x: q.x, y: q.y, color: o.color, ghost: o.ghost, ally: o.traitor });
  }
  pods.push({ id: meId, x: me.x, y: me.y, color: f.me.color, ghost: f.me.ghost, ally: f.me.traitor });
  pods.sort((a, b) => a.y - b.y);
  for (const p of pods) {
    const a = animate(anim, p.id, p.x, p.y);
    ctx.save();
    ctx.translate(p.x, p.y - 4 + (p.ghost ? Math.sin(now / 420 + p.id) * 4 : 0));
    ctx.globalAlpha = p.ghost ? 0.5 : 1;
    drawCharacter(ctx, {
      color: colorOf(p.color),
      facing: a.facing,
      step: a.step,
      moving: a.moving,
      ghost: p.ghost,
      now,
    });
    nameTag(ctx, nameOf(p.id), p.id === meId, !!p.ally && f.me.traitor);
    ctx.restore();
  }

  ctx.setTransform(1, 0, 0, 1, 0, 0);
  // bóng tối ngoài tầm nhìn
  if (!f.me.ghost) {
    const r = f.me.vision * scale;
    const g = ctx.createRadialGradient(W / 2, H / 2, r * 0.5, W / 2, H / 2, r);
    g.addColorStop(0, "rgba(4, 7, 18, 0)");
    g.addColorStop(0.7, dark ? "rgba(4, 7, 18, 0.8)" : "rgba(4, 7, 18, 0.55)");
    g.addColorStop(1, dark ? "rgba(2, 4, 10, 0.97)" : "rgba(4, 7, 18, 0.9)");
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, W, H);
  }

  // mũi tên ở mép màn hình chỉ tới nhiệm vụ / chỗ sửa sự cố
  if (f.phase === "play") {
    const targets = map.stations.filter((s) => mine.has(s.id) || s.id === fixId);
    for (const s of targets) {
      const sx = W / 2 + (s.x - me.x) * scale;
      const sy = H / 2 + (s.y - me.y) * scale;
      const m = 46 * dpr;
      if (sx > m && sx < W - m && sy > m && sy < H - m) continue;
      const ang = Math.atan2(sy - H / 2, sx - W / 2);
      const k = Math.min((W / 2 - m) / Math.abs(Math.cos(ang) || 1e-6), (H / 2 - m) / Math.abs(Math.sin(ang) || 1e-6));
      ctx.save();
      ctx.translate(W / 2 + Math.cos(ang) * k, H / 2 + Math.sin(ang) * k);
      ctx.rotate(ang);
      ctx.scale(dpr, dpr);
      ctx.fillStyle = s.id === fixId ? "#ff4d6d" : "#ffc94d";
      ctx.strokeStyle = "#0b1220";
      ctx.lineWidth = 3;
      ctx.beginPath();
      ctx.moveTo(16, 0);
      ctx.lineTo(-8, -12);
      ctx.lineTo(-3, 0);
      ctx.lineTo(-8, 12);
      ctx.closePath();
      ctx.fill();
      ctx.stroke();
      ctx.restore();
    }
  }
}

function nameTag(ctx: CanvasRenderingContext2D, name: string, me: boolean, ally: boolean) {
  ctx.font = "600 15px 'Be Vietnam Pro', system-ui, sans-serif";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  const w = ctx.measureText(name).width + 16;
  ctx.fillStyle = me ? "rgba(255, 201, 77, 0.92)" : "rgba(8, 13, 26, 0.72)";
  ctx.beginPath();
  ctx.roundRect(-w / 2, -66, w, 22, 11);
  ctx.fill();
  ctx.fillStyle = me ? "#1d1405" : ally ? "#ff6b81" : "#f1f5fb";
  ctx.fillText(name, 0, -54.5);
}

/* ---------- Cần điều khiển cảm ứng ---------- */

function Joystick({ stick }: { stick: MutableRefObject<{ x: number; y: number }> }) {
  const [knob, setKnob] = useState({ x: 0, y: 0 });
  const origin = useRef<{ x: number; y: number; id: number } | null>(null);
  const R = 50;
  const move = (e: React.PointerEvent) => {
    const o = origin.current;
    if (!o || o.id !== e.pointerId) return;
    let dx = e.clientX - o.x;
    let dy = e.clientY - o.y;
    const len = Math.hypot(dx, dy);
    if (len > R) {
      dx = (dx / len) * R;
      dy = (dy / len) * R;
    }
    setKnob({ x: dx, y: dy });
    stick.current = { x: dx / R, y: dy / R };
  };
  const end = () => {
    origin.current = null;
    setKnob({ x: 0, y: 0 });
    stick.current = { x: 0, y: 0 };
  };
  return (
    <div
      className="st-stick"
      onPointerDown={(e) => {
        const r = e.currentTarget.getBoundingClientRect();
        origin.current = { x: r.left + r.width / 2, y: r.top + r.height / 2, id: e.pointerId };
        e.currentTarget.setPointerCapture(e.pointerId);
        move(e);
      }}
      onPointerMove={move}
      onPointerUp={end}
      onPointerCancel={end}
    >
      <i style={{ transform: `translate(${knob.x}px, ${knob.y}px)` }} />
    </div>
  );
}

/* ---------- Bản đồ nhỏ ---------- */

function MiniMap({ map, frame, onClose }: { map: StationMap; frame: StFrame; onClose: () => void }) {
  const mine = new Set(frame.me.tasks.filter((t) => !t.done).map((t) => t.station));
  const fixId = frame.sab === "power" ? "fix-power" : frame.sab === "oxygen" ? "fix-oxygen" : "";
  return (
    <div className="st-map" onClick={onClose}>
      <svg viewBox={`0 0 ${map.w} ${map.h}`} role="img" aria-label="Bản đồ trạm">
        {map.halls.map((h, i) => (
          <rect key={i} x={h.X} y={h.Y} width={h.W} height={h.H} className="st-map-hall" />
        ))}
        {map.rooms.map((r) => (
          <g key={r.id}>
            <rect x={r.x} y={r.y} width={r.w} height={r.h} rx={18} className="st-map-room" />
            <text x={r.x + r.w / 2} y={r.y + r.h / 2}>
              {r.name}
            </text>
          </g>
        ))}
        {map.stations
          .filter((s) => mine.has(s.id) || s.id === fixId)
          .map((s) => (
            <circle key={s.id} cx={s.x} cy={s.y} r={28} className={s.id === fixId ? "st-map-fix" : "st-map-task"} />
          ))}
        <circle cx={frame.me.x} cy={frame.me.y} r={34} fill={colorOf(frame.me.color)} stroke="#fff" strokeWidth={8} />
      </svg>
      <p>Chấm vàng: nhiệm vụ của bạn{fixId ? " · chấm đỏ: chỗ sửa sự cố" : ""}. Bấm để đóng.</p>
    </div>
  );
}

/* ---------- Mở đầu, họp, kết thúc ---------- */

function Intro({ frame, game, meId }: { frame: StFrame; game: GameView; meId: number }) {
  const bad = frame.me.traitor;
  const count = (game.state as StationState | undefined)?.traitor_count ?? 1;
  // kẻ phản bội chỉ thấy đồng bọn; thuyền viên thấy cả đoàn
  const lineup = bad ? [meId, ...(frame.allies ?? [])] : game.players.map((p) => p.id);
  const others = lineup.filter((id) => id !== meId);
  const left = others.slice(0, Math.ceil(others.length / 2));
  const right = others.slice(Math.ceil(others.length / 2));
  const nameOf = (id: number) => game.players.find((p) => p.id === id)?.name ?? "?";
  return (
    <div className={`st-cinema st-intro ${bad ? "is-bad" : "is-good"}`}>
      <h2>{bad ? "Kẻ phản bội" : "Thuyền viên"}</h2>
      <p className="st-cinema-sub">
        {bad
          ? others.length
            ? `Cùng ${others.map(nameOf).join(", ")} hạ gục thuyền viên mà không bị phát hiện.`
            : "Hạ gục thuyền viên mà không bị phát hiện."
          : `Có ${count} kẻ phản bội đang trà trộn. Làm xong nhiệm vụ và tìm ra chúng.`}
      </p>
      <div className="st-lineup">
        {left.map((id, i) => (
          <figure key={id} style={{ ["--d" as string]: left.length - i }}>
            <Portrait color={colorOf(frame.colors[id])} size={84} facing={1} />
            <figcaption>{nameOf(id)}</figcaption>
          </figure>
        ))}
        <figure className="is-me">
          <Portrait color={colorOf(frame.me.color)} size={136} />
          <figcaption>Bạn</figcaption>
        </figure>
        {right.map((id, i) => (
          <figure key={id} style={{ ["--d" as string]: i + 1 }}>
            <Portrait color={colorOf(frame.colors[id])} size={84} facing={-1} />
            <figcaption>{nameOf(id)}</figcaption>
          </figure>
        ))}
      </div>
    </div>
  );
}

function Meeting({
  frame,
  game,
  meId,
  nameOf,
  run,
}: {
  frame: StFrame;
  game: GameView;
  meId: number;
  nameOf: (id: number) => string;
  run: (action: string, data?: unknown) => Promise<void>;
}) {
  const m = frame.meeting!;
  const [pick, setPick] = useState<number | null>(null);
  const [text, setText] = useState("");
  const chatRef = useRef<HTMLOListElement>(null);
  const eject = frame.phase === "eject";
  const voting = m.vote_in <= 0;
  const alive = new Set(m.alive);
  const voted = new Set(m.voted);
  const canVote = !frame.me.ghost && voting && m.my_vote === undefined && !eject;
  const secs = Math.ceil((voting ? (frame.ends_in ?? 0) : m.vote_in) / 1000);

  useEffect(() => {
    chatRef.current?.scrollTo({ top: chatRef.current.scrollHeight });
  }, [m.chat.length]);

  const send = (e: FormEvent) => {
    e.preventDefault();
    if (!text.trim()) return;
    void run("chat", { text: text.trim() })
      .then(() => setText(""))
      .catch(() => {});
  };
  const vote = (target: number) => {
    setPick(null);
    void run("vote", { target }).catch(() => {});
  };
  const votersFor = (target: number) =>
    Object.entries(m.votes ?? {})
      .filter(([, t]) => t === target)
      .map(([v]) => Number(v));

  if (eject) {
    const who = m.ejected;
    return (
      <div className="st-cinema st-eject">
        {who ? (
          <>
            <div className="st-drift">
              <Portrait color={colorOf(frame.colors[who])} size={120} />
            </div>
            <h2>{nameOf(who)} đã bị đẩy ra ngoài không gian.</h2>
            {m.was_traitor !== undefined && (
              <p className="st-cinema-sub">
                {m.was_traitor ? `${nameOf(who)} là kẻ phản bội.` : `${nameOf(who)} không phải kẻ phản bội.`}
              </p>
            )}
          </>
        ) : (
          <h2>{m.tie ? "Hoà phiếu. Không ai bị đẩy ra." : "Cả trạm bỏ qua. Không ai bị đẩy ra."}</h2>
        )}
        <ul className="st-tally" aria-label="Kết quả bỏ phiếu">
          {[...m.alive, 0].map((t) => {
            const vs = votersFor(t);
            if (vs.length === 0) return null;
            return (
              <li key={t}>
                <span>{t ? nameOf(t) : "Bỏ qua"}</span>
                <span className="st-tally-dots">
                  {vs.map((v) => (
                    <i key={v} title={nameOf(v)} style={{ background: colorOf(frame.colors[v]) }} />
                  ))}
                </span>
              </li>
            );
          })}
        </ul>
      </div>
    );
  }

  return (
    <div className="st-meeting">
      <header className="st-meeting-head">
        <div className="st-meeting-badge" aria-hidden>
          <StIcon name={m.body ? "report" : "alarm"} />
        </div>
        <div>
          <h2>{m.body ? `Tìm thấy ${nameOf(m.body)} đã gục` : "Họp khẩn cấp"}</h2>
          <p>{nameOf(m.caller)} triệu tập cuộc họp</p>
        </div>
        <div className={`st-meeting-clock${voting ? " is-voting" : ""}`}>
          <small>{voting ? "Bỏ phiếu" : "Thảo luận"}</small>
          <b>{secs}s</b>
        </div>
      </header>
      <div className="st-meeting-body">
        <section aria-label="Bỏ phiếu">
          <ul className="st-ballot">
            {game.players.map((p) => {
              const dead = !alive.has(p.id);
              const cls = ["st-voter", dead && "is-dead", pick === p.id && "is-picked", m.my_vote === p.id && "is-mine"]
                .filter(Boolean)
                .join(" ");
              return (
                <li key={p.id}>
                  <button
                    type="button"
                    className={cls}
                    disabled={!canVote || dead}
                    onClick={() => setPick(pick === p.id ? null : p.id)}
                  >
                    <Portrait color={colorOf(frame.colors[p.id])} size={52} dead={dead} bust />
                    <span className="st-voter-name">
                      {p.name}
                      {p.id === meId && <small>bạn</small>}
                      {p.id === m.caller && <small>người triệu tập</small>}
                    </span>
                    {dead ? (
                      <em>đã gục</em>
                    ) : (
                      voted.has(p.id) && (
                        <em className="st-voted">
                          <StIcon name="check" />
                          đã bầu
                        </em>
                      )
                    )}
                  </button>
                  {pick === p.id && (
                    <span className="st-confirm">
                      <button type="button" className="st-btn is-red" onClick={() => vote(p.id)}>
                        Bầu {p.name}
                      </button>
                      <button type="button" className="st-btn" onClick={() => setPick(null)}>
                        Thôi
                      </button>
                    </span>
                  )}
                </li>
              );
            })}
          </ul>
          <div className="st-meeting-foot">
            {canVote && (
              <button type="button" className="st-btn" onClick={() => vote(0)}>
                Bỏ qua lượt bầu
              </button>
            )}
            {m.my_vote !== undefined && (
              <span>Bạn đã {m.my_vote ? `bầu ${nameOf(m.my_vote)}` : "bỏ qua lượt bầu"}.</span>
            )}
            {!voting && !frame.me.ghost && <span>Bàn bạc trước, hết giờ thảo luận mới được bầu.</span>}
            {frame.me.ghost && <span>Hồn ma không được bầu, chỉ nói chuyện với hồn ma khác.</span>}
            <span className="st-muted">
              {m.voted.length}/{m.alive.length} người đã bầu
            </span>
          </div>
        </section>
        <section className="st-chat" aria-label="Thảo luận">
          <ol ref={chatRef}>
            {m.chat.length === 0 && <li className="st-chat-empty">Ai thấy gì thì kể đi. Ở đâu, lúc nào, với ai?</li>}
            {m.chat.map((c, i) => (
              <li key={i} className={c.dead ? "is-ghost" : ""}>
                <Portrait color={colorOf(frame.colors[c.from])} size={34} ghost={c.dead} bust />
                <div>
                  <b>{nameOf(c.from)}</b>
                  <p>{c.text}</p>
                </div>
              </li>
            ))}
          </ol>
          <form onSubmit={send}>
            <input
              value={text}
              onChange={(e) => setText(e.target.value)}
              maxLength={200}
              placeholder={frame.me.ghost ? "Nhắn với các hồn ma…" : "Nói gì đó với cả trạm…"}
              aria-label="Tin nhắn trong cuộc họp"
            />
            <button className="st-btn is-primary">Gửi</button>
          </form>
        </section>
      </div>
    </div>
  );
}

function Ending({
  game,
  st,
  meId,
  nameOf,
  onClose,
}: {
  game: GameView;
  st: StationState | undefined;
  meId: number;
  nameOf: (id: number) => string;
  onClose: () => void;
}) {
  const win = game.winners?.includes(meId);
  const traitors = new Set(st?.traitors ?? []);
  const crewWon = game.result?.startsWith("Thuyền viên");
  const winners = game.players.filter((p) => game.winners?.includes(p.id));
  return (
    <div className={`st-cinema st-ending ${crewWon ? "is-good" : "is-bad"}`}>
      <p className="st-cinema-sub">{game.result}</p>
      <h2>{win ? "Chiến thắng" : "Thất bại"}</h2>
      <div className="st-lineup">
        {winners.map((p, i) => (
          <figure key={p.id} style={{ ["--d" as string]: i }}>
            <Portrait color={colorOf(st?.colors[p.id])} size={p.id === meId ? 120 : 88} />
            <figcaption>{p.name}</figcaption>
          </figure>
        ))}
      </div>
      <p className="st-cinema-sub">Kẻ phản bội: {[...traitors].map(nameOf).join(", ") || "không rõ"}</p>
      <button type="button" className="st-btn is-primary" onClick={onClose}>
        Về kênh chat
      </button>
    </div>
  );
}

/* ---------- Trò nhỏ khi làm nhiệm vụ ---------- */

function TaskGame({
  station,
  run,
  onClose,
}: {
  station: StStation;
  run: (action: string, data?: unknown) => Promise<void>;
  onClose: (done: boolean) => void;
}) {
  const started = useRef(performance.now());
  const [state, setState] = useState<"starting" | "ready" | "sending">("starting");

  useEffect(() => {
    run("use", { station: station.id })
      .then(() => setState("ready"))
      .catch(() => onClose(true));
  }, [station.id]);

  const finish = useCallback(() => {
    setState("sending");
    // đợi đủ thời gian tối thiểu máy chủ yêu cầu
    const wait = Math.max(0, MIN_TIME[station.kind] + 200 - (performance.now() - started.current));
    setTimeout(() => {
      run(station.kind === "fix" ? "fix" : "task", { station: station.id })
        .then(() => onClose(true))
        .catch(() => onClose(true));
    }, wait);
  }, [run, station, onClose]);

  const body =
    state === "starting" ? (
      <p className="st-muted">Đang mở…</p>
    ) : station.kind === "wires" ? (
      <Wires onDone={finish} />
    ) : station.kind === "code" ? (
      <Keypad onDone={finish} />
    ) : station.kind === "align" ? (
      <Align onDone={finish} />
    ) : station.kind === "scan" ? (
      <Hold onDone={finish} ms={6200} auto label="Đứng yên trên bệ quét…" />
    ) : (
      <Hold onDone={finish} ms={station.kind === "fix" ? 2700 : 3200} label="Nhấn giữ" />
    );

  return (
    <div className="st-task-backdrop">
      <div className={`st-task st-task-${station.kind}`} role="dialog" aria-label={station.name}>
        <header>
          <h3>{station.name}</h3>
          <button type="button" className="st-icon" onClick={() => onClose(false)} aria-label="Đóng">
            ✕
          </button>
        </header>
        {state === "sending" ? <p className="st-done">✓ Xong!</p> : body}
      </div>
    </div>
  );
}

const WIRE_COLORS = ["#e5484d", "#3e8ef7", "#f5b82e", "#b06cf2"];

function Wires({ onDone }: { onDone: () => void }) {
  const right = useMemo(() => [0, 1, 2, 3].sort(() => Math.random() - 0.5), []);
  const [from, setFrom] = useState<number | null>(null);
  const [links, setLinks] = useState<number[]>([]);
  useEffect(() => {
    if (links.length === 4) onDone();
  }, [links, onDone]);
  const ROW = 56;
  return (
    <div className="st-wires">
      <svg viewBox="0 0 300 230" aria-hidden>
        {links.map((c) => (
          <line
            key={c}
            x1={40}
            y1={30 + c * ROW}
            x2={260}
            y2={30 + right.indexOf(c) * ROW}
            stroke={WIRE_COLORS[c]}
            strokeWidth={10}
            strokeLinecap="round"
          />
        ))}
      </svg>
      <div className="st-wire-col">
        {[0, 1, 2, 3].map((c) => (
          <button
            key={c}
            type="button"
            className={`st-wire${from === c ? " is-on" : ""}`}
            style={{ background: WIRE_COLORS[c] }}
            disabled={links.includes(c)}
            onClick={() => setFrom(c)}
            aria-label={`Dây trái ${c + 1}`}
          />
        ))}
      </div>
      <div className="st-wire-col is-right">
        {right.map((c) => (
          <button
            key={c}
            type="button"
            className="st-wire"
            style={{ background: WIRE_COLORS[c] }}
            disabled={links.includes(c)}
            onClick={() => {
              if (from === c) setLinks((l) => [...l, c]);
              setFrom(null);
            }}
            aria-label={`Dây phải ${c + 1}`}
          />
        ))}
      </div>
      <p className="st-muted">Bấm đầu dây bên trái rồi bấm đầu cùng màu bên phải.</p>
    </div>
  );
}

function Keypad({ onDone }: { onDone: () => void }) {
  const code = useMemo(() => String(Math.floor(10000 + Math.random() * 90000)), []);
  const [typed, setTyped] = useState("");
  const [bad, setBad] = useState(false);
  const press = (d: string) => {
    const next = typed + d;
    if (!code.startsWith(next)) {
      setBad(true);
      setTyped("");
      setTimeout(() => setBad(false), 400);
      return;
    }
    setTyped(next);
    if (next === code) onDone();
  };
  return (
    <div className="st-keypad">
      <p className="st-code">
        Mã: <b>{code}</b>
      </p>
      <p className={`st-screenline${bad ? " is-bad" : ""}`}>{typed.padEnd(5, "·")}</p>
      <div className="st-keys">
        {["1", "2", "3", "4", "5", "6", "7", "8", "9", "0"].map((d) => (
          <button key={d} type="button" onClick={() => press(d)}>
            {d}
          </button>
        ))}
      </div>
    </div>
  );
}

function Align({ onDone }: { onDone: () => void }) {
  const target = useMemo(() => 15 + Math.round(Math.random() * 70), []);
  const [v, setV] = useState(target > 50 ? 5 : 95);
  const ok = Math.abs(v - target) <= 3;
  return (
    <div className="st-align">
      <div className="st-gauge">
        <i className="st-gauge-target" style={{ left: `${target}%` }} />
        <i className={`st-gauge-needle${ok ? " is-ok" : ""}`} style={{ left: `${v}%` }} />
      </div>
      <input
        type="range"
        min={0}
        max={100}
        value={v}
        onChange={(e) => setV(Number(e.target.value))}
        onPointerUp={() => ok && onDone()}
        onKeyUp={() => ok && onDone()}
        aria-label="Căn chỉnh"
      />
      <p className="st-muted">Kéo kim trùng vạch xanh rồi thả tay.</p>
    </div>
  );
}

function Hold({ onDone, ms, label, auto }: { onDone: () => void; ms: number; label: string; auto?: boolean }) {
  const [held, setHeld] = useState(!!auto);
  const [pct, setPct] = useState(0);
  const doneRef = useRef(false);
  useEffect(() => {
    if (!held) return;
    const t = setInterval(() => {
      setPct((p) => {
        const n = Math.min(100, p + (100 * 100) / ms);
        if (n >= 100 && !doneRef.current) {
          doneRef.current = true;
          setTimeout(onDone, 0);
        }
        return n;
      });
    }, 100);
    return () => clearInterval(t);
  }, [held, ms, onDone]);
  return (
    <div className="st-hold">
      <div className="st-bar is-big">
        <i style={{ width: `${pct}%` }} />
      </div>
      {auto ? (
        <p className="st-muted">{label}</p>
      ) : (
        <button
          type="button"
          className="st-btn is-hold"
          onPointerDown={() => setHeld(true)}
          onPointerUp={() => setHeld(false)}
          onPointerLeave={() => setHeld(false)}
          onKeyDown={(e) => e.key === " " && setHeld(true)}
          onKeyUp={() => setHeld(false)}
        >
          {label}
        </button>
      )}
    </div>
  );
}

/* ---------- Thẻ trong kênh chat ---------- */

export function StationSummary({ messageId, game, meId }: { messageId: number; game: GameView; meId: number }) {
  const st = game.state as StationState;
  const pct = st.task_all ? Math.round((st.task_done / st.task_all) * 100) : 0;
  const dead = new Set(st.dead);
  const traitors = new Set(st.traitors ?? []);
  const isPlayer = game.players.some((p) => p.id === meId);
  const meeting = st.phase === "meeting" || st.phase === "eject";
  return (
    <div className="st-summary">
      {game.status === "playing" && (
        <div className="st-summary-status">
          <span>{meeting ? "Cả trạm đang họp" : "Đang làm nhiệm vụ"}</span>
          <div className="st-bar is-light" title={`Nhiệm vụ ${pct}%`}>
            <i style={{ width: `${pct}%` }} />
          </div>
          <b>{pct}%</b>
        </div>
      )}
      <ul className="st-roster-list">
        {game.players.map((p) => (
          <li
            key={p.id}
            className={`st-roster${dead.has(p.id) ? " is-dead" : ""}${traitors.has(p.id) ? " is-bad" : ""}`}
          >
            <Portrait
              color={colorOf(st.colors[p.id])}
              size={34}
              dead={dead.has(p.id) && game.status === "playing"}
              bust
            />
            <span>
              {p.name}
              {p.id === meId && <small>bạn</small>}
            </span>
            {traitors.has(p.id) && <em>phản bội</em>}
          </li>
        ))}
      </ul>
      {game.status === "playing" && isPlayer && (
        <div className="game-actions">
          <button type="button" className="btn btn-primary" onClick={() => stationStore.open(messageId)}>
            Quay lại trạm
          </button>
        </div>
      )}
    </div>
  );
}
