package game

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Cờ tướng (Xiangqi / Chinese Chess) 9×10, hai người chơi: Đỏ đi trước, Đen đi sau.
// Tuân thủ đầy đủ luật Cờ tướng chuẩn quốc tế (WXF):
// - Cản mắt tượng, cản chân mã.
// - Pháo nhảy ăn quân qua đúng 1 ngòi.
// - Lộ diện tướng (hai tướng đối mặt trực diện trên cùng cột không có quân cản).
// - Sĩ và Tướng hoạt động tuyệt đối trong Cửu cung.
// - Tốt chưa qua sông vs đã qua sông (không bao giờ được đi lùi).
// - Bức tử (Hết nước đi / Stalemate): Người bị hết nước đi bị xử THUA.
// - Chiếu bí, xin hàng, đề nghị hoà, xử thua khi hết giờ.

const (
	xiangqiBoardFiles = 9
	xiangqiBoardRanks = 10
	xiangqiBoardSize  = 90
	xiangqiTurnTime   = 3 * time.Minute
)

type xiangqiOptions struct {
	// Thời gian cho mỗi nước đi (phút, mặc định 3).
	TurnMinutes int `json:"turn_minutes"`
	// Người đi trước (cầm Đỏ): "random" (mặc định), "host", "guest".
	First string `json:"first"`
}

type xiangqiMove struct {
	From     string `json:"from"`               // vd: "e0"
	To       string `json:"to"`                 // vd: "e1"
	Piece    string `json:"piece"`              // vd: "K", "C", "h", ...
	Captured string `json:"captured,omitempty"` // vd: "p", ...
	IsCheck  bool   `json:"is_check,omitempty"` // Nước đi tạo thế chiếu
}

type xiangqi struct {
	Base
	Opts          xiangqiOptions `json:"opts"`
	Board         [90]byte       `json:"board"`
	TurnColor     byte           `json:"turn_color"` // 'r' (Đỏ) hoặc 'b' (Đen)
	Halfmove      int            `json:"halfmove"`   // Số nửa nước kể từ lần ăn quân gần nhất (luật 60 nước)
	Fullmove      int            `json:"fullmove"`   // Số lượt đi (bắt đầu từ 1, tăng sau mỗi lượt Đen)
	LastMove      *xiangqiMove   `json:"last_move,omitempty"`
	DrawOfferedBy int64          `json:"draw_offered_by,omitempty"` // ID người chơi đang xin hoà (0 nếu không có)
}

func (x *xiangqi) limits() (int, int) { return 2, 2 }
func (x *xiangqi) autoStart() bool    { return true }
func (x *xiangqi) options() any       { return &x.Opts }

func (x *xiangqi) normalize() {
	if x.Opts.TurnMinutes <= 0 {
		x.Opts.TurnMinutes = 3
	}
	if x.Opts.First != "host" && x.Opts.First != "guest" {
		x.Opts.First = "random"
	}
}

// Bàn cờ Cờ tướng ban đầu:
// Rank 9 (Đen): r h e a k a e h r  (hàng đỉnh, a9..i9: 81..89)
// Rank 8: . . . . . . . . .
// Rank 7 (Đen): . c . . . . . c .  (Pháo đen tại b7, h7)
// Rank 6 (Đen): p . p . p . p . p  (Tốt đen tại a6, c6, e6, g6, i6)
// Rank 5: . . . . . . . . . (bờ sông phía Đen)
// ~~~~~~~~~~~~~~~~~~~~~~~~~ SÔNG ~~~~~~~~~~~~~~~~~~~~~~~~~
// Rank 4: . . . . . . . . . (bờ sông phía Đỏ)
// Rank 3 (Đỏ): P . P . P . P . P  (Tốt đỏ tại a3, c3, e3, g3, i3)
// Rank 2 (Đỏ): . C . . . . . C .  (Pháo đỏ tại b2, h2)
// Rank 1: . . . . . . . . .
// Rank 0 (Đỏ): R H E A K A E H R  (hàng đáy, a0..i0: 0..8)
func initialXiangqiBoard() [90]byte {
	var b [90]byte
	for i := range b {
		b[i] = '.'
	}

	// Đỏ (Chữ hoa) - Ranks 0..3
	copy(b[0:9], []byte{'R', 'H', 'E', 'A', 'K', 'A', 'E', 'H', 'R'})
	b[2*9+1] = 'C' // b2
	b[2*9+7] = 'C' // h2
	b[3*9+0] = 'P' // a3
	b[3*9+2] = 'P' // c3
	b[3*9+4] = 'P' // e3
	b[3*9+6] = 'P' // g3
	b[3*9+8] = 'P' // i3

	// Đen (Chữ thường) - Ranks 6..9
	b[6*9+0] = 'p' // a6
	b[6*9+2] = 'p' // c6
	b[6*9+4] = 'p' // e6
	b[6*9+6] = 'p' // g6
	b[6*9+8] = 'p' // i6
	b[7*9+1] = 'c' // b7
	b[7*9+7] = 'c' // h7
	copy(b[9*9:9*9+9], []byte{'r', 'h', 'e', 'a', 'k', 'a', 'e', 'h', 'r'})

	return b
}

func (x *xiangqi) start(now time.Time) error {
	x.normalize()
	x.Board = initialXiangqiBoard()

	// Xác định ai là Đỏ (Players[0] đi trước), ai là Đen (Players[1])
	if x.Opts.First == "host" {
		if x.Players[0].ID != x.HostID {
			x.Players[0], x.Players[1] = x.Players[1], x.Players[0]
		}
	} else if x.Opts.First == "guest" {
		if x.Players[0].ID == x.HostID {
			x.Players[0], x.Players[1] = x.Players[1], x.Players[0]
		}
	} else {
		shuffle(x.Players)
	}

	x.TurnColor = 'r'
	x.Halfmove = 0
	x.Fullmove = 1
	x.LastMove = nil
	x.DrawOfferedBy = 0

	x.logf("%s (Đỏ) đấu với %s (Đen). Đỏ đi trước.", x.Players[0].Name, x.Players[1].Name)
	x.setDeadline(now, time.Duration(x.Opts.TurnMinutes)*time.Minute)
	return nil
}

func xqSqToStr(sq int) string {
	if sq < 0 || sq >= xiangqiBoardSize {
		return ""
	}
	file := sq % xiangqiBoardFiles
	rank := sq / xiangqiBoardFiles
	return fmt.Sprintf("%c%d", 'a'+file, rank)
}

func strToXqSq(s string) (int, bool) {
	if len(s) != 2 {
		return -1, false
	}
	file := int(s[0] - 'a')
	rank := int(s[1] - '0')
	if file < 0 || file >= xiangqiBoardFiles || rank < 0 || rank >= xiangqiBoardRanks {
		return -1, false
	}
	return rank*xiangqiBoardFiles + file, true
}

func isPieceColorXq(p byte, color byte) bool {
	if color == 'r' {
		return p >= 'A' && p <= 'Z'
	}
	return p >= 'a' && p <= 'z'
}

type xqMoveInput struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (x *xiangqi) act(p int64, action string, data json.RawMessage, now time.Time) error {
	me := x.index(p)
	if me < 0 || me >= len(x.Players) {
		return Error("Bạn không tham gia ván này.")
	}

	switch action {
	case "resign":
		other := 1 - me
		x.logf("%s đầu hàng.", x.Players[me].Name)
		x.end(fmt.Sprintf("%s thắng (%s đầu hàng)!", x.Players[other].Name, x.Players[me].Name), []int64{x.Players[other].ID})
		return nil

	case "draw_offer":
		if x.DrawOfferedBy != 0 {
			if x.DrawOfferedBy == p {
				return Error("Bạn đã gửi lời mời hoà, đang chờ đối phương phản hồi.")
			}
			return Error("Đối phương đã gửi lời mời hoà, bạn có thể chấp nhận.")
		}
		x.DrawOfferedBy = p
		x.logf("%s đề nghị hoà.", x.Players[me].Name)
		return nil

	case "draw_accept":
		if x.DrawOfferedBy == 0 {
			return Error("Hiện không có đề nghị hoà nào.")
		}
		if x.DrawOfferedBy == p {
			return Error("Bạn không thể tự chấp nhận lời mời hoà của chính mình.")
		}
		x.logf("%s chấp nhận lời mời hoà. Ván cờ hoà.", x.Players[me].Name)
		x.end("Ván cờ hoà (hai bên đồng ý)!", nil)
		return nil

	case "draw_decline":
		if x.DrawOfferedBy == 0 {
			return Error("Hiện không có đề nghị hoà nào.")
		}
		if x.DrawOfferedBy == p {
			return Error("Bạn không thể từ chối lời mời hoà của chính mình.")
		}
		x.DrawOfferedBy = 0
		x.logf("%s từ chối lời mời hoà.", x.Players[me].Name)
		return nil

	case "move":
		// Tiếp tục xử lý nước đi
	default:
		return Error("Hành động không hợp lệ.")
	}

	myColor := byte('r')
	if me == 1 {
		myColor = 'b'
	}

	if myColor != x.TurnColor {
		return Error("Chưa tới lượt bạn.")
	}

	var input xqMoveInput
	if err := decode(data, &input); err != nil {
		return err
	}
	input.From = strings.ToLower(strings.TrimSpace(input.From))
	input.To = strings.ToLower(strings.TrimSpace(input.To))

	from, okFrom := strToXqSq(input.From)
	to, okTo := strToXqSq(input.To)
	if !okFrom || !okTo {
		return Error("Toạ độ ô cờ không hợp lệ.")
	}

	legalMoves := x.generateLegalMoves(x.TurnColor)
	var chosenMove *xiangqiMove
	for _, m := range legalMoves {
		if m.From == input.From && m.To == input.To {
			chosenMove = &m
			break
		}
	}

	if chosenMove == nil {
		return Error("Nước cờ không hợp lệ hoặc đang bị chiếu.")
	}

	// Thực hiện nước đi
	x.applyMove(from, to)
	x.LastMove = chosenMove
	x.DrawOfferedBy = 0 // Huỷ đề nghị hoà khi có nước đi mới

	oppColor := byte('b')
	if x.TurnColor == 'b' {
		oppColor = 'r'
	}
	oppInCheck := x.isInCheck(oppColor)
	chosenMove.IsCheck = oppInCheck

	// Đổi lượt
	x.TurnColor = oppColor
	if x.TurnColor == 'r' {
		x.Fullmove++
	}

	// Ghi nhật ký nước cờ
	moveLog := fmt.Sprintf("%s (%s): %s → %s", x.Players[me].Name, xqColorName(myColor), input.From, input.To)
	if chosenMove.Captured != "" {
		moveLog += " (ăn quân)"
	}
	if oppInCheck {
		moveLog += " [Chiếu!]"
	}
	x.logf("%s", moveLog)

	// Kiểm tra trạng thái kết thúc ván đấu
	oppLegalMoves := x.generateLegalMoves(x.TurnColor)
	if len(oppLegalMoves) == 0 {
		if oppInCheck {
			// Chiếu bí (Checkmate)
			x.logf("Chiếu bí! %s thắng ván cờ.", x.Players[me].Name)
			x.end(fmt.Sprintf("%s thắng (Chiếu bí đối phương)!", x.Players[me].Name), []int64{p})
			return nil
		}
		// Bức tử / Hết nước đi (Stalemate)
		// Theo luật Cờ tướng, bên bị hết nước đi BỊ XỬ THUA!
		x.logf("Bức tử! %s hết nước đi hợp lệ. %s thắng ván cờ.", x.Players[1-me].Name, x.Players[me].Name)
		x.end(fmt.Sprintf("%s thắng (Đối phương hết nước đi / Bức tử)!", x.Players[me].Name), []int64{p})
		return nil
	}

	// Kiểm tra luật 60 nước không ăn quân (120 nửa nước)
	if x.Halfmove >= 120 {
		x.logf("Luật 60 nước không ăn quân. Ván cờ hoà.")
		x.end("Ván cờ hoà theo luật 60 nước!", nil)
		return nil
	}

	d := time.Duration(x.Opts.TurnMinutes) * time.Minute
	x.setDeadline(now, d)
	return nil
}

func xqColorName(c byte) string {
	if c == 'r' {
		return "Đỏ"
	}
	return "Đen"
}

func (x *xiangqi) applyMove(from, to int) {
	target := x.Board[to]
	if target != '.' {
		x.Halfmove = 0
	} else {
		x.Halfmove++
	}
	x.Board[to] = x.Board[from]
	x.Board[from] = '.'
}

// areGeneralsFacing kiểm tra hai Tướng có đang đối mặt nhau trên cùng cột mà không có quân cản.
func areGeneralsFacing(board [90]byte) bool {
	redKingSq, blackKingSq := -1, -1
	for sq := 0; sq < xiangqiBoardSize; sq++ {
		p := board[sq]
		if p == 'K' {
			redKingSq = sq
		} else if p == 'k' {
			blackKingSq = sq
		}
	}
	if redKingSq == -1 || blackKingSq == -1 {
		return false
	}

	rf, rr := redKingSq%xiangqiBoardFiles, redKingSq/xiangqiBoardFiles
	bf, br := blackKingSq%xiangqiBoardFiles, blackKingSq/xiangqiBoardFiles

	if rf != bf {
		return false
	}

	// Duyệt các ô giữa 2 tướng trên cùng cột rf
	minRank, maxRank := rr, br
	if minRank > maxRank {
		minRank, maxRank = maxRank, minRank
	}

	for r := minRank + 1; r < maxRank; r++ {
		if board[r*xiangqiBoardFiles+rf] != '.' {
			return false // Có quân cản
		}
	}

	return true // Đối mặt trực tiếp không có quân cản!
}

// isInCheck: Kiểm tra Tướng của `color` có đang bị chiếu hay không.
func (x *xiangqi) isInCheck(color byte) bool {
	return isInCheckOnBoard(x.Board, color)
}

func isInCheckOnBoard(board [90]byte, color byte) bool {
	// Tìm vị trí Tướng
	kingChar := byte('K')
	oppColor := byte('b')
	if color == 'b' {
		kingChar = 'k'
		oppColor = 'r'
	}

	kingSq := -1
	for sq := 0; sq < xiangqiBoardSize; sq++ {
		if board[sq] == kingChar {
			kingSq = sq
			break
		}
	}
	if kingSq == -1 {
		return true // Mất tướng coi như bị chiếu bí
	}

	// 1. Kiểm tra hai tướng đối mặt
	if areGeneralsFacing(board) {
		return true
	}

	kf, kr := kingSq%xiangqiBoardFiles, kingSq/xiangqiBoardFiles

	// 2. Tấn công từ Xe ('R'/'r') và Pháo ('C'/'c') theo 4 hướng trực giao
	dirs := [][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}
	oppRook := byte('r')
	oppCannon := byte('c')
	if oppColor == 'r' {
		oppRook = 'R'
		oppCannon = 'C'
	}

	for _, d := range dirs {
		screenCount := 0
		f, r := kf+d[0], kr+d[1]
		for f >= 0 && f < xiangqiBoardFiles && r >= 0 && r < xiangqiBoardRanks {
			p := board[r*xiangqiBoardFiles+f]
			if p != '.' {
				if screenCount == 0 {
					// Quân đầu tiên gặp: nếu là Xe địch thì đang chiếu!
					if p == oppRook {
						return true
					}
					screenCount++
				} else if screenCount == 1 {
					// Quân thứ 2 gặp (sau 1 ngòi): nếu là Pháo địch thì đang chiếu!
					if p == oppCannon {
						return true
					}
					break
				}
			}
			f += d[0]
			r += d[1]
		}
	}

	// 3. Tấn công từ Mã ('H'/'h')
	oppHorse := byte('h')
	if oppColor == 'r' {
		oppHorse = 'H'
	}
	// Mã nhảy tới (kf, kr) khi nó đứng tại các ô cách King L-shape
	horseDeltas := []struct {
		df, dr   int // vị trí mã đối với King
		legF, legR int // chân cản đối với King (nơi mã phải không bị cản để nhảy tới King)
	}{
		// Mã đứng tại (kf-1, kr-2), chân mã tại (kf-1, kr-1)
		{-1, -2, -1, -1},
		{1, -2, 1, -1},
		{-1, 2, -1, 1},
		{1, 2, 1, 1},
		{-2, -1, -1, -1},
		{-2, 1, -1, 1},
		{2, -1, 1, -1},
		{2, 1, 1, 1},
	}
	for _, hd := range horseDeltas {
		hf, hr := kf+hd.df, kr+hd.dr
		if hf >= 0 && hf < xiangqiBoardFiles && hr >= 0 && hr < xiangqiBoardRanks {
			if board[hr*xiangqiBoardFiles+hf] == oppHorse {
				// Kiểm tra ô cản chân mã: chân mã nằm giữa mã và ô King
				// Với mã tại (hf, hr) nhảy tới (kf, kr):
				// Nếu |hf-kf| == 1 && |hr-kr| == 2: chân mã tại (hf, hr - (hr-kr)/2)
				// Tức là (kf + hd.legF, kr + hd.legR)
				lf, lr := kf+hd.legF, kr+hd.legR
				if board[lr*xiangqiBoardFiles+lf] == '.' {
					return true
				}
			}
		}
	}

	// 4. Tấn công từ Tốt ('P'/'p')
	oppPawn := byte('p')
	if oppColor == 'r' {
		oppPawn = 'P'
	}
	// Đỏ ở dưới tiến lên, Đen ở trên tiến xuống.
	// Nếu King là Đỏ: Tốt Đen tấn công King từ trên xuống (kr+1) hoặc từ hai bên (kf±1, kr).
	// Nếu King là Đen: Tốt Đỏ tấn công King từ dưới lên (kr-1) hoặc từ hai bên (kf±1, kr).
	if color == 'r' {
		// Tốt Đen ở ngay trên King
		if kr < xiangqiBoardRanks-1 && board[(kr+1)*xiangqiBoardFiles+kf] == oppPawn {
			return true
		}
		// Tốt Đen ở hai bên King (đã qua sông)
		if kf > 0 && board[kr*xiangqiBoardFiles+kf-1] == oppPawn {
			return true
		}
		if kf < xiangqiBoardFiles-1 && board[kr*xiangqiBoardFiles+kf+1] == oppPawn {
			return true
		}
	} else {
		// Tốt Đỏ ở ngay dưới King
		if kr > 0 && board[(kr-1)*xiangqiBoardFiles+kf] == oppPawn {
			return true
		}
		// Tốt Đỏ ở hai bên King (đã qua sông)
		if kf > 0 && board[kr*xiangqiBoardFiles+kf-1] == oppPawn {
			return true
		}
		if kf < xiangqiBoardFiles-1 && board[kr*xiangqiBoardFiles+kf+1] == oppPawn {
			return true
		}
	}

	return false
}

// generatePseudoLegalMoves sinh tất cả các nước đi thô của `color` theo hình thế quân.
func (x *xiangqi) generatePseudoLegalMoves(color byte) []xiangqiMove {
	var moves []xiangqiMove
	oppColor := byte('b')
	if color == 'b' {
		oppColor = 'r'
	}

	for sq := 0; sq < xiangqiBoardSize; sq++ {
		p := x.Board[sq]
		if p == '.' || !isPieceColorXq(p, color) {
			continue
		}

		f := sq % xiangqiBoardFiles
		r := sq / xiangqiBoardFiles
		fromStr := xqSqToStr(sq)

		switch p {
		case 'K', 'k': // Tướng
			dirs := [][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}
			minR, maxR := 0, 2
			if p == 'k' {
				minR, maxR = 7, 9
			}
			for _, d := range dirs {
				nf, nr := f+d[0], r+d[1]
				if nf >= 3 && nf <= 5 && nr >= minR && nr <= maxR {
					tSq := nr*xiangqiBoardFiles + nf
					target := x.Board[tSq]
					if target == '.' || isPieceColorXq(target, oppColor) {
						cap := ""
						if target != '.' {
							cap = string(target)
						}
						moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
					}
				}
			}

		case 'A', 'a': // Sĩ
			diagDirs := [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
			minR, maxR := 0, 2
			if p == 'a' {
				minR, maxR = 7, 9
			}
			for _, d := range diagDirs {
				nf, nr := f+d[0], r+d[1]
				if nf >= 3 && nf <= 5 && nr >= minR && nr <= maxR {
					tSq := nr*xiangqiBoardFiles + nf
					target := x.Board[tSq]
					if target == '.' || isPieceColorXq(target, oppColor) {
						cap := ""
						if target != '.' {
							cap = string(target)
						}
						moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
					}
				}
			}

		case 'E', 'e': // Tượng
			diagDirs := [][2]int{{2, 2}, {2, -2}, {-2, 2}, {-2, -2}}
			minR, maxR := 0, 4 // Đỏ không qua sông
			if p == 'e' {
				minR, maxR = 5, 9 // Đen không qua sông
			}
			for _, d := range diagDirs {
				nf, nr := f+d[0], r+d[1]
				if nf >= 0 && nf < xiangqiBoardFiles && nr >= minR && nr <= maxR {
					// Mắt tượng ở trung điểm
					eyeF, eyeR := f+d[0]/2, r+d[1]/2
					if x.Board[eyeR*xiangqiBoardFiles+eyeF] == '.' {
						tSq := nr*xiangqiBoardFiles + nf
						target := x.Board[tSq]
						if target == '.' || isPieceColorXq(target, oppColor) {
							cap := ""
							if target != '.' {
								cap = string(target)
							}
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
						}
					}
				}
			}

		case 'H', 'h': // Mã
			horseMoves := []struct {
				df, dr     int
				legF, legR int
			}{
				{1, 2, 0, 1},
				{-1, 2, 0, 1},
				{1, -2, 0, -1},
				{-1, -2, 0, -1},
				{2, 1, 1, 0},
				{2, -1, 1, 0},
				{-2, 1, -1, 0},
				{-2, -1, -1, 0},
			}
			for _, hm := range horseMoves {
				nf, nr := f+hm.df, r+hm.dr
				if nf >= 0 && nf < xiangqiBoardFiles && nr >= 0 && nr < xiangqiBoardRanks {
					// Kiểm tra chân cản
					lf, lr := f+hm.legF, r+hm.legR
					if x.Board[lr*xiangqiBoardFiles+lf] == '.' {
						tSq := nr*xiangqiBoardFiles + nf
						target := x.Board[tSq]
						if target == '.' || isPieceColorXq(target, oppColor) {
							cap := ""
							if target != '.' {
								cap = string(target)
							}
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
						}
					}
				}
			}

		case 'R', 'r': // Xe
			dirs := [][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}
			for _, d := range dirs {
				nf, nr := f+d[0], r+d[1]
				for nf >= 0 && nf < xiangqiBoardFiles && nr >= 0 && nr < xiangqiBoardRanks {
					tSq := nr*xiangqiBoardFiles + nf
					target := x.Board[tSq]
					if target == '.' {
						moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p)})
					} else {
						if isPieceColorXq(target, oppColor) {
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: string(target)})
						}
						break // Bị cản, không đi tiếp được
					}
					nf += d[0]
					nr += d[1]
				}
			}

		case 'C', 'c': // Pháo
			dirs := [][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}
			for _, d := range dirs {
				nf, nr := f+d[0], r+d[1]
				screenFound := false
				for nf >= 0 && nf < xiangqiBoardFiles && nr >= 0 && nr < xiangqiBoardRanks {
					tSq := nr*xiangqiBoardFiles + nf
					target := x.Board[tSq]
					if !screenFound {
						if target == '.' {
							// Đi bình thường (không ăn quân)
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p)})
						} else {
							// Gặp quân cản đầu tiên -> làm ngòi
							screenFound = true
						}
					} else {
						// Đã có 1 ngòi cản
						if target != '.' {
							if isPieceColorXq(target, oppColor) {
								// Ăn quân đối phương sau 1 ngòi
								moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: string(target)})
							}
							break // Không nhảy qua quân thứ hai
						}
					}
					nf += d[0]
					nr += d[1]
				}
			}

		case 'P', 'p': // Tốt
			if p == 'P' {
				// Đỏ tiến lên (r + 1)
				if r < xiangqiBoardRanks-1 {
					tSq := (r + 1)*xiangqiBoardFiles + f
					target := x.Board[tSq]
					if target == '.' || isPieceColorXq(target, oppColor) {
						cap := ""
						if target != '.' {
							cap = string(target)
						}
						moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
					}
				}
				// Đã qua sông (r >= 5): được đi ngang sang trái/phải
				if r >= 5 {
					if f > 0 {
						tSq := r*xiangqiBoardFiles + f - 1
						target := x.Board[tSq]
						if target == '.' || isPieceColorXq(target, oppColor) {
							cap := ""
							if target != '.' {
								cap = string(target)
							}
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
						}
					}
					if f < xiangqiBoardFiles-1 {
						tSq := r*xiangqiBoardFiles + f + 1
						target := x.Board[tSq]
						if target == '.' || isPieceColorXq(target, oppColor) {
							cap := ""
							if target != '.' {
								cap = string(target)
							}
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
						}
					}
				}
			} else {
				// Đen tiến xuống (r - 1)
				if r > 0 {
					tSq := (r - 1)*xiangqiBoardFiles + f
					target := x.Board[tSq]
					if target == '.' || isPieceColorXq(target, oppColor) {
						cap := ""
						if target != '.' {
							cap = string(target)
						}
						moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
					}
				}
				// Đã qua sông (r <= 4): được đi ngang sang trái/phải
				if r <= 4 {
					if f > 0 {
						tSq := r*xiangqiBoardFiles + f - 1
						target := x.Board[tSq]
						if target == '.' || isPieceColorXq(target, oppColor) {
							cap := ""
							if target != '.' {
								cap = string(target)
							}
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
						}
					}
					if f < xiangqiBoardFiles-1 {
						tSq := r*xiangqiBoardFiles + f + 1
						target := x.Board[tSq]
						if target == '.' || isPieceColorXq(target, oppColor) {
							cap := ""
							if target != '.' {
								cap = string(target)
							}
							moves = append(moves, xiangqiMove{From: fromStr, To: xqSqToStr(tSq), Piece: string(p), Captured: cap})
						}
					}
				}
			}
		}
	}

	return moves
}

// generateLegalMoves lọc các nước đi hợp lệ: không để Tướng mình bị chiếu và không đối mặt tướng.
func (x *xiangqi) generateLegalMoves(color byte) []xiangqiMove {
	pseudo := x.generatePseudoLegalMoves(color)
	var legal []xiangqiMove

	for _, m := range pseudo {
		from, _ := strToXqSq(m.From)
		to, _ := strToXqSq(m.To)

		// Thử nước đi trên bàn cờ ảo
		simBoard := x.Board
		simBoard[to] = simBoard[from]
		simBoard[from] = '.'

		// 1. Kiểm tra hai tướng đối mặt
		if areGeneralsFacing(simBoard) {
			continue
		}

		// 2. Kiểm tra Tướng mình có bị chiếu hay không
		if isInCheckOnBoard(simBoard, color) {
			continue
		}

		legal = append(legal, m)
	}

	return legal
}

func (x *xiangqi) tick(now time.Time) bool {
	turnIdx := 0
	if x.TurnColor == 'b' {
		turnIdx = 1
	}
	slow := x.Players[turnIdx]
	other := x.Players[1-turnIdx]
	x.logf("%s hết giờ (%d phút không đi).", slow.Name, x.Opts.TurnMinutes)
	x.end(fmt.Sprintf("%s thắng (đối thủ hết giờ)!", other.Name), []int64{other.ID})
	return true
}

type xiangqiPublic struct {
	Opts          xiangqiOptions      `json:"opts"`
	Board         string              `json:"board"`
	TurnColor     string              `json:"turn_color"`
	TurnPlayerID  int64               `json:"turn_player_id"`
	RedPlayerID   int64               `json:"red_player_id"`
	BlackPlayerID int64               `json:"black_player_id"`
	LastMove      *xiangqiMove        `json:"last_move,omitempty"`
	InCheck       bool                `json:"in_check"`
	CheckSquare   string              `json:"check_square,omitempty"`
	DrawOffered   bool                `json:"draw_offered"`
	DrawOfferedBy int64               `json:"draw_offered_by,omitempty"`
	LegalMoves    map[string][]string `json:"legal_moves,omitempty"` // from -> []to
	Captured      map[string]int      `json:"captured"`              // piece letter -> count
}

func (x *xiangqi) view(viewer int64) (any, any) {
	x.normalize()

	inCheck := x.isInCheck(x.TurnColor)
	checkSquare := ""
	if inCheck {
		kingChar := byte('K')
		if x.TurnColor == 'b' {
			kingChar = 'k'
		}
		for sq := 0; sq < xiangqiBoardSize; sq++ {
			if x.Board[sq] == kingChar {
				checkSquare = xqSqToStr(sq)
				break
			}
		}
	}

	// Đếm quân bị ăn so với quân xuất phát chuẩn
	initialCount := map[string]int{
		"R": 2, "H": 2, "E": 2, "A": 2, "K": 1, "C": 2, "P": 5,
		"r": 2, "h": 2, "e": 2, "a": 2, "k": 1, "c": 2, "p": 5,
	}
	currentCount := make(map[string]int)
	for _, p := range x.Board {
		if p != '.' {
			currentCount[string(p)]++
		}
	}
	captured := make(map[string]int)
	for piece, initCnt := range initialCount {
		diff := initCnt - currentCount[piece]
		if diff > 0 {
			captured[piece] = diff
		}
	}

	pub := xiangqiPublic{
		Opts:          x.Opts,
		Board:         string(x.Board[:]),
		TurnColor:     string(x.TurnColor),
		InCheck:       inCheck,
		CheckSquare:   checkSquare,
		LastMove:      x.LastMove,
		DrawOffered:   x.DrawOfferedBy != 0,
		DrawOfferedBy: x.DrawOfferedBy,
		Captured:      captured,
	}

	if len(x.Players) >= 2 {
		pub.RedPlayerID = x.Players[0].ID
		pub.BlackPlayerID = x.Players[1].ID
		if x.TurnColor == 'r' {
			pub.TurnPlayerID = x.Players[0].ID
		} else {
			pub.TurnPlayerID = x.Players[1].ID
		}
	}

	// Nếu viewer là người tới lượt, gửi kèm legal_moves để UI hiển thị gợi ý
	if x.Status == Playing && len(x.Players) >= 2 {
		turnIdx := 0
		if x.TurnColor == 'b' {
			turnIdx = 1
		}
		if x.Players[turnIdx].ID == viewer {
			legals := x.generateLegalMoves(x.TurnColor)
			lmMap := make(map[string][]string)
			for _, m := range legals {
				lmMap[m.From] = append(lmMap[m.From], m.To)
			}
			pub.LegalMoves = lmMap
		}
	}

	return pub, nil
}
