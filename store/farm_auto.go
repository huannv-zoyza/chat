package store

import (
	"context"
	"errors"
)

// Tự động: một lần gọi làm hết việc của nông trại (docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md, phần C).

var ErrFarmNothingAuto = &FarmError{"Chưa có việc gì để làm."}

// AutoOpts: cài đặt của một lượt /auto. Crop, KitchenRecipe, JuicerRecipe rỗng thì bỏ qua bước tương
// ứng; BuyFeed: tự mua thức ăn thiếu khi chăm vật nuôi (không thì chỉ dùng thức ăn trong kho).
type AutoOpts struct {
	Crop          string
	KitchenRecipe string
	JuicerRecipe  string
	BuyFeed       bool
}

// AutoReport: những gì một lượt /auto đã làm. Spent: tổng xu đã trả (hạt giống + thức ăn), số dương.
// Skipped: lý do các bước gặp lỗi nghiệp vụ nên bỏ qua.
type AutoReport struct {
	Bugs      int      `json:"bugs"`
	Harvested int      `json:"harvested"`
	Planted   int      `json:"planted"`
	Watered   int      `json:"watered"`
	Fed       int      `json:"fed"`
	Fruits    int      `json:"fruits"`
	Crafted   int      `json:"crafted"`
	Collected int      `json:"collected"`
	Spent     int64    `json:"spent"`
	Skipped   []string `json:"skipped"`
}

func isBusinessErr(err error) bool {
	var fe *FarmError
	return errors.As(err, &fe) || errors.Is(err, ErrInsufficientBalance)
}

func errMsg(err error) string {
	if errors.Is(err, ErrInsufficientBalance) {
		return "Không đủ xu."
	}
	return err.Error()
}

// step chạy fn trong một SAVEPOINT: lỗi thì hoàn lại mọi ghi của bước này. Lỗi nghiệp vụ không thuộc
// silent được ghi vào skipped (kèm nhãn); silent: bước chỉ "không có gì để làm". Lỗi khác (DB) trả về.
func (op *farmOp) step(label string, skipped *[]string, silent []error, fn func() error) error {
	if _, err := op.tx.ExecContext(op.ctx, `SAVEPOINT auto_step`); err != nil {
		return err
	}
	err := fn()
	if err != nil {
		if _, rerr := op.tx.ExecContext(op.ctx, `ROLLBACK TO auto_step`); rerr != nil {
			return rerr
		}
	}
	if _, rerr := op.tx.ExecContext(op.ctx, `RELEASE auto_step`); rerr != nil {
		return rerr
	}
	if err == nil {
		return nil
	}
	if !isBusinessErr(err) {
		return err
	}
	for _, s := range silent {
		if errors.Is(err, s) {
			return nil
		}
	}
	*skipped = append(*skipped, label+": "+errMsg(err))
	return nil
}

func sumItems(m map[string]int64) int {
	var n int64
	for _, q := range m {
		n += q
	}
	return int(n)
}

// add gộp g vào tổng.
func (g *Gained) add(o *Gained) {
	if o == nil {
		return
	}
	g.Xu += o.Xu
	g.XP += o.XP
	g.LevelUp = max(g.LevelUp, o.LevelUp)
	g.Fed += o.Fed
	g.Hungry += o.Hungry
	g.addItems(o.Items)
	for k, v := range o.Bought {
		if g.Bought == nil {
			g.Bought = map[string]int64{}
		}
		g.Bought[k] += v
	}
}

func (g *Gained) addItems(items map[string]int64) {
	for k, v := range items {
		if g.Items == nil {
			g.Items = map[string]int64{}
		}
		g.Items[k] += v
	}
}

// FarmAuto: làm lần lượt mọi việc có thể: bắt sâu, thu hoạch ô chín, gieo Crop, chăm chuồng gà rồi đồng cỏ, hái
// vườn cây, thu món đã xong, lấp lò xưởng và máy ép theo công thức đã chọn. Tất cả trong một giao dịch;
// mỗi bước chạy trong SAVEPOINT nên bước lỗi nghiệp vụ không để lại ghi chép dở và chỉ được ghi vào
// Skipped. Lỗi DB thì dừng cả lượt. Không làm được gì thì ErrFarmNothingAuto.
func (s *Store) FarmAuto(ctx context.Context, userID int64, o AutoOpts) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		g := &Gained{}
		r := &AutoReport{Skipped: []string{}}
		now := op.unix()

		// 1. bắt sâu đã lộ (trước khi thu để ô chín không bị trừ 30%)
		if err := op.step("Bắt sâu", &r.Skipped, []error{ErrFarmNothingBug}, func() error {
			n, err := op.bugAll(userID)
			if err != nil {
				return err
			}
			r.Bugs += n
			return nil
		}); err != nil {
			return err
		}

		// 2. thu hoạch mọi ô chín
		if err := op.step("Thu hoạch", &r.Skipped, nil, func() error {
			plots, err := loadPlots(ctx, op.tx, userID)
			if err != nil {
				return err
			}
			var ripe []plotRow
			for _, p := range plots {
				if p.state(now) == stateRipe {
					ripe = append(ripe, p)
				}
			}
			if len(ripe) == 0 {
				return nil
			}
			hg, n, err := op.harvestPlots(userID, ripe)
			if err != nil {
				return err
			}
			g.add(hg)
			r.Harvested += int(n)
			return nil
		}); err != nil {
			return err
		}

		// 3. gieo Crop
		if o.Crop != "" {
			if err := op.step("Gieo", &r.Skipped, []error{ErrFarmNoEmptyPlot}, func() error {
				n, err := op.plantAll(userID, o.Crop)
				if err != nil {
					return err
				}
				c, _ := cropByID(o.Crop)
				r.Planted += n
				r.Spent += int64(n) * c.Seed
				return nil
			}); err != nil {
				return err
			}
		}

		// 4. tưới mọi ô đang lớn (kể cả ô vừa gieo)
		if err := op.step("Tưới", &r.Skipped, []error{ErrFarmNothingWater}, func() error {
			n, err := op.waterAll(userID)
			if err != nil {
				return err
			}
			r.Watered += n
			return nil
		}); err != nil {
			return err
		}

		// 5. chăm chuồng gà rồi đồng cỏ
		for _, b := range []struct{ id, label string }{{"coop", "Chuồng gà"}, {"pasture", "Đồng cỏ"}} {
			if err := op.step(b.label, &r.Skipped, []error{ErrFarmNothingTend, ErrFarmNoFeedStock}, func() error {
				tg, err := op.tendBuilding(userID, b.id, o.BuyFeed)
				if err != nil {
					return err
				}
				g.add(tg)
				r.Fed += tg.Fed
				r.Collected += sumItems(tg.Items)
				r.Spent -= tg.Xu
				return nil
			}); err != nil {
				return err
			}
		}

		// 6. hái cả vườn cây
		if err := op.step("Vườn cây", &r.Skipped, nil, func() error {
			items, xp, lv, err := op.harvestTrees(userID, now)
			if err != nil {
				return err
			}
			g.XP += xp
			g.LevelUp = max(g.LevelUp, lv)
			g.addItems(items)
			r.Fruits += sumItems(items)
			return nil
		}); err != nil {
			return err
		}

		// 7. thu mọi món đã xong của xưởng và máy ép
		if err := op.step("Thu món", &r.Skipped, nil, func() error {
			items, xp, lv, err := op.collectJobs(userID, "")
			if err != nil {
				return err
			}
			g.XP += xp
			g.LevelUp = max(g.LevelUp, lv)
			g.addItems(items)
			r.Collected += sumItems(items)
			return nil
		}); err != nil {
			return err
		}

		// 8. lấp lò theo công thức đã chọn
		for _, f := range []struct{ recipe, building, label string }{
			{o.KitchenRecipe, kitchenID, "Xưởng"}, {o.JuicerRecipe, juicerID, "Máy ép"}} {
			if f.recipe == "" {
				continue
			}
			if err := op.step(f.label, &r.Skipped, nil, func() error {
				if rc, ok := recipeByID(f.recipe); ok && rc.Building != f.building {
					return farmErr("Món %s không làm ở đây.", rc.Name)
				}
				n, err := op.fillSlots(userID, f.recipe)
				if err != nil {
					return err
				}
				r.Crafted += n
				return nil
			}); err != nil {
				return err
			}
		}

		if r.Bugs+r.Harvested+r.Planted+r.Watered+r.Fed+r.Fruits+r.Crafted+r.Collected == 0 {
			return ErrFarmNothingAuto
		}
		g.Planted = r.Planted
		g.Watered = r.Watered
		g.Bugs = r.Bugs
		g.Xu = -r.Spent
		g.Auto = r
		res.Gained = g
		return nil
	})
	return res, err
}
