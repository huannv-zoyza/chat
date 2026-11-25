package controller_test

import (
	"context"
	"sync"
	"testing"
	"time"
)

type farmPlotView struct {
	Index      int     `json:"index"`
	State      string  `json:"state"`
	Crop       string  `json:"crop"`
	ReadyAt    string  `json:"ready_at"`
	Progress   float64 `json:"progress"`
	Bug        bool    `json:"bug"`
	Waters     int     `json:"waters"`
	Stolen     int64   `json:"stolen"`
	Steals     int     `json:"steals"`
	CanWater   bool    `json:"can_water"`
	CanBug     bool    `json:"can_bug"`
	CanSteal   bool    `json:"can_steal"`
	CanHarvest bool    `json:"can_harvest"`
}

type farmView struct {
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
	Me        bool           `json:"me"`
	Level     int            `json:"level"`
	XP        int64          `json:"xp"`
	Plots     []farmPlotView `json:"plots"`
	PlotCount int            `json:"plot_count"`
	DogUntil  *string        `json:"dog_until"`
	Wallet    *struct {
		Balance int64 `json:"balance"`
		Debt    int64 `json:"debt"`
	} `json:"wallet"`
	Quests []struct {
		ID string `json:"id"`
	} `json:"quests"`
	Log []struct {
		Action string `json:"action"`
	} `json:"log"`
	StealsLeft *int `json:"steals_left"`
	Gained     *struct {
		Xu      int64 `json:"xu"`
		XP      int64 `json:"xp"`
		LevelUp int   `json:"level_up"`
	} `json:"gained"`
	Dog   *bool  `json:"dog"`
	Error string `json:"error"`
}

func (e *env) farm(method, path, token string, body any) (int, farmView) {
	e.t.Helper()
	var v farmView
	code := e.do(method, path, token, body, &v)
	return code, v
}

func TestFarmAPI(t *testing.T) {
	e := newEnv(t)
	clk := e.clock(time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)) // 01:00 giờ VN
	var rollMu sync.Mutex
	roll := 0.9
	setRoll := func(r float64) { rollMu.Lock(); roll = r; rollMu.Unlock() }
	e.st.SetRand(func() float64 { rollMu.Lock(); defer rollMu.Unlock(); return roll })
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	ctx := context.Background()
	e.st.CreateUser(ctx, "ai", "ai@x.vn", "h", "Trợ lý AI")
	bot, _ := e.st.CreateUser(ctx, "tin_bot", "bot@x.vn", "h", "Bot")

	// khung JSON nông trại của mình
	var raw map[string]any
	if code := e.do("GET", "/api/farm", alice.Token, nil, &raw); code != 200 {
		t.Fatalf("get farm: %d", code)
	}
	for _, k := range []string{"user", "me", "level", "xp", "level_xp", "next_level_xp", "plots", "plot_count",
		"max_plots", "next_plot", "dog_until", "crops", "now", "wallet", "quests", "chest", "log", "steals_left"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("own farm missing %q: %v", k, raw)
		}
	}
	if raw["dog_until"] != nil || raw["me"] != true || len(raw["plots"].([]any)) != 6 || len(raw["log"].([]any)) != 0 {
		t.Fatalf("own farm: %v", raw)
	}
	if code := e.do("GET", "/api/farm", "", nil, nil); code != 401 {
		t.Fatalf("farm without login: %d", code)
	}

	// trồng
	if code, v := e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"plot": 0, "crop": "ca_chua"}); code != 400 ||
		v.Error != "Cần đạt cấp 2 mới trồng được Cà chua." {
		t.Fatalf("plant locked crop: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"crop": "rau_muong"}); code != 400 || v.Error == "" {
		t.Fatalf("plant without plot: %d %+v", code, v)
	}
	for i := range 3 {
		if code, v := e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"plot": i, "crop": "rau_muong"}); code != 200 ||
			v.Plots[i].State != "growing" || v.Wallet.Balance != 1000-10*int64(i+1) {
			t.Fatalf("plant %d: %d %+v", i, code, v)
		}
	}
	if code, v := e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"plot": 0, "crop": "rau_muong"}); code != 400 ||
		v.Error != "Ô này đang có cây rồi." {
		t.Fatalf("plant busy plot: %d %q", code, v.Error)
	}

	// bob ghé, tưới giúp: alice nhận farm.updated, bob nhận wallet.updated
	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	wb := e.dial(bob.Token)
	wb.waitFor("ready")
	code, v := e.farm("GET", "/api/farm/users/"+itoa(alice.User.ID), bob.Token, nil)
	if code != 200 || v.Me || v.Wallet != nil || v.Quests != nil || v.Log != nil || v.StealsLeft == nil || *v.StealsLeft != 5 ||
		!v.Plots[0].CanWater || v.Plots[0].CanHarvest {
		t.Fatalf("bob visits alice: %d %+v", code, v)
	}
	code, v = e.farm("POST", "/api/farm/water", bob.Token, map[string]any{"user_id": alice.User.ID, "plot": 0})
	if code != 200 || v.User.ID != alice.User.ID || v.Plots[0].Waters != 1 || v.Plots[0].CanWater ||
		v.Gained == nil || v.Gained.Xu != 2 || v.Gained.XP != 1 {
		t.Fatalf("bob waters: %d %+v", code, v)
	}
	ev := wa.waitFor("farm.updated")
	if log, _ := ev["log"].(map[string]any); log["action"] != "water" || log["crop"] != "rau_muong" ||
		log["actor"].(map[string]any)["id"] != float64(bob.User.ID) {
		t.Fatalf("farm.updated: %v", ev)
	}
	if ev := wb.waitFor("wallet.updated"); ev["balance"] != float64(1002) {
		t.Fatalf("bob wallet.updated: %v", ev)
	}
	if code, v := e.farm("POST", "/api/farm/water", bob.Token, map[string]any{"user_id": alice.User.ID, "plot": 0}); code != 400 ||
		v.Error != "Bạn đã tưới vụ này rồi." {
		t.Fatalf("water twice: %d %q", code, v.Error)
	}
	// không có user_id: tưới nông trại của mình
	if code, v := e.farm("POST", "/api/farm/water", alice.Token, map[string]any{"plot": 1}); code != 200 || !v.Me ||
		v.Plots[1].Waters != 1 || v.Gained != nil {
		t.Fatalf("self water: %d %+v", code, v)
	}

	// chín: trộm, thu hoạch
	clk.advance(5 * time.Minute)
	if code, v := e.farm("POST", "/api/farm/steal", alice.Token, map[string]any{"user_id": alice.User.ID, "plot": 0}); code != 400 ||
		v.Error != "Không trộm được nông trại của chính mình." {
		t.Fatalf("self steal: %d %q", code, v.Error)
	}
	code, v = e.farm("POST", "/api/farm/steal", bob.Token, map[string]any{"user_id": alice.User.ID, "plot": 0})
	if code != 200 || v.Dog == nil || *v.Dog || v.Gained == nil || v.Gained.Xu != 2 || v.Plots[0].Stolen != 2 || *v.StealsLeft != 4 {
		t.Fatalf("steal: %d %+v", code, v)
	}
	if ev := wa.waitFor("farm.updated"); ev["log"].(map[string]any)["action"] != "steal" {
		t.Fatalf("steal event: %v", ev)
	}
	code, v = e.farm("POST", "/api/farm/harvest", alice.Token, map[string]any{"all": true})
	if code != 200 || v.Gained == nil || v.Gained.Xu != 12+14+14 || v.Gained.XP != 6 || v.Plots[0].State != "empty" || v.XP != 6 {
		t.Fatalf("harvest all: %d %+v", code, v)
	}
	if code, v := e.farm("POST", "/api/farm/harvest", alice.Token, map[string]any{"plot": 0}); code != 400 ||
		v.Error != "Ô này chưa trồng gì." {
		t.Fatalf("harvest empty: %d %q", code, v.Error)
	}
	if code, _ := e.farm("POST", "/api/farm/harvest", alice.Token, map[string]any{}); code != 400 {
		t.Fatalf("harvest without plot: %d", code)
	}

	// chó giữ nhà: bị đuổi thì mất 10 xu cho chủ
	if code, v := e.farm("POST", "/api/farm/buy-dog", alice.Token, nil); code != 200 || v.DogUntil == nil {
		t.Fatalf("buy dog: %d %+v", code, v)
	}
	e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"plot": 0, "crop": "rau_muong"})
	clk.advance(5 * time.Minute)
	setRoll(0.1)
	code, v = e.farm("POST", "/api/farm/steal", bob.Token, map[string]any{"user_id": alice.User.ID, "plot": 0})
	if code != 200 || v.Dog == nil || !*v.Dog || v.Gained.Xu != -10 {
		t.Fatalf("dog chase: %d %+v", code, v)
	}
	setRoll(0.9)
	_, mine := e.farm("GET", "/api/farm", alice.Token, nil)
	if len(mine.Log) != 3 || mine.Log[0].Action != "dog" || mine.Log[1].Action != "steal" || mine.Log[2].Action != "water" {
		t.Fatalf("log: %+v", mine.Log)
	}

	// ô đất, nhiệm vụ
	if code, v := e.farm("POST", "/api/farm/buy-plot", alice.Token, nil); code != 400 ||
		v.Error != "Cần đạt cấp 2 mới mua được ô đất thứ 7." {
		t.Fatalf("buy plot at level 1: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/quests/"+mine.Quests[0].ID+"/claim", alice.Token, nil); code != 400 ||
		v.Error != "Nhiệm vụ chưa hoàn thành." {
		t.Fatalf("claim undone quest: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/quests/khong_co/claim", alice.Token, nil); code != 400 ||
		v.Error != "Hôm nay không có nhiệm vụ này." {
		t.Fatalf("claim unknown quest: %d %q", code, v.Error)
	}

	// hàng xóm, bảng xếp hạng: không có bot, AI, chính mình
	var neighbors []struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
		Ripe      int  `json:"ripe"`
		Stealable int  `json:"stealable"`
		NeedsHelp int  `json:"needs_help"`
		Dog       bool `json:"dog"`
	}
	if code := e.do("GET", "/api/farm/neighbors", bob.Token, nil, &neighbors); code != 200 || len(neighbors) != 1 ||
		neighbors[0].User.ID != alice.User.ID || !neighbors[0].Dog || neighbors[0].Ripe != 1 || neighbors[0].Stealable != 0 {
		t.Fatalf("neighbors: %d %+v", code, neighbors)
	}
	var board []struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
		Level int   `json:"level"`
		XP    int64 `json:"xp"`
	}
	if code := e.do("GET", "/api/farm/leaderboard", bob.Token, nil, &board); code != 200 || len(board) != 2 ||
		board[0].User.ID != alice.User.ID || board[0].XP != 6 {
		t.Fatalf("leaderboard: %d %+v", code, board)
	}
	if code, v := e.farm("GET", "/api/farm/users/"+itoa(bot.ID), bob.Token, nil); code != 404 || v.Error != "Không tìm thấy nông trại." {
		t.Fatalf("bot farm: %d %q", code, v.Error)
	}
	if code, _ := e.farm("GET", "/api/farm/users/99999", bob.Token, nil); code != 404 {
		t.Fatalf("missing farm: %d", code)
	}
	if code, _ := e.farm("POST", "/api/farm/water", bob.Token, map[string]any{"user_id": bot.ID, "plot": 0}); code != 404 {
		t.Fatalf("water bot farm: %d", code)
	}

	// không đủ xu
	e.do("POST", "/api/wallet/transfer", bob.Token, map[string]any{"to_user_id": alice.User.ID, "amount": e.wallet(bob.Token).Balance}, nil)
	if code, v := e.farm("POST", "/api/farm/buy-dog", bob.Token, nil); code != 400 || v.Error != "Không đủ xu." {
		t.Fatalf("broke dog: %d %q", code, v.Error)
	}
}
