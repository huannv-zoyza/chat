/* Emoji vẽ bằng hình (xem lib/emoji.ts): trong HTML và trong SVG của nông trại. */
import {
  Fragment,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
  type SVGProps,
} from "react";
import { emojiUrl, splitEmoji } from "../lib/emoji";

/** Một emoji trong dòng chữ HTML (cao ~1.2em); chưa có hình thì in chữ. */
export function Emo({
  e,
  className,
}: {
  e: string | undefined | null;
  className?: string;
}) {
  if (!e) return null;
  const url = emojiUrl(e);
  if (!url) return <>{e}</>;
  return (
    <img
      src={url}
      alt={e}
      className={`emo${className ? ` ${className}` : ""}`}
      draggable={false}
    />
  );
}

/** Chữ có lẫn emoji → các đoạn chữ + <Emo>. */
export function emo(text: string | undefined | null): ReactNode {
  if (!text) return text;
  const parts = splitEmoji(text);
  if (!parts.some((p) => p.emoji && emojiUrl(p.text))) return text;
  return parts.map((p, i) => (
    <Fragment key={i}>{p.emoji ? <Emo e={p.text} /> : p.text}</Fragment>
  ));
}

/**
 * Emoji trong SVG ở vị trí giống <text x y fontSize textAnchor="middle">: (x, y) là chân dòng chữ,
 * ở giữa theo chiều ngang. Chưa có hình thì vẽ chữ.
 */
export function SvgEmo({
  e,
  x,
  y,
  size,
  className,
  style,
}: {
  e: string;
  x: number;
  y: number;
  size: number;
  className?: string;
  style?: CSSProperties;
}) {
  const url = emojiUrl(e);
  if (!url)
    return (
      <text
        x={x}
        y={y}
        fontSize={size}
        textAnchor="middle"
        className={className}
        style={style}
      >
        {e}
      </text>
    );
  const s = size * 1.08;
  return (
    <image
      href={url}
      x={x - s / 2}
      y={y - size * 0.86}
      width={s}
      height={s}
      className={className}
      style={style}
      preserveAspectRatio="xMidYMid meet"
    />
  );
}

const EM = "\u2003"; // chỗ trống rộng 1em giữ chỗ cho hình emoji trong dòng chữ SVG

/**
 * Dòng chữ SVG có lẫn emoji (vd. "🔨 Xây chuồng gà · 200🪙"): emoji thành chỗ trống trong chữ,
 * hình đặt đúng chỗ đó sau khi đo (getExtentOfChar), nên căn giữa vẫn chuẩn.
 */
export function SvgLabel({
  text,
  x = 0,
  y = 0,
  className,
  anchor = "middle",
  ...attrs
}: {
  text: string;
  x?: number;
  y?: number;
  className?: string;
  anchor?: "start" | "middle" | "end";
} & Pick<SVGProps<SVGTextElement>, "fontSize" | "fontWeight" | "fill">) {
  const ref = useRef<SVGTextElement>(null);
  const parts = splitEmoji(text).map((p) => ({
    ...p,
    url: p.emoji ? emojiUrl(p.text) : null,
  }));
  const shown = parts.map((p) => (p.url ? EM : p.text)).join("");
  const [boxes, setBoxes] = useState<
    { x: number; y: number; w: number; h: number; url: string }[]
  >([]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const out: typeof boxes = [];
      let i = 0;
      for (const p of parts) {
        const len = p.url ? 1 : p.text.length;
        if (p.url) {
          try {
            const r = el.getExtentOfChar(i);
            out.push({ x: r.x, y: r.y, w: r.width, h: r.height, url: p.url });
          } catch {
            /* chưa vẽ xong */
          }
        }
        i += len;
      }
      setBoxes((old) =>
        JSON.stringify(old) === JSON.stringify(out) ? old : out,
      );
    };
    measure();
    // phông chữ tải xong thì bề rộng chữ đổi: đo lại
    void document.fonts?.ready.then(measure);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shown, x, y]);

  return (
    <>
      <text
        ref={ref}
        x={x}
        y={y}
        textAnchor={anchor}
        className={className}
        {...attrs}
      >
        {shown}
      </text>
      {boxes.map((b, i) => {
        const s = Math.max(b.w, b.h * 0.8) * 1.15;
        return (
          <image
            key={i}
            href={b.url}
            x={b.x + b.w / 2 - s / 2}
            y={b.y + b.h / 2 - s / 2}
            width={s}
            height={s}
            pointerEvents="none"
          />
        );
      })}
    </>
  );
}
