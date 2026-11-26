package store

import (
	"context"
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
	{"salad", "Salad rau", "🥗", ingredients("rau_muong", 2, "ca_rot", 1, "ca_chua", 1), 15 * 60, 60, 4, 4},
	{"bap_rang", "Bắp rang", "🍿", ingredients("ngo", 2), 30 * 60, 88, 6, 4},
	{"com_nam", "Cơm nắm", "🍙", ingredients("lua", 2), 45 * 60, 148, 9, 5},
	{"banh_flan", "Bánh flan", "🍮", ingredients("trung", 2, "sua", 1), 60 * 60, 180, 11, 5},
	{"sinh_to", "Sinh tố dâu", "🥤", ingredients("dau_tay", 2, "sua", 1), 90 * 60, 345, 18, 6},
	{"pizza", "Pizza", "🍕", ingredients("lua", 1, "ca_chua", 2, "pho_mai", 1), 2 * 3600, 275, 16, 6},
	{"kem_dua", "Kem dưa hấu", "🍧", ingredients("dua_hau", 1, "sua", 2), 2 * 3600, 425, 22, 7},
	{"ao_len", "Áo len", "🧥", ingredients("len", 3), 3 * 3600, 495, 26, 7},
	{"banh_bi_ngo", "Bánh bí ngô", "🥧", ingredients("bi_ngo", 1, "trung", 2, "sua", 1), 4 * 3600, 500, 30, 8},
	{"sup_nam", "Súp nấm", "🍲", ingredients("nam", 1, "sua", 1, "ca_rot", 2), 3 * 3600, 475, 28, 9},
}

const kitchenID = "kitchen"

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
	ErrFarmKitchenBusy   = &FarmError{"Các lò đều đang nấu, chờ xong món đang làm nhé."}
	ErrFarmNoJob         = &FarmError{"Lò này đang trống."}
	ErrFarmJobNotReady   = &FarmError{"Món này chưa xong."}
	ErrFarmNothingCraft  = &FarmError{"Chưa có món nào xong để lấy."}
)

// Job: một lò của xưởng đang nấu (working) hoặc đã xong chờ lấy (ready). Lò trống không có Job.
type Job struct {
	Slot    int    `json:"slot"`
	Recipe  string `json:"recipe"`
	ReadyAt string `json:"ready_at"`
	State   string `json:"state"` // working | ready
}

type jobRow struct {
	Slot   int
	Recipe Recipe
	Ready  int64
}

func loadJobs(ctx context.Context, q queryer, userID int64) ([]jobRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT slot, recipe, ready_at FROM farm_jobs WHERE user_id = ? ORDER BY slot`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []jobRow
	for rows.Next() {
		var j jobRow
		var id string
		if err := rows.Scan(&j.Slot, &id, &j.Ready); err != nil {
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

// FarmCraft: bắt đầu nấu món recipeID ở lò trống đầu tiên; nguyên liệu trừ khỏi kho ngay.
func (s *Store) FarmCraft(ctx context.Context, userID int64, recipeID string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		r, ok := recipeByID(recipeID)
		if !ok {
			return ErrFarmUnknownRecipe
		}
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		built, err := loadBuildings(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		lv := built[kitchenID]
		if lv == 0 {
			return ErrFarmNoKitchen
		}
		if levelFor(f.XP) < r.Level {
			return farmErr("Cần đạt cấp %d mới làm được %s.", r.Level, r.Name)
		}
		jobs, err := loadJobs(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		used := map[int]bool{}
		for _, j := range jobs {
			used[j.Slot] = true
		}
		b, _ := buildingByID(kitchenID)
		slot := -1
		for i := range b.capAt(lv) {
			if !used[i] {
				slot = i
				break
			}
		}
		if slot < 0 {
			return ErrFarmKitchenBusy
		}
		stock, err := loadStock(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		var miss []string
		for _, it := range r.Inputs {
			if n := it.Qty - stock[it.Item]; n > 0 {
				miss = append(miss, fmt.Sprintf("%d %s", n, strings.ToLower(itemName(it.Item))))
			}
		}
		if len(miss) > 0 {
			return farmErr("Thiếu %s trong kho để làm %s.", strings.Join(miss, ", "), r.Name)
		}
		for _, it := range r.Inputs {
			if ok, err := op.takeStock(userID, it.Item, it.Qty); err != nil {
				return err
			} else if !ok {
				return farmErr("Thiếu %s trong kho để làm %s.", strings.ToLower(itemName(it.Item)), r.Name)
			}
		}
		_, err = op.tx.ExecContext(ctx, `INSERT INTO farm_jobs (user_id, slot, recipe, ready_at) VALUES (?, ?, ?, ?)`,
			userID, slot, r.ID, op.unix()+r.TimeSec)
		return err
	})
	return res, err
}

// FarmCraftCollect: lấy món đã xong ở lò slot (hoặc mọi lò đã xong nếu all, lần lượt tới khi kho đầy)
// vào kho, cộng XP của món.
func (s *Store) FarmCraftCollect(ctx context.Context, userID int64, slot int, all bool) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		now := op.unix()
		jobs, err := loadJobs(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		var ready []jobRow
		for _, j := range jobs {
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
		room, err := op.storageRoom(userID)
		if err != nil {
			return err
		}
		if room < 1 {
			return ErrFarmStorageFull
		}
		ready = ready[:min(int64(len(ready)), room)]
		var xp int64
		items := map[string]int64{}
		for _, j := range ready {
			if _, err := op.tx.ExecContext(ctx, `DELETE FROM farm_jobs WHERE user_id = ? AND slot = ?`, userID, j.Slot); err != nil {
				return err
			}
			if err := op.addStock(userID, j.Recipe.ID, 1); err != nil {
				return err
			}
			items[j.Recipe.ID]++
			xp += j.Recipe.XP
		}
		lv, err := op.addXP(userID, xp)
		if err != nil {
			return err
		}
		res.Gained = &Gained{XP: xp, LevelUp: lv, Items: items}
		return nil
	})
	return res, err
}

// fillKitchen thêm lò đang nấu vào FarmView (ai xem cũng thấy, để vẽ khói bếp) và danh mục món (chỉ chủ).
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
		v.Jobs[i] = Job{Slot: j.Slot, Recipe: j.Recipe.ID, ReadyAt: rfc(j.Ready), State: st}
	}
	if v.Me {
		v.Recipes = Recipes
	}
	return nil
}
