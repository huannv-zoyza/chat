package controller

import (
	"errors"
	"net/http"
	"strconv"

	"chat/store"
)

// Nông trại (luật chơi ở store/farm.go, đặc tả ở specs/FARM_SPEC.md).
//
//	GET  /api/farm                         nông trại của mình (kèm ví, nhiệm vụ, nhật ký)
//	GET  /api/farm/users/{userId}          nông trại người khác (tính nhiệm vụ "visit")
//	GET  /api/farm/neighbors               hàng xóm: mọi người dùng thật trừ mình, bot, AI
//	GET  /api/farm/leaderboard             top 20 theo XP
//	POST /api/farm/plant      {plot, crop}
//	POST /api/farm/harvest    {plot} | {all: true}
//	POST /api/farm/clear      {plot}
//	POST /api/farm/water      {user_id, plot}
//	POST /api/farm/bug        {user_id, plot}
//	POST /api/farm/steal      {user_id, plot}
//	POST /api/farm/expand                  (bí danh cũ: /api/farm/buy-plot)
//	POST /api/farm/buy-dog
//	POST /api/farm/quests/{questId}/claim  ("chest" = rương thưởng ngày)
//	POST /api/farm/sell       {item, qty} | {all: true}
//	POST /api/farm/storage/upgrade
//	POST /api/farm/buildings/{id}          xây coop | pasture
//	POST /api/farm/buildings/{id}/upgrade
//	POST /api/farm/animals    {kind}       mua con vật
//	POST /api/farm/animals/feed    {id} | {all: true}
//	POST /api/farm/animals/collect {id} | {all: true}
//	POST /api/farm/orders/{id}/deliver
//	DELETE /api/farm/orders/{id}           huỷ đơn
//
// Hành động trả FarmView của nông trại vừa tác động (kèm "gained" / "dog" nếu có). Ví đổi thì người đó
// nhận wallet.updated; người khác tưới / bắt sâu / trộm / bị chó đuổi thì chủ nhận farm.updated {log}.

type farmUpdatedEvent struct {
	Type string          `json:"type"`
	Log  *store.LogEntry `json:"log"`
}

func (a *API) farmError(w http.ResponseWriter, r *http.Request, err error) {
	var fe *store.FarmError
	switch {
	case errors.As(err, &fe):
		writeError(w, http.StatusBadRequest, fe.Msg)
	case errors.Is(err, store.ErrInsufficientBalance):
		writeError(w, http.StatusBadRequest, "Không đủ xu.")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Không tìm thấy nông trại.")
	default:
		internalError(w, r, err)
	}
}

func (a *API) getFarm(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r).ID
	v, err := a.store.GetFarm(r.Context(), me, me)
	if err != nil {
		a.farmError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) getUserFarm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "Không tìm thấy nông trại.")
		return
	}
	v, err := a.store.FarmVisit(r.Context(), currentUser(r).ID, id)
	if err != nil {
		a.farmError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (a *API) farmNeighbors(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.FarmNeighbors(r.Context(), currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) farmLeaderboard(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.FarmLeaderboard(r.Context())
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// farmRespond: sau một hành động, đẩy ví / sự kiện rồi trả FarmView của nông trại vừa tác động.
func (a *API) farmRespond(w http.ResponseWriter, r *http.Request, res store.FarmResult, err error) {
	if err != nil {
		a.farmError(w, r, err)
		return
	}
	me := currentUser(r).ID
	for _, wal := range res.Wallets {
		a.pushWallet(wal)
	}
	if res.Log != nil && res.OwnerID != me {
		a.hub.ToUser(res.OwnerID, mustJSON(farmUpdatedEvent{Type: "farm.updated", Log: res.Log}))
	}
	v, err := a.store.GetFarm(r.Context(), res.OwnerID, me)
	if err != nil {
		internalError(w, r, err)
		return
	}
	v.Gained, v.Dog = res.Gained, res.Dog
	writeJSON(w, http.StatusOK, v)
}

type farmPlotReq struct {
	UserID int64  `json:"user_id"`
	Plot   *int   `json:"plot"`
	Crop   string `json:"crop"`
	All    bool   `json:"all"`
}

// decodePlotReq đọc body; cần plot (trừ khi allowAll và all=true).
func decodePlotReq(w http.ResponseWriter, r *http.Request, allowAll bool) (farmPlotReq, bool) {
	var req farmPlotReq
	if !decodeJSON(w, r, &req) {
		return req, false
	}
	if req.Plot == nil && !(allowAll && req.All) {
		writeError(w, http.StatusBadRequest, "Ô đất không hợp lệ.")
		return req, false
	}
	if req.Plot == nil {
		req.Plot = new(int)
	}
	return req, true
}

func (a *API) farmPlant(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePlotReq(w, r, false)
	if !ok {
		return
	}
	res, err := a.store.FarmPlant(r.Context(), currentUser(r).ID, *req.Plot, req.Crop)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmHarvest(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePlotReq(w, r, true)
	if !ok {
		return
	}
	res, err := a.store.FarmHarvest(r.Context(), currentUser(r).ID, *req.Plot, req.All)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmClear(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePlotReq(w, r, false)
	if !ok {
		return
	}
	res, err := a.store.FarmClear(r.Context(), currentUser(r).ID, *req.Plot)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmWater(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePlotReq(w, r, false)
	if !ok {
		return
	}
	me := currentUser(r).ID
	res, err := a.store.FarmWater(r.Context(), me, ownerOrMe(req.UserID, me), *req.Plot)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmBug(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePlotReq(w, r, false)
	if !ok {
		return
	}
	me := currentUser(r).ID
	res, err := a.store.FarmBug(r.Context(), me, ownerOrMe(req.UserID, me), *req.Plot)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmSteal(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePlotReq(w, r, false)
	if !ok {
		return
	}
	me := currentUser(r).ID
	res, err := a.store.FarmSteal(r.Context(), me, ownerOrMe(req.UserID, me), *req.Plot)
	a.farmRespond(w, r, res, err)
}

func ownerOrMe(owner, me int64) int64 {
	if owner <= 0 {
		return me
	}
	return owner
}

func (a *API) farmExpand(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmExpand(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmBuyDog(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmBuyDog(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmClaimQuest(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmClaimQuest(r.Context(), currentUser(r).ID, r.PathValue("questId"))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmSell(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item string `json:"item"`
		Qty  int64  `json:"qty"`
		All  bool   `json:"all"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmSell(r.Context(), currentUser(r).ID, req.Item, req.Qty, req.All)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmUpgradeStorage(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmUpgradeStorage(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmBuild(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmBuild(r.Context(), currentUser(r).ID, r.PathValue("id"))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmUpgradeBuilding(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmUpgradeBuilding(r.Context(), currentUser(r).ID, r.PathValue("id"))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmBuyAnimal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmBuyAnimal(r.Context(), currentUser(r).ID, req.Kind)
	a.farmRespond(w, r, res, err)
}

type farmAnimalReq struct {
	ID  int64 `json:"id"`
	All bool  `json:"all"`
}

func (a *API) farmFeed(w http.ResponseWriter, r *http.Request) {
	var req farmAnimalReq
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmFeed(r.Context(), currentUser(r).ID, req.ID, req.All)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmCollect(w http.ResponseWriter, r *http.Request) {
	var req farmAnimalReq
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmCollect(r.Context(), currentUser(r).ID, req.ID, req.All)
	a.farmRespond(w, r, res, err)
}

// orderID: id đơn trên đường dẫn (không hợp lệ thì 0, store báo "Không tìm thấy đơn hàng này.").
func orderID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (a *API) farmDeliverOrder(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmDeliverOrder(r.Context(), currentUser(r).ID, orderID(r))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmDiscardOrder(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmDiscardOrder(r.Context(), currentUser(r).ID, orderID(r))
	a.farmRespond(w, r, res, err)
}
