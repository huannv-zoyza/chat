package controller_test

import (
	"testing"
	"time"

	"chat/store"
)

func TestFarmAutoAPI(t *testing.T) {
	e := newEnv(t)
	clk := e.clock(time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC))
	e.st.SetRand(func() float64 { return 0.9 })
	alice := e.register("alice", "Alice")

	if code, v := e.farm("POST", "/api/farm/auto", alice.Token, map[string]any{"buy_feed": true}); code != 400 ||
		v.Error != store.ErrFarmNothingAuto.Error() {
		t.Fatalf("nothing: %d %q", code, v.Error)
	}

	var raw map[string]any
	if code := e.do("POST", "/api/farm/plant", alice.Token, map[string]any{"all": true, "crop": "rau_muong"}, &raw); code != 200 {
		t.Fatalf("plant all: %d %v", code, raw)
	}
	if g, _ := raw["gained"].(map[string]any); g == nil || g["planted"] != float64(6) {
		t.Fatalf("plant all gained: %v", raw["gained"])
	}
	if code, v := e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"all": true, "crop": "rau_muong"}); code != 400 ||
		v.Error != store.ErrFarmNoEmptyPlot.Error() {
		t.Fatalf("plant all full: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"crop": "rau_muong"}); code != 400 {
		t.Fatalf("plant without plot: %d %q", code, v.Error)
	}

	clk.advance(15 * time.Minute)
	raw = nil
	code := e.do("POST", "/api/farm/auto", alice.Token,
		map[string]any{"crop": "rau_muong", "buy_feed": true}, &raw)
	if code != 200 {
		t.Fatalf("auto: %d %v", code, raw)
	}
	g, _ := raw["gained"].(map[string]any)
	a, _ := g["auto"].(map[string]any)
	if a == nil || a["harvested"] != float64(6) || a["planted"] != float64(6) || a["spent"] != float64(60) ||
		keys(a) != "collected,crafted,fed,fruits,harvested,planted,skipped,spent" {
		t.Fatalf("auto report: %v", g)
	}
}
