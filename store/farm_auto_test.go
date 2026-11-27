package store_test

import (
	"testing"
	"time"

	"chat/store"
)

// Gieo tất cả và /auto (docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md, phần C).

func TestFarmPlantAll(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.leaveCoins(alice.ID, 25)
	res, err := e.st.FarmPlantAll(e.ctx, alice.ID, "rau_muong")
	if err != nil || res.Gained == nil || res.Gained.Planted != 2 {
		t.Fatalf("plant all: %v %+v", err, res.Gained)
	}
	if e.balance(alice.ID) != 5 {
		t.Fatalf("balance %d", e.balance(alice.ID))
	}
	n := 0
	for _, p := range e.view(alice.ID, alice.ID).Plots {
		if p.State == "growing" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("growing plots %d", n)
	}

	// ô héo được dọn rồi gieo lại
	e2 := newFarmEnv(t)
	bob := e2.user("bob")
	e2.plant(bob.ID, 0, "rau_muong")
	e2.advance(store.FarmWitherAfter + 48*time.Hour)
	if st := e2.view(bob.ID, bob.ID).Plots[0].State; st != "withered" {
		t.Fatalf("state %s", st)
	}
	e2.leaveCoins(bob.ID, 60)
	res, err = e2.st.FarmPlantAll(e2.ctx, bob.ID, "rau_muong")
	if err != nil || res.Gained.Planted != 6 {
		t.Fatalf("replant: %v %+v", err, res.Gained)
	}
	if st := e2.view(bob.ID, bob.ID).Plots[0].State; st != "growing" {
		t.Fatalf("state %s", st)
	}

	// kèm cấp và xu
	e3 := newFarmEnv(t)
	carol := e3.user("carol")
	hi := store.Crops[len(store.Crops)-1]
	if hi.Level <= 1 {
		t.Fatal("need a level-gated crop")
	}
	_, err = e3.st.FarmPlantAll(e3.ctx, carol.ID, hi.ID)
	wantFarmErr(t, err)
	e3.leaveCoins(carol.ID, 0)
	_, err = e3.st.FarmPlantAll(e3.ctx, carol.ID, "rau_muong")
	wantErr(t, err, store.ErrInsufficientBalance)
	if e3.balance(carol.ID) != 0 {
		t.Fatalf("balance %d", e3.balance(carol.ID))
	}
	_, err = e3.st.FarmPlantAll(e3.ctx, carol.ID, "khong_co")
	wantErr(t, err, store.ErrFarmUnknownCrop)
}

func TestFarmAutoAllSteps(t *testing.T) {
	e, aliceID := tendFarm(t, levelXPForTest(8))
	alice := struct{ ID int64 }{aliceID}
	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	e.plant(alice.ID, 0, "rau_muong")
	e.plant(alice.ID, 1, "rau_muong")
	e.plantTree(alice.ID, 0, "cam")
	e.setStock(alice.ID, "ngo", 4)
	if _, err := e.st.FarmCraft(e.ctx, alice.ID, "bap_rang"); err != nil {
		t.Fatal(err)
	}
	e.buyAnimals(alice.ID, "ga")
	e.advance(3 * time.Hour) // cây chín, cây ăn quả chín, món xong
	e.setStock(alice.ID, "ngo", 2)
	e.setStock(alice.ID, "rau_muong", 5) // thức ăn của gà

	res, err := e.st.FarmAuto(e.ctx, alice.ID, store.AutoOpts{
		Crop: "rau_muong", KitchenRecipe: "bap_rang", JuicerRecipe: "nuoc_cam", BuyFeed: false})
	if err != nil {
		t.Fatal(err)
	}
	a := res.Gained.Auto
	if a == nil || a.Harvested < 2 || a.Planted == 0 || a.Fed == 0 || a.Fruits == 0 || a.Collected == 0 || a.Crafted == 0 {
		t.Fatalf("report: %+v", a)
	}
	if a.Spent <= 0 || res.Gained.Xu != -a.Spent {
		t.Fatalf("spent %d xu %d", a.Spent, res.Gained.Xu)
	}
	found := false
	for _, s := range a.Skipped {
		if s == "Máy ép: "+store.ErrFarmNoJuicer.Error() {
			found = true
		}
	}
	if !found {
		t.Fatalf("skipped: %v", a.Skipped)
	}
}

func TestFarmAutoNoBuyFeed(t *testing.T) {
	e, id := tendFarm(t, 400)
	e.buyAnimals(id, "ga")
	bal := e.balance(id)
	_, err := e.st.FarmAuto(e.ctx, id, store.AutoOpts{})
	wantErr(t, err, store.ErrFarmNothingAuto)
	if e.balance(id) != bal {
		t.Fatalf("balance %d -> %d", bal, e.balance(id))
	}
	res, err := e.st.FarmAuto(e.ctx, id, store.AutoOpts{BuyFeed: true})
	if err != nil {
		t.Fatal(err)
	}
	if a := res.Gained.Auto; a.Fed != 1 || a.Spent <= 0 {
		t.Fatalf("report: %+v", a)
	}
	if e.balance(id) >= bal {
		t.Fatalf("balance %d -> %d", bal, e.balance(id))
	}
}

func TestFarmAutoNothing(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	_, err := e.st.FarmAuto(e.ctx, alice.ID, store.AutoOpts{})
	wantErr(t, err, store.ErrFarmNothingAuto)
}

// Bước gặp lỗi nghiệp vụ không để lại ghi chép dở: gieo không đủ xu thì ô héo vẫn còn.
func TestFarmAutoStepRollback(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.plant(alice.ID, 0, "rau_muong")
	e.advance(store.FarmWitherAfter + 48*time.Hour)
	e.leaveCoins(alice.ID, 0)
	_, err := e.st.FarmAuto(e.ctx, alice.ID, store.AutoOpts{Crop: "rau_muong"})
	wantErr(t, err, store.ErrFarmNothingAuto)
	if st := e.view(alice.ID, alice.ID).Plots[0].State; st != "withered" {
		t.Fatalf("state %s", st)
	}
}

// Sản phẩm vật nuôi đã chín được thu và tính vào Collected dù không có thức ăn để cho ăn tiếp.
func TestFarmAutoCollectsAnimalProducts(t *testing.T) {
	e, id := tendFarm(t, 400)
	e.buyAnimals(id, "ga")
	if _, err := e.st.FarmTend(e.ctx, id, "coop"); err != nil {
		t.Fatal(err)
	}
	e.setStock(id, "rau_muong", 0)
	e.setStock(id, "trung", 0)
	e.advance(time.Hour)
	res, err := e.st.FarmAuto(e.ctx, id, store.AutoOpts{BuyFeed: false})
	if err != nil {
		t.Fatal(err)
	}
	if a := res.Gained.Auto; a.Collected < 1 || a.Fed != 0 {
		t.Fatalf("report: %+v", a)
	}
	if res.Gained.Items["trung"] < 1 || e.storage(id).Stock["trung"] < 1 {
		t.Fatalf("egg: %+v %v", res.Gained.Items, e.storage(id).Stock)
	}
}

func TestFarmAutoLevelUp(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setXP(alice.ID, levelXPForTest(3)-1)
	e.plant(alice.ID, 0, "rau_muong")
	e.advance(time.Hour)
	res, err := e.st.FarmAuto(e.ctx, alice.ID, store.AutoOpts{})
	if err != nil || res.Gained.LevelUp != 3 {
		t.Fatalf("level up: %v %+v", err, res.Gained)
	}
}

func TestFarmAutoTreeLevelUp(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setXP(alice.ID, levelXPForTest(5))
	e.plantTree(alice.ID, 0, "cam")
	e.advance(3 * time.Hour)
	e.setXP(alice.ID, levelXPForTest(6)-1)
	res, err := e.st.FarmAuto(e.ctx, alice.ID, store.AutoOpts{})
	if err != nil || res.Gained.LevelUp != 6 {
		t.Fatalf("level up: %v %+v", err, res.Gained)
	}
}
