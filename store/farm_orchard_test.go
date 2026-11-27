package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"chat/database"
	"chat/store"
)

// Vườn cây ăn quả (specs: docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md, phần A).

func (e *farmEnv) plantTree(id int64, spot int, tree string) {
	e.t.Helper()
	if _, err := e.st.FarmPlantTree(e.ctx, id, spot, tree); err != nil {
		e.t.Fatalf("plant tree %s at %d: %v", tree, spot, err)
	}
}

func treeAt(v store.FarmView, spot int) (store.TreeView, bool) {
	for _, t := range v.Trees {
		if t.Spot == spot {
			return t, true
		}
	}
	return store.TreeView{}, false
}

func TestOrchardSpotsByLevel(t *testing.T) {
	e := newFarmEnv(t)
	u := e.user("alice")
	for lv, want := range map[int]int{4: 0, 5: 3, 7: 3, 8: 6, 12: 9, 16: 12, 40: 12} {
		e.setXP(u.ID, levelXPForTest(lv))
		if got := e.view(u.ID, u.ID).OrchardSpots; got != want {
			t.Fatalf("level %d: spots = %d, want %d", lv, got, want)
		}
	}
	e.setXP(u.ID, levelXPForTest(5))
	e.plantTree(u.ID, 2, "cam")
	_, err := e.st.FarmPlantTree(e.ctx, u.ID, 3, "cam")
	wantErr(t, err, store.ErrFarmSpotLocked)
	for _, bad := range []int{-1, 12} {
		_, err = e.st.FarmPlantTree(e.ctx, u.ID, bad, "cam")
		wantErr(t, err, store.ErrFarmSpotLocked)
	}
	if e.balance(u.ID) != 1000-300 {
		t.Fatalf("balance %d", e.balance(u.ID))
	}
}

// levelXPForTest: XP tối thiểu của cấp lv (cấp 1..10 theo bảng, sau đó tính theo store.LevelXPFor).
func levelXPForTest(lv int) int64 { return store.LevelXPForTest(lv) }

func TestOrchardPlantHarvest(t *testing.T) {
	e := newFarmEnv(t)
	u := e.user("alice")
	e.setXP(u.ID, 280) // cấp 5: grow_pct 8, harvest_bonus 1
	e.plantTree(u.ID, 0, "cam")
	if e.balance(u.ID) != 1000-300 {
		t.Fatalf("balance %d", e.balance(u.ID))
	}
	v := e.view(u.ID, u.ID)
	tv, ok := treeAt(v, 0)
	if !ok || tv.Tree != "cam" || tv.State != "growing" {
		t.Fatalf("tree: %+v", tv)
	}
	if len(v.TreeKinds) != len(store.FruitTrees) || len(store.FruitTrees) != 6 {
		t.Fatalf("tree kinds: %d", len(v.TreeKinds))
	}
	mature := 2 * time.Hour * 92 / 100
	e.advance(mature - time.Second)
	_, err := e.st.FarmHarvestTree(e.ctx, u.ID, 0, false)
	wantErr(t, err, store.ErrFarmTreeNotReady)
	e.advance(time.Second)
	if tv, _ := treeAt(e.view(u.ID, u.ID), 0); tv.State != "ready" {
		t.Fatalf("tree: %+v", tv)
	}
	res, err := e.st.FarmHarvestTree(e.ctx, u.ID, 0, false)
	if err != nil || res.Gained == nil || res.Gained.Items["cam"] != 4 || res.Gained.XP != 4 {
		t.Fatalf("harvest: %v %+v", err, res.Gained)
	}
	v = e.view(u.ID, u.ID)
	tv, _ = treeAt(v, 0)
	if tv.State != "fruiting" || v.XP != 284 || v.Storage.Stock["cam"] != 4 {
		t.Fatalf("after harvest: %+v xp %d stock %v", tv, v.XP, v.Storage.Stock)
	}
	want := e.now.Add(time.Hour * 92 / 100).UTC().Format(time.RFC3339)
	if tv.ReadyAt != want {
		t.Fatalf("ready_at %s, want %s", tv.ReadyAt, want)
	}
	_, err = e.st.FarmHarvestTree(e.ctx, u.ID, 0, false)
	wantErr(t, err, store.ErrFarmTreeNotReady)

	// chỗ đã có cây, cây chưa đủ cấp, cây không có
	_, err = e.st.FarmPlantTree(e.ctx, u.ID, 0, "cam")
	wantErr(t, err, store.ErrFarmSpotUsed)
	_, err = e.st.FarmPlantTree(e.ctx, u.ID, 1, "tao")
	wantMsg(t, err, "Cần đạt cấp 7 mới trồng được Táo.")
	_, err = e.st.FarmPlantTree(e.ctx, u.ID, 1, "xyz")
	wantMsg(t, err, "Không có loại cây này.")
	_, err = e.st.FarmHarvestTree(e.ctx, u.ID, 1, false)
	wantErr(t, err, store.ErrFarmNoTree)
	if e.balance(u.ID) != 700 {
		t.Fatalf("failed plants charged: %d", e.balance(u.ID))
	}
}

func TestOrchardHarvestAllStorage(t *testing.T) {
	e := newFarmEnv(t)
	u := e.user("alice")
	e.setXP(u.ID, 280)
	_, err := e.st.FarmHarvestTree(e.ctx, u.ID, 0, true)
	wantErr(t, err, store.ErrFarmNoFruit)
	for i := range 3 {
		e.plantTree(u.ID, i, "cam")
	}
	_, err = e.st.FarmHarvestTree(e.ctx, u.ID, 0, true) // chưa chín
	wantErr(t, err, store.ErrFarmNoFruit)
	e.advance(3 * time.Hour)

	e.setStock(u.ID, "ca_rot", 55) // còn chỗ 5, một cây cho 4
	res, err := e.st.FarmHarvestTree(e.ctx, u.ID, 0, true)
	if err != nil || res.Gained.Items["cam"] != 4 || res.Gained.XP != 4 {
		t.Fatalf("partial: %v %+v", err, res.Gained)
	}
	v := e.view(u.ID, u.ID)
	if a, _ := treeAt(v, 0); a.State != "fruiting" {
		t.Fatalf("tree 0: %+v", a)
	}
	for _, s := range []int{1, 2} {
		if a, _ := treeAt(v, s); a.State != "ready" {
			t.Fatalf("tree %d: %+v", s, a)
		}
	}
	// còn chỗ 1: không vừa một cây
	_, err = e.st.FarmHarvestTree(e.ctx, u.ID, 0, true)
	wantErr(t, err, store.ErrFarmStorageFull)
	_, err = e.st.FarmHarvestTree(e.ctx, u.ID, 1, false)
	wantErr(t, err, store.ErrFarmStorageFull)
	e.setStock(u.ID, "ca_rot", 56)
	_, err = e.st.FarmHarvestTree(e.ctx, u.ID, 0, true)
	wantErr(t, err, store.ErrFarmStorageFull)

	// hái một cây cụ thể
	e.setStock(u.ID, "ca_rot", 0)
	res, err = e.st.FarmHarvestTree(e.ctx, u.ID, 2, false)
	if err != nil || res.Gained.Items["cam"] != 4 {
		t.Fatalf("single: %v %+v", err, res.Gained)
	}
	if a, _ := treeAt(e.view(u.ID, u.ID), 1); a.State != "ready" {
		t.Fatalf("tree 1: %+v", a)
	}
}

func TestOrchardRemove(t *testing.T) {
	e := newFarmEnv(t)
	u := e.user("alice")
	e.setXP(u.ID, 280)
	e.plantTree(u.ID, 1, "cam")
	if _, err := e.st.FarmRemoveTree(e.ctx, u.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := treeAt(e.view(u.ID, u.ID), 1); ok || e.balance(u.ID) != 700 {
		t.Fatalf("after remove: balance %d", e.balance(u.ID))
	}
	_, err := e.st.FarmRemoveTree(e.ctx, u.ID, 1)
	wantErr(t, err, store.ErrFarmNoTree)
	e.plantTree(u.ID, 1, "cam") // trồng lại được
}

func TestOrchardVisitorView(t *testing.T) {
	e := newFarmEnv(t)
	a, b := e.user("alice"), e.user("bob")
	e.setXP(a.ID, 280)
	e.plantTree(a.ID, 0, "cam")
	v := e.view(a.ID, b.ID)
	if len(v.Trees) != 1 || v.TreeKinds != nil || v.OrchardSpots != 3 {
		t.Fatalf("visitor view: %+v", v)
	}
	if v := e.view(b.ID, b.ID); v.Trees == nil || len(v.Trees) != 0 {
		t.Fatalf("empty trees must be [] not nil: %#v", v.Trees)
	}
}

func TestOrchardFruitSellAndItems(t *testing.T) {
	e := newFarmEnv(t)
	u := e.user("alice")
	e.setXP(u.ID, 2480) // cấp 11: sell_pct 20
	e.setStock(u.ID, "cam", 3)
	before := e.balance(u.ID)
	if _, err := e.st.FarmSell(e.ctx, u.ID, "cam", 3, false); err != nil {
		t.Fatal(err)
	}
	if got := e.balance(u.ID) - before; got != 18*3*120/100 {
		t.Fatalf("fruit sold for %d", got)
	}
	var fruits int
	for _, it := range store.Items {
		if it.Kind == "fruit" {
			fruits++
			if it.BuyPrice != 0 {
				t.Fatalf("fruit buyable: %+v", it)
			}
		}
	}
	if fruits != 6 {
		t.Fatalf("fruit items: %d", fruits)
	}
	_, err := e.st.FarmBuyItem(e.ctx, u.ID, "cam", 1)
	if err == nil {
		t.Fatal("fruit must not be buyable")
	}
}

// Migration 000032 (farm_trees) lùi rồi tiến lại được.
func TestOrchardMigrationUpDown(t *testing.T) {
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	migrate := func(mode database.Mode, steps int) {
		t.Helper()
		if err := database.Migrate(database.MigrationOpt{DB: db, Mode: mode, Steps: steps}); err != nil {
			t.Fatal(err)
		}
	}
	applied := func() bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = 'farm_orchard'`).Scan(&n)
		return n > 0
	}
	migrate(database.ModeUp, 0)
	if !applied() {
		t.Fatal("farm_orchard not applied")
	}
	for applied() {
		migrate(database.ModeDown, 1)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'farm_trees'`).Scan(&n)
	if n != 0 {
		t.Fatal("farm_trees still exists after down")
	}
	migrate(database.ModeUp, 0)
	if !applied() {
		t.Fatal("farm_orchard not re-applied")
	}
}
