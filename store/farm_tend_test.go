package store_test

import (
	"fmt"
	"testing"
	"time"

	"chat/store"
)

// Chăm nhanh cả chuồng: thu sản phẩm + cho ăn, tự mua thức ăn thiếu.

func (e *farmEnv) buyPrice(item string) int64 {
	e.t.Helper()
	for _, it := range store.Items {
		if it.ID == item {
			return it.BuyPrice
		}
	}
	e.t.Fatalf("no item %s", item)
	return 0
}

// tendFarm: alice cấp cao, đủ xu, đã xây chuồng gà + chuồng thú, chưa nuôi con nào.
func tendFarm(t *testing.T, xp int64) (*farmEnv, int64) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	if _, _, _, err := e.st.TransferCoins(e.ctx, bob.ID, alice.ID, 1000, ""); err != nil {
		t.Fatal(err)
	}
	e.setXP(alice.ID, xp)
	for _, b := range []string{"coop", "pasture"} {
		if _, err := e.st.FarmBuild(e.ctx, alice.ID, b); err != nil {
			t.Fatal(err)
		}
	}
	return e, alice.ID
}

func (e *farmEnv) buyAnimals(id int64, kinds ...string) {
	e.t.Helper()
	for _, k := range kinds {
		if _, err := e.st.FarmBuyAnimal(e.ctx, id, k); err != nil {
			e.t.Fatalf("buy %s: %v", k, err)
		}
	}
}

// leaveCoins: chuyển bớt xu cho bob để ví của id còn đúng n.
func (e *farmEnv) leaveCoins(id, n int64) {
	e.t.Helper()
	bob := e.user("drain_" + fmt.Sprint(id, n))
	if d := e.balance(id) - n; d > 0 {
		if _, _, _, err := e.st.TransferCoins(e.ctx, id, bob.ID, d, ""); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *farmEnv) states(id int64) []string {
	var out []string
	for _, a := range e.view(id, id).Animals {
		out = append(out, a.State)
	}
	return out
}

func TestFarmTendCollectAndFeed(t *testing.T) {
	e, id := tendFarm(t, 400)
	e.buyAnimals(id, "ga", "ga", "bo")
	e.setStock(id, "rau_muong", 2)
	bal := e.balance(id)

	res, err := e.st.FarmTend(e.ctx, id, "coop")
	if err != nil {
		t.Fatal(err)
	}
	if g := res.Gained; g.Fed != 2 || g.Xu != 0 || g.Bought != nil || g.Hungry != 0 {
		t.Fatalf("first tend: %+v", g)
	}
	if got := e.states(id); got[0] != "producing" || got[1] != "producing" || got[2] != "hungry" {
		t.Fatalf("states: %v (bò phải còn đói, chuồng thú không bị đụng)", got)
	}
	if e.balance(id) != bal {
		t.Fatalf("balance changed: %d -> %d", bal, e.balance(id))
	}

	e.advance(20 * time.Minute)
	e.setStock(id, "rau_muong", 2)
	res, err = e.st.FarmTend(e.ctx, id, "coop")
	if err != nil {
		t.Fatal(err)
	}
	if g := res.Gained; g.Items["trung"] != 2 || g.Fed != 2 || g.XP != 4 {
		t.Fatalf("second tend: %+v", g)
	}
	if got := e.states(id); got[0] != "producing" || got[1] != "producing" {
		t.Fatalf("states: %v", got)
	}
	if st := e.storage(id); st.Stock["trung"] != 2 {
		t.Fatalf("stock: %+v", st)
	}

	_, err = e.st.FarmTend(e.ctx, id, "coop")
	wantErr(t, err, store.ErrFarmNothingTend)
	_, err = e.st.FarmTend(e.ctx, id, "kitchen")
	wantErr(t, err, store.ErrFarmUnknownBuild)
	_, err = e.st.FarmTend(e.ctx, id, "xyz")
	wantErr(t, err, store.ErrFarmUnknownBuild)
}

func TestFarmTendAutoBuy(t *testing.T) {
	e, id := tendFarm(t, 400)
	e.buyAnimals(id, "ga", "ga", "ga", "cuu")
	price := e.buyPrice("rau_muong")
	e.setStock(id, "rau_muong", 1)
	bal := e.balance(id)

	res, err := e.st.FarmTend(e.ctx, id, "coop")
	if err != nil {
		t.Fatal(err)
	}
	g := res.Gained
	if g.Fed != 3 || g.Bought["rau_muong"] != 2 || g.Xu != -2*price || g.Hungry != 0 {
		t.Fatalf("auto buy: %+v", g)
	}
	if e.balance(id) != bal-2*price {
		t.Fatalf("balance %d, want %d", e.balance(id), bal-2*price)
	}
	if st := e.storage(id); st.Stock["rau_muong"] != 0 || st.Used != 0 {
		t.Fatalf("storage: %+v", st)
	}

	// Cừu cần 2 cà rốt, kho có 1: lấy 1, mua 1.
	e.setStock(id, "ca_rot", 1)
	bal = e.balance(id)
	res, err = e.st.FarmTend(e.ctx, id, "pasture")
	if err != nil {
		t.Fatal(err)
	}
	carrot := e.buyPrice("ca_rot")
	if g := res.Gained; g.Fed != 1 || g.Bought["ca_rot"] != 1 || g.Xu != -carrot {
		t.Fatalf("sheep: %+v", g)
	}
	if e.balance(id) != bal-carrot || e.storage(id).Stock["ca_rot"] != 0 {
		t.Fatalf("sheep balance/stock: %d %+v", e.balance(id), e.storage(id))
	}
}

func TestFarmTendPartialAffordability(t *testing.T) {
	e, id := tendFarm(t, 400)
	e.buyAnimals(id, "ga", "ga", "ga")
	price := e.buyPrice("rau_muong")

	e.leaveCoins(id, price+price/2) // đủ mua đúng một phần
	bal := e.balance(id)
	res, err := e.st.FarmTend(e.ctx, id, "coop")
	if err != nil {
		t.Fatal(err)
	}
	if g := res.Gained; g.Fed != 1 || g.Hungry != 2 || g.Bought["rau_muong"] != 1 || g.Xu != -price {
		t.Fatalf("partial: %+v", g)
	}
	if e.balance(id) != bal-price {
		t.Fatalf("balance %d, want %d", e.balance(id), bal-price)
	}

	e.leaveCoins(id, price-1)
	bal = e.balance(id)
	_, err = e.st.FarmTend(e.ctx, id, "coop")
	wantErr(t, err, store.ErrFarmNoXuFeed)
	if e.balance(id) != bal {
		t.Fatalf("balance changed on failure: %d -> %d", bal, e.balance(id))
	}
}

func TestFarmTendStorageFull(t *testing.T) {
	e, id := tendFarm(t, 400)
	e.buyAnimals(id, "ga", "ga")
	e.setStock(id, "rau_muong", 2)
	if _, err := e.st.FarmTend(e.ctx, id, "coop"); err != nil {
		t.Fatal(err)
	}
	e.advance(20 * time.Minute) // cả hai ready
	e.setStock(id, "trung", e.storage(id).Cap)
	price := e.buyPrice("rau_muong")

	// Kho đầy: thu 0, nhưng ready không đói => lỗi kho đầy.
	_, err := e.st.FarmTend(e.ctx, id, "coop")
	wantErr(t, err, store.ErrFarmStorageFull)

	// Có con đói (thêm con mới): vẫn mua thức ăn và cho ăn dù kho đầy.
	e.buyAnimals(id, "ga")
	bal := e.balance(id)
	res, err := e.st.FarmTend(e.ctx, id, "coop")
	if err != nil {
		t.Fatal(err)
	}
	g := res.Gained
	if len(g.Items) != 0 || g.XP != 0 || g.Fed != 1 || g.Bought["rau_muong"] != 1 || g.Xu != -price {
		t.Fatalf("full storage tend: %+v", g)
	}
	if e.balance(id) != bal-price {
		t.Fatalf("balance %d want %d", e.balance(id), bal-price)
	}
	if st := e.storage(id); st.Used != st.Cap {
		t.Fatalf("storage changed: %+v", st)
	}
}

func TestFarmTendLevelGate(t *testing.T) {
	e, id := tendFarm(t, 600) // heo cấp 6
	e.buyAnimals(id, "heo")
	e.setXP(id, 150) // cấp 4 < cấp của dâu tây
	var item store.Item
	for _, it := range store.Items {
		if it.ID == "dau_tay" {
			item = it
		}
	}
	if item.Level <= 1 {
		t.Fatalf("dau_tay level %d", item.Level)
	}
	e.setStock(id, "ca_rot", 1) // thiếu thức ăn: kho không đủ, phải mua mà chưa đủ cấp
	bal := e.balance(id)
	_, err := e.st.FarmTend(e.ctx, id, "pasture")
	wantMsg(t, err, fmt.Sprintf("Cần đạt cấp %d mới mua được %s.", item.Level, item.Name))
	if e.balance(id) != bal {
		t.Fatalf("balance changed: %d -> %d", bal, e.balance(id))
	}
	if st := e.storage(id); st.Stock["ca_rot"] != 1 {
		t.Fatalf("stock touched: %+v", st)
	}
	if got := e.states(id); len(got) != 1 || got[0] != "hungry" {
		t.Fatalf("states: %v", got)
	}
}

func TestFarmTendStorageRoomCapsCollect(t *testing.T) {
	e, id := tendFarm(t, 400)
	e.buyAnimals(id, "ga", "ga")
	e.setStock(id, "rau_muong", 2)
	if _, err := e.st.FarmTend(e.ctx, id, "coop"); err != nil {
		t.Fatal(err)
	}
	e.advance(20 * time.Minute) // cả hai ready
	e.setStock(id, "trung", e.storage(id).Cap-1)
	price := e.buyPrice("rau_muong")

	res, err := e.st.FarmTend(e.ctx, id, "coop")
	if err != nil {
		t.Fatal(err)
	}
	g := res.Gained
	if g.Items["trung"] != 1 || g.Fed != 1 || g.Hungry != 0 || g.Bought["rau_muong"] != 1 || g.Xu != -price {
		t.Fatalf("capped tend: %+v", g)
	}
	got := e.states(id)
	producing, ready := 0, 0
	for _, s := range got {
		switch s {
		case "producing":
			producing++
		case "ready":
			ready++
		}
	}
	if producing != 1 || ready != 1 {
		t.Fatalf("states: %v (con chưa thu phải còn ready, không được cho ăn)", got)
	}
}
