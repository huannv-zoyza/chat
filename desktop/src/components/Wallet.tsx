import { useEffect, useState } from "react";
import { ApiError, type Wallet, type WalletBoard } from "../lib/api";
import { displayName } from "../lib/format";
import { useChat } from "../state/ChatProvider";
import { Avatar, Dialog, FormError } from "./ui";

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
  const { api, state } = useChat();
  const [wallet, setWallet] = useState<Wallet | null>(null);
  const [board, setBoard] = useState<WalletBoard | null>(null);
  const [tab, setTab] = useState<"rich" | "debtors">("rich");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
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

  const run = (fn: () => Promise<Wallet>) => {
    setBusy(true);
    setError(null);
    fn()
      .then(setWallet)
      .catch((err) => setError(err instanceof ApiError ? err.message : "Không thực hiện được, thử lại."))
      .finally(() => setBusy(false));
  };

  const list = board ? board[tab] : [];

  return (
    <Dialog title="Ví xu" onClose={onClose}>
      <div className="wallet">
        <div className="wallet-sum">
          <p className="wallet-balance">
            <span aria-hidden>🪙</span> {formatXu(balance)} <small>xu</small>
          </p>
          {debt > 0 && <p className="wallet-debt">Đang nợ ngân hàng {formatXu(debt)} xu</p>}
          {!!wallet?.in_tables && <p className="wallet-note">{formatXu(wallet.in_tables)} xu đang trên bàn bầu cua</p>}
        </div>

        <div className="wallet-actions">
          <button
            type="button"
            className="btn btn-primary"
            disabled={busy || !wallet?.can_claim}
            onClick={() => run(api.claimDaily)}
          >
            {wallet?.can_claim === false ? "Mai quay lại nhé" : "Nhận 100 xu hôm nay"}
          </button>
          <button
            type="button"
            className="btn"
            disabled={busy || !wallet?.can_loan}
            onClick={() => run(api.takeLoan)}
            title={wallet?.can_loan ? undefined : "Chỉ vay khi còn dưới 100 xu"}
          >
            Vay ngân hàng 500
          </button>
          <button type="button" className="btn" disabled={busy || debt === 0 || balance === 0} onClick={() => run(api.repayDebt)}>
            Trả nợ
          </button>
        </div>
        <FormError>{error}</FormError>

        {channel && (
          <section className="wallet-board">
            <div className="wallet-tabs" role="tablist" aria-label={`Bảng xu của ${channel.name}`}>
              <button type="button" role="tab" aria-selected={tab === "rich"} onClick={() => setTab("rich")}>
                💰 Đại gia
              </button>
              <button type="button" role="tab" aria-selected={tab === "debtors"} onClick={() => setTab("debtors")}>
                💸 Con nợ
              </button>
            </div>
            {list.length === 0 ? (
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
            )}
          </section>
        )}
      </div>
    </Dialog>
  );
}
