package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"chat/database"
	"chat/store"
)

// Phần 2 (specs/FARM_SPEC.md §4–8): kho, chăn nuôi, đơn hàng, mở rộng đất.

func (e *farmEnv) setXP(id, xp int64) {
	e.t.Helper()
	if err := e.st.FarmSetXPForTest(e.ctx, id, xp); err != nil {
		e.t.Fatal(err)
	}
}

func (e *farmEnv) setStock(id int64, item string, qty int64) {
	e.t.Helper()
	if err := e.st.FarmSetStockForTest(e.ctx, id, item, qty); err != nil {
		e.t.Fatal(err)
	}
}

func (e *farmEnv) storage(id int64) store.StorageView {
	e.t.Helper()
	return *e.view(id, id).Storage
}

func (e *farmEnv) counter(id int64, key string) int64 {
	e.t.Helper()
	n, err := e.st.FarmCounterForTest(e.ctx, id, key)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func wantMsg(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil || err.Error() != msg {
		t.Fatalf("err = %v, want %q", err, msg)
	}
}

// Vụ có sâu và bị trộm 2 lần: 2 − 2 = 0 nhưng chủ vẫn còn tối thiểu 1.
func TestFarmHarvestUnitsMinimum(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob, carol := e.user("alice"), e.user("bob"), e.user("carol")
	e.setRoll(0.1)
	e.plant(alice.ID, 0, "ca_rot")
	e.setRoll(0.9)
	e.advance(15 * time.Minute)
	for _, th := range []int64{bob.ID, carol.ID} {
		if _, err := e.st.FarmSteal(e.ctx, th, alice.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.Stolen != 2 || p.Steals != 2 {
		t.Fatalf("plot: %+v", p)
	}
	res, err := e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	if err != nil || res.Gained.Items["ca_rot"] != 1 || res.Gained.XP != 4 {
		t.Fatalf("harvest: %v %+v", err, res.Gained)
	}
	if e.storage(bob.ID).Stock["ca_rot"] != 1 || e.storage(carol.ID).Stock["ca_rot"] != 1 {
		t.Fatal("thieves did not get a unit each")
	}
}

func TestFarmStorageCap(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	if st := e.storage(alice.ID); st.Cap != 60 || st.Used != 0 || st.NextUpgrade == nil || st.NextUpgrade.Price != 300 ||
		st.Stock == nil || len(st.Stock) != 0 {
		t.Fatalf("fresh storage: %+v", st)
	}
	for i := range 3 {
		e.plant(alice.ID, i, "rau_muong")
	}
	e.plant(bob.ID, 0, "rau_muong")
	e.advance(5 * time.Minute)

	// 58/60: một vụ (3) không vừa
	e.setStock(alice.ID, "ca_rot", 58)
	_, err := e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	wantErr(t, err, store.ErrFarmStorageFull)
	wantMsg(t, err, "Kho đầy, bán bớt hoặc nâng cấp kho.")
	_, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, true)
	wantErr(t, err, store.ErrFarmStorageFull)
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.State != "ripe" {
		t.Fatalf("blocked harvest removed crop: %+v", p)
	}
	// 54/60: harvest all chỉ thu 2 ô vừa kho, ô còn lại để nguyên
	e.setStock(alice.ID, "ca_rot", 54)
	res, err := e.st.FarmHarvest(e.ctx, alice.ID, 0, true)
	if err != nil || res.Gained.Items["rau_muong"] != 6 || res.Gained.XP != 4 {
		t.Fatalf("partial harvest all: %v %+v", err, res.Gained)
	}
	if v := e.view(alice.ID, alice.ID); v.Storage.Used != 60 || v.Plots[2].State != "ripe" {
		t.Fatalf("after partial: %+v %+v", v.Storage, v.Plots[2])
	}
	// kẻ trộm kho đầy: không trộm được, không mất lượt
	_, err = e.st.FarmSteal(e.ctx, alice.ID, bob.ID, 0)
	wantErr(t, err, store.ErrFarmStorageFull)
	if v := e.view(bob.ID, alice.ID); v.StealsLeft != 5 || !v.Plots[0].CanSteal {
		t.Fatalf("failed steal consumed a try: %+v", v)
	}

	// nâng cấp kho: +30, hết lượt nâng thì báo
	if _, err := e.st.FarmUpgradeStorage(e.ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if st := e.storage(alice.ID); st.Cap != 90 || st.NextUpgrade.Price != 600 || e.balance(alice.ID) != 1000-30-300 {
		t.Fatalf("after upgrade: %+v balance %d", st, e.balance(alice.ID))
	}
	if res, err := e.st.FarmHarvest(e.ctx, alice.ID, 2, false); err != nil || res.Gained.Items["rau_muong"] != 3 {
		t.Fatalf("harvest after upgrade: %v %+v", err, res)
	}
	if _, err := e.st.FarmSteal(e.ctx, alice.ID, bob.ID, 0); err != nil {
		t.Fatalf("steal after upgrade: %v", err)
	}
}

func TestFarmStorageMax(t *testing.T) {
	e := newFarmEnv(t)
	rich, donors := e.user("rich"), []int64{}
	for i := range 7 {
		d := e.user(fmt.Sprintf("donor%d", i))
		donors = append(donors, d.ID)
		e.st.TransferCoins(e.ctx, d.ID, rich.ID, 1000, "")
	}
	for range 5 {
		if _, err := e.st.FarmUpgradeStorage(e.ctx, rich.ID); err != nil {
			t.Fatal(err)
		}
	}
	_, err := e.st.FarmUpgradeStorage(e.ctx, rich.ID)
	wantErr(t, err, store.ErrFarmStorageMax)
	wantMsg(t, err, "Kho đã nâng cấp tối đa.")
	if st := e.storage(rich.ID); st.Cap != 210 || st.NextUpgrade != nil || e.balance(rich.ID) != 8000-5900 {
		t.Fatalf("max storage: %+v balance %d", st, e.balance(rich.ID))
	}
}

func TestFarmSell(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setStock(alice.ID, "ca_rot", 5) // 10 / đơn vị
	e.setStock(alice.ID, "trung", 2)  // 22
	e.setStock(alice.ID, "bi_ngo", 1) // ceil(640 / 3) = 214
	_, err := e.st.FarmSell(e.ctx, alice.ID, "xoai", 1, false)
	wantErr(t, err, store.ErrFarmUnknownItem)
	_, err = e.st.FarmSell(e.ctx, alice.ID, "ca_rot", 0, false)
	wantErr(t, err, store.ErrFarmBadQty)
	_, err = e.st.FarmSell(e.ctx, alice.ID, "ca_rot", 6, false)
	wantMsg(t, err, "Trong kho không đủ 6 Cà rốt để bán.")
	_, err = e.st.FarmSell(e.ctx, alice.ID, "lua", 1, false)
	wantFarmErr(t, err)

	res, err := e.st.FarmSell(e.ctx, alice.ID, "ca_rot", 2, false)
	if err != nil || res.Gained == nil || res.Gained.Xu != 20 || res.Gained.XP != 0 || len(res.Wallets) != 1 ||
		res.Wallets[0].Balance != 1020 {
		t.Fatalf("sell 2: %v %+v", err, res)
	}
	if st := e.storage(alice.ID); st.Stock["ca_rot"] != 3 || st.Used != 6 {
		t.Fatalf("after sell 2: %+v", st)
	}
	res, err = e.st.FarmSell(e.ctx, alice.ID, "", 0, true)
	if err != nil || res.Gained.Xu != 30+44+214 {
		t.Fatalf("sell all: %v %+v", err, res.Gained)
	}
	if st := e.storage(alice.ID); st.Used != 0 || len(st.Stock) != 0 || e.balance(alice.ID) != 1020+288 {
		t.Fatalf("after sell all: %+v %d", st, e.balance(alice.ID))
	}
	if n := e.counter(alice.ID, "earn"); n != 20+288 {
		t.Fatalf("earn quest counter: %d", n)
	}
	_, err = e.st.FarmSell(e.ctx, alice.ID, "", 0, true)
	wantErr(t, err, store.ErrFarmStockEmpty)

	// đang nợ: 50% tiền bán tự trả nợ
	e.st.TransferCoins(e.ctx, bob.ID, alice.ID, 950, "")
	if _, err := e.st.TakeLoan(e.ctx, bob.ID); err != nil { // 350 / 360
		t.Fatal(err)
	}
	e.setStock(bob.ID, "sua", 3) // 225
	if res, err := e.st.FarmSell(e.ctx, bob.ID, "sua", 3, false); err != nil || res.Gained.Xu != 225 ||
		res.Wallets[0].Balance != 350+225-112 || res.Wallets[0].Debt != 360-112 {
		t.Fatalf("sell in debt: %v %+v", err, res)
	}
}

func TestFarmBuildingsAndAnimals(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	v := e.view(alice.ID, alice.ID)
	if len(v.Buildings) != 3 || v.Buildings[0].ID != "coop" || v.Buildings[0].Built || v.Buildings[0].Cap != 4 ||
		v.Buildings[0].Price != 200 || v.Buildings[0].Level != 2 || v.Buildings[0].UpgradePrice != nil ||
		v.Buildings[1].ID != "pasture" || v.Buildings[1].Cap != 3 || v.Buildings[1].Price != 600 || v.Buildings[1].Level != 3 ||
		v.Animals == nil || len(v.Animals) != 0 || len(v.AnimalKinds) != 5 {
		t.Fatalf("fresh buildings: %+v", v.Buildings)
	}

	_, err := e.st.FarmBuild(e.ctx, alice.ID, "chuong_lon")
	wantErr(t, err, store.ErrFarmUnknownBuild)
	_, err = e.st.FarmBuild(e.ctx, alice.ID, "coop")
	wantMsg(t, err, "Cần đạt cấp 2 mới xây được Chuồng gà.")
	_, err = e.st.FarmBuyAnimal(e.ctx, alice.ID, "ga")
	wantMsg(t, err, "Cần đạt cấp 2 mới nuôi được Gà.")
	_, err = e.st.FarmBuyAnimal(e.ctx, alice.ID, "khung_long")
	wantErr(t, err, store.ErrFarmUnknownAnimal)
	e.setXP(alice.ID, 30) // cấp 2
	_, err = e.st.FarmBuyAnimal(e.ctx, alice.ID, "ga")
	wantMsg(t, err, "Cần xây Chuồng gà trước khi nuôi Gà.")
	_, err = e.st.FarmUpgradeBuilding(e.ctx, alice.ID, "coop")
	wantMsg(t, err, "Chưa xây Chuồng gà.")
	_, err = e.st.FarmBuild(e.ctx, alice.ID, "pasture")
	wantMsg(t, err, "Cần đạt cấp 3 mới xây được Đồng cỏ.")

	// không đủ xu
	e.st.TransferCoins(e.ctx, alice.ID, bob.ID, 850, "")
	_, err = e.st.FarmBuild(e.ctx, alice.ID, "coop")
	wantErr(t, err, store.ErrInsufficientBalance)
	e.st.TransferCoins(e.ctx, bob.ID, alice.ID, 850, "")

	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "coop"); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmBuild(e.ctx, alice.ID, "coop")
	wantMsg(t, err, "Bạn đã xây Chuồng gà rồi.")
	if b := e.view(alice.ID, alice.ID).Buildings[0]; !b.Built || b.Cap != 4 || b.UpgradePrice == nil || *b.UpgradePrice != 400 {
		t.Fatalf("built coop: %+v", b)
	}
	for range 4 {
		if _, err := e.st.FarmBuyAnimal(e.ctx, alice.ID, "ga"); err != nil {
			t.Fatal(err)
		}
	}
	_, err = e.st.FarmBuyAnimal(e.ctx, alice.ID, "ga")
	wantMsg(t, err, "Chuồng gà đã đầy (4 con), nâng cấp để nuôi thêm.")
	if b := e.balance(alice.ID); b != 1000-200-4*80 {
		t.Fatalf("after 4 hens: %d", b)
	}
	if _, err := e.st.FarmUpgradeBuilding(e.ctx, alice.ID, "coop"); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmUpgradeBuilding(e.ctx, alice.ID, "coop")
	wantMsg(t, err, "Chuồng gà đã nâng cấp tối đa.")
	if b := e.view(alice.ID, alice.ID).Buildings[0]; b.Cap != 8 || b.UpgradePrice != nil {
		t.Fatalf("upgraded coop: %+v", b)
	}
	e.st.TransferCoins(e.ctx, bob.ID, alice.ID, 1000, "")
	for range 4 {
		if _, err := e.st.FarmBuyAnimal(e.ctx, alice.ID, "ga"); err != nil {
			t.Fatal(err)
		}
	}
	_, err = e.st.FarmBuyAnimal(e.ctx, alice.ID, "ga")
	wantMsg(t, err, "Chuồng gà đã đầy (8 con).")

	// bò cần cấp 3 và đồng cỏ
	_, err = e.st.FarmBuyAnimal(e.ctx, alice.ID, "bo")
	wantMsg(t, err, "Cần đạt cấp 3 mới nuôi được Bò.")
	e.setXP(alice.ID, 80)
	e.st.TransferCoins(e.ctx, e.user("carol").ID, alice.ID, 1000, "")
	_, err = e.st.FarmBuyAnimal(e.ctx, alice.ID, "bo")
	wantMsg(t, err, "Cần xây Đồng cỏ trước khi nuôi Bò.")
	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "pasture"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.FarmBuyAnimal(e.ctx, alice.ID, "bo"); err != nil {
		t.Fatal(err)
	}
	v = e.view(alice.ID, alice.ID)
	if len(v.Animals) != 9 || v.Animals[8].Kind != "bo" || v.Animals[0].State != "hungry" || v.Animals[0].ReadyAt != "" {
		t.Fatalf("animals: %+v", v.Animals)
	}
}

func TestFarmFeedAndCollect(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setXP(alice.ID, 30)
	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "coop"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := e.st.FarmBuyAnimal(e.ctx, alice.ID, "ga"); err != nil {
			t.Fatal(err)
		}
	}
	animals := e.view(alice.ID, alice.ID).Animals
	hen := animals[0].ID
	if animals[0].CanFeed {
		t.Fatalf("can feed without stock: %+v", animals[0])
	}
	_, err := e.st.FarmFeed(e.ctx, alice.ID, hen, false)
	wantMsg(t, err, "Thiếu Rau muống trong kho để cho Gà ăn.")
	_, err = e.st.FarmFeed(e.ctx, alice.ID, 0, true)
	wantMsg(t, err, "Thiếu Rau muống trong kho để cho Gà ăn.")
	_, err = e.st.FarmFeed(e.ctx, alice.ID, 9999, false)
	wantErr(t, err, store.ErrFarmNoAnimal)
	_, err = e.st.FarmFeed(e.ctx, bob.ID, hen, false) // con của người khác
	wantErr(t, err, store.ErrFarmNoAnimal)

	e.setStock(alice.ID, "rau_muong", 1)
	if a := e.view(alice.ID, alice.ID).Animals[0]; !a.CanFeed {
		t.Fatalf("cannot feed with stock: %+v", a)
	}
	if a := e.view(alice.ID, bob.ID).Animals[0]; a.CanFeed {
		t.Fatalf("neighbour can feed: %+v", a)
	}
	if _, err := e.st.FarmFeed(e.ctx, alice.ID, hen, false); err != nil {
		t.Fatal(err)
	}
	v := e.view(alice.ID, alice.ID)
	wantReady := e.now.Add(20 * time.Minute).UTC().Format(time.RFC3339)
	if a := v.Animals[0]; a.State != "producing" || a.ReadyAt != wantReady || a.CanFeed || v.Storage.Used != 0 {
		t.Fatalf("fed hen: %+v storage %+v", a, v.Storage)
	}
	_, err = e.st.FarmFeed(e.ctx, alice.ID, hen, false)
	wantErr(t, err, store.ErrFarmFed)
	_, err = e.st.FarmCollect(e.ctx, alice.ID, hen, false)
	wantErr(t, err, store.ErrFarmNotProduced)
	_, err = e.st.FarmCollect(e.ctx, alice.ID, 0, true)
	wantErr(t, err, store.ErrFarmNothingCollect)

	e.advance(20 * time.Minute)
	if a := e.view(alice.ID, alice.ID).Animals[0]; a.State != "ready" || a.ReadyAt != wantReady {
		t.Fatalf("ready hen: %+v", a)
	}
	_, err = e.st.FarmFeed(e.ctx, alice.ID, hen, false)
	wantErr(t, err, store.ErrFarmFed) // đang chờ thu, chưa đói
	// kho đầy thì không thu được
	e.setStock(alice.ID, "ca_rot", 60)
	_, err = e.st.FarmCollect(e.ctx, alice.ID, hen, false)
	wantErr(t, err, store.ErrFarmStorageFull)
	_, err = e.st.FarmCollect(e.ctx, alice.ID, 0, true)
	wantErr(t, err, store.ErrFarmStorageFull)
	e.setStock(alice.ID, "ca_rot", 0)
	res, err := e.st.FarmCollect(e.ctx, alice.ID, hen, false)
	if err != nil || res.Gained == nil || res.Gained.XP != 2 || res.Gained.Xu != 0 || fmt.Sprint(res.Gained.Items) != "map[trung:1]" {
		t.Fatalf("collect: %v %+v", err, res.Gained)
	}
	v = e.view(alice.ID, alice.ID)
	if a := v.Animals[0]; a.State != "hungry" || a.ReadyAt != "" || v.Storage.Stock["trung"] != 1 || v.XP != 32 {
		t.Fatalf("after collect: %+v %+v xp %d", a, v.Storage, v.XP)
	}

	// feed all: 3 gà đói, 2 rau muống → 2 con được ăn; hết thức ăn thì báo thiếu
	e.setStock(alice.ID, "rau_muong", 2)
	if _, err := e.st.FarmFeed(e.ctx, alice.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	v = e.view(alice.ID, alice.ID)
	if v.Animals[0].State != "producing" || v.Animals[1].State != "producing" || v.Animals[2].State != "hungry" ||
		v.Storage.Stock["rau_muong"] != 0 {
		t.Fatalf("feed all: %+v", v.Animals)
	}
	_, err = e.st.FarmFeed(e.ctx, alice.ID, 0, true)
	wantMsg(t, err, "Thiếu Rau muống trong kho để cho Gà ăn.")
	e.setStock(alice.ID, "rau_muong", 5)
	e.st.FarmFeed(e.ctx, alice.ID, 0, true)
	_, err = e.st.FarmFeed(e.ctx, alice.ID, 0, true)
	wantErr(t, err, store.ErrFarmNoHungry)

	// collect all: kho còn 2 chỗ → thu 2 con, con thứ 3 để nguyên
	e.advance(20 * time.Minute)
	e.setStock(alice.ID, "ca_rot", 60-5-2)
	res, err = e.st.FarmCollect(e.ctx, alice.ID, 0, true)
	if err != nil || res.Gained.Items["trung"] != 2 || res.Gained.XP != 4 {
		t.Fatalf("collect all partial: %v %+v", err, res.Gained)
	}
	if a := e.view(alice.ID, alice.ID).Animals; a[0].State != "hungry" || a[1].State != "hungry" || a[2].State != "ready" {
		t.Fatalf("after partial collect: %+v", a)
	}
}

// Con vật có thức ăn hơi khác: cừu ăn 2 cà rốt.
func TestFarmFeedQty(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.st.TransferCoins(e.ctx, e.user("bob").ID, alice.ID, 1000, "")
	e.setXP(alice.ID, 280) // cấp 5
	e.st.FarmBuild(e.ctx, alice.ID, "pasture")
	if _, err := e.st.FarmBuyAnimal(e.ctx, alice.ID, "cuu"); err != nil {
		t.Fatal(err)
	}
	e.setStock(alice.ID, "ca_rot", 1)
	id := e.view(alice.ID, alice.ID).Animals[0].ID
	_, err := e.st.FarmFeed(e.ctx, alice.ID, id, false)
	wantMsg(t, err, "Thiếu Cà rốt trong kho để cho Cừu ăn.")
	e.setStock(alice.ID, "ca_rot", 3)
	if _, err := e.st.FarmFeed(e.ctx, alice.ID, id, false); err != nil {
		t.Fatal(err)
	}
	if st := e.storage(alice.ID); st.Stock["ca_rot"] != 1 {
		t.Fatalf("feed qty: %+v", st)
	}
	e.advance(2 * time.Hour)
	if res, err := e.st.FarmCollect(e.ctx, alice.ID, id, false); err != nil || res.Gained.Items["len"] != 1 || res.Gained.XP != 10 {
		t.Fatalf("collect wool: %v %+v", err, res.Gained)
	}
}

func itemLevels() map[string]int {
	out := map[string]int{}
	for _, it := range store.Items {
		out[it.ID] = it.Level
	}
	return out
}

func TestFarmCatalogItems(t *testing.T) {
	want := map[string][2]int64{ // giá, cấp
		"rau_muong": {5, 1}, "ca_rot": {10, 1}, "ca_chua": {19, 2}, "bi_ngo": {214, 8},
		"trung": {22, 2}, "sua": {75, 3}, "pho_mai": {95, 5}, "len": {110, 4}, "nam": {220, 6},
	}
	if len(store.Items) != 13+len(store.Recipes) {
		t.Fatalf("items: %d", len(store.Items))
	}
	byID := map[string]store.Item{}
	for _, it := range store.Items {
		byID[it.ID] = it
	}
	// món chế biến: nguyên liệu có thật, mở trước món, và món đáng giá ≥ 1,4 lần nguyên liệu
	for _, r := range store.Recipes {
		var cost int64
		for _, in := range r.Inputs {
			it, ok := byID[in.Item]
			if !ok || it.Kind == "goods" || it.Level > r.Level {
				t.Fatalf("recipe %s input %+v", r.ID, in)
			}
			cost += it.Price * in.Qty
		}
		if g := byID[r.ID]; g.Kind != "goods" || g.Price != r.Price || r.Price*10 < cost*14 {
			t.Fatalf("recipe %s: price %d, inputs %d", r.ID, r.Price, cost)
		}
	}
	for _, it := range store.Items {
		if it.Kind == "goods" {
			continue
		}
		if w, ok := want[it.ID]; ok && (it.Price != w[0] || int64(it.Level) != w[1]) {
			t.Fatalf("item %+v, want %v", it, w)
		}
		if (it.Kind == "crop") != (it.ID != "trung" && it.ID != "sua" && it.ID != "pho_mai" && it.ID != "len" && it.ID != "nam") {
			t.Fatalf("item kind: %+v", it)
		}
	}
}

func TestFarmOrdersUnlockedItems(t *testing.T) {
	e := newFarmEnv(t)
	var mu sync.Mutex
	i := 0
	e.st.SetRand(func() float64 { // dãy giả ngẫu nhiên phủ đều [0, 1)
		mu.Lock()
		defer mu.Unlock()
		i++
		return float64((i*7919)%1000) / 1000
	})
	levels := itemLevels()
	for _, tc := range []struct {
		xp    int64
		level int
	}{{0, 1}, {80, 3}, {1900, 10}} {
		u := e.user(fmt.Sprintf("u%d", tc.level))
		e.setXP(u.ID, tc.xp)
		seen := map[string]bool{}
		for range 40 {
			v := e.view(u.ID, u.ID)
			if len(v.Orders) != 3 {
				t.Fatalf("orders: %+v", v.Orders)
			}
			for _, o := range v.Orders {
				if o.ID == 0 || len(o.Items) < 1 || len(o.Items) > 3 || o.CanDeliver == nil || o.NextAt != "" {
					t.Fatalf("order: %+v", o)
				}
				var total int64
				distinct := map[string]bool{}
				for _, it := range o.Items {
					if levels[it.Item] == 0 || levels[it.Item] > tc.level || it.Qty < 1 || it.Qty > 4 || distinct[it.Item] {
						t.Fatalf("level %d order item %+v", tc.level, it)
					}
					distinct[it.Item] = true
					seen[it.Item] = true
					for _, c := range store.Items {
						if c.ID == it.Item {
							total += c.Price * it.Qty
						}
					}
				}
				if wantXu := (total*135 + 99) / 100; o.Xu != wantXu || o.XP != (wantXu+9)/10 {
					t.Fatalf("order reward %+v, total %d", o, total)
				}
				if _, err := e.st.FarmDiscardOrder(e.ctx, u.ID, o.ID); err != nil {
					t.Fatal(err)
				}
			}
			e.advance(15 * time.Minute)
		}
		if tc.level == 1 && len(seen) != 2 || tc.level == 10 && len(seen) < 10 {
			t.Fatalf("level %d saw items %v", tc.level, seen)
		}
	}
}

func TestFarmOrdersDeliverDiscard(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setRoll(0.9) // cấp 1: 3 loại → chỉ có 2 loại; mỗi loại 4: 4 cà rốt + 4 rau muống = 60 → 81 xu, 9 XP
	v := e.view(alice.ID, alice.ID)
	o := v.Orders[0]
	if len(v.Orders) != 3 || o.Slot != 0 || v.Orders[2].Slot != 2 || o.Xu != 81 || o.XP != 9 || *o.CanDeliver ||
		fmt.Sprint(o.Items) != "[{ca_rot 4} {rau_muong 4}]" {
		t.Fatalf("orders: %+v", v.Orders)
	}
	if again := e.view(alice.ID, alice.ID).Orders; again[0].ID != o.ID || again[1].ID != v.Orders[1].ID {
		t.Fatalf("orders regenerated: %+v", again)
	}
	_, err := e.st.FarmDeliverOrder(e.ctx, alice.ID, o.ID)
	wantErr(t, err, store.ErrFarmOrderShort)
	wantMsg(t, err, "Chưa đủ hàng để giao đơn này.")
	e.setStock(alice.ID, "ca_rot", 4)
	e.setStock(alice.ID, "rau_muong", 3)
	_, err = e.st.FarmDeliverOrder(e.ctx, alice.ID, o.ID)
	wantErr(t, err, store.ErrFarmOrderShort)
	if st := e.storage(alice.ID); st.Used != 7 {
		t.Fatalf("failed delivery took stock: %+v", st)
	}
	e.setStock(alice.ID, "rau_muong", 5)
	if o := e.view(alice.ID, alice.ID).Orders[0]; !*o.CanDeliver {
		t.Fatalf("can_deliver: %+v", o)
	}
	res, err := e.st.FarmDeliverOrder(e.ctx, alice.ID, o.ID)
	if err != nil || res.Gained == nil || res.Gained.Xu != 81 || res.Gained.XP != 9 || res.Gained.Items != nil ||
		len(res.Wallets) != 1 || res.Wallets[0].Balance != 1081 {
		t.Fatalf("deliver: %v %+v", err, res)
	}
	v = e.view(alice.ID, alice.ID)
	next := e.now.Add(5 * time.Minute).UTC().Format(time.RFC3339)
	if s := v.Orders[0]; s.ID != 0 || s.Items != nil || s.CanDeliver != nil || s.NextAt != next || v.XP != 9 ||
		v.Storage.Stock["rau_muong"] != 1 || v.Storage.Stock["ca_rot"] != 0 {
		t.Fatalf("after deliver: %+v xp %d storage %+v", s, v.XP, v.Storage)
	}
	if e.counter(alice.ID, "order") != 1 || e.counter(alice.ID, "earn") != 81 {
		t.Fatal("deliver did not count for quests")
	}
	_, err = e.st.FarmDeliverOrder(e.ctx, alice.ID, o.ID)
	wantErr(t, err, store.ErrFarmNoOrder)
	_, err = e.st.FarmDiscardOrder(e.ctx, alice.ID, o.ID)
	wantErr(t, err, store.ErrFarmNoOrder)
	e.advance(5*time.Minute - time.Second)
	if s := e.view(alice.ID, alice.ID).Orders[0]; s.ID != 0 {
		t.Fatalf("refilled early: %+v", s)
	}
	e.advance(time.Second)
	refilled := e.view(alice.ID, alice.ID).Orders[0]
	if refilled.ID == 0 || refilled.ID == o.ID || refilled.NextAt != "" {
		t.Fatalf("refill after deliver: %+v", refilled)
	}

	// huỷ: đơn mới sau 15 phút
	second := v.Orders[1].ID
	if res, err := e.st.FarmDiscardOrder(e.ctx, alice.ID, second); err != nil || res.Gained != nil {
		t.Fatalf("discard: %v %+v", err, res)
	}
	next = e.now.Add(15 * time.Minute).UTC().Format(time.RFC3339)
	if s := e.view(alice.ID, alice.ID).Orders[1]; s.ID != 0 || s.NextAt != next {
		t.Fatalf("after discard: %+v", s)
	}
	e.advance(14 * time.Minute)
	if s := e.view(alice.ID, alice.ID).Orders[1]; s.ID != 0 {
		t.Fatalf("refilled early after discard: %+v", s)
	}
	e.advance(time.Minute)
	if s := e.view(alice.ID, alice.ID).Orders[1]; s.ID == 0 || s.ID == second {
		t.Fatalf("refill after discard: %+v", s)
	}
	// đơn của người khác: không thấy
	bob := e.user("bob")
	_, err = e.st.FarmDeliverOrder(e.ctx, bob.ID, refilled.ID)
	wantErr(t, err, store.ErrFarmNoOrder)
	if nv := e.view(alice.ID, bob.ID); nv.Orders != nil || nv.Storage != nil || nv.Items != nil || nv.AnimalKinds != nil {
		t.Fatalf("neighbour sees private fields: %+v", nv)
	}
}

// Giao đơn khi đang nợ: tiền thưởng qua earn, 50% tự trả nợ.
func TestFarmOrderAutoRepay(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.st.TransferCoins(e.ctx, alice.ID, bob.ID, 950, "")
	if _, err := e.st.TakeLoan(e.ctx, alice.ID); err != nil { // 350 / 360
		t.Fatal(err)
	}
	e.setRoll(0) // đơn: 1 rau muống → 7 xu, 1 XP
	o := e.view(alice.ID, alice.ID).Orders[0]
	e.setStock(alice.ID, "rau_muong", 1)
	res, err := e.st.FarmDeliverOrder(e.ctx, alice.ID, o.ID)
	if err != nil || res.Gained.Xu != 7 || res.Wallets[0].Balance != 350+7-3 || res.Wallets[0].Debt != 360-3 {
		t.Fatalf("deliver in debt: %v %+v", err, res)
	}
}

func TestFarmExpansionTiers(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	v := e.view(alice.ID, alice.ID)
	if v.MaxPlots != 20 || v.Expansion.Tier != 1 || v.Expansion.Plots != 6 || v.Expansion.Next == nil ||
		*v.Expansion.Next != (store.ExpansionStep{Plots: 9, Price: 400, Level: 2}) {
		t.Fatalf("fresh expansion: %+v", v.Expansion)
	}
	_, err := e.st.FarmExpand(e.ctx, alice.ID)
	wantMsg(t, err, "Cần đạt cấp 2 mới mở rộng được nông trại.")
	e.setXP(alice.ID, 30)
	e.st.TransferCoins(e.ctx, alice.ID, bob.ID, 700, "")
	_, err = e.st.FarmExpand(e.ctx, alice.ID)
	wantErr(t, err, store.ErrInsufficientBalance)
	e.st.TransferCoins(e.ctx, bob.ID, alice.ID, 700, "")
	if _, err := e.st.FarmExpand(e.ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	v = e.view(alice.ID, alice.ID)
	if v.PlotCount != 9 || len(v.Plots) != 9 || v.Expansion.Tier != 2 || v.Expansion.Plots != 9 ||
		*v.Expansion.Next != (store.ExpansionStep{Plots: 12, Price: 1000, Level: 4}) || e.balance(alice.ID) != 600 {
		t.Fatalf("after expand: %+v balance %d", v.Expansion, e.balance(alice.ID))
	}
	_, err = e.st.FarmExpand(e.ctx, alice.ID)
	wantMsg(t, err, "Cần đạt cấp 4 mới mở rộng được nông trại.")
	// hàng xóm cũng thấy mở rộng
	if nv := e.view(alice.ID, bob.ID); nv.Expansion.Tier != 2 || nv.Buildings == nil || nv.Animals == nil {
		t.Fatalf("neighbour view: %+v", nv)
	}

	// số ô mua lẻ ở phần 1 → bậc lớn nhất có số ô ≤ số ô đang có; bậc kế lên đúng số ô của bậc đó
	for _, tc := range []struct {
		plots, tier int
		next        *store.ExpansionStep
	}{
		{7, 1, &store.ExpansionStep{Plots: 9, Price: 400, Level: 2}},
		{8, 1, &store.ExpansionStep{Plots: 9, Price: 400, Level: 2}},
		{10, 2, &store.ExpansionStep{Plots: 12, Price: 1000, Level: 4}},
		{11, 2, &store.ExpansionStep{Plots: 12, Price: 1000, Level: 4}},
		{12, 3, &store.ExpansionStep{Plots: 16, Price: 2200, Level: 6}},
		{16, 4, &store.ExpansionStep{Plots: 20, Price: 4500, Level: 8}},
		{20, 5, nil},
	} {
		if err := e.st.FarmSetPlotsForTest(e.ctx, bob.ID, tc.plots); err != nil {
			t.Fatal(err)
		}
		v := e.view(bob.ID, bob.ID)
		ok := v.Expansion.Tier == tc.tier && v.Expansion.Plots == tc.plots && len(v.Plots) == tc.plots
		if tc.next == nil {
			ok = ok && v.Expansion.Next == nil && v.NextPlot == nil
		} else {
			ok = ok && v.Expansion.Next != nil && *v.Expansion.Next == *tc.next &&
				v.NextPlot.Price == tc.next.Price && v.NextPlot.Level == tc.next.Level
		}
		if !ok {
			t.Fatalf("%d plots: %+v next_plot %+v", tc.plots, v.Expansion, v.NextPlot)
		}
	}
	_, err = e.st.FarmExpand(e.ctx, bob.ID)
	wantErr(t, err, store.ErrFarmMaxPlots)
	wantMsg(t, err, "Nông trại đã mở rộng tối đa.")

	// 7 ô (mua lẻ) lên bậc 2 thì có đúng 9 ô, ô cũ giữ nguyên
	carol := e.user("carol")
	e.setXP(carol.ID, 30)
	e.st.FarmSetPlotsForTest(e.ctx, carol.ID, 7)
	e.plant(carol.ID, 6, "rau_muong")
	if _, err := e.st.FarmExpand(e.ctx, carol.ID); err != nil {
		t.Fatal(err)
	}
	if v := e.view(carol.ID, carol.ID); v.PlotCount != 9 || v.Plots[6].State != "growing" || e.balance(carol.ID) != 1000-10-400 {
		t.Fatalf("legacy expand: %d %+v %d", v.PlotCount, v.Plots[6], e.balance(carol.ID))
	}
}

// Dữ liệu phần 1 (ô mua lẻ, XP, vụ đang bị trộm tính bằng xu) vẫn dùng được sau migration phần 2;
// migration down rồi up lại chạy được.
func TestFarmV2MigrationKeepsPartOneData(t *testing.T) {
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	migrate := func(mode database.Mode, steps int) {
		t.Helper()
		if err := database.Migrate(database.MigrationOpt{DB: db, Mode: mode, Steps: steps}); err != nil {
			t.Fatal(err)
		}
	}
	v2Applied := func() bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = 'farm_v2'`).Scan(&n)
		return n > 0
	}
	// lên hết rồi lùi về ngay trước farm_v2
	migrate(database.ModeUp, 0)
	for v2Applied() {
		migrate(database.ModeDown, 1)
	}

	st := store.New(db)
	now := time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })
	st.SetRand(func() float64 { return 0.9 })
	ctx := t.Context()
	u, err := st.CreateUser(ctx, "old", "old@x.vn", "h", "Old")
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO farms (user_id, xp, plots) VALUES (?, 100, 8)`, u.ID)
	// vụ chín bị trộm 2 lần kiểu cũ (stolen = 4 xu); vụ chín bị trộm 1 lần (stolen = 3 xu)
	exec(`INSERT INTO farm_plots (user_id, idx, crop, planted_at, ready_at, stolen, steals) VALUES (?, 0, 'rau_muong', ?, ?, 4, 2)`,
		u.ID, now.Unix()-600, now.Unix()-300)
	exec(`INSERT INTO farm_plots (user_id, idx, crop, planted_at, ready_at, stolen, steals) VALUES (?, 7, 'ca_rot', ?, ?, 3, 1)`,
		u.ID, now.Unix()-1200, now.Unix()-300)

	migrate(database.ModeUp, 0)
	v, err := st.GetFarm(ctx, u.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.XP != 100 || v.Level != 3 || v.PlotCount != 8 || len(v.Plots) != 8 || v.Plots[0].Stolen != 2 || v.Plots[7].Stolen != 1 ||
		v.Expansion.Tier != 1 || v.Expansion.Next.Plots != 9 || v.Storage.Cap != 60 || len(v.Orders) != 3 {
		t.Fatalf("migrated farm: %+v", v)
	}
	res, err := st.FarmHarvest(ctx, u.ID, 0, true)
	if err != nil || fmt.Sprint(res.Gained.Items) != "map[ca_rot:2 rau_muong:1]" {
		t.Fatalf("harvest migrated plots: %v %+v", err, res.Gained)
	}

	// down (ô vượt 12 bị bỏ) rồi up lại
	exec(`UPDATE farms SET plots = 16 WHERE user_id = ?`, u.ID)
	for v2Applied() {
		migrate(database.ModeDown, 1)
	}
	var plots int
	db.QueryRow(`SELECT plots FROM farms WHERE user_id = ?`, u.ID).Scan(&plots)
	if plots != 12 {
		t.Fatalf("plots after down: %d", plots)
	}
	migrate(database.ModeUp, 0)
	if !v2Applied() {
		t.Fatal("farm_v2 not re-applied")
	}
}

// Song song: sức chứa kho và giao đơn kiểm tra trong cùng transaction với hành động.
func TestFarmV2Race(t *testing.T) {
	e := newFarmEnv(t)
	owner, thief := e.user("owner"), e.user("thief")
	for i := range 5 {
		e.plant(owner.ID, i, "rau_muong")
	}
	e.advance(5 * time.Minute)
	e.setStock(thief.ID, "ca_rot", 59) // còn đúng 1 chỗ
	e.setStock(owner.ID, "ca_rot", 4)
	e.setStock(owner.ID, "rau_muong", 4)
	o := e.view(owner.ID, owner.ID).Orders[0] // roll 0.9: 4 cà rốt + 4 rau muống
	var wg sync.WaitGroup
	var mu sync.Mutex
	steals, delivers := 0, 0
	for i := range 5 {
		wg.Go(func() {
			if _, err := e.st.FarmSteal(e.ctx, thief.ID, owner.ID, i); err == nil {
				mu.Lock()
				steals++
				mu.Unlock()
			}
		})
		wg.Go(func() {
			if _, err := e.st.FarmDeliverOrder(e.ctx, owner.ID, o.ID); err == nil {
				mu.Lock()
				delivers++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if st := e.storage(thief.ID); steals != 1 || st.Used != 60 || delivers != 1 || e.balance(owner.ID) != 1000-50+81 {
		t.Fatalf("steals %d (want 1) storage %+v delivers %d balance %d", steals, st, delivers, e.balance(owner.ID))
	}
}

func TestFarmStarterFeedAndMarketBuy(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	store.GiveStarterFeed = true
	t.Cleanup(func() { store.GiveStarterFeed = false })
	u, _ := st.CreateUser(ctx, "nongdan", "nd@x.vn", "h", "Nông dân")

	// nông trại mới có gói thức ăn khởi đầu, đúng một lần
	v, err := st.GetFarm(ctx, u.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"rau_muong": 6, "ngo": 3, "ca_rot": 4, "ca_chua": 3}
	for k, n := range want {
		if v.Storage.Stock[k] != n {
			t.Fatalf("starter %s = %d, want %d (stock %v)", k, v.Storage.Stock[k], n, v.Storage.Stock)
		}
	}
	v, _ = st.GetFarm(ctx, u.ID, u.ID)
	if v.Storage.Stock["rau_muong"] != 6 {
		t.Fatalf("starter given twice: %v", v.Storage.Stock)
	}

	// mua ở chợ: giá đắt hơn bán 50%, cộng vào kho, trừ ví
	res, err := st.FarmBuyItem(ctx, u.ID, "rau_muong", 4)
	if err != nil {
		t.Fatal(err)
	}
	if res.Gained == nil || res.Gained.Items["rau_muong"] != 4 {
		t.Fatalf("gained: %+v", res.Gained)
	}
	w, _ := st.GetWallet(ctx, u.ID)
	if w.Balance != store.WalletStart-4*8 {
		t.Fatalf("balance after buy: %d", w.Balance)
	}
	v, _ = st.GetFarm(ctx, u.ID, u.ID)
	if v.Storage.Stock["rau_muong"] != 10 {
		t.Fatalf("stock after buy: %v", v.Storage.Stock)
	}
	// chưa đủ cấp, sản phẩm chăn nuôi, số lượng sai, thiếu xu
	if _, err := st.FarmBuyItem(ctx, u.ID, "ngo", 1); err == nil {
		t.Fatal("bought a level-3 crop at level 1")
	}
	if _, err := st.FarmBuyItem(ctx, u.ID, "trung", 1); err == nil {
		t.Fatal("bought an animal product")
	}
	if _, err := st.FarmBuyItem(ctx, u.ID, "rau_muong", 0); err == nil {
		t.Fatal("bought zero")
	}
	if _, err := st.FarmBuyItem(ctx, u.ID, "ca_rot", 50); err == nil {
		t.Fatal("bought more than the wallet holds")
	}
}

// Xưởng chế biến (§9): xây ở cấp 4, 2 lò, nguyên liệu trừ ngay, món vào kho khi lấy, nâng cấp 4 lò.
func TestFarmKitchen(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	_, err := e.st.FarmCraft(e.ctx, alice.ID, "salad")
	wantErr(t, err, store.ErrFarmNoKitchen)
	_, err = e.st.FarmBuild(e.ctx, alice.ID, "kitchen")
	wantMsg(t, err, "Cần đạt cấp 4 mới xây được Xưởng chế biến.")
	e.setXP(alice.ID, 160) // cấp 4
	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	v := e.view(alice.ID, alice.ID)
	if b := v.Buildings[2]; b.ID != "kitchen" || !b.Built || b.Cap != 2 || b.UpgradePrice == nil || *b.UpgradePrice != 1200 {
		t.Fatalf("kitchen: %+v", b)
	}
	if len(v.Recipes) != len(store.Recipes) || v.Jobs == nil || len(v.Jobs) != 0 {
		t.Fatalf("recipes %d jobs %v", len(v.Recipes), v.Jobs)
	}
	_, err = e.st.FarmCraft(e.ctx, alice.ID, "banh_mi_que")
	wantErr(t, err, store.ErrFarmUnknownRecipe)
	_, err = e.st.FarmCraft(e.ctx, alice.ID, "com_nam")
	wantMsg(t, err, "Cần đạt cấp 5 mới làm được Cơm nắm.")
	e.setStock(alice.ID, "rau_muong", 1)
	_, err = e.st.FarmCraft(e.ctx, alice.ID, "salad")
	wantMsg(t, err, "Thiếu 1 rau muống, 1 cà rốt, 1 cà chua trong kho để làm Salad rau.")
	if e.storage(alice.ID).Stock["rau_muong"] != 1 {
		t.Fatal("failed craft took stock")
	}

	e.setStock(alice.ID, "rau_muong", 4)
	e.setStock(alice.ID, "ca_rot", 2)
	e.setStock(alice.ID, "ca_chua", 2)
	e.setStock(alice.ID, "ngo", 2)
	for _, r := range []string{"salad", "bap_rang"} {
		if _, err := e.st.FarmCraft(e.ctx, alice.ID, r); err != nil {
			t.Fatal(r, err)
		}
	}
	_, err = e.st.FarmCraft(e.ctx, alice.ID, "salad")
	wantErr(t, err, store.ErrFarmKitchenBusy)
	if st := e.storage(alice.ID); st.Used != 4 || st.Stock["ngo"] != 0 || st.Stock["rau_muong"] != 2 {
		t.Fatalf("stock after craft: %+v", st)
	}
	v = e.view(alice.ID, alice.ID)
	if len(v.Jobs) != 2 || v.Jobs[0].Recipe != "salad" || v.Jobs[0].State != "working" || v.Jobs[1].Slot != 1 {
		t.Fatalf("jobs: %+v", v.Jobs)
	}
	// hàng xóm thấy lò đang nấu nhưng không thấy danh mục món
	if nv := e.view(alice.ID, bob.ID); len(nv.Jobs) != 2 || nv.Recipes != nil {
		t.Fatalf("neighbour view: %+v %+v", nv.Jobs, nv.Recipes)
	}
	_, err = e.st.FarmCraftCollect(e.ctx, alice.ID, 0, false)
	wantErr(t, err, store.ErrFarmJobNotReady)
	_, err = e.st.FarmCraftCollect(e.ctx, alice.ID, 0, true)
	wantErr(t, err, store.ErrFarmNothingCraft)

	e.advance(15 * time.Minute) // salad xong, bắp rang chưa
	if v := e.view(alice.ID, alice.ID); v.Jobs[0].State != "ready" || v.Jobs[1].State != "working" {
		t.Fatalf("jobs after 15m: %+v", v.Jobs)
	}
	res, err := e.st.FarmCraftCollect(e.ctx, alice.ID, 0, true)
	if err != nil || res.Gained.Items["salad"] != 1 || res.Gained.XP != 4 {
		t.Fatalf("collect: %v %+v", err, res.Gained)
	}
	_, err = e.st.FarmCraftCollect(e.ctx, alice.ID, 0, false)
	wantErr(t, err, store.ErrFarmNoJob)
	// lò 0 trống lại: nấu tiếp được
	if _, err := e.st.FarmCraft(e.ctx, alice.ID, "salad"); err != nil {
		t.Fatal(err)
	}
	if v := e.view(alice.ID, alice.ID); len(v.Jobs) != 2 || v.Jobs[0].Slot != 0 || v.Jobs[0].Recipe != "salad" {
		t.Fatalf("refilled: %+v", v.Jobs)
	}

	// kho đầy: không lấy được món
	e.advance(30 * time.Minute)
	e.setStock(alice.ID, "lua", 60-e.storage(alice.ID).Used)
	_, err = e.st.FarmCraftCollect(e.ctx, alice.ID, 1, false)
	wantErr(t, err, store.ErrFarmStorageFull)
	e.setStock(alice.ID, "lua", 0)
	res, err = e.st.FarmCraftCollect(e.ctx, alice.ID, 0, true)
	if err != nil || res.Gained.Items["salad"] != 1 || res.Gained.Items["bap_rang"] != 1 || res.Gained.XP != 10 {
		t.Fatalf("collect all: %v %+v", err, res.Gained)
	}
	// món bán được ở kho theo giá món
	before := e.balance(alice.ID)
	if _, err := e.st.FarmSell(e.ctx, alice.ID, "bap_rang", 1, false); err != nil {
		t.Fatal(err)
	}
	if got := e.balance(alice.ID) - before; got != 88 {
		t.Fatalf("sold popcorn for %d", got)
	}

	// nâng cấp: 4 lò
	e.st.TransferCoins(e.ctx, bob.ID, alice.ID, 900, "")
	if _, err := e.st.FarmUpgradeBuilding(e.ctx, alice.ID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	if b := e.view(alice.ID, alice.ID).Buildings[2]; b.Cap != 4 || b.UpgradePrice != nil {
		t.Fatalf("upgraded kitchen: %+v", b)
	}
}

// Món chế biến chỉ vào đơn hàng khi đã có xưởng, mỗi đơn nhiều nhất một loại món.
func TestFarmOrdersGoods(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setXP(alice.ID, 1900) // cấp 10: mọi món đã mở
	goodsIn := func() (lines, orders int) {
		for _, o := range e.view(alice.ID, alice.ID).Orders {
			n := 0
			for _, it := range o.Items {
				for _, r := range store.Recipes {
					if r.ID == it.Item {
						n++
					}
				}
			}
			if n > 1 {
				t.Fatalf("order with %d goods: %+v", n, o.Items)
			}
			lines += n
			if n > 0 {
				orders++
			}
		}
		return
	}
	reroll := func() {
		for _, o := range e.view(alice.ID, alice.ID).Orders {
			if o.ID != 0 {
				if _, err := e.st.FarmDiscardOrder(e.ctx, alice.ID, o.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
		e.advance(store.FarmOrderAfterDiscard)
	}
	// cuộn cuối danh mục (0.99) luôn trúng món cuối cùng: súp nấm nếu có xưởng
	e.setRoll(0.99)
	if n, _ := goodsIn(); n != 0 {
		t.Fatal("goods in orders without a kitchen")
	}
	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	reroll()
	if n, o := goodsIn(); n != 3 || o != 3 {
		t.Fatalf("goods lines %d in %d orders", n, o)
	}
}

// Chọn chỗ đặt công trình tự do (§10): chỗ mặc định hợp lệ, chỗ vướng / ra ngoài bị từ chối, dời được.
func TestFarmBuildingSpots(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setXP(alice.ID, 160)
	k := e.view(alice.ID, alice.ID).Buildings[2]
	if k.Size == nil || k.Spot == nil || *k.Size != (store.Size{W: 3, D: 2}) || *k.Spot != (store.Spot{X: 12, Y: 12}) {
		t.Fatalf("kitchen size/spot: %+v", k)
	}
	if v := e.view(alice.ID, alice.ID); len(v.Blocked) == 0 || v.Buildings[0].Spot != nil {
		t.Fatalf("blocked %d, coop spot %v", len(v.Blocked), v.Buildings[0].Spot)
	}
	before := e.balance(alice.ID)
	for _, c := range []struct {
		at  store.Spot
		err error
	}{
		{store.Spot{X: 10, Y: 7}, store.ErrFarmSpotBlocked}, // giữa ruộng
		{store.Spot{X: 18, Y: 10}, store.ErrFarmSpotBlocked}, // ao
		{store.Spot{X: 22, Y: 5}, store.ErrFarmSpotOut},      // lố mép phải
		{store.Spot{X: -1, Y: 5}, store.ErrFarmSpotOut},
	} {
		_, err := e.st.FarmBuildAt(e.ctx, alice.ID, "kitchen", &c.at)
		wantErr(t, err, c.err)
	}
	if e.balance(alice.ID) != before {
		t.Fatal("rejected spot still charged")
	}
	at := store.Spot{X: 20, Y: 6}
	if _, err := e.st.FarmBuildAt(e.ctx, alice.ID, "kitchen", &at); err != nil {
		t.Fatal(err)
	}
	if k := e.view(alice.ID, bob.ID).Buildings[2]; !k.Built || *k.Spot != at {
		t.Fatalf("neighbour sees kitchen at %+v", k.Spot)
	}
	_, err := e.st.FarmMoveBuilding(e.ctx, alice.ID, "kitchen", store.Spot{X: 9, Y: 9})
	wantErr(t, err, store.ErrFarmSpotBlocked)
	if _, err := e.st.FarmMoveBuilding(e.ctx, alice.ID, "kitchen", store.Spot{X: 2, Y: 17}); err != nil {
		t.Fatal(err)
	}
	if k := e.view(alice.ID, alice.ID).Buildings[2]; *k.Spot != (store.Spot{X: 2, Y: 17}) {
		t.Fatalf("moved to %+v", k.Spot)
	}
	_, err = e.st.FarmMoveBuilding(e.ctx, alice.ID, "coop", store.Spot{X: 2, Y: 17})
	wantErr(t, err, store.ErrFarmNotMovable)
	_, err = e.st.FarmMoveBuilding(e.ctx, bob.ID, "kitchen", store.Spot{X: 2, Y: 17})
	wantMsg(t, err, "Chưa xây Xưởng chế biến.")
}
