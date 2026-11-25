package controller

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"chat/game"
	"chat/store"
)

// Ví xu ảo.
//
//	GET  /api/wallet                     số dư, nợ, nhận được hôm nay chưa, vay được không, xu đang trên bàn
//	POST /api/wallet/claim               nhận 100 xu mỗi ngày (giờ VN)
//	POST /api/wallet/loan                vay ngân hàng 500 xu khi còn dưới 100
//	POST /api/wallet/repay               trả nợ bằng số dư
//	GET  /api/channels/{id}/wallet-board đại gia và con nợ của kênh
//
// Ví đổi thì người đó nhận wallet.updated {balance, debt}.

var vnZone = time.FixedZone("ICT", 7*3600)

func vnToday(now time.Time) string { return now.In(vnZone).Format("2006-01-02") }

type walletResp struct {
	Balance  int64 `json:"balance"`
	Debt     int64 `json:"debt"`
	CanClaim bool  `json:"can_claim"`
	CanLoan  bool  `json:"can_loan"`
	InTables int64 `json:"in_tables"`
}

type walletUpdatedEvent struct {
	Type    string `json:"type"`
	Balance int64  `json:"balance"`
	Debt    int64  `json:"debt"`
}

func (a *API) pushWallet(w store.Wallet) {
	a.hub.ToUser(w.UserID, mustJSON(walletUpdatedEvent{Type: "wallet.updated", Balance: w.Balance, Debt: w.Debt}))
}

func (a *API) walletResponse(w store.Wallet) walletResp {
	return walletResp{
		Balance:  w.Balance,
		Debt:     w.Debt,
		CanClaim: w.LastClaim != vnToday(time.Now()),
		CanLoan:  w.Balance < store.LoanBelow,
		InTables: a.chipsOnTables(w.UserID),
	}
}

func (a *API) getWallet(w http.ResponseWriter, r *http.Request) {
	wal, err := a.store.GetWallet(r.Context(), currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.walletResponse(wal))
}

func (a *API) walletAction(fn func(r *http.Request, userID int64) (store.Wallet, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wal, err := fn(r, currentUser(r).ID)
		switch {
		case errors.Is(err, store.ErrAlreadyClaimed):
			writeError(w, http.StatusConflict, "Hôm nay bạn nhận rồi, mai quay lại nhé.")
		case errors.Is(err, store.ErrLoanNotAllowed):
			writeError(w, http.StatusBadRequest, "Chỉ vay được khi còn dưới 100 xu.")
		case errors.Is(err, store.ErrNothingToRepay):
			writeError(w, http.StatusBadRequest, "Bạn không có nợ.")
		case err != nil:
			internalError(w, r, err)
		default:
			a.pushWallet(wal)
			writeJSON(w, http.StatusOK, a.walletResponse(wal))
		}
	}
}

func (a *API) walletBoard(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	rich, debtors, err := a.store.WalletBoard(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rich": rich, "debtors": debtors})
}

// chipsOnTables: số xu userID đang mang trên các bàn chưa kết thúc.
func (a *API) chipsOnTables(userID int64) int64 {
	a.games.mu.Lock()
	list := make([]*gameSession, 0, len(a.games.sessions))
	for _, s := range a.games.sessions {
		list = append(list, s)
	}
	a.games.mu.Unlock()
	var sum int64
	for _, s := range list {
		s.mu.Lock()
		if !s.deleted && s.g.Staked() && s.g.Status() != game.Ended {
			sum += s.g.StakeOf(userID).Chips
		}
		s.mu.Unlock()
	}
	return sum
}

/* ---------- Ván dùng chip: mua chip / trả chip (gọi khi đang giữ s.mu) ---------- */

// buyInLocked: userID mang toàn bộ số dư ví vào ván, ghi ví và trạng thái ván trong một transaction.
// Lỗi thì nạp lại ván từ DB để bỏ các thay đổi trong bộ nhớ chưa được lưu.
func (a *API) buyInLocked(ctx context.Context, s *gameSession, userID int64) error {
	ctx = context.WithoutCancel(ctx) // ghi tiền không bỏ dở vì người dùng đóng tab
	seq := s.g.BuyInCount(userID) + 1
	wal, err := a.store.BuyIn(ctx, s.messageID, userID, seq, func(amount int64) (string, []byte, error) {
		s.g.BuyIn(userID, amount)
		state, err := s.g.Marshal()
		return string(s.g.Status()), state, err
	})
	if err != nil {
		a.reloadLocked(ctx, s)
		return err
	}
	a.pushWallet(wal)
	return nil
}

// buyInExactLocked trừ đúng exactAmount xu từ ví userID và lưu trạng thái ván trong một transaction.
// Nếu không đủ xu hoặc commit lỗi thì nạp lại ván từ DB để bỏ thay đổi trong bộ nhớ.
func (a *API) buyInExactLocked(ctx context.Context, s *gameSession, userID, exactAmount int64) error {
	ctx = context.WithoutCancel(ctx)
	seq := s.g.BuyInCount(userID) + 1
	wal, err := a.store.BuyInExact(ctx, s.messageID, userID, seq, exactAmount, func(amount int64) (string, []byte, error) {
		s.g.BuyIn(userID, amount)
		state, err := s.g.Marshal()
		return string(s.g.Status()), state, err
	})
	if err != nil {
		a.reloadLocked(ctx, s)
		var ibe store.InsufficientBalanceError
		if errors.As(err, &ibe) {
			return game.Error(fmt.Sprintf("Không đủ %d xu để tham gia (bạn đang có %d xu).", ibe.Need, ibe.Have))
		}
		return err
	}
	a.pushWallet(wal)
	return nil
}

// payOutLocked trả chip (và nợ trong bàn) về ví, đồng thời lưu trạng thái ván. Gọi lại không cộng hai lần.
func (a *API) payOutLocked(ctx context.Context, s *gameSession, kind string, seq int64, stakes []game.Stake) error {
	ctx = context.WithoutCancel(ctx) // ghi tiền không bỏ dở vì người dùng đóng tab
	state, err := s.g.Marshal()
	if err != nil {
		return err
	}
	payouts := make([]store.Payout, 0, len(stakes))
	for _, st := range stakes {
		payouts = append(payouts, store.Payout{UserID: st.UserID, Chips: st.Chips, Loans: st.Loans})
	}
	wallets, err := a.store.PayOut(ctx, s.messageID, kind, seq, payouts, string(s.g.Status()), state)
	if err != nil {
		return err
	}
	for _, w := range wallets {
		a.pushWallet(w)
	}
	return nil
}

func (a *API) reloadLocked(ctx context.Context, s *gameSession) {
	row, err := a.store.GetGame(ctx, s.messageID)
	if err != nil {
		log.Printf("reload game %d: %v", s.messageID, err)
		return
	}
	if g, err := game.Load(row.Kind, row.State); err == nil {
		s.g = g
	}
}

// settleChannelGames: trước khi xoá kênh, kết thúc mọi ván dùng chip của kênh và trả chip về ví
// (xoá kênh xoá luôn các ván, sau đó không còn gì để trả bù).
func (a *API) settleChannelGames(ctx context.Context, channelID int64) error {
	rows, err := a.store.ActiveGamesByChannel(ctx, channelID)
	if err != nil {
		return err
	}
	ids := map[int64]bool{}
	for _, r := range rows {
		ids[r.MessageID] = true
	}
	a.games.mu.Lock()
	for id, s := range a.games.sessions {
		if s.channelID == channelID {
			ids[id] = true // gồm cả ván đã kết thúc mà chưa trả được chip
		}
	}
	a.games.mu.Unlock()
	for id := range ids {
		s, err := a.gameSession(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		s.mu.Lock()
		if s.g.Staked() && !s.paid {
			s.g.Abort("Kênh đã bị xoá.")
			err = a.payOutLocked(ctx, s, "cashout", 0, s.g.Settle())
			if err == nil {
				s.paid = true
			}
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// settlePendingCashouts: lúc khởi động, trả chip/cược cho các ván dùng xu đã kết thúc (hoặc tin đã xoá)
// mà chưa kịp trả (máy chủ tắt giữa chừng).
func (a *API) settlePendingCashouts(ctx context.Context) {
	rows, err := a.store.GamesNeedingCashout(ctx, "baucua", "rps")
	if err != nil {
		log.Printf("pending cashouts: %v", err)
		return
	}
	for _, row := range rows {
		g, err := game.Load(row.Kind, row.State)
		if err != nil {
			log.Printf("load game %d: %v", row.MessageID, err)
			continue
		}
		if !g.Staked() {
			continue
		}
		g.Abort("Ván kết thúc khi máy chủ khởi động lại.")
		s := &gameSession{messageID: row.MessageID, channelID: row.ChannelID, g: g}
		s.mu.Lock()
		if err := a.payOutLocked(ctx, s, "cashout", 0, g.Settle()); err != nil {
			log.Printf("cashout game %d: %v", row.MessageID, err)
		}
		s.mu.Unlock()
	}
}
