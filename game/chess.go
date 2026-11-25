package game

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Cờ vua (Chess) 8×8, hai người: Trắng đi trước, Đen đi sau.
// Tuân thủ đầy đủ luật cờ vua FIDE: nhập thành, bắt tốt qua đường, phong cấp,
// chiếu, chiếu bí, hết nước đi (stalemate), luật 50 nước và không đủ lực lượng chiếu bí.

const (
	chessBoardSize = 64
	chessTurnTime  = 3 * time.Minute
)

// Castling rights bitmask
const (
	castlingWhiteKingside  byte = 1
	castlingWhiteQueenside byte = 2
	castlingBlackKingside  byte = 4
	castlingBlackQueenside byte = 8
)

type chessOptions struct {
	// Thời gian cho mỗi nước đi (phút, mặc định 3).
	TurnMinutes int `json:"turn_minutes"`
	// Người đi trước (cầm Trắng): "random" (mặc định), "host", "guest".
	First string `json:"first"`
}

type chessMove struct {
	From      string `json:"from"`                // vd: "e2"
	To        string `json:"to"`                  // vd: "e4"
	Piece     string `json:"piece"`               // Quân đi: "P", "n", ...
	Captured  string `json:"captured,omitempty"`  // Quân bị ăn: "p", ...
	Promotion string `json:"promotion,omitempty"` // Quân phong cấp: "q", "r", "b", "n"
	IsCheck   bool   `json:"is_check,omitempty"`  // Nước đi có tạo thế chiếu không
}

type chess struct {
	Base
	Opts          chessOptions `json:"opts"`
	Board         [64]byte     `json:"board"`
	TurnColor     byte         `json:"turn_color"` // 'w' hoặc 'b'
	Castling      byte         `json:"castling"`   // bitmask K, Q, k, q
	EnPassant     int          `json:"en_passant"` // Ô mục tiêu bắt tốt qua đường (-1 nếu không có)
	Halfmove      int          `json:"halfmove"`   // Số nửa nước cho luật 50 nước
	Fullmove      int          `json:"fullmove"`   // Số lượt đi (bắt đầu từ 1)
	LastMove      *chessMove   `json:"last_move,omitempty"`
	DrawOfferedBy int64        `json:"draw_offered_by,omitempty"` // ID người chơi đang xin hoà (0 nếu không có)
	// Toàn bộ nước đi theo thứ tự (Trắng đi trước). Không cắt bớt như Log, để phân tích lại ván (Trợ lý AI).
	History []chessMove `json:"history,omitempty"`
}

func (c *chess) limits() (int, int) { return 2, 2 }
func (c *chess) autoStart() bool    { return true }
func (c *chess) options() any       { return &c.Opts }

func (c *chess) normalize() {
	if c.Opts.TurnMinutes <= 0 {
		c.Opts.TurnMinutes = 3
	}
	if c.Opts.First != "host" && c.Opts.First != "guest" {
		c.Opts.First = "random"
	}
}

// Bàn cờ ban đầu:
// Rank 7: r n b q k b n r (a8..h8: 56..63)
// Rank 6: p p p p p p p p (a7..h7: 48..55)
// Rank 1: P P P P P P P P (a2..h2: 8..15)
// Rank 0: R N B Q K B N R (a1..h1: 0..7)
func initialBoard() [64]byte {
	var b [64]byte
	for i := range b {
		b[i] = '.'
	}
	// Rank 0 (White pieces)
	copy(b[0:8], []byte{'R', 'N', 'B', 'Q', 'K', 'B', 'N', 'R'})
	// Rank 1 (White pawns)
	for i := 8; i < 16; i++ {
		b[i] = 'P'
	}
	// Rank 6 (Black pawns)
	for i := 48; i < 56; i++ {
		b[i] = 'p'
	}
	// Rank 7 (Black pieces)
	copy(b[56:64], []byte{'r', 'n', 'b', 'q', 'k', 'b', 'n', 'r'})
	return b
}

func (c *chess) start(now time.Time) error {
	c.normalize()
	c.Board = initialBoard()
	// Xác định ai là Trắng (Players[0] đi trước), ai là Đen (Players[1])
	if c.Opts.First == "host" {
		if c.Players[0].ID != c.HostID {
			c.Players[0], c.Players[1] = c.Players[1], c.Players[0]
		}
	} else if c.Opts.First == "guest" {
		if c.Players[0].ID == c.HostID {
			c.Players[0], c.Players[1] = c.Players[1], c.Players[0]
		}
	} else {
		shuffle(c.Players)
	}
	c.TurnColor = 'w'
	c.Castling = castlingWhiteKingside | castlingWhiteQueenside | castlingBlackKingside | castlingBlackQueenside
	c.EnPassant = -1
	c.Halfmove = 0
	c.Fullmove = 1
	c.LastMove = nil
	c.DrawOfferedBy = 0

	c.logf("%s (Trắng) đấu với %s (Đen). Trắng đi trước.", c.Players[0].Name, c.Players[1].Name)
	c.setDeadline(now, time.Duration(c.Opts.TurnMinutes)*time.Minute)
	return nil
}

func sqToStr(sq int) string {
	if sq < 0 || sq >= 64 {
		return ""
	}
	file := sq % 8
	rank := sq / 8
	return fmt.Sprintf("%c%c", 'a'+file, '1'+rank)
}

func strToSq(s string) (int, bool) {
	if len(s) != 2 {
		return -1, false
	}
	file := int(s[0] - 'a')
	rank := int(s[1] - '1')
	if file < 0 || file > 7 || rank < 0 || rank > 7 {
		return -1, false
	}
	return rank*8 + file, true
}

func isPieceColor(p byte, color byte) bool {
	if color == 'w' {
		return p >= 'A' && p <= 'Z'
	}
	return p >= 'a' && p <= 'z'
}

type moveInput struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Promotion string `json:"promotion,omitempty"`
}

func (c *chess) act(p int64, action string, data json.RawMessage, now time.Time) error {
	me := c.index(p)
	if me < 0 || me >= len(c.Players) {
		return Error("Bạn không tham gia ván này.")
	}
	myColor := byte('w')
	if me == 1 {
		myColor = 'b'
	}
	other := c.Players[1-me]

	switch action {
	case "resign":
		c.logf("%s (%s) chịu thua.", c.Players[me].Name, colorName(myColor))
		c.end(other.Name+" thắng (đối thủ chịu thua).", []int64{other.ID})
		return nil

	case "draw_offer":
		if c.DrawOfferedBy != 0 {
			return Error("Đang có lời đề nghị hoà chưa xử lý.")
		}
		c.DrawOfferedBy = p
		c.logf("%s đề nghị hoà.", c.Players[me].Name)
		return nil

	case "draw_accept":
		if c.DrawOfferedBy == 0 || c.DrawOfferedBy == p {
			return Error("Không có lời đề nghị hoà nào dành cho bạn.")
		}
		c.logf("%s chấp nhận lời đề nghị hoà.", c.Players[me].Name)
		c.end("Ván cờ hoà theo thoả thuận đôi bên.", nil)
		return nil

	case "draw_decline":
		if c.DrawOfferedBy == 0 || c.DrawOfferedBy == p {
			return Error("Không có lời đề nghị hoà nào để từ chối.")
		}
		c.DrawOfferedBy = 0
		c.logf("%s từ chối đề nghị hoà.", c.Players[me].Name)
		return nil

	case "move":
	default:
		return Error("Hành động không hợp lệ.")
	}

	if myColor != c.TurnColor {
		return Error("Chưa tới lượt bạn.")
	}

	var input moveInput
	if err := decode(data, &input); err != nil {
		return err
	}
	input.From = strings.ToLower(strings.TrimSpace(input.From))
	input.To = strings.ToLower(strings.TrimSpace(input.To))

	from, okFrom := strToSq(input.From)
	to, okTo := strToSq(input.To)
	if !okFrom || !okTo {
		return Error("Toạ độ ô cờ không hợp lệ.")
	}

	// Kiểm tra nước đi có nằm trong danh sách nước đi hợp lệ
	legalMoves := c.generateLegalMoves(c.TurnColor)
	var chosenMove *chessMove
	for _, m := range legalMoves {
		if m.From == input.From && m.To == input.To {
			// Nếu là nước phong cấp
			if m.Promotion != "" {
				promo := strings.ToLower(input.Promotion)
				if promo == "" {
					promo = "q" // Mặc định phong Hậu nếu không chọn
				}
				if promo != "q" && promo != "r" && promo != "b" && promo != "n" {
					return Error("Quân phong cấp không hợp lệ (hãy chọn Hậu, Xe, Tượng hoặc Mã).")
				}
				if m.Promotion == promo {
					chosenMove = &m
					break
				}
			} else {
				chosenMove = &m
				break
			}
		}
	}

	if chosenMove == nil {
		return Error("Nước cờ không hợp lệ hoặc đang bị chiếu.")
	}

	// Thực hiện nước đi
	c.applyMove(from, to, chosenMove.Promotion)
	c.LastMove = chosenMove
	c.DrawOfferedBy = 0 // Huỷ đề nghị hoà sau khi có nước đi mới

	oppColor := byte('b')
	if c.TurnColor == 'b' {
		oppColor = 'w'
	}
	oppInCheck := c.isInCheck(oppColor)
	chosenMove.IsCheck = oppInCheck

	// Đổi lượt
	c.TurnColor = oppColor
	if c.TurnColor == 'w' {
		c.Fullmove++
	}

	// Ghi nhật ký nước cờ
	moveLog := fmt.Sprintf("%s (%s): %s → %s", c.Players[me].Name, colorName(myColor), input.From, input.To)
	if chosenMove.Promotion != "" {
		moveLog += "=" + strings.ToUpper(chosenMove.Promotion)
	}
	if chosenMove.Captured != "" {
		moveLog += " (ăn quân)"
	}
	if oppInCheck {
		moveLog += " [Chiếu!]"
	}
	c.logf("%s", moveLog)
	c.History = append(c.History, *chosenMove)

	// Kiểm tra trạng thái kết thúc ván đấu sau nước đi
	oppLegalMoves := c.generateLegalMoves(c.TurnColor)
	if len(oppLegalMoves) == 0 {
		if oppInCheck {
			// Chiếu bí (Checkmate)
			c.logf("Chiếu bí! %s thắng ván cờ.", c.Players[me].Name)
			c.end(fmt.Sprintf("%s thắng (Chiếu bí đối phương)!", c.Players[me].Name), []int64{p})
			return nil
		}
		// Hết nước đi (Stalemate)
		c.logf("Hết nước đi (Pat). Ván cờ hoà.")
		c.end("Ván cờ hoà (Hết nước đi / Stalemate)!", nil)
		return nil
	}

	// Kiểm tra luật 50 nước
	if c.Halfmove >= 100 {
		c.logf("Luật 50 nước không ăn quân hay đi tốt. Ván cờ hoà.")
		c.end("Ván cờ hoà theo luật 50 nước!", nil)
		return nil
	}

	// Kiểm tra không đủ lực lượng chiếu bí
	if c.isInsufficientMaterial() {
		c.logf("Cả hai bên không đủ lực lượng chiếu bí. Ván cờ hoà.")
		c.end("Ván cờ hoà do không đủ quân chiếu bí!", nil)
		return nil
	}

	d := time.Duration(c.Opts.TurnMinutes) * time.Minute
	c.setDeadline(now, d)
	return nil
}

func colorName(c byte) string {
	if c == 'w' {
		return "Trắng"
	}
	return "Đen"
}

func (c *chess) applyMove(from, to int, promo string) {
	p := c.Board[from]
	target := c.Board[to]

	// Cập nhật halfmove clock: reset nếu là nước tốt đi hoặc ăn quân
	if p == 'P' || p == 'p' || target != '.' {
		c.Halfmove = 0
	} else {
		c.Halfmove++
	}

	// Xử lý ăn tốt qua đường (En Passant)
	if (p == 'P' || p == 'p') && to == c.EnPassant {
		if p == 'P' {
			c.Board[to-8] = '.' // Ăn tốt đen tại hàng 5
		} else {
			c.Board[to+8] = '.' // Ăn tốt trắng tại hàng 4
		}
	}

	// Xử lý Nhập thành (Castling)
	if p == 'K' {
		c.Castling &= ^(castlingWhiteKingside | castlingWhiteQueenside)
		if from == 4 && to == 6 {
			// White Kingside: Xe h1 (7) -> f1 (5)
			c.Board[7] = '.'
			c.Board[5] = 'R'
		} else if from == 4 && to == 2 {
			// White Queenside: Xe a1 (0) -> d1 (3)
			c.Board[0] = '.'
			c.Board[3] = 'R'
		}
	} else if p == 'k' {
		c.Castling &= ^(castlingBlackKingside | castlingBlackQueenside)
		if from == 60 && to == 62 {
			// Black Kingside: Xe h8 (63) -> f8 (61)
			c.Board[63] = '.'
			c.Board[61] = 'r'
		} else if from == 60 && to == 58 {
			// Black Queenside: Xe a8 (56) -> d8 (59)
			c.Board[56] = '.'
			c.Board[59] = 'r'
		}
	}

	// Xe di chuyển làm mất quyền nhập thành tương ứng
	if from == 0 {
		c.Castling &= ^castlingWhiteQueenside
	} else if from == 7 {
		c.Castling &= ^castlingWhiteKingside
	} else if from == 56 {
		c.Castling &= ^castlingBlackQueenside
	} else if from == 63 {
		c.Castling &= ^castlingBlackKingside
	}
	// Nếu xe ở các góc bị ăn
	if to == 0 {
		c.Castling &= ^castlingWhiteQueenside
	} else if to == 7 {
		c.Castling &= ^castlingWhiteKingside
	} else if to == 56 {
		c.Castling &= ^castlingBlackQueenside
	} else if to == 63 {
		c.Castling &= ^castlingBlackKingside
	}

	// Đặt ô en passant mới nếu tốt tiến 2 ô
	c.EnPassant = -1
	if p == 'P' && from/8 == 1 && to/8 == 3 {
		c.EnPassant = from + 8
	} else if p == 'p' && from/8 == 6 && to/8 == 4 {
		c.EnPassant = from - 8
	}

	// Phong cấp
	if (p == 'P' && to/8 == 7) || (p == 'p' && to/8 == 0) {
		pr := byte('q')
		if promo != "" {
			pr = strings.ToLower(promo)[0]
		}
		if p == 'P' {
			c.Board[to] = byte(strings.ToUpper(string(pr))[0])
		} else {
			c.Board[to] = pr
		}
	} else {
		c.Board[to] = p
	}
	c.Board[from] = '.'
}

func (c *chess) isInCheck(color byte) bool {
	kingChar := byte('K')
	oppColor := byte('b')
	if color == 'b' {
		kingChar = 'k'
		oppColor = 'w'
	}
	kingSq := -1
	for i := 0; i < 64; i++ {
		if c.Board[i] == kingChar {
			kingSq = i
			break
		}
	}
	if kingSq == -1 {
		return false
	}
	return c.isSquareAttacked(kingSq, oppColor)
}

func (c *chess) isSquareAttacked(sq int, byColor byte) bool {
	sqFile := sq % 8
	sqRank := sq / 8

	// 1. Tốt đối phương tấn công
	if byColor == 'w' {
		// Tốt trắng tấn công hướng chéo lên: rank - 1
		pawnRank := sqRank - 1
		if pawnRank >= 0 {
			if sqFile > 0 && c.Board[pawnRank*8+sqFile-1] == 'P' {
				return true
			}
			if sqFile < 7 && c.Board[pawnRank*8+sqFile+1] == 'P' {
				return true
			}
		}
	} else {
		// Tốt đen tấn công hướng chéo xuống: rank + 1
		pawnRank := sqRank + 1
		if pawnRank <= 7 {
			if sqFile > 0 && c.Board[pawnRank*8+sqFile-1] == 'p' {
				return true
			}
			if sqFile < 7 && c.Board[pawnRank*8+sqFile+1] == 'p' {
				return true
			}
		}
	}

	// 2. Mã đối phương tấn công
	knightChar := byte('N')
	if byColor == 'b' {
		knightChar = 'n'
	}
	knightDeltas := [][2]int{
		{-2, -1}, {-2, 1}, {-1, -2}, {-1, 2},
		{1, -2}, {1, 2}, {2, -1}, {2, 1},
	}
	for _, d := range knightDeltas {
		r := sqRank + d[0]
		f := sqFile + d[1]
		if r >= 0 && r < 8 && f >= 0 && f < 8 {
			if c.Board[r*8+f] == knightChar {
				return true
			}
		}
	}

	// 3. Tượng / Hậu đối phương (đường chéo)
	bishopChar, queenChar := byte('B'), byte('Q')
	if byColor == 'b' {
		bishopChar, queenChar = 'b', 'q'
	}
	diagDirs := [][2]int{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}}
	for _, d := range diagDirs {
		r, f := sqRank+d[0], sqFile+d[1]
		for r >= 0 && r < 8 && f >= 0 && f < 8 {
			p := c.Board[r*8+f]
			if p != '.' {
				if p == bishopChar || p == queenChar {
					return true
				}
				break
			}
			r += d[0]
			f += d[1]
		}
	}

	// 4. Xe / Hậu đối phương (đường thẳng)
	rookChar := byte('R')
	if byColor == 'b' {
		rookChar = 'r'
	}
	straightDirs := [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	for _, d := range straightDirs {
		r, f := sqRank+d[0], sqFile+d[1]
		for r >= 0 && r < 8 && f >= 0 && f < 8 {
			p := c.Board[r*8+f]
			if p != '.' {
				if p == rookChar || p == queenChar {
					return true
				}
				break
			}
			r += d[0]
			f += d[1]
		}
	}

	// 5. Vua đối phương (1 ô xung quanh)
	kingChar := byte('K')
	if byColor == 'b' {
		kingChar = 'k'
	}
	for dr := -1; dr <= 1; dr++ {
		for df := -1; df <= 1; df++ {
			if dr == 0 && df == 0 {
				continue
			}
			r := sqRank + dr
			f := sqFile + df
			if r >= 0 && r < 8 && f >= 0 && f < 8 {
				if c.Board[r*8+f] == kingChar {
					return true
				}
			}
		}
	}

	return false
}

// generateLegalMoves sinh toàn bộ nước đi hợp lệ không để vua bị chiếu
func (c *chess) generateLegalMoves(color byte) []chessMove {
	pseudos := c.generatePseudoMoves(color)
	var legals []chessMove

	for _, m := range pseudos {
		from, _ := strToSq(m.From)
		to, _ := strToSq(m.To)

		// Thử nước đi trên bàn cờ tạm
		clone := *c
		clone.applyMove(from, to, m.Promotion)

		// Nếu vua phe mình không bị chiếu sau khi đi -> Hợp lệ
		if !clone.isInCheck(color) {
			legals = append(legals, m)
		}
	}
	return legals
}

func (c *chess) generatePseudoMoves(color byte) []chessMove {
	var moves []chessMove
	oppColor := byte('b')
	if color == 'b' {
		oppColor = 'w'
	}

	for sq := 0; sq < 64; sq++ {
		p := c.Board[sq]
		if p == '.' || !isPieceColor(p, color) {
			continue
		}

		sqFile := sq % 8
		sqRank := sq / 8
		fromStr := sqToStr(sq)

		switch p {
		case 'P': // Tốt trắng
			// Tiến 1 ô
			if sqRank < 7 {
				to1 := sq + 8
				if c.Board[to1] == '.' {
					toStr := sqToStr(to1)
					if sqRank == 6 { // Phong cấp
						for _, promo := range []string{"q", "r", "b", "n"} {
							moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "P", Promotion: promo})
						}
					} else {
						moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "P"})
					}

					// Tiến 2 ô từ hàng ban đầu
					if sqRank == 1 {
						to2 := sq + 16
						if c.Board[to2] == '.' {
							moves = append(moves, chessMove{From: fromStr, To: sqToStr(to2), Piece: "P"})
						}
					}
				}
			}
			// Ăn chéo trái
			if sqFile > 0 && sqRank < 7 {
				capL := sq + 7
				if isPieceColor(c.Board[capL], 'b') || capL == c.EnPassant {
					toStr := sqToStr(capL)
					captured := string(c.Board[capL])
					if capL == c.EnPassant {
						captured = "p"
					}
					if sqRank == 6 {
						for _, promo := range []string{"q", "r", "b", "n"} {
							moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "P", Captured: captured, Promotion: promo})
						}
					} else {
						moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "P", Captured: captured})
					}
				}
			}
			// Ăn chéo phải
			if sqFile < 7 && sqRank < 7 {
				capR := sq + 9
				if isPieceColor(c.Board[capR], 'b') || capR == c.EnPassant {
					toStr := sqToStr(capR)
					captured := string(c.Board[capR])
					if capR == c.EnPassant {
						captured = "p"
					}
					if sqRank == 6 {
						for _, promo := range []string{"q", "r", "b", "n"} {
							moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "P", Captured: captured, Promotion: promo})
						}
					} else {
						moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "P", Captured: captured})
					}
				}
			}

		case 'p': // Tốt đen
			// Tiến 1 ô
			if sqRank > 0 {
				to1 := sq - 8
				if c.Board[to1] == '.' {
					toStr := sqToStr(to1)
					if sqRank == 1 { // Phong cấp
						for _, promo := range []string{"q", "r", "b", "n"} {
							moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "p", Promotion: promo})
						}
					} else {
						moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "p"})
					}

					// Tiến 2 ô từ hàng ban đầu
					if sqRank == 6 {
						to2 := sq - 16
						if c.Board[to2] == '.' {
							moves = append(moves, chessMove{From: fromStr, To: sqToStr(to2), Piece: "p"})
						}
					}
				}
			}
			// Ăn chéo trái (hướng đen: sq - 9)
			if sqFile > 0 && sqRank > 0 {
				capL := sq - 9
				if isPieceColor(c.Board[capL], 'w') || capL == c.EnPassant {
					toStr := sqToStr(capL)
					captured := string(c.Board[capL])
					if capL == c.EnPassant {
						captured = "P"
					}
					if sqRank == 1 {
						for _, promo := range []string{"q", "r", "b", "n"} {
							moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "p", Captured: captured, Promotion: promo})
						}
					} else {
						moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "p", Captured: captured})
					}
				}
			}
			// Ăn chéo phải (hướng đen: sq - 7)
			if sqFile < 7 && sqRank > 0 {
				capR := sq - 7
				if isPieceColor(c.Board[capR], 'w') || capR == c.EnPassant {
					toStr := sqToStr(capR)
					captured := string(c.Board[capR])
					if capR == c.EnPassant {
						captured = "P"
					}
					if sqRank == 1 {
						for _, promo := range []string{"q", "r", "b", "n"} {
							moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "p", Captured: captured, Promotion: promo})
						}
					} else {
						moves = append(moves, chessMove{From: fromStr, To: toStr, Piece: "p", Captured: captured})
					}
				}
			}

		case 'N', 'n': // Mã
			deltas := [][2]int{
				{-2, -1}, {-2, 1}, {-1, -2}, {-1, 2},
				{1, -2}, {1, 2}, {2, -1}, {2, 1},
			}
			for _, d := range deltas {
				r := sqRank + d[0]
				f := sqFile + d[1]
				if r >= 0 && r < 8 && f >= 0 && f < 8 {
					tSq := r*8 + f
					targetPiece := c.Board[tSq]
					if targetPiece == '.' || isPieceColor(targetPiece, oppColor) {
						captured := ""
						if targetPiece != '.' {
							captured = string(targetPiece)
						}
						moves = append(moves, chessMove{From: fromStr, To: sqToStr(tSq), Piece: string(p), Captured: captured})
					}
				}
			}

		case 'B', 'b': // Tượng
			dirs := [][2]int{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}}
			moves = append(moves, c.slideMoves(sq, fromStr, string(p), dirs, oppColor)...)

		case 'R', 'r': // Xe
			dirs := [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
			moves = append(moves, c.slideMoves(sq, fromStr, string(p), dirs, oppColor)...)

		case 'Q', 'q': // Hậu
			dirs := [][2]int{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}}
			moves = append(moves, c.slideMoves(sq, fromStr, string(p), dirs, oppColor)...)

		case 'K', 'k': // Vua
			for dr := -1; dr <= 1; dr++ {
				for df := -1; df <= 1; df++ {
					if dr == 0 && df == 0 {
						continue
					}
					r := sqRank + dr
					f := sqFile + df
					if r >= 0 && r < 8 && f >= 0 && f < 8 {
						tSq := r*8 + f
						targetPiece := c.Board[tSq]
						if targetPiece == '.' || isPieceColor(targetPiece, oppColor) {
							captured := ""
							if targetPiece != '.' {
								captured = string(targetPiece)
							}
							moves = append(moves, chessMove{From: fromStr, To: sqToStr(tSq), Piece: string(p), Captured: captured})
						}
					}
				}
			}

			// Nhập thành
			if color == 'w' && sq == 4 { // e1
				// Kingside: e1 -> g1 (ô 4 -> 6)
				if c.Castling&castlingWhiteKingside != 0 {
					if c.Board[5] == '.' && c.Board[6] == '.' && c.Board[7] == 'R' {
						if !c.isSquareAttacked(4, 'b') && !c.isSquareAttacked(5, 'b') && !c.isSquareAttacked(6, 'b') {
							moves = append(moves, chessMove{From: "e1", To: "g1", Piece: "K"})
						}
					}
				}
				// Queenside: e1 -> c1 (ô 4 -> 2)
				if c.Castling&castlingWhiteQueenside != 0 {
					if c.Board[1] == '.' && c.Board[2] == '.' && c.Board[3] == '.' && c.Board[0] == 'R' {
						if !c.isSquareAttacked(4, 'b') && !c.isSquareAttacked(3, 'b') && !c.isSquareAttacked(2, 'b') {
							moves = append(moves, chessMove{From: "e1", To: "c1", Piece: "K"})
						}
					}
				}
			} else if color == 'b' && sq == 60 { // e8
				// Kingside: e8 -> g8 (ô 60 -> 62)
				if c.Castling&castlingBlackKingside != 0 {
					if c.Board[61] == '.' && c.Board[62] == '.' && c.Board[63] == 'r' {
						if !c.isSquareAttacked(60, 'w') && !c.isSquareAttacked(61, 'w') && !c.isSquareAttacked(62, 'w') {
							moves = append(moves, chessMove{From: "e8", To: "g8", Piece: "k"})
						}
					}
				}
				// Queenside: e8 -> c8 (ô 60 -> 58)
				if c.Castling&castlingBlackQueenside != 0 {
					if c.Board[57] == '.' && c.Board[58] == '.' && c.Board[59] == '.' && c.Board[56] == 'r' {
						if !c.isSquareAttacked(60, 'w') && !c.isSquareAttacked(59, 'w') && !c.isSquareAttacked(58, 'w') {
							moves = append(moves, chessMove{From: "e8", To: "c8", Piece: "k"})
						}
					}
				}
			}
		}
	}
	return moves
}

func (c *chess) slideMoves(sq int, fromStr, pieceStr string, dirs [][2]int, oppColor byte) []chessMove {
	var moves []chessMove
	sqFile := sq % 8
	sqRank := sq / 8

	for _, d := range dirs {
		r := sqRank + d[0]
		f := sqFile + d[1]
		for r >= 0 && r < 8 && f >= 0 && f < 8 {
			tSq := r*8 + f
			targetPiece := c.Board[tSq]
			if targetPiece == '.' {
				moves = append(moves, chessMove{From: fromStr, To: sqToStr(tSq), Piece: pieceStr})
			} else {
				if isPieceColor(targetPiece, oppColor) {
					moves = append(moves, chessMove{From: fromStr, To: sqToStr(tSq), Piece: pieceStr, Captured: string(targetPiece)})
				}
				break
			}
			r += d[0]
			f += d[1]
		}
	}
	return moves
}

// isInsufficientMaterial kiểm tra cả 2 bên còn đủ quân để chiếu bí không
func (c *chess) isInsufficientMaterial() bool {
	var wMinors, bMinors []byte
	for _, p := range c.Board {
		if p == '.' || p == 'K' || p == 'k' {
			continue
		}
		if p == 'P' || p == 'p' || p == 'Q' || p == 'q' || p == 'R' || p == 'r' {
			return false // Vẫn còn tốt, hậu hoặc xe -> Đủ lực lượng
		}
		if p == 'B' || p == 'N' {
			wMinors = append(wMinors, p)
		} else if p == 'b' || p == 'n' {
			bMinors = append(bMinors, p)
		}
	}
	// K vs K
	if len(wMinors) == 0 && len(bMinors) == 0 {
		return true
	}
	// K+B vs K hoặc K+N vs K
	if (len(wMinors) == 1 && len(bMinors) == 0) || (len(wMinors) == 0 && len(bMinors) == 1) {
		return true
	}
	return false
}

// fen tạo chuỗi FEN chuẩn
func (c *chess) fen() string {
	var sb strings.Builder
	for r := 7; r >= 0; r-- {
		empty := 0
		for f := 0; f < 8; f++ {
			p := c.Board[r*8+f]
			if p == '.' {
				empty++
			} else {
				if empty > 0 {
					sb.WriteString(fmt.Sprintf("%d", empty))
					empty = 0
				}
				sb.WriteByte(p)
			}
		}
		if empty > 0 {
			sb.WriteString(fmt.Sprintf("%d", empty))
		}
		if r > 0 {
			sb.WriteByte('/')
		}
	}

	sb.WriteString(" ")
	sb.WriteByte(c.TurnColor)
	sb.WriteString(" ")

	castling := ""
	if c.Castling&castlingWhiteKingside != 0 {
		castling += "K"
	}
	if c.Castling&castlingWhiteQueenside != 0 {
		castling += "Q"
	}
	if c.Castling&castlingBlackKingside != 0 {
		castling += "k"
	}
	if c.Castling&castlingBlackQueenside != 0 {
		castling += "q"
	}
	if castling == "" {
		castling = "-"
	}
	sb.WriteString(castling)
	sb.WriteString(" ")

	if c.EnPassant >= 0 {
		sb.WriteString(sqToStr(c.EnPassant))
	} else {
		sb.WriteString("-")
	}

	sb.WriteString(fmt.Sprintf(" %d %d", c.Halfmove, c.Fullmove))
	return sb.String()
}

func (c *chess) tick(now time.Time) bool {
	turnIdx := 0
	if c.TurnColor == 'b' {
		turnIdx = 1
	}
	slow := c.Players[turnIdx]
	other := c.Players[1-turnIdx]
	c.logf("%s hết giờ (%d phút không đi).", slow.Name, c.Opts.TurnMinutes)
	c.end(fmt.Sprintf("%s thắng (đối thủ hết giờ)!", other.Name), []int64{other.ID})
	return true
}

type chessPublic struct {
	Opts          chessOptions        `json:"opts"`
	FEN           string              `json:"fen"`
	Board         string              `json:"board"`
	TurnColor     string              `json:"turn_color"`
	TurnPlayerID  int64               `json:"turn_player_id"`
	WhitePlayerID int64               `json:"white_player_id"`
	BlackPlayerID int64               `json:"black_player_id"`
	LastMove      *chessMove          `json:"last_move,omitempty"`
	InCheck       bool                `json:"in_check"`
	CheckSquare   string              `json:"check_square,omitempty"`
	DrawOffered   bool                `json:"draw_offered"`
	DrawOfferedBy int64               `json:"draw_offered_by,omitempty"`
	LegalMoves    map[string][]string `json:"legal_moves,omitempty"` // from -> []to
	Captured      map[string]int      `json:"captured"`              // piece letter -> count
}

func (c *chess) view(viewer int64) (any, any) {
	c.normalize()
	pub := chessPublic{
		Opts:          c.Opts,
		FEN:           c.fen(),
		Board:         string(c.Board[:]),
		TurnColor:     string(c.TurnColor),
		InCheck:       c.isInCheck(c.TurnColor),
		LastMove:      c.LastMove,
		DrawOffered:   c.DrawOfferedBy != 0,
		DrawOfferedBy: c.DrawOfferedBy,
		Captured:      make(map[string]int),
	}

	if len(c.Players) >= 2 {
		pub.WhitePlayerID = c.Players[0].ID
		pub.BlackPlayerID = c.Players[1].ID
		if c.TurnColor == 'w' {
			pub.TurnPlayerID = c.Players[0].ID
		} else {
			pub.TurnPlayerID = c.Players[1].ID
		}
	}

	if pub.InCheck {
		kingChar := byte('K')
		if c.TurnColor == 'b' {
			kingChar = 'k'
		}
		for i := 0; i < 64; i++ {
			if c.Board[i] == kingChar {
				pub.CheckSquare = sqToStr(i)
				break
			}
		}
	}

	// Đếm quân đã bị ăn (so với 32 quân ban đầu)
	origCounts := map[byte]int{
		'P': 8, 'N': 2, 'B': 2, 'R': 2, 'Q': 1,
		'p': 8, 'n': 2, 'b': 2, 'r': 2, 'q': 1,
	}
	currentCounts := make(map[byte]int)
	for _, p := range c.Board {
		if p != '.' && p != 'K' && p != 'k' {
			currentCounts[p]++
		}
	}
	for p, orig := range origCounts {
		diff := orig - currentCounts[p]
		if diff > 0 {
			pub.Captured[string(p)] = diff
		}
	}

	// Nếu đang chơi: tính danh sách legal moves gửi cho client
	if c.Status == Playing {
		legals := c.generateLegalMoves(c.TurnColor)
		moveMap := make(map[string][]string)
		for _, m := range legals {
			dest := m.To
			if m.Promotion != "" {
				dest += m.Promotion
			}
			moveMap[m.From] = append(moveMap[m.From], dest)
		}
		pub.LegalMoves = moveMap
	}

	return pub, nil
}
