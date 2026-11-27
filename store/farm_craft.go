package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Xưởng chế biến (specs/FARM_SPEC.md §9): biến nông sản và sản phẩm chăn nuôi trong kho thành món
// giá cao hơn (~1,5 lần giá nguyên liệu). Mỗi lò làm một món một lúc; xây xưởng có 2 lò, nâng cấp 4.

// Recipe: món chế biến; món làm ra là Item cùng id (kind "goods", giá Price).
type Recipe struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Emoji   string      `json:"emoji"`
	Inputs  []OrderItem `json:"inputs"`
	TimeSec int64       `json:"time_sec"`
	Price   int64       `json:"price"`
	XP      int64       `json:"xp"`
	Level   int         `json:"level"`
	// Building: công trình có lò làm món này (kitchen | juicer).
	Building string `json:"building"`
}

func ingredients(pairs ...any) []OrderItem {
	out := make([]OrderItem, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, OrderItem{Item: pairs[i].(string), Qty: int64(pairs[i+1].(int))})
	}
	return out
}

// Recipes: danh mục món (cố định), theo cấp mở.
var Recipes = []Recipe{
	{"salad", "Salad rau", "🥗", ingredients("rau_muong", 2, "ca_rot", 1, "ca_chua", 1), 15 * 60, 60, 4, 4, kitchenID},
	{"bap_rang", "Bắp rang", "🍿", ingredients("ngo", 2), 30 * 60, 88, 6, 4, kitchenID},
	{"com_nam", "Cơm nắm", "🍙", ingredients("lua", 2), 45 * 60, 148, 9, 5, kitchenID},
	{"banh_flan", "Bánh flan", "🍮", ingredients("trung", 2, "sua", 1), 60 * 60, 180, 11, 5, kitchenID},
	{"sinh_to", "Sinh tố dâu", "🥤", ingredients("dau_tay", 2, "sua", 1), 90 * 60, 345, 18, 6, kitchenID},
	{"pizza", "Pizza", "🍕", ingredients("lua", 1, "ca_chua", 2, "pho_mai", 1), 2 * 3600, 275, 16, 6, kitchenID},
	{"kem_dua", "Kem dưa hấu", "🍧", ingredients("dua_hau", 1, "sua", 2), 2 * 3600, 425, 22, 7, kitchenID},
	{"ao_len", "Áo len", "🧥", ingredients("len", 3), 3 * 3600, 495, 26, 7, kitchenID},
	{"banh_bi_ngo", "Bánh bí ngô", "🥧", ingredients("bi_ngo", 1, "trung", 2, "sua", 1), 4 * 3600, 500, 30, 8, kitchenID},
	{"sup_nam", "Súp nấm", "🍲", ingredients("nam", 1, "sua", 1, "ca_rot", 2), 3 * 3600, 475, 28, 9, kitchenID},
	{"nuoc_cam", "Nước cam", "🧃", ingredients("cam", 3), 20 * 60, 85, 8, 6, juicerID},
	{"nuoc_tao", "Nước táo", "🍏", ingredients("tao", 3), 30 * 60, 120, 10, 7, juicerID},
	{"sinh_to_chuoi", "Sinh tố chuối", "🧋", ingredients("chuoi", 2, "sua", 1), 40 * 60, 215, 14, 9, juicerID},
	{"nuoc_xoai", "Nước xoài", "🫙", ingredients("xoai", 3), 60 * 60, 215, 18, 11, juicerID},
	{"nuoc_thap_cam", "Nước ép thập cẩm", "🍹", ingredients("cam", 1, "tao", 1, "xoai", 1, "ca_rot", 1), 2 * 3600, 165, 14, 12, juicerID},
	{"nuoc_dua", "Nước dừa", "🌴", ingredients("dua", 2), 45 * 60, 195, 16, 14, juicerID},
	{"nuoc_nho", "Nước nho", "🍷", ingredients("nho", 4), 90 * 60, 540, 32, 18, juicerID},
	// Món Noel (xmasRecipes): chỉ nấu được, chỉ vào đơn hàng khi đang sự kiện Giáng sinh
	{"banh_gung", "Bánh gừng Noel", "🍪", ingredients("lua", 1, "trung", 2, "sua", 1), 45 * 60, 250, 12, 4, kitchenID},
	{"ca_cao", "Ca cao nóng", "☕", ingredients("sua", 2), 30 * 60, 225, 10, 4, kitchenID},
	{"keo_gay", "Kẹo gậy Noel", "🍭", ingredients("dau_tay", 2, "sua", 1), 60 * 60, 340, 16, 5, kitchenID},
}

const (
	kitchenID = "kitchen"
	juicerID  = "juicer"
)

// xmasRecipes: món chỉ có trong sự kiện Giáng sinh (đã nấu xong thì vẫn bán / giao được sau sự kiện).
var xmasRecipes = map[string]bool{"banh_gung": true, "ca_cao": true, "keo_gay": true}

// recipeOpen: món recipeID có đang mở ở ngày day (món Noel chỉ mở trong sự kiện).
func recipeOpen(recipeID, day string) bool { return !xmasRecipes[recipeID] || xmasOn(day) }

var ErrXmasRecipeOff = &FarmError{"Món Noel chỉ nấu được trong sự kiện Giáng sinh."}

func recipeByID(id string) (Recipe, bool) {
	for _, r := range Recipes {
		if r.ID == id {
			return r, true
		}
	}
	return Recipe{}, false
}

var (
	ErrFarmUnknownRecipe = &FarmError{"Không có món này."}
	ErrFarmNoKitchen     = &FarmError{"Cần xây Xưởng chế biến trước."}
	ErrFarmNoJuicer      = &FarmError{"Cần xây Máy ép nước trước."}
	ErrFarmKitchenBusy   = &FarmError{"Các lò đều đang nấu, chờ xong món đang làm nhé."}
	ErrFarmNoJob         = &FarmError{"Lò này đang trống."}
	ErrFarmJobNotReady   = &FarmError{"Món này chưa xong."}
	ErrFarmNothingCraft  = &FarmError{"Chưa có món nào xong để lấy."}
)

// Job: một lò của xưởng đang nấu (working) hoặc đã xong chờ lấy (ready). Lò trống không có Job.
type Job struct {
	Building string `json:"building"`
	Slot     int    `json:"slot"`
	Recipe   string `json:"recipe"`
	ReadyAt  string `json:"ready_at"`
	State    string `json:"state"` // working | ready
}

type jobRow struct {
	Building string
	Slot     int
	Recipe   Recipe
	Ready    int64
}

func loadJobs(ctx context.Context, q queryer, userID int64) ([]jobRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT building, slot, recipe, ready_at FROM farm_jobs WHERE user_id = ? ORDER BY building, slot`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []jobRow
	for rows.Next() {
		var j jobRow
		var id string
		if err := rows.Scan(&j.Building, &j.Slot, &id, &j.Ready); err != nil {
			return nil, err
		}
		var ok bool
		if j.Recipe, ok = recipeByID(id); !ok {
			continue
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// freeSlots: các lò trống của công trình mà món r cần (theo thứ tự lò), sau khi kiểm tra đã xây,
// đủ cấp và còn lò. Lỗi giữ nguyên lời của xưởng; máy ép có lời riêng.
func (op *farmOp) freeSlots(userID int64, r Recipe) ([]int, error) {
	if !recipeOpen(r.ID, op.day) {
		return nil, ErrXmasRecipeOff
	}
	f, err := ensureFarm(op.ctx, op.tx, userID)
	if err != nil {
		return nil, err
	}
	built, err := loadBuildings(op.ctx, op.tx, userID)
	if err != nil {
		return nil, err
	}
	b, _ := buildingByID(r.Building)
	lv := built[r.Building]
	if lv == 0 {
		if r.Building == juicerID {
			return nil, ErrFarmNoJuicer
		}
		return nil, ErrFarmNoKitchen
	}
	if levelFor(f.XP) < r.Level {
		return nil, farmErr("Cần đạt cấp %d mới làm được %s.", r.Level, r.Name)
	}
	jobs, err := loadJobs(op.ctx, op.tx, userID)
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, j := range jobs {
		if j.Building == r.Building {
			used[j.Slot] = true
		}
	}
	var free []int
	for i := range b.capAt(lv) {
		if !used[i] {
			free = append(free, i)
		}
	}
	if len(free) == 0 {
		if r.Building == juicerID {
			return nil, farmErr("Các lò của %s đều đang bận.", b.Name)
		}
		return nil, ErrFarmKitchenBusy
	}
	return free, nil
}

// missingErr: lỗi thiếu nguyên liệu để làm r một lần theo kho stock (nil nếu đủ).
func missingErr(r Recipe, stock map[string]int64) error {
	var miss []string
	for _, it := range r.Inputs {
		if n := it.Qty - stock[it.Item]; n > 0 {
			miss = append(miss, fmt.Sprintf("%d %s", n, strings.ToLower(itemName(it.Item))))
		}
	}
	if len(miss) > 0 {
		return farmErr("Thiếu %s trong kho để làm %s.", strings.Join(miss, ", "), r.Name)
	}
	return nil
}

// startJob: trừ nguyên liệu và bắt đầu món r ở lò slot (đã kiểm tra đủ nguyên liệu).
func (op *farmOp) startJob(userID int64, r Recipe, slot int) error {
	for _, it := range r.Inputs {
		if ok, err := op.takeStock(userID, it.Item, it.Qty); err != nil {
			return err
		} else if !ok {
			return farmErr("Thiếu %s trong kho để làm %s.", strings.ToLower(itemName(it.Item)), r.Name)
		}
	}
	_, err := op.tx.ExecContext(op.ctx, `INSERT INTO farm_jobs (user_id, building, slot, recipe, ready_at) VALUES (?, ?, ?, ?, ?)`,
		userID, r.Building, slot, r.ID, op.unix()+r.TimeSec)
	return err
}

// fillSlots: nấu món recipeID ở mọi lò trống của công trình tương ứng, tới khi hết lò hoặc hết nguyên
// liệu. Trả số lò đã bắt đầu; không bắt đầu được lò nào thì trả lỗi như FarmCraft.
func (op *farmOp) fillSlots(userID int64, recipeID string) (int, error) {
	r, ok := recipeByID(recipeID)
	if !ok {
		return 0, ErrFarmUnknownRecipe
	}
	free, err := op.freeSlots(userID, r)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, slot := range free {
		stock, err := loadStock(op.ctx, op.tx, userID)
		if err != nil {
			return started, err
		}
		if err := missingErr(r, stock); err != nil {
			if started == 0 {
				return 0, err
			}
			break
		}
		if err := op.startJob(userID, r, slot); err != nil {
			return started, err
		}
		started++
	}
	return started, nil
}

// FarmCraft: bắt đầu nấu món recipeID ở lò trống đầu tiên của công trình của món; nguyên liệu trừ khỏi kho ngay.
func (s *Store) FarmCraft(ctx context.Context, userID int64, recipeID string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		r, ok := recipeByID(recipeID)
		if !ok {
			return ErrFarmUnknownRecipe
		}
		free, err := op.freeSlots(userID, r)
		if err != nil {
			return err
		}
		stock, err := loadStock(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if err := missingErr(r, stock); err != nil {
			return err
		}
		return op.startJob(userID, r, free[0])
	})
	return res, err
}

// FarmCraftFill: nấu món recipeID ở mọi lò trống của công trình của món, tới khi hết nguyên liệu.
func (s *Store) FarmCraftFill(ctx context.Context, userID int64, recipeID string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		_, err := op.fillSlots(userID, recipeID)
		return err
	})
	return res, err
}

// finishJobs: lấy các món ready (cắt theo sức chứa kho) vào kho, xoá lò, cộng XP. Kho đầy: ErrFarmStorageFull.
func (op *farmOp) finishJobs(userID int64, ready []jobRow) (map[string]int64, int64, int, error) {
	room, err := op.storageRoom(userID)
	if err != nil {
		return nil, 0, 0, err
	}
	if room < 1 {
		return nil, 0, 0, ErrFarmStorageFull
	}
	ready = ready[:min(int64(len(ready)), room)]
	var xp int64
	items := map[string]int64{}
	for _, j := range ready {
		if _, err := op.tx.ExecContext(op.ctx, `DELETE FROM farm_jobs WHERE user_id = ? AND building = ? AND slot = ?`,
			userID, j.Building, j.Slot); err != nil {
			return nil, 0, 0, err
		}
		if err := op.addStock(userID, j.Recipe.ID, 1); err != nil {
			return nil, 0, 0, err
		}
		items[j.Recipe.ID]++
		xp += j.Recipe.XP
	}
	lv, err := op.addXP(userID, xp)
	if err != nil {
		return nil, 0, 0, err
	}
	return items, xp, lv, nil
}

// collectJobs: lấy mọi món đã xong của công trình building ("" = mọi công trình) vừa kho. Không có gì
// lấy được (chưa xong, hoặc kho đầy) thì trả items rỗng, XP 0. Cộng XP ở đây.
func (op *farmOp) collectJobs(userID int64, building string) (map[string]int64, int64, int, error) {
	jobs, err := loadJobs(op.ctx, op.tx, userID)
	if err != nil {
		return nil, 0, 0, err
	}
	now := op.unix()
	var ready []jobRow
	for _, j := range jobs {
		if j.Ready <= now && (building == "" || j.Building == building) {
			ready = append(ready, j)
		}
	}
	if len(ready) == 0 {
		return map[string]int64{}, 0, 0, nil
	}
	items, xp, lv, err := op.finishJobs(userID, ready)
	if errors.Is(err, ErrFarmStorageFull) {
		return map[string]int64{}, 0, 0, nil
	}
	return items, xp, lv, err
}

// FarmCraftCollect: lấy món đã xong ở lò slot của công trình building (rỗng: xưởng), hoặc mọi lò đã
// xong nếu all (building rỗng: mọi công trình; lần lượt tới khi kho đầy) vào kho, cộng XP của món.
func (s *Store) FarmCraftCollect(ctx context.Context, userID int64, building string, slot int, all bool) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if building != "" && building != kitchenID && building != juicerID {
			return ErrFarmUnknownBuild
		}
		if building == "" && !all {
			building = kitchenID
		}
		now := op.unix()
		jobs, err := loadJobs(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		var ready []jobRow
		for _, j := range jobs {
			if building != "" && j.Building != building {
				continue
			}
			if all && j.Ready <= now {
				ready = append(ready, j)
			} else if !all && j.Slot == slot {
				if j.Ready > now {
					return ErrFarmJobNotReady
				}
				ready = append(ready, j)
			}
		}
		if len(ready) == 0 {
			if all {
				return ErrFarmNothingCraft
			}
			return ErrFarmNoJob
		}
		items, xp, lv, err := op.finishJobs(userID, ready)
		if err != nil {
			return err
		}
		res.Gained = &Gained{XP: xp, LevelUp: lv, Items: items}
		return nil
	})
	return res, err
}

// fillKitchen thêm các lò đang làm (xưởng và máy ép; ai xem cũng thấy, để vẽ khói bếp) vào FarmView và
// danh mục món (chỉ chủ).
func (s *Store) fillKitchen(ctx context.Context, v *FarmView, ownerID, now int64) error {
	jobs, err := loadJobs(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	v.Jobs = make([]Job, len(jobs))
	for i, j := range jobs {
		st := "working"
		if j.Ready <= now {
			st = "ready"
		}
		v.Jobs[i] = Job{Building: j.Building, Slot: j.Slot, Recipe: j.Recipe.ID, ReadyAt: rfc(j.Ready), State: st}
	}
	if v.Me {
		day := vnDay(s.now())
		for _, r := range Recipes {
			if recipeOpen(r.ID, day) {
				v.Recipes = append(v.Recipes, r)
			}
		}
	}
	return nil
}
