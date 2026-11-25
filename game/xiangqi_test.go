package game

import (
	"encoding/json"
	"testing"
	"time"
)

func TestXiangqiCoordinateConversion(t *testing.T) {
	tests := []struct {
		str string
		sq  int
	}{
		{"a0", 0},
		{"i0", 8},
		{"e0", 4},
		{"a3", 27},
		{"b2", 19},
		{"e6", 58},
		{"a9", 81},
		{"i9", 89},
		{"e9", 85},
	}

	for _, tt := range tests {
		sq, ok := strToXqSq(tt.str)
		if !ok || sq != tt.sq {
			t.Errorf("strToXqSq(%q) = (%d, %v), want (%d, true)", tt.str, sq, ok, tt.sq)
		}
		back := xqSqToStr(tt.sq)
		if back != tt.str {
			t.Errorf("xqSqToStr(%d) = %q, want %q", tt.sq, back, tt.str)
		}
	}

	// Invalid strings
	if _, ok := strToXqSq("j0"); ok {
		t.Errorf("expected error for file j")
	}
	if _, ok := strToXqSq("a10"); ok {
		t.Errorf("expected error for rank 10")
	}
	if _, ok := strToXqSq("xyz"); ok {
		t.Errorf("expected error for invalid format")
	}
}

func TestXiangqiInitialBoard(t *testing.T) {
	g, _ := newGame(t, "xiangqi", 2, "")
	if g.Status() != Playing {
		t.Fatalf("expected playing, got %v", g.Status())
	}

	xq := g.r.(*xiangqi)
	// Check Kings
	if xq.Board[4] != 'K' { // e0
		t.Errorf("expected Red King at e0, got %c", xq.Board[4])
	}
	if xq.Board[85] != 'k' { // e9
		t.Errorf("expected Black King at e9, got %c", xq.Board[85])
	}

	// Check Cannons
	if xq.Board[19] != 'C' || xq.Board[25] != 'C' { // b2, h2
		t.Errorf("expected Red Cannons at b2 and h2")
	}
	if xq.Board[64] != 'c' || xq.Board[70] != 'c' { // b7, h7
		t.Errorf("expected Black Cannons at b7 and h7")
	}

	// Red starts first
	if xq.TurnColor != 'r' {
		t.Errorf("expected Red turn, got %c", xq.TurnColor)
	}
}

func TestXiangqiHorseLegBlock(t *testing.T) {
	var board [90]byte
	for i := range board {
		board[i] = '.'
	}
	// Kings in palace on different files so they don't face each other
	board[3] = 'K'  // d0 (file 3, rank 0)
	board[85] = 'k' // e9 (file 4, rank 9)

	// Red Horse at c3 (file 2, rank 3 = 29)
	board[29] = 'H'

	xq := &xiangqi{
		Board:     board,
		TurnColor: 'r',
		Opts:      xiangqiOptions{TurnMinutes: 3, First: "host"},
	}
	xq.Players = []Player{{ID: 1, Name: "Red"}, {ID: 2, Name: "Black"}}

	// Legal moves from c3 when unblocked:
	// c3 (2, 3) -> b5, d5, a4, a2, e4, e2, b1, d1 (8 destinations)
	moves := xq.generateLegalMoves('r')
	horseDest := make(map[string]bool)
	for _, m := range moves {
		if m.From == "c3" {
			horseDest[m.To] = true
		}
	}
	if len(horseDest) != 8 {
		t.Errorf("expected 8 unblocked horse moves, got %d: %v", len(horseDest), horseDest)
	}

	// Block upward leg: place piece at c4 (file 2, rank 4 = 38)
	xq.Board[38] = 'P' // Red pawn blocks upward leg
	movesBlocked := xq.generateLegalMoves('r')
	horseDestBlocked := make(map[string]bool)
	for _, m := range movesBlocked {
		if m.From == "c3" {
			horseDestBlocked[m.To] = true
		}
	}

	// Upward destinations b5 and d5 must be blocked!
	if horseDestBlocked["b5"] || horseDestBlocked["d5"] {
		t.Errorf("expected b5 and d5 to be blocked by piece at c4, but got legal")
	}
	// Other 6 destinations should still be accessible
	if !horseDestBlocked["a4"] || !horseDestBlocked["e4"] || !horseDestBlocked["a2"] || !horseDestBlocked["e2"] || !horseDestBlocked["b1"] || !horseDestBlocked["d1"] {
		t.Errorf("expected side and down moves to remain accessible")
	}
}

func TestXiangqiElephantEyeBlockAndRiver(t *testing.T) {
	var board [90]byte
	for i := range board {
		board[i] = '.'
	}
	board[3] = 'K'  // d0 (file 3)
	board[85] = 'k' // e9 (file 4)

	// Red Elephant at c0 (file 2, rank 0 = 2)
	board[2] = 'E'

	xq := &xiangqi{
		Board:     board,
		TurnColor: 'r',
		Opts:      xiangqiOptions{TurnMinutes: 3, First: "host"},
	}
	xq.Players = []Player{{ID: 1, Name: "Red"}, {ID: 2, Name: "Black"}}

	// Unblocked: from c0 can go to a2 (file 0, rank 2) and e2 (file 4, rank 2)
	moves := xq.generateLegalMoves('r')
	dest := make(map[string]bool)
	for _, m := range moves {
		if m.From == "c0" {
			dest[m.To] = true
		}
	}
	if !dest["a2"] || !dest["e2"] {
		t.Errorf("expected Elephant at c0 to reach a2 and e2, got %v", dest)
	}

	// Block eye to a2: middle point is (1, 1) = b1 (10)
	xq.Board[10] = 'P'
	movesBlocked := xq.generateLegalMoves('r')
	destBlocked := make(map[string]bool)
	for _, m := range movesBlocked {
		if m.From == "c0" {
			destBlocked[m.To] = true
		}
	}
	if destBlocked["a2"] {
		t.Errorf("expected Elephant move to a2 to be blocked by b1")
	}
	if !destBlocked["e2"] {
		t.Errorf("expected Elephant move to e2 to remain valid")
	}

	// Move elephant to e4 (boundary of river: file 4, rank 4 = 40)
	xq.Board[2] = '.'
	xq.Board[10] = '.'
	xq.Board[40] = 'E'
	movesRiver := xq.generateLegalMoves('r')
	for _, m := range movesRiver {
		if m.From == "e4" {
			toSq, _ := strToXqSq(m.To)
			toRank := toSq / xiangqiBoardFiles
			if toRank > 4 {
				t.Errorf("Elephant crossed river to rank %d!", toRank)
			}
		}
	}
}

func TestXiangqiCannonJump(t *testing.T) {
	var board [90]byte
	for i := range board {
		board[i] = '.'
	}
	board[3] = 'K'  // d0 (file 3)
	board[85] = 'k' // e9 (file 4)

	// Red Cannon at b2 (file 1, rank 2 = 19)
	board[19] = 'C'
	// Screen (Pawn) at b5 (file 1, rank 5 = 46)
	board[46] = 'P'
	// Target enemy piece at b8 (file 1, rank 8 = 73)
	board[73] = 'r'

	xq := &xiangqi{
		Board:     board,
		TurnColor: 'r',
		Opts:      xiangqiOptions{TurnMinutes: 3, First: "host"},
	}
	xq.Players = []Player{{ID: 1, Name: "Red"}, {ID: 2, Name: "Black"}}

	moves := xq.generateLegalMoves('r')
	cannonMoves := make(map[string]bool)
	for _, m := range moves {
		if m.From == "b2" {
			cannonMoves[m.To] = true
		}
	}

	// Can move along empty squares up to screen (b3, b4)
	if !cannonMoves["b3"] || !cannonMoves["b4"] {
		t.Errorf("expected cannon to slide to b3, b4")
	}
	// Cannot move to b5 (blocked by friendly piece)
	if cannonMoves["b5"] {
		t.Errorf("cannon cannot land on friendly piece b5")
	}
	// CAN capture enemy at b8 by jumping over b5
	if !cannonMoves["b8"] {
		t.Errorf("cannon should capture b8 jumping over b5")
	}
	// Cannot jump over b8 to b9
	if cannonMoves["b9"] {
		t.Errorf("cannon cannot jump over 2 pieces")
	}
}

func TestXiangqiSoldierRiver(t *testing.T) {
	var board [90]byte
	for i := range board {
		board[i] = '.'
	}
	board[3] = 'K'  // d0 (file 3)
	board[85] = 'k' // e9 (file 4)

	// Red Pawn before river at c3 (file 2, rank 3 = 29)
	board[29] = 'P'
	xq := &xiangqi{
		Board:     board,
		TurnColor: 'r',
		Opts:      xiangqiOptions{TurnMinutes: 3, First: "host"},
	}
	xq.Players = []Player{{ID: 1, Name: "Red"}, {ID: 2, Name: "Black"}}

	movesPreRiver := xq.generateLegalMoves('r')
	pawnPre := make(map[string]bool)
	for _, m := range movesPreRiver {
		if m.From == "c3" {
			pawnPre[m.To] = true
		}
	}
	// Before river: only forward (c4)
	if len(pawnPre) != 1 || !pawnPre["c4"] {
		t.Errorf("expected only c4 before river, got %v", pawnPre)
	}

	// Red Pawn after river at c6 (file 2, rank 6 = 56)
	xq.Board[29] = '.'
	xq.Board[56] = 'P'
	movesPostRiver := xq.generateLegalMoves('r')
	pawnPost := make(map[string]bool)
	for _, m := range movesPostRiver {
		if m.From == "c6" {
			pawnPost[m.To] = true
		}
	}
	// After river: forward (c7), left (b6), right (d6)
	if len(pawnPost) != 3 || !pawnPost["c7"] || !pawnPost["b6"] || !pawnPost["d6"] {
		t.Errorf("expected c7, b6, d6 after river, got %v", pawnPost)
	}
}

func TestXiangqiFacingGenerals(t *testing.T) {
	var board [90]byte
	for i := range board {
		board[i] = '.'
	}
	// Red King at e0, Black King at e9 (same file 4)
	board[4] = 'K'
	board[85] = 'k'
	// Intervening piece at e4
	board[40] = 'R'

	xq := &xiangqi{
		Base:      Base{Status: Playing},
		Board:     board,
		TurnColor: 'r',
		Opts:      xiangqiOptions{TurnMinutes: 3, First: "host"},
	}
	xq.Players = []Player{{ID: 1, Name: "Red"}, {ID: 2, Name: "Black"}}

	// Moving rook from e4 to d4 would leave Kings facing each other!
	err := xq.act(1, "move", json.RawMessage(`{"from":"e4","to":"d4"}`), time.Now())
	if err == nil {
		t.Fatalf("expected error moving intervening piece to expose facing generals")
	}

	// Moving rook along file e (e4 -> e5) preserves intervening screen, so it should be allowed!
	err = xq.act(1, "move", json.RawMessage(`{"from":"e4","to":"e5"}`), time.Now())
	if err != nil {
		t.Fatalf("expected move along file e to succeed, got %v", err)
	}
}

func TestXiangqiStalemateIsLoss(t *testing.T) {
	// In Xiangqi, stalemate (bức tử / hết nước đi) is a LOSS for the trapped player, NOT a draw!
	var board [90]byte
	for i := range board {
		board[i] = '.'
	}
	board[3] = 'K'  // Red King at d0 (file 3, rank 0)
	board[85] = 'k' // Black King at e9 (file 4, rank 9)
	board[75] = 'P' // Red Pawn at d8 (rank 8, file 3) -> covers d9
	board[77] = 'P' // Red Pawn at f8 (rank 8, file 5) -> covers f9
	board[58] = 'P' // Red Pawn at e6 (rank 6, file 4 = 6*9 + 4 = 58) -> will move to e7 to cover e8

	xq := &xiangqi{
		Base:      Base{Status: Playing},
		Board:     board,
		TurnColor: 'r',
		Opts:      xiangqiOptions{TurnMinutes: 3, First: "host"},
	}
	xq.Players = []Player{{ID: 1, Name: "Red"}, {ID: 2, Name: "Black"}}

	// Red plays e6 -> e7
	err := xq.act(1, "move", json.RawMessage(`{"from":"e6","to":"e7"}`), time.Now())
	if err != nil {
		t.Fatalf("move e6->e7 failed: %v", err)
	}

	// Verify Black is NOT in check at e9 (pawn at e7 only reaches e8)
	if xq.isInCheck('b') {
		t.Fatalf("expected Black NOT in check at e9, but reported in check")
	}

	// Verify Black has 0 legal moves
	legal := xq.generateLegalMoves('b')
	if len(legal) != 0 {
		t.Fatalf("expected 0 legal moves for Black (stalemate), got %d: %v", len(legal), legal)
	}

	// Game should end with Red WINNING by stalemate!
	if xq.Status != Ended {
		t.Fatalf("expected game to be ended, got %v", xq.Status)
	}
	if len(xq.Winners) != 1 || xq.Winners[0] != 1 {
		t.Errorf("expected Red (ID 1) to be winner by stalemate, got %v", xq.Winners)
	}
}

func TestXiangqiCheckmate(t *testing.T) {
	var board [90]byte
	for i := range board {
		board[i] = '.'
	}
	// Black King at e9 (file 4, rank 9 = 85)
	board[85] = 'k'
	// Red King at e0 (file 4, rank 0 = 4)
	board[4] = 'K'
	// Red Rook at a9 (file 0, rank 9 = 81) -> controls rank 9
	board[81] = 'R'
	// Red Rook at a8 (file 0, rank 8 = 72) -> will move a8 -> e8 to deliver checkmate
	board[72] = 'R'

	xq := &xiangqi{
		Base:      Base{Status: Playing},
		Board:     board,
		TurnColor: 'r',
		Opts:      xiangqiOptions{TurnMinutes: 3, First: "host"},
	}
	xq.Players = []Player{{ID: 1, Name: "Red"}, {ID: 2, Name: "Black"}}

	// Red moves Rook a8 -> e8 (Checkmate!)
	err := xq.act(1, "move", json.RawMessage(`{"from":"a8","to":"e8"}`), time.Now())
	if err != nil {
		t.Fatalf("move a8->e8 failed: %v", err)
	}

	if xq.Status != Ended {
		t.Fatalf("expected game ended after checkmate, got %v", xq.Status)
	}
	if len(xq.Winners) != 1 || xq.Winners[0] != 1 {
		t.Errorf("expected Red to be winner, got %v", xq.Winners)
	}

	v, _ := xq.view(1)
	pub, ok := v.(xiangqiPublic)
	if !ok {
		t.Fatalf("expected xiangqiPublic, got %T", v)
	}
	if !pub.IsCheckmate {
		t.Errorf("expected pub.IsCheckmate to be true")
	}
	if pub.CheckSquare != "e9" {
		t.Errorf("expected pub.CheckSquare to be e9, got %s", pub.CheckSquare)
	}
}

func TestXiangqiBetOptionsAndCancelGuard(t *testing.T) {
	p1 := Player{ID: 1, Name: "Host"}
	p2 := Player{ID: 2, Name: "Guest"}

	// Negative or oversized bet must be rejected
	if _, err := New("xiangqi", p1, json.RawMessage(`{"bet_amount":-100}`), t0); err == nil {
		t.Fatal("expected error for negative bet_amount")
	}
	if _, err := New("xiangqi", p1, json.RawMessage(`{"bet_amount":1000001}`), t0); err == nil {
		t.Fatal("expected error for bet_amount exceeding MaxXiangqiBet")
	}

	// Valid bet in lobby
	g, err := New("xiangqi", p1, json.RawMessage(`{"bet_amount":1000,"first":"host"}`), t0)
	if err != nil {
		t.Fatalf("unexpected error creating xiangqi with bet: %v", err)
	}
	if g.BetAmount() != 1000 {
		t.Fatalf("expected BetAmount=1000, got %d", g.BetAmount())
	}
	v1 := g.View(p1.ID)
	pub1 := v1.State.(xiangqiPublic)
	if pub1.BetAmount != 1000 || pub1.Pot != 1000 {
		t.Fatalf("expected lobby bet_amount=1000 pot=1000, got %+v", pub1)
	}

	// Cancel in lobby succeeds
	gLobby, err := New("xiangqi", p1, json.RawMessage(`{"bet_amount":500}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := gLobby.Act(p1, "cancel", nil, t0); err != nil {
		t.Fatalf("cancel in lobby failed: %v", err)
	}
	if gLobby.Status() != Ended {
		t.Fatalf("expected Ended after lobby cancel, got %v", gLobby.Status())
	}

	// Join starts game and updates pot; cancel during Playing must be rejected
	if err := g.Act(p2, "join", nil, t0); err != nil {
		t.Fatalf("join failed: %v", err)
	}
	if g.Status() != Playing {
		t.Fatalf("expected Playing, got %v", g.Status())
	}
	pub2 := g.View(p1.ID).State.(xiangqiPublic)
	if pub2.Pot != 2000 {
		t.Fatalf("expected playing pot=2000, got %d", pub2.Pot)
	}
	if err := g.Act(p1, "cancel", nil, t0); err == nil {
		t.Fatal("expected cancel while Playing to be rejected")
	}
}
