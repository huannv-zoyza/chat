package game

import (
	"strings"
	"testing"
)

func describe(t *testing.T, g *Game) *Record {
	t.Helper()
	g = roundTrip(t, g) // lịch sử phải còn sau khi lưu / nạp lại
	b, err := g.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Describe(g.r.base().Kind, b)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func moveTexts(rec *Record) []string {
	out := make([]string, len(rec.Moves))
	for i, m := range rec.Moves {
		out[i] = m.Text
	}
	return out
}

func TestDescribeXiangqi(t *testing.T) {
	g, ps := newGame(t, "xiangqi", 2, `{"first":"host"}`)
	a, b := ps[0], ps[1] // A cầm Đỏ
	act(t, g, a, "move", map[string]string{"from": "h2", "to": "e2"})
	act(t, g, b, "move", map[string]string{"from": "h9", "to": "g7"})
	act(t, g, a, "move", map[string]string{"from": "e2", "to": "e6"}) // pháo nhảy qua tốt e3, ăn tốt e6

	rec := describe(t, g)
	if rec.FromLog || rec.Sides != [2]string{"Đỏ", "Đen"} {
		t.Fatalf("rec: fromLog=%v sides=%v", rec.FromLog, rec.Sides)
	}
	got := strings.Join(moveTexts(rec), " | ")
	want := "Pháo h2→e2 | Mã h9→g7 | Pháo e2→e6 ăn Tốt"
	if got != want {
		t.Fatalf("moves:\n got  %s\n want %s", got, want)
	}
	if rec.Moves[0].Side != 0 || rec.Moves[1].Side != 1 {
		t.Fatalf("sides: %+v", rec.Moves)
	}
	// hàng 6 trên bàn: pháo Đỏ ở cột e
	if !strings.Contains(rec.Board, "6  p . p . C . p . p") {
		t.Fatalf("board:\n%s", rec.Board)
	}
}

func TestDescribeXiangqiOldGameFromLog(t *testing.T) {
	// ván lưu trước khi có History: đọc lại nước đi từ nhật ký
	state := `{"kind":"xiangqi","players":[{"id":1,"name":"A"},{"id":2,"name":"B"}],"status":"ended","log":[
		"A (Đỏ) đấu với B (Đen). Đỏ đi trước.",
		"A (Đỏ): h2 → e2",
		"B (Đen): h9 → g7",
		"A (Đỏ): e2 → e6 (ăn quân) [Chiếu!]",
		"B đầu hàng."],"result":"A thắng","board":[]}`
	rec, err := Describe("xiangqi", []byte(state))
	if err != nil {
		t.Fatal(err)
	}
	if !rec.FromLog || len(rec.Moves) != 3 {
		t.Fatalf("fromLog=%v moves=%v", rec.FromLog, moveTexts(rec))
	}
	if rec.Moves[2].Text != "e2→e6 ăn quân, chiếu" || rec.Moves[1].Side != 1 {
		t.Fatalf("moves: %+v", rec.Moves)
	}
}

func TestDescribeChess(t *testing.T) {
	g, ps := newGame(t, "chess", 2, `{"first":"host"}`)
	w, b := ps[0], ps[1]
	act(t, g, w, "move", map[string]string{"from": "e2", "to": "e4"})
	act(t, g, b, "move", map[string]string{"from": "e7", "to": "e5"})
	act(t, g, w, "move", map[string]string{"from": "g1", "to": "f3"})

	rec := describe(t, g)
	got := strings.Join(moveTexts(rec), " | ")
	if got != "Tốt e2→e4 | Tốt e7→e5 | Mã g1→f3" {
		t.Fatalf("moves: %s", got)
	}
	if !strings.Contains(rec.Board, "FEN: ") || !strings.Contains(rec.Board, "4  . . . . P . . .") {
		t.Fatalf("board:\n%s", rec.Board)
	}
	if len(rec.Notes) == 0 || !strings.Contains(rec.Notes[0], "Đen") {
		t.Fatalf("turn note: %v", rec.Notes)
	}
}

func TestDescribeCaroOrder(t *testing.T) {
	g, _ := newGame(t, "caro", 2, "")
	ps := g.r.base().Players // người đi trước (X) được chọn ngẫu nhiên, luôn đứng đầu
	act(t, g, ps[0], "move", map[string]int{"r": 7, "c": 7})
	act(t, g, ps[1], "move", map[string]int{"r": 7, "c": 8})
	act(t, g, ps[0], "move", map[string]int{"r": 16, "c": -2}) // ngoài vùng 15×15 ban đầu

	rec := describe(t, g)
	if got := strings.Join(moveTexts(rec), " "); got != "(7,7) (7,8) (16,-2)" {
		t.Fatalf("order: %s", got)
	}
	if rec.NoOrder || !strings.Contains(rec.Board, "XO") {
		t.Fatalf("noOrder=%v board:\n%s", rec.NoOrder, rec.Board)
	}
}

func TestDescribeRps(t *testing.T) {
	g, ps := newGame(t, "rps", 2, `{"best_of":3}`)
	act(t, g, ps[0], "pick", map[string]string{"choice": "rock"})
	act(t, g, ps[1], "pick", map[string]string{"choice": "scissors"})

	rec := describe(t, g)
	if len(rec.Moves) != 1 || !strings.Contains(rec.Moves[0].Text, "A thắng") || rec.Moves[0].Side != -1 {
		t.Fatalf("rounds: %+v", rec.Moves)
	}
}
