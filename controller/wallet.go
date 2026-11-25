package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"chat/game"
	"chat/model"
	"chat/store"
)

// Ví xu ảo.
//
//	GET  /api/wallet                     số dư, nợ, nhận được hôm nay chưa, vay được không, xu đang trên bàn
//	POST /api/wallet/claim               điểm danh mỗi ngày (giờ VN), thưởng theo chuỗi ngày liên tục
//	POST /api/wallet/loan                vay ngân hàng 300 xu (nợ 360) khi hết nợ, còn dưới 100, 3 ngày một lần
//	POST /api/wallet/repay               trả nợ bằng số dư
//	GET  /api/channels/{id}/wallet-board đại gia và con nợ của kênh
//
// Ví đổi thì người đó nhận wallet.updated {balance, debt}.

var vnZone = time.FixedZone("ICT", 7*3600)

func vnToday(now time.Time) string { return now.In(vnZone).Format("2006-01-02") }

type walletResp struct {
	Balance         int64   `json:"balance"`
	Debt            int64   `json:"debt"`
	CanClaim        bool    `json:"can_claim"`
	CanLoan         bool    `json:"can_loan"`
	InTables        int64   `json:"in_tables"`
	LoanAvailableAt *string `json:"loan_available_at"` // còn phải chờ mới vay lại được; null nếu không
	Streak          int     `json:"streak"`            // số ngày điểm danh liên tục (0 nếu đã đứt)
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
	today := vnToday(a.store.Now())
	resp := walletResp{
		Balance:  w.Balance,
		Debt:     w.Debt,
		CanClaim: w.LastClaim != today,
		InTables: a.chipsOnTables(w.UserID),
		Streak:   w.StreakOn(today),
	}
	until, err := a.store.LoanAvailableAt(context.Background(), w.UserID)
	if err != nil {
		log.Printf("loan available at %d: %v", w.UserID, err)
	}
	if until != nil {
		s := until.UTC().Format(time.RFC3339)
		resp.LoanAvailableAt = &s
	}
	resp.CanLoan = w.Balance < store.LoanBelow && w.Debt == 0 && until == nil && err == nil
	return resp
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
		var cooldown *store.LoanCooldownError
		switch {
		case errors.Is(err, store.ErrAlreadyClaimed):
			writeError(w, http.StatusConflict, "Hôm nay bạn nhận rồi, mai quay lại nhé.")
		case errors.Is(err, store.ErrLoanNotAllowed):
			writeError(w, http.StatusBadRequest, "Chỉ vay được khi còn dưới 100 xu.")
		case errors.Is(err, store.ErrLoanInDebt):
			writeError(w, http.StatusBadRequest, "Bạn còn nợ, trả hết rồi mới vay tiếp được.")
		case errors.As(err, &cooldown):
			writeError(w, http.StatusBadRequest, "Mỗi 3 ngày chỉ vay được một lần. Vay lại được lúc "+
				cooldown.Until.In(vnZone).Format("15:04")+" ngày "+cooldown.Until.In(vnZone).Format("02/01")+".")
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
	if locked, err := a.store.LockedXiangqiBets(context.Background(), userID); err == nil {
		sum += locked
	}
	return sum
}

type walletTxListResp struct {
	Items   []model.WalletTransaction `json:"items"`
	HasMore bool                      `json:"has_more"`
}

func (a *API) listWalletTransactions(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	q := r.URL.Query()
	limit := 30
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	var beforeID int64
	if raw := q.Get("before_id"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			beforeID = n
		}
	}
	kind := strings.TrimSpace(q.Get("kind"))
	items, hasMore, err := a.store.ListWalletTransactions(r.Context(), me.ID, kind, beforeID, limit)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, walletTxListResp{Items: items, HasMore: hasMore})
}

type transferRequest struct {
	ToUserID int64  `json:"to_user_id"`
	Amount   int64  `json:"amount"`
	Note     string `json:"note"`
}

func (a *API) transferCoins(w http.ResponseWriter, r *http.Request) {
	var req transferRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	me := currentUser(r)
	if until := a.coinsLockedUntil(me); until != nil {
		writeError(w, http.StatusForbidden, newAccountMsg("chuyển xu", *until))
		return
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > 120 {
		note = string([]rune(note)[:120])
	}
	senderW, recvW, tx, err := a.store.TransferCoins(r.Context(), me.ID, req.ToUserID, req.Amount, note)
	switch {
	case errors.Is(err, store.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "Số xu chuyển phải lớn hơn 0.")
		return
	case errors.Is(err, store.ErrSelfTransfer):
		writeError(w, http.StatusBadRequest, "Không thể tự chuyển xu cho chính mình.")
		return
	case errors.Is(err, store.ErrRecipientNotFound):
		writeError(w, http.StatusNotFound, "Không tìm thấy người nhận hợp lệ.")
		return
	case errors.Is(err, store.ErrInsufficientBalance):
		writeError(w, http.StatusBadRequest, "Số dư xu không đủ để thực hiện giao dịch.")
		return
	case errors.Is(err, store.ErrTransferInDebt):
		writeError(w, http.StatusBadRequest, "Bạn đang nợ ngân hàng, trả hết nợ rồi mới chuyển xu được.")
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	a.pushWallet(senderW)
	a.pushWallet(recvW)
	writeJSON(w, http.StatusOK, map[string]any{
		"wallet":      a.walletResponse(senderW),
		"transaction": tx,
	})
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
		} else if s.g.BetAmount() > 0 {
			if ws, rErr := a.store.RefundGameBets(ctx, s.messageID, "Kênh đã bị xoá"); rErr == nil {
				for _, w := range ws {
					a.pushWallet(w)
				}
			} else {
				err = rErr
			}
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// settlePendingCashouts: lúc khởi động, trả chip cho các ván bầu cua đã kết thúc (hoặc tin đã xoá)
// mà chưa kịp trả (máy chủ tắt giữa chừng).
func (a *API) settlePendingCashouts(ctx context.Context) {
	rows, err := a.store.GamesNeedingCashout(ctx, "baucua")
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
		g.Abort("Ván kết thúc khi máy chủ khởi động lại.")
		s := &gameSession{messageID: row.MessageID, channelID: row.ChannelID, g: g}
		s.mu.Lock()
		if err := a.payOutLocked(ctx, s, "cashout", 0, g.Settle()); err != nil {
			log.Printf("cashout game %d: %v", row.MessageID, err)
		}
		s.mu.Unlock()
	}
}
