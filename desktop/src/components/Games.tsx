import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { ApiError, type GameKind, type GameView, type Message, type PublicUser } from "../lib/api";
import {
  GAME_INFO,
  ROLE_INFO,
  myTurn,
  type CaroState,
  type RpsChoice,
  type RpsMe,
  type RpsState,
  type SpyMe,
  type SpyState,
  type WerewolfMe,
  type WerewolfRole,
  type WerewolfState,
} from "../lib/games";
import { useChat } from "../state/ChatProvider";
import type { StationState } from "../lib/station";
import { StationSummary } from "./Station";
import { ShareDialog } from "./Share";
import { Avatar, Dialog, FormError, Icon } from "./ui";

/* ---------- Tiện ích ---------- */

/** Số giây còn lại tới deadline, tự cập nhật. */
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

function Countdown({ deadline, label }: { deadline?: string; label?: string }) {
  const left = useSecondsLeft(deadline);
  if (left === null) return null;
  return (
    <span className={`game-clock${left <= 10 ? " is-urgent" : ""}`} role="timer" aria-label={label}>
      ⏱ {clock(left)}
    </span>
  );
}

type Ctx = {
  game: GameView;
  meId: number;
  isPlayer: boolean;
  busy: boolean;
  /** Gửi hành động; lỗi hiện ở cuối thẻ. */
  act: (action: string, data?: unknown) => void;
  user: (id: number) => PublicUser;
  nameOf: (id: number) => string;
  /** Mở hộp thoại chia sẻ phòng chơi sang kênh / tin nhắn riêng khác. */
  invite: () => void;
};

/** Ô người chơi: avatar, tên, nhãn phụ; bấm được nếu có onClick. */
function PlayerTile({
  ctx,
  id,
  badge,
  sub,
  dead,
  selected,
  winner,
  onClick,
  children,
}: {
  ctx: Ctx;
  id: number;
  badge?: ReactNode;
  sub?: ReactNode;
  dead?: boolean;
  selected?: boolean;
  winner?: boolean;
  onClick?: () => void;
  children?: ReactNode;
}) {
  const cls = [
    "game-player",
    dead && "is-dead",
    selected && "is-selected",
    winner && "is-winner",
    onClick && "is-clickable",
    id === ctx.meId && "is-me",
  ]
    .filter(Boolean)
    .join(" ");
  const body = (
    <>
      <Avatar user={ctx.user(id)} size={30} />
      <span className="game-player-text">
        <span className="game-player-name">
          {ctx.nameOf(id)}
          {id === ctx.meId && <span className="game-me-tag">bạn</span>}
        </span>
        {sub && <span className="game-player-sub">{sub}</span>}
      </span>
      {badge && <span className="game-player-badge">{badge}</span>}
      {children}
    </>
  );
  return onClick ? (
    <button type="button" className={cls} onClick={onClick} disabled={ctx.busy} aria-pressed={selected}>
      {body}
    </button>
  ) : (
    <div className={cls}>{body}</div>
  );
}

/** Thẻ bí mật (vai / từ khoá): mặc định úp để người ngồi cạnh không nhìn trộm. */
function SecretCard({ label, children, tone }: { label: string; children: ReactNode; tone?: "wolf" | "plain" }) {
  const [shown, setShown] = useState(false);
  return (
    <div className={`game-secret${tone === "wolf" ? " is-wolf" : ""}${shown ? " is-shown" : ""}`}>
      <div className="game-secret-head">
        <span>{label}</span>
        <button type="button" className="link-btn" onClick={() => setShown((v) => !v)} aria-expanded={shown}>
          {shown ? "Úp thẻ" : "Lật xem"}
        </button>
      </div>
      {shown ? (
        <div className="game-secret-body">{children}</div>
      ) : (
        <button type="button" className="game-secret-cover" onClick={() => setShown(true)}>
          Bấm để lật thẻ (che màn hình khỏi người bên cạnh)
        </button>
      )}
    </div>
  );
}

function votesFor(votes: Record<string, number> | undefined, target: number): number[] {
  if (!votes) return [];
  return Object.entries(votes)
    .filter(([, t]) => t === target)
    .map(([v]) => Number(v));
}

function VoterDots({ ctx, ids }: { ctx: Ctx; ids: number[] }) {
  if (ids.length === 0) return null;
  return (
    <span className="game-voters" title={ids.map(ctx.nameOf).join(", ")}>
      {ids.slice(0, 4).map((id) => (
        <Avatar key={id} user={ctx.user(id)} size={16} />
      ))}
      <b>{ids.length}</b>
    </span>
  );
}

/* ---------- Thẻ ván chơi trong tin nhắn ---------- */

export function GameCard({ message }: { message: Message }) {
  const { state, gameAction } = useChat();
  const game = message.game!;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const meId = state.me.id;
  const isPlayer = game.players.some((p) => p.id === meId);

  const act = (action: string, data?: unknown) => {
    if (busy) return;
    setBusy(true);
    setError(null);
    gameAction(message.id, action, data)
      .catch((err) => setError(err instanceof ApiError ? err.message : "Không gửi được, thử lại."))
      .finally(() => setBusy(false));
  };
  const nameOf = (id: number) => game.players.find((p) => p.id === id)?.name ?? "?";
  const user = (id: number): PublicUser =>
    state.users[id] ?? { id, username: nameOf(id), display_name: nameOf(id), avatar: null, status_text: "" };
  const [sharing, setSharing] = useState(false);
  const ctx: Ctx = { game, meId, isPlayer, busy, act, user, nameOf, invite: () => setSharing(true) };
  const info = GAME_INFO[game.kind];
  const turn = myTurn(game, meId);
  const night = game.kind === "werewolf" && (game.state as WerewolfState | undefined)?.phase === "night";

  const meta =
    game.status === "lobby"
      ? `Đang chờ người chơi · ${game.players.length}/${game.max_players}`
      : game.status === "playing"
        ? "Đang chơi"
        : "Đã kết thúc";

  return (
    <section
      className={`game game-${game.kind} is-${game.status}${night ? " is-night" : ""}`}
      aria-label={`${game.title}: ${meta}`}
    >
      <header className="game-head">
        <span className="game-icon" aria-hidden>
          {info.icon}
        </span>
        <div className="game-titles">
          <p className="game-title">{game.title}</p>
          <p className="game-meta">
            {meta}
            {game.status !== "ended" && <> · chủ phòng {game.host_id === meId ? "bạn" : nameOf(game.host_id)}</>}
          </p>
        </div>
        {turn && <span className="game-turn">{turn}</span>}
      </header>

      {game.status === "lobby" ? (
        <Lobby ctx={ctx} />
      ) : game.kind === "station" ? (
        <StationSummary messageId={message.id} game={game} meId={meId} />
      ) : game.kind === "werewolf" ? (
        <Werewolf ctx={ctx} />
      ) : game.kind === "spy" ? (
        <Spy ctx={ctx} />
      ) : game.kind === "caro" ? (
        <Caro ctx={ctx} />
      ) : (
        <Rps ctx={ctx} />
      )}

      {game.status === "ended" && game.result && (
        <p className={`game-result${game.winners?.includes(meId) ? " is-win" : ""}`}>
          {game.winners?.includes(meId) ? "🏆 " : ""}
          {game.result}
        </p>
      )}
      <GameLog game={game} />
      <FormError>{error}</FormError>
      {sharing && <ShareDialog message={message} onClose={() => setSharing(false)} />}
    </section>
  );
}

function GameLog({ game }: { game: GameView }) {
  const [all, setAll] = useState(false);
  const many = game.kind === "werewolf" || game.kind === "spy";
  const lines = all ? game.log : game.log.slice(many ? -4 : -2);
  if (game.log.length === 0) return null;
  return (
    <div className="game-log">
      <ol aria-label="Diễn biến">
        {lines.map((l, i) => (
          <li key={game.log.length - lines.length + i}>{l}</li>
        ))}
      </ol>
      {game.log.length > lines.length || all ? (
        <button type="button" className="link-btn" onClick={() => setAll((v) => !v)}>
          {all ? "Thu gọn" : `Xem toàn bộ diễn biến (${game.log.length})`}
        </button>
      ) : null}
    </div>
  );
}

/* ---------- Phòng chờ ---------- */

const RULES: Record<GameKind, string[]> = {
  station: [
    "Mỗi người là một phi hành gia trên trạm. Phần lớn là thuyền viên, vài người là kẻ phản bội (đồng bọn biết nhau).",
    "Thuyền viên đi tới các điểm phát sáng để làm nhiệm vụ (nối dây, nhập mã, căn chỉnh…). Xong hết nhiệm vụ của cả trạm là thắng.",
    "Kẻ phản bội giết người khi đứng gần (có thời gian hồi), phá hoại: cắt điện làm mọi người gần như không thấy gì, hoặc xả oxy (45 giây không sửa là thua).",
    "Thấy xác thì báo, hoặc bấm chuông ở sảnh để họp. Thảo luận rồi bỏ phiếu đẩy một người ra ngoài không gian.",
    "Người chết thành hồn ma: đi xuyên tường, vẫn làm nhiệm vụ được, chỉ nói chuyện với hồn ma khác.",
    "Điều khiển: W A S D hoặc phím mũi tên (điện thoại: cần gạt), E dùng, R báo xác, Q giết, M bản đồ.",
  ],
  werewolf: [
    "Mỗi người nhận một vai bí mật. Sói biết nhau, những người khác không biết ai là ai.",
    "Đêm: Sói chọn chung một người để cắn; Tiên tri soi một người; Bảo vệ che một người; Phù thủy biết ai bị cắn và có thể cứu hoặc đầu độc.",
    "Ngày: thảo luận ngay trong kênh chat rồi bỏ phiếu. Người nhiều phiếu nhất bị treo cổ (hoà phiếu thì không ai chết).",
    "Dân thắng khi hết Sói. Sói thắng khi số Sói bằng số người còn lại.",
  ],
  spy: [
    "Mọi người nhận cùng một từ, riêng gián điệp nhận từ gần giống. Không ai biết mình có phải gián điệp không.",
    "Lần lượt từng người mô tả từ của mình bằng một câu ngắn, không được nói thẳng từ đó.",
    "Hết một vòng thì bỏ phiếu loại một người. Dân thắng khi loại hết gián điệp; gián điệp thắng khi còn ngang số dân.",
  ],
  caro: [
    "Chủ phòng cầm X, đi trước. Có 5 quân liền hàng ngang, dọc hoặc chéo là thắng.",
    "Luật chặn hai đầu: hàng 5 bị đối phương chặn cả hai đầu thì không tính.",
    "Mỗi nước có 3 phút suy nghĩ.",
  ],
  rps: ["Búa thắng Kéo, Kéo thắng Bao, Bao thắng Búa.", "Hai người ra cùng lúc, 30 giây mỗi ván."],
};

function Lobby({ ctx }: { ctx: Ctx }) {
  const { game, meId, isPlayer, busy, act } = ctx;
  const isHost = game.host_id === meId;
  const full = game.players.length >= game.max_players;
  const enough = game.players.length >= game.min_players;
  const auto = game.min_players === game.max_players; // trò 2 người: đủ người là vào luôn

  let setup: ReactNode = null;
  if (game.kind === "station") {
    const s = game.state as StationState;
    setup = (
      <p className="game-setup">
        {s.traitor_count} kẻ phản bội · {s.opts.tasks} nhiệm vụ/người · hồi chiêu giết {s.opts.kill_cooldown}s · họp{" "}
        {s.opts.discuss}s + bỏ phiếu {s.opts.vote}s
      </p>
    );
  } else if (game.kind === "werewolf") {
    const s = game.state as WerewolfState;
    setup = (
      <p className="game-setup">
        Vai ({Math.max(game.players.length, 4)} người):{" "}
        {(Object.keys(ROLE_INFO) as WerewolfRole[])
          .filter((r) => s.composition[r])
          .map((r) => `${ROLE_INFO[r].icon} ${s.composition[r]} ${ROLE_INFO[r].name}`)
          .join(" · ")}
        <br />
        Ngày {s.opts.day_seconds}s · đêm {s.opts.night_seconds}s{s.opts.hide_roles ? " · giấu vai người chết" : ""}
      </p>
    );
  } else if (game.kind === "spy") {
    setup = <p className="game-setup">{(game.state as SpyState).spy_count} gián điệp trong phòng</p>;
  } else if (game.kind === "caro") {
    setup = (
      <p className="game-setup">
        Bàn {(game.state as CaroState).size}×{(game.state as CaroState).size}, tự mở rộng khi đánh ra mép ·{" "}
        {(game.state as CaroState).free_style ? "không chặn hai đầu" : "luật chặn hai đầu"}
      </p>
    );
  } else {
    setup = <p className="game-setup">Chạm {Math.floor((game.state as RpsState).best_of / 2) + 1} ván thắng</p>;
  }

  return (
    <div className="game-lobby">
      {setup}
      <ul className="game-roster" aria-label="Người chơi">
        {game.players.map((p) => (
          <li key={p.id}>
            <PlayerTile ctx={ctx} id={p.id} sub={p.id === game.host_id ? "chủ phòng" : undefined} />
          </li>
        ))}
        {Array.from({ length: Math.max(0, game.min_players - game.players.length) }, (_, i) => (
          <li key={`e${i}`} className="game-seat" aria-hidden>
            Chỗ trống
          </li>
        ))}
      </ul>
      <details className="game-rules">
        <summary>Luật chơi</summary>
        <ul>
          {RULES[game.kind].map((r) => (
            <li key={r}>{r}</li>
          ))}
        </ul>
      </details>
      <div className="game-actions">
        {!isPlayer && !full && (
          <button type="button" className="btn btn-primary" disabled={busy} onClick={() => act("join")}>
            {auto ? "Nhận lời thách đấu" : "Vào chơi"}
          </button>
        )}
        {isPlayer && !isHost && (
          <button type="button" className="btn" disabled={busy} onClick={() => act("leave")}>
            Rời phòng
          </button>
        )}
        {isHost && !auto && (
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy || !enough}
            onClick={() => act("start")}
            title={enough ? undefined : `Cần ít nhất ${game.min_players} người`}
          >
            {enough ? "Bắt đầu" : `Cần thêm ${game.min_players - game.players.length} người`}
          </button>
        )}
        {!full && (
          <button type="button" className="btn" onClick={ctx.invite}>
            <Icon name="userPlus" size={16} />
            Mời bạn bè
          </button>
        )}
        {isHost && (
          <button type="button" className="btn" disabled={busy} onClick={() => act("cancel")}>
            Huỷ phòng
          </button>
        )}
        {isHost && auto && <span className="game-hint">Chờ một người nhận lời…</span>}
      </div>
    </div>
  );
}

/* ---------- Ma Sói ---------- */

function Werewolf({ ctx }: { ctx: Ctx }) {
  const { game, meId, busy, act } = ctx;
  const s = game.state as WerewolfState;
  const me = game.me as WerewolfMe | undefined;
  const [pending, setPending] = useState<number | null>(null);
  const [heal, setHeal] = useState(false);
  const [poison, setPoison] = useState(0);
  const step = me?.step;
  const phaseKey = `${s.phase}-${s.round}-${step}`;
  useEffect(() => {
    setPending(null);
    setHeal(false);
    setPoison(0);
  }, [phaseKey]);

  const dead = new Set(s.dead);
  const playing = game.status === "playing";
  const wolfMates = new Set(me?.role === "wolf" ? me.wolves : []);
  const roleOf = (id: number): WerewolfRole | undefined =>
    s.roles[id] ?? (wolfMates.has(id) ? "wolf" : id === meId ? me?.role : undefined);

  // ai bấm được và bấm thì làm gì
  const canPick = (id: number): boolean => {
    if (!playing || dead.has(id) || busy) return false;
    switch (step) {
      case "wolf":
        return !wolfMates.has(id);
      case "seer":
        return id !== meId;
      case "guard":
        return id !== me?.last_guard;
      case "vote":
        return id !== meId;
      case "shoot":
        return id !== meId;
    }
    return false;
  };
  const onPick = (id: number) => {
    if (step === "wolf") act("night", { target: id });
    else if (step === "vote") act("vote", { target: s.votes?.[meId] === id ? 0 : id });
    else setPending(id === pending ? null : id);
  };

  let banner: ReactNode;
  if (!playing) banner = <span>Ván đã kết thúc sau {s.round} đêm</span>;
  else if (s.phase === "night") banner = <span>🌙 Đêm {s.round} · cả làng đi ngủ</span>;
  else if (s.phase === "day") banner = <span>☀️ Ngày {s.round} · thảo luận trong kênh rồi bỏ phiếu treo cổ</span>;
  else banner = <span>🏹 Thợ săn {ctx.nameOf(s.hunter ?? 0)} đang ngắm bắn</span>;

  let hint: ReactNode = null;
  if (playing && me) {
    if (!me.alive && step !== "shoot") hint = "Bạn đã chết. Hãy xem tiếp nhưng đừng tiết lộ gì nhé.";
    else if (step === "wolf")
      hint = me.target
        ? `Bạn chọn cắn ${ctx.nameOf(me.target)}. Đổi được tới khi cả bầy thống nhất.`
        : "Chọn người để cắn. Cả bầy phải chọn cùng một người.";
    else if (step === "seer") hint = "Chọn một người để soi.";
    else if (step === "guard") hint = "Chọn một người để bảo vệ đêm nay.";
    else if (step === "vote")
      hint =
        s.votes?.[meId] === undefined
          ? "Bấm vào người bạn muốn treo cổ, hoặc bỏ qua."
          : s.votes[meId]
            ? `Bạn bỏ phiếu treo ${ctx.nameOf(s.votes[meId])}. Bấm lại để bỏ qua.`
            : "Bạn đã bỏ qua lượt bỏ phiếu.";
    else if (step === "shoot") hint = "Bạn là Thợ săn và vừa chết. Chọn một người để bắn theo.";
    else if (s.phase === "night")
      hint =
        me.role === "seer" || me.role === "guard"
          ? "Bạn đã hành động. Chờ trời sáng…"
          : me.role === "witch"
            ? "Chờ Sói hành động, rồi tới lượt bạn…"
            : "Bạn đang ngủ. Chờ trời sáng…";
  }

  return (
    <div className="ww">
      <div className="game-banner">
        {banner}
        {playing && <Countdown deadline={game.deadline} label="Thời gian còn lại" />}
      </div>

      {me?.role && (
        <SecretCard label="Vai của bạn" tone={me.role === "wolf" ? "wolf" : "plain"}>
          <p className="ww-role">
            <span aria-hidden>{ROLE_INFO[me.role].icon}</span> {ROLE_INFO[me.role].name}
            <small>{ROLE_INFO[me.role].team}</small>
          </p>
          <p className="ww-role-desc">{ROLE_INFO[me.role].desc}</p>
          {me.role === "wolf" && me.wolves && me.wolves.length > 1 && (
            <p className="ww-role-desc">
              Đồng bọn:{" "}
              {me.wolves
                .filter((id) => id !== meId)
                .map(ctx.nameOf)
                .join(", ")}
            </p>
          )}
          {me.role === "witch" && (
            <p className="ww-role-desc">
              Bình cứu: {me.heal_left ? "còn" : "đã dùng"} · Bình độc: {me.poison_left ? "còn" : "đã dùng"}
            </p>
          )}
          {me.seer_log && me.seer_log.length > 0 && (
            <ul className="ww-seer">
              {me.seer_log.map((c) => (
                <li key={c.round}>
                  Đêm {c.round}: {ctx.nameOf(c.target)} — {c.wolf ? <b>🐺 là Sói</b> : "không phải Sói"}
                </li>
              ))}
            </ul>
          )}
        </SecretCard>
      )}

      {hint && <p className="game-hint">{hint}</p>}

      {step === "witch" && me && (
        <WitchPanel ctx={ctx} me={me} s={s} heal={heal} setHeal={setHeal} poison={poison} setPoison={setPoison} />
      )}

      <ul className="game-grid" aria-label="Dân làng">
        {game.players.map((p) => {
          const role = roleOf(p.id);
          const isDead = dead.has(p.id);
          const voters = s.phase === "day" ? votesFor(s.votes, p.id) : [];
          const wolfVoters = step === "wolf" ? votesFor(me?.wolf_votes, p.id) : [];
          const shownRole =
            role && (s.roles[p.id] || p.id === meId || wolfMates.has(p.id)) ? (
              <span className={`ww-badge ww-${role}`} title={ROLE_INFO[role].name} aria-label={ROLE_INFO[role].name}>
                {ROLE_INFO[role].icon}
              </span>
            ) : null;
          // vai đã lộ (người chết / hết ván) ghi dưới tên, để ô hẹp trên điện thoại không cắt tên
          const revealed = s.roles[p.id] ? ROLE_INFO[s.roles[p.id]].name : "";
          const sub = isDead
            ? `đã chết${revealed ? ` · ${revealed}` : ""}`
            : revealed || (wolfMates.has(p.id) && p.id !== meId ? "đồng bọn" : undefined);
          return (
            <li key={p.id}>
              <PlayerTile
                ctx={ctx}
                id={p.id}
                dead={isDead}
                selected={
                  pending === p.id ||
                  (step === "wolf" && me?.target === p.id) ||
                  (step === "vote" && s.votes?.[meId] === p.id)
                }
                winner={game.status === "ended" && game.winners?.includes(p.id)}
                sub={sub}
                badge={shownRole}
                onClick={canPick(p.id) ? () => onPick(p.id) : undefined}
              >
                <VoterDots ctx={ctx} ids={voters.length ? voters : wolfVoters} />
              </PlayerTile>
            </li>
          );
        })}
      </ul>

      {playing && (
        <div className="game-actions">
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
            <span className="game-hint">
              {Object.keys(s.votes ?? {}).length}/{game.players.length - s.dead.length} người đã bỏ phiếu
            </span>
          )}
          {ctx.isPlayer && game.host_id === meId && (
            <button type="button" className="link-btn game-cancel" disabled={busy} onClick={() => act("cancel")}>
              Huỷ ván
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function WitchPanel({
  ctx,
  me,
  s,
  heal,
  setHeal,
  poison,
  setPoison,
}: {
  ctx: Ctx;
  me: WerewolfMe;
  s: WerewolfState;
  heal: boolean;
  setHeal: (v: boolean) => void;
  poison: number;
  setPoison: (v: number) => void;
}) {
  const alive = ctx.game.players.filter((p) => !s.dead.includes(p.id) && p.id !== ctx.meId);
  return (
    <div className="ww-witch">
      <p>
        {me.victim ? (
          <>
            Đêm nay Sói cắn <b>{ctx.nameOf(me.victim)}</b>
            {me.victim === ctx.meId ? " (chính bạn!)" : ""}.
          </>
        ) : (
          "Đêm nay Sói không cắn ai."
        )}
      </p>
      {me.victim && me.heal_left ? (
        <label className="check-field">
          <input type="checkbox" checked={heal} onChange={(e) => setHeal(e.target.checked)} />
          <span>Dùng bình cứu cho {ctx.nameOf(me.victim)}</span>
        </label>
      ) : null}
      {me.poison_left && (
        <label className="field">
          <span>Bình độc</span>
          <select value={poison} onChange={(e) => setPoison(Number(e.target.value))}>
            <option value={0}>Không dùng</option>
            {alive.map((p) => (
              <option key={p.id} value={p.id}>
                Đầu độc {p.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <button
        type="button"
        className="btn btn-primary"
        disabled={ctx.busy}
        onClick={() => ctx.act("witch", { heal, poison })}
      >
        {heal || poison ? "Xác nhận" : "Không làm gì, đi ngủ"}
      </button>
    </div>
  );
}

/* ---------- Ai là gián điệp ---------- */

function Spy({ ctx }: { ctx: Ctx }) {
  const { game, meId, busy, act } = ctx;
  const s = game.state as SpyState;
  const me = game.me as SpyMe | undefined;
  const [clue, setClue] = useState("");
  const playing = game.status === "playing";
  const out = new Set(s.out);
  const spies = new Set(s.spies ?? []);
  const myTurnNow = playing && s.phase === "describe" && s.speaker === meId;
  const myVote = s.votes?.[meId];
  const canVote = playing && s.phase === "vote" && !!me && !me.out;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (clue.trim()) {
      act("clue", { text: clue.trim() });
      setClue("");
    }
  };

  const rounds = new Map<number, SpyState["clues"]>();
  for (const c of s.clues) rounds.set(c.round, [...(rounds.get(c.round) ?? []), c]);

  return (
    <div className="spy">
      <div className="game-banner">
        {!playing ? (
          <span>Ván đã kết thúc sau {s.round} vòng</span>
        ) : s.phase === "describe" ? (
          <span>
            🗣️ Vòng {s.round} · lượt {s.speaker === meId ? "bạn" : ctx.nameOf(s.speaker ?? 0)} mô tả
          </span>
        ) : (
          <span>🗳️ Vòng {s.round} · bỏ phiếu tìm gián điệp</span>
        )}
        {playing && <Countdown deadline={game.deadline} />}
      </div>

      {me?.word && (
        <SecretCard label="Từ khoá của bạn">
          <p className="spy-word">{me.word}</p>
          <p className="ww-role-desc">
            Có {s.spy_count} gián điệp nhận từ khác. Bạn cũng có thể là gián điệp mà không biết: nghe kỹ mọi người mô
            tả!
          </p>
        </SecretCard>
      )}

      {s.words && (
        <p className="spy-reveal">
          Từ của dân: <b>{s.words[0]}</b> · Từ của gián điệp: <b>{s.words[1]}</b>
        </p>
      )}

      {s.clues.length > 0 && (
        <div className="spy-clues">
          {[...rounds.entries()].map(([r, list]) => (
            <section key={r}>
              <h4>Vòng {r}</h4>
              <ul>
                {list.map((c, i) => (
                  <li key={i}>
                    <b>{ctx.nameOf(c.player)}:</b> {c.text ? `“${c.text}”` : <em>(hết giờ, bỏ lượt)</em>}
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}

      {myTurnNow && (
        <form className="spy-form" onSubmit={submit}>
          <input
            value={clue}
            onChange={(e) => setClue(e.target.value)}
            maxLength={80}
            placeholder="Mô tả từ của bạn (đừng nói thẳng ra)…"
            aria-label="Mô tả từ khoá"
            autoFocus
            disabled={busy}
          />
          <button className="btn btn-primary" disabled={busy || !clue.trim()}>
            Gửi
          </button>
        </form>
      )}

      <ul className="game-grid" aria-label="Người chơi">
        {(s.order && playing
          ? [...s.order, ...game.players.map((p) => p.id).filter((id) => !s.order!.includes(id))]
          : game.players.map((p) => p.id)
        ).map((id) => {
          const voters = s.phase === "vote" ? votesFor(s.votes, id) : [];
          const isOut = out.has(id);
          return (
            <li key={id}>
              <PlayerTile
                ctx={ctx}
                id={id}
                dead={isOut}
                selected={myVote === id || (s.phase === "describe" && s.speaker === id)}
                winner={game.status === "ended" && game.winners?.includes(id)}
                sub={
                  isOut
                    ? `đã bị loại · ${spies.has(id) ? "gián điệp" : "dân"}`
                    : spies.has(id)
                      ? "gián điệp"
                      : s.phase === "describe" && s.speaker === id
                        ? "đang mô tả…"
                        : undefined
                }
                badge={
                  spies.has(id) ? (
                    <span className="ww-badge ww-wolf" title="Gián điệp" aria-label="Gián điệp">
                      🕵️
                    </span>
                  ) : null
                }
                onClick={canVote && !isOut && id !== meId && !busy ? () => act("vote", { target: id }) : undefined}
              >
                <VoterDots ctx={ctx} ids={voters} />
              </PlayerTile>
            </li>
          );
        })}
      </ul>

      {playing && (
        <div className="game-actions">
          {canVote && myVote === undefined && (
            <button type="button" className="btn" disabled={busy} onClick={() => act("vote", { target: 0 })}>
              Bỏ qua
            </button>
          )}
          {canVote && (
            <span className="game-hint">
              {myVote === undefined ? "Bấm vào người bạn nghi là gián điệp." : "Đã bỏ phiếu, bấm người khác để đổi."}
            </span>
          )}
          {game.host_id === meId && (
            <button type="button" className="link-btn game-cancel" disabled={busy} onClick={() => act("cancel")}>
              Huỷ ván
            </button>
          )}
        </div>
      )}
    </div>
  );
}

/* ---------- Cờ caro ---------- */

// Ô giữ một cỡ suốt ván: cỡ để bàn 15×15 vừa khung (rộng tối đa 34rem, cao tối đa 70% màn hình). Bàn nở to
// hơn thì khung vẫn cỡ 15×15 và thành cửa sổ nhìn: kéo để xem chỗ khác, nước mới tự trượt vào giữa.
const CARO_VIEW = 15;
const CARO_CELL_MIN = 18;
const CARO_CELL_MAX = 34;
const CARO_BOX_MAX = 544; // 34rem

function Caro({ ctx }: { ctx: Ctx }) {
  const { game, meId, busy, act } = ctx;
  const s = game.state as CaroState;
  const [x, o] = game.players;
  const board = s.board ?? ".".repeat(s.size * s.size);
  const win = new Set(s.win ?? []);
  const myMove = game.status === "playing" && s.turn === meId;
  const wrapRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [room, setRoom] = useState({ w: 0, h: 0 });
  const [overview, setOverview] = useState(false);
  const [edges, setEdges] = useState({ l: false, r: false, t: false, b: false });
  const drag = useRef<{ x: number; y: number; sl: number; st: number; moved: boolean } | null>(null);
  const dragged = useRef(false);
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const measure = () => setRoom({ w: el.clientWidth, h: window.innerHeight });
    measure();
    window.addEventListener("resize", measure);
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    ro?.observe(el);
    return () => {
      window.removeEventListener("resize", measure);
      ro?.disconnect();
    };
  }, []);
  const room15 = Math.min(Math.min(room.w, CARO_BOX_MAX), Math.min(room.h * 0.7, CARO_BOX_MAX)) - 2; // trừ viền
  const base = room.w
    ? Math.max(CARO_CELL_MIN, Math.min(CARO_CELL_MAX, Math.floor((room15 - CARO_VIEW + 1) / CARO_VIEW)))
    : 28;
  const view = CARO_VIEW * base + CARO_VIEW - 1; // cạnh cửa sổ nhìn (ô + đường kẻ 1px)
  const big = s.size > CARO_VIEW;
  const zoomed = big && overview;
  const cell = zoomed ? Math.max(6, Math.floor((view - s.size + 1) / s.size)) : base;
  const boardPx = s.size * cell + s.size - 1;
  const boxPx = Math.min(boardPx, view);
  const panning = big && !zoomed;
  const updateEdges = () => {
    const el = scrollRef.current;
    if (!el) return;
    const e = {
      l: el.scrollLeft > 1,
      r: el.scrollLeft + el.clientWidth < el.scrollWidth - 1,
      t: el.scrollTop > 1,
      b: el.scrollTop + el.clientHeight < el.scrollHeight - 1,
    };
    setEdges((p) => (p.l === e.l && p.r === e.r && p.t === e.t && p.b === e.b ? p : e));
  };
  // nước mới (hoặc bàn vừa mở rộng): trượt cho nước vừa đánh nằm giữa cửa sổ
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (panning) {
      const at = s.last >= 0 ? s.last : Math.floor(s.size / 2) * s.size + Math.floor(s.size / 2);
      const cx = (at % s.size) * (cell + 1) + cell / 2;
      const cy = Math.floor(at / s.size) * (cell + 1) + cell / 2;
      el.scrollTo({ left: cx - el.clientWidth / 2, top: cy - el.clientHeight / 2, behavior: "smooth" });
    }
    updateEdges();
  }, [s.last, s.size, cell, panning]);
  // kéo bằng chuột (cảm ứng thì trình duyệt tự cuộn khi vuốt)
  const onPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!panning || e.pointerType !== "mouse" || e.button !== 0) return;
    const el = e.currentTarget;
    drag.current = { x: e.clientX, y: e.clientY, sl: el.scrollLeft, st: el.scrollTop, moved: false };
  };
  const onPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d) return;
    const dx = e.clientX - d.x;
    const dy = e.clientY - d.y;
    if (!d.moved && Math.hypot(dx, dy) < 5) return;
    if (!d.moved) {
      d.moved = true;
      e.currentTarget.setPointerCapture(e.pointerId);
    }
    e.currentTarget.scrollLeft = d.sl - dx;
    e.currentTarget.scrollTop = d.st - dy;
  };
  const onPointerUp = () => {
    if (drag.current?.moved) {
      dragged.current = true; // nhả chuột sau khi kéo: không tính là bấm ô
      setTimeout(() => (dragged.current = false), 0);
    }
    drag.current = null;
  };
  return (
    <div className="caro" ref={wrapRef}>
      <div className="caro-players">
        {[x, o].map((p, i) =>
          p ? (
            <span key={p.id} className={`caro-side${s.turn === p.id ? " is-turn" : ""}`}>
              <b className={`caro-mark caro-${i ? "o" : "x"}`}>{i ? "O" : "X"}</b>
              <Avatar user={ctx.user(p.id)} size={20} />
              {p.id === meId ? "Bạn" : p.name}
            </span>
          ) : null,
        )}
        {game.status === "playing" && <Countdown deadline={game.deadline} label="Thời gian nước đi" />}
      </div>
      <div className="caro-view">
        <div
          className={`caro-scroll${panning ? " is-panning" : ""}`}
          ref={scrollRef}
          style={{ width: `${boxPx + 2}px`, height: `${boxPx + 2}px` }}
          onScroll={updateEdges}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
          onClickCapture={(e) => {
            if (dragged.current) {
              e.stopPropagation();
              e.preventDefault();
            }
          }}
        >
          <div
            className="caro-board"
            style={{
              gridTemplateColumns: `repeat(${s.size}, ${cell}px)`,
              gridTemplateRows: `repeat(${s.size}, ${cell}px)`,
              fontSize: `${Math.max(7, Math.round(cell * 0.55))}px`,
            }}
            role="grid"
            aria-label={`Bàn cờ ${s.size}×${s.size}`}
          >
            {[...board].map((c, i) => (
              <button
                key={i}
                type="button"
                className={`caro-cell${c !== "." ? ` caro-${c}` : ""}${s.last === i ? " is-last" : ""}${win.has(i) ? " is-win" : ""}`}
                disabled={!myMove || c !== "." || busy}
                onClick={() => act("move", { cell: i, size: s.size })}
                aria-label={`Hàng ${Math.floor(i / s.size) + 1}, cột ${(i % s.size) + 1}${c === "." ? "" : `: ${c.toUpperCase()}`}`}
              >
                {c === "." ? "" : c.toUpperCase()}
              </button>
            ))}
          </div>
        </div>
        {panning && (
          <>
            {edges.l && <span className="caro-fade is-l" aria-hidden />}
            {edges.r && <span className="caro-fade is-r" aria-hidden />}
            {edges.t && <span className="caro-fade is-t" aria-hidden />}
            {edges.b && <span className="caro-fade is-b" aria-hidden />}
          </>
        )}
      </div>
      {big && (
        <button type="button" className="link-btn caro-zoom" onClick={() => setOverview((v) => !v)}>
          {zoomed ? "Về cỡ thường" : `Xem toàn bàn ${s.size}×${s.size}`}
        </button>
      )}
      {game.status === "playing" && ctx.isPlayer && (
        <div className="game-actions">
          <span className="game-hint">
            {myMove ? "Tới lượt bạn." : `Chờ ${ctx.nameOf(s.turn ?? 0)} đánh…`} Nước thứ {s.moves + 1}.
            {panning ? " Kéo bàn để xem chỗ khác." : ""}
          </span>
          <button type="button" className="link-btn game-cancel" disabled={busy} onClick={() => act("resign")}>
            Chịu thua
          </button>
        </div>
      )}
    </div>
  );
}

/* ---------- Oẳn tù tì ---------- */

const RPS: { id: RpsChoice; icon: string; name: string }[] = [
  { id: "rock", icon: "✊", name: "Búa" },
  { id: "paper", icon: "✋", name: "Bao" },
  { id: "scissors", icon: "✌️", name: "Kéo" },
];
const rpsIcon = (c: string) => RPS.find((r) => r.id === c)?.icon ?? "—";

function Rps({ ctx }: { ctx: Ctx }) {
  const { game, meId, busy, act } = ctx;
  const s = game.state as RpsState;
  const me = game.me as RpsMe | undefined;
  const playing = game.status === "playing";
  return (
    <div className="rps">
      <div className="rps-score">
        {game.players.map((p, i) => (
          <div key={p.id} className="rps-side">
            <Avatar user={ctx.user(p.id)} size={34} />
            <span className="rps-name">{p.id === meId ? "Bạn" : p.name}</span>
            {playing && <span className="rps-ready">{s.ready[i] ? "đã ra ✓" : "đang nghĩ…"}</span>}
          </div>
        ))}
        <p className="rps-points" aria-label="Tỉ số">
          {s.score[0]} – {s.score[1]}
        </p>
      </div>
      {playing && (
        <div className="game-banner">
          <span>Ván {s.rounds.length + 1}</span>
          <Countdown deadline={game.deadline} />
        </div>
      )}
      {playing && ctx.isPlayer && (
        <div className="rps-picks" role="group" aria-label="Ra tay">
          {RPS.map((r) => (
            <button
              key={r.id}
              type="button"
              className={`rps-pick${me?.pick === r.id ? " is-picked" : ""}`}
              disabled={busy || !!me?.pick}
              onClick={() => act("pick", { choice: r.id })}
            >
              <span aria-hidden>{r.icon}</span>
              {r.name}
            </button>
          ))}
        </div>
      )}
      {s.rounds.length > 0 && (
        <ol className="rps-rounds">
          {s.rounds
            .map((r, i) => (
              <li key={i}>
                Ván {i + 1}: {rpsIcon(r.picks[0])} vs {rpsIcon(r.picks[1])} →{" "}
                {r.winner ? (r.winner === meId ? "bạn thắng" : `${ctx.nameOf(r.winner)} thắng`) : "hoà"}
              </li>
            ))
            .reverse()}
        </ol>
      )}
      {playing && ctx.isPlayer && (
        <div className="game-actions">
          <button type="button" className="link-btn game-cancel" disabled={busy} onClick={() => act("resign")}>
            Chịu thua
          </button>
        </div>
      )}
    </div>
  );
}

/* ---------- Mở phòng chơi ---------- */

export function CreateGameDialog({ channelId, dm, onClose }: { channelId: number; dm: boolean; onClose: () => void }) {
  const { createGame } = useChat();
  const kinds = (Object.keys(GAME_INFO) as GameKind[]).filter((k) => !dm || k === "caro" || k === "rps");
  const [kind, setKind] = useState<GameKind>(kinds[0]);
  const [day, setDay] = useState(120);
  const [night, setNight] = useState(45);
  const [hideRoles, setHideRoles] = useState(false);
  const [spies, setSpies] = useState(0);
  const [blockRule, setBlockRule] = useState(true);
  const [bestOf, setBestOf] = useState(3);
  const [st, setSt] = useState({
    traitors: 0,
    kill_cooldown: 25,
    tasks: 5,
    discuss: 30,
    vote: 60,
    emergencies: 1,
    hide_ejects: false,
  });
  const setOpt = (k: keyof typeof st, v: number | boolean) => setSt((o) => ({ ...o, [k]: v }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const options: Record<GameKind, Record<string, unknown>> = {
      station: st,
      werewolf: { day_seconds: day, night_seconds: night, hide_roles: hideRoles },
      spy: { spies },
      caro: { free_style: !blockRule },
      rps: { best_of: bestOf },
    };
    try {
      await createGame(channelId, kind, options[kind]);
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không mở được phòng.");
      setBusy(false);
    }
  };

  return (
    <Dialog title="Chơi game" onClose={onClose}>
      <form className="dialog-form" onSubmit={submit}>
        <div className="game-picker" role="radiogroup" aria-label="Trò chơi">
          {kinds.map((k) => (
            <label key={k} className={`game-pick${kind === k ? " is-chosen" : ""}`}>
              <input type="radio" name="game-kind" value={k} checked={kind === k} onChange={() => setKind(k)} />
              <span className="game-pick-icon" aria-hidden>
                {GAME_INFO[k].icon}
              </span>
              <span className="game-pick-text">
                <b>{GAME_INFO[k].title}</b>
                <small>{GAME_INFO[k].players}</small>
                <span>{GAME_INFO[k].blurb}</span>
              </span>
            </label>
          ))}
        </div>

        {kind === "station" && (
          <>
            <div className="game-opts">
              <label className="field">
                <span>Số kẻ phản bội</span>
                <select value={st.traitors} onChange={(e) => setOpt("traitors", Number(e.target.value))}>
                  <option value={0}>Tự động (1 / 2 / 3)</option>
                  <option value={1}>1</option>
                  <option value={2}>2</option>
                  <option value={3}>3</option>
                </select>
              </label>
              <label className="field">
                <span>Hồi chiêu giết</span>
                <select value={st.kill_cooldown} onChange={(e) => setOpt("kill_cooldown", Number(e.target.value))}>
                  {[15, 20, 25, 30, 45].map((v) => (
                    <option key={v} value={v}>
                      {v} giây
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Nhiệm vụ mỗi người</span>
                <select value={st.tasks} onChange={(e) => setOpt("tasks", Number(e.target.value))}>
                  {[3, 4, 5, 6, 8].map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Lượt họp khẩn mỗi người</span>
                <select value={st.emergencies} onChange={(e) => setOpt("emergencies", Number(e.target.value))}>
                  {[1, 2, 3].map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Thảo luận</span>
                <select value={st.discuss} onChange={(e) => setOpt("discuss", Number(e.target.value))}>
                  {[15, 30, 45, 60, 90].map((v) => (
                    <option key={v} value={v}>
                      {v} giây
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Bỏ phiếu</span>
                <select value={st.vote} onChange={(e) => setOpt("vote", Number(e.target.value))}>
                  {[30, 60, 90, 120].map((v) => (
                    <option key={v} value={v}>
                      {v} giây
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <label className="check-field">
              <input
                type="checkbox"
                checked={st.hide_ejects}
                onChange={(e) => setOpt("hide_ejects", e.target.checked)}
              />
              <span>
                Giấu danh tính người bị đẩy ra
                <small>Không cho biết người bị loại có phải kẻ phản bội không (khó hơn).</small>
              </span>
            </label>
          </>
        )}
        {kind === "werewolf" && (
          <>
            <div className="game-opts">
              <label className="field">
                <span>Ban ngày (thảo luận + bỏ phiếu)</span>
                <select value={day} onChange={(e) => setDay(Number(e.target.value))}>
                  <option value={60}>1 phút</option>
                  <option value={120}>2 phút</option>
                  <option value={180}>3 phút</option>
                  <option value={300}>5 phút</option>
                </select>
              </label>
              <label className="field">
                <span>Ban đêm</span>
                <select value={night} onChange={(e) => setNight(Number(e.target.value))}>
                  <option value={30}>30 giây</option>
                  <option value={45}>45 giây</option>
                  <option value={60}>1 phút</option>
                  <option value={90}>1 phút 30</option>
                </select>
              </label>
            </div>
            <label className="check-field">
              <input type="checkbox" checked={hideRoles} onChange={(e) => setHideRoles(e.target.checked)} />
              <span>
                Giấu vai người chết
                <small>Chỉ lộ vai của mọi người khi hết ván (khó hơn).</small>
              </span>
            </label>
          </>
        )}
        {kind === "spy" && (
          <label className="field">
            <span>Số gián điệp</span>
            <select value={spies} onChange={(e) => setSpies(Number(e.target.value))}>
              <option value={0}>Tự động (1, từ 8 người là 2)</option>
              <option value={1}>1</option>
              <option value={2}>2</option>
              <option value={3}>3</option>
            </select>
          </label>
        )}
        {kind === "caro" && (
          <label className="check-field">
            <input type="checkbox" checked={blockRule} onChange={(e) => setBlockRule(e.target.checked)} />
            <span>
              Luật chặn hai đầu
              <small>Hàng 5 bị chặn cả hai đầu thì không tính thắng.</small>
            </span>
          </label>
        )}
        {kind === "rps" && (
          <label className="field">
            <span>Thể thức</span>
            <select value={bestOf} onChange={(e) => setBestOf(Number(e.target.value))}>
              <option value={1}>1 ván quyết định</option>
              <option value={3}>Chạm 2 (3 ván)</option>
              <option value={5}>Chạm 3 (5 ván)</option>
            </select>
          </label>
        )}

        <FormError>{error}</FormError>
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            Huỷ
          </button>
          <button className="btn btn-primary" disabled={busy}>
            {busy ? "Đang mở…" : "Mở phòng"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
