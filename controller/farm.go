package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
//	POST /api/farm/harvest    {plot} | {all: true}
//	POST /api/farm/clear      {plot}
//	POST /api/farm/water      {user_id, plot}
//	POST /api/farm/bug        {user_id, plot} | {all:true}
//	POST /api/farm/steal      {user_id, plot}
//	POST /api/farm/expand                  (bí danh cũ: /api/farm/buy-plot)
//	POST /api/farm/buy-dog
//	POST /api/farm/quests/{questId}/claim  ("chest" = rương thưởng ngày)
//	POST /api/farm/sell       {item, qty} | {all: true}
//	POST /api/farm/storage/upgrade
//	POST /api/farm/buildings/{id}          xây coop | pasture | kitchen | juicer   {x, y} tuỳ chọn: chỗ đặt
//	POST /api/farm/buildings/{id}/move     {x, y}  dời công trình tự do (xưởng)
//	POST /api/farm/buildings/{id}/upgrade
//	POST /api/farm/buildings/{id}/tend     chăm nhanh cả chuồng (coop | pasture): thu + cho ăn, tự mua thức ăn thiếu
//	POST /api/farm/animals    {kind}       mua con vật
//	POST /api/farm/animals/feed    {id} | {all: true}
//	POST /api/farm/animals/collect {id} | {all: true}
//	POST /api/farm/craft      {recipe, fill?} nấu một món ở lò trống của công trình của món (fill: mọi lò trống)
//	POST /api/farm/craft/collect {building?, slot} | {all: true, building?}  (building mặc định: xưởng khi có slot)
//	POST /api/farm/plant           {plot, crop} | {all: true, crop}  gieo một ô / mọi ô trống (dọn ô héo, tới khi hết xu)
//	POST /api/farm/auto            {crop?, kitchen_recipe?, juicer_recipe?, buy_feed}  làm hết mọi việc một lần
//	POST /api/farm/orchard/plant   {spot, tree}       trồng cây ăn quả vào chỗ trống
//	POST /api/farm/orchard/harvest {spot} | {all: true}  hái quả
//	DELETE /api/farm/orchard/{spot}                   chặt cây
//	POST /api/farm/couple/propose  {user_id}          ngỏ lời kết đôi
//	POST /api/farm/couple/respond  {user_id, accept}  trả lời lời ngỏ
//	DELETE /api/farm/couple/propose                   rút lại lời ngỏ của mình
//	DELETE /api/farm/couple                           chia tay
//	POST /api/farm/couple/gift     {item, qty}        tặng nông sản cho người thương
//	POST /api/farm/orders/{id}/deliver
//	DELETE /api/farm/orders/{id}           huỷ đơn
//	POST /api/farm/market          {item, qty, price}  treo hàng trong kho lên chợ đen (giá cả lô)
//	POST /api/farm/market/{id}/buy                    mua cả lô
//	DELETE /api/farm/market/{id}                      gỡ lô của mình, hàng về kho
//	POST /api/farm/xmas/orders/{slot}/deliver         giao đơn Ông già Noel (Giáng sinh, store/farm_xmas.go)
//	POST /api/farm/xmas/shop/{id}                     đổi Kẹo Noel lấy quà
//	POST /api/farm/xmas/sock       {user_id, item, qty} treo tất Noel ở vườn người khác
//	POST /api/farm/xmas/socks/open                    mở mọi tất đã treo từ hôm trước ở vườn mình
//	POST /api/farm/xmas/snowball   {user_id}          ném cầu tuyết vào vườn người khác
//	POST /api/farm/xmas/drops/{id}/claim              nhặt quà Ông già Noel (ai bấm trước được)
//	POST /api/farm/xmas/box                           mở hộp quà bí ẩn (đổi kẹo)
//	POST /api/farm/xmas/tree       {item, qty}        góp hàng vào cây thông chung toàn server
//	POST /api/farm/xmas/tree/{goal}/claim             nhận thưởng mốc cây thông chung
//	GET  /api/farm/xmas/top                           top kẹo (huy hiệu 🎄 trong chat)
//	POST /api/farm/xmas/daily-gift                    mở quà dưới gốc cây thông (mỗi ngày một lần)
//	POST /api/farm/xmas/santa/join                    đăng ký Ông già Noel bí mật (trước ngày bốc thăm)
//	POST /api/farm/xmas/santa/send {item, qty}        gửi quà bí mật cho người mình bốc được
//	POST /api/farm/xmas/santa/open                    mở quà bí mật (từ đêm Noel)
//	POST /api/farm/xmas/milestones/{n}/claim          nhận quà mốc kẹo cá nhân
//	POST /api/farm/xmas/eve-gift                      nhận quà đêm Noel
//	GET  /api/farm/xmas/inbox?since=N                 tin Noel đã lỡ (tin chung + tin riêng, 2 ngày gần nhất)
//
// Tin sự kiện (nhặt quà, trúng đồ hiếm, cây chung lên mốc, quà vừa thả) bắn farm.xmas {text} tới mọi người.
//
// Hành động trả FarmView của nông trại vừa tác động (kèm "gained" / "dog" nếu có). Ví đổi thì người đó
// nhận wallet.updated; người khác tưới / bắt sâu / trộm / bị chó đuổi thì chủ nhận farm.updated {log}.
// Chợ đen đổi (treo / mua / gỡ) thì mọi người nhận farm.market để tải lại.

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
	if res.Market {
		a.hub.ToAll(mustJSON(map[string]string{"type": "farm.market"}))
	}
	if res.News != "" {
		// người làm và người nhận tin riêng không cần thấy tin chung nữa
		except := []int64{me}
		for _, p := range res.Pokes {
			except = append(except, p.UserID)
		}
		a.farmXmasNews(res.News, except...)
	} else if res.Sync {
		a.hub.ToAll(mustJSON(map[string]string{"type": "farm.xmas"})) // không text: chỉ tải lại
	}
	for _, p := range res.Pokes {
		id, err := a.store.AddXmasNews(r.Context(), p.UserID, p.Kind, p.Text, p.From, nil)
		if err != nil {
			log.Printf("xmas poke: %v", err)
		}
		a.hub.ToUser(p.UserID, mustJSON(struct {
			Type string `json:"type"`
			store.XmasPoke
			ID int64 `json:"id,omitempty"`
		}{"farm.xmas_me", p, id}))
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
	req, ok := decodePlotReq(w, r, true)
	if !ok {
		return
	}
	if req.All {
		res, err := a.store.FarmPlantAll(r.Context(), currentUser(r).ID, req.Crop)
		a.farmRespond(w, r, res, err)
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
	req, ok := decodePlotReq(w, r, true)
	if !ok {
		return
	}
	me := currentUser(r).ID
	if req.All {
		if req.UserID > 0 && req.UserID != me {
			writeError(w, http.StatusBadRequest, store.ErrFarmWaterAllOwn.Error())
			return
		}
		res, err := a.store.FarmWaterAll(r.Context(), me)
		a.farmRespond(w, r, res, err)
		return
	}
	res, err := a.store.FarmWater(r.Context(), me, ownerOrMe(req.UserID, me), *req.Plot)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmBug(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePlotReq(w, r, true)
	if !ok {
		return
	}
	me := currentUser(r).ID
	if req.All {
		if req.UserID > 0 && req.UserID != me {
			writeError(w, http.StatusBadRequest, store.ErrFarmBugAllOwn.Error())
			return
		}
		res, err := a.store.FarmBugAll(r.Context(), me)
		a.farmRespond(w, r, res, err)
		return
	}
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

func (a *API) farmAuto(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Crop          string `json:"crop"`
		KitchenRecipe string `json:"kitchen_recipe"`
		JuicerRecipe  string `json:"juicer_recipe"`
		BuyFeed       bool   `json:"buy_feed"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmAuto(r.Context(), currentUser(r).ID, store.AutoOpts{
		Crop: req.Crop, KitchenRecipe: req.KitchenRecipe, JuicerRecipe: req.JuicerRecipe, BuyFeed: req.BuyFeed})
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

// orderID: id {id} trên đường dẫn (đơn hàng, lô chợ đen); không hợp lệ thì 0, store báo không tìm thấy.
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

// Chợ đen (store/farm_market.go).
func (a *API) farmMarketList(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item  string `json:"item"`
		Qty   int64  `json:"qty"`
		Price int64  `json:"price"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmMarketList(r.Context(), currentUser(r).ID, req.Item, req.Qty, req.Price)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmMarketBuy(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmMarketBuy(r.Context(), currentUser(r).ID, orderID(r))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmMarketCancel(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmMarketCancel(r.Context(), currentUser(r).ID, orderID(r))
	a.farmRespond(w, r, res, err)
}

// Giáng sinh (store/farm_xmas.go).
func (a *API) farmXmasDeliver(w http.ResponseWriter, r *http.Request) {
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil {
		slot = -1 // store báo không tìm thấy đơn
	}
	res, err := a.store.FarmXmasDeliver(r.Context(), currentUser(r).ID, slot)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasBuy(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasBuy(r.Context(), currentUser(r).ID, r.PathValue("id"))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasSock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64  `json:"user_id"`
		Item   string `json:"item"`
		Qty    int64  `json:"qty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmXmasSock(r.Context(), currentUser(r).ID, req.UserID, req.Item, req.Qty)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasOpenSocks(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasOpenSocks(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

// farmXmasNews báo cả server tin Giáng sinh; except: id những người client tự bỏ qua (đã có tin riêng).
func (a *API) farmXmasNews(text string, except ...int64) {
	// lưu lại để người đang offline vào sau vẫn xem được (lỗi lưu không chặn việc báo)
	id, err := a.store.AddXmasNews(context.Background(), 0, "news", text, 0, except)
	if err != nil {
		log.Printf("xmas news: %v", err)
	}
	a.hub.ToAll(mustJSON(struct {
		Type   string  `json:"type"`
		Text   string  `json:"text"`
		Except []int64 `json:"except,omitempty"`
		ID     int64   `json:"id,omitempty"`
	}{"farm.xmas", text, except, id}))
}

func (a *API) farmXmasSnowball(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64 `json:"user_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmXmasSnowball(r.Context(), currentUser(r).ID, req.UserID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasClaimDrop(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasClaimDrop(r.Context(), currentUser(r).ID, orderID(r))
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasBox(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasBox(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasTree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item string `json:"item"`
		Qty  int64  `json:"qty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmXmasTreeDonate(r.Context(), currentUser(r).ID, req.Item, req.Qty)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasTreeClaim(w http.ResponseWriter, r *http.Request) {
	goal, _ := strconv.Atoi(r.PathValue("goal")) // sai thì 0, store báo chưa đạt mốc
	res, err := a.store.FarmXmasTreeClaim(r.Context(), currentUser(r).ID, goal)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasDailyGift(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasDailyGift(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

// farmXmasInbox: tin Noel từ sau id since (?since=N; thiếu hoặc âm: chỉ trả last để đánh dấu đã xem).
func (a *API) farmXmasInbox(w http.ResponseWriter, r *http.Request) {
	since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if err != nil {
		since = -1
	}
	items, last, err := a.store.XmasInbox(r.Context(), currentUser(r).ID, since)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "last": last})
}

func (a *API) farmXmasTop(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.FarmXmasTop(r.Context())
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) farmXmasSantaJoin(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasSantaJoin(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasSantaSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Item string `json:"item"`
		Qty  int64  `json:"qty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.store.FarmXmasSantaSend(r.Context(), currentUser(r).ID, req.Item, req.Qty)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasSantaOpen(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasSantaOpen(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasMilestone(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.PathValue("n")) // sai thì 0, store báo chưa đạt mốc
	res, err := a.store.FarmXmasMilestone(r.Context(), currentUser(r).ID, n)
	a.farmRespond(w, r, res, err)
}

func (a *API) farmXmasEveGift(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.FarmXmasEveGift(r.Context(), currentUser(r).ID)
	a.farmRespond(w, r, res, err)
}

// farmXmasTick (bộ hẹn giờ): thả quà Ông già Noel tới lượt, bốc thăm Ông già Noel bí mật, báo đêm Noel.
func (a *API) farmXmasTick(ctx context.Context) {
	news, err := a.store.FarmXmasFinaleTick(ctx)
	if err != nil {
		log.Printf("scheduler xmas finale: %v", err)
	}
	for _, n := range news {
		a.farmXmasNews(n)
	}
	d, err := a.store.FarmXmasDropTick(ctx)
	if err != nil {
		log.Printf("scheduler xmas drop: %v", err)
		return
	}
	if d != nil {
		a.farmXmasNews(fmt.Sprintf("🎅 Ông già Noel vừa thả quà ở vườn %s! Ai nhanh tay thì nhặt được (10 phút).",
			store.UserLabel(d.Owner)))
	}
}
