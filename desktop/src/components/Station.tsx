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
  findPath,
  roomAt,
  roomOf,
  snapWalkable,
  stationStore,
  walkable,
  type Pt,
  type StFrame,
  type StRoom,
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

/** Chỗ đang tự đi tới. open: tới nơi thì mở luôn máy (nhiệm vụ / chỗ sửa sự cố). */
type Dest = { x: number; y: number; label: string; station?: string; open?: boolean };

const fixIdOf = (sab: StFrame["sab"]) => (sab === "power" ? "fix-power" : sab === "oxygen" ? "fix-oxygen" : "");

/** Mình mở được máy này không (máy chủ vẫn kiểm tra lại). Kẻ phản bội chỉ có nhiệm vụ giả. */
function canUse(s: StStation, f: StFrame): boolean {
  if (s.kind === "fix") return !f.me.ghost && s.id === fixIdOf(f.sab);
  return !f.me.traitor && f.me.tasks.some((t) => t.station === s.id && !t.done);
}

/** Tỉ lệ phóng bản đồ theo kích thước màn hình (px CSS / đơn vị bản đồ). */
const viewScale = (w: number, h: number) => Math.max(0.55, Math.min(1.35, Math.min(w, h) / 720));

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
  const [showHelp, setShowHelp] = useState(false);
  const [sabOpen, setSabOpen] = useState(false);
  const [dest, setDest] = useState<Dest | null>(null);
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

  // hết lượt đi lại (họp, kết thúc) thì thôi tự đi
  useEffect(() => {
    if (phase !== "play") setDest(null);
  }, [phase]);

  // lần đầu vào chơi trên máy này: mở hướng dẫn ngắn
  useEffect(() => {
    if (phase !== "play") return;
    try {
      if (localStorage.getItem("st-help-seen")) return;
      localStorage.setItem("st-help-seen", "1");
    } catch {
      return;
    }
    setShowHelp(true);
  }, [phase]);

  const openStation = (s: StStation) => {
    setDest(null);
    setTask(s);
  };
  const goStation = (s: StStation) => {
    const f = stationStore.frame;
    setDest({ x: s.x, y: s.y, station: s.id, label: s.name, open: !!f && canUse(s, f) });
  };
  const goPoint = (x: number, y: number) => {
    const f = stationStore.frame;
    if (!map || !f) return;
    const q = f.me.ghost ? { x, y } : snapWalkable(map, x, y);
    if (q) setDest({ ...q, label: roomAt(map, q.x, q.y) });
  };
  const onArrive = useCallback((d: Dest) => {
    setDest(null);
    const f = stationStore.frame;
    const s = d.open && d.station ? stationStore.map?.stations.find((x) => x.id === d.station) : undefined;
    if (s && f && f.phase === "play" && canUse(s, f)) setTask(s);
  }, []);
  const onCancelDest = useCallback(() => setDest(null), []);
  const onLost = useCallback(() => {
    setDest(null);
    setToast("Không tìm được đường tới đó.");
  }, []);
  // bấm/chạm vào màn chơi: trúng máy thì đi tới (đang đứng cạnh thì mở luôn), chỗ trống thì đi tới đó
  const onTap = (pt: Pt) => {
    const f = stationStore.frame;
    if (!map || !f || f.phase !== "play" || task) return;
    const fixId = fixIdOf(f.sab);
    const s = map.stations.find((x) => (x.kind !== "fix" || x.id === fixId) && Math.hypot(x.x - pt.x, x.y - pt.y) < 60);
    if (s) {
      if (near.use?.kind === "station" && near.use.station.id === s.id) openStation(s);
      else goStation(s);
      return;
    }
    goPoint(pt.x, pt.y);
  };

  const doUse = () => {
    const u = near.use;
    if (!u) return;
    if (u.kind === "button") void run("emergency").catch(() => {});
    else openStation(u.station);
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
        if (showHelp) setShowHelp(false);
        else if (showMap) setShowMap(false);
        else if (sabOpen) setSabOpen(false);
        else setDest(null);
        return;
      }
      if (phase !== "play" || task) return;
      const k = e.key.toLowerCase();
      if (k === "e" || k === " ") {
        e.preventDefault();
        if (!e.repeat) doUse();
      } else if (k === "r") doReport();
      else if (k === "q") doKill();
      else if (k === "f" && frame?.me.traitor) setSabOpen((v) => !v);
      else if (k === "h") setShowHelp((v) => !v);
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
  const fixId = fixIdOf(frame.sab);
  const fixSt = fixId ? map.stations.find((s) => s.id === fixId) : undefined;
  const fixRoom = fixSt ? map.rooms.find((r) => r.id === fixSt.room) : undefined;
  const here = roomAt(map, me.x, me.y);
  const useLabel =
    near.use?.kind === "button"
      ? `Họp khẩn (${me.emergencies})`
      : near.use?.kind === "station" && near.use.station.kind === "fix"
        ? "Sửa"
        : "Dùng";

  return (
    <div
      className={`st-screen${frame.sab ? " has-sab" : ""}${frame.sab === "oxygen" ? " is-alarm" : ""}${frame.sab === "power" ? " is-dark" : ""}`}
      role="dialog"
      aria-label="Trạm Vũ Trụ"
    >
      <StationCanvas
        map={map}
        meId={meId}
        nameOf={nameOf}
        frozen={frozen}
        dest={dest}
        onNear={setNear}
        onArrive={onArrive}
        onCancelDest={onCancelDest}
        onLost={onLost}
        onTap={onTap}
      />

      {/* thanh trên */}
      <div className="st-top">
        <div className="st-taskbar" title="Tiến độ nhiệm vụ của cả trạm">
          <div className="st-taskbar-label">
            <span>Nhiệm vụ toàn trạm</span>
            <b>{pct}%</b>
          </div>
          <div className="st-bar">
            <i style={{ width: `${pct}%` }} />
          </div>
        </div>
        <div className="st-top-actions">
          <button type="button" className="st-icon" onClick={() => setShowHelp((v) => !v)} title="Cách chơi (H)">
            <StIcon name="help" />
          </button>
          <button type="button" className="st-icon" onClick={onClose} title="Thu nhỏ, vẫn ở trong ván">
            <StIcon name="min" />
          </button>
        </div>
      </div>

      {/* bản đồ nhỏ góc phải: phòng đang đứng, nhiệm vụ, chỗ có sự cố */}
      {phase === "play" && (
        <button type="button" className="st-minimap" onClick={() => setShowMap(true)} title="Mở bản đồ lớn (M)">
          <MapSvg map={map} frame={frame} dest={dest} />
          <span className="st-minimap-room">
            <StIcon name="pin" />
            {here}
          </span>
        </button>
      )}

      {/* sự cố: ở đâu, còn bao lâu, bấm để tự đi tới */}
      {frame.sab && fixSt && phase === "play" && (
        <button
          type="button"
          className={`st-sab-banner is-${frame.sab}`}
          onClick={() => !me.ghost && goStation(fixSt)}
          disabled={me.ghost}
        >
          <span className="st-sab-icon" aria-hidden>
            <StIcon name={frame.sab === "power" ? "bolt" : "alarm"} />
          </span>
          <span className="st-sab-text">
            <b>{frame.sab === "power" ? "Mất điện toàn trạm" : "Rò rỉ oxy"}</b>
            <small>
              Tới <strong>{fixRoom?.name}</strong> · {fixSt.name}
            </small>
          </span>
          {frame.sab === "oxygen" && <em>{Math.ceil((frame.sab_in ?? 0) / 1000)}s</em>}
          {!me.ghost && (
            <i className="st-sab-go">
              <StIcon name="go" />
              {dest?.station === fixSt.id ? "Đang tới" : "Chỉ đường"}
            </i>
          )}
        </button>
      )}

      {/* danh sách nhiệm vụ: bấm một dòng là tự đi tới */}
      <aside className={`st-tasks${tasksOpen ? "" : " is-closed"}${me.traitor ? " is-bad" : ""}`}>
        <button
          type="button"
          className="st-tasks-head"
          onClick={() => setTasksOpen((v) => !v)}
          aria-expanded={tasksOpen}
        >
          <span>{me.traitor ? "Nhiệm vụ giả" : me.ghost ? "Việc còn dang dở" : "Nhiệm vụ"}</span>
          {!me.traitor && (
            <b>
              {myDone}/{me.tasks.length}
            </b>
          )}
          <StIcon name={tasksOpen ? "up" : "down"} />
        </button>
        {tasksOpen && me.traitor && (
          <p className="st-bad-hint">
            Không làm thật được: đứng cạnh máy vài giây để giả vờ. Hạ gục, phá hoại, đừng để lộ.
          </p>
        )}
        <ul hidden={!tasksOpen}>
          {me.tasks.map((t) => {
            const s = map.stations.find((x) => x.id === t.station);
            const going = dest?.station === t.station;
            return (
              <li key={t.station}>
                <button
                  type="button"
                  className={`st-task-row${t.done ? " is-done" : ""}${going ? " is-going" : ""}`}
                  disabled={t.done || phase !== "play" || !s}
                  onClick={() => s && (going ? setDest(null) : goStation(s))}
                  title={going ? "Đang đi tới, bấm để dừng" : "Bấm để tự đi tới"}
                >
                  <i aria-hidden>{t.done ? <StIcon name="check" /> : null}</i>
                  <span>
                    {s?.name}
                    <small>{roomAt(map, s?.x ?? 0, s?.y ?? 0)}</small>
                  </span>
                  {!t.done && (
                    <em className="st-go">
                      {going ? <span className="st-going">đang tới</span> : <StIcon name="go" />}
                    </em>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      </aside>

      {/* nút hành động */}
      {phase === "play" && (
        <div className="st-actions">
          <ActButton
            kind={
              near.use?.kind === "button" || (near.use?.kind === "station" && near.use.station.kind === "fix")
                ? "fix"
                : "use"
            }
            icon={
              near.use?.kind === "button"
                ? "alarm"
                : near.use?.kind === "station" && near.use.station.kind === "fix"
                  ? "wrench"
                  : "hand"
            }
            label={useLabel}
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
                keyHint="F"
              />
              {sabOpen && (
                <div className="st-sab-menu" role="menu">
                  {(
                    [
                      ["power", "Cắt điện", "Thuyền viên gần như không nhìn thấy gì cho tới khi sửa ở Phòng điện"],
                      ["oxygen", "Xả oxy", "45 giây không ai sửa van ở Trạm oxy là phe bạn thắng"],
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

      {showMap && (
        <BigMap
          map={map}
          frame={frame}
          dest={dest}
          onPick={(pt, s) => {
            setShowMap(false);
            if (phase !== "play") return;
            if (s) goStation(s);
            else goPoint(pt.x, pt.y);
          }}
          onClose={() => setShowMap(false)}
        />
      )}

      {showHelp && <Help traitor={me.traitor} touch={isTouchDevice()} onClose={() => setShowHelp(false)} />}

      {task && (
        <TaskGame
          station={task}
          roomName={map.rooms.find((r) => r.id === task.room)?.name ?? ""}
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
        <p className="st-ghost-tag">
          {me.traitor
            ? "Bạn đã thành hồn ma: vẫn phá hoại được để giúp đồng bọn."
            : "Bạn đã thành hồn ma: đi xuyên tường được, hãy làm nốt nhiệm vụ."}
        </p>
      )}
    </div>
  );
}

function Help({ traitor, touch, onClose }: { traitor: boolean; touch: boolean; onClose: () => void }) {
  return (
    <div className="st-help" onClick={onClose}>
      <div className="st-help-card" role="dialog" aria-label="Cách chơi" onClick={(e) => e.stopPropagation()}>
        <header>
          <h3>Cách chơi</h3>
          <button type="button" className="st-icon" onClick={onClose} aria-label="Đóng">
            <StIcon name="x" />
          </button>
        </header>
        <dl>
          <dt>Đi lại</dt>
          <dd>
            {touch
              ? "Kéo cần gạt bên trái, hoặc chạm giữ màn hình theo hướng muốn đi."
              : "W A S D / phím mũi tên, hoặc giữ chuột trên màn hình theo hướng muốn đi."}
          </dd>
          <dt>Tự đi tới</dt>
          <dd>
            {touch ? "Chạm" : "Bấm"} vào một chỗ, vào một nhiệm vụ trong danh sách, hoặc vào bản đồ: nhân vật tự tìm
            đường. Tới nhiệm vụ là máy tự mở.
          </dd>
          <dt>Dùng / Sửa</dt>
          <dd>
            Đứng cạnh máy sáng <b className="is-amber">vàng</b> (nhiệm vụ) hoặc <b className="is-red">đỏ</b> (sự cố) rồi
            bấm {touch ? "nút Dùng" : "E hoặc Space"}.
          </dd>
          <dt>Sự cố</dt>
          <dd>Thanh đỏ phía trên cho biết phòng nào cần sửa. Bấm vào thanh đó để được chỉ đường.</dd>
          <dt>Báo xác · Họp</dt>
          <dd>
            Thấy xác thì báo ({touch ? "nút Báo xác" : "R"}). Chuông đỏ ở Sảnh trung tâm gọi họp khẩn. Bản đồ lớn:{" "}
            {touch ? "chạm bản đồ nhỏ" : "M"}.
          </dd>
          {traitor && (
            <>
              <dt className="is-red">Kẻ phản bội</dt>
              <dd>
                Nhiệm vụ của bạn là giả, không làm được: đứng cạnh máy để giả vờ. Hạ gục ({touch ? "nút đỏ" : "Q"}) khi
                không ai thấy, phá hoại ({touch ? "nút tím" : "F"}) để chia lẻ thuyền viên.
              </dd>
            </>
          )}
        </dl>
        <button type="button" className="st-btn is-primary" onClick={onClose}>
          Vào trạm
        </button>
      </div>
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
  help: "M9.1 9a3 3 0 0 1 5.8 1c0 2-3 2.5-3 4.5M12 17.5h.01M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Z",
  go: "M4 11.5 20 4l-7.5 16-2-6.5-6.5-2Z",
  pin: "M12 21s-6.5-5.8-6.5-11a6.5 6.5 0 0 1 13 0c0 5.2-6.5 11-6.5 11ZM12 12a2.2 2.2 0 1 0 0-4.4 2.2 2.2 0 0 0 0 4.4Z",
  x: "M6 6l12 12M18 6 6 18",
  wrench:
    "M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.8-3.8a6 6 0 0 1-7.9 7.9l-6.9 6.9a2.1 2.1 0 0 1-3-3l6.9-6.9a6 6 0 0 1 7.9-7.9l-3.8 3.8Z",
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

type CanvasProps = {
  map: StationMap;
  meId: number;
  nameOf: (id: number) => string;
  frozen: boolean;
  dest: Dest | null;
  onNear: (n: Near) => void;
  onArrive: (d: Dest) => void;
  onCancelDest: () => void;
  onLost: () => void;
  onTap: (p: Pt) => void;
};

type Pointer = { id: number; x: number; y: number; sx: number; sy: number; t: number; steer: boolean };

/**
 * Bước đi có trượt tường: chặn một trục thì đi tiếp trục kia; đâm vào mép cửa thì tự né sang
 * bên để lọt vào hành lang (khỏi phải căn từng li).
 */
function stepMove(p: Pt, dx: number, dy: number, step: number, ok: (x: number, y: number) => boolean) {
  const nx = p.x + dx * step;
  const ny = p.y + dy * step;
  if (ok(nx, ny)) {
    p.x = nx;
    p.y = ny;
    return;
  }
  const alongX = Math.abs(dx) >= Math.abs(dy);
  if (alongX ? ok(nx, p.y) : ok(p.x, ny)) {
    if (alongX) p.x = nx;
    else p.y = ny;
    return;
  }
  if (alongX ? dy && ok(p.x, ny) : dx && ok(nx, p.y)) {
    if (alongX) p.y = ny;
    else p.x = nx;
    return;
  }
  // né mép cửa: tìm khe gần nhất theo trục vuông góc (ưu tiên phía đang nghiêng tới)
  const lean = alongX ? Math.sign(dy) : Math.sign(dx);
  for (let o = 4; o <= 34; o += 3) {
    for (const s of lean ? [lean, -lean] : [1, -1]) {
      const free = alongX ? ok(nx, p.y + s * o) : ok(p.x + s * o, ny);
      if (!free) continue;
      const shift = s * Math.min(step, o);
      if (alongX ? ok(p.x, p.y + shift) : ok(p.x + shift, p.y)) {
        if (alongX) p.y += shift;
        else p.x += shift;
        return;
      }
    }
  }
}

function StationCanvas(props: CanvasProps) {
  const { map, meId } = props;
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const keys = useRef(new Set<string>());
  const stick = useRef({ x: 0, y: 0 });
  const pointer = useRef<Pointer | null>(null);
  const pos = useRef<{ x: number; y: number; correction: number } | null>(null);
  const live = useRef(props);
  live.current = props;
  // cần gạt: máy cảm ứng, hoặc ngay khi có lần chạm đầu tiên (laptop màn cảm ứng, trình giả lập)
  const [touch, setTouch] = useState(() => isTouchDevice() || navigator.maxTouchPoints > 0);
  const touchRef = useRef(touch);
  touchRef.current = touch;
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
    const blur = () => {
      keys.current.clear();
      pointer.current = null;
    };
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
    // đường tự đi: dest là đích đang theo (so theo tham chiếu), path các điểm rẽ còn lại
    const route = { dest: null as Dest | null, active: false, path: [] as Pt[], lx: 0, ly: 0, still: 0, retries: 0 };
    const plan = (p: Pt, d: Dest, ghost: boolean) => (ghost ? [{ x: d.x, y: d.y }] : findPath(map, p, d));

    const loop = (now: number) => {
      raf = requestAnimationFrame(loop);
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      const f = stationStore.frame;
      if (!f) return;
      const me = f.me;
      const L = live.current;

      // vị trí của mình: tự đi trên máy, máy chủ chỉnh lại khi cần
      let p = pos.current;
      if (!p || p.correction !== me.correction || f.phase !== "play") {
        p = pos.current = { x: me.x, y: me.y, correction: me.correction };
      }

      // đích mới (hoặc đã huỷ)
      if (L.dest !== route.dest) {
        route.dest = L.dest;
        route.retries = 0;
        route.still = 0;
        route.path = L.dest && f.phase === "play" ? (plan(p, L.dest, me.ghost) ?? []) : [];
        route.active = route.path.length > 0;
        if (L.dest && !route.active) L.onLost();
      }

      if (!L.frozen && f.phase === "play") {
        let dx = 0;
        let dy = 0;
        const k = keys.current;
        if (k.has("arrowleft") || k.has("a")) dx -= 1;
        if (k.has("arrowright") || k.has("d")) dx += 1;
        if (k.has("arrowup") || k.has("w")) dy -= 1;
        if (k.has("arrowdown") || k.has("s")) dy += 1;
        dx += stick.current.x;
        dy += stick.current.y;
        // giữ chuột / ngón tay trên màn hình: đi theo hướng từ giữa màn hình tới đó
        const ptr = pointer.current;
        if (ptr && !ptr.steer && (now - ptr.t > 200 || Math.hypot(ptr.x - ptr.sx, ptr.y - ptr.sy) > 14))
          ptr.steer = true;
        if (ptr?.steer) {
          const r = canvas.getBoundingClientRect();
          const vx = ptr.x - (r.left + r.width / 2);
          const vy = ptr.y - (r.top + r.height / 2);
          const l = Math.hypot(vx, vy);
          if (l > 18) {
            dx += (vx / l) * Math.min(1, l / 90);
            dy += (vy / l) * Math.min(1, l / 90);
          }
        }
        let len = Math.hypot(dx, dy);
        // tự điều khiển thì thôi tự đi
        if (len > 0.05 && route.active) {
          route.active = false;
          route.path = [];
          L.onCancelDest();
        }
        let cap = Infinity;
        if (len <= 0.05 && route.active && route.dest) {
          const d = route.dest;
          const reach = d.station ? RANGE.use * 0.45 : 12;
          while (route.path.length > 1 && Math.hypot(route.path[0].x - p.x, route.path[0].y - p.y) < 8)
            route.path.shift();
          const t = route.path[0];
          const l = t ? Math.hypot(t.x - p.x, t.y - p.y) : 0;
          if (Math.hypot(d.x - p.x, d.y - p.y) < reach || !t || (route.path.length === 1 && l < 2)) {
            route.active = false;
            route.path = [];
            L.onArrive(d);
          } else {
            dx = (t.x - p.x) / l;
            dy = (t.y - p.y) / l;
            len = 1;
            cap = l; // không đi lố điểm rẽ
          }
        }
        if (len > 0.05) {
          const step = Math.min(cap, (map.speed * dt) / Math.max(1, len));
          const ok = (x: number, y: number) =>
            me.ghost ? x > 0 && y > 0 && x < map.w && y < map.h : walkable(map, x, y);
          stepMove(p, dx, dy, step, ok);
          if (route.active && route.dest) {
            // kẹt (bị ai chắn, máy chủ kéo lại…): tìm đường lại vài lần rồi thôi
            route.still = Math.hypot(p.x - route.lx, p.y - route.ly) < 0.3 ? route.still + dt : 0;
            if (route.still > 0.6) {
              route.still = 0;
              const again = ++route.retries <= 2 ? plan(p, route.dest, me.ghost) : null;
              if (again?.length) route.path = again;
              else {
                route.active = false;
                route.path = [];
                L.onLost();
              }
            }
          }
          route.lx = p.x;
          route.ly = p.y;
        }
        if (now - lastSent > 66 && (p.x !== sentX || p.y !== sentY)) {
          stationStore.sendPos(p.x, p.y);
          sentX = p.x;
          sentY = p.y;
          lastSent = now;
        }
      }

      // những thứ ở gần (để bật nút Dùng / Báo xác / Hạ gục)
      const near: Near = {};
      let fake: StStation | undefined;
      if (f.phase === "play") {
        if (f.sab) {
          const fix = map.stations.find((s) => s.id === fixIdOf(f.sab));
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
              // kẻ phản bội: nhiệm vụ giả, chỉ đứng giả vờ
              if (me.traitor) fake = s;
              else near.use = { kind: "station", station: s };
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
        L.onNear(near);
      }

      // nhãn gợi ý nổi trên đầu máy / xác ở gần
      const kb = !touchRef.current;
      let prompt: Prompt | undefined;
      if (near.use?.kind === "station") {
        const s = near.use.station;
        prompt = {
          x: s.x,
          y: s.y - 62,
          key: kb ? "E" : "",
          text: s.kind === "fix" ? `Sửa: ${s.name}` : s.name,
          tone: s.kind === "fix" ? "alarm" : "use",
        };
      } else if (near.use?.kind === "button") {
        prompt = { x: map.button[0], y: map.button[1] - 72, key: kb ? "E" : "", text: "Gọi họp khẩn", tone: "alarm" };
      } else if (near.report) {
        const b = f.bodies.find((x) => x.id === near.report);
        if (b) prompt = { x: b.x, y: b.y - 54, key: kb ? "R" : "", text: "Báo xác", tone: "report" };
      } else if (fake) {
        prompt = { x: fake.x, y: fake.y - 62, key: "", text: `Giả vờ làm: ${fake.name}`, tone: "fake" };
      }
      const fixSt = f.sab ? map.stations.find((s) => s.id === fixIdOf(f.sab)) : undefined;
      draw(ctx, canvas, map, f, p, now, meId, L.nameOf, stars, anim, {
        prompt,
        route: route.active ? route.path : [],
        dest: route.active ? route.dest : null,
        alarmRoom: fixSt ? map.rooms.find((r) => r.id === fixSt.room) : undefined,
      });
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

  const release = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const ptr = pointer.current;
    if (!ptr || ptr.id !== e.pointerId) return;
    pointer.current = null;
    const p = pos.current;
    if (ptr.steer || !p || e.type === "pointercancel") return;
    // chạm nhanh: đổi toạ độ màn hình ra toạ độ bản đồ
    const r = e.currentTarget.getBoundingClientRect();
    const k = viewScale(r.width, r.height);
    live.current.onTap({
      x: p.x + (e.clientX - r.left - r.width / 2) / k,
      y: p.y + (e.clientY - r.top - r.height / 2) / k,
    });
  };

  return (
    <>
      <canvas
        ref={canvasRef}
        className="st-canvas"
        aria-hidden
        onPointerDown={(e) => {
          if (e.pointerType === "mouse" && e.button !== 0) return;
          e.currentTarget.setPointerCapture(e.pointerId);
          const t = performance.now();
          pointer.current = {
            id: e.pointerId,
            x: e.clientX,
            y: e.clientY,
            sx: e.clientX,
            sy: e.clientY,
            t,
            steer: false,
          };
        }}
        onPointerMove={(e) => {
          const ptr = pointer.current;
          if (ptr && ptr.id === e.pointerId) {
            ptr.x = e.clientX;
            ptr.y = e.clientY;
          }
        }}
        onPointerUp={release}
        onPointerCancel={release}
        onContextMenu={(e) => e.preventDefault()}
      />
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

type Prompt = { x: number; y: number; key: string; text: string; tone: "use" | "alarm" | "report" | "fake" };
type Hud = { prompt?: Prompt; route: Pt[]; dest: Pt | null; alarmRoom?: StRoom };
const TONE: Record<Prompt["tone"], string> = { use: "#ffc94d", alarm: "#ff4d6d", report: "#ff9f45", fake: "#b48cff" };

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
  hud: Hud,
) {
  const W = canvas.width;
  const H = canvas.height;
  // tỉ lệ theo kích thước màn hình tính bằng px CSS (điện thoại không bị thu quá nhỏ)
  const dpr = W / Math.max(1, canvas.clientWidth);
  const scale = dpr * viewScale(canvas.clientWidth, canvas.clientHeight);
  const dark = f.sab === "power" && !f.me.traitor && !f.me.ghost;

  ctx.setTransform(1, 0, 0, 1, 0, 0);
  drawSpace(ctx, W, H, me.x * scale, me.y * scale, stars);
  ctx.setTransform(scale, 0, 0, scale, W / 2 - me.x * scale, H / 2 - me.y * scale);
  const view = { x: me.x - W / 2 / scale, y: me.y - H / 2 / scale, w: W / scale, h: H / scale };
  drawWorld(ctx, map, view, now, dark);

  // phòng đang có sự cố: viền đỏ nhấp nháy
  if (hud.alarmRoom) {
    const r = hud.alarmRoom;
    const k = 0.5 + 0.5 * Math.sin(now / 220);
    ctx.save();
    ctx.fillStyle = `rgba(255, 40, 70, ${0.04 + 0.08 * k})`;
    ctx.strokeStyle = `rgba(255, 77, 109, ${0.45 + 0.45 * k})`;
    ctx.lineWidth = 8;
    ctx.beginPath();
    ctx.roundRect(r.x + 4, r.y + 4, r.w - 8, r.h - 8, 16);
    ctx.fill();
    ctx.stroke();
    ctx.restore();
  }

  drawButton(ctx, map.button[0], map.button[1], now, !f.sab && f.me.emergencies > 0 && !f.me.ghost);

  // trạm làm việc
  const mine = new Set(f.me.tasks.filter((t) => !t.done).map((t) => t.station));
  const fixId = fixIdOf(f.sab);
  for (const s of map.stations) {
    if (s.kind === "fix" && s.id !== fixId) continue;
    const state = s.id === fixId ? "alarm" : mine.has(s.id) ? "task" : "idle";
    drawStation(ctx, s, now, state, roomAccent(s.room));
  }

  // đường tự đi: chấm vàng chạy trên sàn tới đích
  if (hud.route.length) {
    const end = hud.route[hud.route.length - 1];
    ctx.save();
    ctx.strokeStyle = "rgba(255, 201, 77, 0.8)";
    ctx.lineWidth = 8;
    ctx.lineCap = "round";
    ctx.setLineDash([0.1, 24]);
    ctx.lineDashOffset = -now / 30;
    ctx.beginPath();
    ctx.moveTo(me.x, me.y + 18);
    for (const q of hud.route) ctx.lineTo(q.x, q.y + 18);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.lineWidth = 4;
    const k = 0.5 + 0.5 * Math.sin(now / 200);
    ctx.globalAlpha = 0.5 + 0.5 * k;
    ctx.beginPath();
    ctx.ellipse(end.x, end.y + 18, 26 + 6 * k, 11 + 3 * k, 0, 0, Math.PI * 2);
    ctx.stroke();
    ctx.restore();
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

  const toScreen = (x: number, y: number) => ({ x: W / 2 + (x - me.x) * scale, y: H / 2 + (y - me.y) * scale });

  // nhãn gợi ý (vẽ theo px màn hình để chữ luôn đọc được)
  if (hud.prompt) {
    const s = toScreen(hud.prompt.x, hud.prompt.y);
    drawPrompt(ctx, hud.prompt, s.x, s.y, dpr, now);
  }

  // mũi tên ở mép màn hình chỉ tới nhiệm vụ / chỗ sửa sự cố / chỗ đang tự đi tới
  if (f.phase === "play") {
    const targets: { x: number; y: number; color: string }[] = map.stations
      .filter((s) => mine.has(s.id) || s.id === fixId)
      .map((s) => ({ x: s.x, y: s.y, color: s.id === fixId ? "#ff4d6d" : f.me.traitor ? "#b48cff" : "#ffc94d" }));
    if (hud.dest) targets.push({ x: hud.dest.x, y: hud.dest.y, color: "#5ee1ff" });
    for (const t of targets) {
      const { x: sx, y: sy } = toScreen(t.x, t.y);
      const m = 46 * dpr;
      if (sx > m && sx < W - m && sy > m && sy < H - m) continue;
      const ang = Math.atan2(sy - H / 2, sx - W / 2);
      const k = Math.min((W / 2 - m) / Math.abs(Math.cos(ang) || 1e-6), (H / 2 - m) / Math.abs(Math.sin(ang) || 1e-6));
      ctx.save();
      ctx.translate(W / 2 + Math.cos(ang) * k, H / 2 + Math.sin(ang) * k);
      ctx.rotate(ang);
      ctx.scale(dpr, dpr);
      ctx.fillStyle = t.color;
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

function drawPrompt(ctx: CanvasRenderingContext2D, pr: Prompt, sx: number, sy: number, dpr: number, now: number) {
  const col = TONE[pr.tone];
  ctx.save();
  ctx.setTransform(dpr, 0, 0, dpr, sx, sy + Math.sin(now / 300) * 2 * dpr);
  ctx.font = "700 14px 'Chakra Petch', 'Be Vietnam Pro', sans-serif";
  ctx.textBaseline = "middle";
  const tw = ctx.measureText(pr.text).width;
  const kw = pr.key ? 28 : 0;
  const w = tw + kw + 24;
  const h = 32;
  ctx.fillStyle = "rgba(8, 13, 26, 0.9)";
  ctx.strokeStyle = col;
  ctx.lineWidth = 2;
  ctx.beginPath();
  ctx.roundRect(-w / 2, -h / 2, w, h, 9);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = col;
  ctx.beginPath();
  ctx.moveTo(-7, h / 2);
  ctx.lineTo(7, h / 2);
  ctx.lineTo(0, h / 2 + 8);
  ctx.closePath();
  ctx.fill();
  let x = -w / 2 + 12;
  if (pr.key) {
    ctx.beginPath();
    ctx.roundRect(x, -11, 22, 22, 5);
    ctx.fill();
    ctx.fillStyle = "#0b1220";
    ctx.textAlign = "center";
    ctx.fillText(pr.key, x + 11, 1);
    x += kw;
  }
  ctx.fillStyle = "#f1f5fb";
  ctx.textAlign = "left";
  ctx.fillText(pr.text, x, 1);
  ctx.restore();
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

/* ---------- Bản đồ: góc màn hình (luôn hiện) và bản đồ lớn (bấm để tự đi tới) ---------- */

function MapSvg({
  map,
  frame,
  dest,
  big,
  onPick,
}: {
  map: StationMap;
  frame: StFrame;
  dest: Dest | null;
  big?: boolean;
  onPick?: (p: Pt, s?: StStation) => void;
}) {
  const me = frame.me;
  const here = roomOf(map, me.x, me.y)?.id;
  const fixId = fixIdOf(frame.sab);
  const fix = fixId ? map.stations.find((s) => s.id === fixId) : undefined;
  const todo = new Set(me.tasks.filter((t) => !t.done).map((t) => t.station));
  const k = big ? 1 : 1.7; // bản đồ góc nhỏ: chấm to hơn cho dễ thấy
  const pick = (e: React.MouseEvent<SVGSVGElement>) => {
    const m = e.currentTarget.getScreenCTM();
    if (!onPick || !m) return;
    const q = new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse());
    const s = map.stations.find((x) => (todo.has(x.id) || x.id === fixId) && Math.hypot(x.x - q.x, x.y - q.y) < 90);
    onPick({ x: q.x, y: q.y }, s);
  };
  const X = dest ? 34 * k : 0;
  return (
    <svg
      viewBox={`-30 -30 ${map.w + 60} ${map.h + 60}`}
      className={`st-mm${big ? " is-big" : ""}`}
      onClick={onPick ? pick : undefined}
      role="img"
      aria-label="Bản đồ trạm"
    >
      {map.halls.map((h, i) => (
        <rect key={i} x={h.X} y={h.Y} width={h.W} height={h.H} className="st-mm-hall" />
      ))}
      {map.rooms.map((r) => (
        <rect
          key={r.id}
          x={r.x}
          y={r.y}
          width={r.w}
          height={r.h}
          rx={24}
          className={`st-mm-room${r.id === here ? " is-here" : ""}${r.id === fix?.room ? " is-alarm" : ""}`}
          style={{ ["--a" as string]: roomAccent(r.id) }}
        />
      ))}
      {big &&
        map.rooms.map((r) => (
          <text key={r.id} x={r.x + r.w / 2} y={r.y + r.h - 56} className="st-mm-name">
            {r.name}
          </text>
        ))}
      {big && <circle cx={map.button[0]} cy={map.button[1]} r={34} className="st-mm-button" />}
      {map.stations
        .filter((s) => todo.has(s.id))
        .map((s) => (
          <circle key={s.id} cx={s.x} cy={s.y} r={30 * k} className={`st-mm-task${me.traitor ? " is-fake" : ""}`} />
        ))}
      {fix && (
        <g className="st-mm-fix">
          <circle cx={fix.x} cy={fix.y} r={38 * k} className="st-mm-ping" />
          <circle cx={fix.x} cy={fix.y} r={38 * k} />
        </g>
      )}
      {dest && (
        <path
          d={`M${dest.x - X} ${dest.y - X}L${dest.x + X} ${dest.y + X}M${dest.x + X} ${dest.y - X}L${dest.x - X} ${dest.y + X}`}
          className="st-mm-dest"
          style={{ strokeWidth: 16 * k }}
        />
      )}
      <circle
        cx={me.x}
        cy={me.y}
        r={40 * k}
        fill={colorOf(me.color)}
        className="st-mm-me"
        style={{ strokeWidth: 10 * k }}
      />
    </svg>
  );
}

function BigMap({
  map,
  frame,
  dest,
  onPick,
  onClose,
}: {
  map: StationMap;
  frame: StFrame;
  dest: Dest | null;
  onPick: (p: Pt, s?: StStation) => void;
  onClose: () => void;
}) {
  return (
    <div className="st-map" onClick={onClose}>
      <div className="st-map-card" role="dialog" aria-label="Bản đồ trạm" onClick={(e) => e.stopPropagation()}>
        <header>
          <h3>Bản đồ Trạm Hải Đăng</h3>
          <button type="button" className="st-icon" onClick={onClose} aria-label="Đóng bản đồ">
            <StIcon name="x" />
          </button>
        </header>
        <MapSvg map={map} frame={frame} dest={dest} big onPick={frame.phase === "play" ? onPick : undefined} />
        <ul className="st-map-legend">
          <li>
            <i style={{ background: colorOf(frame.me.color) }} /> Bạn
          </li>
          <li>
            <i className={frame.me.traitor ? "is-fake" : "is-task"} />{" "}
            {frame.me.traitor ? "Nhiệm vụ giả" : "Nhiệm vụ của bạn"}
          </li>
          {frame.sab && (
            <li>
              <i className="is-fix" /> Chỗ sửa sự cố
            </li>
          )}
          <li>
            <i className="is-button" /> Chuông họp khẩn
          </li>
        </ul>
        <p className="st-muted">Bấm vào một chỗ trên bản đồ để nhân vật tự đi tới.</p>
      </div>
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

const KIND_HINT: Record<StStation["kind"], string> = {
  wires: "Nối dây",
  hold: "Giữ nút",
  code: "Nhập mã",
  align: "Căn chỉnh",
  scan: "Quét",
  fix: "Sửa sự cố",
};

/**
 * Lưu ý: màn chơi vẽ lại ~15 lần/giây (mỗi frame), nên mọi callback truyền xuống trò nhỏ đều
 * đổi tham chiếu liên tục. Trò nhỏ giữ callback qua ref để bộ đếm không bị khởi động lại
 * (lỗi cũ: nhiệm vụ "giữ nút", quét và sửa sự cố không bao giờ đầy).
 */
function useLatest<T>(v: T) {
  const r = useRef(v);
  r.current = v;
  return r;
}

function TaskGame({
  station,
  roomName,
  run,
  onClose,
}: {
  station: StStation;
  roomName: string;
  run: (action: string, data?: unknown) => Promise<void>;
  onClose: (done: boolean) => void;
}) {
  const started = useRef(performance.now());
  const [state, setState] = useState<"starting" | "ready" | "sending">("starting");
  const runRef = useLatest(run);
  const closeRef = useLatest(onClose);
  const finished = useRef(false);

  useEffect(() => {
    runRef
      .current("use", { station: station.id })
      .then(() => setState("ready"))
      .catch(() => closeRef.current(true));
  }, [station.id, runRef, closeRef]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && !finished.current && closeRef.current(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [closeRef]);

  const finish = useCallback(() => {
    if (finished.current) return;
    finished.current = true;
    setState("sending");
    // đợi đủ thời gian tối thiểu máy chủ yêu cầu
    const wait = Math.max(0, MIN_TIME[station.kind] + 200 - (performance.now() - started.current));
    setTimeout(() => {
      runRef
        .current(station.kind === "fix" ? "fix" : "task", { station: station.id })
        .then(() => setTimeout(() => closeRef.current(true), 450))
        .catch(() => closeRef.current(true));
    }, wait);
  }, [station.id, station.kind, runRef, closeRef]);

  const body =
    state === "starting" ? (
      <p className="st-muted st-task-wait">Đang mở máy…</p>
    ) : station.kind === "wires" ? (
      <Wires onDone={finish} />
    ) : station.kind === "code" ? (
      <Keypad onDone={finish} />
    ) : station.kind === "align" ? (
      <Align onDone={finish} />
    ) : station.kind === "scan" ? (
      <Hold onDone={finish} ms={6200} auto label="Đang quét" />
    ) : (
      <Hold
        onDone={finish}
        ms={station.kind === "fix" ? 2700 : 3200}
        label={station.kind === "fix" ? "Giữ để sửa" : "Nhấn giữ"}
        tone={station.kind === "fix" ? "red" : "amber"}
      />
    );

  return (
    <div className="st-task-backdrop" onPointerDown={(e) => e.target === e.currentTarget && closeRef.current(false)}>
      <div className={`st-task st-task-${station.kind}`} role="dialog" aria-label={station.name}>
        <header>
          <div>
            <small>
              {roomName} · {KIND_HINT[station.kind]}
            </small>
            <h3>{station.name}</h3>
          </div>
          <button type="button" className="st-icon" onClick={() => onClose(false)} aria-label="Đóng">
            <StIcon name="x" />
          </button>
        </header>
        {state === "sending" ? (
          <p className="st-done">
            <StIcon name="check" /> Xong!
          </p>
        ) : (
          body
        )}
      </div>
    </div>
  );
}

const WIRE_COLORS = ["#e5484d", "#3e8ef7", "#f5b82e", "#b06cf2"];
const WIRE_Y = (i: number) => 38 + i * 58;
const WIRE_L = 52;
const WIRE_R = 268;

function Wires({ onDone }: { onDone: () => void }) {
  const right = useMemo(() => [0, 1, 2, 3].sort(() => Math.random() - 0.5), []);
  const [links, setLinks] = useState<number[]>([]);
  const [sel, setSel] = useState<number | null>(null);
  const [cursor, setCursor] = useState<Pt | null>(null);
  const [miss, setMiss] = useState(false);
  const svgRef = useRef<SVGSVGElement>(null);
  const done = useLatest(onDone);
  useEffect(() => {
    if (links.length === 4) done.current();
  }, [links, done]);

  const toSvg = (e: React.PointerEvent): Pt => {
    const r = svgRef.current!.getBoundingClientRect();
    return { x: ((e.clientX - r.left) / r.width) * 320, y: ((e.clientY - r.top) / r.height) * 250 };
  };
  const leftAt = (q: Pt) => [0, 1, 2, 3].find((i) => Math.abs(q.y - WIRE_Y(i)) < 26 && q.x < WIRE_L + 40);
  const rightAt = (q: Pt) => [0, 1, 2, 3].find((i) => Math.abs(q.y - WIRE_Y(i)) < 26 && q.x > WIRE_R - 40);
  const connect = (c: number, row: number) => {
    setSel(null);
    setCursor(null);
    if (right[row] === c) setLinks((l) => (l.includes(c) ? l : [...l, c]));
    else {
      setMiss(true);
      setTimeout(() => setMiss(false), 350);
    }
  };

  return (
    <div className="st-wires">
      <svg
        ref={svgRef}
        viewBox="0 0 320 250"
        className={`st-wires-board${miss ? " is-miss" : ""}`}
        onPointerDown={(e) => {
          const q = toSvg(e);
          const li = leftAt(q);
          if (li !== undefined && !links.includes(li)) {
            e.currentTarget.setPointerCapture(e.pointerId);
            setSel(li);
            setCursor(q);
            return;
          }
          const ri = rightAt(q);
          if (ri !== undefined && sel !== null) connect(sel, ri);
        }}
        onPointerMove={(e) => sel !== null && cursor && setCursor(toSvg(e))}
        onPointerUp={(e) => {
          if (sel === null || !cursor) return;
          const q = toSvg(e);
          const ri = rightAt(q);
          if (ri !== undefined) connect(sel, ri);
          else if (leftAt(q) === sel)
            setCursor(null); // bấm (không kéo): chờ bấm đầu bên phải
          else {
            setSel(null);
            setCursor(null);
          }
        }}
      >
        <rect x="0" y="0" width="320" height="250" rx="14" className="st-wires-bg" />
        {[0, 1, 2, 3].map((i) => (
          <g key={`l${i}`}>
            <rect x="0" y={WIRE_Y(i) - 9} width={WIRE_L} height="18" fill={WIRE_COLORS[i]} />
            <circle
              cx={WIRE_L}
              cy={WIRE_Y(i)}
              r="15"
              fill={WIRE_COLORS[i]}
              className={`st-port${sel === i ? " is-sel" : ""}${links.includes(i) ? " is-done" : ""}`}
            />
          </g>
        ))}
        {right.map((c, row) => (
          <g key={`r${row}`}>
            <rect x={WIRE_R} y={WIRE_Y(row) - 9} width={320 - WIRE_R} height="18" fill={WIRE_COLORS[c]} />
            <circle cx={WIRE_R} cy={WIRE_Y(row)} r="15" fill={WIRE_COLORS[c]} className="st-port" />
            <circle cx="306" cy={WIRE_Y(row) - 20} r="4" className={`st-led${links.includes(c) ? " is-on" : ""}`} />
          </g>
        ))}
        {links.map((c) => {
          const y1 = WIRE_Y(c);
          const y2 = WIRE_Y(right.indexOf(c));
          return (
            <path
              key={c}
              d={`M${WIRE_L} ${y1}C${WIRE_L + 90} ${y1} ${WIRE_R - 90} ${y2} ${WIRE_R} ${y2}`}
              stroke={WIRE_COLORS[c]}
              className="st-wire-line"
            />
          );
        })}
        {sel !== null && cursor && (
          <line
            x1={WIRE_L}
            y1={WIRE_Y(sel)}
            x2={cursor.x}
            y2={cursor.y}
            stroke={WIRE_COLORS[sel]}
            className="st-wire-line"
          />
        )}
      </svg>
      <p className="st-muted">Kéo từ đầu dây bên trái sang đầu cùng màu bên phải (hoặc bấm lần lượt hai đầu).</p>
    </div>
  );
}

function Keypad({ onDone }: { onDone: () => void }) {
  const code = useMemo(() => String(Math.floor(10000 + Math.random() * 90000)), []);
  const [typed, setTyped] = useState("");
  const [bad, setBad] = useState(false);
  const done = useLatest(onDone);
  const press = useLatest((d: string) => {
    const next = typed + d;
    if (!code.startsWith(next)) {
      setBad(true);
      setTyped("");
      setTimeout(() => setBad(false), 400);
      return;
    }
    setTyped(next);
    if (next === code) done.current();
  });
  // gõ số bằng bàn phím cũng được
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => /^[0-9]$/.test(e.key) && press.current(e.key);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [press]);
  return (
    <div className="st-keypad">
      <p className="st-code">
        <span>Mã cần nhập</span>
        <b>{code}</b>
      </p>
      <p className={`st-screenline${bad ? " is-bad" : ""}`}>{typed.padEnd(5, "·")}</p>
      <div className="st-keys">
        {["1", "2", "3", "4", "5", "6", "7", "8", "9", "0"].map((d) => (
          <button key={d} type="button" onClick={() => press.current(d)}>
            {d}
          </button>
        ))}
      </div>
      <p className="st-muted">Bấm hoặc gõ đúng 5 chữ số. Sai một số là nhập lại từ đầu.</p>
    </div>
  );
}

function Align({ onDone }: { onDone: () => void }) {
  const target = useMemo(() => 15 + Math.round(Math.random() * 70), []);
  const [v, setV] = useState(target > 50 ? 5 : 95);
  const ok = Math.abs(v - target) <= 4;
  const done = useLatest(onDone);
  // kim nằm trong vạch xanh một lúc là xong (không cần thả tay đúng lúc)
  useEffect(() => {
    if (!ok) return;
    const t = setTimeout(() => done.current(), 450);
    return () => clearTimeout(t);
  }, [ok, done]);
  return (
    <div className="st-align">
      <div className={`st-gauge${ok ? " is-ok" : ""}`}>
        <i className="st-gauge-target" style={{ left: `${target}%` }} />
        <i className={`st-gauge-needle${ok ? " is-ok" : ""}`} style={{ left: `${v}%` }} />
      </div>
      <input
        type="range"
        min={0}
        max={100}
        value={v}
        onChange={(e) => setV(Number(e.target.value))}
        aria-label="Căn chỉnh"
        autoFocus
        data-autofocus
      />
      <p className="st-muted">{ok ? "Giữ nguyên…" : "Kéo kim vào vạch xanh (hoặc dùng phím ← →)."}</p>
    </div>
  );
}

function Hold({
  onDone,
  ms,
  label,
  auto,
  tone = "amber",
}: {
  onDone: () => void;
  ms: number;
  label: string;
  auto?: boolean;
  tone?: "amber" | "red";
}) {
  const [held, setHeld] = useState(!!auto);
  const [pct, setPct] = useState(0);
  const acc = useRef(0);
  const done = useLatest(onDone);

  // giữ Space / E cũng được
  useEffect(() => {
    if (auto) return;
    const isKey = (e: KeyboardEvent) => e.key === " " || e.key.toLowerCase() === "e";
    const down = (e: KeyboardEvent) => {
      if (!isKey(e)) return;
      e.preventDefault();
      if (!e.repeat) setHeld(true);
    };
    const up = (e: KeyboardEvent) => isKey(e) && setHeld(false);
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
    };
  }, [auto]);

  // tiến độ tính theo thời gian thật; thả tay giữa chừng vẫn giữ phần đã làm
  useEffect(() => {
    if (!held) return;
    let raf = 0;
    let last = performance.now();
    const tick = (t: number) => {
      acc.current = Math.min(ms, acc.current + (t - last));
      last = t;
      setPct(acc.current / ms);
      if (acc.current >= ms) {
        done.current();
        return;
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [held, ms, done]);

  const C = 2 * Math.PI * 54;
  const stop = () => !auto && setHeld(false);
  return (
    <div className={`st-hold is-${tone}${auto ? " is-auto" : ""}`}>
      <button
        type="button"
        className={`st-holdbtn${held ? " is-held" : ""}`}
        disabled={auto}
        onPointerDown={(e) => {
          if (auto) return;
          e.currentTarget.setPointerCapture(e.pointerId);
          setHeld(true);
        }}
        onPointerUp={stop}
        onPointerCancel={stop}
        onLostPointerCapture={stop}
        onContextMenu={(e) => e.preventDefault()}
      >
        <svg viewBox="0 0 120 120" aria-hidden>
          <circle cx="60" cy="60" r="54" className="st-holdbtn-track" />
          <circle
            cx="60"
            cy="60"
            r="54"
            className="st-holdbtn-fill"
            style={{ strokeDasharray: C, strokeDashoffset: C * (1 - pct) }}
          />
        </svg>
        <span>
          <b>{Math.round(pct * 100)}%</b>
          <small>{auto ? label : held ? "Giữ tiếp…" : label}</small>
        </span>
      </button>
      <p className="st-muted">
        {auto
          ? "Đứng yên trên bệ quét cho tới khi xong."
          : "Giữ nút (hoặc phím Space / E) tới khi đầy vòng. Thả tay giữa chừng vẫn giữ phần đã làm."}
      </p>
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
