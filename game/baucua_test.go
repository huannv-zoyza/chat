package game

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var bcNow = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

// fixDice: các lần lắc trả lần lượt theo faces.
func fixDice(t *testing.T, faces ...string) {
	i := 0
	old := rollDie
	rollDie = func() string { f := faces[i%len(faces)]; i++; return f }
	t.Cleanup(func() { rollDie = old })
}

// newBauCua: A (chủ phòng, cái đầu) 1000, B 1000, C 200, đã bắt đầu.
func newBauCua(t *testing.T, rolls int) (*Game, *baucua, [3]Player) {
	t.Helper()
	ps := [3]Player{{1, "A"}, {2, "B"}, {3, "C"}}
	opts, _ := json.Marshal(map[string]int{"rolls_per_dealer": rolls})
	g, err := New("baucua", ps[0], opts, bcNow)
	if err != nil {
		t.Fatal(err)
	}
	g.BuyIn(1, 1000)
	for _, p := range ps[1:] {
		if err := g.Act(p, "join", nil, bcNow); err != nil {
			t.Fatal(err)
		}
	}
	g.BuyIn(2, 1000)
	g.BuyIn(3, 200)
	if err := g.Act(ps[0], "start", nil, bcNow); err != nil {
		t.Fatal(err)
	}
	return g, g.r.(*baucua), ps
}

func bet(g *Game, p Player, sym string, amt int64) error {
	d, _ := json.Marshal(baucuaBet{Symbol: sym, Amount: amt})
	return g.Act(p, "bet", d, bcNow)
}

func TestBauCuaBetValidationAndPayout(t *testing.T) {
	g, b, ps := newBauCua(t, 3)
	if b.Phase != "betting" || b.dealer() != 1 || b.RollNo != 1 {
		t.Fatalf("start: %+v", b)
	}
	for _, c := range []struct {
		p    Player
		sym  string
		amt  int64
		want string
	}{
		{ps[1], "bau", 30, "Mệnh giá"},
		{ps[1], "rong", 50, "Ô cược"},
		{ps[0], "bau", 50, "Cái không"},
		{ps[2], "bau", 500, "Không đủ xu"},
	} {
		if err := bet(g, c.p, c.sym, c.amt); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("bet %+v: %v", c, err)
		}
	}
	if err := g.Act(ps[0], "roll", nil, bcNow); err == nil || !strings.Contains(err.Error(), "Chưa ai") {
		t.Fatalf("roll without bets: %v", err)
	}
	must(t, bet(g, ps[1], "bau", 100))
	must(t, bet(g, ps[1], "ca", 50))
	must(t, bet(g, ps[2], "cua", 100))
	if err := g.Act(ps[1], "roll", nil, bcNow); err == nil {
		t.Fatal("non-dealer rolled")
	}
	fixDice(t, "bau", "bau", "cua")
	must(t, g.Act(ps[0], "roll", nil, bcNow))
	if b.Chips[1] != 750 || b.Chips[2] != 1150 || b.Chips[3] != 300 {
		t.Fatalf("chips after payout: %v", b.Chips)
	}
	if b.Phase != "reveal" || len(b.History) != 1 || b.LastResult[2] != 150 {
		t.Fatalf("reveal: %+v", b)
	}
	if err := bet(g, ps[1], "bau", 10); err == nil {
		t.Fatal("bet during reveal")
	}
}

func TestBauCuaBustRotationAndEnd(t *testing.T) {
	g, b, ps := newBauCua(t, 1)
	fixDice(t, "nai", "nai", "nai")
	// A làm cái, không ai cược: hết giờ vẫn lắc
	if !g.Tick(bcNow.Add(baucuaBetTime)) || b.Phase != "reveal" {
		t.Fatalf("auto roll: %+v", b)
	}
	g.Tick(bcNow.Add(time.Minute)) // sang cái B
	if b.dealer() != 2 {
		t.Fatalf("dealer should be B: %d", b.dealer())
	}
	g.Tick(bcNow.Add(2 * time.Minute))
	g.Tick(bcNow.Add(3 * time.Minute)) // sang cái C (200 chip)
	if b.dealer() != 3 {
		t.Fatalf("dealer should be C: %d", b.dealer())
	}
	// B cược nai 100 ra 3 mặt: C phải trả 300, chỉ có 200 → cháy túi, nợ 100
	must(t, bet(g, ps[1], "nai", 100))
	must(t, g.Act(ps[2], "roll", nil, bcNow))
	if b.Chips[3] != 0 || b.Loans[3] != 100 || b.Busts[3] != 1 || b.Chips[2] != 1300 {
		t.Fatalf("bust: chips %v loans %v busts %v", b.Chips, b.Loans, b.Busts)
	}
	g.Tick(bcNow.Add(time.Hour)) // C là cái cuối: hết vòng
	if g.Status() != Ended || !strings.Contains(g.Result(), "Hết vòng cái") {
		t.Fatalf("end: %s %s", g.Status(), g.Result())
	}
	if s := g.StakeOf(3); s.Chips != 0 || s.Loans != 100 {
		t.Fatalf("stake C: %+v", s)
	}
}

func TestBauCuaLoanFinishAndAbort(t *testing.T) {
	g, b, ps := newBauCua(t, 3)
	if err := g.Act(ps[1], "loan", nil, bcNow); err == nil {
		t.Fatal("loan while rich")
	}
	b.Chips[3] = 5
	must(t, g.Act(ps[2], "loan", nil, bcNow))
	if b.Chips[3] != 505 || b.Loans[3] != 500 {
		t.Fatalf("loan: %v %v", b.Chips, b.Loans)
	}
	must(t, bet(g, ps[1], "ga", 100))
	if err := g.Act(ps[1], "finish", nil, bcNow); err == nil {
		t.Fatal("non-host finish")
	}
	if err := g.Act(ps[0], "finish", nil, bcNow); err == nil {
		t.Fatal("finish with open bets")
	}
	// xoá tin giữa lượt: cược đang mở vẫn nằm trong chip của người cược
	g.Abort("Tin ván đã bị xoá.")
	st := g.Settle()
	if g.Status() != Ended || len(st) != 3 || st[1].Chips != 1000 || st[2].Loans != 500 {
		t.Fatalf("abort settle: %s %+v", g.Status(), st)
	}
}

func TestBauCuaLeaveInLobby(t *testing.T) {
	g, _ := New("baucua", Player{1, "A"}, nil, bcNow)
	must(t, g.Act(Player{2, "B"}, "join", nil, bcNow))
	if seq := g.BuyIn(2, 1000); seq != 1 {
		t.Fatalf("seq %d", seq)
	}
	must(t, g.Act(Player{2, "B"}, "leave", nil, bcNow))
	if s := g.StakeOf(2); s.Chips != 0 || g.BuyInCount(2) != 1 {
		t.Fatalf("after leave: %+v count %d", s, g.BuyInCount(2))
	}
	must(t, g.Act(Player{2, "B"}, "join", nil, bcNow))
	if seq := g.BuyIn(2, 1000); seq != 2 {
		t.Fatalf("second buy seq %d", seq)
	}
	if !g.Staked() {
		t.Fatal("baucua should be staked")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
