package game

import (
	"testing"
	"time"
)

func strToSqUnchecked(s string) int {
	sq, _ := strToSq(s)
	return sq
}

func TestChess_ScholarMate(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	if g.Status() != Playing {
		t.Fatalf("chess must auto-start, got %s", g.Status())
	}
	white, black := g.Players()[0], g.Players()[1]

	// Black cannot move first
	if err := tryAct(g, black, "move", map[string]string{"from": "e7", "to": "e5"}); err == nil {
		t.Fatal("Black must not move first")
	}

	// 1. e4 e5
	act(t, g, white, "move", map[string]string{"from": "e2", "to": "e4"})
	act(t, g, black, "move", map[string]string{"from": "e7", "to": "e5"})

	// 2. Qh5 Nc6
	act(t, g, white, "move", map[string]string{"from": "d1", "to": "h5"})
	act(t, g, black, "move", map[string]string{"from": "b8", "to": "c6"})

	// 3. Bc4 Nf6
	act(t, g, white, "move", map[string]string{"from": "f1", "to": "c4"})
	act(t, g, black, "move", map[string]string{"from": "g8", "to": "f6"})

	g = roundTrip(t, g)

	// 4. Qxf7# (Checkmate!)
	act(t, g, white, "move", map[string]string{"from": "h5", "to": "f7"})

	if g.Status() != Ended {
		t.Fatalf("expected Ended status after checkmate, got %s", g.Status())
	}
	if len(g.Winners()) != 1 || g.Winners()[0] != white.ID {
		t.Fatalf("white should win, winners=%v", g.Winners())
	}
}

func TestChess_FoolsMate(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	white, black := g.Players()[0], g.Players()[1]

	// 1. f3 e5
	act(t, g, white, "move", map[string]string{"from": "f2", "to": "f3"})
	act(t, g, black, "move", map[string]string{"from": "e7", "to": "e5"})

	// 2. g4 Qh4#
	act(t, g, white, "move", map[string]string{"from": "g2", "to": "g4"})
	act(t, g, black, "move", map[string]string{"from": "d8", "to": "h4"})

	if g.Status() != Ended {
		t.Fatalf("expected Ended status after checkmate, got %s", g.Status())
	}
	if len(g.Winners()) != 1 || g.Winners()[0] != black.ID {
		t.Fatalf("black should win, winners=%v", g.Winners())
	}
}

func TestChess_Castling(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	white, black := g.Players()[0], g.Players()[1]

	// Clear kingside for white: e2-e4, g1-f3, f1-e2
	act(t, g, white, "move", map[string]string{"from": "e2", "to": "e4"})
	act(t, g, black, "move", map[string]string{"from": "e7", "to": "e5"})
	act(t, g, white, "move", map[string]string{"from": "g1", "to": "f3"})
	act(t, g, black, "move", map[string]string{"from": "b8", "to": "c6"})
	act(t, g, white, "move", map[string]string{"from": "f1", "to": "e2"})
	act(t, g, black, "move", map[string]string{"from": "f8", "to": "c5"})

	// Now White castles kingside: e1 -> g1
	act(t, g, white, "move", map[string]string{"from": "e1", "to": "g1"})

	// Check board: King on g1 (sq 6), Rook on f1 (sq 5)
	c := g.r.(*chess)
	if c.Board[6] != 'K' || c.Board[5] != 'R' || c.Board[4] != '.' || c.Board[7] != '.' {
		t.Fatalf("White kingside castling failed, board: %s", string(c.Board[:]))
	}
}

func TestChess_EnPassant(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	white, black := g.Players()[0], g.Players()[1]

	// 1. e4 a6
	act(t, g, white, "move", map[string]string{"from": "e2", "to": "e4"})
	act(t, g, black, "move", map[string]string{"from": "a7", "to": "a6"})
	// 2. e5 d5 (Black moves 2 squares, adjacent to e5)
	act(t, g, white, "move", map[string]string{"from": "e4", "to": "e5"})
	act(t, g, black, "move", map[string]string{"from": "d7", "to": "d5"})

	c := g.r.(*chess)
	if c.EnPassant != 43 { // d6 is square 43
		t.Fatalf("expected en passant square 43 (d6), got %d", c.EnPassant)
	}

	// 3. exd6 (en passant capture)
	act(t, g, white, "move", map[string]string{"from": "e5", "to": "d6"})

	// Check that d5 is empty now
	d5Sq, _ := strToSq("d5")
	if c.Board[d5Sq] != '.' {
		t.Fatalf("black pawn at d5 was not captured by en passant")
	}
	d6Sq, _ := strToSq("d6")
	if c.Board[d6Sq] != 'P' {
		t.Fatalf("white pawn should be at d6")
	}
}

func TestChess_PawnPromotion(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	c := g.r.(*chess)
	// Setup custom board for promotion: White pawn at a7, Black king at h8, White king at h1
	for i := range c.Board {
		c.Board[i] = '.'
	}
	c.Board[strToSqUnchecked("h1")] = 'K'
	c.Board[strToSqUnchecked("h8")] = 'k'
	c.Board[strToSqUnchecked("a7")] = 'P'
	c.TurnColor = 'w'

	white, _ := g.Players()[0], g.Players()[1]

	// Promote to Knight
	act(t, g, white, "move", map[string]string{"from": "a7", "to": "a8", "promotion": "n"})
	a8Sq := strToSqUnchecked("a8")
	if c.Board[a8Sq] != 'N' {
		t.Fatalf("expected piece to be promoted to N, got %c", c.Board[a8Sq])
	}
}

func TestChess_Resign(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	white, black := g.Players()[0], g.Players()[1]

	act(t, g, white, "resign", nil)
	if g.Status() != Ended {
		t.Fatalf("expected status Ended, got %s", g.Status())
	}
	if len(g.Winners()) != 1 || g.Winners()[0] != black.ID {
		t.Fatalf("black should win after white resigns, winners=%v", g.Winners())
	}
}

func TestChess_Draw(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	white, black := g.Players()[0], g.Players()[1]

	// White offers draw
	act(t, g, white, "draw_offer", nil)
	// White cannot accept their own draw offer
	if err := tryAct(g, white, "draw_accept", nil); err == nil {
		t.Fatal("white should not accept own draw offer")
	}
	// Black declines
	act(t, g, black, "draw_decline", nil)
	if g.Status() == Ended {
		t.Fatal("game should not end after draw decline")
	}

	// Black offers draw, white accepts
	act(t, g, black, "draw_offer", nil)
	act(t, g, white, "draw_accept", nil)
	if g.Status() != Ended {
		t.Fatalf("expected Ended status after draw accept, got %s", g.Status())
	}
	if len(g.Winners()) != 0 {
		t.Fatalf("draw should have 0 winners, got %v", g.Winners())
	}
}

func TestChess_Stalemate(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	c := g.r.(*chess)
	// Stalemate setup:
	// Black King at a8 (sq 56)
	// White King at c7 (sq 50)
	// White Queen at b6 (sq 41)
	for i := range c.Board {
		c.Board[i] = '.'
	}
	c.Board[strToSqUnchecked("a8")] = 'k'
	c.Board[strToSqUnchecked("c7")] = 'K'
	c.Board[strToSqUnchecked("b6")] = 'Q'
	c.TurnColor = 'b'

	// Generate moves for Black:
	moves := c.generateLegalMoves('b')
	if len(moves) != 0 {
		t.Fatalf("expected 0 legal moves in stalemate, got %d", len(moves))
	}
	if c.isInCheck('b') {
		t.Fatal("king should not be in check in stalemate")
	}
}

func TestChess_Timeout(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	white, black := g.Players()[0], g.Players()[1]
	_ = white

	// Tick after deadline
	now := t0.Add(5 * time.Minute)
	g.Tick(now)

	if g.Status() != Ended {
		t.Fatalf("expected game Ended on timeout, got %s", g.Status())
	}
	if len(g.Winners()) != 1 || g.Winners()[0] != black.ID {
		t.Fatalf("black should win when white times out, winners=%v", g.Winners())
	}
}

func TestChess_View(t *testing.T) {
	g, _ := newGame(t, "chess", 2, "")
	white := g.Players()[0]

	v := g.View(white.ID)
	pub, ok := v.State.(chessPublic)
	if !ok {
		t.Fatalf("expected chessPublic, got %T", v.State)
	}
	if pub.TurnColor != "w" {
		t.Fatalf("expected TurnColor 'w', got %s", pub.TurnColor)
	}
	if len(pub.LegalMoves) == 0 {
		t.Fatal("expected non-empty legal moves for initial position")
	}
	if len(pub.Board) != 64 {
		t.Fatalf("expected board length 64, got %d", len(pub.Board))
	}
}
