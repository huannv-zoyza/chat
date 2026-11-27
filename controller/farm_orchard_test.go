package controller_test

import (
	"testing"
	"time"

	"chat/store"
)

func TestFarmOrchardAPI(t *testing.T) {
	e := newEnv(t)
	clk := e.clock(time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC))
	e.st.SetRand(func() float64 { return 0.9 })
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")

	// cấp 1: vườn khoá, chỗ 0 báo lỗi 400
	if code, v := e.farm("POST", "/api/farm/orchard/plant", alice.Token, map[string]any{"spot": 0, "tree": "cam"}); code != 400 ||
		v.Error != store.ErrFarmSpotLocked.Error() {
		t.Fatalf("locked: %d %q", code, v.Error)
	}
	e.farmRaw(alice.Token) // tạo hàng farms
	if _, err := e.db.Exec(`UPDATE farms SET xp = 280 WHERE user_id = ?`, alice.User.ID); err != nil {
		t.Fatal(err)
	}

	if code, v := e.farm("POST", "/api/farm/orchard/plant", alice.Token, map[string]any{"spot": 0, "tree": "cam"}); code != 200 {
		t.Fatalf("plant: %d %q", code, v.Error)
	}
	_, raw := e.farmRaw(alice.Token)
	trees, ok := raw["trees"].([]any)
	if !ok || len(trees) != 1 || keys(trees[0]) != "ready_at,spot,state,tree" || raw["orchard_spots"] != float64(3) {
		t.Fatalf("trees: %v spots %v", raw["trees"], raw["orchard_spots"])
	}
	kinds, ok := raw["tree_kinds"].([]any)
	if !ok || len(kinds) != 6 ||
		keys(kinds[0]) != "cycle_sec,emoji,fruit,fruit_emoji,fruit_name,fruit_price,id,level,mature_sec,name,price,xp" {
		t.Fatalf("tree_kinds: %v", raw["tree_kinds"])
	}
	// khách thấy cây nhưng không có tree_kinds
	var visitor map[string]any
	if code := e.do("GET", "/api/farm/users/"+itoa(alice.User.ID), bob.Token, nil, &visitor); code != 200 {
		t.Fatalf("visit: %d", code)
	}
	if _, has := visitor["tree_kinds"]; has || len(visitor["trees"].([]any)) != 1 {
		t.Fatalf("visitor: %v", visitor["trees"])
	}

	if code, v := e.farm("POST", "/api/farm/orchard/harvest", alice.Token, map[string]any{"spot": 0}); code != 400 ||
		v.Error != store.ErrFarmTreeNotReady.Error() {
		t.Fatalf("early harvest: %d %q", code, v.Error)
	}
	clk.advance(3 * time.Hour)
	if code, v := e.farm("POST", "/api/farm/orchard/harvest", alice.Token, map[string]any{"spot": 0}); code != 200 ||
		v.Gained == nil || v.Gained.Items["cam"] != 4 {
		t.Fatalf("harvest: %d %+v %q", code, v.Gained, v.Error)
	}
	clk.advance(3 * time.Hour)
	if code, v := e.farm("POST", "/api/farm/orchard/harvest", alice.Token, map[string]any{"all": true}); code != 200 ||
		v.Gained == nil || v.Gained.Items["cam"] != 4 {
		t.Fatalf("harvest all: %d %+v %q", code, v.Gained, v.Error)
	}
	if code, v := e.farm("DELETE", "/api/farm/orchard/0", alice.Token, nil); code != 200 {
		t.Fatalf("remove: %d %q", code, v.Error)
	}
	if code, v := e.farm("DELETE", "/api/farm/orchard/0", alice.Token, nil); code != 400 || v.Error != store.ErrFarmNoTree.Error() {
		t.Fatalf("remove empty: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/orchard/harvest", alice.Token, map[string]any{}); code != 400 {
		t.Fatalf("harvest without spot: %d %q", code, v.Error)
	}
}
