package controller_test

import (
	"testing"
	"time"

	"chat/store"
)

func TestFarmTendAPI(t *testing.T) {
	e := newEnv(t)
	clk := e.clock(time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC))
	e.st.SetRand(func() float64 { return 0.9 })
	alice := e.register("alice", "Alice")

	if code, v := e.farm("POST", "/api/farm/buildings/xyz/tend", alice.Token, nil); code != 400 ||
		v.Error != store.ErrFarmUnknownBuild.Error() {
		t.Fatalf("unknown building: %d %q", code, v.Error)
	}

	for _, crop := range []string{"rau_muong", "ca_rot"} {
		for i := range 6 {
			e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"plot": i, "crop": crop})
		}
		clk.advance(15 * time.Minute)
		e.farm("POST", "/api/farm/harvest", alice.Token, map[string]any{"all": true})
	}
	if code, _ := e.farm("POST", "/api/farm/buildings/coop", alice.Token, nil); code != 200 {
		t.Fatalf("build coop: %d", code)
	}
	if code, _ := e.farm("POST", "/api/farm/animals", alice.Token, map[string]any{"kind": "ga"}); code != 200 {
		t.Fatalf("buy hen: %d", code)
	}
	e.farm("POST", "/api/farm/sell", alice.Token, map[string]any{"all": true}) // kho trống
	// kho chưa có thức ăn -> tự mua
	var raw map[string]any
	if code := e.do("POST", "/api/farm/buildings/coop/tend", alice.Token, nil, &raw); code != 200 {
		t.Fatalf("tend: %d %v", code, raw)
	}
	g, ok := raw["gained"].(map[string]any)
	if !ok || g["fed"] != float64(1) || g["bought"] == nil {
		t.Fatalf("gained: %v", raw["gained"])
	}
}
