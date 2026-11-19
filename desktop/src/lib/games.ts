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
  hunter?: number;
  opts: { day_seconds: number; night_seconds: number; hide_roles: boolean };
};

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
export type RpsState = {
  best_of: number;
  score: [number, number];
  rounds: { picks: [RpsChoice | "", RpsChoice | ""]; winner: number }[];
  ready: [boolean, boolean];
};
export type RpsMe = { pick?: RpsChoice };

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
    case "rps": {
      const i = g.players.findIndex((p) => p.id === meId);
      const s = g.state as RpsState | undefined;
      return i >= 0 && s && !s.ready[i] ? "Ra tay đi: Búa, Bao hay Kéo?" : null;
    }
  }
  return null;
}
