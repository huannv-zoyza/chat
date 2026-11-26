package controller_test

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// keys: khoá của một object JSON, sắp xếp, nối bằng dấu phẩy.
func keys(v any) string {
	m, _ := v.(map[string]any)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// Nông trại phần 2 qua HTTP: khung JSON các trường mới, kho, chuồng, vật nuôi, đơn hàng, mở rộng.
func TestFarmV2API(t *testing.T) {
	e := newEnv(t)
	clk := e.clock(time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC))
	e.st.SetRand(func() float64 { return 0.9 }) // không sâu; đơn cấp 1: 4 cà rốt + 4 rau muống
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")

	var raw map[string]any
	if code := e.do("GET", "/api/farm", alice.Token, nil, &raw); code != 200 {
		t.Fatalf("get farm: %d", code)
	}
	for _, k := range []string{"items", "storage", "buildings", "animal_kinds", "animals", "orders", "expansion"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("own farm missing %q", k)
		}
	}
	items := raw["items"].([]any)
	if len(items) != 23 || keys(items[0]) != "buy_price,emoji,id,kind,level,name,price" || items[0].(map[string]any)["price"] != float64(5) ||
		items[8].(map[string]any)["id"] != "trung" || items[8].(map[string]any)["kind"] != "product" {
		t.Fatalf("items: %v", items)
	}
	storage := raw["storage"].(map[string]any)
	if keys(storage) != "cap,next_upgrade,stock,used" || storage["cap"] != float64(60) || keys(storage["next_upgrade"]) != "price" ||
		keys(storage["stock"]) != "" || storage["stock"] == nil {
		t.Fatalf("storage: %v", storage)
	}
	b := raw["buildings"].([]any)
	if len(b) != 3 || keys(b[0]) != "built,cap,id,level,name,price,upgrade_price" || b[0].(map[string]any)["upgrade_price"] != nil {
		t.Fatalf("buildings: %v", b)
	}
	kinds := raw["animal_kinds"].([]any)
	if len(kinds) != 5 || keys(kinds[0]) != "building,emoji,feed,feed_qty,id,level,name,price,product,time_sec,xp" ||
		kinds[0].(map[string]any)["time_sec"] != float64(1200) {
		t.Fatalf("animal_kinds: %v", kinds)
	}
	if a, ok := raw["animals"].([]any); !ok || len(a) != 0 {
		t.Fatalf("animals: %v", raw["animals"])
	}
	orders := raw["orders"].([]any)
	if len(orders) != 3 || keys(orders[0]) != "can_deliver,id,items,slot,xp,xu" || orders[0].(map[string]any)["xu"] != float64(81) ||
		keys(orders[0].(map[string]any)["items"].([]any)[0]) != "item,qty" || orders[0].(map[string]any)["can_deliver"] != false {
		t.Fatalf("orders: %v", orders)
	}
	exp := raw["expansion"].(map[string]any)
	if keys(exp) != "next,plots,tier" || exp["tier"] != float64(1) || exp["plots"] != float64(6) || keys(exp["next"]) != "level,plots,price" ||
		raw["max_plots"] != float64(20) {
		t.Fatalf("expansion: %v max %v", exp, raw["max_plots"])
	}
	// nhà người khác: chỉ thấy chuồng, vật nuôi, mở rộng
	raw = nil
	e.do("GET", "/api/farm/users/"+itoa(alice.User.ID), bob.Token, nil, &raw)
	for k, want := range map[string]bool{"buildings": true, "animals": true, "expansion": true,
		"items": false, "storage": false, "animal_kinds": false, "orders": false} {
		if _, ok := raw[k]; ok != want {
			t.Fatalf("neighbour view %q present=%v", k, ok)
		}
	}

	// thu hoạch vào kho, bán
	for i := range 6 {
		e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"plot": i, "crop": "rau_muong"})
	}
	clk.advance(5 * time.Minute)
	code, v := e.farm("POST", "/api/farm/harvest", alice.Token, map[string]any{"all": true})
	if code != 200 || v.Gained == nil || v.Gained.Xu != 0 || v.Gained.Items["rau_muong"] != 18 || v.Gained.XP != 12 {
		t.Fatalf("harvest: %d %+v", code, v.Gained)
	}
	for _, tc := range []struct {
		body any
		msg  string
	}{
		{map[string]any{"item": "xoai", "qty": 1}, "Không có món hàng này."},
		{map[string]any{"item": "rau_muong", "qty": 0}, "Số lượng không hợp lệ."},
		{map[string]any{"item": "rau_muong", "qty": 19}, "Trong kho không đủ 19 Rau muống để bán."},
	} {
		if code, v := e.farm("POST", "/api/farm/sell", alice.Token, tc.body); code != 400 || v.Error != tc.msg {
			t.Fatalf("sell %v: %d %q", tc.body, code, v.Error)
		}
	}
	code, v = e.farm("POST", "/api/farm/sell", alice.Token, map[string]any{"item": "rau_muong", "qty": 2})
	if code != 200 || v.Gained == nil || v.Gained.Xu != 10 || v.Wallet.Balance != 1000-60+10 {
		t.Fatalf("sell 2: %d %+v %+v", code, v.Gained, v.Wallet)
	}

	// đơn hàng
	_, mine := e.farmRaw(alice.Token)
	o := mine["orders"].([]any)[0].(map[string]any)
	oid := itoa(int64(o["id"].(float64)))
	if code, v := e.farm("POST", "/api/farm/orders/"+oid+"/deliver", alice.Token, nil); code != 400 || v.Error != "Chưa đủ hàng để giao đơn này." {
		t.Fatalf("deliver short: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/orders/abc/deliver", alice.Token, nil); code != 400 || v.Error != "Không tìm thấy đơn hàng này." {
		t.Fatalf("deliver bad id: %d %q", code, v.Error)
	}
	if code, _ := e.farm("DELETE", "/api/farm/orders/"+oid, alice.Token, nil); code != 200 {
		t.Fatalf("discard: %d", code)
	}
	_, mine = e.farmRaw(alice.Token)
	if slot := mine["orders"].([]any)[0].(map[string]any); keys(slot) != "next_at,slot" {
		t.Fatalf("empty slot: %v", slot)
	}
	if code, v := e.farm("DELETE", "/api/farm/orders/"+oid, alice.Token, nil); code != 400 || v.Error != "Không tìm thấy đơn hàng này." {
		t.Fatalf("discard twice: %d %q", code, v.Error)
	}

	// chưa đủ cấp
	for _, tc := range []struct{ method, path, msg string }{
		{"POST", "/api/farm/expand", "Cần đạt cấp 2 mới mở rộng được nông trại."},
		{"POST", "/api/farm/buildings/coop", "Cần đạt cấp 2 mới xây được Chuồng gà."},
		{"POST", "/api/farm/buildings/chuong_lon", "Không có công trình này."},
		{"POST", "/api/farm/buildings/coop/upgrade", "Chưa xây Chuồng gà."},
	} {
		if code, v := e.farm(tc.method, tc.path, alice.Token, nil); code != 400 || v.Error != tc.msg {
			t.Fatalf("%s: %d %q", tc.path, code, v.Error)
		}
	}
	if code, v := e.farm("POST", "/api/farm/animals", alice.Token, map[string]any{"kind": "ga"}); code != 400 ||
		v.Error != "Cần đạt cấp 2 mới nuôi được Gà." {
		t.Fatalf("buy hen at level 1: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/animals/feed", alice.Token, map[string]any{"id": 1}); code != 400 ||
		v.Error != "Không tìm thấy con vật này." {
		t.Fatalf("feed missing: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/animals/feed", alice.Token, map[string]any{"all": true}); code != 400 ||
		v.Error != "Không có con nào đang đói." {
		t.Fatalf("feed all none: %d %q", code, v.Error)
	}
	if code, v := e.farm("POST", "/api/farm/animals/collect", alice.Token, map[string]any{"all": true}); code != 400 ||
		v.Error != "Chưa có sản phẩm nào để thu." {
		t.Fatalf("collect all none: %d %q", code, v.Error)
	}

	// lên cấp 2 (12 + 6 × 4 = 36 XP), xây chuồng, nuôi gà, cho ăn, thu trứng
	for i := range 6 {
		e.farm("POST", "/api/farm/plant", alice.Token, map[string]any{"plot": i, "crop": "ca_rot"})
	}
	clk.advance(15 * time.Minute)
	if code, v := e.farm("POST", "/api/farm/harvest", alice.Token, map[string]any{"all": true}); code != 200 || v.Gained.LevelUp != 2 {
		t.Fatalf("level up: %d %+v", code, v.Gained)
	}
	if code, _ := e.farm("POST", "/api/farm/buildings/coop", alice.Token, nil); code != 200 {
		t.Fatalf("build coop: %d", code)
	}
	if code, _ := e.farm("POST", "/api/farm/animals", alice.Token, map[string]any{"kind": "ga"}); code != 200 {
		t.Fatalf("buy hen: %d", code)
	}
	_, mine = e.farmRaw(alice.Token)
	hen := mine["animals"].([]any)[0].(map[string]any)
	if keys(hen) != "can_feed,id,kind,state" || hen["state"] != "hungry" || hen["can_feed"] != true {
		t.Fatalf("hen: %v", hen)
	}
	if code, _ := e.farm("POST", "/api/farm/animals/feed", alice.Token, map[string]any{"id": hen["id"]}); code != 200 {
		t.Fatalf("feed: %d", code)
	}
	_, mine = e.farmRaw(alice.Token)
	if hen := mine["animals"].([]any)[0].(map[string]any); keys(hen) != "can_feed,id,kind,ready_at,state" || hen["state"] != "producing" {
		t.Fatalf("fed hen: %v", hen)
	}
	clk.advance(20 * time.Minute)
	code, v = e.farm("POST", "/api/farm/animals/collect", alice.Token, map[string]any{"id": hen["id"]})
	if code != 200 || v.Gained == nil || v.Gained.XP != 2 || v.Gained.Items["trung"] != 1 {
		t.Fatalf("collect: %d %+v", code, v.Gained)
	}

	// kho: nâng cấp; mở rộng (bí danh buy-plot cũng được)
	code, v = e.farm("POST", "/api/farm/storage/upgrade", alice.Token, nil)
	if code != 200 {
		t.Fatalf("upgrade storage: %d %q", code, v.Error)
	}
	_, mine = e.farmRaw(alice.Token)
	if st := mine["storage"].(map[string]any); st["cap"] != float64(90) || st["next_upgrade"].(map[string]any)["price"] != float64(600) {
		t.Fatalf("storage after upgrade: %v", st)
	}
	// bán sạch kho: 15 rau muống × 5 + 18 cà rốt × 10 + 1 trứng × 22 = 277 (290 → 567, đủ 400 mở rộng)
	if code, v := e.farm("POST", "/api/farm/sell", alice.Token, map[string]any{"all": true}); code != 200 || v.Gained.Xu != 277 ||
		v.Wallet.Balance != 567 {
		t.Fatalf("sell all: %d %+v %+v", code, v.Gained, v.Wallet)
	}
	code, v = e.farm("POST", "/api/farm/buy-plot", alice.Token, nil)
	if code != 200 || v.PlotCount != 9 || len(v.Plots) != 9 {
		t.Fatalf("expand via buy-plot: %d %q %d", code, v.Error, v.PlotCount)
	}
	_, mine = e.farmRaw(alice.Token)
	if exp := mine["expansion"].(map[string]any); exp["tier"] != float64(2) || exp["next"].(map[string]any)["plots"] != float64(12) {
		t.Fatalf("expansion: %v", exp)
	}
	if code, v := e.farm("POST", "/api/farm/sell", alice.Token, map[string]any{"all": true}); code != 400 ||
		v.Error != "Kho trống, không có gì để bán." {
		t.Fatalf("sell all empty: %d %q", code, v.Error)
	}
}

func (e *env) farmRaw(token string) (int, map[string]any) {
	e.t.Helper()
	var raw map[string]any
	code := e.do("GET", "/api/farm", token, nil, &raw)
	return code, raw
}
