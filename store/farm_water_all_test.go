package store_test

import (
	"testing"
	"time"

	"chat/store"
)

// Tưới tất cả và bước "Tưới" của /auto (docs/superpowers/specs/2026-11-27-farm-water-all-design.md).

func readyAt(t *testing.T, e *farmEnv, id int64, idx int) time.Time {
	t.Helper()
	p := e.view(id, id).Plots[idx]
	tm, err := time.Parse(time.RFC3339, p.ReadyAt)
	if err != nil {
		t.Fatalf("ready_at %q: %v", p.ReadyAt, err)
	}
	return tm
}

func TestFarmWaterAll(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	for i := 0; i < 3; i++ {
		e.plant(alice.ID, i, "rau_muong") // 300s
	}
	before := []time.Time{readyAt(t, e, alice.ID, 0), readyAt(t, e, alice.ID, 1), readyAt(t, e, alice.ID, 2)}
	bal := e.balance(alice.ID)
	res, err := e.st.FarmWaterAll(e.ctx, alice.ID)
	if err != nil || res.Gained == nil || res.Gained.Watered != 3 {
		t.Fatalf("water all: %v %+v", err, res.Gained)
	}
	for i := 0; i < 3; i++ {
		if d := before[i].Sub(readyAt(t, e, alice.ID, i)); d != 30*time.Second {
			t.Fatalf("plot %d shortened by %v", i, d)
		}
	}
	if st := e.view(alice.ID, alice.ID).Plots[3].State; st != "empty" {
		t.Fatalf("plot 3 %s", st)
	}
	_, err = e.st.FarmWaterAll(e.ctx, alice.ID)
	wantErr(t, err, store.ErrFarmNothingWater)
	if e.balance(alice.ID) != bal {
		t.Fatalf("balance %d -> %d", bal, e.balance(alice.ID))
	}
}

func TestFarmWaterAllSkipsRipe(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.plant(alice.ID, 0, "rau_muong")
	e.advance(10 * time.Minute) // chín
	e.plant(alice.ID, 1, "rau_muong")
	res, err := e.st.FarmWaterAll(e.ctx, alice.ID)
	if err != nil || res.Gained.Watered != 1 {
		t.Fatalf("water all: %v %+v", err, res.Gained)
	}
}

func TestFarmWaterAllSkipsMaxed(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.plant(alice.ID, 0, "rau_muong")
	for _, n := range []string{"bob", "carol", "dave"} {
		u := e.user(n)
		if _, err := e.st.FarmWater(e.ctx, u.ID, alice.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	e.plant(alice.ID, 1, "rau_muong")
	res, err := e.st.FarmWaterAll(e.ctx, alice.ID)
	if err != nil || res.Gained.Watered != 1 {
		t.Fatalf("water all: %v %+v", err, res.Gained)
	}
	if w := e.view(alice.ID, alice.ID).Plots[0].Waters; w != 3 {
		t.Fatalf("plot 0 waters %d", w)
	}
}

func TestFarmAutoWaters(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	res, err := e.st.FarmAuto(e.ctx, alice.ID, store.AutoOpts{Crop: "rau_muong"})
	if err != nil {
		t.Fatal(err)
	}
	a := res.Gained.Auto
	if a.Planted == 0 || a.Watered != a.Planted {
		t.Fatalf("report: %+v", a)
	}
	// chỉ còn việc tưới: ô mới gieo tay chưa tưới
	e2 := newFarmEnv(t)
	bob := e2.user("bob")
	e2.plant(bob.ID, 0, "rau_muong")
	res, err = e2.st.FarmAuto(e2.ctx, bob.ID, store.AutoOpts{})
	if err != nil || res.Gained.Auto.Watered != 1 || res.Gained.Watered != 1 {
		t.Fatalf("only water: %v %+v", err, res.Gained)
	}
	_, err = e2.st.FarmAuto(e2.ctx, bob.ID, store.AutoOpts{})
	wantErr(t, err, store.ErrFarmNothingAuto)
}
