import { useCallback, useEffect, useState, type FormEvent } from "react";
import { ApiError, type PublicUser, type Wallet, type WalletBoard, type WalletTransaction } from "../lib/api";
import { displayName } from "../lib/format";
import { playCoinSound } from "../lib/sound";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, FormError } from "./ui";

const TX_META: Record<string, { icon: string; label: string }> = {
  daily: { icon: "📅", label: "Nhận xu hằng ngày" },
  loan: { icon: "🏦", label: "Vay ngân hàng" },
  repay: { icon: "🧾", label: "Trả nợ" },
  buyin: { icon: "🎲", label: "Mang chip vào bàn" },
  cashout: { icon: "💰", label: "Rút chip về ví" },
  refund: { icon: "↩️", label: "Hoàn chip" },
  transfer_out: { icon: "💸", label: "Chuyển xu" },
  transfer_in: { icon: "🪙", label: "Nhận xu" },
  bet_lock: { icon: "🔒", label: "Đặt cược" },
  bet_refund: { icon: "↩️", label: "Hoàn tiền cược" },
  bet_win: { icon: "🏆", label: "Thắng cược" },
  bet_loss: { icon: "♟️", label: "Thua cược" },
  farm: { icon: "🌾", label: "Nông trại" },
  auto_repay: { icon: "🧾", label: "Tự trừ nợ (50% xu kiếm được)" },
};

const STREAK_REWARD = [100, 120, 140, 160, 180, 200, 300];

/** Vì sao chưa vay được: đang nợ, đang trong 3 ngày chờ, hay còn đủ xu. */
function loanBlock(w: Wallet | null, balance: number, debt: number): string | null {
  if (!w || w.can_loan) return null;
  if (debt > 0) return "Trả hết nợ cũ rồi mới vay tiếp được";
  if (w.loan_available_at) {
    const d = new Date(w.loan_available_at);
    const pad = (n: number) => String(n).padStart(2, "0");
    return `Vay lại được lúc ${pad(d.getHours())}:${pad(d.getMinutes())} ngày ${pad(d.getDate())}/${pad(d.getMonth() + 1)}`;
  }
  if (balance >= 100) return "Chỉ vay khi còn dưới 100 xu";
  return "Chưa vay được lúc này";
}

const QUICK_TRANSFER = [50, 100, 500, 1000, 5000];

/** 1250 -> "1.250" */
export function formatXu(n: number): string {
  return n.toLocaleString("vi-VN");
}

/** Nút số xu ở thanh bên, bấm mở ví. */
export function WalletButton({ onOpen }: { onOpen: () => void }) {
  const { state } = useChat();
  const w = state.wallet;
  if (!w) return null;
  return (
    <button
      type="button"
      className={`wallet-btn${w.debt > 0 ? " has-debt" : ""}`}
      onClick={onOpen}
      title="Ví xu"
    >
      <span aria-hidden>🪙</span>
      <span className="wallet-btn-amount">{formatXu(w.balance)} xu</span>
      {w.debt > 0 && <span className="wallet-btn-debt">nợ {formatXu(w.debt)}</span>}
    </button>
  );
}

export function WalletDialog({ channelId, onClose }: { channelId: number | null; onClose: () => void }) {
  const { api, state, dispatch } = useChat();
  const [wallet, setWallet] = useState<Wallet | null>(null);
  const [board, setBoard] = useState<WalletBoard | null>(null);
  const [tab, setTab] = useState<"rich" | "debtors" | "transfer" | "history">(channelId !== null ? "rich" : "transfer");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // Chuyển xu P2P
  const [query, setQuery] = useState("");
  const [users, setUsers] = useState<PublicUser[]>([]);
  const [recipient, setRecipient] = useState<PublicUser | null>(null);
  const [amount, setAmount] = useState<number>(100);
  const [note, setNote] = useState("");

  // Lịch sử giao dịch
  const [txFilter, setTxFilter] = useState("all");
  const [txs, setTxs] = useState<WalletTransaction[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [loadingTx, setLoadingTx] = useState(false);

  const channel = channelId !== null ? state.channels[channelId] : undefined;
  const balance = state.wallet?.balance ?? wallet?.balance ?? 0;
  const debt = state.wallet?.debt ?? wallet?.debt ?? 0;

  useEffect(() => {
    api.getWallet().then(setWallet).catch(() => setError("Không tải được ví."));
  }, [api, state.wallet]);
  useEffect(() => {
    if (channelId === null) return;
    api.walletBoard(channelId).then(setBoard).catch(() => setBoard(null));
  }, [api, channelId, state.wallet]);

  useEffect(() => {
    if (tab !== "transfer") return;
    let alive = true;
    const t = window.setTimeout(() => {
      api
        .searchUsers(query.trim())
        .then((res) => {
          if (alive) setUsers(res.filter((u) => u.id !== state.me.id));
        })
        .catch(() => {});
    }, 150);
    return () => {
      alive = false;
      window.clearTimeout(t);
    };
  }, [api, query, tab, state.me.id]);

  const loadHistory = useCallback(
    async (beforeId?: number, append = false) => {
      setLoadingTx(true);
      try {
        const res = await api.walletTransactions({ limit: 25, before_id: beforeId, kind: txFilter });
        setTxs((prev) => (append ? [...prev, ...res.items] : res.items));
        setHasMore(res.has_more);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "Không tải được lịch sử giao dịch.");
      } finally {
        setLoadingTx(false);
      }
    },
    [api, txFilter],
  );

  useEffect(() => {
    if (tab === "history") {
      void loadHistory(undefined, false);
    }
  }, [tab, loadHistory]);

  const run = (fn: () => Promise<Wallet>) => {
    setBusy(true);
    setError(null);
    setNotice(null);
    fn()
      .then((w) => {
        setWallet(w);
        dispatch({ type: "wallet.set", balance: w.balance, debt: w.debt });
      })
      .catch((err) => setError(err instanceof ApiError ? err.message : "Không thực hiện được, thử lại."))
      .finally(() => setBusy(false));
  };

  const handleTransfer = async (e: FormEvent) => {
    e.preventDefault();
    if (!recipient) {
      setError("Vui lòng chọn người nhận xu.");
      return;
    }
    if (!Number.isFinite(amount) || amount <= 0) {
      setError("Số xu chuyển phải lớn hơn 0.");
      return;
    }
    if (amount > balance) {
      setError("Số dư xu không đủ.");
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const res = await api.walletTransfer({
        to_user_id: recipient.id,
        amount: Math.floor(amount),
        note: note.trim(),
      });
      setWallet(res.wallet);
      dispatch({ type: "wallet.set", balance: res.wallet.balance, debt: res.wallet.debt });
      playCoinSound();
      setNotice(`Đã chuyển ${formatXu(Math.floor(amount))} xu cho ${displayName(recipient)}.`);
      setNote("");
      setRecipient(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Chuyển xu thất bại.");
    } finally {
      setBusy(false);
    }
  };

  const list = board && (tab === "rich" || tab === "debtors") ? board[tab] : [];

  return (
    <Dialog title="Ví xu" onClose={onClose}>
      <div className="wallet">
        <div className="wallet-sum">
          <p className="wallet-balance">
            <span aria-hidden>🪙</span> {formatXu(balance)} <small>xu</small>
          </p>
          {debt > 0 && <p className="wallet-debt">Đang nợ ngân hàng {formatXu(debt)} xu</p>}
          {!!wallet?.in_tables && <p className="wallet-note">{formatXu(wallet.in_tables)} xu đang trên bàn / đặt cược</p>}
        </div>

        <div className="wallet-actions">
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy || !wallet?.can_claim}
            onClick={() => run(api.claimDaily)}
          >
            {wallet?.can_claim === false
              ? "Mai quay lại nhé"
              : `Nhận ${STREAK_REWARD[(wallet?.streak ?? 0) % 7]} xu hôm nay`}
          </button>
          <button
            type="button"
            className="btn"
            disabled={busy || !wallet?.can_loan}
            onClick={() => run(api.takeLoan)}
            title={loanBlock(wallet, balance, debt) ?? "Vay 300 xu, trả 360 xu (lãi 20%)"}
          >
            Vay 300 xu
          </button>
          <button type="button" className="btn" disabled={busy || debt === 0 || balance === 0} onClick={() => run(api.repayDebt)}>
            Trả nợ
          </button>
        </div>
        {loanBlock(wallet, balance, debt) && balance < 100 && (
          <p className="wallet-note wallet-loan-note">
            🏦 {loanBlock(wallet, balance, debt)}. Ra <b>Nông trại</b> trồng rau kiếm xu nhé 🌾
          </p>
        )}
        {debt > 0 && <p className="wallet-note">Đang nợ: 50% số xu kiếm được từ nông trại và điểm danh sẽ tự trừ vào nợ.</p>}
        {notice && <p className="wallet-notice">{notice}</p>}
        <FormError>{error}</FormError>

        <section className="wallet-board">
          <div className="wallet-tabs" role="tablist" aria-label="Tiện ích ví xu">
            {channel && (
              <>
                <button type="button" role="tab" aria-selected={tab === "rich"} onClick={() => setTab("rich")}>
                  Đại gia
                </button>
                <button type="button" role="tab" aria-selected={tab === "debtors"} onClick={() => setTab("debtors")}>
                  Con nợ
                </button>
              </>
            )}
            <button type="button" role="tab" aria-selected={tab === "transfer"} onClick={() => setTab("transfer")}>
              Chuyển xu
            </button>
            <button type="button" role="tab" aria-selected={tab === "history"} onClick={() => setTab("history")}>
              Lịch sử
            </button>
          </div>

          {(tab === "rich" || tab === "debtors") &&
            (list.length === 0 ? (
              <p className="wallet-note">{tab === "rich" ? "Chưa có ai." : "Kênh này chưa ai nợ. Ngoan quá!"}</p>
            ) : (
              <ol className="wallet-list">
                {list.map((e, i) => (
                  <li key={e.user.id} className={e.user.id === state.me.id ? "is-me" : undefined}>
                    <span className="wallet-rank">{i + 1}</span>
                    <Avatar user={state.users[e.user.id] ?? e.user} size={26} />
                    <span className="wallet-name">{displayName(state.users[e.user.id] ?? e.user)}</span>
                    <span className="wallet-amount">
                      {tab === "rich" ? `${formatXu(e.balance - e.debt)} xu` : `nợ ${formatXu(e.debt)}`}
                    </span>
                  </li>
                ))}
              </ol>
            ))}

          {tab === "transfer" && debt > 0 && (
            <p className="wallet-note wallet-transfer-blocked">
              Bạn đang nợ ngân hàng {formatXu(debt)} xu. Trả hết nợ rồi mới chuyển xu cho người khác được.
            </p>
          )}
          {tab === "transfer" && debt === 0 && (
            <form className="wallet-transfer-form" onSubmit={(e) => void handleTransfer(e)}>
              <div className="field">
                <span>Người nhận</span>
                {recipient ? (
                  <div className="wallet-recipient-picked">
                    <Avatar user={recipient} size={24} />
                    <span>
                      <b>{displayName(recipient)}</b> <small className="muted">@{recipient.username}</small>
                    </span>
                    <button
                      type="button"
                      className="link-btn"
                      onClick={() => setRecipient(null)}
                      style={{ marginLeft: "auto" }}
                    >
                      Đổi người
                    </button>
                  </div>
                ) : (
                  <>
                    <input
                      className="search"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                      placeholder="Tìm theo tên hoặc @username..."
                    />
                    <div className="wallet-user-picker">
                      {users.length === 0 ? (
                        <p className="wallet-note" style={{ margin: "0.4rem" }}>
                          Không tìm thấy thành viên phù hợp.
                        </p>
                      ) : (
                        users.slice(0, 8).map((u) => (
                          <button
                            key={u.id}
                            type="button"
                            className="wallet-user-option"
                            onClick={() => setRecipient(u)}
                          >
                            <Avatar user={u} size={22} online={!!state.online[u.id]} />
                            <span>{displayName(u)}</span>
                            <small className="muted">@{u.username}</small>
                          </button>
                        ))
                      )}
                    </div>
                  </>
                )}
              </div>

              <div className="field">
                <span>Số xu muốn chuyển</span>
                <div className="wallet-chips">
                  {QUICK_TRANSFER.map((v) => (
                    <button
                      key={v}
                      type="button"
                      className={`wallet-chip${amount === v ? " is-active" : ""}`}
                      onClick={() => setAmount(v)}
                    >
                      {formatXu(v)}
                    </button>
                  ))}
                </div>
                <input
                  type="number"
                  min={1}
                  max={Math.max(1, balance)}
                  value={amount || ""}
                  onChange={(e) => setAmount(Math.max(0, Number(e.target.value)))}
                  placeholder="Nhập số xu"
                />
              </div>

              <label className="field">
                <span>Lời nhắn (tuỳ chọn)</span>
                <input
                  type="text"
                  maxLength={120}
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder="Ví dụ: Trả kèo cờ tướng..."
                />
              </label>

              <button
                type="submit"
                className="btn btn-primary"
                disabled={busy || !recipient || amount <= 0 || amount > balance}
              >
                {busy ? "Đang chuyển..." : `Chuyển ${formatXu(Math.floor(amount || 0))} xu`}
              </button>
            </form>
          )}

          {tab === "history" && (
            <div className="wallet-history">
              <div className="wallet-history-filter">
                <select value={txFilter} onChange={(e) => setTxFilter(e.target.value)} aria-label="Lọc giao dịch">
                  <option value="all">Tất cả giao dịch</option>
                  <option value="bet_win">Thắng cược Cờ tướng</option>
                  <option value="bet_lock">Đặt cược Cờ tướng</option>
                  <option value="bet_refund">Hoàn tiền cược</option>
                  <option value="transfer_out">Chuyển xu đi</option>
                  <option value="transfer_in">Nhận xu</option>
                  <option value="daily">Nhận xu hằng ngày</option>
                  <option value="loan">Vay ngân hàng</option>
                  <option value="repay">Trả nợ</option>
                  <option value="buyin">Mang chip Bầu cua</option>
                  <option value="cashout">Rút chip Bầu cua</option>
                </select>
              </div>

              {txs.length === 0 && !loadingTx ? (
                <p className="wallet-note">Chưa có giao dịch nào.</p>
              ) : (
                <ul className="wallet-tx-list">
                  {txs.map((t) => {
                    const meta = TX_META[t.kind] ?? { icon: "🪙", label: t.kind };
                    const pos = t.amount > 0;
                    const neg = t.amount < 0;
                    return (
                      <li key={t.id} className="wallet-tx-item">
                        <span className="wallet-tx-icon" aria-hidden>
                          {meta.icon}
                        </span>
                        <div className="wallet-tx-body">
                          <div className="wallet-tx-title">{t.note || meta.label}</div>
                          <div className="wallet-tx-sub">
                            <span>{meta.label}</span>
                            <span>·</span>
                            <span>{new Date(t.created_at).toLocaleString("vi-VN")}</span>
                          </div>
                        </div>
                        <div className={`wallet-tx-amount${pos ? " is-plus" : neg ? " is-minus" : ""}`}>
                          {pos ? `+${formatXu(t.amount)}` : formatXu(t.amount)} xu
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}

              {hasMore && (
                <div style={{ textAlign: "center", marginTop: "0.5rem" }}>
                  <button
                    type="button"
                    className="btn btn-sm"
                    disabled={loadingTx}
                    onClick={() => void loadHistory(txs[txs.length - 1]?.id, true)}
                  >
                    {loadingTx ? "Đang tải..." : "Xem thêm"}
                  </button>
                </div>
              )}
            </div>
          )}
        </section>
      </div>
    </Dialog>
  );
}
