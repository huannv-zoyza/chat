import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { GameView, PublicUser } from "../lib/api";
import type { XiangqiState } from "../lib/games";
import { XiangqiPiece } from "./XiangqiPiece";
import { playPieceCapture, playPiecePlace } from "../lib/sound";
import { Dialog } from "./ui";

export type XiangqiCtx = {
  game: GameView;
  meId: number;
  isPlayer: boolean;
  busy: boolean;
  act: (action: string, data?: unknown) => void;
  user: (id: number) => PublicUser;
  nameOf: (id: number) => string;
  invite: () => void;
  onOpenLeaderboard?: () => void;
};

// Tọa độ bàn cờ:
// 9 cột dọc (files: 0..8 tương ứng 'a'..'i')
// 10 hàng ngang (ranks: 0..9, 0 ở dưới Đỏ, 9 ở trên Đen)
const CELL_SIZE = 44;
const PADDING_X = 28;
const PADDING_Y = 28;
const BOARD_WIDTH = PADDING_X * 2 + 8 * CELL_SIZE; // 28*2 + 352 = 408
const BOARD_HEIGHT = PADDING_Y * 2 + 9 * CELL_SIZE; // 28*2 + 396 = 452

function useSecondsLeft(deadline: string | undefined): number | null {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!deadline) return;
    const t = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(t);
  }, [deadline]);
  if (!deadline) return null;
  return Math.max(0, Math.ceil((new Date(deadline).getTime() - now) / 1000));
}

function clock(s: number) {
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

export function XiangqiGame({ ctx }: { ctx: XiangqiCtx }) {
  const { game, meId, busy, act, onOpenLeaderboard } = ctx;
  const s = game.state as XiangqiState;

  const [selectedSquare, setSelectedSquare] = useState<string | null>(null);
  const [confirmResign, setConfirmResign] = useState(false);
  const [manualFlip, setManualFlip] = useState<boolean | null>(null);

  const redPlayer = game.players.find((p) => p.id === s.red_player_id);
  const blackPlayer = game.players.find((p) => p.id === s.black_player_id);
  const isRed = s.red_player_id === meId;
  const isBlack = s.black_player_id === meId;
  const isPlayer = isRed || isBlack;
  const isMyTurn = game.status === "playing" && s.turn_player_id === meId;

  // Tự động xoay bàn cờ nếu mình cầm Đen (để phe mình luôn ở phía dưới)
  const flipped = manualFlip !== null ? manualFlip : isBlack;

  // Âm thanh khi có nước đi mới
  const lastMoveRef = useRef<string | null>(null);
  useEffect(() => {
    if (!s.last_move) return;
    const key = `${s.last_move.from}-${s.last_move.to}`;
    if (lastMoveRef.current && lastMoveRef.current !== key) {
      if (s.last_move.captured) {
        playPieceCapture();
      } else {
        playPiecePlace();
      }
    }
    lastMoveRef.current = key;
  }, [s.last_move]);

  // Danh sách các nước đi hợp lệ từ ô đang chọn
  const validTargets = useMemo(() => {
    if (!selectedSquare || !s.legal_moves) return new Set<string>();
    const moves = s.legal_moves[selectedSquare] || [];
    return new Set(moves);
  }, [selectedSquare, s.legal_moves]);

  // Kiểm tra quân cờ có thuộc phe mình không
  const isMyPiece = useCallback(
    (pieceChar: string) => {
      if (!pieceChar || pieceChar === ".") return false;
      if (isRed) return pieceChar >= "A" && pieceChar <= "Z";
      if (isBlack) return pieceChar >= "a" && pieceChar <= "z";
      return false;
    },
    [isRed, isBlack]
  );

  // Xử lý click chọn ô
  const onPointClick = (sq: string, pieceChar: string) => {
    if (!isMyTurn || busy) return;

    if (selectedSquare) {
      if (selectedSquare === sq) {
        setSelectedSquare(null);
        return;
      }
      if (validTargets.has(sq)) {
        act("move", { from: selectedSquare, to: sq });
        setSelectedSquare(null);
        return;
      }
      if (isMyPiece(pieceChar)) {
        setSelectedSquare(sq);
        return;
      }
      setSelectedSquare(null);
    } else {
      if (isMyPiece(pieceChar)) {
        setSelectedSquare(sq);
      }
    }
  };

  // Tính tọa độ pixel (cx, cy) trên SVG từ file (0..8) và rank (0..9)
  const getCoords = useCallback(
    (file: number, rank: number) => {
      const displayFile = flipped ? 8 - file : file;
      const displayRank = flipped ? rank : 9 - rank;
      const cx = PADDING_X + displayFile * CELL_SIZE;
      const cy = PADDING_Y + displayRank * CELL_SIZE;
      return { cx, cy };
    },
    [flipped]
  );

  // Giá trị ước lượng quân cờ để tính lợi thế
  const pieceWeights: Record<string, number> = {
    r: 9, // Xe
    c: 4.5, // Pháo
    h: 4, // Mã
    e: 2, // Tượng
    a: 2, // Sĩ
    p: 1.5, // Tốt
  };

  let redMaterialLoss = 0;
  let blackMaterialLoss = 0;
  if (s.captured) {
    for (const [p, count] of Object.entries(s.captured)) {
      const w = pieceWeights[p.toLowerCase()] || 0;
      if (p >= "A" && p <= "Z") {
        redMaterialLoss += w * count;
      } else {
        blackMaterialLoss += w * count;
      }
    }
  }
  const redAdvantage = Math.round((blackMaterialLoss - redMaterialLoss) * 10) / 10;
  const blackAdvantage = Math.round((redMaterialLoss - blackMaterialLoss) * 10) / 10;

  const topPlayer = flipped ? redPlayer : blackPlayer;
  const topColor = flipped ? "red" : "black";
  const topAdvantage = flipped ? redAdvantage : blackAdvantage;

  const bottomPlayer = flipped ? blackPlayer : redPlayer;
  const bottomColor = flipped ? "black" : "red";
  const bottomAdvantage = flipped ? blackAdvantage : redAdvantage;

  return (
    <div className="xiangqi-container" style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: "0.5rem" }}>
      {/* Banner cảnh báo Chiếu tướng */}
      {game.status === "playing" && s.in_check && (
        <div
          className="xiangqi-banner is-check"
          style={{
            backgroundColor: "#fee2e2",
            color: "#991b1b",
            border: "1px solid #f87171",
            padding: "0.35rem 0.8rem",
            borderRadius: "6px",
            fontWeight: "bold",
            display: "flex",
            alignItems: "center",
            gap: "0.4rem",
            fontSize: "0.9rem",
          }}
        >
          <span>⚠️ {s.turn_color === "r" ? "Phe Đỏ" : "Phe Đen"} đang bị CHIẾU TƯỚNG!</span>
        </div>
      )}

      {/* Banner đề nghị hoà */}
      {game.status === "playing" && s.draw_offered && (
        <div
          className="xiangqi-banner is-draw-offer"
          style={{
            backgroundColor: "#fef3c7",
            color: "#92400e",
            border: "1px solid #fcd34d",
            padding: "0.4rem 0.8rem",
            borderRadius: "6px",
            display: "flex",
            alignItems: "center",
            gap: "0.6rem",
            fontSize: "0.85rem",
          }}
        >
          {s.draw_offered_by === meId ? (
            <span>Bạn đã đề nghị hoà cờ. Đang chờ đối thủ phản hồi...</span>
          ) : (
            <>
              <span>Đối thủ đề nghị hoà cờ:</span>
              <div style={{ display: "flex", gap: "0.3rem" }}>
                <button
                  type="button"
                  className="btn btn-sm btn-primary"
                  disabled={busy}
                  onClick={() => act("draw_accept")}
                >
                  Đồng ý hoà
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-secondary"
                  disabled={busy}
                  onClick={() => act("draw_decline")}
                >
                  Từ chối
                </button>
              </div>
            </>
          )}
        </div>
      )}

      {/* Thanh thông tin người chơi phía trên (Đối thủ) */}
      <div
        className="xiangqi-player-bar"
        style={{
          width: BOARD_WIDTH,
          maxWidth: "100%",
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          padding: "0.25rem 0.5rem",
          borderRadius: "6px",
          backgroundColor: "var(--bg-secondary, #f3f4f6)",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
          <span
            style={{
              display: "inline-block",
              width: "12px",
              height: "12px",
              borderRadius: "50%",
              backgroundColor: topColor === "red" ? "#dc2626" : "#1e293b",
            }}
          />
          <span style={{ fontWeight: 600, fontSize: "0.9rem" }}>
            {topPlayer?.name || (topColor === "red" ? "Phe Đỏ" : "Phe Đen")}
            {topPlayer?.id === meId ? " (Bạn)" : ""}
          </span>
          {topAdvantage > 0 && (
            <span style={{ fontSize: "0.75rem", color: "#16a34a", fontWeight: "bold" }}>
              +{topAdvantage}
            </span>
          )}

          {/* Quân bị ăn */}
          <div style={{ display: "flex", gap: "2px", marginLeft: "0.3rem" }}>
            {Object.entries(s.captured || {}).map(([p, count]) => {
              const isThisSide = topColor === "red" ? p >= "a" && p <= "z" : p >= "A" && p <= "Z";
              if (!isThisSide) return null;
              return Array.from({ length: count }).map((_, i) => (
                <XiangqiPiece key={`${p}-${i}`} piece={p} size={20} />
              ));
            })}
          </div>
        </div>

        {game.status === "playing" && s.turn_color === (topColor === "red" ? "r" : "b") && (
          <Countdown deadline={game.deadline} label="Thời gian nước đi" />
        )}
      </div>

      {/* Bàn cờ Cờ Tướng SVG */}
      <div
        className="xiangqi-board-wrapper"
        style={{
          position: "relative",
          width: BOARD_WIDTH,
          maxWidth: "100%",
          boxShadow: "0 6px 16px rgba(0,0,0,0.22)",
          borderRadius: "8px",
          overflow: "hidden",
        }}
      >
        <svg
          viewBox={`0 0 ${BOARD_WIDTH} ${BOARD_HEIGHT}`}
          style={{ width: "100%", height: "auto", display: "block" }}
        >
          <defs>
            {/* Vân nền gỗ phong thủy */}
            <radialGradient id="woodBg" cx="50%" cy="50%" r="70%">
              <stop offset="0%" stopColor="#f5dfb8" />
              <stop offset="70%" stopColor="#e7c48f" />
              <stop offset="100%" stopColor="#d1a667" />
            </radialGradient>
          </defs>

          {/* Mặt bàn cờ nền gỗ */}
          <rect
            x="0"
            y="0"
            width={BOARD_WIDTH}
            height={BOARD_HEIGHT}
            fill="url(#woodBg)"
            stroke="#5c3610"
            strokeWidth="3"
            rx="6"
          />

          {/* Khung viền chỉ đôi truyền thống */}
          <rect
            x={PADDING_X - 6}
            y={PADDING_Y - 6}
            width={8 * CELL_SIZE + 12}
            height={9 * CELL_SIZE + 12}
            fill="none"
            stroke="#5c3610"
            strokeWidth="2.5"
          />
          <rect
            x={PADDING_X - 2}
            y={PADDING_Y - 2}
            width={8 * CELL_SIZE + 4}
            height={9 * CELL_SIZE + 4}
            fill="none"
            stroke="#7c4a1e"
            strokeWidth="1"
          />

          {/* 10 đường kẻ ngang */}
          {Array.from({ length: 10 }).map((_, r) => {
            const y = PADDING_Y + r * CELL_SIZE;
            return (
              <line
                key={`h-${r}`}
                x1={PADDING_X}
                y1={y}
                x2={PADDING_X + 8 * CELL_SIZE}
                y2={y}
                stroke="#5c3610"
                strokeWidth="1.2"
              />
            );
          })}

          {/* 9 đường kẻ dọc (ngắt quãng tại sông ở giữa ranks 4 và 5) */}
          {Array.from({ length: 9 }).map((_, f) => {
            const x = PADDING_X + f * CELL_SIZE;
            if (f === 0 || f === 8) {
              // Hai biên ngoài nối liền qua sông
              return (
                <line
                  key={`v-${f}`}
                  x1={x}
                  y1={PADDING_Y}
                  x2={x}
                  y2={PADDING_Y + 9 * CELL_SIZE}
                  stroke="#5c3610"
                  strokeWidth="1.2"
                />
              );
            }
            // Các đường bên trong bị ngắt quãng bởi sông
            return (
              <g key={`v-${f}`}>
                <line
                  x1={x}
                  y1={PADDING_Y}
                  x2={x}
                  y2={PADDING_Y + 4 * CELL_SIZE}
                  stroke="#5c3610"
                  strokeWidth="1.2"
                />
                <line
                  x1={x}
                  y1={PADDING_Y + 5 * CELL_SIZE}
                  x2={x}
                  y2={PADDING_Y + 9 * CELL_SIZE}
                  stroke="#5c3610"
                  strokeWidth="1.2"
                />
              </g>
            );
          })}

          {/* Cung Đen (Ranks 0..2 trên hoặc dưới tuỳ góc nhìn) & Cung Đỏ */}
          {/* Cửu cung trên (ranks 0..2 của bàn SVG) */}
          <line
            x1={PADDING_X + 3 * CELL_SIZE}
            y1={PADDING_Y}
            x2={PADDING_X + 5 * CELL_SIZE}
            y2={PADDING_Y + 2 * CELL_SIZE}
            stroke="#5c3610"
            strokeWidth="1.2"
          />
          <line
            x1={PADDING_X + 5 * CELL_SIZE}
            y1={PADDING_Y}
            x2={PADDING_X + 3 * CELL_SIZE}
            y2={PADDING_Y + 2 * CELL_SIZE}
            stroke="#5c3610"
            strokeWidth="1.2"
          />

          {/* Cửu cung dưới (ranks 7..9 của bàn SVG) */}
          <line
            x1={PADDING_X + 3 * CELL_SIZE}
            y1={PADDING_Y + 7 * CELL_SIZE}
            x2={PADDING_X + 5 * CELL_SIZE}
            y2={PADDING_Y + 9 * CELL_SIZE}
            stroke="#5c3610"
            strokeWidth="1.2"
          />
          <line
            x1={PADDING_X + 5 * CELL_SIZE}
            y1={PADDING_Y + 7 * CELL_SIZE}
            x2={PADDING_X + 3 * CELL_SIZE}
            y2={PADDING_Y + 9 * CELL_SIZE}
            stroke="#5c3610"
            strokeWidth="1.2"
          />

          {/* Chữ Sông: 楚河 (Sở Hà) - 漢界 (Hán Giới) */}
          <g style={{ userSelect: "none" }}>
            <text
              x={PADDING_X + 2 * CELL_SIZE}
              y={PADDING_Y + 4.5 * CELL_SIZE + 6}
              textAnchor="middle"
              fill="#7c4a1e"
              fontSize="16"
              fontWeight="bold"
              fontFamily='"Noto Serif TC", "KaiTi", serif'
              opacity="0.8"
            >
              楚 河
            </text>
            <text
              x={PADDING_X + 6 * CELL_SIZE}
              y={PADDING_Y + 4.5 * CELL_SIZE + 6}
              textAnchor="middle"
              fill="#7c4a1e"
              fontSize="16"
              fontWeight="bold"
              fontFamily='"Noto Serif TC", "KaiTi", serif'
              opacity="0.8"
            >
              漢 界
            </text>
          </g>

          {/* Hiển thị nước đi vừa đánh (Last Move) */}
          {s.last_move && (
            <>
              {(() => {
                const f1 = s.last_move.from.charCodeAt(0) - 97;
                const r1 = parseInt(s.last_move.from[1], 10);
                const { cx, cy } = getCoords(f1, r1);
                return (
                  <circle
                    cx={cx}
                    cy={cy}
                    r={CELL_SIZE / 2 - 2}
                    fill="none"
                    stroke="#3b82f6"
                    strokeWidth="2.5"
                    strokeDasharray="4 3"
                    opacity="0.8"
                  />
                );
              })()}
              {(() => {
                const f2 = s.last_move.to.charCodeAt(0) - 97;
                const r2 = parseInt(s.last_move.to[1], 10);
                const { cx, cy } = getCoords(f2, r2);
                return (
                  <circle
                    cx={cx}
                    cy={cy}
                    r={CELL_SIZE / 2 - 2}
                    fill="none"
                    stroke="#3b82f6"
                    strokeWidth="2.5"
                    opacity="0.9"
                  />
                );
              })()}
            </>
          )}

          {/* Render 90 giao điểm và quân cờ */}
          {Array.from({ length: 10 }).map((_, rank) =>
            Array.from({ length: 9 }).map((_, file) => {
              const sq = `${String.fromCharCode(97 + file)}${rank}`;
              const idx = rank * 9 + file;
              const pieceChar = s.board ? s.board[idx] : ".";
              const { cx, cy } = getCoords(file, rank);

              const isSelected = selectedSquare === sq;
              const isTarget = validTargets.has(sq);
              const isInCheckKing = s.in_check && s.check_square === sq;
              const hasEnemy = pieceChar !== "." && !isMyPiece(pieceChar);

              return (
                <g
                  key={sq}
                  style={{ cursor: isMyTurn ? "pointer" : "default" }}
                  onClick={() => onPointClick(sq, pieceChar)}
                  onDragOver={(e) => {
                    if (isTarget) e.preventDefault();
                  }}
                  onDrop={(e) => {
                    e.preventDefault();
                    const from = e.dataTransfer.getData("text/plain");
                    if (from && isTarget) {
                      act("move", { from, to: sq });
                      setSelectedSquare(null);
                    }
                  }}
                >
                  {/* Ô vùng bắt click vô hình */}
                  <circle cx={cx} cy={cy} r={CELL_SIZE / 2} fill="transparent" />

                  {/* Vòng sáng khi Tướng bị chiếu */}
                  {isInCheckKing && (
                    <circle
                      cx={cx}
                      cy={cy}
                      r="23"
                      fill="#ef4444"
                      opacity="0.45"
                      className="xiangqi-check-pulse"
                    />
                  )}

                  {/* Hiệu ứng chọn quân */}
                  {isSelected && (
                    <circle
                      cx={cx}
                      cy={cy}
                      r="24"
                      fill="none"
                      stroke="#eab308"
                      strokeWidth="3.5"
                    />
                  )}

                  {/* Quân cờ nếu có */}
                  {pieceChar !== "." && (
                    <g
                      transform={`translate(${cx - 21}, ${cy - 21})`}
                      {...((isMyTurn && isMyPiece(pieceChar) ? { draggable: true } : {}) as Record<string, unknown>)}
                      onDragStart={(e) => {
                        e.dataTransfer.setData("text/plain", sq);
                        setSelectedSquare(sq);
                      }}
                    >
                      <XiangqiPiece piece={pieceChar} size={42} />
                    </g>
                  )}

                  {/* Chỉ báo nước đi hợp lệ: chấm xanh nếu ô trống, vòng đỏ nếu ăn quân */}
                  {isTarget && (
                    <>
                      {hasEnemy ? (
                        <circle
                          cx={cx}
                          cy={cy}
                          r="22"
                          fill="none"
                          stroke="#ef4444"
                          strokeWidth="3"
                          opacity="0.9"
                        />
                      ) : (
                        <circle cx={cx} cy={cy} r="6.5" fill="#16a34a" opacity="0.85" />
                      )}
                    </>
                  )}
                </g>
              );
            })
          )}
        </svg>
      </div>

      {/* Thanh thông tin người chơi phía dưới (Bạn) */}
      <div
        className="xiangqi-player-bar"
        style={{
          width: BOARD_WIDTH,
          maxWidth: "100%",
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          padding: "0.25rem 0.5rem",
          borderRadius: "6px",
          backgroundColor: "var(--bg-secondary, #f3f4f6)",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
          <span
            style={{
              display: "inline-block",
              width: "12px",
              height: "12px",
              borderRadius: "50%",
              backgroundColor: bottomColor === "red" ? "#dc2626" : "#1e293b",
            }}
          />
          <span style={{ fontWeight: 600, fontSize: "0.9rem" }}>
            {bottomPlayer?.name || (bottomColor === "red" ? "Phe Đỏ" : "Phe Đen")}
            {bottomPlayer?.id === meId ? " (Bạn)" : ""}
          </span>
          {bottomAdvantage > 0 && (
            <span style={{ fontSize: "0.75rem", color: "#16a34a", fontWeight: "bold" }}>
              +{bottomAdvantage}
            </span>
          )}

          {/* Quân bị ăn */}
          <div style={{ display: "flex", gap: "2px", marginLeft: "0.3rem" }}>
            {Object.entries(s.captured || {}).map(([p, count]) => {
              const isThisSide = bottomColor === "red" ? p >= "a" && p <= "z" : p >= "A" && p <= "Z";
              if (!isThisSide) return null;
              return Array.from({ length: count }).map((_, i) => (
                <XiangqiPiece key={`${p}-${i}`} piece={p} size={20} />
              ));
            })}
          </div>
        </div>

        {game.status === "playing" && s.turn_color === (bottomColor === "red" ? "r" : "b") && (
          <Countdown deadline={game.deadline} label="Thời gian nước đi" />
        )}
      </div>

      {/* Các nút chức năng điều khiển */}
      <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap", justifyContent: "center", marginTop: "0.2rem" }}>
        <button
          type="button"
          className="btn btn-sm btn-secondary"
          onClick={() => setManualFlip((prev) => (prev !== null ? !prev : !isBlack))}
          title="Đổi hướng nhìn bàn cờ"
        >
          🔄 Lật bàn cờ
        </button>

        {isPlayer && game.status === "playing" && (
          <>
            <button
              type="button"
              className="btn btn-sm btn-secondary"
              disabled={busy || s.draw_offered}
              onClick={() => act("draw_offer")}
              title="Gửi lời mời hoà tới đối phương"
            >
              🤝 Xin hoà
            </button>
            <button
              type="button"
              className="btn btn-sm btn-danger"
              disabled={busy}
              onClick={() => setConfirmResign(true)}
              title="Chấp nhận thua ván cờ"
            >
              🏳️ Xin thua
            </button>
          </>
        )}

        {onOpenLeaderboard && (
          <button
            type="button"
            className="btn btn-sm btn-secondary"
            onClick={onOpenLeaderboard}
            title="Xem bảng xếp hạng"
          >
            🏆 Bảng xếp hạng
          </button>
        )}
      </div>

      {/* Hộp thoại xác nhận đầu hàng */}
      {confirmResign && (
        <Dialog title="Xác nhận đầu hàng" onClose={() => setConfirmResign(false)}>
          <div style={{ display: "flex", flexDirection: "column", gap: "1rem" }}>
            <p>Bạn có chắc chắn muốn nhận thua ván Cờ tướng này không?</p>
            <div style={{ display: "flex", gap: "0.5rem", justifyContent: "flex-end" }}>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setConfirmResign(false)}
              >
                Huỷ
              </button>
              <button
                type="button"
                className="btn btn-danger"
                disabled={busy}
                onClick={() => {
                  act("resign");
                  setConfirmResign(false);
                }}
              >
                Chấp nhận thua
              </button>
            </div>
          </div>
        </Dialog>
      )}
    </div>
  );
}

function Countdown({ deadline, label }: { deadline?: string; label?: string }) {
  const left = useSecondsLeft(deadline);
  if (left === null) return null;
  return (
    <span
      className={`game-clock${left <= 10 ? " is-urgent" : ""}`}
      role="timer"
      aria-label={label}
      style={{
        fontWeight: "bold",
        fontSize: "0.85rem",
        color: left <= 10 ? "#dc2626" : "inherit",
      }}
    >
      ⏱ {clock(left)}
    </span>
  );
}
