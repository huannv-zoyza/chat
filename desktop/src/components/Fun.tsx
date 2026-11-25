import { useEffect, useMemo, type CSSProperties } from "react";
import { displayName } from "../lib/format";
import { throwItem } from "../lib/fun";
import { playThrowSound } from "../lib/sound";
import { useChat } from "../state/ChatProvider";

/** Thời điểm (ms) món đồ chạm màn hình; khớp với hoạt ảnh throw-fly trong styles.css. */
const IMPACT = 450;
/** Dép đập phát thứ hai. */
const IMPACT2 = 790;
const DURATION = 3400;

const SPLATS: Record<string, "tomato" | "egg" | "cake"> = {
  cachua: "tomato",
  trung: "egg",
  banhkem: "cake",
};

const reducedMotion = () =>
  window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

/** Rung cả ứng dụng một nhịp ngắn. */
function shake() {
  if (reducedMotion()) return;
  const el = document.documentElement;
  el.classList.remove("is-hit-shake");
  void el.offsetWidth; // chạy lại hoạt ảnh nếu đang rung dở
  el.classList.add("is-hit-shake");
  window.setTimeout(() => el.classList.remove("is-hit-shake"), 420);
}

/**
 * Bị ném đồ: món đồ bay vào màn hình rồi trúng theo kiểu riêng: cà chua, trứng, bánh kem vỡ toé
 * và chảy xuống; dép đập "bốp bốp" làm rung màn hình; hoa thì rơi cánh hoa. Không chặn thao tác.
 */
export function ThrowOverlay() {
  const { state, dispatch } = useChat();
  const hit = state.hit;
  useEffect(() => {
    if (!hit) return;
    playThrowSound(hit.item, IMPACT / 1000);
    const timers: number[] = [];
    if (hit.item !== "hoa") {
      timers.push(window.setTimeout(shake, IMPACT));
      if (hit.item === "dep") timers.push(window.setTimeout(shake, IMPACT2));
    }
    timers.push(
      window.setTimeout(() => dispatch({ type: "hit.clear" }), DURATION),
    );
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [hit, dispatch]);
  // hướng bay: từ góc dưới bên trái hoặc bên phải
  const side = useMemo(() => (hit && hit.key % 2 ? -1 : 1), [hit]);
  if (!hit) return null;
  const item = throwItem(hit.item);
  const from = displayName(state.users[hit.fromId]);
  const splat = SPLATS[hit.item];
  return (
    <div
      className={`throw-overlay throw-${hit.item}${splat ? " is-splat" : ""}`}
      key={hit.key}
      style={{ "--side": side } as CSSProperties}
      role="status"
      aria-live="polite"
    >
      {hit.item !== "hoa" && <span className="throw-flash" aria-hidden />}
      <div className="throw-stage" aria-hidden>
        {splat && <Splat kind={splat} seed={hit.key} />}
        {hit.item === "dep" && (
          <>
            <Bop className="throw-bop is-first" />
            <Bop className="throw-bop is-second" />
          </>
        )}
        <span className="throw-item">{item?.icon ?? "🎯"}</span>
      </div>
      {hit.item === "hoa" && <Petals seed={hit.key} />}
      <p className="throw-text">
        {from} vừa ném {item?.name ?? "đồ"} vào bạn!
      </p>
    </div>
  );
}

/** Số giả ngẫu nhiên theo seed, để một lượt ném vẽ ra cùng một vết toé trong suốt lượt đó. */
function rng(seed: number) {
  let s = seed % 2147483647 || 1;
  return () => {
    s = (s * 16807) % 2147483647;
    return (s - 1) / 2147483646;
  };
}

/** Đường viền vết toé: vòng tròn méo, thỉnh thoảng có tia bắn dài ra. */
function blobPath(rand: () => number, r: number, n: number) {
  const pts: [number, number][] = [];
  for (let i = 0; i < n; i++) {
    const a = (i / n) * Math.PI * 2 + (rand() - 0.5) * 0.25;
    const spike = rand() < 0.3;
    const rr = r * (spike ? 1.25 + rand() * 0.4 : 0.78 + rand() * 0.25);
    pts.push([Math.cos(a) * rr, Math.sin(a) * rr]);
  }
  const mid = (p: [number, number], q: [number, number]) => [
    (p[0] + q[0]) / 2,
    (p[1] + q[1]) / 2,
  ];
  const m0 = mid(pts[n - 1], pts[0]);
  let d = `M${m0[0].toFixed(1)},${m0[1].toFixed(1)}`;
  for (let i = 0; i < n; i++) {
    const p = pts[i];
    const m = mid(p, pts[(i + 1) % n]);
    d += ` Q${p[0].toFixed(1)},${p[1].toFixed(1)} ${m[0].toFixed(1)},${m[1].toFixed(1)}`;
  }
  return d + "Z";
}

const SPLAT_COLORS = {
  tomato: {
    base: "#a8130c",
    top: "#e0301f",
    shine: "#ff8a75",
    drip: "#c11d12",
  },
  egg: { base: "#e9e2cf", top: "#fbf8ee", shine: "#ffffff", drip: "#f1ebd9" },
  cake: { base: "#f3c6d6", top: "#fff6f2", shine: "#ffffff", drip: "#fbe3ea" },
};
const SPRINKLES = ["#ff5d8f", "#ffd23f", "#3ec1d3", "#7bd389", "#b388ff"];

function Splat({
  kind,
  seed,
}: {
  kind: "tomato" | "egg" | "cake";
  seed: number;
}) {
  const c = SPLAT_COLORS[kind];
  const shape = useMemo(() => {
    const rand = rng(seed);
    const outer = blobPath(rand, 100, 22);
    const inner = blobPath(rand, 78, 16);
    const drops = Array.from({ length: 11 }, () => {
      const a = rand() * Math.PI * 2;
      const dist = 120 + rand() * 60;
      return {
        x: Math.cos(a) * dist,
        y: Math.sin(a) * dist,
        r: 4 + rand() * 11,
      };
    });
    const drips = Array.from({ length: 5 }, (_, i) => ({
      x: -62 + i * 31 + (rand() - 0.5) * 14,
      w: 9 + rand() * 9,
      len: 70 + rand() * 120,
      delay: rand() * 0.5,
    }));
    const bits = Array.from({ length: kind === "egg" ? 7 : 16 }, () => ({
      x: (rand() - 0.5) * 130,
      y: (rand() - 0.5) * 120,
      rot: rand() * 180,
      col: SPRINKLES[Math.floor(rand() * SPRINKLES.length)],
    }));
    return { outer, inner, drops, drips, bits };
  }, [seed, kind]);
  return (
    <svg className={`throw-splat splat-${kind}`} viewBox="-200 -200 400 440">
      <g className="splat-drips">
        {shape.drips.map((d, i) => (
          <rect
            key={i}
            x={d.x - d.w / 2}
            y={20}
            width={d.w}
            height={d.len}
            rx={d.w / 2}
            fill={c.drip}
            style={{ animationDelay: `${0.5 + d.delay}s` }}
          />
        ))}
      </g>
      <g className="splat-body">
        <path d={shape.outer} fill={c.base} />
        <path d={shape.inner} fill={c.top} />
        {shape.drops.map((d, i) => (
          <circle
            key={i}
            cx={d.x}
            cy={d.y}
            r={d.r}
            fill={i % 3 ? c.top : c.base}
          />
        ))}
        {kind === "tomato" &&
          shape.bits.map((b, i) => (
            <ellipse
              key={i}
              cx={b.x * 0.8}
              cy={b.y * 0.8}
              rx={4.5}
              ry={2.6}
              fill="#f6d96b"
              transform={`rotate(${b.rot} ${b.x * 0.8} ${b.y * 0.8})`}
            />
          ))}
        {kind === "egg" && (
          <>
            <circle cx={6} cy={-4} r={44} fill="#ffb000" />
            <circle cx={6} cy={-4} r={44} fill="url(#yolk)" />
            <defs>
              <radialGradient id="yolk" cx="0.38" cy="0.35" r="0.7">
                <stop offset="0" stopColor="#fff3b0" stopOpacity="0.9" />
                <stop offset="0.35" stopColor="#ffc928" stopOpacity="0.4" />
                <stop offset="1" stopColor="#e08a00" stopOpacity="0" />
              </radialGradient>
            </defs>
            {shape.bits.map((b, i) => (
              <path
                key={i}
                d="M0,-11 L9,-2 L4,10 L-8,6 L-6,-5Z"
                fill="#f4e9d4"
                stroke="#d9c7a3"
                strokeWidth={1.5}
                transform={`translate(${b.x * 1.35} ${b.y * 1.35}) rotate(${b.rot})`}
              />
            ))}
          </>
        )}
        {kind === "cake" &&
          shape.bits.map((b, i) => (
            <rect
              key={i}
              x={b.x - 6}
              y={b.y - 2}
              width={12}
              height={4}
              rx={2}
              fill={b.col}
              transform={`rotate(${b.rot} ${b.x} ${b.y})`}
            />
          ))}
        <ellipse
          cx={-34}
          cy={-42}
          rx={26}
          ry={12}
          fill={c.shine}
          opacity={0.45}
          transform="rotate(-30 -34 -42)"
        />
      </g>
    </svg>
  );
}

/** Chữ "BỐP!" trong vụ nổ kiểu truyện tranh. */
function Bop({ className }: { className: string }) {
  const star = useMemo(() => {
    const n = 14;
    let d = "";
    for (let i = 0; i < n * 2; i++) {
      const a = (i / (n * 2)) * Math.PI * 2;
      const r = i % 2 ? 62 : 100;
      d += `${i ? "L" : "M"}${(Math.cos(a) * r * 1.25).toFixed(1)},${(Math.sin(a) * r).toFixed(1)} `;
    }
    return d + "Z";
  }, []);
  return (
    <svg className={className} viewBox="-130 -110 260 220">
      <path d={star} fill="#e8231a" />
      <path d={star} fill="#ffd400" transform="scale(0.82)" />
      <text x={0} y={16} textAnchor="middle" className="throw-bop-text">
        BỐP!
      </text>
    </svg>
  );
}

const PETALS = ["🌸", "🌹", "💕", "🌸", "💗", "🌷"];

function Petals({ seed }: { seed: number }) {
  const petals = useMemo(() => {
    const rand = rng(seed);
    return Array.from({ length: 18 }, (_, i) => ({
      icon: PETALS[i % PETALS.length],
      left: rand() * 100,
      delay: 0.3 + rand() * 1.4,
      dur: 2 + rand() * 1.2,
      size: 1.2 + rand() * 1.6,
      drift: (rand() - 0.5) * 16,
    }));
  }, [seed]);
  return (
    <div className="throw-petals" aria-hidden>
      {petals.map((p, i) => (
        <span
          key={i}
          style={
            {
              left: `${p.left}%`,
              fontSize: `${p.size}rem`,
              animationDelay: `${p.delay}s`,
              animationDuration: `${p.dur}s`,
              "--drift": `${p.drift}vw`,
            } as CSSProperties
          }
        >
          {p.icon}
        </span>
      ))}
    </div>
  );
}
