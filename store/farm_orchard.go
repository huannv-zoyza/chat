package store

import (
	"context"
)

// Vườn cây ăn quả (docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md, phần A):
// cây trồng một lần, hái nhiều lần. Cùng khuôn với chăn nuôi: mỗi hành động là một farmTx.

// OrchardMaxSpots: số chỗ trồng cây tối đa (lưới 4×3).
const OrchardMaxSpots = 12

type FruitTree struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Emoji      string `json:"emoji"`
	Fruit      string `json:"fruit"` // id quả (Item kind "fruit")
	FruitName  string `json:"fruit_name"`
	FruitEmoji string `json:"fruit_emoji"`
	Price      int64  `json:"price"` // giá cây giống
	Level      int    `json:"level"`
	MatureSec  int64  `json:"mature_sec"` // từ lúc trồng tới lần ra quả đầu
	CycleSec   int64  `json:"cycle_sec"`  // giữa hai lần hái
	FruitPrice int64  `json:"fruit_price"`
	XP         int64  `json:"xp"` // mỗi lần hái
}

// FruitTrees: danh mục cây ăn quả (cố định).
var FruitTrees = []FruitTree{
	{"cam", "Cây cam", "🌳", "cam", "Cam", "🍊", 300, 5, 2 * 3600, 3600, 18, 4},
	{"tao", "Cây táo", "🌳", "tao", "Táo", "🍎", 500, 7, 3 * 3600, 5400, 26, 6},
	{"chuoi", "Cây chuối", "🌳", "chuoi", "Chuối", "🍌", 700, 9, 4 * 3600, 2 * 3600, 34, 8},
	{"xoai", "Cây xoài", "🌳", "xoai", "Xoài", "🥭", 1000, 11, 6 * 3600, 3 * 3600, 48, 11},
	{"dua", "Cây dừa", "🌳", "dua", "Dừa", "🥥", 1500, 14, 8 * 3600, 4 * 3600, 65, 15},
	{"nho", "Cây nho", "🌳", "nho", "Nho", "🍇", 2200, 18, 10 * 3600, 5 * 3600, 90, 20},
}

func fruitTreeByID(id string) (FruitTree, bool) {
	for _, t := range FruitTrees {
		if t.ID == id {
			return t, true
		}
	}
	return FruitTree{}, false
}

var (
	ErrFarmSpotLocked   = &FarmError{"Chỗ này chưa mở, lên cấp để mở thêm chỗ trồng cây."}
	ErrFarmSpotUsed     = &FarmError{"Chỗ này đã có cây."}
	ErrFarmNoTree       = &FarmError{"Chỗ này chưa có cây."}
	ErrFarmTreeNotReady = &FarmError{"Cây chưa có quả."}
	ErrFarmNoFruit      = &FarmError{"Chưa có cây nào có quả để hái."}
)

// orchardSpots: số chỗ trồng cây đã mở ở cấp level (dưới cấp 5: vườn khoá).
func orchardSpots(level int) int {
	switch {
	case level >= 16:
		return 12
	case level >= 12:
		return 9
	case level >= 8:
		return 6
	case level >= 5:
		return 3
	}
	return 0
}

const (
	treeGrowing  = "growing"
	treeFruiting = "fruiting"
	treeReady    = "ready"
)

type treeRow struct {
	ID     int64
	Spot   int
	Kind   FruitTree
	Ready  int64
	Cycles int64
}

func (t *treeRow) state(now int64) string {
	switch {
	case now >= t.Ready:
		return treeReady
	case t.Cycles == 0:
		return treeGrowing
	default:
		return treeFruiting
	}
}

// loadTrees: cây của userID theo thứ tự chỗ.
func loadTrees(ctx context.Context, q queryer, userID int64) ([]treeRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, spot, tree, ready_at, cycles FROM farm_trees WHERE user_id = ? ORDER BY spot`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []treeRow
	for rows.Next() {
		var t treeRow
		var kind string
		if err := rows.Scan(&t.ID, &t.Spot, &kind, &t.Ready, &t.Cycles); err != nil {
			return nil, err
		}
		var ok bool
		if t.Kind, ok = fruitTreeByID(kind); !ok {
			continue
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// treeFruitSet: id các quả mà userID đang có cây (để mở khoá quả trong đơn hàng).
func treeFruitSet(trees []treeRow) map[string]bool {
	set := map[string]bool{}
	for _, t := range trees {
		set[t.Kind.Fruit] = true
	}
	return set
}

// FarmPlantTree: trồng cây treeID vào chỗ trống spot. Cần chỗ đã mở, đủ cấp và đủ xu. Cây ra quả lần
// đầu sau MatureSec, rút ngắn theo cấp lúc trồng (grow_pct).
func (s *Store) FarmPlantTree(ctx context.Context, userID int64, spot int, treeID string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		k, ok := fruitTreeByID(treeID)
		if !ok {
			return farmErr("Không có loại cây này.")
		}
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		lv := levelFor(f.XP)
		if spot < 0 || spot >= OrchardMaxSpots || spot >= orchardSpots(lv) {
			return ErrFarmSpotLocked
		}
		if lv < k.Level {
			return farmErr("Cần đạt cấp %d mới trồng được %s.", k.Level, k.FruitName)
		}
		var used int
		if err := op.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_trees WHERE user_id = ? AND spot = ?`,
			userID, spot).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return ErrFarmSpotUsed
		}
		if err := op.spend(userID, k.Price, "Mua cây giống "+k.FruitName); err != nil {
			return err
		}
		now := op.unix()
		ready := now + k.MatureSec*int64(100-farmPerks(lv).GrowPct)/100
		_, err = op.tx.ExecContext(ctx,
			`INSERT INTO farm_trees (user_id, spot, tree, planted_at, ready_at) VALUES (?, ?, ?, ?, ?)`,
			userID, spot, k.ID, now, ready)
		return err
	})
	return res, err
}

// treeYield: số quả mỗi lần hái của chủ ở cấp level.
func treeYield(level int) int64 { return 3 + farmPerks(level).HarvestBonus }

// harvestRows hái các cây trong rows (người gọi đã kiểm tra chín và sức chứa kho): quả vào kho, cây
// bắt đầu chu kỳ mới (CycleSec rút ngắn theo cấp), cộng XP.
func (op *farmOp) harvestRows(userID int64, rows []*treeRow, level int) (map[string]int64, int64, error) {
	items := map[string]int64{}
	var xp int64
	now := op.unix()
	yield := treeYield(level)
	for _, t := range rows {
		t.Ready = now + t.Kind.CycleSec*int64(100-farmPerks(level).GrowPct)/100
		t.Cycles++
		if _, err := op.tx.ExecContext(op.ctx, `UPDATE farm_trees SET ready_at = ?, cycles = cycles + 1 WHERE id = ?`,
			t.Ready, t.ID); err != nil {
			return nil, 0, err
		}
		if err := op.addStock(userID, t.Kind.Fruit, yield); err != nil {
			return nil, 0, err
		}
		items[t.Kind.Fruit] += yield
		xp += t.Kind.XP
	}
	return items, xp, nil
}

// harvestTrees hái mọi cây đã chín mà cả lượt quả còn vừa kho, theo thứ tự chỗ (cây nào không vừa thì
// dừng, vì mọi cây cho cùng số quả). Không có cây nào hái được thì trả items rỗng, XP 0. Cộng XP ở đây.
func (op *farmOp) harvestTrees(userID int64, now int64) (map[string]int64, int64, int, error) {
	f, err := ensureFarm(op.ctx, op.tx, userID)
	if err != nil {
		return nil, 0, 0, err
	}
	lv := levelFor(f.XP)
	trees, err := loadTrees(op.ctx, op.tx, userID)
	if err != nil {
		return nil, 0, 0, err
	}
	room, err := op.storageRoom(userID)
	if err != nil {
		return nil, 0, 0, err
	}
	var pick []*treeRow
	yield := treeYield(lv)
	for i := range trees {
		if trees[i].state(now) != treeReady || room < yield {
			continue
		}
		room -= yield
		pick = append(pick, &trees[i])
	}
	if len(pick) == 0 {
		return map[string]int64{}, 0, 0, nil
	}
	items, xp, err := op.harvestRows(userID, pick, lv)
	if err != nil {
		return nil, 0, 0, err
	}
	lv, err = op.addXP(userID, xp)
	if err != nil {
		return nil, 0, 0, err
	}
	return items, xp, lv, nil
}

// FarmHarvestTree: hái cây ở chỗ spot, hoặc mọi cây đã chín mà còn vừa kho nếu all. Mỗi cây cho
// 3 + harvest_bonus quả vào kho; cả lượt phải vừa kho, không thì báo kho đầy.
func (s *Store) FarmHarvestTree(ctx context.Context, userID int64, spot int, all bool) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		now := op.unix()
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		lv := levelFor(f.XP)
		trees, err := loadTrees(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		var ready []*treeRow
		for i := range trees {
			t := &trees[i]
			if all && t.state(now) == treeReady {
				ready = append(ready, t)
			} else if !all && t.Spot == spot {
				if t.state(now) != treeReady {
					return ErrFarmTreeNotReady
				}
				ready = append(ready, t)
			}
		}
		if len(ready) == 0 {
			if all {
				return ErrFarmNoFruit
			}
			return ErrFarmNoTree
		}
		room, err := op.storageRoom(userID)
		if err != nil {
			return err
		}
		yield := treeYield(lv)
		n := min(int64(len(ready)), room/yield)
		if n < 1 {
			return ErrFarmStorageFull
		}
		items, xp, err := op.harvestRows(userID, ready[:n], lv)
		if err != nil {
			return err
		}
		up, err := op.addXP(userID, xp)
		if err != nil {
			return err
		}
		res.Gained = &Gained{XP: xp, LevelUp: up, Items: items}
		return nil
	})
	return res, err
}

// FarmRemoveTree: chặt cây ở chỗ spot (không hoàn tiền) để trồng loại khác.
func (s *Store) FarmRemoveTree(ctx context.Context, userID int64, spot int) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		r, err := op.tx.ExecContext(ctx, `DELETE FROM farm_trees WHERE user_id = ? AND spot = ?`, userID, spot)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil || n == 0 {
			if err != nil {
				return err
			}
			return ErrFarmNoTree
		}
		return nil
	})
	return res, err
}

// fillOrchard thêm vườn cây vào FarmView: trees ai xem cũng thấy, tree_kinds chỉ chủ.
func (s *Store) fillOrchard(ctx context.Context, v *FarmView, ownerID int64, f farmRow, now int64) error {
	trees, err := loadTrees(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	v.Trees = make([]TreeView, len(trees))
	for i := range trees {
		t := &trees[i]
		v.Trees[i] = TreeView{Spot: t.Spot, Tree: t.Kind.ID, State: t.state(now), ReadyAt: rfc(t.Ready)}
	}
	v.OrchardSpots = orchardSpots(levelFor(f.XP))
	if v.Me {
		v.TreeKinds = FruitTrees
	}
	return nil
}
