package store_test

import (
	"testing"
	"time"
)

// Cấp nông trại không giới hạn + quyền lợi theo cấp (view, trồng, nuôi, thu hoạch, bán).

func TestFarmLevelView(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setXP(alice.ID, 5000)
	v := e.view(alice.ID, alice.ID)
	if v.Level < 11 || v.NextLevelXP == nil || *v.NextLevelXP <= v.XP {
		t.Fatalf("view: level %d next %v", v.Level, v.NextLevelXP)
	}
	if v.Perks.GrowPct != 2*(v.Level-1) || v.NextPerks.GrowPct != min(2*v.Level, 50) {
		t.Fatalf("perks %+v next %+v", v.Perks, v.NextPerks)
	}
	// dữ liệu cũ: XP >= 1900 không còn bị kẹt ở cấp 10
	e.setXP(alice.ID, 1900)
	if v := e.view(alice.ID, alice.ID); v.Level != 10 || v.NextLevelXP == nil || *v.NextLevelXP != 2480 {
		t.Fatalf("level 10: %d %v", v.Level, v.NextLevelXP)
	}
	e.setXP(alice.ID, 2480)
	if v := e.view(alice.ID, alice.ID); v.Level != 11 || v.Perks.GrowPct != 20 || v.Perks.AnimalPct != 10 {
		t.Fatalf("level 11: %+v", v)
	}
}

func TestFarmPerkGrowAndAnimal(t *testing.T) {
	e, id := tendFarm(t, 2480) // cấp 11: grow 20%, animal 10%
	e.plant(id, 0, "rau_muong")
	wantReady := func(d time.Duration) string { return e.now.Add(d).UTC().Format(time.RFC3339) }
	if p := e.view(id, id).Plots[0]; p.ReadyAt != wantReady(240*time.Second) {
		t.Fatalf("plot ready_at %s", p.ReadyAt)
	}
	e.advance(239 * time.Second)
	if p := e.view(id, id).Plots[0]; p.State != "growing" {
		t.Fatalf("239s: %+v", p)
	}
	e.advance(time.Second)
	if p := e.view(id, id).Plots[0]; p.State != "ripe" {
		t.Fatalf("240s: %+v", p)
	}

	e.buyAnimals(id, "ga", "ga")
	e.setStock(id, "rau_muong", 1)
	hen := e.view(id, id).Animals[0].ID
	if _, err := e.st.FarmFeed(e.ctx, id, hen, false); err != nil {
		t.Fatal(err)
	}
	if a := e.view(id, id).Animals[0]; a.ReadyAt != wantReady(1080*time.Second) {
		t.Fatalf("fed hen ready_at %s", a.ReadyAt)
	}
	// FarmTend: con còn lại cũng 1080s
	e.setStock(id, "rau_muong", 1)
	if _, err := e.st.FarmTend(e.ctx, id, "coop"); err != nil {
		t.Fatal(err)
	}
	if a := e.view(id, id).Animals[1]; a.State != "producing" || a.ReadyAt != wantReady(1080*time.Second) {
		t.Fatalf("tend hen: %+v", a)
	}
}

func TestFarmHarvestBonus(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setXP(alice.ID, 1900) // cấp 10: +2 đơn vị
	e.plant(alice.ID, 0, "ca_rot")
	e.setRoll(0.1)
	e.plant(alice.ID, 1, "ca_rot") // có sâu
	e.setRoll(0.9)
	e.advance(time.Hour)
	res, err := e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	if err != nil || res.Gained.Items["ca_rot"] != 5 {
		t.Fatalf("clean: %v %+v", err, res.Gained)
	}
	res, err = e.st.FarmHarvest(e.ctx, alice.ID, 1, false)
	if err != nil || res.Gained.Items["ca_rot"] != 4 {
		t.Fatalf("bug: %v %+v", err, res.Gained)
	}
	// trộm: kẻ trộm đúng 1 đơn vị, chủ còn 5 - đã trộm
	e.plant(alice.ID, 0, "ca_rot")
	e.advance(time.Hour)
	if _, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 0); err != nil {
		t.Fatal(err)
	}
	if n := e.storage(bob.ID).Stock["ca_rot"]; n != 1 {
		t.Fatalf("thief got %d", n)
	}
	res, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	if err != nil || res.Gained.Items["ca_rot"] != 4 {
		t.Fatalf("after steal: %v %+v", err, res.Gained)
	}
}

func TestFarmSellBonus(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setXP(alice.ID, 2480) // cấp 11: +20%
	e.setStock(alice.ID, "trung", 5)
	e.setStock(alice.ID, "ca_rot", 3)
	res, err := e.st.FarmSell(e.ctx, alice.ID, "trung", 5, false)
	if err != nil || res.Gained.Xu != 22*5*120/100 {
		t.Fatalf("sell: %v %+v", err, res.Gained)
	}
	e.setStock(alice.ID, "trung", 2)
	res, err = e.st.FarmSell(e.ctx, alice.ID, "", 0, true)
	if want := int64(22*2*120/100 + 10*3*120/100); err != nil || res.Gained.Xu != want {
		t.Fatalf("sell all: %v %+v want %d", err, res.Gained, want)
	}
	e.setStock(bob.ID, "trung", 5) // cấp 1: không thưởng
	res, err = e.st.FarmSell(e.ctx, bob.ID, "trung", 5, false)
	if err != nil || res.Gained.Xu != 110 {
		t.Fatalf("level 1: %v %+v", err, res.Gained)
	}
}
