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
  const strokeColor = isRed ? "#dc2626" : "#0f172a";
  const textColor = isRed ? "#b91c1c" : "#1e293b";
  const ringColor = isRed ? "#f87171" : "#94a3b8";

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 50 50"
      className={className ? `xiangqi-piece ${className}` : "xiangqi-piece"}
      style={{ userSelect: "none", overflow: "visible" }}
    >
      <defs>
        {/* Đổ bóng 3D nhẹ */}
        <filter id={`xq-shadow-${piece}`} x="-15%" y="-15%" width="130%" height="130%">
          <feDropShadow dx="0" dy="2" stdDeviation="1.5" floodOpacity="0.4" />
        </filter>
        {/* Mặt gỗ / ngà sáng bóng */}
        <radialGradient id={`xq-grad-${piece}`} cx="38%" cy="32%" r="65%">
          <stop offset="0%" stopColor="#fffdfa" />
          <stop offset="60%" stopColor="#fef3c7" />
          <stop offset="100%" stopColor="#f5deb3" />
        </radialGradient>
      </defs>

      {/* Thân quân tròn ngoài */}
      <circle
        cx="25"
        cy="25"
        r="22.5"
        fill={`url(#xq-grad-${piece})`}
        stroke="#8b5a2b"
        strokeWidth="1.2"
        filter={`url(#xq-shadow-${piece})`}
      />

      {/* Vòng viền chỉ màu bên trong */}
      <circle
        cx="25"
        cy="25"
        r="19"
        fill="none"
        stroke={strokeColor}
        strokeWidth="1.4"
      />
      <circle
        cx="25"
        cy="25"
        r="17"
        fill="none"
        stroke={ringColor}
        strokeWidth="0.6"
        strokeDasharray="2 1.5"
        opacity="0.8"
      />

      {/* Chữ Hán thư pháp lớn */}
      <text
        x="25"
        y="27.5"
        textAnchor="middle"
        dominantBaseline="central"
        fill={textColor}
        fontSize="21"
        fontWeight="bold"
        fontFamily='"Noto Serif TC", "KaiTi", "STKaiti", "Baskerville", "Georgia", serif'
        style={{ pointerEvents: "none" }}
      >
        {hanzi}
      </text>

      {/* Nhãn tiếng Việt phụ bên dưới */}
      <text
        x="25"
        y="39"
        textAnchor="middle"
        dominantBaseline="central"
        fill={textColor}
        fontSize="6.5"
        fontWeight="700"
        fontFamily="system-ui, -apple-system, sans-serif"
        letterSpacing="0.2px"
        opacity="0.85"
        style={{ pointerEvents: "none" }}
      >
        {vn}
      </text>
    </svg>
  );
}
