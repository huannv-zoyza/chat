package controller_test

import (
	"testing"
	"time"

	"chat/store"
)

func TestFarmJuicerAPI(t *testing.T) {
	e := newEnv(t)
	clk := e.clock(time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC))
	e.st.SetRand(func() float64 { return 0.9 })
	alice := e.register("alice", "Alice")
	e.farmRaw(alice.Token) // tạo hàng farms
	if _, err := e.db.Exec(`UPDATE farms SET xp = 600 WHERE user_id = ?`, alice.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO farm_stock (user_id, item, qty) VALUES (?, 'cam', 12)`, alice.User.ID); err != nil {
		t.Fatal(err)
	}
	if code, v := e.farm("POST", "/api/farm/craft", alice.Token, map[string]any{"recipe": "nuoc_cam", "fill": true}); code != 400 ||
		v.Error != store.ErrFarmNoJuicer.Error() {
		t.Fatalf("no juicer: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/buildings/juicer", alice.Token, nil); code != 200 {
		t.Fatalf("build: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/craft", alice.Token, map[string]any{"recipe": "nuoc_cam", "fill": true}); code != 200 {
		t.Fatalf("fill: %d %q", code, v.Error)
	}
	_, raw := e.farmRaw(alice.Token)
	jobs, _ := raw["jobs"].([]any)
	if len(jobs) != 2 || keys(jobs[0]) != "building,ready_at,recipe,slot,state" {
		t.Fatalf("jobs: %v", raw["jobs"])
	}
	clk.advance(20 * time.Minute)
	// slot không kèm building: mặc định xưởng, nên máy ép không bị đụng
	if code, v := e.farm("POST", "/api/farm/craft/collect", alice.Token, map[string]any{"slot": 0}); code != 400 ||
		v.Error != store.ErrFarmNoJob.Error() {
		t.Fatalf("collect default kitchen: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/craft/collect", alice.Token, map[string]any{"building": "juicer", "slot": 0}); code != 200 ||
		v.Gained == nil || v.Gained.Items["nuoc_cam"] != 1 {
		t.Fatalf("collect slot: %d %+v %q", code, v.Gained, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/craft/collect", alice.Token, map[string]any{"all": true}); code != 200 ||
		v.Gained == nil || v.Gained.Items["nuoc_cam"] != 1 {
		t.Fatalf("collect all: %d %+v %q", code, v.Gained, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/craft/collect", alice.Token, map[string]any{"all": true, "building": "juicer"}); code != 400 ||
		v.Error != store.ErrFarmNothingCraft.Error() {
		t.Fatalf("collect all empty: %d %q", code, v.Error)
	}
}
