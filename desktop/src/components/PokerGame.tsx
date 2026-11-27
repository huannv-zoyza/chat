import { useEffect, useState, type ReactNode } from "react";
import type { PokerCard, PokerMe, PokerSeat, PokerState } from "../lib/games";
import { Countdown, type Ctx } from "./Games";
import { formatXu } from "./Wallet";
import { Avatar, Dialog } from "./ui";

/* =========================================================
   Lá bài
   ========================================================= */

const SUITS: Record<string, { sym: string; name: string; red: boolean }> = {
  s: { sym: "♠", name: "bích", red: false },
  h: { sym: "♥", name: "cơ", red: true },
  d: { sym: "♦", name: "rô", red: true },
  c: { sym: "♣", name: "chuồn", red: false },
};

const rankLabel = (c: PokerCard) => (c[0] === "T" ? "10" : c[0]);

export function PlayingCard({ card, hidden, small, hot }: { card?: PokerCard; hidden?: boolean; small?: boolean; hot?: boolean }) {
  const cls = `pk-card${small ? " is-small" : ""}${hot ? " is-hot" : ""}`;
  if (hidden || !card) return <span className={`${cls} is-back`} aria-label="Lá bài úp" />;
  const s = SUITS[card[1]];
  return (
    <span className={`${cls}${s?.red ? " is-red" : ""}`} aria-label={`${rankLabel(card)} ${s?.name ?? ""}`}>
      <b>{rankLabel(card)}</b>
      <i aria-hidden>{s?.sym}</i>
    </span>
  );
}

function CardRow({ cards, hot, small }: { cards: PokerCard[]; hot?: PokerCard[]; small?: boolean }) {
  return (
    <span className="pk-cards">
      {cards.map((c) => (
        <PlayingCard key={c} card={c} small={small} hot={hot?.includes(c)} />
      ))}
    </span>
  );
}

/* =========================================================
   Hướng dẫn cho người mới
   ========================================================= */

const HAND_RANKS: { name: string; en: string; cards: PokerCard[]; note: string }[] = [
  { name: "Thùng phá sảnh lớn", en: "Royal flush", cards: ["As", "Ks", "Qs", "Js", "Ts"], note: "10-J-Q-K-A cùng chất. Mạnh nhất." },
  { name: "Thùng phá sảnh", en: "Straight flush", cards: ["9h", "8h", "7h", "6h", "5h"], note: "5 lá liên tiếp cùng chất." },
  { name: "Tứ quý", en: "Four of a kind", cards: ["Qs", "Qh", "Qd", "Qc", "2s"], note: "4 lá cùng hạng." },
  { name: "Cù lũ", en: "Full house", cards: ["Js", "Jh", "Jd", "4c", "4s"], note: "Một bộ ba + một đôi." },
  { name: "Thùng", en: "Flush", cards: ["Kd", "Jd", "9d", "6d", "3d"], note: "5 lá cùng chất, không liên tiếp." },
  { name: "Sảnh", en: "Straight", cards: ["Th", "9s", "8d", "7c", "6h"], note: "5 lá liên tiếp, khác chất. A-2-3-4-5 là sảnh nhỏ nhất." },
  { name: "Sám cô", en: "Three of a kind", cards: ["7s", "7h", "7d", "Kc", "2s"], note: "3 lá cùng hạng." },
  { name: "Hai đôi", en: "Two pair", cards: ["Ks", "Kh", "4d", "4c", "9s"], note: "Hai đôi khác hạng." },
  { name: "Một đôi", en: "One pair", cards: ["As", "Ah", "9d", "5c", "2s"], note: "2 lá cùng hạng." },
  { name: "Mậu thầu", en: "High card", cards: ["As", "Qd", "9h", "5c", "3s"], note: "Không có gì: so lá cao nhất." },
];

export function PokerGuide() {
  return (
    <div className="pk-guide">
      <section>
        <h3>🎯 Mục tiêu</h3>
        <p>
          Giành chip của người khác. Một tay bài kết thúc khi mọi người khác <b>bỏ bài</b> (người còn lại ăn hết, không cần
          lật bài), hoặc khi qua hết 4 vòng cược thì <b>lật bài</b>: ai có bộ 5 lá mạnh nhất ăn pot.
        </p>
        <p>
          Bạn dùng <b>5 lá tốt nhất trong 7 lá</b>: 2 lá tẩy riêng của mình + 5 lá bài chung trên bàn (dùng 0, 1 hay cả 2 lá tẩy
          đều được).
        </p>
      </section>

      <section>
        <h3>🃏 Diễn biến một tay</h3>
        <ol>
          <li>
            <b>Nút D</b> (dealer) xoay vòng mỗi tay. Người bên trái nút đặt <b>mù nhỏ</b>, người kế tiếp đặt <b>mù lớn</b> (gấp
            đôi). Chơi 2 người thì người cầm nút là mù nhỏ.
          </li>
          <li>Mỗi người nhận <b>2 lá tẩy</b> úp, chỉ mình thấy.</li>
          <li>
            <b>Preflop</b>: cược vòng 1, bắt đầu từ người bên trái mù lớn. Mù lớn được nói cuối.
          </li>
          <li>
            <b>Flop</b>: lật 3 lá chung, cược vòng 2. Từ vòng này người bên trái nút nói trước.
          </li>
          <li>
            <b>Turn</b>: lật lá chung thứ 4, cược vòng 3.
          </li>
          <li>
            <b>River</b>: lật lá chung thứ 5, cược vòng cuối.
          </li>
          <li>
            <b>Lật bài</b>: ai còn lại lật bài, bộ mạnh nhất thắng. Bằng nhau thì chia đều.
          </li>
        </ol>
      </section>

      <section>
        <h3>🕹️ Tới lượt bạn có thể</h3>
        <ul>
          <li>
            <b>Bỏ bài</b> (Fold): bỏ tay này, mất số chip đã cược.
          </li>
          <li>
            <b>Check</b>: không cược thêm, chỉ được khi chưa ai cược ở vòng này.
          </li>
          <li>
            <b>Theo</b> (Call): bỏ thêm chip cho bằng mức cược cao nhất.
          </li>
          <li>
            <b>Cược / Tố</b> (Bet / Raise): nâng mức cược. Cược ít nhất bằng 1 mù lớn; tố thì phải tăng thêm ít nhất bằng lần
            tố trước đó.
          </li>
          <li>
            <b>Tất tay</b> (All-in): đẩy hết chip. Hết chip giữa chừng vẫn được lật bài, nhưng chỉ ăn được phần pot mà mỗi người
            góp bằng mình (phần dư thành <b>pot phụ</b> cho người khác).
          </li>
        </ul>
      </section>

      <section>
        <h3>🏆 Thứ tự bộ bài (mạnh → yếu)</h3>
        <ul className="pk-ranks">
          {HAND_RANKS.map((h, i) => (
            <li key={h.name}>
              <span className="pk-rank-no">{i + 1}</span>
              <span className="pk-rank-main">
                <b>
                  {h.name} <small>({h.en})</small>
                </b>
                <small>{h.note}</small>
              </span>
              <CardRow cards={h.cards} small />
            </li>
          ))}
        </ul>
        <p className="pk-guide-note">
          Cùng loại bộ thì so hạng lá (A lớn nhất, rồi K, Q, J, 10… 2), rồi tới các lá phụ (kicker). Chất bài không phân hơn
          kém.
        </p>
      </section>

      <section>
        <h3>🪙 Xu và chip</h3>
        <ul>
          <li>Vào bàn bị trừ đúng mức mua vào trong ví, đổi thành chip (1 xu = 1 chip).</li>
          <li>Hết chip là bị loại. Còn một người có chip thì người đó thắng cả bàn.</li>
          <li>Hết ván (hoặc chủ phòng bấm Kết thúc), chip còn lại tự đổi lại thành xu về ví.</li>
          <li>Mù tăng dần theo số tay để ván không kéo dài mãi.</li>
          <li>Hết giờ lượt thì tự check (nếu được) hoặc bỏ bài. Hết giờ 2 lần liền là bị tính vắng mặt cho tới khi bấm Quay lại.</li>
        </ul>
      </section>

      <section>
        <h3>💡 Mẹo cho người mới</h3>
        <ul>
          <li>Đừng chơi mọi tay. Bài đẹp để theo: đôi lớn (A-A, K-K, Q-Q, J-J), A-K, A-Q, K-Q cùng chất.</li>
          <li>Ngồi gần nút (nói sau) có lợi: bạn thấy người khác làm gì rồi mới quyết.</li>
          <li>Đã bỏ nhiều chip vào pot không phải lý do để theo tiếp. Bài yếu thì bỏ.</li>
          <li>Có bài mạnh thì cược để người khác phải trả tiền, đừng chỉ check.</li>
          <li>Để ý ai hay tố, ai hay theo. Đừng tố tháu (bluff) người lúc nào cũng theo.</li>
        </ul>
      </section>
    </div>
  );
}

export function PokerGuideButton({ label = "📖 Hướng dẫn chơi" }: { label?: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" className="btn btn-sm pk-guide-btn" onClick={() => setOpen(true)}>
        {label}
      </button>
      {open && (
        <Dialog title="Hướng dẫn Poker Texas Hold'em" onClose={() => setOpen(false)} className="pk-guide-dialog">
          <PokerGuide />
        </Dialog>
      )}
    </>
  );
}

/* =========================================================
   Bàn chơi
   ========================================================= */

const PHASE_NAME: Record<string, string> = {
  preflop: "Preflop",
  flop: "Flop",
  turn: "Turn",
  river: "River",
  showdown: "Kết quả",
};

function signed(n: number) {
  return (n > 0 ? "+" : n < 0 ? "−" : "") + formatXu(Math.abs(n));
}

export function Poker({ ctx }: { ctx: Ctx }) {
  const { game, meId, isPlayer, busy, act, user, nameOf } = ctx;
  const s = (game.state ?? {}) as PokerState;
  const me = game.me as PokerMe | undefined;
  const isHost = game.host_id === meId;

  if (game.status === "ended") return <PokerLedger ctx={ctx} state={s} />;

  const r = s.phase === "showdown" ? s.result : undefined;
  const winners = new Set(r?.pots.flatMap((p) => p.winners) ?? []);
  // lá thắng (5 lá tạo bộ của người ăn pot chính) để làm nổi trên bàn
  const mainWinner = r?.pots[0]?.winners[0];
  const hot = mainWinner !== undefined ? r?.best?.[mainWinner] : undefined;
  const myTurn = s.turn_id === meId && !!me;

  return (
    <div className="poker">
      <div className="pk-top">
        <span>
          Tay #{s.hand_no} · Mù <b>{formatXu(s.sb)}</b>/<b>{formatXu(s.bb)}</b>
          {s.next_blinds > 0 && <small> · tăng sau {s.next_blinds} tay</small>}
        </span>
        <Countdown deadline={game.deadline} label={s.phase === "showdown" ? "Tay mới" : myTurn ? "Lượt bạn" : "Lượt"} />
        <PokerGuideButton label="📖 Luật" />
      </div>

      <div className="pk-felt">
        <span className="pk-phase">{PHASE_NAME[s.phase] ?? ""}</span>
        <div className="pk-board" aria-label="Bài chung">
          {Array.from({ length: 5 }, (_, i) =>
            s.board[i] ? (
              <PlayingCard key={s.board[i]} card={s.board[i]} hot={hot?.includes(s.board[i])} />
            ) : (
              <span key={`e${i}`} className="pk-card is-slot" aria-hidden />
            ),
          )}
        </div>
        <span className="pk-pot">
          Pot <b>{formatXu(s.pot)}</b>
        </span>
      </div>

      {r && (
        <div className="pk-result" aria-live="polite">
          {r.pots.map((p, i) => (
            <p key={i}>
              🏆 {p.winners.map((id) => (id === meId ? "Bạn" : nameOf(id))).join(", ")} ăn{" "}
              {r.pots.length > 1 ? (i === 0 ? "pot chính" : `pot phụ ${i}`) : "pot"} {formatXu(p.amount)}
              {p.hand ? ` · ${p.hand.toLowerCase()}` : r.showdown ? "" : " (mọi người bỏ bài)"}
            </p>
          ))}
        </div>
      )}

      <ul className="pk-seats" aria-label="Người chơi">
        {s.seats.map((seat) => (
          <SeatRow
            key={seat.id}
            seat={seat}
            s={s}
            meId={meId}
            name={seat.id === meId ? "Bạn" : nameOf(seat.id)}
            avatar={<Avatar user={user(seat.id)} size={30} />}
            shown={r?.shown?.[seat.id]}
            hand={r?.hands?.[seat.id]}
            won={r?.won?.[seat.id] ?? 0}
            winner={winners.has(seat.id)}
            hot={r?.best?.[seat.id]}
          />
        ))}
      </ul>

      {isPlayer && me && (
        <div className={`pk-me${myTurn ? " is-turn" : ""}`}>
          <div className="pk-hole">
            <span className="pk-me-label">Bài của bạn</span>
            {me.hole.length ? <CardRow cards={me.hole} hot={hot} /> : <span className="muted">Chờ tay sau</span>}
            {me.hand_name && <span className="pk-handname">{me.hand_name}</span>}
          </div>
          {me.away && (
            <p className="pk-away">
              Bạn đang bị tính vắng mặt, tới lượt sẽ tự check/bỏ bài.{" "}
              <button type="button" className="btn btn-sm btn-primary" disabled={busy} onClick={() => act("back")}>
                Quay lại bàn
              </button>
            </p>
          )}
          {myTurn && <ActionBar s={s} me={me} busy={busy} act={act} />}
        </div>
      )}

      {isHost && (
        <div className="game-actions">
          <button
            type="button"
            className="btn btn-sm"
            disabled={busy}
            onClick={() => {
              if (window.confirm("Kết thúc ván? Tay đang dở sẽ hoàn cược, chip của mọi người đổi lại thành xu về ví."))
                act("finish");
            }}
          >
            Kết thúc ván
          </button>
        </div>
      )}
    </div>
  );
}

function SeatRow({
  seat,
  s,
  meId,
  name,
  avatar,
  shown,
  hand,
  won,
  winner,
  hot,
}: {
  seat: PokerSeat;
  s: PokerState;
  meId: number;
  name: string;
  avatar: ReactNode;
  shown?: PokerCard[];
  hand?: string;
  won: number;
  winner: boolean;
  hot?: PokerCard[];
}) {
  const turn = s.turn_id === seat.id;
  const status = seat.out
    ? "Đã bị loại"
    : seat.folded
      ? "Bỏ bài"
      : seat.all_in
        ? "Tất tay"
        : seat.away
          ? "Vắng mặt"
          : "";
  return (
    <li
      className={`pk-seat${turn ? " is-turn" : ""}${seat.folded || seat.out ? " is-folded" : ""}${winner ? " is-winner" : ""}${seat.id === meId ? " is-me" : ""}`}
    >
      <span className="pk-avatar">
        {avatar}
        {s.button_id === seat.id && (
          <i className="pk-badge is-d" title="Nút dealer">
            D
          </i>
        )}
      </span>
      <span className="pk-seat-main">
        <b>
          {name}
          {s.sb_id === seat.id && s.phase !== "showdown" && (
            <small className="pk-blind" title="Mù nhỏ">
              SB
            </small>
          )}
          {s.bb_id === seat.id && s.phase !== "showdown" && (
            <small className="pk-blind" title="Mù lớn">
              BB
            </small>
          )}
        </b>
        <small>
          {formatXu(seat.stack)} chip
          {seat.last && seat.last !== status && ` · ${seat.last}`}
          {status && ` · ${status}`}
          {hand && ` · ${hand}`}
        </small>
      </span>
      <span className="pk-seat-side">
        {shown ? (
          <CardRow cards={shown} hot={hot} small />
        ) : seat.cards > 0 && seat.id !== meId ? (
          <span className="pk-cards">
            {Array.from({ length: seat.cards }, (_, i) => (
              <PlayingCard key={i} hidden small />
            ))}
          </span>
        ) : null}
        {seat.bet > 0 && <span className="pk-bet">{formatXu(seat.bet)}</span>}
        {won > 0 && <span className="pk-won">+{formatXu(won)}</span>}
      </span>
    </li>
  );
}

function ActionBar({
  s,
  me,
  busy,
  act,
}: {
  s: PokerState;
  me: PokerMe;
  busy: boolean;
  act: (action: string, data?: unknown) => void;
}) {
  const [to, setTo] = useState(me.min_to);
  // lượt mới (mức cược đổi) thì đưa ô tố về mức tối thiểu
  useEffect(() => setTo(me.min_to), [me.min_to, s.cur_bet, s.phase]);
  const clamp = (n: number) => Math.max(me.min_to, Math.min(me.max_to, Math.round(n)));
  const potAfterCall = s.pot + me.to_call;
  const quick = [
    { label: "Tối thiểu", to: me.min_to },
    { label: "½ pot", to: clamp(s.cur_bet + potAfterCall / 2) },
    { label: "Pot", to: clamp(s.cur_bet + potAfterCall) },
  ];
  const betWord = s.cur_bet === 0 ? "Cược" : "Tố lên";
  const value = clamp(to);
  return (
    <div className="pk-actions" role="group" aria-label="Hành động">
      <div className="pk-actions-main">
        <button type="button" className="btn pk-fold" disabled={busy} onClick={() => act("fold")}>
          Bỏ bài
        </button>
        {me.can_check ? (
          <button type="button" className="btn btn-primary" disabled={busy} onClick={() => act("check")}>
            Check
          </button>
        ) : (
          me.can_call && (
            <button type="button" className="btn btn-primary" disabled={busy} onClick={() => act("call")}>
              {me.to_call >= me.max_to ? `Tất tay ${formatXu(me.to_call)}` : `Theo ${formatXu(me.to_call)}`}
            </button>
          )
        )}
        {me.can_raise && (
          <button
            type="button"
            className="btn btn-primary pk-raise"
            disabled={busy}
            onClick={() => (value >= me.max_to ? act("allin") : act("raise", { to: value }))}
          >
            {value >= me.max_to ? `Tất tay ${formatXu(me.max_to)}` : `${betWord} ${formatXu(value)}`}
          </button>
        )}
      </div>
      {me.can_raise && me.max_to > me.min_to && (
        <div className="pk-raise-box">
          <input
            type="range"
            min={me.min_to}
            max={me.max_to}
            step={Math.max(1, s.bb / 2)}
            value={value}
            onChange={(e) => setTo(Number(e.target.value))}
            aria-label={`Mức ${betWord.toLowerCase()}`}
          />
          <input
            type="number"
            min={me.min_to}
            max={me.max_to}
            value={to || ""}
            onChange={(e) => setTo(Math.floor(Number(e.target.value) || 0))}
            onBlur={() => setTo(value)}
            aria-label="Nhập số chip"
          />
          <div className="pk-quick">
            {quick.map((q) => (
              <button key={q.label} type="button" className="btn btn-sm" disabled={busy} onClick={() => setTo(q.to)}>
                {q.label}
              </button>
            ))}
            <button type="button" className="btn btn-sm" disabled={busy} onClick={() => setTo(me.max_to)}>
              Tất tay
            </button>
          </div>
        </div>
      )}
      <p className="pk-hint">
        {me.can_check
          ? "Chưa ai cược: check để xem tiếp miễn phí, hoặc cược nếu bài tốt."
          : `Cần thêm ${formatXu(me.to_call)} chip để theo.`}
        {!me.can_raise && !me.can_check && me.max_to > s.cur_bet && " Lượt tố vừa rồi chưa đủ mức nên bạn chỉ được theo hoặc bỏ."}
      </p>
    </div>
  );
}

/** Hết ván: chip cuối và lãi/lỗ từng người. */
function PokerLedger({ ctx, state }: { ctx: Ctx; state: PokerState }) {
  const { meId, nameOf, user } = ctx;
  const rows = [...(state.seats ?? [])].sort((a, b) => b.net - a.net);
  return (
    <div className="poker">
      <p className="pk-ledger-title">
        Đã chơi {state.hand_no} tay · mua vào {formatXu(state.opts?.buy_in ?? 0)} xu/người · chip đã đổi về ví
      </p>
      <ul className="pk-seats">
        {rows.map((r) => (
          <li key={r.id} className={`pk-seat${r.net > 0 ? " is-winner" : ""}`}>
            <span className="pk-avatar">
              <Avatar user={user(r.id)} size={30} />
            </span>
            <span className="pk-seat-main">
              <b>{r.id === meId ? "Bạn" : nameOf(r.id)}</b>
              <small>{formatXu(r.stack)} chip</small>
            </span>
            <span className={`pk-net${r.net > 0 ? " is-up" : r.net < 0 ? " is-down" : ""}`}>{signed(r.net)} xu</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
