package controller

import (
	"errors"
	"net/http"
	"time"

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
	return 0
}
