package store_test

import (
	"strings"
	"testing"
	"time"

	"chat/store"
)

// Cao trào Giáng sinh (§16c): Ông già Noel bí mật, mốc kẹo, đêm Noel, món Noel.

func TestFarmXmasSecretSanta(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas() // 20/12: còn đăng ký được (bốc thăm 21/12)
	a, b, c := e.vet("an"), e.vet("binh"), e.vet("chi")
	for _, u := range []int64{a.ID, b.ID, c.ID} {
		if _, err := e.st.FarmXmasSantaJoin(e.ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	_, err := e.st.FarmXmasSantaJoin(e.ctx, a.ID)
	wantErr(t, err, store.ErrXmasSantaJoined)
	_, err = e.st.FarmXmasSantaSend(e.ctx, a.ID, "ca_chua", 1)
	wantErr(t, err, store.ErrXmasSantaNoPair)
	if news, err := e.st.FarmXmasFinaleTick(e.ctx); err != nil || len(news) != 0 {
		t.Fatalf("draw too early: %v %v", news, err)
	}

	e.advance(24 * time.Hour) // 21/12: bốc thăm
	news, err := e.st.FarmXmasFinaleTick(e.ctx)
	if err != nil || len(news) != 1 || !strings.Contains(news[0], "3 người") {
		t.Fatalf("draw news %v %v", news, err)
	}
	if again, _ := e.st.FarmXmasFinaleTick(e.ctx); len(again) != 0 {
		t.Fatalf("drew twice: %v", again)
	}
	// vào muộn sau bốc thăm: chen ngay vào vòng, không giới hạn số người
	d := e.vet("muon")
	if res, err := e.st.FarmXmasSantaJoin(e.ctx, d.ID); err != nil || !strings.Contains(res.News, "vào vòng") {
		t.Fatalf("late join %v %+v", err, res)
	}
	wantRing(t, e, a.ID, b.ID, c.ID, d.ID)

	e.setStock(a.ID, "sua", 5)
	aTarget := e.view(a.ID, a.ID).Xmas.Santa.Target.ID
	res, err := e.st.FarmXmasSantaSend(e.ctx, a.ID, "sua", 3)
	if err != nil || res.Gained.Candy != store.XmasSantaSendCandy || !strings.Contains(res.News, "1/4") {
		t.Fatalf("send %v %+v", err, res)
	}
	_, err = e.st.FarmXmasSantaSend(e.ctx, a.ID, "sua", 1)
	wantErr(t, err, store.ErrXmasSantaSent)
	_, err = e.st.FarmXmasSantaOpen(e.ctx, aTarget)
	wantErr(t, err, store.ErrXmasSantaEarly)

	e.at(time.Date(2026, 12, 24, 13, 0, 0, 0, time.UTC)) // 20:00 đêm Noel giờ VN
	_, err = e.st.FarmXmasSantaSend(e.ctx, b.ID, "sua", 1)
	wantErr(t, err, store.ErrXmasSantaLate)
	_, err = e.st.FarmXmasSantaJoin(e.ctx, e.vet("qua_muon").ID)
	wantErr(t, err, store.ErrXmasSantaClosed)
	news, err = e.st.FarmXmasFinaleTick(e.ctx)
	if err != nil || len(news) < 2 || !strings.Contains(news[1], "Lộ diện") || !strings.Contains(news[1], "an → ") {
		t.Fatalf("eve news %v %v", news, err)
	}
	if !e.view(aTarget, aTarget).Xmas.Santa.GiftReady {
		t.Fatal("gift should be ready")
	}
	res, err = e.st.FarmXmasSantaOpen(e.ctx, aTarget)
	if err != nil || res.Gained.Items["sua"] != 3 || len(res.Gained.Socks) != 1 || res.Gained.Socks[0].From.ID != a.ID ||
		!strings.Contains(res.News, "an tặng 3") {
		t.Fatalf("open %v %+v", err, res)
	}
	_, err = e.st.FarmXmasSantaOpen(e.ctx, aTarget)
	wantErr(t, err, store.ErrXmasSantaNoGift)
}

// wantRing: ids tạo đúng một vòng tặng quà: ai cũng tặng một người khác mình, mỗi người nhận đúng một lần,
// đi theo vòng từ ids[0] qua hết mọi người rồi quay lại.
func wantRing(t *testing.T, e *farmEnv, ids ...int64) {
	t.Helper()
	next := map[int64]int64{}
	for _, u := range ids {
		sv := e.view(u, u).Xmas.Santa
		if sv.Target == nil || sv.Target.ID == u {
			t.Fatalf("bad target for %d: %+v", u, sv.Target)
		}
		next[u] = sv.Target.ID
	}
	seen := map[int64]bool{}
	for u := ids[0]; !seen[u]; u = next[u] {
		seen[u] = true
	}
	if len(seen) != len(ids) {
		t.Fatalf("not a single ring: %v", next)
	}
}

func TestFarmXmasSantaLateJoin(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	a := e.vet("an")
	if _, err := e.st.FarmXmasSantaJoin(e.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	e.advance(24 * time.Hour) // bốc thăm khi mới có 1 người: chưa ghép, không chốt luôn
	if news, _ := e.st.FarmXmasFinaleTick(e.ctx); len(news) != 1 || !strings.Contains(news[0], "chưa đủ người") {
		t.Fatalf("draw news %v", news)
	}
	b := e.vet("binh")
	res, err := e.st.FarmXmasSantaJoin(e.ctx, b.ID) // người thứ hai: hai người tặng nhau
	if err != nil || !strings.Contains(res.News, "vào vòng") {
		t.Fatalf("join b %v %+v", err, res)
	}
	wantRing(t, e, a.ID, b.ID)
	// ai đã gói quà thì giữ nguyên người nhận; gói hết rồi thì không chen được nữa
	e.setStock(a.ID, "sua", 5)
	e.setStock(b.ID, "sua", 5)
	if _, err := e.st.FarmXmasSantaSend(e.ctx, a.ID, "sua", 1); err != nil {
		t.Fatal(err)
	}
	c := e.vet("chi")
	if _, err := e.st.FarmXmasSantaJoin(e.ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if tg := e.view(a.ID, a.ID).Xmas.Santa.Target; tg == nil || tg.ID != b.ID {
		t.Fatalf("a already sent, target changed: %+v", tg)
	}
	wantRing(t, e, a.ID, b.ID, c.ID)
	for _, u := range []int64{b.ID, c.ID} {
		e.setStock(u, "sua", 5)
		if _, err := e.st.FarmXmasSantaSend(e.ctx, u, "sua", 1); err != nil {
			t.Fatal(err)
		}
	}
	_, err = e.st.FarmXmasSantaJoin(e.ctx, e.vet("duy").ID)
	wantErr(t, err, store.ErrXmasSantaFull)
}

func TestFarmXmasMilestones(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	a, b := e.vet("an"), e.vet("binh")
	_, err := e.st.FarmXmasMilestone(e.ctx, a.ID, 1)
	wantErr(t, err, store.ErrXmasMilestoneNot)
	if err := e.st.FarmSetXmasForTest(e.ctx, a.ID, 0, 400); err != nil {
		t.Fatal(err)
	}
	res, err := e.st.FarmXmasMilestone(e.ctx, a.ID, 1)
	if err != nil || res.Gained.Xu != 300 {
		t.Fatalf("ms1 %v %+v", err, res.Gained)
	}
	_, err = e.st.FarmXmasMilestone(e.ctx, a.ID, 1)
	wantErr(t, err, store.ErrFarmQuestClaimed)
	res, err = e.st.FarmXmasMilestone(e.ctx, a.ID, 4) // mốc cuối: ngôi sao độc quyền
	if err != nil || res.Gained.Prize.Decor != "star" || res.News == "" {
		t.Fatalf("ms4 %v %+v", err, res)
	}
	if d := e.view(a.ID, b.ID).XmasDecor; len(d) != 1 || d[0] != "star" {
		t.Fatalf("decor %v", d)
	}
	ms := e.view(a.ID, a.ID).Xmas.Milestones
	if !ms[0].Claimed || ms[1].Claimed || !ms[3].Claimed {
		t.Fatalf("milestones %+v", ms)
	}
	_, err = e.st.FarmXmasMilestone(e.ctx, a.ID, 9)
	wantErr(t, err, store.ErrXmasMilestoneNot)
}

func TestFarmXmasEveGift(t *testing.T) {
	e := newFarmEnv(t)
	e.toXmas()
	a := e.vet("an")
	_, err := e.st.FarmXmasEveGift(e.ctx, a.ID)
	wantErr(t, err, store.ErrXmasEveNotYet)
	if e.view(a.ID, a.ID).Xmas.Eve.Gift {
		t.Fatal("eve gift before eve")
	}
	e.at(time.Date(2026, 12, 24, 13, 30, 0, 0, time.UTC))
	if !e.view(a.ID, a.ID).Xmas.Eve.Gift {
		t.Fatal("eve gift should be available")
	}
	before := e.balance(a.ID)
	res, err := e.st.FarmXmasEveGift(e.ctx, a.ID)
	// 20:30 đêm Noel trùng giờ vàng: kẹo x2
	if err != nil || res.Gained.Candy != 2*store.XmasEveCandy || e.balance(a.ID) != before+store.XmasEveXu {
		t.Fatalf("eve %v %+v", err, res.Gained)
	}
	if d := e.view(a.ID, a.ID).XmasDecor; len(d) != 1 || d[0] != "bell" {
		t.Fatalf("decor %v", d)
	}
	_, err = e.st.FarmXmasEveGift(e.ctx, a.ID)
	wantErr(t, err, store.ErrFarmQuestClaimed)
	e.at(time.Date(2026, 12, 26, 3, 0, 0, 0, time.UTC)) // 26/12: qua đêm Noel
	_, err = e.st.FarmXmasEveGift(e.ctx, e.vet("binh").ID)
	wantErr(t, err, store.ErrXmasEveOver)
}

func TestFarmXmasRecipes(t *testing.T) {
	e := newFarmEnv(t) // 24/11: ngoài sự kiện
	a := e.vet("an")
	if err := e.st.FarmBuildForTest(e.ctx, a.ID, "kitchen"); err != nil {
		t.Fatal(err)
	}
	e.setStock(a.ID, "sua", 10)
	for _, r := range e.view(a.ID, a.ID).Recipes {
		if r.ID == "ca_cao" {
			t.Fatal("xmas recipe listed off season")
		}
	}
	_, err := e.st.FarmCraft(e.ctx, a.ID, "ca_cao")
	wantErr(t, err, store.ErrXmasRecipeOff)
	e.toXmas()
	found := false
	for _, r := range e.view(a.ID, a.ID).Recipes {
		found = found || r.ID == "ca_cao"
	}
	if !found {
		t.Fatal("xmas recipe missing in season")
	}
	if _, err := e.st.FarmCraft(e.ctx, a.ID, "ca_cao"); err != nil {
		t.Fatalf("craft in season: %v", err)
	}
}

func TestFarmXmasAfterEvent(t *testing.T) {
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
	e.setStock(a.ID, "sua", 3)
	if _, err := e.st.FarmXmasSantaSend(e.ctx, a.ID, "sua", 2); err != nil {
		t.Fatal(err)
	}
	if err := e.st.FarmSetXmasForTest(e.ctx, a.ID, 0, 400); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.FarmXmasMilestone(e.ctx, a.ID, 4); err != nil { // 🌟 ngôi sao
		t.Fatal(err)
	}
	e.at(time.Date(2027, 2, 1, 3, 0, 0, 0, time.UTC)) // tháng 2: sự kiện và cửa hàng đã đóng
	if d := e.view(a.ID, b.ID).XmasDecor; len(d) != 1 || d[0] != "star" {
		t.Fatalf("decor gone after event: %v", d)
	}
	// binh không gửi quà cho an: an vẫn có quà bù để mở, tab Noel hiện; mở xong thì ẩn
	if x := e.view(a.ID, a.ID).Xmas; x == nil || !x.Santa.GiftReady {
		t.Fatalf("fallback gift hidden after event: %+v", x)
	}
	if res, err := e.st.FarmXmasSantaOpen(e.ctx, a.ID); err != nil || !res.Gained.SantaFallback {
		t.Fatalf("fallback after event %v %+v", err, res)
	}
	if e.view(a.ID, a.ID).Xmas != nil {
		t.Fatal("xmas tab should be hidden for a after opening")
	}
	// b chưa mở quà bí mật: tab Noel vẫn hiện để mở
	x := e.view(b.ID, b.ID).Xmas
	if x == nil || !x.Santa.GiftReady {
		t.Fatalf("santa gift hidden after event: %+v", x)
	}
	if res, err := e.st.FarmXmasSantaOpen(e.ctx, b.ID); err != nil || res.Gained.Items["sua"] != 2 {
		t.Fatalf("open after event %v %+v", err, res)
	}
	if e.view(b.ID, b.ID).Xmas != nil {
		t.Fatal("xmas tab should hide after opening")
	}
}
