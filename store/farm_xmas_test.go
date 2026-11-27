package store_test

import (
	"testing"
	"time"

	"chat/model"
	"chat/store"
)

// Giáng sinh (§16): đơn Ông già Noel + chuỗi ngày, cửa hàng kẹo, tất Noel, tắt trộm.

// vet: người chơi lâu (cấp 10), không có ưu đãi tân thủ, để kiểm tra số kẹo gốc.
func (e *farmEnv) vet(name string) model.User {
	e.t.Helper()
	u := e.user(name)
	if err := e.st.FarmSetXPForTest(e.ctx, u.ID, store.LevelXPForTest(10)); err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *farmEnv) toXmas() {
	e.mu.Lock()
	e.now = time.Date(2026, 12, 19, 18, 0, 0, 0, time.UTC) // 01:00 ngày 20/12 giờ VN
	e.mu.Unlock()
}

// deliverAllXmas: gom đủ hàng rồi giao cả 3 đơn Noel hôm nay; trả tổng kẹo nhận được.
func (e *farmEnv) deliverAllXmas(id int64) int64 {
	e.t.Helper()
	var candy int64
	for _, o := range e.view(id, id).Xmas.Orders {
		for _, it := range o.Items {
			have := e.storage(id).Stock[it.Item]
			e.setStock(id, it.Item, have+it.Qty)
		}
		res, err := e.st.FarmXmasDeliver(e.ctx, id, o.Slot)
		if err != nil {
			e.t.Fatalf("deliver slot %d: %v", o.Slot, err)
		}
		if o.Xu <= 0 || res.Gained.Xu != o.Xu { // đơn Noel trả thêm xu (40% đơn xe tải)
			e.t.Fatalf("order xu %d, gained %d", o.Xu, res.Gained.Xu)
		}
		candy += res.Gained.Candy
	}
	return candy
}

func TestFarmXmasOffSeason(t *testing.T) {
	e := newFarmEnv(t) // 24/11: chưa tới sự kiện
	alice, bob := e.vet("alice"), e.vet("bob")
	if v := e.view(alice.ID, alice.ID); v.Xmas != nil || v.StealsLeft != store.FarmStealsPerDay {
		t.Fatalf("off season view %+v steals %d", v.Xmas, v.StealsLeft)
	}
	_, err := e.st.FarmXmasDeliver(e.ctx, alice.ID, 0)
	wantErr(t, err, store.ErrXmasOff)
	_, err = e.st.FarmXmasSock(e.ctx, alice.ID, bob.ID, "ca_chua", 1)
	wantErr(t, err, store.ErrXmasOff)
	_, err = e.st.FarmXmasBuy(e.ctx, alice.ID, "xu")
	wantErr(t, err, store.ErrXmasShopOff)
}

func TestFarmXmasOrdersStreakShop(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	alice := e.vet("alice")

	x := e.view(alice.ID, alice.ID).Xmas
	if x == nil || !x.On || len(x.Orders) != store.XmasOrders || x.Candy != 0 || x.Streak != 0 {
		t.Fatalf("xmas view %+v", x)
	}
	// mở lại không sinh thêm / đổi đơn
	if again := e.view(alice.ID, alice.ID).Xmas.Orders; again[0].Candy != x.Orders[0].Candy ||
		again[0].Items[0] != x.Orders[0].Items[0] {
		t.Fatalf("orders changed: %+v vs %+v", again[0], x.Orders[0])
	}
	_, err := e.st.FarmXmasDeliver(e.ctx, alice.ID, 0)
	if len(x.Orders[0].Items) > 0 && err != store.ErrFarmOrderShort {
		t.Fatalf("deliver without stock: %v", err)
	}

	var base int64
	for _, o := range x.Orders {
		base += o.Candy
	}
	got := e.deliverAllXmas(alice.ID)
	if got != base+store.XmasStreakCandy*1 {
		t.Fatalf("day 1 candy %d, want %d", got, base+store.XmasStreakCandy)
	}
	_, err = e.st.FarmXmasDeliver(e.ctx, alice.ID, 0)
	wantErr(t, err, store.ErrXmasOrderDone)
	x = e.view(alice.ID, alice.ID).Xmas
	if x.Candy != got || x.Streak != 1 {
		t.Fatalf("after day 1: candy %d streak %d", x.Candy, x.Streak)
	}
	if n := e.counter(alice.ID, "order"); n != store.XmasOrders {
		t.Fatalf("order quest counter %d", n)
	}

	// hôm sau: đơn mới, chuỗi 2 ngày
	e.advance(24 * time.Hour)
	x = e.view(alice.ID, alice.ID).Xmas
	base = 0
	for _, o := range x.Orders {
		base += o.Candy
		if o.Done {
			t.Fatalf("new day order already done %+v", o)
		}
	}
	if got := e.deliverAllXmas(alice.ID); got != base+2*store.XmasStreakCandy {
		t.Fatalf("day 2 candy %d, want %d", got, base+2*store.XmasStreakCandy)
	}

	// cửa hàng
	before := e.balance(alice.ID)
	candy := e.view(alice.ID, alice.ID).Xmas.Candy
	res, err := e.st.FarmXmasBuy(e.ctx, alice.ID, "xu")
	if err != nil {
		t.Fatal(err)
	}
	if res.Gained.Candy != -15 || e.balance(alice.ID) != before+500 ||
		e.view(alice.ID, alice.ID).Xmas.Candy != candy-15 {
		t.Fatalf("buy xu: %+v balance %d", res.Gained, e.balance(alice.ID))
	}
	_, err = e.st.FarmXmasBuy(e.ctx, alice.ID, "khong_co")
	wantErr(t, err, store.ErrXmasNoReward)
	// túi xu: tối đa 2 lần mỗi ngày (xu dùng chung với cả app)
	if _, err := e.st.FarmXmasBuy(e.ctx, alice.ID, "xu"); err != nil {
		t.Fatal(err)
	}
	if left := e.view(alice.ID, alice.ID).Xmas.ShopLeft["xu"]; left != 0 {
		t.Fatalf("xu bags left %d", left)
	}
	_, err = e.st.FarmXmasBuy(e.ctx, alice.ID, "xu")
	wantMsg(t, err, "Hôm nay bạn đã đổi Túi 500 xu đủ 2 lần rồi, mai đổi tiếp nhé.")
	if err := e.st.FarmSetXmasForTest(e.ctx, alice.ID, 0, 0); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmXmasBuy(e.ctx, alice.ID, "len")
	wantErr(t, err, store.ErrXmasCandy)

	// hết sự kiện: không còn đơn, cửa hàng vẫn mở tới XmasShopTo
	e.mu.Lock()
	e.now = time.Date(2027, 1, 3, 18, 0, 0, 0, time.UTC)
	e.mu.Unlock()
	if x := e.view(alice.ID, alice.ID).Xmas; x == nil || x.On || !x.ShopOpen || len(x.Orders) != 0 {
		t.Fatalf("after event %+v", x)
	}
	_, err = e.st.FarmXmasDeliver(e.ctx, alice.ID, 0)
	wantErr(t, err, store.ErrXmasOff)
}

func TestFarmXmasSocks(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	alice, bob := e.vet("alice"), e.vet("bob")
	e.setStock(alice.ID, "ca_chua", 10)

	if s := e.view(bob.ID, alice.ID).XmasSock; s != "can" {
		t.Fatalf("xmas_sock %q", s)
	}
	_, err := e.st.FarmXmasSock(e.ctx, alice.ID, alice.ID, "ca_chua", 1)
	wantErr(t, err, store.ErrXmasSockSelf)
	_, err = e.st.FarmXmasSock(e.ctx, alice.ID, bob.ID, "ca_chua", 6)
	wantErr(t, err, store.ErrXmasSockBadQty)
	sres, err := e.st.FarmXmasSock(e.ctx, alice.ID, bob.ID, "ca_chua", 11-6) // 5: ok
	if err != nil {
		t.Fatal(err)
	}
	// người nhận được báo ngay nhưng chưa biết ai treo; ai ghé vườn bob cũng thấy 1 chiếc tất
	if len(sres.Pokes) != 1 || sres.Pokes[0].UserID != bob.ID || sres.Pokes[0].Kind != "sock" || sres.Pokes[0].From != 0 ||
		sres.News != "🧦 Có ai đó vừa treo tất Noel ở vườn bob 👀" {
		t.Fatalf("sock pokes %+v", sres.Pokes)
	}
	if n := e.view(bob.ID, alice.ID).XmasSocks; n != 1 {
		t.Fatalf("socks on bob farm %d", n)
	}
	if e.storage(alice.ID).Stock["ca_chua"] != 5 || e.view(alice.ID, alice.ID).Xmas.Candy != store.XmasSockCandy {
		t.Fatalf("after sock: stock %v", e.storage(alice.ID).Stock)
	}
	_, err = e.st.FarmXmasSock(e.ctx, alice.ID, bob.ID, "ca_chua", 1)
	wantErr(t, err, store.ErrXmasSockHung)
	if s := e.view(bob.ID, alice.ID).XmasSock; s != "hung" {
		t.Fatalf("xmas_sock %q", s)
	}
	// tối đa 3 vườn mỗi ngày
	for _, n := range []string{"c1", "c2"} {
		if _, err := e.st.FarmXmasSock(e.ctx, alice.ID, e.vet(n).ID, "ca_chua", 1); err != nil {
			t.Fatal(err)
		}
	}
	_, err = e.st.FarmXmasSock(e.ctx, alice.ID, e.vet("c3").ID, "ca_chua", 1)
	wantErr(t, err, store.ErrXmasSockDaily)

	// hôm nay chưa mở được
	if x := e.view(bob.ID, bob.ID).Xmas; x.SocksReady != 0 || x.SocksWaiting != 1 {
		t.Fatalf("bob socks %+v", x)
	}
	_, err = e.st.FarmXmasOpenSocks(e.ctx, bob.ID)
	wantErr(t, err, store.ErrXmasNoSock)

	e.advance(24 * time.Hour)
	res, err := e.st.FarmXmasOpenSocks(e.ctx, bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	g := res.Gained
	if g.Candy != store.XmasSockCandy || g.Items["ca_chua"] != 5 || len(g.Socks) != 1 || g.Socks[0].From.ID != alice.ID {
		t.Fatalf("open %+v", g)
	}
	if len(res.Pokes) != 1 || res.Pokes[0].UserID != alice.ID || res.Pokes[0].Kind != "sock_opened" ||
		res.News != "🧦 bob vừa mở 1 chiếc tất Noel, quà của alice!" {
		t.Fatalf("open pokes %+v", res.Pokes)
	}
	if n := e.view(bob.ID, bob.ID).XmasSocks; n != 0 {
		t.Fatalf("socks after open %d", n)
	}
	if e.storage(bob.ID).Stock["ca_chua"] != 5 {
		t.Fatalf("bob stock %v", e.storage(bob.ID).Stock)
	}
	if log := *e.view(alice.ID, alice.ID).Log; len(log) == 0 || log[0].Action != "sock_opened" || log[0].Actor.ID != bob.ID {
		t.Fatalf("alice log %+v", log)
	}
	_, err = e.st.FarmXmasOpenSocks(e.ctx, bob.ID)
	wantErr(t, err, store.ErrXmasNoSock)
}

func TestFarmXmasNoStealing(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	alice, bob := e.vet("alice"), e.vet("bob")
	if v := e.view(bob.ID, alice.ID); v.StealsLeft != 0 {
		t.Fatalf("steals left %d", v.StealsLeft)
	}
	_, err := e.st.FarmSteal(e.ctx, alice.ID, bob.ID, 0)
	wantErr(t, err, store.ErrXmasSteal)
	for i := range 30 { // không ai nhận nhiệm vụ trộm trong mùa Noel
		u := e.vet("u" + string(rune('a'+i)))
		for _, q := range e.view(u.ID, u.ID).Quests {
			if q.ID == "steal" {
				t.Fatalf("steal quest during xmas for %s", u.Username)
			}
		}
	}
}

func (e *farmEnv) at(t time.Time) { e.mu.Lock(); e.now = t; e.mu.Unlock() }

func (e *farmEnv) candy(id int64) int64 {
	e.t.Helper()
	return e.view(id, id).Xmas.Candy
}

func TestFarmXmasGoldenHour(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.vet("alice"), e.vet("bob")
	e.setStock(alice.ID, "ca_chua", 10)
	e.at(time.Date(2026, 12, 24, 14, 0, 0, 0, time.UTC)) // 21:00 đêm Noel giờ VN
	res, err := e.st.FarmXmasSock(e.ctx, alice.ID, bob.ID, "ca_chua", 1)
	if err != nil || res.Gained.Candy != 2*store.XmasSockCandy || !e.view(alice.ID, alice.ID).Xmas.Golden {
		t.Fatalf("golden sock: %v %+v", err, res.Gained)
	}
}

func TestFarmXmasSnowball(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	alice, bob := e.vet("alice"), e.vet("bob")
	e.setRoll(0.1) // trúng
	_, err := e.st.FarmXmasSnowball(e.ctx, alice.ID, alice.ID)
	wantErr(t, err, store.ErrXmasSnowSelf)
	if s := e.view(bob.ID, alice.ID).XmasSnow; s != "can" {
		t.Fatalf("xmas_snow %q", s)
	}
	res, err := e.st.FarmXmasSnowball(e.ctx, alice.ID, bob.ID)
	if err != nil || res.Gained.Snow != "hit" || res.Gained.Candy != 1 || res.Log == nil || res.Log.Action != "snowball" ||
		len(res.Pokes) != 1 || res.Pokes[0].UserID != bob.ID || res.Pokes[0].Kind != "snowball" || res.Pokes[0].From != alice.ID {
		t.Fatalf("hit: %v %+v", err, res.Gained)
	}
	if v := e.view(bob.ID, alice.ID); len(v.SnowedBy) != 1 || v.SnowedBy[0].ID != alice.ID || v.XmasSnow != "cooldown" {
		t.Fatalf("bob farm %+v %q", v.SnowedBy, v.XmasSnow)
	}
	_, err = e.st.FarmXmasSnowball(e.ctx, alice.ID, bob.ID)
	wantErr(t, err, store.ErrXmasSnowCooldown)
	// bob ném trả trong 30 phút: +1 +2
	res, err = e.st.FarmXmasSnowball(e.ctx, bob.ID, alice.ID)
	if err != nil || res.Gained.Snow != "revenge" || res.Gained.Candy != 1+store.XmasSnowRevenge ||
		len(res.Pokes) != 1 || res.Pokes[0].Kind != "snowball_back" {
		t.Fatalf("revenge: %v %+v", err, res.Gained)
	}
	// hết phủ tuyết sau 15 phút, ném lại được sau 30 phút; trượt thì không kẹo
	e.advance(31 * time.Minute)
	if v := e.view(bob.ID, bob.ID); len(v.SnowedBy) != 0 {
		t.Fatalf("still snowed %+v", v.SnowedBy)
	}
	e.setRoll(0.9)
	res, err = e.st.FarmXmasSnowball(e.ctx, alice.ID, bob.ID)
	if err != nil || res.Gained.Snow != "miss" || res.Gained.Candy != 0 || res.Log != nil || len(res.Pokes) != 0 ||
		res.News == "" {
		t.Fatalf("miss: %v %+v", err, res.Gained)
	}
	if n := e.counter(alice.ID, "snowball"); n != 1 {
		t.Fatalf("snowball quest counter %d", n)
	}
	if left := e.view(alice.ID, alice.ID).Xmas.SnowLeft; left != store.XmasSnowballsPerDay-2 {
		t.Fatalf("snow left %d", left)
	}
}

func TestFarmXmasDrop(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.vet("alice"), e.vet("bob")
	e.view(alice.ID, alice.ID) // tạo nông trại
	e.view(bob.ID, bob.ID)
	// 08:50 → 11:20 ngày 20/12 giờ VN: lượt thả đầu tiên rơi vào 09:00–11:00
	e.at(time.Date(2026, 12, 20, 1, 50, 0, 0, time.UTC))
	var drop *store.XmasDropView
	for range 150 {
		d, err := e.st.FarmXmasDropTick(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if d != nil {
			if drop != nil {
				t.Fatalf("dropped twice in one window")
			}
			drop = d
		}
		e.advance(time.Minute)
	}
	if drop == nil {
		t.Fatal("no drop in first window")
	}
	exp, _ := time.Parse(time.RFC3339, drop.ExpiresAt)
	e.at(exp.Add(-time.Minute))
	owner := drop.Owner.ID
	other := alice.ID
	if owner == alice.ID {
		other = bob.ID
	}
	if v := e.view(owner, other); v.XmasDrop == nil || v.XmasDrop.ID != drop.ID {
		t.Fatalf("drop not visible on owner farm: %+v", v.XmasDrop)
	}
	res, err := e.st.FarmXmasClaimDrop(e.ctx, other, drop.ID)
	// chủ vườn nhận tin riêng, cả nhóm nhận tin chung
	if err != nil || res.Gained.Candy != store.XmasDropCandy || res.News == "" || res.OwnerID != owner ||
		len(res.Pokes) != 1 || res.Pokes[0].UserID != owner || res.Pokes[0].Kind != "drop_claimed" {
		t.Fatalf("claim: %v %+v", err, res)
	}
	if e.candy(owner) != store.XmasDropOwnerCandy {
		t.Fatalf("owner candy %d", e.candy(owner))
	}
	_, err = e.st.FarmXmasClaimDrop(e.ctx, owner, drop.ID)
	wantErr(t, err, store.ErrXmasDropGone)
	_, err = e.st.FarmXmasClaimDrop(e.ctx, owner, 999)
	wantErr(t, err, store.ErrXmasNoDrop)
}

func TestFarmXmasBox(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	alice, bob := e.vet("alice"), e.vet("bob")
	_, err := e.st.FarmXmasBox(e.ctx, alice.ID)
	wantErr(t, err, store.ErrXmasCandy)
	e.setStock(alice.ID, "ca_chua", 10)
	for _, n := range []string{"c1", "c2", "c3", "c4"} {
		u := e.vet(n)
		e.st.FarmXmasSock(e.ctx, alice.ID, u.ID, "ca_chua", 1) // 2 kẹo mỗi tất, tối đa 3
	}
	e.advance(24 * time.Hour)
	for _, n := range []string{"c5", "c6", "c7"} {
		e.st.FarmXmasSock(e.ctx, alice.ID, e.vet(n).ID, "ca_chua", 1)
	}
	if e.candy(alice.ID) != 12 {
		t.Fatalf("candy %d", e.candy(alice.ID))
	}
	e.setRoll(0.99) // đồ trang trí hiếm
	res, err := e.st.FarmXmasBox(e.ctx, alice.ID)
	if err != nil || res.Gained.Prize == nil || res.Gained.Prize.Kind != "decor" || res.News == "" {
		t.Fatalf("box: %v %+v", err, res)
	}
	if d := e.view(alice.ID, bob.ID).XmasDecor; len(d) != 1 || d[0] != res.Gained.Prize.Decor {
		t.Fatalf("decor seen by bob %v", d)
	}
	if e.candy(alice.ID) != 0 {
		t.Fatalf("candy after box %d", e.candy(alice.ID))
	}
}

func TestFarmXmasTreeAndTop(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	alice, bob := e.vet("alice"), e.vet("bob")
	e.setStock(alice.ID, "nam", 50)
	_, err := e.st.FarmXmasTreeDonate(e.ctx, alice.ID, "nam", 51)
	wantErr(t, err, store.ErrXmasTreeBadQty)
	_, err = e.st.FarmXmasTreeClaim(e.ctx, alice.ID, 1)
	wantErr(t, err, store.ErrXmasTreeNoGoal)
	res, err := e.st.FarmXmasTreeDonate(e.ctx, alice.ID, "nam", 50) // 50 × 220 = 11000 điểm: qua 2 mốc
	// kẹo từ góp cây có trần mỗi ngày: kho to không đổi hết ra kẹo được
	if err != nil || res.News == "" || res.Gained.Candy != store.XmasTreeCandyDay {
		t.Fatalf("donate: %v %+v", err, res)
	}
	e.setStock(alice.ID, "rau_muong", 20)
	res, err = e.st.FarmXmasTreeDonate(e.ctx, alice.ID, "rau_muong", 10) // hết phần kẹo hôm nay: vẫn góp được điểm
	if err != nil || res.Gained.Candy != 0 {
		t.Fatalf("donate over cap: %v %+v", err, res.Gained)
	}
	if x := e.view(alice.ID, alice.ID).Xmas; x.TreeCandyLeft != 0 || x.Tree.Mine != 11050 {
		t.Fatalf("tree candy left %d mine %d", x.TreeCandyLeft, x.Tree.Mine)
	}
	e.setStock(alice.ID, "nam", 50) // cho lại để phần dưới giữ nguyên số
	x := e.view(alice.ID, alice.ID).Xmas
	if x.Tree.Total != 11050 || x.Tree.Mine != 11050 || e.view(bob.ID, bob.ID).XmasTreeLevel != 2 {
		t.Fatalf("tree %+v", x.Tree)
	}
	if _, err := e.st.FarmXmasTreeClaim(e.ctx, alice.ID, 2); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmXmasTreeClaim(e.ctx, alice.ID, 2)
	wantErr(t, err, store.ErrFarmQuestClaimed)
	_, err = e.st.FarmXmasTreeClaim(e.ctx, alice.ID, 3)
	wantErr(t, err, store.ErrXmasTreeNoGoal)
	_, err = e.st.FarmXmasTreeClaim(e.ctx, bob.ID, 1)
	wantErr(t, err, store.ErrXmasTreeNoShare)
	e.setStock(bob.ID, "rau_muong", 5) // góp 1 bó (5 điểm) chưa đủ 100 điểm
	if _, err := e.st.FarmXmasTreeDonate(e.ctx, bob.ID, "rau_muong", 1); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmXmasTreeClaim(e.ctx, bob.ID, 1)
	wantErr(t, err, store.ErrXmasTreeNoShare)

	top, err := e.st.FarmXmasTop(e.ctx)
	if err != nil || len(top) != 2 || top[0].User.ID != alice.ID ||
		top[0].Value != store.XmasTreeCandyDay+store.XmasTreeGoalCandy {
		t.Fatalf("top %+v %v", top, err)
	}
}

func TestFarmXmasNewbieBonus(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	newbie, vet, bob := e.user("moi"), e.vet("lau"), e.vet("bob")
	e.setStock(newbie.ID, "ca_chua", 5)
	e.setStock(vet.ID, "ca_chua", 5)
	if !e.view(newbie.ID, newbie.ID).Xmas.Newbie || e.view(vet.ID, vet.ID).Xmas.Newbie {
		t.Fatal("newbie flag")
	}
	want := int64(store.XmasSockCandy + (store.XmasSockCandy*store.XmasNewbiePct+99)/100) // 2 + 50% = 3
	res, err := e.st.FarmXmasSock(e.ctx, newbie.ID, bob.ID, "ca_chua", 1)
	if err != nil || res.Gained.Candy != want {
		t.Fatalf("newbie sock candy %+v %v, want %d", res.Gained, err, want)
	}
	res, err = e.st.FarmXmasSock(e.ctx, vet.ID, bob.ID, "ca_chua", 1)
	if err != nil || res.Gained.Candy != store.XmasSockCandy {
		t.Fatalf("vet sock candy %+v %v", res.Gained, err)
	}
	// đơn Noel của người mới (hàng rẻ) vẫn được ít nhất XmasOrderCandyMin kẹo
	for _, o := range e.view(newbie.ID, newbie.ID).Xmas.Orders {
		if o.Candy < store.XmasOrderCandyMin || o.Candy > store.XmasOrderCandyMax {
			t.Fatalf("order candy %d out of range", o.Candy)
		}
	}
}

func TestFarmXmasDailyGift(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.vet("alice")
	_, err := e.st.FarmXmasDailyGift(e.ctx, alice.ID)
	wantErr(t, err, store.ErrXmasOff)
	e.toXmas()
	if !e.view(alice.ID, alice.ID).Xmas.DailyGift {
		t.Fatal("daily gift should be available")
	}
	e.setRoll(0.99) // mức cao nhất
	res, err := e.st.FarmXmasDailyGift(e.ctx, alice.ID)
	if err != nil || res.Gained.Candy != store.XmasDailyGiftMax {
		t.Fatalf("gift %+v %v", res.Gained, err)
	}
	_, err = e.st.FarmXmasDailyGift(e.ctx, alice.ID)
	wantErr(t, err, store.ErrXmasGiftTaken)
	if e.view(alice.ID, alice.ID).Xmas.DailyGift {
		t.Fatal("daily gift still available")
	}
	e.advance(24 * time.Hour)
	if _, err := e.st.FarmXmasDailyGift(e.ctx, alice.ID); err != nil {
		t.Fatalf("next day: %v", err)
	}
}

func TestFarmXmasBuildVouchers(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	a := e.vet("an") // cấp 10: đủ cấp mở rộng mọi bậc
	if err := e.st.FarmSetXmasForTest(e.ctx, a.ID, 200, 200); err != nil {
		t.Fatal(err)
	}
	v := e.view(a.ID, a.ID)
	plots, price := v.PlotCount, v.Expansion.Next.Price
	before := e.balance(a.ID)
	if _, err := e.st.FarmXmasBuy(e.ctx, a.ID, "expand"); err != nil {
		t.Fatal(err)
	}
	if v := e.view(a.ID, a.ID); v.PlotCount <= plots || e.balance(a.ID) != before-price/2 {
		t.Fatalf("expand voucher: plots %d→%d, paid %d of %d", plots, v.PlotCount, before-e.balance(a.ID), price)
	}
	capBefore := e.storage(a.ID).Cap
	before = e.balance(a.ID)
	if _, err := e.st.FarmXmasBuy(e.ctx, a.ID, "storage"); err != nil {
		t.Fatal(err)
	}
	if e.storage(a.ID).Cap <= capBefore || e.balance(a.ID) != before {
		t.Fatalf("storage voucher: cap %d→%d, paid %d", capBefore, e.storage(a.ID).Cap, before-e.balance(a.ID))
	}
	if _, err := e.st.FarmXmasBuy(e.ctx, a.ID, "tree"); err != nil {
		t.Fatal(err)
	}
	if n := e.view(a.ID, a.ID).Xmas.TreeVouchers; n != 1 {
		t.Fatalf("tree vouchers %d", n)
	}
	before = e.balance(a.ID)
	if _, err := e.st.FarmPlantTree(e.ctx, a.ID, 0, "cam"); err != nil {
		t.Fatal(err)
	}
	if e.balance(a.ID) != before || e.view(a.ID, a.ID).Xmas.TreeVouchers != 0 {
		t.Fatalf("tree voucher not used: paid %d", before-e.balance(a.ID))
	}
	before = e.balance(a.ID) // hết phiếu: trả giá thường
	if _, err := e.st.FarmPlantTree(e.ctx, a.ID, 1, "cam"); err != nil {
		t.Fatal(err)
	}
	if e.balance(a.ID) >= before {
		t.Fatal("second tree should cost xu")
	}
	// lỗi khi dùng phiếu (chưa đủ cấp) thì không mất kẹo
	newbie := e.user("moi")
	if err := e.st.FarmSetXmasForTest(e.ctx, newbie.ID, 100, 100); err != nil {
		t.Fatal(err)
	}
	_, err := e.st.FarmXmasBuy(e.ctx, newbie.ID, "expand")
	wantMsg(t, err, "Cần đạt cấp 2 mới mở rộng được nông trại.")
	if c := e.candy(newbie.ID); c != 100 {
		t.Fatalf("candy lost on failed voucher: %d", c)
	}
}
