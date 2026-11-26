package store_test

import (
	"testing"
	"time"

	"chat/store"
)

// Kết đôi (§11): ngỏ lời / nhận lời, mỗi người một đôi, điểm thân mật, quà, không trộm người thương.
func TestFarmCouple(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob, carol := e.user("alice"), e.user("bob"), e.user("carol")

	_, err := e.st.FarmPropose(e.ctx, alice.ID, alice.ID)
	wantErr(t, err, store.ErrFarmCoupleSelf)
	if v := e.view(bob.ID, alice.ID); v.CoupleState != "can_propose" || v.Couple != nil {
		t.Fatalf("fresh state %q", v.CoupleState)
	}
	res, err := e.st.FarmPropose(e.ctx, alice.ID, bob.ID)
	if err != nil || res.Log == nil || res.Log.Action != "propose" || res.OwnerID != bob.ID {
		t.Fatalf("propose: %v %+v", err, res.Log)
	}
	if v := e.view(bob.ID, alice.ID); v.CoupleState != "sent" {
		t.Fatalf("after propose, alice sees %q", v.CoupleState)
	}
	if v := e.view(alice.ID, bob.ID); v.CoupleState != "incoming" {
		t.Fatalf("bob sees %q", v.CoupleState)
	}
	if v := e.view(bob.ID, bob.ID); len(v.CoupleRequests) != 1 || v.CoupleRequests[0].User.ID != alice.ID {
		t.Fatalf("bob requests: %+v", v.CoupleRequests)
	}
	if v := e.view(alice.ID, alice.ID); v.CoupleSent == nil || v.CoupleSent.User.ID != bob.ID {
		t.Fatalf("alice sent: %+v", v.CoupleSent)
	}
	// ngỏ người khác thì thay lời cũ
	if _, err := e.st.FarmPropose(e.ctx, alice.ID, carol.ID); err != nil {
		t.Fatal(err)
	}
	if v := e.view(bob.ID, bob.ID); len(v.CoupleRequests) != 0 {
		t.Fatal("old proposal kept")
	}
	_, err = e.st.FarmCoupleRespond(e.ctx, bob.ID, alice.ID, true)
	wantErr(t, err, store.ErrFarmCoupleNoAsk)
	// bob ngỏ với alice: alice nhận lời → thành đôi, lời ngỏ alice→carol cũng bị xoá
	if _, err := e.st.FarmPropose(e.ctx, bob.ID, alice.ID); err != nil {
		t.Fatal(err)
	}
	res, err = e.st.FarmCoupleRespond(e.ctx, alice.ID, bob.ID, true)
	if err != nil || res.Notify != bob.ID || res.Log.Action != "couple" {
		t.Fatalf("accept: %v %+v", err, res)
	}
	if v := e.view(carol.ID, carol.ID); len(v.CoupleRequests) != 0 {
		t.Fatal("proposal to carol kept after coupling")
	}
	v := e.view(alice.ID, alice.ID)
	if v.Couple == nil || v.Couple.Partner.ID != bob.ID || v.Couple.Level != 1 || v.Couple.LevelName != "Mới quen" ||
		v.Couple.GiftsLeft == nil || *v.Couple.GiftsLeft != 3 || v.Couple.NextLove == nil || *v.Couple.NextLove != 20 {
		t.Fatalf("couple: %+v", v.Couple)
	}
	if nv := e.view(alice.ID, carol.ID); nv.Couple == nil || nv.Couple.GiftsLeft != nil || nv.CoupleState != "" {
		t.Fatalf("carol sees %+v %q", nv.Couple, nv.CoupleState)
	}
	_, err = e.st.FarmPropose(e.ctx, carol.ID, alice.ID)
	wantErr(t, err, store.ErrFarmCoupleTaken)
	_, err = e.st.FarmPropose(e.ctx, alice.ID, carol.ID)
	wantErr(t, err, store.ErrFarmCoupleHave)

	// tưới + bắt sâu giúp người thương: +2, +3; ghé thăm lần đầu trong ngày: +2
	e.setRoll(0.1) // có sâu
	e.plant(bob.ID, 0, "ca_rot")
	e.advance(10 * time.Minute)
	res, err = e.st.FarmWater(e.ctx, alice.ID, bob.ID, 0)
	if err != nil || res.Gained.Love != 2 {
		t.Fatalf("water: %v %+v", err, res.Gained)
	}
	res, err = e.st.FarmBug(e.ctx, alice.ID, bob.ID, 0)
	if err != nil || res.Gained.Love != 3 {
		t.Fatalf("bug: %v %+v", err, res.Gained)
	}
	for range 2 {
		if _, err := e.st.FarmVisit(e.ctx, alice.ID, bob.ID); err != nil {
			t.Fatal(err)
		}
	}
	if c := e.view(bob.ID, bob.ID).Couple; c.Love != 7 {
		t.Fatalf("love after help+visit: %d", c.Love)
	}
	// không trộm vườn người thương
	e.advance(10 * time.Minute)
	if p := e.view(bob.ID, alice.ID).Plots[0]; p.State != "ripe" || p.CanSteal {
		t.Fatalf("partner plot: %+v", p)
	}
	_, err = e.st.FarmSteal(e.ctx, alice.ID, bob.ID, 0)
	wantErr(t, err, store.ErrFarmStealPartner)
	ns, err := e.st.FarmNeighbors(e.ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range ns {
		if (n.User.ID == bob.ID) != n.Partner || (n.Partner && n.Stealable != 0) {
			t.Fatalf("neighbour %+v", n)
		}
	}

	// quà: 10 bí ngô (2140 xu) → tối đa 10 điểm (7 → 17); thêm 1 bí ngô (214 xu → 10 điểm) → 27, lên cấp 2
	// "Cảm nắng" → mỗi người +60 xu
	e.setStock(alice.ID, "bi_ngo", 12)
	_, err = e.st.FarmGift(e.ctx, alice.ID, "bi_ngo", 11)
	wantErr(t, err, store.ErrFarmBadQty)
	res, err = e.st.FarmGift(e.ctx, alice.ID, "bi_ngo", 10)
	if err != nil || res.Gained.Love != 10 || res.Gained.LoveUp != 0 || res.Notify != bob.ID || res.Log.Action != "gift" ||
		res.Log.Crop != "bi_ngo" || res.Log.Xu != 10 {
		t.Fatalf("gift: %v %+v %+v", err, res.Gained, res.Log)
	}
	if e.storage(bob.ID).Stock["bi_ngo"] != 10 || e.storage(alice.ID).Stock["bi_ngo"] != 2 {
		t.Fatal("gift did not move stock")
	}
	_, err = e.st.FarmGift(e.ctx, alice.ID, "bi_ngo", 5)
	wantMsg(t, err, "Trong kho không đủ 5 Bí ngô để tặng.")
	ba, bb := e.balance(alice.ID), e.balance(bob.ID)
	res, err = e.st.FarmGift(e.ctx, alice.ID, "bi_ngo", 1)
	if err != nil || res.Gained.Love != 10 || res.Gained.LoveUp != 2 {
		t.Fatalf("gift 2: %v %+v", err, res.Gained)
	}
	if e.balance(alice.ID)-ba != 60 || e.balance(bob.ID)-bb != 60 {
		t.Fatalf("level-up reward: %d %d", e.balance(alice.ID)-ba, e.balance(bob.ID)-bb)
	}
	if c := e.view(alice.ID, alice.ID).Couple; c.Love != 27 || c.Level != 2 || c.LevelName != "Cảm nắng" ||
		c.LevelLove != 20 || *c.NextLove != 60 || *c.GiftsLeft != 1 {
		t.Fatalf("after gifts: %+v", c)
	}
	if _, err := e.st.FarmGift(e.ctx, alice.ID, "bi_ngo", 1); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmGift(e.ctx, alice.ID, "bi_ngo", 0)
	wantErr(t, err, store.ErrFarmBadQty)
	_, err = e.st.FarmGift(e.ctx, alice.ID, "ca_rot", 1)
	wantErr(t, err, store.ErrFarmGiftDaily)
	_, err = e.st.FarmGift(e.ctx, carol.ID, "bi_ngo", 1)
	wantErr(t, err, store.ErrFarmCoupleNone)

	// chia tay: mất điểm, cả hai độc thân lại
	res, err = e.st.FarmBreakUp(e.ctx, bob.ID)
	if err != nil || res.Notify != alice.ID || res.Log.Action != "breakup" {
		t.Fatalf("breakup: %v %+v", err, res)
	}
	if v := e.view(alice.ID, alice.ID); v.Couple != nil {
		t.Fatal("still a couple")
	}
	if v := e.view(bob.ID, alice.ID); v.CoupleState != "can_propose" {
		t.Fatalf("after breakup %q", v.CoupleState)
	}
	_, err = e.st.FarmBreakUp(e.ctx, bob.ID)
	wantErr(t, err, store.ErrFarmCoupleNone)
}

// Hai người cùng ngỏ lời với nhau thì thành đôi luôn; rút lại lời ngỏ được.
func TestFarmCoupleMutualAndCancel(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob, carol := e.user("alice"), e.user("bob"), e.user("carol")
	if _, err := e.st.FarmPropose(e.ctx, carol.ID, alice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.FarmCancelPropose(e.ctx, carol.ID); err != nil {
		t.Fatal(err)
	}
	if v := e.view(alice.ID, alice.ID); len(v.CoupleRequests) != 0 {
		t.Fatal("cancelled proposal kept")
	}
	if _, err := e.st.FarmPropose(e.ctx, alice.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	res, err := e.st.FarmPropose(e.ctx, bob.ID, alice.ID)
	if err != nil || res.Log.Action != "couple" {
		t.Fatalf("mutual: %v %+v", err, res.Log)
	}
	if c := e.view(bob.ID, bob.ID).Couple; c == nil || c.Partner.ID != alice.ID {
		t.Fatalf("mutual couple: %+v", c)
	}
	// từ chối: lời ngỏ biến mất, không thành đôi
	dave := e.user("dave")
	if _, err := e.st.FarmPropose(e.ctx, carol.ID, dave.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.FarmCoupleRespond(e.ctx, dave.ID, carol.ID, false); err != nil {
		t.Fatal(err)
	}
	if v := e.view(dave.ID, dave.ID); len(v.CoupleRequests) != 0 || v.Couple != nil {
		t.Fatalf("declined: %+v %+v", v.CoupleRequests, v.Couple)
	}
}
