package game

// Diễn biến một ván đã lưu, viết thành chữ để Trợ lý AI đọc và phân tích (nước đi, thế cờ cuối).

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RecordMove là một nước đi (hoặc một ván nhỏ với Oẳn tù tì) theo thứ tự.
type RecordMove struct {
	Side int    // 0: người đi trước (Đỏ / Trắng / X), 1: người đi sau
	Text string // vd. "Pháo h2→e2", "Mã g1→f3 ăn Tốt, chiếu", "(7,8)"
}

// Record: diễn biến ván để đọc lại.
type Record struct {
	Kind    string
	Title   string
	Status  Status
	Players []Player
	Sides   [2]string // tên phe của Players[0], Players[1]: "Đỏ"/"Đen", "Trắng"/"Đen", "X"/"O"
	Moves   []RecordMove
	// Nước đi lấy lại từ nhật ký (ván cũ chưa lưu History): nhật ký chỉ giữ 200 dòng cuối nên
	// có thể thiếu phần đầu ván.
	FromLog bool
	// Ván caro cũ không lưu thứ tự: chỉ biết vị trí quân lúc cuối.
	NoOrder bool
	Board   string // thế cờ hiện tại / cuối ván, dạng chữ
	Notes   []string
	Result  string
	Winners []int64
	Log     []string
}

// Describe đọc lại ván đã lưu (Marshal) thành diễn biến đọc được.
func Describe(kind string, data []byte) (*Record, error) {
	g, err := Load(kind, data)
	if err != nil {
		return nil, err
	}
	b := g.r.base()
	rec := &Record{
		Kind: kind, Title: Title(kind), Status: b.Status, Players: b.Players,
		Result: b.Result, Winners: b.Winners, Log: b.Log,
	}
	switch r := g.r.(type) {
	case *xiangqi:
		rec.Sides = [2]string{"Đỏ", "Đen"}
		r.describe(rec)
	case *chess:
		rec.Sides = [2]string{"Trắng", "Đen"}
		r.describe(rec)
	case *caro:
		rec.Sides = [2]string{"X", "O"}
		r.describe(rec)
	case *rps:
		r.describe(rec)
	}
	return rec, nil
}

/* ---------- Cờ tướng ---------- */

var xqNames = map[byte]string{'k': "Tướng", 'a': "Sĩ", 'e': "Tượng", 'h': "Mã", 'r': "Xe", 'c': "Pháo", 'p': "Tốt"}

func pieceName(names map[byte]string, p string) string {
	if p == "" {
		return "quân"
	}
	if n, ok := names[strings.ToLower(p)[0]]; ok {
		return n
	}
	return p
}

func moveText(names map[byte]string, piece, from, to, captured, promo string, check bool) string {
	s := fmt.Sprintf("%s %s→%s", pieceName(names, piece), from, to)
	if captured != "" {
		s += " ăn " + pieceName(names, captured)
	}
	if promo != "" {
		s += " phong " + pieceName(names, promo)
	}
	if check {
		s += ", chiếu"
	}
	return s
}

// Dòng nhật ký nước đi (ván cũ): "Tên (Đỏ): h2 → e2 (ăn quân) [Chiếu!]", cờ vua thêm "=Q" sau ô đến.
var logMoveRe = regexp.MustCompile(`\((Đỏ|Đen|Trắng)\): ([a-i]\d+) → ([a-i]\d+)(=[QRBNqrbn])?( \(ăn quân\))?( \[Chiếu!\])?$`)

func movesFromLog(log []string, first string, names map[byte]string) []RecordMove {
	var out []RecordMove
	for _, l := range log {
		m := logMoveRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		side := 1
		if m[1] == first {
			side = 0
		}
		s := m[2] + "→" + m[3]
		if m[4] != "" {
			s += " phong " + pieceName(names, m[4][1:])
		}
		if m[5] != "" {
			s += " ăn quân"
		}
		if m[6] != "" {
			s += ", chiếu"
		}
		out = append(out, RecordMove{Side: side, Text: s})
	}
	return out
}

func (x *xiangqi) describe(rec *Record) {
	if len(x.History) > 0 {
		for i, m := range x.History {
			rec.Moves = append(rec.Moves, RecordMove{Side: i % 2, Text: moveText(xqNames, m.Piece, m.From, m.To, m.Captured, "", m.IsCheck)})
		}
	} else {
		rec.Moves = movesFromLog(x.Log, "Đỏ", xqNames)
		rec.FromLog = len(rec.Moves) > 0
	}
	var sb strings.Builder
	sb.WriteString("Toạ độ: cột a–i từ trái sang phải nhìn từ phía Đỏ, hàng 0 là hàng cuối của Đỏ, hàng 9 là hàng cuối của Đen.\n")
	sb.WriteString("Chữ HOA = quân Đỏ, chữ thường = quân Đen; K Tướng, A Sĩ, E Tượng, H Mã, R Xe, C Pháo, P Tốt; . = trống.\n")
	sb.WriteString("   a b c d e f g h i\n")
	for r := 9; r >= 0; r-- {
		fmt.Fprintf(&sb, "%d  ", r)
		for f := 0; f < 9; f++ {
			ch := x.Board[r*9+f]
			if ch == 0 {
				ch = '.'
			}
			sb.WriteByte(ch)
			if f < 8 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
	}
	rec.Board = sb.String()
	if x.Status == Playing {
		rec.Notes = append(rec.Notes, "Lượt đi: "+xqColorName(x.TurnColor))
	}
}

/* ---------- Cờ vua ---------- */

var chessNames = map[byte]string{'k': "Vua", 'q': "Hậu", 'r': "Xe", 'b': "Tượng", 'n': "Mã", 'p': "Tốt"}

func (c *chess) describe(rec *Record) {
	if len(c.History) > 0 {
		for i, m := range c.History {
			rec.Moves = append(rec.Moves, RecordMove{Side: i % 2, Text: moveText(chessNames, m.Piece, m.From, m.To, m.Captured, m.Promotion, m.IsCheck)})
		}
	} else {
		rec.Moves = movesFromLog(c.Log, "Trắng", chessNames)
		rec.FromLog = len(rec.Moves) > 0
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "FEN: %s\n", c.fen())
	sb.WriteString("Chữ HOA = Trắng, chữ thường = Đen; K Vua, Q Hậu, R Xe, B Tượng, N Mã, P Tốt; . = trống.\n")
	sb.WriteString("   a b c d e f g h\n")
	for r := 7; r >= 0; r-- {
		fmt.Fprintf(&sb, "%d  ", r+1)
		for f := 0; f < 8; f++ {
			ch := c.Board[r*8+f]
			if ch == 0 {
				ch = '.'
			}
			sb.WriteByte(ch)
			if f < 7 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
	}
	rec.Board = sb.String()
	if c.Status == Playing {
		rec.Notes = append(rec.Notes, "Lượt đi: "+colorName(c.TurnColor))
	}
}

/* ---------- Cờ caro ---------- */

func (c *caro) describe(rec *Record) {
	c.normalize()
	if len(c.Order) > 0 {
		for i, p := range c.Order {
			rec.Moves = append(rec.Moves, RecordMove{Side: i % 2, Text: fmt.Sprintf("(%d,%d)", p.R, p.C)})
		}
	} else if len(c.Pieces) > 0 {
		rec.NoOrder = true
	}
	// bàn cờ: vùng có quân, nới 1 ô mỗi phía
	r0, r1, c0, c1 := 0, -1, 0, -1
	first := true
	for k := range c.Pieces {
		var r, col int
		if _, err := fmt.Sscanf(k, "%d,%d", &r, &col); err != nil {
			continue
		}
		if first {
			r0, r1, c0, c1, first = r, r, col, col, false
			continue
		}
		r0, r1, c0, c1 = min(r0, r), max(r1, r), min(c0, col), max(c1, col)
	}
	if first {
		rec.Board = "(bàn trống)"
		return
	}
	r0, r1, c0, c1 = r0-1, r1+1, c0-1, c1+1
	var sb strings.Builder
	sb.WriteString("Toạ độ (hàng,cột); X = người đi trước, O = người đi sau, . = trống. Thắng khi có 5 quân liền")
	if !c.Opts.FreeStyle {
		sb.WriteString(" (bị chặn cả hai đầu thì không tính)")
	}
	sb.WriteString(".\n")
	fmt.Fprintf(&sb, "Hàng %d..%d, cột %d..%d:\n", r0, r1, c0, c1)
	for r := r0; r <= r1; r++ {
		fmt.Fprintf(&sb, "%4d  ", r)
		for col := c0; col <= c1; col++ {
			switch c.Pieces[coordKey(r, col)] {
			case "x":
				sb.WriteByte('X')
			case "o":
				sb.WriteByte('O')
			default:
				sb.WriteByte('.')
			}
		}
		sb.WriteByte('\n')
	}
	rec.Board = sb.String()
	if len(c.Win) > 0 {
		pts := make([]string, len(c.Win))
		for i, p := range c.Win {
			pts[i] = fmt.Sprintf("(%d,%d)", p.R, p.C)
		}
		sort.Strings(pts)
		rec.Notes = append(rec.Notes, "Đường thắng: "+strings.Join(pts, " "))
	}
}

/* ---------- Oẳn tù tì ---------- */

func (g *rps) describe(rec *Record) {
	rec.Sides = [2]string{"", ""}
	for i, r := range g.Rounds {
		res := "hoà"
		switch r.Winner {
		case 0:
		default:
			res = g.name(r.Winner) + " thắng"
		}
		pick := func(c string) string {
			if c == "" {
				return "không ra"
			}
			return rpsNames[c]
		}
		rec.Moves = append(rec.Moves, RecordMove{Side: -1, Text: fmt.Sprintf("Ván %d: %s ra %s, %s ra %s → %s",
			i+1, g.Players[0].Name, pick(r.Picks[0]), g.Players[1].Name, pick(r.Picks[1]), res)})
	}
	rec.Notes = append(rec.Notes, fmt.Sprintf("Tỉ số: %d–%d (chạm %d)", g.Score[0], g.Score[1], g.need()))
}
