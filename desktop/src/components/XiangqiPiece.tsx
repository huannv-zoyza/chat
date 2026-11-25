// Định nghĩa thông tin hiển thị của từng quân cờ Cờ tướng
type PieceMeta = {
  hanzi: string;
  vn: string;
  isRed: boolean;
};

const PIECE_INFO: Record<string, PieceMeta> = {
  // Phe Đỏ (Chữ hoa)
  K: { hanzi: "帥", vn: "Tướng", isRed: true },
  A: { hanzi: "仕", vn: "Sĩ", isRed: true },
  E: { hanzi: "相", vn: "Tượng", isRed: true },
  H: { hanzi: "傌", vn: "Mã", isRed: true },
  R: { hanzi: "俥", vn: "Xe", isRed: true },
  C: { hanzi: "炮", vn: "Pháo", isRed: true },
  P: { hanzi: "兵", vn: "Tốt", isRed: true },

  // Phe Đen (Chữ thường)
  k: { hanzi: "將", vn: "Tướng", isRed: false },
  a: { hanzi: "士", vn: "Sĩ", isRed: false },
  e: { hanzi: "象", vn: "Tượng", isRed: false },
  h: { hanzi: "馬", vn: "Mã", isRed: false },
  r: { hanzi: "車", vn: "Xe", isRed: false },
  c: { hanzi: "砲", vn: "Pháo", isRed: false },
  p: { hanzi: "卒", vn: "Tốt", isRed: false },
};

/** Font chữ Hán nét đậm, rõ ở cỡ nhỏ (tránh KaiTi: nét bút lông mảnh, nhìn nhoè). */
const HANZI_FONT =
  '"Noto Serif CJK TC", "Noto Serif TC", "Source Han Serif TC", "Songti TC", "PMingLiU", "MingLiU", "SimSun", serif';
/** Font giao diện của app: dấu tiếng Việt rõ ở cỡ nhỏ. */
const LABEL_FONT = '"Be Vietnam Pro", system-ui, sans-serif';

export function XiangqiPiece({
  piece,
  className,
  size = 46,
}: {
  piece: string;
  className?: string;
  size?: number;
}) {
  const meta = PIECE_INFO[piece];
  if (!meta) return null;

  const { hanzi, vn, isRed } = meta;
  const ink = isRed ? "#b3121b" : "#14202e";
  // quân nhỏ (quân bị ăn bên cạnh tên người chơi): chữ phụ không đọc được, chỉ để chữ Hán
  const label = size >= 30;
  const gid = `xq-face-${isRed ? "r" : "b"}`;

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 50 50"
      className={className ? `xiangqi-piece ${className}` : "xiangqi-piece"}
      style={{ userSelect: "none", overflow: "visible" }}
      shapeRendering="geometricPrecision"
      textRendering="geometricPrecision"
      role="img"
      aria-label={`${vn} ${isRed ? "Đỏ" : "Đen"}`}
    >
      <title>{`${vn} (${isRed ? "Đỏ" : "Đen"})`}</title>
      <defs>
        {/* mặt quân màu ngà, sáng ở góc trên trái */}
        <radialGradient id={gid} cx="40%" cy="35%" r="70%">
          <stop offset="0%" stopColor="#fffaf0" />
          <stop offset="100%" stopColor="#f1d9a8" />
        </radialGradient>
      </defs>

      {/* bóng đổ: một vòng tối lệch xuống, không làm mờ mép quân */}
      <circle cx="25" cy="26.6" r="23.2" fill="rgb(60 30 5 / 0.35)" />
      {/* thân quân */}
      <circle cx="25" cy="25" r="23" fill={`url(#${gid})`} stroke="#6b4219" strokeWidth="1.6" />
      {/* vòng chỉ màu bên trong */}
      <circle cx="25" cy="25" r="19.2" fill="none" stroke={ink} strokeWidth="1.5" />

      {/* chữ Hán */}
      <text
        x="25"
        y={label ? 21 : 25.5}
        textAnchor="middle"
        dominantBaseline="central"
        fill={ink}
        fontSize={label ? 18.5 : 24}
        fontWeight="900"
        fontFamily={HANZI_FONT}
        style={{ pointerEvents: "none" }}
      >
        {hanzi}
      </text>

      {/* chữ phụ tiếng Việt */}
      {label && (
        <text
          x="25"
          y="35.4"
          textAnchor="middle"
          dominantBaseline="central"
          fill={ink}
          // tên 5 chữ cái (Tướng, Tượng) hẹp lại chút để không chạm vòng viền
          fontSize={vn.length >= 5 ? 8.1 : 8.6}
          fontWeight="700"
          fontFamily={LABEL_FONT}
          letterSpacing="-0.15"
          style={{ pointerEvents: "none" }}
        >
          {vn}
        </text>
      )}
    </svg>
  );
}
