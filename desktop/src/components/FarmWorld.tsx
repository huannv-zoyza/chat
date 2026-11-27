import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as RPointerEvent,
  type ReactNode,
} from "react";
import {
  formatLeft,
  orchardSpotXY,
  orchardUnlockLevel,
  ORCHARD_SPOTS,
  tendPlan,
  treeLive,
  type Animal,
  type Crop,
  type FarmView,
  type Plot,
  type PlotState,
  type Spot,
  type TreeView,
} from "../lib/farm";
import { SvgEmo, SvgLabel } from "./Emoji";

/*
 * Nông trại vẽ isometric bằng SVG (kiểu FarmVille / Hay Day): mặt đất là lưới ô thoi 2:1, mọi vật là khối
 * có mặt trên + hai mặt bên đậm nhạt khác nhau, có bóng đổ, vẽ theo thứ tự xa → gần.
 *
 * Toạ độ lưới (gx, gy): gx tăng về phía dưới-phải, gy tăng về phía dưới-trái; z là độ cao (px màn hình).
 * Bố cục (ô lưới), mặt đất 24 × 20:
 *   - Ruộng: gốc (9, 6), tối đa 5 cột × 4 hàng
 *   - Chuồng gà: (2..7, 2..6) · Đồng cỏ: (2..7, 9..15)   (bên trái đường dọc gx = 8)
 *   - Nhà kho (kho chứa): (10..13, 1..3) · Silo (14, 2) · Xe tải đơn hàng: (20, 16)
 *   - Ao: (17..21, 9..13) · Đường đá: dọc gx = 8, ngang gy = 16
 */

const TW = 96; // bề ngang một ô thoi
const TH = 48; // bề cao một ô thoi
const GW = 24;
const GH = 20;
const FIELD = { gx: 9.2, gy: 6.2 };
const TIERS: { cols: number; rows: number }[] = [
  { cols: 3, rows: 2 },
  { cols: 3, rows: 3 },
  { cols: 4, rows: 3 },
  { cols: 4, rows: 4 },
  { cols: 5, rows: 4 },
];
const COOP = { gx: 2, gy: 2, w: 5, d: 4.4 };
const PASTURE = { gx: 2, gy: 9, w: 5.4, d: 5.6 };
/** Xưởng chế biến: chỗ mặc định (ô) và cỡ vẽ bên trong khối 3 × 2 ô nó chiếm (store/farm_place.go). */
const KITCHEN_SPOT: Spot = { x: 12, y: 12 };
const kitchenRect = (s: Spot) => ({
  gx: s.x + 0.2,
  gy: s.y + 0.05,
  w: 2.6,
  d: 1.9,
});

/** Máy ép nước: chỗ mặc định (16,13) và cỡ vẽ bên trong khối 2 × 2 ô nó chiếm (store/farm_v2.go). */
const JUICER_SPOT: Spot = { x: 16, y: 13 };
const juicerRect = (s: Spot) => ({
  gx: s.x + 0.2,
  gy: s.y + 0.15,
  w: 1.6,
  d: 1.6,
});
/** Màu quả trên cây theo loại cây giống. */
const FRUIT_COLOR: Record<string, string> = {
  cam: "#ff9f1c",
  tao: "#e63946",
  chuoi: "#ffe14d",
  xoai: "#ffb347",
  dua: "#8b6b4a",
  nho: "#7b3fa0",
};

/** Danh mục vật nuôi dự phòng: xem vườn hàng xóm máy chủ không gửi animal_kinds. */
const ANIMAL_FALLBACK: Record<
  string,
  { emoji: string; building: "coop" | "pasture"; product: string }
> = {
  ga: { emoji: "🐓", building: "coop", product: "🥚" }, // 🐔 ở font Noto chỉ có cái đầu
  bo: { emoji: "🐄", building: "pasture", product: "🥛" },
  de: { emoji: "🐐", building: "pasture", product: "🧀" },
  cuu: { emoji: "🐑", building: "pasture", product: "🧶" },
  heo: { emoji: "🐖", building: "pasture", product: "🍄" },
};

type P = [number, number];
const pt = (x: number, y: number, z = 0): P => [
  ((x - y) * TW) / 2,
  ((x + y) * TH) / 2 - z,
];
const poly = (ps: P[]) =>
  ps.map((p) => `${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(" ");

function layoutFor(n: number) {
  return TIERS.find((t) => t.cols * t.rows >= n) ?? TIERS[TIERS.length - 1];
}

/** Khối hộp: mặt trên + mặt trước-trái + mặt trước-phải. */
function Box({
  x,
  y,
  w,
  d,
  h,
  z = 0,
  top,
  left,
  right,
  stroke,
}: {
  x: number;
  y: number;
  w: number;
  d: number;
  h: number;
  z?: number;
  top: string;
  left: string;
  right: string;
  stroke?: string;
}) {
  const A = (zz: number) => pt(x, y, zz);
  const B = (zz: number) => pt(x + w, y, zz);
  const C = (zz: number) => pt(x + w, y + d, zz);
  const D = (zz: number) => pt(x, y + d, zz);
  const s = stroke
    ? { stroke, strokeWidth: 1.2, strokeLinejoin: "round" as const }
    : {};
  return (
    <g>
      <polygon
        points={poly([D(z + h), C(z + h), C(z), D(z)])}
        fill={left}
        {...s}
      />
      <polygon
        points={poly([B(z + h), C(z + h), C(z), B(z)])}
        fill={right}
        {...s}
      />
      <polygon
        points={poly([A(z + h), B(z + h), C(z + h), D(z + h)])}
        fill={top}
        {...s}
      />
    </g>
  );
}

/** Bóng đổ hình bầu dục dưới chân vật. */
function Shadow({
  x,
  y,
  r = 0.35,
  o = 0.22,
}: {
  x: number;
  y: number;
  r?: number;
  o?: number;
}) {
  const [cx, cy] = pt(x, y);
  return (
    <ellipse
      cx={cx}
      cy={cy}
      rx={(r * TW) / 1.4}
      ry={(r * TH) / 1.4}
      fill={`rgba(30,50,10,${o})`}
    />
  );
}

/** Emoji dựng đứng ở (x, y), đáy chạm đất. */
function Sprite({
  x,
  y,
  size,
  children,
  className,
  z = 0,
}: {
  x: number;
  y: number;
  size: number;
  children: ReactNode;
  className?: string;
  z?: number;
}) {
  const [cx, cy] = pt(x, y, z);
  const style = { transformOrigin: `${cx}px ${cy}px` };
  if (typeof children === "string")
    return (
      <SvgEmo
        e={children}
        x={cx}
        y={cy + size * 0.12}
        size={size}
        className={className}
        style={style}
      />
    );
  return (
    <text
      x={cx}
      y={cy + size * 0.12}
      fontSize={size}
      textAnchor="middle"
      className={className}
      style={style}
    >
      {children}
    </text>
  );
}

/**
 * Bác nông dân đủ thân (emoji 🧑‍🌾 chỉ có nửa người): mũ rơm, áo kẻ đỏ, quần yếm xanh, ủng,
 * tay cầm chĩa. (x, y) là chỗ chân đứng; cao ~60px.
 */
function Farmer({ x, y }: { x: number; y: number }) {
  const [cx, cy] = pt(x, y);
  const skin = "#f2c29b";
  const skinDk = "#d99d74";
  const shirt = "#d9453a";
  const shirtDk = "#b5332a";
  const denim = "#3f6fb5";
  const denimDk = "#2f558f";
  const boot = "#5a3a1e";
  return (
    <g>
      <Shadow x={x} y={y} r={0.32} o={0.25} />
      <g transform={`translate(${cx}, ${cy})`}>
        <g className="iso-farmer">
          {/* chĩa (sau người) */}
          <line
            x1={15}
            y1={-2}
            x2={15}
            y2={-56}
            stroke="#8a5a2c"
            strokeWidth={2.6}
            strokeLinecap="round"
          />
          <path
            d="M10 -56 V-64 M15 -56 V-66 M20 -56 V-64 M10 -56 H20"
            stroke="#9aa3ad"
            strokeWidth={1.8}
            strokeLinecap="round"
            fill="none"
          />
          {/* chân + ủng */}
          <rect x={-8} y={-20} width={7} height={15} rx={2.5} fill={denimDk} />
          <rect x={1} y={-20} width={7} height={15} rx={2.5} fill={denim} />
          <rect x={-10.5} y={-7} width={10} height={6.5} rx={3} fill={boot} />
          <rect x={0.5} y={-7} width={10} height={6.5} rx={3} fill={boot} />
          {/* thân: áo + yếm */}
          <path d="M-11 -40 q11 -5 22 0 l1 21 q-12 4 -24 0 z" fill={shirt} />
          <path
            d="M-11 -40 q5 -2.5 11 -2.6 v25.2 q-6 -0.2 -12 -1.8 z"
            fill={shirtDk}
            opacity={0.35}
          />
          <path d="M-8 -31 h16 v12 q-8 3 -16 0 z" fill={denim} />
          <rect x={-8} y={-38} width={3} height={8} fill={denim} />
          <rect x={5} y={-38} width={3} height={8} fill={denim} />
          <circle cx={-6.5} cy={-30} r={1.3} fill="#f3d36b" />
          <circle cx={6.5} cy={-30} r={1.3} fill="#f3d36b" />
          <rect x={-3.5} y={-27} width={7} height={5} rx={1} fill={denimDk} />
          {/* tay trái buông, tay phải nắm cán chĩa */}
          <path
            d="M-11 -39 q-6 6 -6 16"
            stroke={shirt}
            strokeWidth={6}
            strokeLinecap="round"
            fill="none"
          />
          <circle cx={-17} cy={-22} r={3.2} fill={skin} />
          <path
            d="M11 -39 q5 5 4 12"
            stroke={shirt}
            strokeWidth={6}
            strokeLinecap="round"
            fill="none"
          />
          <circle cx={15} cy={-27} r={3.2} fill={skin} />
          {/* cổ + đầu */}
          <rect x={-2.5} y={-44} width={5} height={5} fill={skinDk} />
          <circle cx={0} cy={-50} r={8.5} fill={skin} />
          <circle cx={-8.2} cy={-49} r={2} fill={skinDk} />
          <circle cx={8.2} cy={-49} r={2} fill={skinDk} />
          <circle cx={-3} cy={-50} r={1.2} fill="#3a2414" />
          <circle cx={3} cy={-50} r={1.2} fill="#3a2414" />
          <path
            d="M-3 -46 q3 2.5 6 0"
            stroke="#7a3a22"
            strokeWidth={1.2}
            strokeLinecap="round"
            fill="none"
          />
          <circle cx={-5} cy={-47} r={1.5} fill="#f08a7a" opacity={0.6} />
          <circle cx={5} cy={-47} r={1.5} fill="#f08a7a" opacity={0.6} />
          {/* mũ rơm */}
          <ellipse cx={0} cy={-56} rx={15} ry={4.2} fill="#e3b04b" />
          <path d="M-8 -56 q0 -9 8 -9 q8 0 8 9 z" fill="#efc35e" />
          <path d="M-8 -57.5 q8 2.5 16 0 v-2 q-8 2.5 -16 0 z" fill="#c0392b" />
          <ellipse
            cx={0}
            cy={-56}
            rx={15}
            ry={4.2}
            fill="none"
            stroke="#b9852c"
            strokeWidth={0.8}
          />
        </g>
      </g>
    </g>
  );
}

function Tree({
  x,
  y,
  s = 1,
  fruit,
}: {
  x: number;
  y: number;
  s?: number;
  fruit?: string;
}) {
  const [cx, cy] = pt(x, y);
  const r = 26 * s;
  return (
    <g className="iso-tree">
      <Shadow x={x + 0.15} y={y + 0.15} r={0.45 * s} o={0.25} />
      <rect
        x={cx - 4 * s}
        y={cy - 30 * s}
        width={8 * s}
        height={30 * s}
        rx={3}
        fill="#8a5a2c"
      />
      <circle cx={cx} cy={cy - 44 * s} r={r} fill="url(#leaf)" />
      <circle
        cx={cx - r * 0.6}
        cy={cy - 34 * s}
        r={r * 0.7}
        fill="url(#leaf)"
      />
      <circle
        cx={cx + r * 0.62}
        cy={cy - 36 * s}
        r={r * 0.68}
        fill="url(#leaf2)"
      />
      {fruit && (
        <>
          <circle cx={cx - 9 * s} cy={cy - 48 * s} r={4 * s} fill={fruit} />
          <circle cx={cx + 11 * s} cy={cy - 40 * s} r={4 * s} fill={fruit} />
          <circle cx={cx + 2 * s} cy={cy - 60 * s} r={4 * s} fill={fruit} />
        </>
      )}
    </g>
  );
}

function Pine({ x, y, s = 1 }: { x: number; y: number; s?: number }) {
  const [cx, cy] = pt(x, y);
  return (
    <g>
      <Shadow x={x + 0.12} y={y + 0.12} r={0.35 * s} o={0.22} />
      <rect
        x={cx - 3.5 * s}
        y={cy - 16 * s}
        width={7 * s}
        height={16 * s}
        fill="#7a4b22"
      />
      <polygon
        points={poly([
          [cx, cy - 78 * s],
          [cx + 24 * s, cy - 14 * s],
          [cx - 24 * s, cy - 14 * s],
        ])}
        fill="#2f7d32"
      />
      <polygon
        points={poly([
          [cx, cy - 78 * s],
          [cx + 24 * s, cy - 14 * s],
          [cx, cy - 18 * s],
        ])}
        fill="#25692a"
      />
    </g>
  );
}

/** Hàng rào gỗ quanh một hình chữ nhật trên lưới. part: "back" (cạnh xa) hoặc "front" (cạnh gần). */
function Fence({
  gx,
  gy,
  w,
  d,
  part,
}: {
  gx: number;
  gy: number;
  w: number;
  d: number;
  part: "back" | "front";
}) {
  const els: ReactNode[] = [];
  const seg = (x1: number, y1: number, x2: number, y2: number, key: string) => {
    const n = Math.max(1, Math.round(Math.hypot(x2 - x1, y2 - y1) / 0.5));
    for (const zr of [8, 17]) {
      const a = pt(x1, y1, zr);
      const b = pt(x2, y2, zr);
      els.push(
        <line
          key={`${key}r${zr}`}
          x1={a[0]}
          y1={a[1]}
          x2={b[0]}
          y2={b[1]}
          stroke="#b07a43"
          strokeWidth={3.2}
          strokeLinecap="round"
        />,
      );
    }
    for (let i = 0; i <= n; i++) {
      const x = x1 + ((x2 - x1) * i) / n;
      const y = y1 + ((y2 - y1) * i) / n;
      const a = pt(x, y, 0);
      els.push(
        <rect
          key={`${key}p${i}`}
          x={a[0] - 2.5}
          y={a[1] - 22}
          width={5}
          height={22}
          rx={1.5}
          fill="#8a5a2c"
          stroke="#5e3a17"
          strokeWidth={0.8}
        />,
      );
    }
  };
  if (part === "back") {
    seg(gx, gy, gx + w, gy, "t");
    seg(gx, gy, gx, gy + d, "l");
  } else {
    seg(gx, gy + d, gx + w, gy + d, "b");
    seg(gx + w, gy, gx + w, gy + d, "r");
  }
  return <g>{els}</g>;
}

/** Nhà kho mái hai mảng. */
function Barn({
  x,
  y,
  onClick,
  label,
}: {
  x: number;
  y: number;
  onClick?: () => void;
  label?: string;
}) {
  const w = 3;
  const d = 2.2;
  const h = 46;
  const rh = 34;
  const P = (a: number, b: number, z: number) => pt(x + a, y + b, z);
  const [dx, dy] = P(w, d * 0.5, 0);
  return (
    <g className={onClick ? "iso-click" : undefined} onClick={onClick}>
      <Shadow x={x + w * 0.6} y={y + d * 0.7} r={1.6} o={0.2} />
      <Box
        x={x}
        y={y}
        w={w}
        d={d}
        h={h}
        top="#c94b36"
        left="#c94b36"
        right="#a83a28"
      />
      {/* cửa lớn mặt phải */}
      <polygon
        points={poly([
          P(w, d * 0.28, 0),
          P(w, d * 0.72, 0),
          P(w, d * 0.72, 30),
          P(w, d * 0.28, 30),
        ])}
        fill="#f4ede0"
      />
      <line
        x1={P(w, d * 0.28, 0)[0]}
        y1={P(w, d * 0.28, 0)[1]}
        x2={P(w, d * 0.72, 30)[0]}
        y2={P(w, d * 0.72, 30)[1]}
        stroke="#a83a28"
        strokeWidth={3}
      />
      <line
        x1={P(w, d * 0.72, 0)[0]}
        y1={P(w, d * 0.72, 0)[1]}
        x2={P(w, d * 0.28, 30)[0]}
        y2={P(w, d * 0.28, 30)[1]}
        stroke="#a83a28"
        strokeWidth={3}
      />
      {/* mái: mảng sau, đầu hồi, mảng trước */}
      <polygon
        points={poly([
          P(0, 0, h),
          P(w, 0, h),
          P(w, d / 2, h + rh),
          P(0, d / 2, h + rh),
        ])}
        fill="#7b2a1f"
      />
      <polygon
        points={poly([P(w, 0, h), P(w, d, h), P(w, d / 2, h + rh)])}
        fill="#b9452f"
      />
      <polygon
        points={poly([
          P(0, d, h),
          P(w, d, h),
          P(w, d / 2, h + rh),
          P(0, d / 2, h + rh),
        ])}
        fill="#8e2f22"
      />
      <polygon
        points={poly([
          P(w, d * 0.42, h + 8),
          P(w, d * 0.58, h + 8),
          P(w, d * 0.58, h + 18),
          P(w, d * 0.42, h + 18),
        ])}
        fill="#f7e08a"
      />
      {label && (
        <g transform={`translate(${dx + 46}, ${dy - 70})`}>
          <rect
            x={-34}
            y={-14}
            width={68}
            height={24}
            rx={12}
            fill="#fff8e6"
            stroke="#7a4b22"
            strokeWidth={2.5}
          />
          <SvgLabel
            text={label}
            y={3}
            fontSize={12}
            fontWeight={800}
            fill="#4a2f14"
          />
        </g>
      )}
    </g>
  );
}

function Silo({ x, y }: { x: number; y: number }) {
  const [cx, cy] = pt(x, y);
  return (
    <g>
      <Shadow x={x + 0.2} y={y + 0.2} r={0.5} o={0.2} />
      <rect x={cx - 18} y={cy - 96} width={36} height={96} fill="url(#silo)" />
      <ellipse cx={cx} cy={cy} rx={18} ry={8} fill="#9aa3ad" />
      <ellipse cx={cx} cy={cy - 96} rx={18} ry={8} fill="#c9ced6" />
      <path
        d={`M${cx - 18} ${cy - 96} Q ${cx} ${cy - 126} ${cx + 18} ${cy - 96}`}
        fill="#8e9aa6"
      />
    </g>
  );
}

/** Chuồng nhỏ (chuồng gà): khối gỗ mái một mảng. */
function Hut({ x, y }: { x: number; y: number }) {
  const P = (a: number, b: number, z: number) => pt(x + a, y + b, z);
  return (
    <g>
      <Shadow x={x + 0.9} y={y + 0.8} r={0.9} o={0.2} />
      <Box
        x={x}
        y={y}
        w={1.6}
        d={1.2}
        h={30}
        top="#d9a066"
        left="#c98d4e"
        right="#a8703a"
        stroke="#7a4b22"
      />
      <polygon
        points={poly([
          P(-0.1, -0.1, 30),
          P(1.7, -0.1, 30),
          P(1.7, 1.3, 44),
          P(-0.1, 1.3, 44),
        ])}
        fill="#9b3d2b"
        stroke="#6b2a1d"
        strokeWidth={1.2}
      />
      <polygon
        points={poly([
          P(1.6, 0.45, 0),
          P(1.6, 0.85, 0),
          P(1.6, 0.85, 18),
          P(1.6, 0.45, 18),
        ])}
        fill="#4a2f14"
      />
    </g>
  );
}

/* ---------- Ô ruộng ---------- */

const SPOTS3: P[] = [0.22, 0.5, 0.78].flatMap((b) =>
  [0.22, 0.5, 0.78].map((a) => [a, b] as P),
);
const SPOTS2: P[] = [0.32, 0.68].flatMap((b) =>
  [0.32, 0.68].map((a) => [a, b] as P),
);

function PlotTile({
  plot,
  x,
  y,
  crop,
  live,
  me,
  busy,
  onClick,
  part = "body",
}: {
  part?: "body" | "hud";
  plot: Plot;
  x: number;
  y: number;
  crop?: Crop;
  live: { state: PlotState; progress: number; left: number };
  me: boolean;
  busy: boolean;
  onClick: () => void;
}) {
  const st = live.state;
  const wet = plot.waters > 0 && st === "growing";
  const s = 0.9; // cạnh ô (chừa rãnh giữa các ô)
  const glyph =
    st === "empty"
      ? null
      : st === "withered"
        ? "🥀"
        : st === "ripe"
          ? crop?.emoji
          : live.progress < 0.34
            ? "🌱"
            : live.progress < 0.67
              ? "🌿"
              : crop?.emoji;
  const big = crop?.id === "dua_hau" || crop?.id === "bi_ngo";
  const spots = big ? SPOTS2 : SPOTS3;
  const size =
    st === "ripe"
      ? big
        ? 30
        : 24
      : st === "withered"
        ? 18
        : live.progress < 0.34
          ? 13
          : live.progress < 0.67
            ? 17
            : big
              ? 22
              : 19;
  const top = wet ? "#6e4426" : st === "empty" ? "#a87447" : "#8f5c35";
  const cls = [
    "iso-plot",
    `is-${st}`,
    busy && "is-busy",
    !me && st === "ripe" && plot.can_steal && "is-steal",
  ]
    .filter(Boolean)
    .join(" ");
  const [lx, ly] = pt(x + s / 2, y + s / 2, 7);
  const label =
    st === "growing"
      ? formatLeft(live.left)
      : !me && st === "ripe" && plot.can_steal
        ? "Trộm!"
        : null;
  if (part === "hud") {
    // lớp nhãn vẽ sau cùng (trên mọi ô), nổi phía trên khóm cây giữa ô
    if (!label) return null;
    return (
      <g
        transform={`translate(${lx}, ${ly - 40})`}
        className="iso-tag iso-click"
        onClick={onClick}
      >
        <rect
          x={-26}
          y={-11}
          width={52}
          height={18}
          rx={9}
          className={!me && st === "ripe" ? "is-steal" : undefined}
        />
        <text x={0} y={3} textAnchor="middle">
          {label}
        </text>
      </g>
    );
  }
  return (
    <g className={cls} onClick={onClick}>
      <Box
        x={x}
        y={y}
        w={s}
        d={s}
        h={7}
        top={top}
        left="#6b4423"
        right="#56361b"
      />
      {/* luống: các rãnh song song trên mặt đất */}
      {[0.25, 0.5, 0.75].map((f) => {
        const a = pt(x + 0.06, y + f * s, 7);
        const b = pt(x + s - 0.06, y + f * s, 7);
        return (
          <line
            key={f}
            x1={a[0]}
            y1={a[1]}
            x2={b[0]}
            y2={b[1]}
            stroke="rgba(40,20,5,0.35)"
            strokeWidth={2}
          />
        );
      })}
      {st === "ripe" && (
        <polygon
          className="iso-ripe-glow"
          points={poly([
            pt(x, y, 7),
            pt(x + s, y, 7),
            pt(x + s, y + s, 7),
            pt(x, y + s, 7),
          ])}
        />
      )}
      {glyph &&
        spots.map(([a, b], i) => (
          <Sprite
            key={i}
            x={x + a * s}
            y={y + b * s}
            z={7}
            size={size}
            className={st === "ripe" ? "iso-bob" : undefined}
          >
            {glyph}
          </Sprite>
        ))}
      {st === "empty" && me && (
        <Sprite
          x={x + s / 2}
          y={y + s / 2}
          z={7}
          size={22}
          className="iso-plus"
        >
          ＋
        </Sprite>
      )}
      {plot.bug && st === "growing" && (
        <Sprite x={x + 0.25} y={y + 0.15} z={30} size={18} className="iso-bug">
          🐛
        </Sprite>
      )}
      {wet && (
        <Sprite x={x + 0.85} y={y + 0.15} z={26} size={11}>
          💧
        </Sprite>
      )}
    </g>
  );
}

/* ---------- Vật nuôi ---------- */

function AnimalSprite({
  a,
  x,
  y,
  emoji,
  product,
  now,
  me,
  onClick,
  part = "body",
}: {
  part?: "body" | "hud";
  a: Animal;
  x: number;
  y: number;
  emoji: string;
  product?: string;
  now: number;
  me: boolean;
  onClick: () => void;
}) {
  const [cx, cy] = pt(x, y);
  const ready =
    a.state === "ready" ||
    (a.state === "producing" &&
      a.ready_at &&
      new Date(a.ready_at).getTime() <= now);
  const left = a.ready_at ? new Date(a.ready_at).getTime() - now : 0;
  if (part === "body")
    return (
      <g className={`iso-animal${me ? " iso-click" : ""}`} onClick={onClick}>
        <Shadow x={x} y={y} r={0.3} o={0.25} />
        <SvgEmo
          e={emoji}
          x={cx}
          y={cy + 4}
          size={34}
          className="iso-graze"
          style={{ transformOrigin: `${cx}px ${cy}px` }}
        />
      </g>
    );
  return (
    <g className={`iso-animal${me ? " iso-click" : ""}`} onClick={onClick}>
      {ready ? (
        <g
          transform={`translate(${cx}, ${cy - 52})`}
          className="iso-bubble is-ready"
        >
          <circle r={15} />
          <SvgEmo e={product ?? "⭐"} x={0} y={6} size={17} />
        </g>
      ) : a.state === "hungry" ? (
        me && (
          <g
            transform={`translate(${cx}, ${cy - 50})`}
            className="iso-bubble is-hungry"
          >
            <circle r={12} />
            <SvgEmo e="🍽️" x={0} y={5} size={13} />
          </g>
        )
      ) : (
        <g transform={`translate(${cx}, ${cy - 46})`} className="iso-tag">
          <rect x={-22} y={-10} width={44} height={16} rx={8} />
          <text x={0} y={2} textAnchor="middle">
            {formatLeft(left)}
          </text>
        </g>
      )}
    </g>
  );
}

/* ---------- Cả thế giới ---------- */

export type WorldHandlers = {
  onPlot: (p: Plot) => void;
  onAnimal: (a: Animal) => void;
  onBuild: (id: "coop" | "pasture") => void;
  onTend: (id: "coop" | "pasture") => void;
  onKitchen: () => void;
  onJuicer: () => void;
  /** bấm cây: ready thì hái, chưa thì cho biết còn bao lâu */
  onTree: (t: TreeView) => void;
  /** bấm chỗ trồng trống: mở cửa hàng cây giống */
  onOrchardSpot: (spot: number) => void;
  onOrchardHarvest: () => void;
  onExpand: () => void;
  onStorage: () => void;
  onOrders: () => void;
  /** đang chọn chỗ đặt công trình: chạm / kéo trên bản đồ để dời chỗ */
  onPlaceSpot?: (s: Spot) => void;
};

/** Đang chọn chỗ đặt công trình tự do id (xem lib/farm.ts spotProblem). */
export type Placing = {
  id: string;
  spot: Spot;
  ok: boolean;
  size: { w: number; d: number };
};

export function FarmWorld({
  view,
  now,
  busy,
  live,
  dog,
  placing = null,
  ...h
}: {
  view: FarmView;
  now: number;
  busy: boolean;
  dog: boolean;
  live: (p: Plot) => { state: PlotState; progress: number; left: number };
  placing?: Placing | null;
} & WorldHandlers) {
  const me = view.me;
  const crop = (id?: string) => view.crops.find((c) => c.id === id);
  const kinds = useMemo(
    () => new Map((view.animal_kinds ?? []).map((k) => [k.id, k])),
    [view.animal_kinds],
  );
  const items = useMemo(
    () => new Map((view.items ?? []).map((i) => [i.id, i])),
    [view.items],
  );
  const coop = view.buildings?.find((b) => b.id === "coop");
  const pasture = view.buildings?.find((b) => b.id === "pasture");
  const kitchen = view.buildings?.find((b) => b.id === "kitchen");
  const jobs = (view.jobs ?? []).filter(
    (j) => (j.building ?? "kitchen") === "kitchen",
  );
  const kitchenOn = (kitchen?.built ?? false) || jobs.length > 0;
  const KITCHEN = kitchenRect(kitchen?.spot ?? KITCHEN_SPOT);
  // đang dời / chọn chỗ cho xưởng: xưởng thật ẩn đi, vẽ bản nháp ở chỗ đang chọn
  const placingKitchen = placing?.id === "kitchen";
  const juicer = view.buildings?.find((b) => b.id === "juicer");
  const juicerJobs = (view.jobs ?? []).filter((j) => j.building === "juicer");
  const juicerOn = (juicer?.built ?? false) || juicerJobs.length > 0;
  const JUICER = juicerRect(juicer?.spot ?? JUICER_SPOT);
  const placingJuicer = placing?.id === "juicer";
  const juicerDone = juicerJobs.filter(
    (j) => j.state === "ready" || new Date(j.ready_at).getTime() <= now,
  );
  const juicerWorking = juicerJobs.length > juicerDone.length;
  // vườn cây ăn quả
  const orchardOpen = view.orchard_spots ?? 0;
  const treeBySpot = new Map((view.trees ?? []).map((t) => [t.spot, t]));
  const treeKinds = new Map((view.tree_kinds ?? []).map((k) => [k.id, k]));
  const readyTrees = (view.trees ?? []).filter((t) => treeLive(t, now).ready);
  const perTree = 3 + (view.perks?.harvest_bonus ?? 0);
  const jobDone = jobs.filter(
    (j) => j.state === "ready" || new Date(j.ready_at).getTime() <= now,
  );
  const jobWorking = jobs.length > jobDone.length;

  // ô ruộng theo bậc mở rộng; bậc kế tiếp hiện thành vùng đất "chờ mở"
  const owned = view.plots.length;
  const tier = layoutFor(owned);
  const nextPlots =
    view.expansion?.next?.plots ?? (me && view.next_plot ? owned + 1 : owned);
  const nextTier = layoutFor(Math.max(owned, nextPlots));
  const plotXY = (i: number): P => [
    FIELD.gx + (i % tier.cols),
    FIELD.gy + Math.floor(i / tier.cols),
  ];

  const buildingOf = (kind: string) =>
    kinds.get(kind)?.building ?? ANIMAL_FALLBACK[kind]?.building;
  const emojiOf = (kind: string) =>
    kinds.get(kind)?.emoji ?? ANIMAL_FALLBACK[kind]?.emoji ?? "🐾";
  const productOf = (kind: string) =>
    items.get(kinds.get(kind)?.product ?? "")?.emoji ??
    ANIMAL_FALLBACK[kind]?.product;
  const coopAnimals = (view.animals ?? []).filter(
    (a) => buildingOf(a.kind) === "coop",
  );
  const pastureAnimals = (view.animals ?? []).filter(
    (a) => buildingOf(a.kind) === "pasture",
  );
  const spotIn = (
    r: { gx: number; gy: number; w: number; d: number },
    i: number,
    skip = 0,
  ): P => {
    const cols = Math.max(2, Math.floor(r.w / 1.25));
    const k = i + skip;
    return [
      r.gx + 0.8 + (k % cols) * 1.25 + (Math.floor(k / cols) % 2) * 0.55,
      r.gy + 0.9 + Math.floor(k / cols) * 1.15,
    ];
  };

  /* ---- pan & zoom ---- */
  const wrap = useRef<HTMLDivElement>(null);
  const [cam, setCam] = useState<{ x: number; y: number; z: number } | null>(
    null,
  );
  const fit = useCallback(() => {
    const el = wrap.current;
    if (!el) return;
    // máy tính: bảng nổi bên phải che ~23.5rem; nhìn trọn khu chính (chuồng, ruộng, nhà kho)
    const wide = el.clientWidth > 960;
    const panel = wide ? 376 : 0;
    const usable = el.clientWidth - panel;
    if (wide) {
      const c = pt(8, 7.6);
      const z = Math.max(
        0.5,
        Math.min(1.3, Math.min(usable / 1300, el.clientHeight / 760)),
      );
      setCam({ x: c[0] + panel / 2 / z, y: c[1], z });
    } else {
      // điện thoại: căn vào ruộng, kéo để xem chuồng
      const c = pt(FIELD.gx + 1.6, FIELD.gy + 1.4);
      setCam({
        x: c[0],
        y: c[1],
        z: Math.max(0.55, Math.min(1, usable / 620)),
      });
    }
  }, []);
  useEffect(() => {
    fit();
    const ro = new ResizeObserver(fit);
    if (wrap.current) ro.observe(wrap.current);
    return () => ro.disconnect();
  }, [fit]);
  const drag = useRef<{
    x: number;
    y: number;
    cx: number;
    cy: number;
    moved: boolean;
    ghost: boolean; // kéo bản nháp công trình thay vì kéo bản đồ
  } | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  /** Ô công trình đang chọn chỗ khi tâm của nó nằm dưới ngón tay / con trỏ (clientX, clientY). */
  const spotAt = (clientX: number, clientY: number): Spot | null => {
    const svg = svgRef.current;
    const m = svg?.getScreenCTM();
    if (!svg || !m || !placing) return null;
    const q = new DOMPoint(clientX, clientY).matrixTransform(m.inverse());
    const gx = (q.x / (TW / 2) + q.y / (TH / 2)) / 2;
    const gy = (q.y / (TH / 2) - q.x / (TW / 2)) / 2;
    const { w, d } = placing.size;
    return {
      x: Math.max(0, Math.min(GW - w, Math.round(gx - w / 2))),
      y: Math.max(0, Math.min(GH - d, Math.round(gy - d / 2))),
    };
  };
  const onDown = (e: RPointerEvent) => {
    if (!cam) return;
    drag.current = {
      x: e.clientX,
      y: e.clientY,
      cx: cam.x,
      cy: cam.y,
      moved: false,
      ghost: !!placing && !!(e.target as Element).closest?.(".iso-ghost"),
    };
  };
  const onMove = (e: RPointerEvent) => {
    const d = drag.current;
    if (!d || !cam) return;
    const dx = e.clientX - d.x;
    const dy = e.clientY - d.y;
    if (!d.moved && Math.hypot(dx, dy) < 6) return;
    if (!d.moved) (e.currentTarget as Element).setPointerCapture(e.pointerId);
    d.moved = true;
    if (d.ghost) {
      const s = spotAt(e.clientX, e.clientY);
      if (s && (s.x !== placing?.spot.x || s.y !== placing?.spot.y))
        h.onPlaceSpot?.(s);
      return;
    }
    setCam({ ...cam, x: d.cx - dx / cam.z, y: d.cy - dy / cam.z });
  };
  const onUp = () => {
    // kéo xong thì chặn cú "click" ngay sau đó (không gieo hạt nhầm khi thả tay)
    if (drag.current?.moved) {
      const stop = (ev: Event) => {
        ev.stopPropagation();
        ev.preventDefault();
      };
      window.addEventListener("click", stop, { capture: true, once: true });
      window.setTimeout(
        () => window.removeEventListener("click", stop, { capture: true }),
        50,
      );
    }
    drag.current = null;
  };
  const zoomBy = (f: number) =>
    cam && setCam({ ...cam, z: Math.max(0.45, Math.min(1.8, cam.z * f)) });
  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      setCam((c) =>
        c
          ? {
              ...c,
              z: Math.max(
                0.45,
                Math.min(1.8, c.z * (e.deltaY < 0 ? 1.1 : 0.9)),
              ),
            }
          : c,
      );
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  const W = wrap.current?.clientWidth ?? 1200;
  const H = wrap.current?.clientHeight ?? 800;
  const vb = cam
    ? `${cam.x - W / 2 / cam.z} ${cam.y - H / 2 / cam.z} ${W / cam.z} ${H / cam.z}`
    : "-800 -100 1600 1000";

  /* ---- vật đứng: sắp xếp theo độ sâu (gx + gy) ---- */
  type Item = { depth: number; node: ReactNode };
  const things: Item[] = [];
  const add = (gx: number, gy: number, node: ReactNode) =>
    things.push({ depth: gx + gy, node });

  // hàng cây bao quanh
  for (let i = 0; i < GW; i += 1.6)
    add(
      i,
      -0.4,
      i % 3.2 < 1.6 ? (
        <Pine key={`tp${i}`} x={i} y={-0.4} s={1.1} />
      ) : (
        <Tree key={`tt${i}`} x={i} y={-0.4} s={1.15} />
      ),
    );
  for (let j = 1.2; j < GH; j += 1.6)
    add(
      -0.4,
      j,
      j % 3.2 < 1.6 ? (
        <Tree key={`lt${j}`} x={-0.4} y={j} s={1.1} />
      ) : (
        <Pine key={`lp${j}`} x={-0.4} y={j} s={1.05} />
      ),
    );
  // vườn cây ăn quả (bên phải ruộng): 12 chỗ lưới 4 × 3
  for (let i = 0; i < ORCHARD_SPOTS; i++) {
    const { x, y } = orchardSpotXY(i);
    const t = treeBySpot.get(i);
    if (i >= orchardOpen) {
      // chỗ khoá chỉ nói với chủ vườn
      if (me)
        add(
          x,
          y,
          <g key={`os${i}`}>
            <SpotStake x={x} y={y} locked />
            <OrchardTag x={x} y={y} text={`🔒 Mở ở cấp ${orchardUnlockLevel(i)}`} />
          </g>,
        );
    } else if (!t) {
      add(
        x,
        y,
        <SpotStake
          key={`os${i}`}
          x={x}
          y={y}
          onClick={me ? () => h.onOrchardSpot(i) : undefined}
          tip={me ? "Trồng cây ăn quả" : undefined}
        />,
      );
    } else {
      const live = treeLive(t, now);
      const young = t.state === "growing" && !live.ready;
      add(
        x,
        y,
        <g
          key={`os${i}`}
          className={me ? "iso-click" : undefined}
          onClick={me ? () => h.onTree(t) : undefined}
        >
          <title>{treeKinds.get(t.tree)?.name ?? "Cây ăn quả"}</title>
          <Tree
            x={x}
            y={y}
            s={young ? 0.5 : 0.95}
            fruit={live.ready ? (FRUIT_COLOR[t.tree] ?? "#ff9f1c") : undefined}
          />
        </g>,
      );
    }
  }
  add(
    11.5,
    2.2,
    <Barn key="barn" x={10} y={1} onClick={me ? h.onStorage : undefined} />,
  );
  add(14.2, 2.4, <Silo key="silo" x={14.2} y={2.4} />);
  // xe tải đơn hàng
  if (me) {
    const [tx, ty] = pt(20.5, 16.3);
    add(
      20.5,
      16.3,
      <g key="truck" className="iso-click iso-truck" onClick={h.onOrders}>
        <Shadow x={20.5} y={16.3} r={0.6} o={0.22} />
        <SvgEmo e="🚚" x={tx} y={ty + 6} size={56} />
        <g transform={`translate(${tx}, ${ty - 62})`} className="iso-sign">
          <rect x={-40} y={-13} width={80} height={24} rx={12} />
          <SvgLabel text="📦 Đơn hàng" y={4} />
        </g>
      </g>,
    );
  }
  // nhà vịt & ao: vịt bơi
  add(
    18.6,
    10.7,
    <Sprite key="duck1" x={18.6} y={10.7} size={26} className="iso-swim">
      🦆
    </Sprite>,
  );
  add(
    19.8,
    11.8,
    <Sprite
      key="duck2"
      x={19.8}
      y={11.8}
      size={22}
      className="iso-swim is-late"
    >
      🦆
    </Sprite>,
  );
  // nông dân, chó, đống rơm, hoa
  add(8.9, 11.3, <Farmer key="farmer" x={8.9} y={11.3} />);
  if (dog)
    add(
      15,
      10.5,
      <Sprite key="dog" x={15} y={10.5} size={34} className="iso-idle">
        🐕
      </Sprite>,
    );
  add(
    15.2,
    1.6,
    <Sprite key="hay1" x={15.2} y={1.6} size={30}>
      🌾
    </Sprite>,
  );
  add(
    15.8,
    2.2,
    <Sprite key="hay2" x={15.8} y={2.2} size={26}>
      🌾
    </Sprite>,
  );
  const FL = ["🌷", "🌻", "🌼", "🌸"];
  for (let i = 0; i < 14; i++) {
    const x = 9.4 + (i % 7) * 0.9;
    const y = 17.2 + Math.floor(i / 7) * 0.8;
    add(
      x,
      y,
      <Sprite key={`f${i}`} x={x} y={y} size={20}>
        {FL[i % 4]}
      </Sprite>,
    );
  }

  // ruộng
  view.plots.forEach((p, i) => {
    const [x, y] = plotXY(i);
    add(
      x + 0.45,
      y + 0.45,
      <PlotTile
        key={`p${p.index}`}
        plot={p}
        x={x}
        y={y}
        crop={crop(p.crop)}
        live={live(p)}
        me={me}
        busy={busy}
        onClick={() => h.onPlot(p)}
      />,
    );
  });

  // chuồng gà
  const coopOn = coop?.built ?? coopAnimals.length > 0;
  add(
    COOP.gx + 0.8,
    COOP.gy + 0.6,
    <Hut key="hut" x={COOP.gx + 0.15} y={COOP.gy + 0.15} />,
  );
  coopAnimals.forEach((a, i) => {
    const [x, y] = spotIn(COOP, i, 2);
    add(
      x,
      y,
      <AnimalSprite
        key={`a${a.id}`}
        a={a}
        x={x}
        y={y}
        emoji={emojiOf(a.kind)}
        product={productOf(a.kind)}
        now={now}
        me={me}
        onClick={() => h.onAnimal(a)}
      />,
    );
  });
  pastureAnimals.forEach((a, i) => {
    const [x, y] = spotIn(PASTURE, i);
    add(
      x,
      y,
      <AnimalSprite
        key={`a${a.id}`}
        a={a}
        x={x}
        y={y}
        emoji={emojiOf(a.kind)}
        product={productOf(a.kind)}
        now={now}
        me={me}
        onClick={() => h.onAnimal(a)}
      />,
    );
  });
  // hàng rào phía trước của chuồng vẽ sau vật nuôi bên trong
  add(
    COOP.gx + COOP.w,
    COOP.gy + COOP.d,
    <Fence key="coopF" {...COOP} part="front" />,
  );
  add(
    PASTURE.gx + PASTURE.w,
    PASTURE.gy + PASTURE.d,
    <Fence key="pasF" {...PASTURE} part="front" />,
  );

  // xưởng chế biến (chưa xây: chỉ có nền đất chờ xây ở vườn mình)
  if (kitchenOn && !placingKitchen)
    add(
      KITCHEN.gx + KITCHEN.w,
      KITCHEN.gy + KITCHEN.d,
      <Workshop
        key="kitchen"
        r={KITCHEN}
        working={jobWorking}
        onClick={me ? h.onKitchen : undefined}
        label={me ? "🍳 Xưởng" : undefined}
      />,
    );

  if (juicerOn && !placingJuicer)
    add(
      JUICER.gx + JUICER.w,
      JUICER.gy + JUICER.d,
      <Juicer
        key="juicer"
        r={JUICER}
        working={juicerWorking}
        onClick={me ? h.onJuicer : undefined}
        label={me ? "🧃 Máy ép" : undefined}
      />,
    );

  things.sort((a, b) => a.depth - b.depth);

  // nhãn đồng hồ / "Trộm!" và bong bóng vật nuôi: vẽ sau cùng để không vật nào che
  const hud: ReactNode[] = [];
  view.plots.forEach((p, i) => {
    const [x, y] = plotXY(i);
    hud.push(
      <PlotTile
        key={`ph${p.index}`}
        part="hud"
        plot={p}
        x={x}
        y={y}
        crop={crop(p.crop)}
        live={live(p)}
        me={me}
        busy={busy}
        onClick={() => h.onPlot(p)}
      />,
    );
  });
  [
    ...coopAnimals.map((a, i) => [a, spotIn(COOP, i, 2)] as const),
    ...pastureAnimals.map((a, i) => [a, spotIn(PASTURE, i)] as const),
  ].forEach(([a, [x, y]]) =>
    hud.push(
      <AnimalSprite
        key={`ah${a.id}`}
        part="hud"
        a={a}
        x={x}
        y={y}
        emoji={emojiOf(a.kind)}
        product={productOf(a.kind)}
        now={now}
        me={me}
        onClick={() => h.onAnimal(a)}
      />,
    ),
  );

  // có đôi: biển 💞 tên người thương nổi trên đầu bác nông dân (ai ghé cũng thấy)
  if (view.couple) {
    const [fx, fy] = pt(8.9, 11.3);
    const who =
      view.couple.partner.display_name || view.couple.partner.username;
    const text = `💞 ${who.length > 14 ? who.slice(0, 13) + "…" : who}`;
    const w = Math.max(70, text.length * 7.2 + 26);
    hud.push(
      <g
        key="couple"
        className="iso-sign is-love"
        transform={`translate(${fx}, ${fy - 88})`}
      >
        <title>{`Người thương · thân mật cấp ${view.couple.level} (${view.couple.level_name})`}</title>
        {/* chạy sang phải từ đầu bác nông dân: bác đứng sát mép trái màn hình điện thoại */}
        <rect x={-22} y={-14} width={w} height={25} rx={12.5} />
        <SvgLabel text={text} x={w / 2 - 22} y={4} />
      </g>,
    );
  }
  // biển "Kho" của nhà kho: vẽ ở lớp trên cùng, không bị tháp silo che mất
  if (me) {
    const [bx, by] = pt(13, 2.1);
    hud.push(
      <g
        key="barn-label"
        className="iso-click"
        onClick={h.onStorage}
        transform={`translate(${bx + 46}, ${by - 70})`}
      >
        <rect
          x={-34}
          y={-14}
          width={68}
          height={24}
          rx={12}
          fill="#fff8e6"
          stroke="#7a4b22"
          strokeWidth={2.5}
        />
        <SvgLabel
          text="🏚️ Kho"
          y={3}
          fontSize={12}
          fontWeight={800}
          fill="#4a2f14"
        />
      </g>,
    );
  }
  if (kitchenOn && !placingKitchen && jobDone.length > 0) {
    const [bx, by] = pt(
      KITCHEN.gx + KITCHEN.w / 2,
      KITCHEN.gy + KITCHEN.d / 2,
      100,
    );
    const r = jobDone[0].recipe;
    hud.push(
      <g
        key="kitchen-ready"
        className={`iso-bubble is-ready${me ? " iso-click" : ""}`}
        transform={`translate(${bx}, ${by})`}
        onClick={me ? h.onKitchen : undefined}
      >
        <circle r={18} />
        <SvgEmo
          e={view.recipes?.find((x) => x.id === r)?.emoji ?? "🍳"}
          x={0}
          y={7}
          size={20}
        />
        {jobDone.length > 1 && (
          <g transform="translate(15, -13)" className="iso-count">
            <circle r={9} />
            <text y={4} textAnchor="middle">
              {jobDone.length}
            </text>
          </g>
        )}
      </g>,
    );
  }

  if (juicerOn && !placingJuicer && juicerDone.length > 0) {
    const [bx, by] = pt(
      JUICER.gx + JUICER.w / 2,
      JUICER.gy + JUICER.d / 2,
      100,
    );
    const r = juicerDone[0].recipe;
    hud.push(
      <g
        key="juicer-ready"
        className={`iso-bubble is-ready${me ? " iso-click" : ""}`}
        transform={`translate(${bx}, ${by})`}
        onClick={me ? h.onJuicer : undefined}
      >
        <circle r={18} />
        <SvgEmo
          e={view.recipes?.find((x) => x.id === r)?.emoji ?? "🧃"}
          x={0}
          y={7}
          size={20}
        />
        {juicerDone.length > 1 && (
          <g transform="translate(15, -13)" className="iso-count">
            <circle r={9} />
            <text y={4} textAnchor="middle">
              {juicerDone.length}
            </text>
          </g>
        )}
      </g>,
    );
  }
  // quả lơ lửng trên cây đã chín
  readyTrees.forEach((t) => {
    if (t.spot >= orchardOpen) return;
    const { x, y } = orchardSpotXY(t.spot);
    const [bx, by] = pt(x, y);
    hud.push(
      <g
        key={`tree-ready${t.spot}`}
        className={`iso-bubble is-ready${me ? " iso-click" : ""}`}
        transform={`translate(${bx}, ${by - 100})`}
        onClick={me ? () => h.onTree(t) : undefined}
      >
        <circle r={14} />
        <SvgEmo
          e={treeKinds.get(t.tree)?.fruit_emoji ?? "🍊"}
          x={0}
          y={6}
          size={16}
        />
      </g>,
    );
  });
  // biển hái tất cả: chỉ ở vườn mình
  if (me && !placing && readyTrees.length > 0) {
    const [sx, sy] = pt(16 + 2.4, 3 - 0.9);
    const text = `🧺 Hái ${readyTrees.length * perTree} quả`;
    hud.push(
      <g
        key="orchard-harvest"
        className={`iso-click iso-sign is-tend${busy ? " is-busy" : ""}`}
        transform={`translate(${sx}, ${sy - 30})`}
        onClick={busy ? undefined : h.onOrchardHarvest}
      >
        <rect x={-62} y={-15} width={124} height={28} rx={14} />
        <SvgLabel text={text} y={4} />
      </g>,
    );
  }

  /* ---- mặt đất ---- */
  const ground: ReactNode[] = [];
  for (let gx = -1; gx < GW; gx++)
    for (let gy = -1; gy < GH; gy++) {
      const v = (((gx * 7 + gy * 13) % 5) + 5) % 5; // toạ độ âm: % trả số âm
      ground.push(
        <polygon
          key={`g${gx}_${gy}`}
          points={poly([
            pt(gx, gy),
            pt(gx + 1, gy),
            pt(gx + 1, gy + 1),
            pt(gx, gy + 1),
          ])}
          fill={["#7cc04a", "#78bb46", "#80c44e", "#74b743", "#7dbf4b"][v]}
        />,
      );
    }
  const path: ReactNode[] = [];
  for (let gy = 0.5; gy < 16; gy += 1)
    path.push(<Stone key={`sv${gy}`} x={8} y={gy} />);
  for (let gx = 0; gx < GW; gx += 1)
    path.push(<Stone key={`sh${gx}`} x={gx} y={16} />);

  // vùng đất mở rộng kế tiếp: khung nét đứt + biển "Mở rộng"
  const expandArea =
    me &&
    nextTier &&
    (nextTier.cols > tier.cols || nextTier.rows > tier.rows) ? (
      // chỉ là khung báo vùng đất sắp mở, không bấm được: bấm nhầm quanh ruộng là mất xu
      <g className="iso-expand" aria-hidden>
        <polygon
          points={poly([
            pt(FIELD.gx - 0.1, FIELD.gy - 0.1, 1),
            pt(FIELD.gx + nextTier.cols, FIELD.gy - 0.1, 1),
            pt(FIELD.gx + nextTier.cols, FIELD.gy + nextTier.rows, 1),
            pt(FIELD.gx - 0.1, FIELD.gy + nextTier.rows, 1),
          ])}
        />
      </g>
    ) : null;
  const signAt = pt(
    FIELD.gx + nextTier.cols - 0.3,
    FIELD.gy + nextTier.rows - 0.3,
  );

  return (
    <div
      ref={wrap}
      className="iso-wrap"
      onPointerDown={onDown}
      onPointerMove={onMove}
      onPointerUp={onUp}
      onPointerCancel={onUp}
    >
      <svg
        ref={svgRef}
        className={`iso-svg${placing ? " is-placing" : ""}`}
        viewBox={vb}
        preserveAspectRatio="xMidYMid meet"
        role="img"
        aria-label="Bản đồ nông trại"
      >
        <defs>
          <radialGradient id="leaf" cx="0.35" cy="0.3" r="0.8">
            <stop offset="0" stopColor="#8fd16a" />
            <stop offset="0.6" stopColor="#4f9e35" />
            <stop offset="1" stopColor="#3a7d27" />
          </radialGradient>
          <radialGradient id="leaf2" cx="0.35" cy="0.3" r="0.8">
            <stop offset="0" stopColor="#7fc35a" />
            <stop offset="1" stopColor="#2f6e22" />
          </radialGradient>
          <linearGradient id="silo" x1="0" x2="1">
            <stop offset="0" stopColor="#d9dee5" />
            <stop offset="0.6" stopColor="#b9c0c9" />
            <stop offset="1" stopColor="#959ea9" />
          </linearGradient>
          <radialGradient id="water" cx="0.4" cy="0.35" r="0.8">
            <stop offset="0" stopColor="#8fdcff" />
            <stop offset="0.7" stopColor="#3aa3dc" />
            <stop offset="1" stopColor="#2b85bd" />
          </radialGradient>
        </defs>
        <g>{ground}</g>
        {/* nền chuồng */}
        <Pen r={COOP} fill="#d8bf86" />
        <Pen r={PASTURE} fill="#9fd36a" />
        {/* ao */}
        <Pond />
        <g>{path}</g>
        {/* hàng rào phía sau, vẽ trước mọi vật */}
        <Fence {...COOP} part="back" />
        <Fence {...PASTURE} part="back" />
        {/* viền ruộng */}
        <polygon
          className="iso-field-bed"
          points={poly([
            pt(FIELD.gx - 0.15, FIELD.gy - 0.15),
            pt(FIELD.gx + tier.cols + 0.05, FIELD.gy - 0.15),
            pt(FIELD.gx + tier.cols + 0.05, FIELD.gy + tier.rows + 0.05),
            pt(FIELD.gx - 0.15, FIELD.gy + tier.rows + 0.05),
          ])}
        />
        {expandArea}
        {things.map((t) => t.node)}
        <g>{hud}</g>
        {placing && (
          <>
            {/* chạm chỗ nào trên bản đồ thì dời bản nháp tới đó; không bấm nhầm vào ruộng, chuồng */}
            <rect
              className="iso-place-catch"
              x={-6000}
              y={-6000}
              width={12000}
              height={12000}
              onClick={(e) => {
                const s = spotAt(e.clientX, e.clientY);
                if (s) h.onPlaceSpot?.(s);
              }}
            />
            <g className={`iso-ghost${placing.ok ? "" : " is-bad"}`}>
              <polygon
                className="iso-ghost-foot"
                points={poly([
                  pt(placing.spot.x, placing.spot.y),
                  pt(placing.spot.x + placing.size.w, placing.spot.y),
                  pt(
                    placing.spot.x + placing.size.w,
                    placing.spot.y + placing.size.d,
                  ),
                  pt(placing.spot.x, placing.spot.y + placing.size.d),
                ])}
              />
              {placingKitchen && (
                <Workshop r={kitchenRect(placing.spot)} working={false} />
              )}
              {placingJuicer && (
                <Juicer r={juicerRect(placing.spot)} working={false} />
              )}
            </g>
          </>
        )}
        {/* biển chuồng chưa xây */}
        {me && !placing && !coopOn && (
          <BuildSign
            r={COOP}
            text={`Xây chuồng gà · ${coop?.price ?? 200}🪙`}
            onClick={() => h.onBuild("coop")}
          />
        )}
        {me && !placing && !(pasture?.built ?? pastureAnimals.length > 0) && (
          <BuildSign
            r={PASTURE}
            text={`Xây đồng cỏ · ${pasture?.price ?? 600}🪙`}
            onClick={() => h.onBuild("pasture")}
          />
        )}
        {me && !placing && coopOn && (
          <TendSign
            view={view}
            building="coop"
            now={now}
            r={COOP}
            busy={busy}
            onClick={() => h.onTend("coop")}
          />
        )}
        {me && !placing && (pasture?.built ?? pastureAnimals.length > 0) && (
          <TendSign
            view={view}
            building="pasture"
            now={now}
            r={PASTURE}
            busy={busy}
            onClick={() => h.onTend("pasture")}
          />
        )}
        {me && !placing && !kitchenOn && (
          <BuildSign
            r={KITCHEN}
            text={
              view.level < (kitchen?.level ?? 4)
                ? `Xưởng chế biến · cấp ${kitchen?.level ?? 4}`
                : `Xây xưởng · ${kitchen?.price ?? 500}🪙`
            }
            locked={view.level < (kitchen?.level ?? 4)}
            onClick={h.onKitchen}
          />
        )}
        {me && !placing && !juicerOn && juicer && (
          <BuildSign
            r={JUICER}
            text={
              view.level < juicer.level
                ? `Máy ép nước · cấp ${juicer.level}`
                : `Xây máy ép · ${juicer.price}🪙`
            }
            locked={view.level < juicer.level}
            onClick={h.onJuicer}
          />
        )}
        {!placing && expandArea && view.expansion?.next && (
          <g
            className="iso-click iso-sign"
            transform={`translate(${signAt[0]}, ${signAt[1] - 6})`}
            onClick={h.onExpand}
          >
            <rect x={-62} y={-14} width={124} height={26} rx={13} />
            <SvgLabel
              text={`🪧 Mở rộng · ${view.expansion.next.price}🪙`}
              y={4}
            />
          </g>
        )}
      </svg>
      <div className="iso-zoom">
        <button
          type="button"
          onClick={() => zoomBy(1.15)}
          aria-label="Phóng to"
        >
          +
        </button>
        <button
          type="button"
          onClick={() => zoomBy(1 / 1.15)}
          aria-label="Thu nhỏ"
        >
          −
        </button>
        <button type="button" onClick={fit} aria-label="Về giữa ruộng">
          ⌂
        </button>
      </div>
    </div>
  );
}

/**
 * Máy ép nước: thùng ép xanh bạc hà, mái hiên sọc cam, ly nước ép khổng lồ trên nóc. Lò đang ép thì
 * ly sủi bọt; có món xong thì bong bóng món nảy trên nóc (vẽ ở lớp hud).
 */
function Juicer({
  r,
  working,
  onClick,
  label,
}: {
  r: { gx: number; gy: number; w: number; d: number };
  working: boolean;
  onClick?: () => void;
  label?: string;
}) {
  const { gx: x, gy: y, w, d } = r;
  const h = 34;
  const P = (a: number, b: number, z: number) => pt(x + a, y + b, z);
  const cup = P(w / 2, d / 2, h);
  const stripes = [0, 1, 2, 3];
  return (
    <g className={onClick ? "iso-click" : undefined} onClick={onClick}>
      <Shadow x={x + w * 0.55} y={y + d * 0.65} r={1.2} o={0.2} />
      <Box
        x={x}
        y={y}
        w={w}
        d={d}
        h={h}
        top="#bfeede"
        left="#a6e3cf"
        right="#86cdb6"
        stroke="#3f8a73"
      />
      {/* quầy cửa sổ mặt trước-trái */}
      <polygon
        points={poly([
          P(w * 0.15, d, 12),
          P(w * 0.85, d, 12),
          P(w * 0.85, d, 26),
          P(w * 0.15, d, 26),
        ])}
        fill={working ? "#ffd56b" : "#e9f7ff"}
        stroke="#3f8a73"
        strokeWidth={1.5}
      />
      {/* mái hiên sọc cam trắng phía trước-trái */}
      {stripes.map((i) => {
        const a = (w * i) / stripes.length;
        const b = (w * (i + 1)) / stripes.length;
        return (
          <polygon
            key={i}
            points={poly([
              P(a, d, h - 4),
              P(b, d, h - 4),
              P(b, d + 0.35, h - 12),
              P(a, d + 0.35, h - 12),
            ])}
            fill={i % 2 ? "#fff6e0" : "#ff9a3c"}
            stroke="#c4651a"
            strokeWidth={1}
          />
        );
      })}
      {/* ly nước ép trên nóc */}
      <polygon
        points={`${cup[0] - 15},${cup[1] - 34} ${cup[0] + 15},${cup[1] - 34} ${cup[0] + 10},${cup[1]} ${cup[0] - 10},${cup[1]}`}
        fill="#ffb24a"
        stroke="#c4651a"
        strokeWidth={1.5}
        strokeLinejoin="round"
      />
      <ellipse
        cx={cup[0]}
        cy={cup[1] - 34}
        rx={15}
        ry={5}
        fill="#ffd28a"
        stroke="#c4651a"
        strokeWidth={1.2}
      />
      <line
        x1={cup[0] + 4}
        y1={cup[1] - 36}
        x2={cup[0] + 11}
        y2={cup[1] - 52}
        stroke="#e63946"
        strokeWidth={3}
        strokeLinecap="round"
      />
      {working && (
        <g className="iso-smoke" aria-hidden>
          {[0, 1, 2].map((i) => (
            <circle
              key={i}
              cx={cup[0]}
              cy={cup[1] - 52}
              r={5 + i * 2}
              style={{ animationDelay: `${i * 0.9}s` }}
            />
          ))}
        </g>
      )}
      {label && (
        <g transform={`translate(${P(w, d, 0)[0] + 30}, ${P(w, d, 0)[1] - 6})`}>
          <rect
            x={-40}
            y={-14}
            width={80}
            height={24}
            rx={12}
            fill="#fff8e6"
            stroke="#7a4b22"
            strokeWidth={2.5}
          />
          <SvgLabel
            text={label}
            y={3}
            fontSize={12}
            fontWeight={800}
            fill="#4a2f14"
          />
        </g>
      )}
    </g>
  );
}

/**
 * Xưởng chế biến: nhà tường kem mái cam, có ống khói. Lò đang nấu thì khói bay; có món xong thì bong
 * bóng món nảy trên mái (vẽ ở lớp hud).
 */
function Workshop({
  r,
  working,
  onClick,
  label,
}: {
  r: { gx: number; gy: number; w: number; d: number };
  working: boolean;
  onClick?: () => void;
  label?: string;
}) {
  const { gx: x, gy: y, w, d } = r;
  const h = 40;
  const rh = 28;
  const P = (a: number, b: number, z: number) => pt(x + a, y + b, z);
  // ống khói trên mảng mái sau
  const ch = P(w * 0.72, d * 0.3, h + rh * 0.55);
  return (
    <g className={onClick ? "iso-click" : undefined} onClick={onClick}>
      <Shadow x={x + w * 0.55} y={y + d * 0.65} r={1.45} o={0.2} />
      <Box
        x={x}
        y={y}
        w={w}
        d={d}
        h={h}
        top="#f6e7c8"
        left="#f3dfb6"
        right="#dcc193"
        stroke="#9a7444"
      />
      {/* cửa sổ có rèm, mặt trước-trái */}
      {[0.25, 0.7].map((a) => (
        <polygon
          key={a}
          points={poly([
            P(w * a - 0.22, d, 14),
            P(w * a + 0.22, d, 14),
            P(w * a + 0.22, d, 30),
            P(w * a - 0.22, d, 30),
          ])}
          fill={working ? "#ffd56b" : "#a9d8f2"}
          stroke="#9a7444"
          strokeWidth={1.5}
        />
      ))}
      {/* cửa ra vào mặt phải */}
      <polygon
        points={poly([
          P(w, d * 0.32, 0),
          P(w, d * 0.68, 0),
          P(w, d * 0.68, 26),
          P(w, d * 0.32, 26),
        ])}
        fill="#8a5a2b"
        stroke="#5e3b18"
        strokeWidth={1.5}
      />
      {/* ống khói (sau mái) */}
      <rect
        x={ch[0] - 7}
        y={ch[1] - 26}
        width={14}
        height={30}
        fill="#b5503a"
        stroke="#7b2f20"
        strokeWidth={1.2}
      />
      {/* mái: mảng sau, đầu hồi, mảng trước */}
      <polygon
        points={poly([
          P(-0.1, -0.1, h),
          P(w + 0.1, -0.1, h),
          P(w + 0.1, d / 2, h + rh),
          P(-0.1, d / 2, h + rh),
        ])}
        fill="#c4562e"
      />
      <polygon
        points={poly([
          P(w + 0.1, -0.1, h),
          P(w + 0.1, d + 0.1, h),
          P(w + 0.1, d / 2, h + rh),
        ])}
        fill="#f0d9ac"
        stroke="#9a7444"
        strokeWidth={1.2}
      />
      <polygon
        points={poly([
          P(-0.1, d + 0.1, h),
          P(w + 0.1, d + 0.1, h),
          P(w + 0.1, d / 2, h + rh),
          P(-0.1, d / 2, h + rh),
        ])}
        fill="#e06b3a"
        stroke="#a8431f"
        strokeWidth={1.2}
      />
      {working && (
        <g className="iso-smoke" aria-hidden>
          {[0, 1, 2].map((i) => (
            <circle
              key={i}
              cx={ch[0]}
              cy={ch[1] - 30}
              r={7 + i * 2}
              style={{ animationDelay: `${i * 0.9}s` }}
            />
          ))}
        </g>
      )}
      {label && (
        <g transform={`translate(${P(w, d, 0)[0] + 30}, ${P(w, d, 0)[1] - 6})`}>
          <rect
            x={-40}
            y={-14}
            width={80}
            height={24}
            rx={12}
            fill="#fff8e6"
            stroke="#7a4b22"
            strokeWidth={2.5}
          />
          <SvgLabel
            text={label}
            y={3}
            fontSize={12}
            fontWeight={800}
            fill="#4a2f14"
          />
        </g>
      )}
    </g>
  );
}

function Stone({ x, y }: { x: number; y: number }) {
  const s = 0.86;
  const o = (1 - s) / 2;
  return (
    <polygon
      points={poly([
        pt(x + o, y + o, 1),
        pt(x + o + s, y + o, 1),
        pt(x + o + s, y + o + s, 1),
        pt(x + o, y + o + s, 1),
      ])}
      fill="#cfc8b6"
      stroke="#a89f88"
      strokeWidth={1.5}
    />
  );
}

function Pen({
  r,
  fill,
}: {
  r: { gx: number; gy: number; w: number; d: number };
  fill: string;
}) {
  return (
    <polygon
      points={poly([
        pt(r.gx, r.gy),
        pt(r.gx + r.w, r.gy),
        pt(r.gx + r.w, r.gy + r.d),
        pt(r.gx, r.gy + r.d),
      ])}
      fill={fill}
      opacity={0.95}
    />
  );
}

function Pond() {
  const [cx, cy] = pt(19.2, 11.2);
  return (
    <g>
      <ellipse cx={cx} cy={cy + 4} rx={150} ry={78} fill="#a9d36f" />
      <ellipse cx={cx} cy={cy + 4} rx={140} ry={70} fill="#c8b48a" />
      <ellipse cx={cx} cy={cy} rx={128} ry={62} fill="url(#water)" />
      <ellipse
        cx={cx - 40}
        cy={cy - 18}
        rx={26}
        ry={7}
        fill="rgba(255,255,255,0.45)"
      />
    </g>
  );
}

/** Cọc đất đánh dấu chỗ trồng cây (trống hoặc còn khoá). */
function SpotStake({
  x,
  y,
  locked = false,
  onClick,
  tip,
}: {
  x: number;
  y: number;
  locked?: boolean;
  onClick?: () => void;
  tip?: string;
}) {
  const [cx, cy] = pt(x, y);
  return (
    <g
      className={`iso-stake${onClick ? " iso-click" : ""}${locked ? " is-locked" : ""}`}
      onClick={onClick}
    >
      {tip && <title>{tip}</title>}
      <ellipse
        cx={cx}
        cy={cy + 2}
        rx={22}
        ry={11}
        fill={locked ? "#8a7a62" : "#8a5a2c"}
        opacity={locked ? 0.45 : 0.7}
      />
      <rect
        x={cx - 2}
        y={cy - 18}
        width={4}
        height={20}
        rx={1.5}
        fill="#c79a5b"
        stroke="#7a4b22"
        strokeWidth={1}
      />
      {onClick && <circle cx={cx} cy={cy - 20} r={4} fill="#7bd36b" />}
    </g>
  );
}

/** Nhãn nhỏ trên chỗ trồng cây ("Mở ở cấp N"). */
function OrchardTag({ x, y, text }: { x: number; y: number; text: string }) {
  const [cx, cy] = pt(x, y);
  return (
    <g transform={`translate(${cx}, ${cy - 30})`} className="iso-sign is-build is-locked">
      <rect x={-48} y={-10} width={96} height={19} rx={9.5} />
      <SvgLabel text={text} y={3} fontSize={10} />
    </g>
  );
}

function BuildSign({
  r,
  text,
  onClick,
  locked = false,
}: {
  r: { gx: number; gy: number; w: number; d: number };
  text: string;
  onClick: () => void;
  locked?: boolean;
}) {
  const [cx, cy] = pt(r.gx + r.w / 2, r.gy + r.d / 2);
  return (
    <g
      className={`iso-click iso-sign is-build${locked ? " is-locked" : ""}`}
      transform={`translate(${cx}, ${cy})`}
      onClick={onClick}
    >
      <rect x={-78} y={-15} width={156} height={28} rx={14} />
      <SvgLabel text={`${locked ? "🔒" : "🔨"} ${text}`} y={4} />
    </g>
  );
}

/** Biển "chăm nhanh" trên chuồng: một chạm thu sản phẩm + cho ăn cả chuồng (tự mua thức ăn thiếu). */
function TendSign({
  view,
  building,
  now,
  r,
  busy,
  onClick,
}: {
  view: FarmView;
  building: "coop" | "pasture";
  now: number;
  r: { gx: number; gy: number; w: number; d: number };
  busy: boolean;
  onClick: () => void;
}) {
  const plan = tendPlan(view, building, now);
  if (!plan) return null;
  const emoji = (id: string) =>
    view.items?.find((i) => i.id === id)?.emoji ??
    view.crops.find((c) => c.id === id)?.emoji ??
    "📦";
  const parts: string[] = [];
  if (plan.collect > 0)
    parts.push(`${building === "coop" ? "🥚" : "🧺"} Thu ${plan.collect}`);
  if (plan.feed > 0) parts.push(`🍽️ Cho ăn ${plan.feed}`);
  if (plan.cost > 0)
    parts.push(
      `mua ${Object.entries(plan.buy)
        .map(([id, n]) => `${n}${emoji(id)}`)
        .join(" ")} ${plan.cost}🪙`,
    );
  const text = parts.join(" · ");
  const w = Math.max(110, text.length * 7 + 28);
  // đặt phía trên khối chuồng để không che đàn vật nuôi
  const [cx, cy] = pt(r.gx + r.w / 2, r.gy);
  return (
    <g
      className={`iso-click iso-sign is-tend${busy ? " is-busy" : ""}`}
      transform={`translate(${cx}, ${cy - 34})`}
      onClick={busy ? undefined : onClick}
    >
      <rect x={-w / 2} y={-15} width={w} height={28} rx={14} />
      <SvgLabel text={text} y={4} />
    </g>
  );
}
