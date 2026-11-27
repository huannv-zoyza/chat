// Kiểu dữ liệu từng trò chơi (khớp package game bên Go) và tiện ích dùng chung.
import type { GameKind, GameView } from "./api";

export type WerewolfRole = "wolf" | "villager" | "seer" | "guard" | "witch" | "hunter";

export type WerewolfState = {
  phase?: "night" | "day" | "hunter";
  round?: number;
  dead: number[];
  roles: Record<string, WerewolfRole>;
  votes?: Record<string, number>;
  composition: Partial<Record<WerewolfRole, number>>;
  /** Bộ vai tự chọn: cần ít nhất chừng này người. */
  cast_min?: number;
  hunter?: number;
  opts: { day_seconds: number; night_seconds: number; hide_roles: boolean; cast?: WerewolfCast };
  /** Kênh chat của bầy Sói, chỉ lộ cho mọi người khi hết ván. */
  wolf_chat?: WolfLine[];
};

export type WolfLine = { from: number; text: string; at: string };

/** Bộ vai chủ phòng tự chọn (số Sói, có/không các vai đặc biệt); còn lại là Dân làng. */
export type WerewolfCast = Partial<Record<Exclude<WerewolfRole, "villager">, number>>;

export const CAST_SPECIALS = ["seer", "guard", "witch", "hunter"] as const;

/** Số người ít nhất cho một bộ vai: đủ chỗ cho mọi vai và Sói ít hơn phe còn lại (khớp castMin bên Go). */
export function castMin(cast: WerewolfCast): number {
  const used = Object.values(cast).reduce((a, k) => a + (k ?? 0), 0);
  return Math.max(used, 2 * (cast.wolf ?? 1) + 1, 4);
}

export type WerewolfMe = {
  role?: WerewolfRole;
  alive: boolean;
  step?: "wolf" | "seer" | "guard" | "witch" | "vote" | "shoot";
  target?: number;
  wolves?: number[];
  wolf_votes?: Record<string, number>;
  seer_log?: { round: number; target: number; wolf: boolean }[];
  last_guard?: number;
  heal_left?: boolean;
  poison_left?: boolean;
  victim?: number;
  wolf_chat?: WolfLine[];
};

export type SpyState = {
  phase?: "describe" | "vote";
  round?: number;
  order?: number[];
  speaker?: number;
  clues: { round: number; player: number; text: string }[];
  votes?: Record<string, number>;
  out: number[];
  spies?: number[];
  spy_count: number;
  words?: [string, string];
  last_votes?: Record<string, number>;
};
export type SpyMe = { word?: string; out?: boolean };

export type CaroCoord = { r: number; c: number };

export type CaroState = {
  size: number;
  board?: string;
  pieces?: Record<string, "x" | "o">;
  turn?: number;
  last?: number;
  last_coord?: CaroCoord;
  win?: number[];
  win_coords?: CaroCoord[];
  moves: number;
  free_style: boolean;
};

export type RpsChoice = "rock" | "paper" | "scissors";
export const RPS_BET_LEVELS = [0, 50, 100, 200, 500, 1000] as const;
export type RpsState = {
  best_of: number;
  bet?: number;
  pot?: number;
  score: [number, number];
  rounds: { picks: [RpsChoice | "", RpsChoice | ""]; winner: number }[];
  ready: [boolean, boolean];
};
export type RpsMe = { pick?: RpsChoice };

export type ChessMove = {
  from: string;
  to: string;
  piece: string;
  captured?: string;
  promotion?: string;
  is_check?: boolean;
};

export type ChessState = {
  opts?: { turn_minutes: number; first: string };
  fen: string;
  board: string;
  turn_color: "w" | "b";
  turn_player_id: number;
  white_player_id: number;
  black_player_id: number;
  last_move?: ChessMove;
  in_check: boolean;
  check_square?: string;
  is_checkmate?: boolean;
  draw_offered: boolean;
  draw_offered_by?: number;
  legal_moves?: Record<string, string[]>;
  captured: Record<string, number>;
};

export type XiangqiMove = {
  from: string;
  to: string;
  piece: string;
  captured?: string;
  is_check?: boolean;
};

export type XiangqiState = {
  opts?: { turn_minutes: number; first: string; bet_amount?: number };
  bet_amount?: number;
  pot?: number;
  board: string;
  turn_color: "r" | "b";
  turn_player_id: number;
  red_player_id: number;
  black_player_id: number;
  last_move?: XiangqiMove;
  in_check: boolean;
  check_square?: string;
  is_checkmate?: boolean;
  draw_offered: boolean;
  draw_offered_by?: number;
  legal_moves?: Record<string, string[]>;
  captured: Record<string, number>;
};

export const ROLE_INFO: Record<WerewolfRole, { name: string; icon: string; team: string; desc: string }> = {
  wolf: {
    name: "Sói",
    icon: "🐺",
    team: "Phe Sói",
    desc: "Mỗi đêm cùng đồng bọn chọn một người để cắn. Ban ngày giả làm dân lành.",
  },
  villager: {
    name: "Dân làng",
    icon: "🧑‍🌾",
    team: "Phe Dân",
    desc: "Không có chức năng đêm. Ban ngày tìm và treo cổ Sói.",
  },
  seer: {
    name: "Tiên tri",
    icon: "🔮",
    team: "Phe Dân",
    desc: "Mỗi đêm soi một người để biết người đó có phải Sói không.",
  },
  guard: {
    name: "Bảo vệ",
    icon: "🛡️",
    team: "Phe Dân",
    desc: "Mỗi đêm che chở một người khỏi Sói (được che mình). Không che cùng một người hai đêm liền.",
  },
  witch: {
    name: "Phù thủy",
    icon: "🧪",
    team: "Phe Dân",
    desc: "Biết ai bị Sói cắn. Có một bình cứu và một bình độc, mỗi bình dùng một lần cả ván.",
  },
  hunter: {
    name: "Thợ săn",
    icon: "🏹",
    team: "Phe Dân",
    desc: "Khi chết (bị cắn hoặc bị treo) được bắn chết một người.",
  },
};

export const BAUCUA_SYMBOLS = [
  { id: "bau", name: "Bầu", icon: "🍐" },
  { id: "cua", name: "Cua", icon: "🦀" },
  { id: "tom", name: "Tôm", icon: "🦐" },
  { id: "ca", name: "Cá", icon: "🐟" },
  { id: "ga", name: "Gà", icon: "🐓" },
  { id: "nai", name: "Nai", icon: "🦌" },
];

export type BauCuaState = {
  phase: "" | "betting" | "reveal";
  dealer_id: number;
  roll_no: number;
  rolls_per_dealer: number;
  dealer_order: number[];
  chips: Record<number, number>;
  loans: Record<number, number>;
  bets: Record<number, Record<string, number>>;
  dice: string[];
  last_result: Record<number, number>;
  history: string[][];
  net: Record<number, number>;
  busts: Record<number, number>;
  last_bust?: number;
  last_bust_amount?: number;
};

/* ---------- Poker Texas Hold'em (khớp game/poker.go) ---------- */

/** Lá bài: hạng "23456789TJQKA" + chất "shdc", ví dụ "As", "Td". */
export type PokerCard = string;

export const POKER_BUY_INS = [100, 200, 500, 1000, 2000, 5000] as const;

export type PokerPhase = "" | "preflop" | "flop" | "turn" | "river" | "showdown";

export type PokerSeat = {
  id: number;
  stack: number;
  bet: number;
  in_hand: boolean;
  folded: boolean;
  all_in: boolean;
  away: boolean;
  out: boolean;
  last?: string;
  cards: number;
  net: number;
};

export type PokerResult = {
  pots: { amount: number; winners: number[]; hand?: string }[];
  shown?: Record<string, PokerCard[]>;
  hands?: Record<string, string>;
  best?: Record<string, PokerCard[]>;
  won: Record<string, number>;
  showdown: boolean;
  busted?: number[];
};

export type PokerState = {
  opts: { buy_in: number; turn_seconds: number; blind_up: number };
  phase: PokerPhase;
  hand_no: number;
  sb: number;
  bb: number;
  next_blinds: number;
  button_id: number;
  sb_id: number;
  bb_id: number;
  seats: PokerSeat[];
  board: PokerCard[];
  pot: number;
  cur_bet: number;
  turn_id: number;
  result?: PokerResult;
};

export type PokerMe = {
  hole: PokerCard[];
  hand_name?: string;
  to_call: number;
  can_check: boolean;
  can_call: boolean;
  can_raise: boolean;
  min_to: number;
  max_to: number;
  away: boolean;
};

export const GAME_INFO: Record<GameKind, { title: string; icon: string; players: string; blurb: string }> = {
  station: {
    title: "Trạm Vũ Trụ",
    icon: "🛰️",
    players: "4–12 người · thời gian thực",
    blurb: "Đi khắp trạm làm nhiệm vụ. Kẻ phản bội trà trộn giết người, phá hoại. Thấy xác thì báo, họp và bỏ phiếu.",
  },
  werewolf: {
    title: "Ma Sói",
    icon: "🐺",
    players: "4–16 người",
    blurb: "Đêm Sói đi săn, ngày cả làng bàn bạc treo cổ. Máy làm quản trò, chia vai bí mật.",
  },
  spy: {
    title: "Ai là gián điệp",
    icon: "🕵️",
    players: "4–12 người",
    blurb: "Ai cũng nhận một từ, riêng gián điệp nhận từ hơi khác. Mô tả khéo, tìm ra kẻ lạc loài.",
  },
  caro: {
    title: "Cờ caro",
    icon: "⭕",
    players: "2 người",
    blurb: "Bàn 15×15, ai có 5 quân liền hàng trước thì thắng.",
  },
  rps: {
    title: "Oẳn tù tì",
    icon: "✊",
    players: "2 người",
    blurb: "Búa – Bao – Kéo, ra cùng lúc, chạm số ván thắng trước.",
  },
  chess: {
    title: "Cờ vua",
    icon: "♟️",
    players: "2 người",
    blurb: "Cờ vua quốc tế 8×8, đầy đủ luật FIDE: nhập thành, bắt tốt qua đường, phong cấp.",
  },
  xiangqi: {
    title: "Cờ tướng",
    icon: "🪖",
    players: "2 người",
    blurb: "Cờ tướng 9×10, luật chuẩn: cản mắt tượng, cản chân mã, pháo nhảy ngòi, lộ diện tướng.",
  },  baucua: {
    title: "Bầu cua tôm cá",
    icon: "🎲",
    players: "2–10 người",
    blurb: "Đặt cược 6 con vật, cái xoay vòng, cháy túi thì vay ngân hàng. Cược bằng xu trong ví.",
  },
  poker: {
    title: "Poker Texas Hold'em",
    icon: "🃏",
    players: "2–9 người · cược xu",
    blurb: "Hold'em không giới hạn, luật quốc tế. Mua vào bằng xu trong ví, ai giữ chip tới cuối ăn cả bàn. Có hướng dẫn cho người mới.",
  },
};

/** Việc mình cần làm ngay trong ván (null nếu không có): dùng cho thông báo và nhãn "Tới lượt bạn". */
export function myTurn(g: GameView | undefined, meId: number): string | null {
  if (!g || g.status !== "playing") return null;
  switch (g.kind) {
    case "werewolf": {
      const me = g.me as WerewolfMe | undefined;
      const s = g.state as WerewolfState | undefined;
      if (!me?.step) return null;
      if (me.step === "vote") return s?.votes?.[meId] !== undefined ? null : "Trời sáng, hãy bỏ phiếu";
      if (me.step === "wolf") return me.target ? null : "Đêm rồi, Sói chọn người để cắn";
      return {
        seer: "Tiên tri hãy soi một người",
        guard: "Bảo vệ hãy chọn người che chở",
        witch: "Phù thủy, tới lượt bạn",
        shoot: "Thợ săn, hãy chọn người để bắn",
      }[me.step];
    }
    case "spy": {
      const s = g.state as SpyState | undefined;
      const me = g.me as SpyMe | undefined;
      if (!s || !me || me.out) return null;
      if (s.phase === "describe" && s.speaker === meId) return "Tới lượt bạn mô tả từ khoá";
      if (s.phase === "vote" && s.votes?.[meId] === undefined) return "Bỏ phiếu tìm gián điệp";
      return null;
    }
    case "caro":
      return (g.state as CaroState | undefined)?.turn === meId ? "Tới lượt bạn đánh" : null;
    case "chess": {
      const s = g.state as ChessState | undefined;
      return s?.turn_player_id === meId ? "Tới lượt bạn đi" : null;
    }
    case "xiangqi": {
      const s = g.state as XiangqiState | undefined;
      return s?.turn_player_id === meId ? "Tới lượt bạn đi" : null;
    }

    case "poker": {
      const s = g.state as PokerState | undefined;
      const me = g.me as PokerMe | undefined;
      if (me?.away) return "Bạn đang vắng mặt, bấm Quay lại";
      return s?.turn_id === meId ? "Tới lượt bạn" : null;
    }
    case "rps": {
      const i = g.players.findIndex((p) => p.id === meId);
      const s = g.state as RpsState | undefined;
      return i >= 0 && s && !s.ready[i] ? "Ra tay đi: Búa, Bao hay Kéo?" : null;
    }
  }
  return null;
}
