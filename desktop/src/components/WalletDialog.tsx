import { useCallback, useEffect, useState, type FormEvent } from "react";
import { ApiError, type PublicUser, type WalletTransaction } from "../lib/api";
import { displayName } from "../lib/format";
import { playCoinSound } from "../lib/sound";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, FormError } from "./ui";

const TX_META: Record<string, { icon: string; label: string }> = {
  signup_bonus: { icon: "🎁", label: "Vốn khởi tạo" },
  daily_checkin: { icon: "📅", label: "Điểm danh" },
  bailout: { icon: "🆘", label: "Cứu trợ vốn" },
  transfer_out: { icon: "💸", label: "Chuyển Xu" },
  transfer_in: { icon: "💰", label: "Nhận Xu" },
  bet_lock: { icon: "🔒", label: "Đặt cược" },
  bet_refund: { icon: "↩️", label: "Hoàn tiền cược" },
  bet_win: { icon: "🏆", label: "Thắng cược" },
  bet_loss: { icon: "♟️", label: "Thua cược" },
};

const QUICK_AMOUNTS = [100, 500, 1000, 5000, 10000];

export function WalletDialog({ onClose }: { onClose: () => void }) {
  const { api, state, dispatch, refreshWallet } = useChat();
  const [tab, setTab] = useState<"transfer" | "history">("transfer");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // Transfer form state
  const [query, setQuery] = useState("");
  const [users, setUsers] = useState<PublicUser[]>([]);
  const [selectedUser, setSelectedUser] = useState<PublicUser | null>(null);
  const [amount, setAmount] = useState<number>(500);
  const [note, setNote] = useState("");

  // History state
  const [txFilter, setTxFilter] = useState("all");
  const [txs, setTxs] = useState<WalletTransaction[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [loadingTx, setLoadingTx] = useState(false);

  useEffect(() => {
    void refreshWallet();
  }, [refreshWallet]);

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
        const res = await api.walletTransactions({ limit: 25, before_id: beforeId, type: txFilter });
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

  const wallet = state.wallet;
  const balance = wallet?.balance ?? 0;
  const locked = wallet?.locked_balance ?? 0;

  const handleCheckin = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const res = await api.walletCheckin();
      dispatch({ type: "wallet.set", wallet: res.wallet });
      playCoinSound();
      setNotice("Điểm danh thành công! Bạn nhận được +2,000 Xu.");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không điểm danh được.");
    } finally {
      setBusy(false);
    }
  };

  const handleBailout = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const res = await api.walletBailout();
      dispatch({ type: "wallet.set", wallet: res.wallet });
      playCoinSound();
      setNotice("Đã nhận +1,000 Xu cứu trợ! Chúc bạn lật kèo thành công.");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không nhận được cứu trợ.");
    } finally {
      setBusy(false);
    }
  };

  const handleTransfer = async (e: FormEvent) => {
    e.preventDefault();
    if (!selectedUser) {
      setError("Vui lòng chọn người nhận Xu.");
      return;
    }
    if (!Number.isFinite(amount) || amount <= 0) {
      setError("Số Xu chuyển phải lớn hơn 0.");
      return;
    }
    if (amount > balance) {
      setError("Số dư khả dụng không đủ.");
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const res = await api.walletTransfer({
        to_user_id: selectedUser.id,
        amount: Math.floor(amount),
        note: note.trim(),
      });
      dispatch({ type: "wallet.set", wallet: res.wallet });
      setNotice(`Đã chuyển ${Math.floor(amount).toLocaleString("vi-VN")} Xu cho ${displayName(selectedUser)}.`);
      setNote("");
      setSelectedUser(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Chuyển Xu thất bại.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog title="🪙 Ví Xu & Giao Dịch" onClose={onClose} wide>
      <div className="wallet-dialog">
        {/* Thẻ tổng quan số dư */}
        <div className="wallet-summary-card">
          <div className="wallet-balances">
            <div>
              <span className="wallet-label">Số dư khả dụng</span>
              <div className="wallet-main-balance">{balance.toLocaleString("vi-VN")} Xu</div>
            </div>
            {locked > 0 && (
              <div className="wallet-locked-box">
                <span className="wallet-label">Đang giữ cược</span>
                <div className="wallet-locked-value">🔒 {locked.toLocaleString("vi-VN")} Xu</div>
              </div>
            )}
          </div>

          <div className="wallet-quick-actions">
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={busy || !wallet?.can_checkin}
              onClick={() => void handleCheckin()}
            >
              {wallet?.can_checkin ? "📅 Điểm danh (+2,000 Xu)" : "✅ Đã điểm danh hôm nay"}
            </button>
            {wallet?.can_bailout && (
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={busy}
                onClick={() => void handleBailout()}
              >
                🆘 Cứu trợ hết vốn (+1,000 Xu)
              </button>
            )}
          </div>
        </div>

        {notice && <div className="wallet-notice">{notice}</div>}
        <FormError>{error}</FormError>

        {/* Tabs */}
        <div className="lb-tabs" role="tablist">
          <button
            type="button"
            className={`lb-tab${tab === "transfer" ? " is-active" : ""}`}
            onClick={() => {
              setTab("transfer");
              setError(null);
            }}
          >
            💸 Chuyển Xu P2P
          </button>
          <button
            type="button"
            className={`lb-tab${tab === "history" ? " is-active" : ""}`}
            onClick={() => {
              setTab("history");
              setError(null);
            }}
          >
            📜 Lịch sử giao dịch
          </button>
        </div>

        {tab === "transfer" ? (
          <form className="dialog-form wallet-transfer-form" onSubmit={(e) => void handleTransfer(e)}>
            <div className="field">
              <span>Người nhận</span>
              {selectedUser ? (
                <div className="wallet-recipient-picked">
                  <Avatar user={selectedUser} size={26} />
                  <span>
                    <b>{displayName(selectedUser)}</b>{" "}
                    <small className="muted">@{selectedUser.username}</small>
                  </span>
                  <button
                    type="button"
                    className="link-btn"
                    onClick={() => setSelectedUser(null)}
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
                      <p className="muted" style={{ margin: "0.4rem" }}>
                        Không tìm thấy thành viên phù hợp.
                      </p>
                    ) : (
                      users.slice(0, 8).map((u) => (
                        <button
                          key={u.id}
                          type="button"
                          className="wallet-user-option"
                          onClick={() => setSelectedUser(u)}
                        >
                          <Avatar user={u} size={24} online={!!state.online[u.id]} />
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
              <span>Số Xu muốn chuyển</span>
              <div className="wallet-chips">
                {QUICK_AMOUNTS.map((v) => (
                  <button
                    key={v}
                    type="button"
                    className={`wallet-chip${amount === v ? " is-active" : ""}`}
                    onClick={() => setAmount(v)}
                  >
                    {v.toLocaleString("vi-VN")}
                  </button>
                ))}
              </div>
              <input
                type="number"
                min={1}
                max={Math.max(1, balance)}
                value={amount || ""}
                onChange={(e) => setAmount(Math.max(0, Number(e.target.value)))}
                placeholder="Nhập số Xu"
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

            <div className="dialog-actions">
              <button type="button" className="btn" onClick={onClose} disabled={busy}>
                Đóng
              </button>
              <button
                type="submit"
                className="btn btn-primary"
                disabled={busy || !selectedUser || amount <= 0 || amount > balance}
              >
                {busy ? "Đang chuyển..." : `Chuyển ${Math.floor(amount || 0).toLocaleString("vi-VN")} Xu`}
              </button>
            </div>
          </form>
        ) : (
          <div className="wallet-history">
            <div className="wallet-history-filter">
              <select value={txFilter} onChange={(e) => setTxFilter(e.target.value)}>
                <option value="all">Tất cả giao dịch</option>
                <option value="bet_win">Thắng cược</option>
                <option value="bet_lock">Đặt cược</option>
                <option value="bet_refund">Hoàn tiền cược</option>
                <option value="transfer_out">Chuyển Xu đi</option>
                <option value="transfer_in">Nhận Xu</option>
                <option value="daily_checkin">Điểm danh</option>
                <option value="bailout">Cứu trợ vốn</option>
              </select>
            </div>

            {txs.length === 0 && !loadingTx ? (
              <p className="muted" style={{ textAlign: "center", padding: "1.5rem 0" }}>
                Chưa có giao dịch nào.
              </p>
            ) : (
              <ul className="wallet-tx-list">
                {txs.map((t) => {
                  const meta = TX_META[t.type] ?? { icon: "🪙", label: t.type };
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
                          <span>·</span>
                          <span>Số dư sau: {t.balance_after.toLocaleString("vi-VN")} Xu</span>
                        </div>
                      </div>
                      <div className={`wallet-tx-amount${pos ? " is-plus" : neg ? " is-minus" : ""}`}>
                        {pos ? `+${t.amount.toLocaleString("vi-VN")}` : t.amount.toLocaleString("vi-VN")} Xu
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}

            {hasMore && (
              <div style={{ textAlign: "center", marginTop: "0.75rem" }}>
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
      </div>
    </Dialog>
  );
}
