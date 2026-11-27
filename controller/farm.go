package controller

import (
	"encoding/json"
	"errors"
	"io"
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
//	POST /api/farm/harvest    {plot} |//	POST /api/farm/craft/collect {building?, slot} | {all: true, building?}  (building mặc định: xưởng khi có slot)
//	POST /api/farm/clear      {plot}
//	POST /api/farm/water      {user_id, plot}
//	POST /api/farm/bug        {user_id, plot}
//	POST /api/farm/steal      {user_id, plot}
//	POST /api/farm/expand                  (bí danh cũ: /api/farm/buy-plot)
//	POST /api/farm/buy-dog
//	POST /api/farm/quests/{questId}/claim  ("chest" = rương thưởng ngày)
//	POST /api/farm/sell       {item, qty} |//	POST /api/farm/craft/collect {building?, slot} | {all: true, building?}  (building mặc định: xưởng khi có slot)
//	POST /api/farm/storage/upgrade
//	POST /api/farm/buildings/{id}          xây coop | pasture | kitchen   {x, y} tuỳ chọn: chỗ đặt
//	POST /api/farm/buildings/{id}/move     {x, y}  dời công trình tự do (xưởng)
//	POST /api/farm/buildings/{id}/upgrade
//	POST /api/farm/buildings/{id}/tend     chăm nhanh cả chuồng (coop | pasture): thu + cho ăn, tự mua thức ăn thiếu
//	POST /api/farm/animals    {kind}       mua con vật
//	POST /api/farm/animals/feed    {id} |//	POST /api/farm/craft/collect {building?, slot} | {all: true, building?}  (building mặc định: xưởng khi có slot)
//	POST /api/farm/animals/collect {id} |//	POST /api/farm/craft/collect {building?, slot} | {all: true, building?}  (building mặc định: xưởng khi có slot)
//	POST /api/farm/craft      {recipe, fill?} nấu một món ở lò trống của công trình của món (fill: mọi lò trống)
//	POST /api/farm/craft/collect {building?, slot} | {all: true, building?}  (building mặc định: xưởng khi có slot)| {all: true}
//	POST /api/farm/orchard/plant   {spot, tree}       trồng cây ăn quả vào chỗ trống
//	POST /api/farm/orchard/harvest {spot} |//	POST /api/farm/craft/collect {building?, slot} | {all: true, building?}  (building mặc định: xưởng khi có slot)  hái quả
//	DELETE /api/farm/orchard/{spot}                   chặt cây
//	POST /api/farm/couple/propose  {user_id}          ngỏ lời kết đôi
//	POST /api/farm/couple/respond  {user_id, accept}  trả lời lời ngỏ
//	DELETE /api/farm/couple/propose                   rút lại lời ngỏ của mình
//	DELETE /api/farm/couple                           chia tay
//	POST /api/farm/couple/gift     {item, qty}        tặng nông sản cho người thương
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
	notify := res.OwnerID
	if res.Notify != 0 {
		notify = res.Notify
	}
	if res.Log != nil && notify != me {
		a.hub.ToUser(notify, mustJSON(farmUpdatedEvent{Type: "farm.updated", Log: res.Log}))
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

func (a *API) farmBuyItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item string `json:"item"`
		Qty  int64  `json:"qty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmBuyItem(r.Context(), currentUser(r).ID, req.Item, req.Qty)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmUpgradeStorage(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmUpgradeStorage(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

// farmBuild: thân {x, y} (tuỳ chọn) là chỗ đặt công trình tự do; không gửi thì đặt chỗ mặc định.
func (a *API) farmBuild(w http.ResponseWriter, r *http.Request) {
	var req struct {
		X *int `json:"x"`
		Y *int `json:"y"`
	}
	if r.ContentLength != 0 && r.Body != nil {
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "Dữ liệu gửi lên không hợp lệ.")
			return
		}
	}
	var at *store.Spot
	if req.X != nil && req.Y != nil {
		at = &store.Spot{X: *req.X, Y: *req.Y}
	}
	res, err := a.store.FarmBuildAt(r.Context(), currentUser(r).ID, r.PathValue("id"), at)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmMoveBuilding(w http.ResponseWriter, r *http.Request) {
	var req store.Spot
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmMoveBuilding(r.Context(), currentUser(r).ID, r.PathValue("id"), req)
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

func (a *API) farmTend(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmTend(r.Context(), currentUser(r).ID, r.PathValue("id"))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmCraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Recipe string `json:"recipe"`
		Fill   bool   `json:"fill"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	craft := a.store.FarmCraft
	if req.Fill {
		craft = a.store.FarmCraftFill
	}
	res, err := craft(r.Context(), currentUser(r).ID, req.Recipe)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmCraftCollect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Building string `json:"building"` // rỗng: xưởng khi lấy một lò, mọi công trình khi all
		Slot     int    `json:"slot"`
		All      bool   `json:"all"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmCraftCollect(r.Context(), currentUser(r).ID, req.Building, req.Slot, req.All)
	a.farmRespond(w, r, res, err)
}

// Kết đôi (store/farm_couple.go).
func (a *API) farmPropose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64 `json:"user_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmPropose(r.Context(), currentUser(r).ID, req.UserID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmCoupleRespond(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64 `json:"user_id"`
		Accept bool  `json:"accept"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmCoupleRespond(r.Context(), currentUser(r).ID, req.UserID, req.Accept)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmCancelPropose(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmCancelPropose(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmBreakUp(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmBreakUp(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmGift(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item string `json:"item"`
		Qty  int64  `json:"qty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmGift(r.Context(), currentUser(r).ID, req.Item, req.Qty)
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

type farmTreeReq struct {
	Spot *int   `json:"spot"`
	Tree string `json:"tree"`
	All  bool   `json:"all"`
}

func (a *API) farmPlantTree(w http.ResponseWriter, r *http.Request) {
	var req farmTreeReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Spot == nil {
		writeError(w, http.StatusBadRequest, "Thiếu chỗ trồng cây.")
		return
	}
	res, err := a.store.FarmPlantTree(r.Context(), currentUser(r).ID, *req.Spot, req.Tree)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmHarvestTree(w http.ResponseWriter, r *http.Request) {
	var req farmTreeReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Spot == nil && !req.All {
		writeError(w, http.StatusBadRequest, "Thiếu chỗ trồng cây.")
		return
	}
	spot := 0
	if req.Spot != nil {
		spot = *req.Spot
	}
	res, err := a.store.FarmHarvestTree(r.Context(), currentUser(r).ID, spot, req.All)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmRemoveTree(w http.ResponseWriter, r *http.Request) {
	spot, err := strconv.Atoi(r.PathValue("spot"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Chỗ trồng cây không hợp lệ.")
		return
	}
	res, err := a.store.FarmRemoveTree(r.Context(), currentUser(r).ID, spot)
	a.farmRespond(w, r, res, err)
}
