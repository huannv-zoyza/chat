import { useEffect, useState } from "react";
import type { WalletBoard } from "../lib/api";
import { BAUCUA_SYMBOLS, type BauCuaState } from "../lib/games";
import { useChat } from "../state/ChatProvider";
import { Countdown, type Ctx } from "./Games";
import { formatXu } from "./Wallet";
import { Avatar } from "./ui";

const CHIPS = [10, 50, 100, 500];

function signed(n: number) {
  return (n > 0 ? "+" : n < 0 ? "−" : "") + formatXu(Math.abs(n));
}

function Face({ sym, rolling }: { sym: string; rolling?: boolean }) {
  const s = BAUCUA_SYMBOLS.find((x) => x.id === sym);
  return (
    <span className={`baucua-die${rolling ? " is-rolling" : ""}`} title={s?.name}>
      {s?.icon ?? "?"}
    </span>
  );
}

/** Bàn bầu cua: lưới 6 con, chip, xúc xắc, lãi/lỗ từng người. */
export function BauCua({ ctx }: { ctx: Ctx }) {
  const { game, meId, isPlayer, busy, act, user, nameOf } = ctx;
  const s = (game.state ?? {}) as BauCuaState;
  const [chip, setChip] = useState(50);
  const ended = game.status === "ended";
  const betting = s.phase === "betting";
  const isDealer = s.dealer_id === meId;
  const myChips = s.chips?.[meId] ?? 0;
  const myBets = s.bets?.[meId] ?? {};
  const myBetTotal = Object.values(myBets).reduce((a, b) => a + b, 0);
  const canBet = isPlayer && betting && !isDealer && !busy;
  const anyBets = Object.values(s.bets ?? {}).some((b) => Object.keys(b).length > 0);

  // hiệu ứng lắc bát mỗi lần có kết quả mới
  const rollKey = s.history?.length ?? 0;
  const [rolling, setRolling] = useState(false);
  useEffect(() => {
    if (s.phase !== "reveal") return;
    setRolling(true);
    const t = window.setTimeout(() => setRolling(false), 900);
    return () => window.clearTimeout(t);
  }, [rollKey, s.phase]);

  const players = game.players;

  if (ended) return <BauCuaLedger ctx={ctx} state={s} />;

  return (
    <div className="baucua">
      <div className="baucua-top">
        <span className="baucua-dealer">
          {s.dealer_id ? (
            <>
              Cái: <strong>{isDealer ? "Bạn" : nameOf(s.dealer_id)}</strong> · lượt {s.roll_no}/{s.rolls_per_dealer} · còn{" "}
              {formatXu(s.chips?.[s.dealer_id] ?? 0)} xu
            </>
          ) : null}
        </span>
        <Countdown deadline={game.deadline} label={betting ? "Đặt cược" : "Mở bát"} />
      </div>

      {s.phase === "reveal" && s.dice.length === 3 && (
        <div className="baucua-dice" aria-live="polite">
          {s.dice.map((d, i) => (
            <Face key={`${rollKey}-${i}`} sym={rolling ? BAUCUA_SYMBOLS[(i * 2 + rollKey) % 6].id : d} rolling={rolling} />
          ))}
        </div>
      )}
      {s.phase === "reveal" && !rolling && !!s.last_bust && (
        <p className="baucua-bust">
          💸 {s.last_bust === meId ? "Bạn" : nameOf(s.last_bust)} cháy túi! Thiếu {formatXu(s.last_bust_amount ?? 0)} xu, người thắng được chia theo tỉ lệ
        </p>
      )}

      <div className="baucua-board" role="group" aria-label="Bàn cược">
        {BAUCUA_SYMBOLS.map((sym) => {
          const bettors = Object.entries(s.bets ?? {})
            .filter(([, b]) => (b[sym.id] ?? 0) > 0)
            .map(([uid, b]) => ({ id: Number(uid), amt: b[sym.id] }));
          const total = bettors.reduce((a, b) => a + b.amt, 0);
          const hits = s.phase === "reveal" && !rolling ? s.dice.filter((d) => d === sym.id).length : 0;
          return (
            <button
              key={sym.id}
              type="button"
              className={`baucua-cell${myBets[sym.id] ? " has-mine" : ""}${hits ? " is-hit" : ""}`}
              disabled={!canBet || myBetTotal + chip > myChips}
              onClick={() => act("bet", { symbol: sym.id, amount: chip })}
              aria-label={`${sym.name}${total ? `, đang cược ${total} xu` : ""}`}
            >
              <span className="baucua-icon" aria-hidden>
                {sym.icon}
              </span>
              <span className="baucua-name">
                {sym.name}
                {hits > 1 && <span className="baucua-hits"> ×{hits}</span>}
              </span>
              {total > 0 && <span className="baucua-total">{formatXu(total)}</span>}
              {bettors.length > 0 && (
                <span className="baucua-bettors">
                  {bettors.slice(0, 4).map((b) => (
                    <Avatar key={b.id} user={user(b.id)} size={18} />
                  ))}
                </span>
              )}
              {!!myBets[sym.id] && <span className="baucua-mine">bạn {formatXu(myBets[sym.id])}</span>}
            </button>
          );
        })}
      </div>

      {isPlayer && (
        <div className="baucua-controls">
          {!isDealer && (
            <>
              <div className="baucua-chips" role="radiogroup" aria-label="Mệnh giá chip">
                {CHIPS.map((c) => (
                  <button
                    key={c}
                    type="button"
                    role="radio"
                    aria-checked={chip === c}
                    className={`baucua-chip chip-${c}${chip === c ? " is-chosen" : ""}`}
                    onClick={() => setChip(c)}
                  >
                    {c}
                  </button>
                ))}
              </div>
              <button type="button" className="btn btn-sm" disabled={!canBet || myBetTotal === 0} onClick={() => act("clear")}>
                Xoá cược
              </button>
            </>
          )}
          {isDealer && betting && (
            <button type="button" className="btn btn-primary baucua-roll" disabled={busy || !anyBets} onClick={() => act("roll")}>
              🥣 Lắc!
            </button>
          )}
          {myChips < 10 && <span className="baucua-broke">Hết xu rồi: ngồi xem, hoặc ra 🌾 Nông trại kiếm thêm nhé</span>}
          {game.host_id === meId && (!betting || !anyBets) && (
            <button type="button" className="btn btn-sm btn-danger-outline" disabled={busy} onClick={() => act("finish")}>
              Kết thúc ván
            </button>
          )}
        </div>
      )}
      {isPlayer && !isDealer && (
        <p className="baucua-me">
          Bạn còn <strong>{formatXu(myChips)}</strong> xu{myBetTotal > 0 && <> · đang cược {formatXu(myBetTotal)}</>}
        </p>
      )}

      <ul className="baucua-players">
        {players.map((p) => {
          const res = s.phase === "reveal" && !rolling ? s.last_result?.[p.id] : undefined;
          return (
            <li key={p.id} className={p.id === s.dealer_id ? "is-dealer" : undefined}>
              <Avatar user={user(p.id)} size={24} />
              <span className="baucua-pname">
                {p.id === meId ? "Bạn" : p.name}
                {p.id === s.dealer_id && <span className="baucua-tag">cái</span>}
              </span>
              <span className="baucua-pchips">{formatXu(s.chips?.[p.id] ?? 0)}</span>
              {res !== undefined && res !== 0 && <span className={`baucua-delta ${res > 0 ? "is-up" : "is-down"}`}>{signed(res)}</span>}
            </li>
          );
        })}
      </ul>

      {s.history?.length > 1 && (
        <div className="baucua-history" aria-label="Các lượt trước">
          {s.history.slice(1, 6).map((h, i) => (
            <span key={i} className="baucua-past">
              {h.map((d, j) => (
                <Face key={j} sym={d} />
              ))}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

/** Tổng kết ván: lãi/lỗ, cháy túi, nợ, và top đại gia của kênh. */
function BauCuaLedger({ ctx, state: s }: { ctx: Ctx; state: BauCuaState }) {
  const { api } = useChat();
  const { game, meId, user } = ctx;
  const [board, setBoard] = useState<WalletBoard | null>(null);
  useEffect(() => {
    api.walletBoard(ctx.channelId).then(setBoard).catch(() => {});
  }, [api, ctx.channelId]);
  const rows = [...game.players].sort((a, b) => (s.net?.[b.id] ?? 0) - (s.net?.[a.id] ?? 0));
  return (
    <div className="baucua baucua-ledger">
      <ol className="baucua-players">
        {rows.map((p) => {
          const net = s.net?.[p.id] ?? 0;
          const busts = s.busts?.[p.id] ?? 0;
          const loans = s.loans?.[p.id] ?? 0;
          return (
            <li key={p.id}>
              <Avatar user={user(p.id)} size={24} />
              <span className="baucua-pname">
                {p.id === meId ? "Bạn" : p.name}
                {busts > 0 && <span className="baucua-tag is-bust">💸 cháy túi {busts} lần</span>}
              </span>
              {loans > 0 && <span className="baucua-loan">nợ {formatXu(loans)}</span>}
              <span className={`baucua-delta ${net > 0 ? "is-up" : net < 0 ? "is-down" : ""}`}>{signed(net)}</span>
            </li>
          );
        })}
      </ol>
      {board && board.rich.length > 0 && (
        <p className="baucua-rich">
          💰 Đại gia kênh:{" "}
          {board.rich.slice(0, 3).map((e, i) => (
            <span key={e.user.id}>
              {i > 0 && " · "}
              {e.user.id === meId ? "Bạn" : e.user.display_name || e.user.username} {formatXu(e.balance - e.debt)}
            </span>
          ))}
        </p>
      )}
    </div>
  );
}
