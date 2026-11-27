package store

import "testing"

// Chỗ mặc định của máy ép nước hợp lệ trên nông trại trống, không đè vườn cây, và cùng tồn tại
// được với xưởng ở chỗ mặc định.
func TestFarmJuicerDefaultSpot(t *testing.T) {
	b, ok := buildingByID(juicerID)
	if !ok || !b.movable() || b.Size != (Size{2, 2}) || b.Price != 800 || b.Level != 6 || b.Cap != 2 || b.UpgradeCap != 4 || b.UpgradePrice != 1600 {
		t.Fatalf("juicer def: %+v ok=%v", b, ok)
	}
	if err := checkSpot(b, b.Default, map[string]Spot{}); err != nil {
		t.Fatalf("default spot invalid on empty farm: %v", err)
	}
	orchard := Rect{15.5, 2.5, 6.5, 4.8}
	if b.rectAt(b.Default).overlaps(orchard) {
		t.Fatal("juicer default spot overlaps the orchard")
	}
	k, _ := buildingByID(kitchenID)
	if err := checkSpot(b, b.Default, map[string]Spot{kitchenID: k.Default}); err != nil {
		t.Fatalf("juicer collides with kitchen default: %v", err)
	}
	if err := checkSpot(k, k.Default, map[string]Spot{juicerID: b.Default}); err != nil {
		t.Fatalf("kitchen collides with juicer default: %v", err)
	}
}

func TestFarmJuiceNoArbitrageInternal(t *testing.T) {
	juice, pure := 0, 0
	for _, r := range Recipes {
		var buy int64
		allBuyable := true
		for _, in := range r.Inputs {
			it, found := itemByID(in.Item)
			if !found {
				t.Fatalf("%s: unknown ingredient %s", r.ID, in.Item)
			}
			if it.BuyPrice <= 0 {
				allBuyable = false
			}
			buy += it.BuyPrice * in.Qty
		}
		out, found := itemByID(r.ID)
		if !found || out.Kind != "goods" {
			t.Fatalf("recipe %s: item %+v", r.ID, out)
		}
		if r.Building == juicerID {
			juice++
			if allBuyable {
				t.Fatalf("%s: juice must need at least one non-buyable (own-produced) ingredient", r.ID)
			}
		}
		if !allBuyable {
			continue
		}
		pure++
		for lv := 1; lv <= 60; lv++ {
			pct := int64(100 + farmPerks(lv).SellPct)
			if got := sellValue(out, 1, pct); got > buy {
				t.Fatalf("%s level %d: sells %d > ingredient buy cost %d", r.ID, lv, got, buy)
			}
		}
	}
	if juice != 7 {
		t.Fatalf("looped %d juice recipes, want 7", juice)
	}
	if pure == 0 {
		t.Fatal("no all-buyable recipe checked")
	}
}

func TestFarmOrdersJuiceGating(t *testing.T) {
	s := &Store{}
	n := 0
	s.SetRand(func() float64 { n++; return float64(n%97) / 97 })
	fruits := map[string]bool{"cam": true, "tao": true, "chuoi": true, "xoai": true, "dua": true, "nho": true}
	count := func(built map[string]int) (kitchen, juice int) {
		for range 600 {
			items, _, _ := s.newOrder("2026-11-24", 30, built, fruits)
			goods := 0
			for _, it := range items {
				r, ok := recipeByID(it.Item)
				if !ok {
					continue
				}
				goods++
				if r.Building == juicerID {
					juice++
				} else {
					kitchen++
				}
			}
			if goods > 1 {
				t.Fatalf("order has %d goods", goods)
			}
		}
		return
	}
	if k, j := count(map[string]int{}); k != 0 || j != 0 {
		t.Fatalf("nothing built: kitchen %d juice %d", k, j)
	}
	if k, j := count(map[string]int{kitchenID: 1}); k == 0 || j != 0 {
		t.Fatalf("kitchen only: kitchen %d juice %d", k, j)
	}
	if k, j := count(map[string]int{juicerID: 1}); k != 0 || j == 0 {
		t.Fatalf("juicer only: kitchen %d juice %d", k, j)
	}
	if k, j := count(map[string]int{kitchenID: 1, juicerID: 1}); k == 0 || j == 0 {
		t.Fatalf("both: kitchen %d juice %d", k, j)
	}
}
