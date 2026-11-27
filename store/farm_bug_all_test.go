package store_test

import (
	"testing"
	"time"

	"chat/store"
)

// Bắt sâu tất cả và bước "Bắt sâu" của /auto (docs/superpowers/specs/2026-11-27-farm-bug-all-design.md).

func TestFarmBugAll(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setRoll(0.1) // ô 0, 1, 2 có sâu
	e.plant(alice.ID, 0, "rau_muong")
	e.plant(alice.ID, 1, "rau_muong")
	e.advance(2 * time.Minute) // 40%: sâu chưa lộ
	e.plant(alice.ID, 2, "rau_muong")
	e.setRoll(0.9)
	e.plant(alice.ID, 3, "rau_muong") // sạch
	e.advance(time.Minute)            // ô 0, 1 ở 60%; ô 2 ở 20%
	before := e.counter(alice.ID, "bug")
	bal := e.balance(alice.ID)
	res, err := e.st.FarmBugAll(e.ctx, alice.ID)
	if err != nil || res.Gained == nil || res.Gained.Bugs != 2 {
		t.Fatalf("bug all: %v %+v", err, res.Gained)
	}
	if got := e.counter(alice.ID, "bug") - before; got != 2 {
		t.Fatalf("quest progress +%d", got)
	}
	if e.balance(alice.ID) != bal {
		t.Fatalf("balance %d -> %d", bal, e.balance(alice.ID))
	}
	_, err = e.st.FarmBugAll(e.ctx, alice.ID)
	wantErr(t, err, store.ErrFarmNothingBug)
}

func TestFarmAutoCatchesBugBeforeHarvest(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setRoll(0.1)
	e.plant(alice.ID, 0, "rau_muong")
	e.setRoll(0.9)
	e.advance(10 * time.Minute) // chín, còn sâu
	res, err := e.st.FarmAuto(e.ctx, alice.ID, store.AutoOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if a := res.Gained.Auto; a.Bugs != 1 || a.Harvested != 1 {
		t.Fatalf("report: %+v", a)
	}
	if n := res.Gained.Items["rau_muong"]; n != 3 {
		t.Fatalf("harvest yield %d, want 3 (no bug penalty)", n)
	}
	if res.Gained.Bugs != 1 {
		t.Fatalf("gained bugs %d", res.Gained.Bugs)
	}
}
