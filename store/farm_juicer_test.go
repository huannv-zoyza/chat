package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"chat/database"
	"chat/store"
)

// Máy ép nước và chế biến theo từng công trình (docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md, phần B).

// newFarmEnvDB: như newFarmEnv nhưng giữ lại db để chỉnh ví.
func newFarmEnvDB(t *testing.T) (*farmEnv, *sql.DB) {
	t.Helper()
	store.GiveStarterFeed = false
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(database.MigrationOpt{DB: db, Mode: database.ModeUp}); err != nil {
		t.Fatal(err)
	}
	e := &farmEnv{t: t, ctx: t.Context(), st: store.New(db), roll: 0.9,
		now: time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)}
	e.st.SetClock(func() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.now })
	e.st.SetRand(func() float64 { e.mu.Lock(); defer e.mu.Unlock(); return e.roll })
	return e, db
}

func jobsOf(v store.FarmView, building string) []store.Job {
	var out []store.Job
	for _, j := range v.Jobs {
		if j.Building == building {
			out = append(out, j)
		}
	}
	return out
}

func TestFarmJuicer(t *testing.T) {
	e, db := newFarmEnvDB(t)
	alice := e.user("alice")
	_, err := e.st.FarmCraft(e.ctx, alice.ID, "nuoc_cam")
	wantErr(t, err, store.ErrFarmNoJuicer)
	e.setXP(alice.ID, levelXPForTest(6))
	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "juicer"); err != nil {
		t.Fatal(err)
	}
	if e.balance(alice.ID) != 200 {
		t.Fatalf("balance %d", e.balance(alice.ID))
	}
	// xưởng vẫn cần xây xưởng
	e.setStock(alice.ID, "rau_muong", 2)
	e.setStock(alice.ID, "ca_rot", 1)
	e.setStock(alice.ID, "ca_chua", 1)
	_, err = e.st.FarmCraft(e.ctx, alice.ID, "salad")
	wantErr(t, err, store.ErrFarmNoKitchen)

	e.setStock(alice.ID, "cam", 3)
	if _, err := e.st.FarmCraft(e.ctx, alice.ID, "nuoc_cam"); err != nil {
		t.Fatal(err)
	}
	v := e.view(alice.ID, alice.ID)
	if len(v.Jobs) != 1 || v.Jobs[0].Building != "juicer" || v.Jobs[0].Slot != 0 || v.Jobs[0].Recipe != "nuoc_cam" {
		t.Fatalf("jobs: %+v", v.Jobs)
	}
	if b := v.Buildings[3]; b.ID != "juicer" || !b.Built || b.Cap != 2 {
		t.Fatalf("juicer: %+v", b)
	}
	// nhà bếp không bị ảnh hưởng: cả hai lò máy ép bận thì báo theo máy ép
	e.setStock(alice.ID, "cam", 6)
	if _, err := e.st.FarmCraft(e.ctx, alice.ID, "nuoc_cam"); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmCraft(e.ctx, alice.ID, "nuoc_cam")
	wantMsg(t, err, "Các lò của Máy ép nước đều đang bận.")

	e.advance(20 * time.Minute)
	res, err := e.st.FarmCraftCollect(e.ctx, alice.ID, "juicer", 0, true)
	if err != nil || res.Gained.Items["nuoc_cam"] != 2 || res.Gained.XP != 16 {
		t.Fatalf("collect: %v %+v", err, res.Gained)
	}
	// nâng cấp: 4 lò
	if _, err := db.Exec(`UPDATE wallets SET balance = balance + 5000 WHERE user_id = ?`, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.FarmUpgradeBuilding(e.ctx, alice.ID, "juicer"); err != nil {
		t.Fatal(err)
	}
	if b := e.view(alice.ID, alice.ID).Buildings[3]; b.Cap != 4 {
		t.Fatalf("upgraded juicer: %+v", b)
	}
	e.setStock(alice.ID, "cam", 12)
	for range 4 {
		if _, err := e.st.FarmCraft(e.ctx, alice.ID, "nuoc_cam"); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(jobsOf(e.view(alice.ID, alice.ID), "juicer")); n != 4 {
		t.Fatalf("jobs %d", n)
	}
}

func TestFarmJuicerJobsSeparate(t *testing.T) {
	e, db := newFarmEnvDB(t)
	alice := e.user("alice")
	e.setXP(alice.ID, levelXPForTest(6))
	e.balance(alice.ID) // tạo ví
	if _, err := db.Exec(`UPDATE wallets SET balance = balance + 5000 WHERE user_id = ?`, alice.ID); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"kitchen", "juicer"} {
		if _, err := e.st.FarmBuild(e.ctx, alice.ID, b); err != nil {
			t.Fatal(b, err)
		}
	}
	e.setStock(alice.ID, "cam", 3)
	e.setStock(alice.ID, "ngo", 2)
	for _, r := range []string{"nuoc_cam", "bap_rang"} {
		if _, err := e.st.FarmCraft(e.ctx, alice.ID, r); err != nil {
			t.Fatal(r, err)
		}
	}
	v := e.view(alice.ID, alice.ID)
	if len(jobsOf(v, "kitchen")) != 1 || len(jobsOf(v, "juicer")) != 1 || jobsOf(v, "kitchen")[0].Slot != 0 || jobsOf(v, "juicer")[0].Slot != 0 {
		t.Fatalf("jobs: %+v", v.Jobs)
	}
	e.advance(time.Hour)
	// thu theo công trình: chỉ máy ép
	res, err := e.st.FarmCraftCollect(e.ctx, alice.ID, "juicer", 0, true)
	if err != nil || len(res.Gained.Items) != 1 || res.Gained.Items["nuoc_cam"] != 1 {
		t.Fatalf("juicer collect: %v %+v", err, res.Gained)
	}
	// building rỗng + all: mọi công trình
	e.setStock(alice.ID, "cam", 3)
	if _, err := e.st.FarmCraft(e.ctx, alice.ID, "nuoc_cam"); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Hour)
	res, err = e.st.FarmCraftCollect(e.ctx, alice.ID, "", 0, true)
	if err != nil || res.Gained.Items["nuoc_cam"] != 1 || res.Gained.Items["bap_rang"] != 1 {
		t.Fatalf("collect everything: %v %+v", err, res.Gained)
	}
}

func TestFarmCraftFill(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setXP(alice.ID, 160)
	if _, err := e.st.FarmBuild(e.ctx, alice.ID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	stock := func(n int64) {
		e.setStock(alice.ID, "rau_muong", 2*n)
		e.setStock(alice.ID, "ca_rot", n)
		e.setStock(alice.ID, "ca_chua", n)
	}
	started := func() int { return len(jobsOf(e.view(alice.ID, alice.ID), "kitchen")) }
	stock(0)
	_, err := e.st.FarmCraftFill(e.ctx, alice.ID, "salad")
	wantFarmErr(t, err)
	if started() != 0 {
		t.Fatal("fill with no stock started jobs")
	}
	stock(3)
	if _, err := e.st.FarmCraftFill(e.ctx, alice.ID, "salad"); err != nil || started() != 2 {
		t.Fatalf("fill 3: %v started %d", err, started())
	}
	if got := e.storage(alice.ID).Stock["ca_rot"]; got != 1 {
		t.Fatalf("ca_rot left %d, want 1", got)
	}
	// lò đầy: fill thêm báo bận
	_, err = e.st.FarmCraftFill(e.ctx, alice.ID, "salad")
	wantErr(t, err, store.ErrFarmKitchenBusy)
	e.advance(15 * time.Minute)
	if _, err := e.st.FarmCraftCollect(e.ctx, alice.ID, "kitchen", 0, true); err != nil {
		t.Fatal(err)
	}
	stock(1)
	if _, err := e.st.FarmCraftFill(e.ctx, alice.ID, "salad"); err != nil || started() != 1 {
		t.Fatalf("fill 1: %v started %d", err, started())
	}
}

func TestFarmJuiceNoArbitrage(t *testing.T) {
	checked := 0
	for _, r := range store.Recipes {
		if r.Building != "juicer" {
			continue
		}
		checked++
		if r.Level < 6 {
			t.Fatalf("%s level %d", r.ID, r.Level)
		}
	}
	if checked != 7 {
		t.Fatalf("juice recipes = %d, want 7", checked)
	}
	// phần đối chiếu giá mua nằm ở test nội bộ (sellValue, buyPrice)
}

func TestFarmJuicerMigrationKeepsKitchenJobs(t *testing.T) {
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
	applied := func() bool {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = 'farm_juicer'`).Scan(&n)
		return n > 0
	}
	migrate(database.ModeUp, 0)
	for applied() {
		migrate(database.ModeDown, 1)
	}

	store.GiveStarterFeed = false
	st := store.New(db)
	now := time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })
	st.SetRand(func() float64 { return 0.9 })
	ctx := t.Context()
	u, err := st.CreateUser(ctx, "old", "old@x.vn", "h", "Old")
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO farms (user_id, xp, plots) VALUES (?, 1000, 6)`, u.ID)
	exec(`INSERT INTO farm_buildings (user_id, building, level) VALUES (?, 'kitchen', 1)`, u.ID)
	exec(`INSERT INTO farm_jobs (user_id, slot, recipe, ready_at) VALUES (?, 1, 'salad', ?)`, u.ID, now.Unix()-60)

	check := func(label string, slot int) {
		t.Helper()
		v, err := st.GetFarm(ctx, u.ID, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Jobs) != 1 || v.Jobs[0].Building != "kitchen" || v.Jobs[0].Slot != slot || v.Jobs[0].State != "ready" {
			t.Fatalf("%s: jobs %+v", label, v.Jobs)
		}
	}
	migrate(database.ModeUp, 0)
	if !applied() {
		t.Fatal("farm_juicer not applied")
	}
	check("after up", 1)
	// collectable at the same slot
	res, err := st.FarmCraftCollect(ctx, u.ID, "kitchen", 1, false)
	if err != nil || res.Gained.Items["salad"] != 1 {
		t.Fatalf("collect: %v %+v", err, res.Gained)
	}
	exec(`INSERT INTO farm_jobs (user_id, building, slot, recipe, ready_at) VALUES (?, 'kitchen', 0, 'salad', ?)`, u.ID, now.Unix()-60)
	exec(`INSERT INTO farm_jobs (user_id, building, slot, recipe, ready_at) VALUES (?, 'juicer', 0, 'nuoc_cam', ?)`, u.ID, now.Unix()-60)

	for applied() {
		migrate(database.ModeDown, 1)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM farm_jobs WHERE user_id = ?`, u.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("after down: %d jobs, want only the kitchen one", n)
	}
	migrate(database.ModeUp, 0)
	check("after re-up", 0)
}
