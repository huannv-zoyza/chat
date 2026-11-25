package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

func (a *API) pushWalletUpdate(u store.WalletUpdate) {
	txCopy := u.Transaction
	a.hub.ToUser(u.UserID, mustJSON(walletUpdatedEvent{
		Type:        "wallet.updated",
		Wallet:      u.Wallet,
		Transaction: &txCopy,
	}))
}

func (a *API) pushWalletUpdates(updates []store.WalletUpdate) {
	for _, u := range updates {
		a.pushWalletUpdate(u)
	}
}

/* ---------- GET /api/wallet ---------- */

func (a *API) getWallet(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	wallet, err := a.store.GetWallet(r.Context(), me.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

/* ---------- GET /api/wallet/transactions ---------- */

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
	txType := strings.TrimSpace(q.Get("type"))
	items, hasMore, err := a.store.ListWalletTransactions(r.Context(), me.ID, txType, beforeID, limit)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, walletTxListResp{Items: items, HasMore: hasMore})
}

/* ---------- POST /api/wallet/checkin ---------- */

type walletActionResp struct {
	Wallet      model.Wallet            `json:"wallet"`
	Transaction model.WalletTransaction `json:"transaction"`
}

func (a *API) claimCheckin(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	wallet, tx, err := a.store.ClaimDailyCheckin(r.Context(), me.ID, time.Now())
	if errors.Is(err, store.ErrCheckinAlreadyClaimed) {
		writeError(w, http.StatusBadRequest, "Hôm nay bạn đã điểm danh nhận Xu rồi. Hãy quay lại vào ngày mai!")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.pushWalletUpdate(store.WalletUpdate{UserID: me.ID, Wallet: wallet, Transaction: tx})
	writeJSON(w, http.StatusOK, walletActionResp{Wallet: wallet, Transaction: tx})
}

/* ---------- POST /api/wallet/bailout ---------- */

func (a *API) claimBailout(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	wallet, tx, err := a.store.ClaimBailout(r.Context(), me.ID, time.Now())
	switch {
	case errors.Is(err, store.ErrBailoutNotEligible):
		writeError(w, http.StatusBadRequest, "Bạn chỉ nhận được cứu trợ khi tổng Xu dưới 500.")
		return
	case errors.Is(err, store.ErrBailoutLimitReached):
		writeError(w, http.StatusBadRequest, "Bạn đã dùng hết lượt cứu trợ trong ngày hôm nay (tối đa 3 lần/ngày).")
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	a.pushWalletUpdate(store.WalletUpdate{UserID: me.ID, Wallet: wallet, Transaction: tx})
	writeJSON(w, http.StatusOK, walletActionResp{Wallet: wallet, Transaction: tx})
}

/* ---------- POST /api/wallet/transfer ---------- */

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
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > 120 {
		note = string([]rune(note)[:120])
	}
	senderUpd, recvUpd, err := a.store.TransferCoins(r.Context(), me.ID, req.ToUserID, req.Amount, note)
	switch {
	case errors.Is(err, store.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "Số Xu chuyển phải lớn hơn 0.")
		return
	case errors.Is(err, store.ErrSelfTransfer):
		writeError(w, http.StatusBadRequest, "Không thể tự chuyển Xu cho chính mình.")
		return
	case errors.Is(err, store.ErrRecipientNotFound):
		writeError(w, http.StatusNotFound, "Không tìm thấy người nhận hợp lệ.")
		return
	case errors.Is(err, store.ErrInsufficientBalance):
		writeError(w, http.StatusBadRequest, "Số dư Xu khả dụng không đủ để thực hiện giao dịch.")
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	a.pushWalletUpdate(senderUpd)
	a.pushWalletUpdate(recvUpd)
	writeJSON(w, http.StatusOK, walletActionResp{Wallet: senderUpd.Wallet, Transaction: senderUpd.Transaction})
}
