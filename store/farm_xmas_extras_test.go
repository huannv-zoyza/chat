package store_test

import (
	"testing"
	"time"

	"chat/store"
)

// Bổ sung Giáng sinh (§16d): quà bù Ông già Noel bí mật, thống kê theo ngày, tin cho người offline.

func TestFarmXmasSantaFallback(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	a, b := e.vet("an"), e.vet("binh")
	for _, u := range []int64{a.ID, b.ID} {
		if _, err := e.st.FarmXmasSantaJoin(e.ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	e.advance(24 * time.Hour)
	if _, err := e.st.FarmXmasFinaleTick(e.ctx); err != nil {
		t.Fatal(err)
	}
	// không ai gửi quà; tới đêm Noel cả hai vẫn mở được quà bù
	e.at(time.Date(2026, 12, 25, 3, 0, 0, 0, time.UTC)) // 25/12 10:00 (ngoài giờ vàng)
	if !e.view(a.ID, a.ID).Xmas.Santa.GiftReady {
		t.Fatal("fallback gift should be ready")
	}
	before := e.balance(a.ID)
	res, err := e.st.FarmXmasSantaOpen(e.ctx, a.ID)
	if err != nil || !res.Gained.SantaFallback || res.Gained.Xu != store.XmasSantaFallbackXu ||
		res.Gained.Candy != store.XmasSantaFallbackCandy+store.XmasSantaOpenCandy || e.balance(a.ID) != before+store.XmasSantaFallbackXu {
		t.Fatalf("fallback %v %+v", err, res.Gained)
	}
	if res.News != "🎅 Ông già Noel đã gửi bù quà bí mật cho an!" {
		t.Fatalf("news %q", res.News)
	}
	_, err = e.st.FarmXmasSantaOpen(e.ctx, a.ID)
	wantErr(t, err, store.ErrXmasSantaNoGift)
}

func TestFarmXmasStats(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	a, b := e.vet("an"), e.vet("binh")
	e.setRoll(0.1)
	if _, err := e.st.FarmXmasSnowball(e.ctx, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.FarmXmasSnowball(e.ctx, b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	e.setStock(a.ID, "ca_chua", 2)
	if _, err := e.st.FarmXmasSock(e.ctx, a.ID, b.ID, "ca_chua", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.FarmXmasDailyGift(e.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	st := e.view(a.ID, a.ID).Xmas.Stats
	if st.Today[store.StatSnowballs] != 2 || st.Today[store.StatSocks] != 1 || st.Today[store.StatGifts] != 1 ||
		st.PlayersToday != 2 || st.Players != 2 {
		t.Fatalf("stats today %+v", st)
	}
	e.advance(24 * time.Hour)
	if _, err := e.st.FarmXmasDailyGift(e.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	st = e.view(a.ID, a.ID).Xmas.Stats
	if st.Today[store.StatGifts] != 1 || st.Total[store.StatGifts] != 2 || st.Total[store.StatSnowballs] != 2 ||
		st.PlayersToday != 1 || st.Players != 2 {
		t.Fatalf("stats day 2 %+v", st)
	}
}

func TestFarmXmasInbox(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	a, b, c := e.vet("an"), e.vet("binh"), e.vet("chi")
	_, last, err := e.st.XmasInbox(e.ctx, c.ID, -1) // lần đầu: chỉ lấy mốc, không dội tin cũ
	if err != nil || last != 0 {
		t.Fatalf("first %d %v", last, err)
	}
	// a ném b: tin chung bỏ qua a (người làm) và b (đã có tin riêng); b có tin riêng
	if _, err := e.st.AddXmasNews(e.ctx, 0, "news", "⛄ an ném binh", 0, []int64{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddXmasNews(e.ctx, b.ID, "snowball", "⛄ an ném bạn", a.ID, nil); err != nil {
		t.Fatal(err)
	}
	check := func(uid int64, want ...string) {
		t.Helper()
		items, _, err := e.st.XmasInbox(e.ctx, uid, 0)
		if err != nil || len(items) != len(want) {
			t.Fatalf("inbox %d: %+v %v", uid, items, err)
		}
		for i, w := range want {
			if items[i].Text != w {
				t.Fatalf("inbox %d[%d] = %q, want %q", uid, i, items[i].Text, w)
			}
		}
	}
	check(a.ID)
	check(b.ID, "⛄ an ném bạn")
	check(c.ID, "⛄ an ném binh")
	items, last, _ := e.st.XmasInbox(e.ctx, c.ID, 1) // đã xem tới id 1
	if len(items) != 0 || last != 2 {
		t.Fatalf("since 1: %+v last %d", items, last)
	}
	e.advance(49 * time.Hour) // quá 2 ngày: không xem lại nữa
	if items, _, _ := e.st.XmasInbox(e.ctx, c.ID, 0); len(items) != 0 {
		t.Fatalf("old news still shown %+v", items)
	}
}
