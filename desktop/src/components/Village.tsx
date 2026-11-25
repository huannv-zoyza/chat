// Màn hình làng Ma Sói: dân làng 2D ngồi quanh đống lửa, chat Khán đài (và kênh Sói) ở bên cạnh.
// Luật và thao tác giữ nguyên như thẻ trong chat (useWerewolf), đây chỉ là cách hiển thị khác.
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type FormEvent } from "react";
import { createPortal } from "react-dom";
import type { Message } from "../lib/api";
import { ROLE_INFO, type WerewolfRole, type WolfLine } from "../lib/games";
import { useVillageId, village } from "../lib/village";
import { findMessage } from "../state/chat";
import { useChat } from "../state/ChatProvider";
import { Countdown, useGameCtx, useWerewolf, WitchPanel, type Ctx } from "./Games";
import { ThreadPanel } from "./ThreadPanel";
import { Avatar, FormError, Icon } from "./ui";

export function VillageOverlay() {
  const id = useVillageId();
  const { state } = useChat();
  const msg = id !== null ? findMessage(state, id) : undefined;
  useEffect(() => {
    if (id === null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !document.querySelector("dialog[open]")) village.close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [id]);
  if (id === null || !msg?.game || msg.game.kind !== "werewolf") return null;
  return createPortal(<Village key={id} message={msg} />, document.body);
}

type Tab = "stands" | "wolves";

function Village({ message }: { message: Message }) {
  const { state } = useChat();
  const { ctx, error } = useGameCtx(message);
  const ww = useWerewolf(ctx);
  const { s, me, playing } = ww;
  const { game, meId } = ctx;
  const channel = state.channels[message.channel_id];
  const isWolf = me?.role === "wolf";
  const wolfLines = (isWolf ? me?.wolf_chat : s.wolf_chat) ?? [];
  const showWolfTab = isWolf || (!playing && wolfLines.length > 0);
  const [tab, setTab] = useState<Tab>("stands");
  const [chatOpen, setChatOpen] = useState(() => window.innerWidth > 900);
  // tin Sói mới khi đang xem tab khác: chấm báo
  const [seenWolf, setSeenWolf] = useState(wolfLines.length);
  useEffect(() => {
    if (tab === "wolves" && chatOpen) setSeenWolf(wolfLines.length);
  }, [tab, chatOpen, wolfLines.length]);
  const wolfUnread = wolfLines.length > seenWolf;

  const night = playing && s.phase !== "day";
  const lastLog = game.log[game.log.length - 1];

  return (
    <div className={`village${night ? " is-night" : " is-day"}${chatOpen ? " has-chat" : ""}`} role="dialog" aria-label="Làng Ma Sói">
      <div className="village-stage">
        <Scene ctx={ctx} ww={ww} />

        <header className="village-top">
          <div className="village-phase">
            <b>{ww.banner}</b>
            {playing && <Countdown deadline={game.deadline} label="Thời gian còn lại" />}
          </div>
          <div className="village-top-actions">
            <button
              type="button"
              className={`village-btn${wolfUnread ? " has-dot" : ""}`}
              aria-pressed={chatOpen}
              onClick={() => setChatOpen((v) => !v)}
            >
              <Icon name="thread" size={16} />
              <span className="village-btn-label">{chatOpen ? "Ẩn chat" : "Chat"}</span>
            </button>
            <button type="button" className="village-btn" onClick={() => village.close()} aria-label="Rời làng, về kênh chat">
              <Icon name="x" size={16} />
              <span className="village-btn-label">Rời làng</span>
            </button>
          </div>
        </header>

        {lastLog && (
          <p className="village-narrator" key={game.log.length} aria-live="polite">
            {lastLog}
          </p>
        )}

        <Hud ctx={ctx} ww={ww} />
        <FormError>{error}</FormError>

        {!playing && (
          <div className={`village-end${game.winners?.includes(meId) ? " is-win" : ""}`}>
            <p>{game.winners?.includes(meId) ? "🏆 " : ""}{game.result || "Ván đã kết thúc."}</p>
          </div>
        )}
      </div>

      {chatOpen && channel && (
        <aside className="village-chat" aria-label="Chat của ván">
          <div className="village-tabs" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={tab === "stands"}
              className={tab === "stands" ? "is-active" : ""}
              onClick={() => setTab("stands")}
            >
              Khán đài
            </button>
            {showWolfTab && (
              <button
                type="button"
                role="tab"
                aria-selected={tab === "wolves"}
                className={`${tab === "wolves" ? "is-active" : ""}${wolfUnread ? " has-dot" : ""}`}
                onClick={() => setTab("wolves")}
              >
                🐺 {playing ? "Bầy Sói" : "Sói đã bàn"}
              </button>
            )}
            <button type="button" className="village-tabs-close" onClick={() => setChatOpen(false)} aria-label="Ẩn chat">
              <Icon name="x" size={16} />
            </button>
          </div>
          {tab === "wolves" && showWolfTab ? (
            <WolfChat ctx={ctx} lines={wolfLines} canSend={playing && isWolf && !!me?.alive} />
          ) : (
            <ThreadPanel channel={channel} parentId={message.id} onClose={() => setChatOpen(false)} embedded />
          )}
        </aside>
      )}
    </div>
  );
}

/* ---------- Cảnh làng ---------- */

type WW = ReturnType<typeof useWerewolf>;

function Scene({ ctx, ww }: { ctx: Ctx; ww: WW }) {
  const ref = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState({ w: 800, h: 600 });
  useLayoutEffect(() => {
    const el = ref.current!;
    const ro = new ResizeObserver(() => setBox({ w: el.clientWidth, h: el.clientHeight }));
    ro.observe(el);
    setBox({ w: el.clientWidth, h: el.clientHeight });
    return () => ro.disconnect();
  }, []);

  const { game, meId } = ctx;
  const { s, me, step, dead, wolfMates, roleOf, canPick, onPick, pending, playing } = ww;
  const n = game.players.length;
  // mình ngồi ở giữa phía trước, người khác xếp vòng quanh lửa
  const order = useMemo(() => {
    const i = Math.max(0, game.players.findIndex((p) => p.id === meId));
    return [...game.players.slice(i), ...game.players.slice(0, i)];
  }, [game.players, meId]);

  const { w, h } = box;
  // Đường chân trời nằm cao (~20% màn hình): cả vòng người ngồi hẳn trên bãi đất, kể cả hàng phía sau.
  const cx = w / 2;
  const cy = h * 0.6;
  const portrait = h > w;
  // màn hình dọc (điện thoại): người to hơn theo bề ngang, vòng tròn cao hơn để dùng hết chiều dọc
  const base = portrait ? w * 0.25 : Math.min(w, h) * 0.2;
  const rx = Math.max(60, Math.min(w * 0.4, h * 0.8, w / 2 - base * 0.6));
  const ry = Math.min(h * (portrait ? 0.22 : 0.24), rx * (portrait ? 1.25 : 0.55));
  const perim = Math.PI * (3 * (rx + ry) - Math.sqrt((3 * rx + ry) * (rx + 3 * ry)));
  const size = Math.max(46, Math.min(base, 130, (perim / Math.max(n, 1)) * 1.05));
  const fire = Math.max(70, Math.min(Math.min(w, h) * 0.24, 190));

  const votes = s.phase === "day" ? s.votes : step === "wolf" ? me?.wolf_votes : undefined;
  const votersOf = (id: number) =>
    Object.entries(votes ?? {})
      .filter(([, t]) => t === id)
      .map(([v]) => Number(v));
  const selectedId =
    pending ?? (step === "wolf" ? me?.target : step === "vote" ? s.votes?.[meId] || undefined : undefined) ?? null;

  return (
    <div
      className="village-scene"
      ref={ref}
      style={
        {
          "--fx": `${cx}px`,
          "--fy": `${cy}px`,
          "--rx": `${rx}px`,
          "--ry": `${ry}px`,
          "--fr": `${Math.max(rx, ry) * 1.5}px`,
        } as CSSProperties
      }
    >
      <Backdrop />
      <GroundDetails w={w} h={h} cx={cx} cy={cy} rx={rx} ry={ry} />
      <div className="village-firelight" aria-hidden />
      <Campfire x={cx} y={cy} size={fire} />
      <ul className="village-ring" aria-label="Dân làng">
        {order.map((p, i) => {
          const a = Math.PI / 2 + (i * 2 * Math.PI) / Math.max(n, 1);
          const x = cx + rx * Math.cos(a);
          const y = cy + ry * Math.sin(a);
          const depth = (Math.sin(a) + 1) / 2; // 0 = phía sau lửa, 1 = phía trước
          const scale = 0.78 + 0.22 * depth;
          const isDead = dead.has(p.id);
          const role = roleOf(p.id);
          const shownRole = role && (s.roles[p.id] || p.id === meId || wolfMates.has(p.id)) ? role : undefined;
          const voters = votersOf(p.id);
          const pickable = canPick(p.id);
          const sleeping = playing && s.phase !== "day" && p.id !== meId && !wolfMates.has(p.id);
          return (
            <li
              key={p.id}
              className="village-seat"
              style={
                {
                  left: x,
                  top: y,
                  zIndex: Math.round(y),
                  "--size": `${size * scale}px`,
                  "--depth": depth,
                  "--delay": `${-(i * 0.37) % 3}s`,
                } as CSSProperties
              }
            >
              <Villager
                ctx={ctx}
                id={p.id}
                name={ctx.nameOf(p.id)}
                me={p.id === meId}
                dead={isDead}
                sleeping={sleeping}
                wolfForm={playing && s.phase !== "day" && wolfMates.has(p.id) && !isDead}
                role={shownRole}
                selected={selectedId === p.id}
                winner={game.status === "ended" && !!game.winners?.includes(p.id)}
                voters={voters}
                onPick={pickable ? () => onPick(p.id) : undefined}
                busy={ctx.busy}
              />
            </li>
          );
        })}
      </ul>
      {/* bóng đêm phủ lên cả người: chỉ quanh đống lửa là sáng, càng xa càng tối */}
      <div className="village-dark" aria-hidden />
    </div>
  );
}

/** Cỏ và đá rải trên mặt đất (xa thì nhỏ, gần thì to), chừa bãi đất quanh lửa. */
function GroundDetails({ w, h, cx, cy, rx, ry }: { w: number; h: number; cx: number; cy: number; rx: number; ry: number }) {
  const items = useMemo(() => {
    const out: { x: number; y: number; k: number; kind: number }[] = [];
    for (let i = 0; i < 70 && out.length < 34; i++) {
      const fx = ((i * 0.618034) % 1) * 1.04 - 0.02;
      const fy = 0.22 + ((i * 0.381966 * 7) % 1) * 0.76;
      const x = fx * w;
      const y = fy * h;
      const dx = (x - cx) / (rx * 1.3);
      const dy = (y - cy) / (ry * 1.5);
      if (dx * dx + dy * dy < 1) continue;
      out.push({ x, y, k: 0.45 + ((fy - 0.22) / 0.76) * 0.9, kind: i % 5 });
    }
    return out;
  }, [w, h, cx, cy, rx, ry]);
  return (
    <div className="village-details" aria-hidden>
      {items.map((it, i) => (
        <svg
          key={i}
          className={it.kind === 4 ? "village-rock" : "village-grass"}
          viewBox="0 0 40 24"
          style={{ left: it.x, top: it.y, width: 34 * it.k, zIndex: Math.round(it.y) }}
        >
          {it.kind === 4 ? (
            <>
              <ellipse cx={20} cy={20} rx={16} ry={3.5} className="village-detail-shadow" />
              <path d="M6 20 Q8 9 18 8 Q30 7 34 20Z" />
            </>
          ) : (
            <path d="M8 24 Q9 12 4 4 Q13 12 14 24 M16 24 Q18 8 20 0 Q22 10 23 24 M25 24 Q28 12 36 6 Q30 14 31 24Z" />
          )}
        </svg>
      ))}
    </div>
  );
}

/** Trời, sao, trăng/mặt trời, rừng và nhà phía xa, mặt đất. */
function Backdrop() {
  const stars = useMemo(
    () =>
      Array.from({ length: 48 }, (_, i) => ({
        left: `${(i * 37.7) % 100}%`,
        top: `${(i * 13.3) % 80}%`,
        size: 1 + ((i * 7) % 3),
        delay: `${-((i * 0.53) % 4)}s`,
      })),
    [],
  );
  return (
    <div className="village-backdrop" aria-hidden>
      <div className="village-sky is-night-sky" />
      <div className="village-sky is-day-sky" />
      <div className="village-stars">
        {stars.map((st, i) => (
          <i key={i} style={{ left: st.left, top: st.top, width: st.size, height: st.size, animationDelay: st.delay }} />
        ))}
      </div>
      <div className="village-moon" />
      <div className="village-sun" />
      <svg className="village-horizon" viewBox="0 0 1200 220" preserveAspectRatio="none">
        <path
          className="village-hills"
          d="M0 120 Q150 60 300 110 T600 100 T900 90 T1200 110 V220 H0Z"
        />
        <g className="village-trees">
          {Array.from({ length: 26 }, (_, i) => {
            const x = i * 48 + ((i * 17) % 20);
            const t = 70 + ((i * 29) % 40);
            return <path key={i} d={`M${x} ${t} L${x + 22} ${t + 90} L${x - 22} ${t + 90}Z`} />;
          })}
        </g>
        <g className="village-houses">
          <House x={180} y={118} />
          <House x={930} y={112} flip />
          <House x={1040} y={128} small />
        </g>
        <path className="village-ground-edge" d="M0 180 Q300 160 600 175 T1200 170 V220 H0Z" />
      </svg>
      <div className="village-ground" />
      <div className="village-clearing" />
    </div>
  );
}

function House({ x, y, flip, small }: { x: number; y: number; flip?: boolean; small?: boolean }) {
  const k = small ? 0.7 : 1;
  return (
    <g transform={`translate(${x} ${y}) scale(${flip ? -k : k} ${k})`}>
      <path className="village-house-roof" d="M-34 0 L0 -30 L34 0Z" />
      <rect className="village-house-wall" x={-28} y={0} width={56} height={44} />
      <rect className="village-house-window" x={-18} y={10} width={12} height={12} />
      <rect className="village-house-door" x={6} y={18} width={12} height={26} />
    </g>
  );
}

function Campfire({ x, y, size }: { x: number; y: number; size: number }) {
  return (
    <div className="village-fire" style={{ left: x, top: y, width: size, height: size, zIndex: Math.round(y) }} aria-hidden>
      <div className="village-fire-glow" />
      <svg viewBox="0 0 100 100">
        {/* vòng đá */}
        {Array.from({ length: 9 }, (_, i) => {
          const a = (i / 9) * Math.PI * 2;
          return <ellipse key={i} className="fire-stone" cx={50 + 34 * Math.cos(a)} cy={84 + 9 * Math.sin(a)} rx={7} ry={4.5} />;
        })}
        {/* củi */}
        <rect className="fire-log" x={24} y={78} width={52} height={9} rx={4.5} transform="rotate(-14 50 82)" />
        <rect className="fire-log is-2" x={24} y={78} width={52} height={9} rx={4.5} transform="rotate(14 50 82)" />
        <g className="fire-flames">
          <path className="flame flame-outer" d="M50 18 C62 36 74 48 70 66 C67 80 58 86 50 86 C42 86 33 80 30 66 C27 50 40 40 42 28 C46 38 48 42 50 18Z" />
          <path className="flame flame-mid" d="M50 34 C58 46 64 56 62 68 C60 78 55 84 50 84 C45 84 40 78 38 68 C37 58 44 52 46 44 C48 50 49 52 50 34Z" />
          <path className="flame flame-core" d="M50 52 C55 60 58 66 57 72 C56 79 53 83 50 83 C47 83 44 79 43 72 C43 66 47 62 50 52Z" />
        </g>
      </svg>
      <div className="village-embers">
        {Array.from({ length: 10 }, (_, i) => (
          <i key={i} style={{ left: `${30 + ((i * 41) % 40)}%`, animationDelay: `${-(i * 0.31)}s`, animationDuration: `${1.8 + (i % 4) * 0.35}s` }} />
        ))}
      </div>
    </div>
  );
}

/* ---------- Dân làng 2D ---------- */

const SKINS = ["#f7d7b8", "#eebf94", "#d39a6a", "#a8704a", "#7a4e33"];
const HAIRS = ["#2a1f18", "#4b2e1c", "#8a4b25", "#c9923c", "#e2c27a", "#6e6e6e", "#3a2a4a"];
const SHIRTS = ["#d9534f", "#3b82c4", "#e0a030", "#4fa36b", "#8e5cc2", "#2bb0a6", "#e07a4a", "#c2477e", "#6b7fd6", "#8fa33a", "#b36b3a", "#4a5a70"];
const PANTS = ["#3a4660", "#4b3a2e", "#2f4a3c", "#5a4a3a"];

/** Ngoại hình cố định theo id người chơi. */
function looks(id: number) {
  const h = Math.abs(Math.imul(id + 7, 2654435761)) >>> 0;
  const pick = <T,>(a: readonly T[], k: number) => a[Math.floor(h / k) % a.length];
  return {
    skin: pick(SKINS, 1),
    hair: pick(HAIRS, 5),
    shirt: SHIRTS[id % SHIRTS.length],
    pants: pick(PANTS, 35),
    style: Math.floor(h / 140) % 6, // 0 ngắn, 1 dài, 2 búi, 3 dựng, 4 hói + râu, 5 tóc mái
    hat: Math.floor(h / 840) % 5, // 0..2 không, 3 nón rơm, 4 khăn trùm
  };
}

function Villager({
  ctx,
  id,
  name,
  me,
  dead,
  sleeping,
  wolfForm,
  role,
  selected,
  winner,
  voters,
  onPick,
  busy,
}: {
  ctx: Ctx;
  id: number;
  name: string;
  me: boolean;
  dead: boolean;
  sleeping: boolean;
  /** Ban đêm, chỉ bầy Sói nhìn thấy nhau trong hình dạng sói (máy chủ không cho người khác biết ai là Sói). */
  wolfForm: boolean;
  role?: WerewolfRole;
  selected: boolean;
  winner: boolean;
  voters: number[];
  onPick?: () => void;
  busy: boolean;
}) {
  const cls = [
    "villager",
    me && "is-me",
    dead && "is-dead",
    selected && "is-selected",
    winner && "is-winner",
    onPick && "is-pickable",
  ]
    .filter(Boolean)
    .join(" ");
  const label = `${name}${me ? " (bạn)" : ""}${dead ? ", đã chết" : ""}${role ? `, ${ROLE_INFO[role].name}` : ""}`;
  const body = (
    <>
      {voters.length > 0 && (
        <span className="villager-votes" title={voters.map(ctx.nameOf).join(", ")}>
          {voters.slice(0, 3).map((v) => (
            <Avatar key={v} user={ctx.user(v)} size={16} />
          ))}
          <b>{voters.length}</b>
        </span>
      )}
      {role && (
        <span className={`villager-role ww-${role}`} title={ROLE_INFO[role].name}>
          {ROLE_INFO[role].icon}
        </span>
      )}
      <span className="villager-figure">
        {dead ? <Tombstone /> : wolfForm ? <Werewolf id={id} /> : <Person id={id} sleeping={sleeping} />}
      </span>
      <span className="villager-name">
        {name}
        {me && <small>bạn</small>}
      </span>
    </>
  );
  return onPick ? (
    <button type="button" className={cls} onClick={onPick} disabled={busy} aria-pressed={selected} aria-label={label}>
      {body}
    </button>
  ) : (
    <div className={cls} aria-label={label}>
      {body}
    </div>
  );
}

/** Một người ngồi trên khúc gỗ, nhìn ra phía trước. */
function Person({ id, sleeping }: { id: number; sleeping: boolean }) {
  const l = looks(id);
  const longHair = l.style === 1;
  return (
    <svg className="person" viewBox="0 0 100 124" aria-hidden>
      {/* khúc gỗ làm ghế */}
      <ellipse className="person-shadow" cx={50} cy={118} rx={34} ry={5} />
      <rect x={20} y={98} width={60} height={18} rx={8} fill="#6b4426" />
      <ellipse cx={20} cy={107} rx={5} ry={9} fill="#8a5a33" />
      <g className="person-body">
        {/* chân (ngồi, đầu gối ra trước) */}
        <rect x={34} y={86} width={13} height={26} rx={6} fill={l.pants} />
        <rect x={53} y={86} width={13} height={26} rx={6} fill={l.pants} />
        <ellipse cx={40.5} cy={113} rx={8} ry={4} fill="#3a2a1e" />
        <ellipse cx={59.5} cy={113} rx={8} ry={4} fill="#3a2a1e" />
        {/* tóc dài phía sau */}
        {longHair && <path d="M30 34 Q28 62 36 70 L64 70 Q72 62 70 34Z" fill={l.hair} />}
        {/* áo */}
        <path d="M34 56 Q50 50 66 56 L70 92 Q50 97 30 92Z" fill={l.shirt} />
        <path d="M44 55 L50 64 L56 55" fill="none" stroke="rgba(0,0,0,.22)" strokeWidth={2} strokeLinejoin="round" />
        <rect x={31} y={84} width={38} height={4} rx={2} fill="rgba(0,0,0,.25)" />
        {/* tay đặt lên gối */}
        <path d="M35 58 Q26 72 33 88" fill="none" stroke={l.shirt} strokeWidth={8} strokeLinecap="round" />
        <path d="M65 58 Q74 72 67 88" fill="none" stroke={l.shirt} strokeWidth={8} strokeLinecap="round" />
        <circle cx={34} cy={90} r={4.5} fill={l.skin} />
        <circle cx={66} cy={90} r={4.5} fill={l.skin} />
        {/* cổ + đầu */}
        <rect x={45} y={46} width={10} height={10} rx={3} fill={l.skin} />
        {l.hat === 4 && <path d="M28 36 Q28 12 50 12 Q72 12 72 36 L70 56 Q50 62 30 56Z" fill={shade(l.shirt)} />}
        <circle cx={31.5} cy={36} r={4} fill={l.skin} />
        <circle cx={68.5} cy={36} r={4} fill={l.skin} />
        <circle cx={50} cy={34} r={18} fill={l.skin} />
        <Hair style={l.hat === 4 ? -1 : l.style} color={l.hair} />
        {/* mặt */}
        <g className={`person-eyes${sleeping ? " is-closed" : ""}`}>
          {sleeping ? (
            <>
              <path d="M40 36 Q43 39 46 36" fill="none" stroke="#2a1f18" strokeWidth={1.8} strokeLinecap="round" />
              <path d="M54 36 Q57 39 60 36" fill="none" stroke="#2a1f18" strokeWidth={1.8} strokeLinecap="round" />
            </>
          ) : (
            <>
              <ellipse className="person-eye" cx={43} cy={35} rx={2.3} ry={2.8} />
              <ellipse className="person-eye" cx={57} cy={35} rx={2.3} ry={2.8} />
            </>
          )}
        </g>
        <ellipse cx={39} cy={41} rx={3.2} ry={1.8} fill="#ff8a8a" opacity={0.35} />
        <ellipse cx={61} cy={41} rx={3.2} ry={1.8} fill="#ff8a8a" opacity={0.35} />
        {sleeping ? (
          <ellipse cx={50} cy={44} rx={2} ry={1.4} fill="#7a3b2e" />
        ) : (
          <path d="M45.5 43 Q50 46.5 54.5 43" fill="none" stroke="#7a3b2e" strokeWidth={1.6} strokeLinecap="round" />
        )}
        {l.style === 4 && l.hat !== 4 && <path d="M36 40 Q38 56 50 57 Q62 56 64 40 Q58 50 50 50 Q42 50 36 40Z" fill={l.hair} />}
        {l.hat === 3 && (
          <g>
            <ellipse cx={50} cy={22} rx={28} ry={6} fill="#d9b25a" />
            <path d="M37 22 Q38 8 50 8 Q62 8 63 22Z" fill="#e8c46d" />
            <rect x={37} y={18} width={26} height={3} fill="#a8432f" />
          </g>
        )}
      </g>
      {sleeping && (
        <text className="person-z" x={70} y={18}>
          z
        </text>
      )}
    </svg>
  );
}

const FURS = ["#5b5f6b", "#6e5a48", "#4a4a52", "#7d7f86", "#3e3a38"];

/** Người sói ngồi trên khúc gỗ: lông, tai nhọn, mõm dài, mắt đỏ phát sáng. Áo vẫn là màu áo của người đó. */
function Werewolf({ id }: { id: number }) {
  const l = looks(id);
  const fur = FURS[id % FURS.length];
  const dark = shade(fur);
  return (
    <svg className="person is-werewolf" viewBox="0 0 100 124" aria-hidden>
      <ellipse className="person-shadow" cx={50} cy={118} rx={34} ry={5} />
      <rect x={20} y={98} width={60} height={18} rx={8} fill="#6b4426" />
      <ellipse cx={20} cy={107} rx={5} ry={9} fill="#8a5a33" />
      <g className="person-body">
        {/* đuôi */}
        <path d="M70 100 Q92 96 90 74 Q84 88 70 92Z" fill={fur} />
        {/* chân lông, móng */}
        <rect x={33} y={84} width={15} height={28} rx={7} fill={fur} />
        <rect x={52} y={84} width={15} height={28} rx={7} fill={fur} />
        <path d="M33 112 l3 4 l3 -4 l3 4 l3 -4 l3 4" fill="none" stroke="#e8e2d6" strokeWidth={1.6} />
        <path d="M52 112 l3 4 l3 -4 l3 4 l3 -4 l3 4" fill="none" stroke="#e8e2d6" strokeWidth={1.6} />
        {/* thân to hơn, áo rách */}
        <path d="M30 56 Q50 46 70 56 L74 92 Q50 99 26 92Z" fill={fur} />
        <path d="M36 58 Q50 54 64 58 L66 84 L60 80 L56 86 L50 80 L44 86 L40 80 L34 84Z" fill={l.shirt} opacity={0.9} />
        <path d="M42 62 Q50 70 58 62" fill="none" stroke={dark} strokeWidth={2} />
        {/* tay có móng */}
        <path d="M32 58 Q20 74 30 90" fill="none" stroke={fur} strokeWidth={10} strokeLinecap="round" />
        <path d="M68 58 Q80 74 70 90" fill="none" stroke={fur} strokeWidth={10} strokeLinecap="round" />
        <path d="M25 92 l2 4 M29 93 l1 4 M33 92 l0 4" stroke="#e8e2d6" strokeWidth={1.5} strokeLinecap="round" />
        <path d="M67 92 l0 4 M71 93 l-1 4 M75 92 l-2 4" stroke="#e8e2d6" strokeWidth={1.5} strokeLinecap="round" />
        {/* đầu sói */}
        <path d="M33 24 L30 2 L44 16Z" fill={fur} />
        <path d="M67 24 L70 2 L56 16Z" fill={fur} />
        <path d="M34 20 L32.5 8 L40 16Z" fill="#c98f8f" />
        <path d="M66 20 L67.5 8 L60 16Z" fill="#c98f8f" />
        <path d="M30 34 Q30 14 50 14 Q70 14 70 34 Q70 46 62 50 L38 50 Q30 46 30 34Z" fill={fur} />
        {/* bờm má */}
        <path d="M30 34 L24 42 L32 42 L28 50 L38 48Z M70 34 L76 42 L68 42 L72 50 L62 48Z" fill={dark} />
        {/* mõm */}
        <path d="M40 38 Q50 34 60 38 L58 52 Q50 57 42 52Z" fill="#cfc6b8" />
        <ellipse cx={50} cy={40} rx={4.5} ry={3} fill="#1e1a18" />
        <path d="M44 51 l2 3 l2 -3 M52 51 l2 3 l2 -3" fill="#fff" stroke="#fff" strokeWidth={0.8} />
        {/* mắt đỏ phát sáng */}
        <g className="werewolf-eyes">
          <path d="M37 30 L45 32 L38 34Z" fill="#ff3b30" />
          <path d="M63 30 L55 32 L62 34Z" fill="#ff3b30" />
        </g>
        <path d="M36 26 L45 29 M64 26 L55 29" stroke={dark} strokeWidth={2.2} strokeLinecap="round" />
      </g>
    </svg>
  );
}

function Hair({ style, color }: { style: number; color: string }) {
  switch (style) {
    case 0:
      return <path d="M32 32 Q31 14 50 14 Q69 14 68 32 Q62 23 50 23 Q38 23 32 32Z" fill={color} />;
    case 1:
      return <path d="M31 36 Q29 13 50 13 Q71 13 69 36 Q66 22 50 21 Q34 22 31 36Z" fill={color} />;
    case 2:
      return (
        <>
          <circle cx={50} cy={11} r={7} fill={color} />
          <path d="M32 32 Q31 15 50 15 Q69 15 68 32 Q62 22 50 22 Q38 22 32 32Z" fill={color} />
        </>
      );
    case 3:
      return <path d="M32 30 L34 16 L39 22 L42 11 L47 19 L51 9 L55 19 L60 12 L62 21 L67 16 L68 30 Q60 22 50 22 Q40 22 32 30Z" fill={color} />;
    case 4:
      return <path d="M32 30 Q34 22 38 21 Q35 26 34 32Z M68 30 Q66 22 62 21 Q65 26 66 32Z" fill={color} />;
    case 5:
      return <path d="M32 33 Q30 14 50 14 Q70 14 68 33 Q64 26 58 25 Q52 30 40 28 Q35 29 32 33Z" fill={color} />;
  }
  return null;
}

function Tombstone() {
  return (
    <svg className="person tombstone" viewBox="0 0 100 124" aria-hidden>
      <ellipse className="person-shadow" cx={50} cy={118} rx={30} ry={5} />
      <path d="M28 116 V60 Q28 36 50 36 Q72 36 72 60 V116Z" fill="#8b929c" />
      <path d="M28 116 V60 Q28 36 50 36 Q56 36 60 38 Q36 42 36 64 V116Z" fill="#a3aab3" />
      <text x={50} y={74} textAnchor="middle" className="tomb-text">
        RIP
      </text>
      <path d="M22 116 Q30 108 36 116 Q42 108 50 116 Q58 108 64 116 Q70 108 78 116Z" fill="#4a7a3a" />
    </svg>
  );
}

function shade(hex: string) {
  const n = parseInt(hex.slice(1), 16);
  const f = (v: number) => Math.round(v * 0.72);
  return `rgb(${f(n >> 16)},${f((n >> 8) & 255)},${f(n & 255)})`;
}

/* ---------- Bảng điều khiển dưới cùng ---------- */

function Hud({ ctx, ww }: { ctx: Ctx; ww: WW }) {
  const { game, meId, busy, act } = ctx;
  const { s, me, step, playing, pending, heal, setHeal, poison, setPoison, hint } = ww;
  const [roleShown, setRoleShown] = useState(false);
  if (!me?.role && !playing) return null;
  return (
    <div className="village-hud">
      {me?.role && (
        <button
          type="button"
          className={`village-role${roleShown ? " is-shown" : ""}${me.role === "wolf" ? " is-wolf" : ""}`}
          onClick={() => setRoleShown((v) => !v)}
          aria-expanded={roleShown}
          title={roleShown ? "Bấm để úp thẻ" : "Bấm để xem vai (che màn hình khỏi người bên cạnh)"}
        >
          {roleShown ? (
            <>
              <span className="village-role-icon" aria-hidden>
                {ROLE_INFO[me.role].icon}
              </span>
              <span className="village-role-text">
                <b>{ROLE_INFO[me.role].name}</b>
                <small>
                  {ROLE_INFO[me.role].team}
                  {me.role === "witch" &&
                    ` · cứu: ${me.heal_left ? "còn" : "hết"} · độc: ${me.poison_left ? "còn" : "hết"}`}
                </small>
              </span>
            </>
          ) : (
            <>
              <span className="village-role-icon" aria-hidden>
                🎴
              </span>
              <span className="village-role-text">
                <b>Vai của bạn</b>
                <small>Bấm để lật thẻ</small>
              </span>
            </>
          )}
        </button>
      )}

      <div className="village-hud-main">
        {roleShown && me?.seer_log && me.seer_log.length > 0 && (
          <ul className="village-seer">
            {me.seer_log.map((c) => (
              <li key={c.round}>
                Đêm {c.round}: {ctx.nameOf(c.target)} {c.wolf ? <b>🐺 là Sói</b> : "không phải Sói"}
              </li>
            ))}
          </ul>
        )}
        {step === "witch" && me ? (
          <WitchPanel ctx={ctx} me={me} s={s} heal={heal} setHeal={setHeal} poison={poison} setPoison={setPoison} />
        ) : (
          hint && <p className="village-hint">{hint}</p>
        )}
        {playing && (
          <div className="village-actions">
            {pending !== null && (step === "seer" || step === "guard" || step === "shoot") && (
              <button
                type="button"
                className="btn btn-primary"
                disabled={busy}
                onClick={() => act(step === "shoot" ? "shoot" : "night", { target: pending })}
              >
                {step === "seer" ? "Soi" : step === "guard" ? "Bảo vệ" : "Bắn"} {ctx.nameOf(pending)}
              </button>
            )}
            {step === "shoot" && (
              <button type="button" className="btn" disabled={busy} onClick={() => act("shoot", { target: 0 })}>
                Không bắn ai
              </button>
            )}
            {step === "vote" && s.votes?.[meId] === undefined && (
              <button type="button" className="btn" disabled={busy} onClick={() => act("vote", { target: 0 })}>
                Bỏ qua
              </button>
            )}
            {s.phase === "day" && game.host_id === meId && (
              <button type="button" className="btn" disabled={busy} onClick={() => act("skip_day")}>
                Chốt phiếu ngay
              </button>
            )}
            {s.phase === "day" && (
              <span className="village-count">
                {Object.keys(s.votes ?? {}).length}/{game.players.length - s.dead.length} đã bỏ phiếu
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/* ---------- Kênh chat Sói ---------- */

function WolfChat({ ctx, lines, canSend }: { ctx: Ctx; lines: WolfLine[]; canSend: boolean }) {
  const { gameAction } = useChat();
  const [text, setText] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const listRef = useRef<HTMLOListElement>(null);
  useLayoutEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines.length]);

  const send = async (e: FormEvent) => {
    e.preventDefault();
    const t = text.trim();
    if (!t || sending) return;
    setSending(true);
    setErr(null);
    try {
      await gameAction(ctx.messageId, "wolf_chat", { text: t });
      setText("");
    } catch (er) {
      setErr(er instanceof Error ? er.message : "Không gửi được.");
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="wolf-chat">
      <p className="wolf-chat-note">
        {ctx.game.status === "playing"
          ? "Chỉ bầy Sói thấy kênh này. Hết ván mọi người mới được xem lại."
          : "Những gì bầy Sói đã bàn trong ván."}
      </p>
      <ol className="wolf-chat-list" ref={listRef}>
        {lines.length === 0 && <li className="wolf-chat-empty">Chưa ai nói gì. Bàn xem đêm nay cắn ai…</li>}
        {lines.map((l, i) => {
          const grouped = i > 0 && lines[i - 1].from === l.from;
          return (
            <li key={i} className={`wolf-line${l.from === ctx.meId ? " is-mine" : ""}${grouped ? " is-grouped" : ""}`}>
              {!grouped && (
                <span className="wolf-line-head">
                  <Avatar user={ctx.user(l.from)} size={20} />
                  <b>{ctx.nameOf(l.from)}</b>
                  <time>{new Date(l.at).toLocaleTimeString("vi", { hour: "2-digit", minute: "2-digit" })}</time>
                </span>
              )}
              <span className="wolf-line-text">{l.text}</span>
            </li>
          );
        })}
      </ol>
      {canSend ? (
        <form className="wolf-chat-form" onSubmit={send}>
          <input
            value={text}
            maxLength={300}
            onChange={(e) => setText(e.target.value)}
            placeholder="Nhắn cho bầy Sói…"
            aria-label="Nhắn cho bầy Sói"
          />
          <button type="submit" className="btn btn-primary" disabled={sending || !text.trim()} aria-label="Gửi">
            <Icon name="send" size={16} />
          </button>
        </form>
      ) : (
        ctx.game.status === "playing" && <p className="wolf-chat-note">Bạn đã chết, chỉ đọc được.</p>
      )}
      <FormError>{err}</FormError>
    </div>
  );
}
