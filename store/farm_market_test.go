package store_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"chat/store"
)

// Chợ đen (§15): treo hàng giá tự đặt, 10 chỗ chung toàn server, mua cả lô, gỡ, hết hạn.

func (e *farmEnv) market(viewer int64) store.MarketView {
	e.t.Helper()
	return *e.view(viewer, viewer).Market
}

func (e *farmEnv) list(seller int64, item string, qty, price int64) store.MarketListing {
	e.t.Helper()
	res, err := e.st.FarmMarketList(e.ctx, seller, item, qty, price)
	if err != nil || !res.Market {
		e.t.Fatalf("list %d %s: %v", qty, item, err)
	}
	var last store.MarketListing // lô vừa treo: id lớn nhất
	for _, l := range e.market(seller).Listings {
		if l.ID > last.ID {
			last = l
		}
	}
	return last
}

func TestFarmMarketListAndBuy(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setStock(alice.ID, "ca_chua", 10)

	_, err := e.st.FarmMarketList(e.ctx, alice.ID, "ca_chua", 11, 100)
	wantMsg(t, err, "Trong kho không đủ 11 Cà chua để treo bán.")
	_, err = e.st.FarmMarketList(e.ctx, alice.ID, "ca_chua", 0, 100)
	wantErr(t, err, store.ErrFarmBadQty)
	_, err = e.st.FarmMarketList(e.ctx, alice.ID, "ca_chua", 1, 0)
	wantErr(t, err, store.ErrMarketBadPrice)
	_, err = e.st.FarmMarketList(e.ctx, alice.ID, "khong_co", 1, 10)
	wantErr(t, err, store.ErrFarmUnknownItem)

	l := e.list(alice.ID, "ca_chua", 4, 500)
	if l.Slot != 0 || l.Qty != 4 || l.Price != 500 || !l.Mine || l.Seller.ID != alice.ID {
		t.Fatalf("listing %+v", l)
	}
	if st := e.storage(alice.ID); st.Stock["ca_chua"] != 6 {
		t.Fatalf("stock after list %v", st.Stock)
	}
	if m := e.market(bob.ID); len(m.Listings) != 1 || m.Listings[0].Mine || m.Used != 1 {
		t.Fatalf("bob sees %+v", m)
	}

	_, err = e.st.FarmMarketBuy(e.ctx, alice.ID, l.ID)
	wantErr(t, err, store.ErrMarketOwnBuy)

	res, err := e.st.FarmMarketBuy(e.ctx, bob.ID, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Notify != alice.ID || res.Log == nil || res.Log.Action != "market_sold" || res.Log.Xu != 500 ||
		res.Gained.Xu != -500 || res.Gained.Items["ca_chua"] != 4 || len(res.Wallets) != 2 {
		t.Fatalf("buy result %+v %+v", res, res.Gained)
	}
	if e.balance(alice.ID) != store.WalletStart+500 || e.balance(bob.ID) != store.WalletStart-500 {
		t.Fatalf("balances %d %d", e.balance(alice.ID), e.balance(bob.ID))
	}
	if st := e.storage(bob.ID); st.Stock["ca_chua"] != 4 {
		t.Fatalf("bob stock %v", st.Stock)
	}
	if m := e.market(bob.ID); len(m.Listings) != 0 || m.Used != 0 {
		t.Fatalf("market after buy %+v", m)
	}
	_, err = e.st.FarmMarketBuy(e.ctx, bob.ID, l.ID)
	wantErr(t, err, store.ErrMarketGone)
}

func TestFarmMarketSlotsAreShared(t *testing.T) {
	e := newFarmEnv(t)
	var sellers []int64
	for i := range 4 {
		u := e.user(fmt.Sprintf("u%d", i))
		e.setStock(u.ID, "rau_muong", 10)
		sellers = append(sellers, u.ID)
	}
	for i := range store.MarketPerUser {
		e.list(sellers[0], "rau_muong", 1, int64(10+i))
	}
	_, err := e.st.FarmMarketList(e.ctx, sellers[0], "rau_muong", 1, 10)
	wantErr(t, err, store.ErrMarketUserMax)

	for _, id := range sellers[1:] {
		for range store.MarketPerUser {
			if len(e.market(id).Listings) == store.MarketSlots {
				break
			}
			e.list(id, "rau_muong", 1, 10)
		}
	}
	m := e.market(sellers[3])
	if len(m.Listings) != store.MarketSlots || m.Used != store.MarketSlots {
		t.Fatalf("listings %d", len(m.Listings))
	}
	for i, l := range m.Listings {
		if l.Slot != i {
			t.Fatalf("slot %d at %d", l.Slot, i)
		}
	}
	// u3 mới treo 1 món nhưng chợ đã kín
	_, err = e.st.FarmMarketList(e.ctx, sellers[3], "rau_muong", 1, 10)
	wantErr(t, err, store.ErrMarketFull)
	if st := e.storage(sellers[3]); st.Stock["rau_muong"] != 9 {
		t.Fatalf("stock must stay when market full: %v", st.Stock)
	}

	// gỡ một lô thì chỗ đó trống, người khác treo vào đúng chỗ đó
	first := m.Listings[1]
	_, err = e.st.FarmMarketCancel(e.ctx, sellers[3], first.ID)
	wantErr(t, err, store.ErrMarketNotOwner)
	res, err := e.st.FarmMarketCancel(e.ctx, sellers[0], first.ID)
	if err != nil || !res.Market || res.Gained.Items["rau_muong"] != 1 {
		t.Fatalf("cancel %v %+v", err, res)
	}
	if st := e.storage(sellers[0]); st.Stock["rau_muong"] != 8 {
		t.Fatalf("stock after cancel %v", st.Stock)
	}
	if l := e.list(sellers[3], "rau_muong", 1, 10); l.Slot != first.Slot {
		t.Fatalf("reused slot %d, want %d", l.Slot, first.Slot)
	}
}

func TestFarmMarketExpires(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setStock(alice.ID, "ngo", 5)
	l := e.list(alice.ID, "ngo", 5, 300)

	e.advance(store.MarketTTL - time.Minute)
	if m := e.market(bob.ID); len(m.Listings) != 1 {
		t.Fatalf("still listed before ttl: %+v", m)
	}
	e.advance(2 * time.Minute)
	_, err := e.st.FarmMarketBuy(e.ctx, bob.ID, l.ID)
	wantErr(t, err, store.ErrMarketGone)
	if st := e.storage(alice.ID); st.Stock["ngo"] != 5 {
		t.Fatalf("expired goods back to seller: %v", st.Stock)
	}
	v := e.view(alice.ID, alice.ID)
	if len(v.Market.Listings) != 0 || len(*v.Log) == 0 || (*v.Log)[0].Action != "market_expired" ||
		(*v.Log)[0].Crop != "ngo" || (*v.Log)[0].Xu != 5 {
		t.Fatalf("after expire: %+v log %+v", v.Market, *v.Log)
	}
	if e.balance(bob.ID) != store.WalletStart {
		t.Fatalf("bob paid for expired listing: %d", e.balance(bob.ID))
	}
}

// Hết hạn mà không ai đụng tới chợ: mở nông trại của mình cũng dọn lô quá hạn.
func TestFarmMarketExpireOnView(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	e.setStock(alice.ID, "ngo", 2)
	e.list(alice.ID, "ngo", 2, 50)
	e.advance(store.MarketTTL)
	if st := e.storage(alice.ID); st.Stock["ngo"] != 2 {
		t.Fatalf("stock %v", st.Stock)
	}
}

func TestFarmMarketBuyChecks(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.setStock(alice.ID, "nam", 2)
	e.setStock(alice.ID, "trung", 30)
	pricey := e.list(alice.ID, "nam", 1, store.WalletStart+1)
	_, err := e.st.FarmMarketBuy(e.ctx, bob.ID, pricey.ID)
	wantErr(t, err, store.ErrInsufficientBalance)
	if m := e.market(bob.ID); len(m.Listings) != 1 || e.balance(bob.ID) != store.WalletStart {
		t.Fatalf("failed buy changed state: %+v %d", m, e.balance(bob.ID))
	}

	eggs := e.list(alice.ID, "trung", 30, 10)
	e.setStock(bob.ID, "rau_muong", 31) // kho 60, còn 29 chỗ < 30
	_, err = e.st.FarmMarketBuy(e.ctx, bob.ID, eggs.ID)
	wantErr(t, err, store.ErrFarmStorageFull)
}

// Đang nợ: tiền bán ở chợ đen qua earn nên tự trừ 50% vào nợ.
func TestFarmMarketSellerInDebtRepays(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	if _, _, _, err := e.st.TransferCoins(e.ctx, alice.ID, bob.ID, store.WalletStart-10, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.TakeLoan(e.ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	e.setStock(alice.ID, "ngo", 1)
	l := e.list(alice.ID, "ngo", 1, 100)
	if _, err := e.st.FarmMarketBuy(e.ctx, bob.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	w, err := e.st.GetWallet(e.ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Debt != store.LoanDebt-50 {
		t.Fatalf("debt %d", w.Debt)
	}
}

func TestFarmMarketConcurrentBuy(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob, carol := e.user("alice"), e.user("bob"), e.user("carol")
	e.setStock(alice.ID, "lua", 3)
	l := e.list(alice.ID, "lua", 3, 200)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []int64{bob.ID, carol.ID} {
		wg.Go(func() { _, errs[i] = e.st.FarmMarketBuy(e.ctx, id, l.ID) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else {
			wantErr(t, err, store.ErrMarketGone)
		}
	}
	if ok != 1 || e.balance(alice.ID) != store.WalletStart+200 {
		t.Fatalf("ok=%d errs=%v alice=%d", ok, errs, e.balance(alice.ID))
	}
}
