// Vá cho trình duyệt cũ (chủ yếu iPhone iOS 15): thiếu thì bổ sung, có rồi thì để nguyên.

// Canvas roundRect (Safari 16+): game Trạm Vũ Trụ vẽ bo góc bằng hàm này.
type Radii = number | DOMPointInit | (number | DOMPointInit)[];
for (const proto of [CanvasRenderingContext2D.prototype, globalThis.Path2D?.prototype].filter(Boolean)) {
  const p = proto as CanvasRenderingContext2D;
  if (typeof p.roundRect === "function") continue;
  p.roundRect = function (this: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, radii?: Radii) {
    const list = Array.isArray(radii) ? radii : [radii ?? 0];
    const num = (r: number | DOMPointInit | undefined) => (typeof r === "number" ? r : (r?.x ?? 0));
    const [tl, tr, br, bl] =
      list.length === 1
        ? [list[0], list[0], list[0], list[0]].map(num)
        : list.length === 2
          ? [list[0], list[1], list[0], list[1]].map(num)
          : list.length === 3
            ? [list[0], list[1], list[2], list[1]].map(num)
            : list.slice(0, 4).map(num);
    const k = Math.min(
      1,
      Math.abs(w) / 2 / Math.max(tl, tr, br, bl, 1e-9),
      Math.abs(h) / 2 / Math.max(tl, tr, br, bl, 1e-9),
    );
    const [a, b, c, d] = [tl * k, tr * k, br * k, bl * k];
    this.moveTo(x + a, y);
    this.arcTo(x + w, y, x + w, y + h, b);
    this.arcTo(x + w, y + h, x, y + h, c);
    this.arcTo(x, y + h, x, y, d);
    this.arcTo(x, y, x + w, y, a);
    this.closePath();
  };
}
