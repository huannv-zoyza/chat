package game

import (
	"slices"
	"strings"
	"testing"
	"time"
)

/* ---------- đánh giá bài ---------- */

func TestPokerHandRanking(t *testing.T) {
	// từ mạnh tới yếu
	hands := [][]string{
		{"As", "Ks", "Qs", "Js", "Ts"}, // thùng phá sảnh lớn
		{"9h", "8h", "7h", "6h", "5h"}, // thùng phá sảnh
		{"5d", "4d", "3d", "2d", "Ad"}, // thùng phá sảnh thấp nhất (A = 1)
		{"Qs", "Qh", "Qd", "Qc", "2s"}, // tứ quý
		{"Js", "Jh", "Jd", "4c", "4s"}, // cù lũ
		{"Ks", "Js", "9s", "6s", "3s"}, // thùng
		{"Th", "9s", "8d", "7c", "6h"}, // sảnh
		{"5h", "4s", "3d", "2c", "Ah"}, // sảnh A-5 thấp hơn sảnh 6
		{"7s", "7h", "7d", "Kc", "2s"}, // sám cô
		{"Ks", "Kh", "4d", "4c", "As"}, // hai đôi, kicker A
		{"Ks", "Kh", "4d", "4c", "Qs"}, // hai đôi, kicker Q
		{"As", "Ah", "9d", "5c", "2s"}, // đôi A
		{"As", "Kd", "9h", "5c", "3s"}, // mậu thầu A-K
		{"As", "Qd", "9h", "5c", "3s"}, // mậu thầu A-Q
	}
	for i := 1; i < len(hands); i++ {
		if a, b := eval5(hands[i-1]), eval5(hands[i]); a <= b {
			t.Errorf("%v (%x) should beat %v (%x)", hands[i-1], a, hands[i], b)
		}
	}
	if got := bestHand(hands[0]).name(); !strings.Contains(got, "Royal") {
		t.Errorf("royal name %q", got)
	}
	// cùng bộ khác chất: hoà
	if eval5([]string{"As", "Kd", "9h", "5c", "3s"}) != eval5([]string{"Ah", "Kc", "9s", "5d", "3h"}) {
		t.Error("same ranks must tie")
	}
}

func TestPokerBestOfSeven(t *testing.T) {
	h := bestHand([]string{"Ah", "Kh", "Qh", "2c", "Jh", "Th", "2d"})
	if h.Cat != handStraightFlush || len(h.Cards) != 5 || slices.Contains(h.Cards, "2c") {
		t.Fatalf("best %+v", h)
	}
	// bài chung là sảnh, lá tẩy không giúp gì: hai người hoà
	board := []string{"9s", "8d", "7c", "6h", "5s"}
	a := bestHand(append([]string{"2c", "3d"}, board...))
	b := bestHand(append([]string{"Kc", "Qd"}, board...))
	if a.Score != b.Score || a.Cat != handStraight {
		t.Fatalf("board straight should tie: %+v %+v", a, b)
	}
}

/* ---------- dựng ván ---------- */

// pokerTable: n người, mua vào buyIn, nút ở ghế đầu (A), bài chia theo deck.
func pokerTable(t *testing.T, n int, buyIn int64, deck func() []string) (*Game, *poker, []Player) {
	t.Helper()
	oldDeck, oldBtn := pokerDeck, pokerFirstButton
	pokerDeck = deck
	pokerFirstButton = func(seats []int) int { return seats[0] }
	t.Cleanup(func() { pokerDeck, pokerFirstButton = oldDeck, oldBtn })
	g, ps := newGame(t, "poker", n, `{"buy_in":200,"blind_up":0}`)
	for _, p := range ps {
		g.BuyIn(p.ID, buyIn)
	}
	act(t, g, ps[0], "start", nil)
	return g, g.r.(*poker), ps
}

// fixedDeck: bộ bài chia đúng holes (theo thứ tự nhận bài, bắt đầu từ người bên trái nút) và board.
func fixedDeck(holes [][2]string, board [5]string) func() []string {
	return func() []string {
		var d []string
		for _, h := range holes {
			d = append(d, h[0])
		}
		for _, h := range holes {
			d = append(d, h[1])
		}
		used := slices.Clone(d)
		used = append(used, board[:]...)
		var rest []string
		for _, c := range newPokerDeck() {
			if !slices.Contains(used, c) {
				rest = append(rest, c)
			}
		}
		burn := func() string { c := rest[0]; rest = rest[1:]; return c }
		d = append(d, burn(), board[0], board[1], board[2], burn(), board[3], burn(), board[4])
		return append(d, rest...)
	}
}

func raiseTo(n int64) map[string]int64 { return map[string]int64{"to": n} }

func totalChips(p *poker) int64 {
	var n int64
	for _, id := range p.Seats {
		n += p.Stack[id] + p.Total[id]
	}
	return n
}

/* ---------- luật ---------- */

// Tay đôi: nút là mù nhỏ, hành động trước ở preflop và sau ở các vòng sau.
func TestPokerHeadsUpOrder(t *testing.T) {
	// thứ tự nhận bài: B (bên trái nút) rồi A
	g, p, ps := pokerTable(t, 2, 200, fixedDeck([][2]string{{"2c", "7d"}, {"As", "Ah"}},
		[5]string{"Kd", "9s", "4h", "3c", "Jd"}))
	A, B := ps[0], ps[1]
	if p.SBID != A.ID || p.BBID != B.ID || p.SB != 2 || p.BB != 4 || p.Turn != A.ID {
		t.Fatalf("blinds sb=%d bb=%d turn=%d", p.SBID, p.BBID, p.Turn)
	}
	if err := tryAct(g, B, "call", nil); err == nil || !strings.Contains(err.Error(), "lượt") {
		t.Fatalf("out of turn: %v", err)
	}
	if err := tryAct(g, A, "check", nil); err == nil {
		t.Fatal("SB cannot check facing BB")
	}
	act(t, g, A, "call", nil)
	if p.Turn != B.ID || p.Phase != "preflop" {
		t.Fatalf("BB must get the option: turn %d phase %s", p.Turn, p.Phase)
	}
	act(t, g, B, "check", nil)
	if p.Phase != "flop" || len(p.Board) != 3 || p.Turn != B.ID {
		t.Fatalf("flop: phase %s board %v turn %d", p.Phase, p.Board, p.Turn)
	}
	for _, street := range []string{"turn", "river"} {
		act(t, g, B, "check", nil)
		act(t, g, A, "check", nil)
		if p.Phase != street {
			t.Fatalf("want %s got %s", street, p.Phase)
		}
	}
	act(t, g, B, "check", nil)
	act(t, g, A, "check", nil)
	if p.Phase != "showdown" || !p.Result.Showdown || p.Result.Won[A.ID] != 8 || p.Stack[A.ID] != 204 {
		t.Fatalf("showdown %+v stacks %v", p.Result, p.Stack)
	}
	if p.Result.Hands[A.ID] != "Một đôi" || len(p.Result.Shown[B.ID]) != 2 {
		t.Fatalf("shown %+v", p.Result)
	}
	// tay sau: nút chuyển sang B
	if !g.Tick(t0.Add(pokerShowTime)) || p.HandNo != 2 || p.Seats[p.Button] != B.ID || p.SBID != B.ID {
		t.Fatalf("next hand: no=%d button=%d", p.HandNo, p.Button)
	}
}

func TestPokerBetSizing(t *testing.T) {
	g, p, ps := pokerTable(t, 3, 200, fixedDeck([][2]string{{"2c", "7d"}, {"3c", "8d"}, {"As", "Ah"}},
		[5]string{"Kd", "9s", "4h", "Qc", "Jd"}))
	A, B, C := ps[0], ps[1], ps[2] // A nút, B mù nhỏ, C mù lớn; A nói trước
	if p.SBID != B.ID || p.BBID != C.ID || p.Turn != A.ID {
		t.Fatalf("positions sb=%d bb=%d turn=%d", p.SBID, p.BBID, p.Turn)
	}
	if err := tryAct(g, A, "raise", raiseTo(7)); err == nil || !strings.Contains(err.Error(), "tối thiểu lên 8") {
		t.Fatalf("min raise preflop is 2×BB: %v", err)
	}
	act(t, g, A, "raise", raiseTo(12)) // tố thêm 8
	if err := tryAct(g, B, "raise", raiseTo(19)); err == nil || !strings.Contains(err.Error(), "20") {
		t.Fatalf("re-raise must add at least the last raise (8): %v", err)
	}
	if err := tryAct(g, B, "raise", raiseTo(500)); err == nil {
		t.Fatal("cannot raise more than stack")
	}
	act(t, g, B, "raise", raiseTo(20))
	if p.MinRaise != 8 || p.CurBet != 20 {
		t.Fatalf("cur %d min %d", p.CurBet, p.MinRaise)
	}
	act(t, g, C, "fold", nil)
	act(t, g, A, "call", nil)
	if p.Phase != "flop" || p.Turn != B.ID || p.MinRaise != 4 {
		t.Fatalf("flop turn %d min %d", p.Turn, p.MinRaise)
	}
	if err := tryAct(g, B, "raise", raiseTo(3)); err == nil || !strings.Contains(err.Error(), "Cược tối thiểu 4") {
		t.Fatalf("min bet is BB: %v", err)
	}
	act(t, g, B, "raise", raiseTo(30))
	act(t, g, A, "fold", nil)
	// B ăn pot không phải lật bài
	if p.Phase != "showdown" || p.Result.Showdown || len(p.Result.Shown) != 0 || p.Result.Won[B.ID] != 74 {
		t.Fatalf("fold win %+v", p.Result)
	}
	if totalChips(p) != 600 {
		t.Fatalf("chips not conserved: %d", totalChips(p))
	}
}

// Tất tay thiếu mức tố tối thiểu không mở lại quyền tố cho người đã hành động.
func TestPokerShortAllInDoesNotReopen(t *testing.T) {
	g, p, ps := pokerTable(t, 3, 200, fixedDeck([][2]string{{"2c", "7d"}, {"3c", "8d"}, {"As", "Ah"}},
		[5]string{"Kd", "9s", "4h", "Qc", "Jd"}))
	A, B, C := ps[0], ps[1], ps[2]
	p.Stack[B.ID] = 22 // B còn 22 sau mù nhỏ: tất tay lên 24
	act(t, g, A, "raise", raiseTo(20))
	act(t, g, B, "allin", nil) // 24: chỉ thêm 4 < 16
	if p.CurBet != 24 || p.MinRaise != 16 {
		t.Fatalf("cur %d min %d", p.CurBet, p.MinRaise)
	}
	act(t, g, C, "call", nil) // C chưa hành động: vẫn tố được, nhưng chọn theo
	if p.Turn != A.ID {
		t.Fatalf("A must respond to the extra 4, turn %d", p.Turn)
	}
	v := g.View(A.ID).Me.(pokerMe)
	if v.CanRaise || v.ToCall != 4 {
		t.Fatalf("A view %+v", v)
	}
	if err := tryAct(g, A, "raise", raiseTo(60)); err == nil || !strings.Contains(err.Error(), "không mở lại") {
		t.Fatalf("A must not re-raise: %v", err)
	}
	act(t, g, A, "call", nil)
	if p.Phase != "flop" {
		t.Fatalf("phase %s", p.Phase)
	}
}

// Pot phụ: người tất tay ít chỉ ăn được pot chính.
func TestPokerSidePots(t *testing.T) {
	// B (mù nhỏ) đôi A thắng tất cả, nhưng chỉ có 50 chip
	g, p, ps := pokerTable(t, 3, 200, fixedDeck([][2]string{{"As", "Ah"}, {"Ks", "Kh"}, {"2c", "7d"}},
		[5]string{"Qd", "9s", "4h", "3c", "Jd"}))
	A, B, C := ps[0], ps[1], ps[2]
	p.Stack[B.ID] = 48 // 50 trước mù nhỏ
	act(t, g, A, "raise", raiseTo(100))
	act(t, g, B, "allin", nil) // 50 < 100: chỉ là theo hết chip
	act(t, g, C, "call", nil)
	if p.Phase != "flop" {
		t.Fatalf("phase %s", p.Phase)
	}
	for p.Phase != "showdown" {
		act(t, g, ps[slices.Index(p.Seats, p.Turn)], "check", nil)
	}
	r := p.Result
	if len(r.Pots) != 2 || r.Pots[0].Amount != 150 || r.Pots[0].Winners[0] != B.ID ||
		r.Pots[1].Amount != 100 || r.Pots[1].Winners[0] != C.ID {
		t.Fatalf("pots %+v", r.Pots)
	}
	if p.Stack[B.ID] != 150 || p.Stack[C.ID] != 200 || p.Stack[A.ID] != 100 || totalChips(p) != 450 {
		t.Fatalf("stacks %v", p.Stack)
	}
}

// Cược không ai theo được trả lại người cược.
func TestPokerUncalledBetReturned(t *testing.T) {
	g, p, ps := pokerTable(t, 2, 200, fixedDeck([][2]string{{"2c", "7d"}, {"As", "Ah"}},
		[5]string{"Kd", "9s", "4h", "3c", "Jd"}))
	A, B := ps[0], ps[1]
	p.Stack[B.ID] = 36         // B 40 tổng
	act(t, g, A, "allin", nil) // A tất tay 200
	act(t, g, B, "call", nil)  // B chỉ theo được 40
	if p.Phase != "showdown" || len(p.Board) != 5 {
		t.Fatalf("must run out the board: %s %v", p.Phase, p.Board)
	}
	if p.Stack[A.ID] != 240 || p.Stack[B.ID] != 0 || len(p.Result.Busted) != 1 {
		t.Fatalf("stacks %v result %+v", p.Stack, p.Result)
	}
	if !g.Tick(t0.Add(pokerShowTime)) || g.Status() != Ended || !slices.Equal(g.Winners(), []int64{A.ID}) {
		t.Fatalf("game over: %s %v %s", g.Status(), g.Winners(), g.Result())
	}
	if s := g.Settle(); s[0].Chips+s[1].Chips != 240 {
		t.Fatalf("settle %+v", s)
	}
}

// Hoà chia đều, chip lẻ cho người gần bên trái nút nhất.
func TestPokerSplitOddChip(t *testing.T) {
	g, p, ps := pokerTable(t, 3, 200, fixedDeck([][2]string{{"2c", "3d"}, {"2h", "3s"}, {"4c", "5d"}},
		[5]string{"As", "Ks", "Qd", "Jh", "Tc"}))
	A, B, C := ps[0], ps[1], ps[2]
	act(t, g, A, "call", nil)
	act(t, g, B, "fold", nil) // mù nhỏ 2 ở lại pot: pot 10
	act(t, g, C, "check", nil)
	for p.Phase != "showdown" {
		act(t, g, ps[slices.Index(p.Seats, p.Turn)], "check", nil)
	}
	// cả A và C đều có sảnh A-K-Q-J-10 trên bài chung: chia 10 → 5/5
	if p.Result.Won[A.ID] != 5 || p.Result.Won[C.ID] != 5 {
		t.Fatalf("split %+v", p.Result.Won)
	}
	// pot lẻ: 11 chia hai → người bên trái nút (C) được chip lẻ
	p.Phase, p.Total = "river", map[int64]int64{A.ID: 5, C.ID: 5, B.ID: 1}
	p.InHand, p.Folded = map[int64]bool{A.ID: true, B.ID: true, C.ID: true}, map[int64]bool{B.ID: true}
	before := map[int64]int64{A.ID: p.Stack[A.ID], C.ID: p.Stack[C.ID]}
	p.settle(t0, true)
	if p.Stack[C.ID]-before[C.ID] != 6 || p.Stack[A.ID]-before[A.ID] != 5 {
		t.Fatalf("odd chip: %+v", p.Result.Won)
	}
}

func TestPokerTimeoutAndAway(t *testing.T) {
	g, p, ps := pokerTable(t, 3, 200, fixedDeck([][2]string{{"2c", "7d"}, {"3c", "8d"}, {"As", "Ah"}},
		[5]string{"Kd", "9s", "4h", "Qc", "Jd"}))
	A := ps[0]
	if g.Deadline() == nil || !g.Deadline().Equal(t0.Add(30*time.Second)) {
		t.Fatalf("deadline %v", g.Deadline())
	}
	g.Tick(t0.Add(31 * time.Second)) // A hết giờ phải theo: bỏ bài
	if !p.Folded[A.ID] || p.Away[A.ID] {
		t.Fatalf("timeout fold: folded=%v away=%v", p.Folded[A.ID], p.Away[A.ID])
	}
	p.Timeouts[A.ID] = 1
	p.Phase = "showdown"
	g.Tick(t0.Add(time.Hour)) // tay mới: B cầm nút, C mù nhỏ, A mù lớn → B nói trước
	if p.HandNo != 2 || p.Turn != ps[1].ID {
		t.Fatalf("hand 2 turn %d", p.Turn)
	}
	act(t, g, ps[1], "call", nil)
	act(t, g, ps[2], "call", nil)
	g.Tick(t0.Add(2 * time.Hour)) // A (mù lớn) hết giờ lần 2: tự check, thành vắng mặt
	if !p.Away[A.ID] || p.Folded[A.ID] || p.Phase != "flop" {
		t.Fatalf("away=%v folded=%v phase=%s", p.Away[A.ID], p.Folded[A.ID], p.Phase)
	}
	act(t, g, A, "back", nil)
	if p.Away[A.ID] {
		t.Fatal("back")
	}
}

func TestPokerFinishAndCancelRefundHand(t *testing.T) {
	g, p, ps := pokerTable(t, 3, 200, fixedDeck([][2]string{{"2c", "7d"}, {"3c", "8d"}, {"As", "Ah"}},
		[5]string{"Kd", "9s", "4h", "Qc", "Jd"}))
	act(t, g, ps[0], "raise", raiseTo(50))
	g2 := roundTrip(t, g)
	if err := tryAct(g2, ps[0], "cancel", nil); err == nil {
		t.Fatal("cancel while playing must be blocked")
	}
	// tin chứa ván bị xoá giữa tay: chip đã cược được hoàn khi trả về ví
	g2.Abort("Tin nhắn chứa ván đã bị xoá.")
	var sum int64
	for _, s := range g2.Settle() {
		if s.Chips != 200 {
			t.Fatalf("cancel refund %+v", g2.Settle())
		}
		sum += s.Chips
	}
	if err := tryAct(g, ps[1], "finish", nil); err == nil {
		t.Fatal("only host can finish")
	}
	act(t, g, ps[0], "finish", nil)
	if g.Status() != Ended || p.Stack[ps[0].ID] != 200 || p.Stack[ps[1].ID] != 200 || len(g.Winners()) != 0 {
		t.Fatalf("finish %s %v winners %v", g.Status(), p.Stack, g.Winners())
	}
}

func TestPokerViewHidesCards(t *testing.T) {
	g, _, ps := pokerTable(t, 2, 200, fixedDeck([][2]string{{"2c", "7d"}, {"As", "Ah"}},
		[5]string{"Kd", "9s", "4h", "3c", "Jd"}))
	pub := g.View(0)
	if pub.Me != nil {
		t.Fatal("spectator must not get private part")
	}
	me := g.View(ps[0].ID).Me.(pokerMe)
	if !slices.Equal(me.Hole, []string{"As", "Ah"}) || me.HandName != "Đôi A" || !me.CanCall || me.ToCall != 2 {
		t.Fatalf("A view %+v", me)
	}
	if other := g.View(ps[1].ID).Me.(pokerMe); slices.Equal(other.Hole, me.Hole) {
		t.Fatal("B sees A's cards")
	}
	st := pub.State.(pokerView)
	if st.Pot != 6 || st.Seats[0].Cards != 2 || len(st.Board) != 0 {
		t.Fatalf("public %+v", st)
	}
}

func TestPokerBlindsIncrease(t *testing.T) {
	p := &poker{Opts: pokerOptions{BuyIn: 1000, BlindUp: 10}}
	for _, c := range []struct {
		hand   int
		sb, bb int64
	}{{1, 10, 20}, {10, 10, 20}, {11, 20, 40}, {21, 30, 60}, {500, 640, 1280}} {
		p.HandNo = c.hand
		if sb, bb := p.blinds(); sb != c.sb || bb != c.bb {
			t.Errorf("hand %d: %d/%d", c.hand, sb, bb)
		}
	}
}

func TestPokerOptions(t *testing.T) {
	if _, err := New("poker", Player{1, "A"}, []byte(`{"buy_in":123}`), t0); err == nil {
		t.Fatal("bad buy-in accepted")
	}
	g, err := New("poker", Player{1, "A"}, []byte(`{"buy_in":500}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if exact, amt := g.StakeRequirement(); !exact || amt != 500 || !g.Staked() {
		t.Fatalf("stake %v %d", exact, amt)
	}
}
