package game

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Cờ caro 15×15, hai người: X (chủ phòng) đi trước, ai có 5 quân liền hàng ngang/dọc/chéo
// trước thì thắng. Luật "chặn hai đầu" (mặc định bật, như chơi ngoài đời): hàng 5 bị quân
// đối phương chặn cả hai đầu thì không tính.

const (
	caroSize     = 15
	caroTurnTime = 3 * time.Minute
)

type caroOptions struct {
	// Tắt luật chặn hai đầu.
	FreeStyle bool `json:"free_style"`
}

type caro struct {
	Base
	Opts  caroOptions `json:"opts"`
	Board string      `json:"board,omitempty"` // caroSize² ký tự: '.', 'x', 'o'
	Turn  int         `json:"turn"`            // 0: X (Players[0]), 1: O
	Last  int         `json:"last"`
	Win   []int       `json:"win,omitempty"`
	Moves int         `json:"moves"`
}

func (c *caro) limits() (int, int) { return 2, 2 }
func (c *caro) autoStart() bool    { return true }
func (c *caro) options() any       { return &c.Opts }
func (c *caro) normalize()         {}

func (c *caro) start(now time.Time) error {
	c.Board = strings.Repeat(".", caroSize*caroSize)
	c.Turn, c.Last, c.Moves, c.Win = 0, -1, 0, nil
	c.logf("%s (X) đấu với %s (O). X đi trước.", c.Players[0].Name, c.Players[1].Name)
	c.setDeadline(now, caroTurnTime)
	return nil
}

func mark(turn int) byte { return "xo"[turn] }

type moveData struct {
	Cell int `json:"cell"`
}

func (c *caro) act(p int64, action string, data json.RawMessage, now time.Time) error {
	me := c.index(p)
	switch action {
	case "resign":
		other := c.Players[1-me]
		c.logf("%s chịu thua.", c.Players[me].Name)
		c.end(other.Name+" thắng (đối thủ chịu thua).", []int64{other.ID})
		return nil
	case "move":
	default:
		return Error("Hành động không hợp lệ.")
	}
	if me != c.Turn {
		return Error("Chưa tới lượt bạn.")
	}
	var d moveData
	if err := decode(data, &d); err != nil {
		return err
	}
	if d.Cell < 0 || d.Cell >= caroSize*caroSize || c.Board[d.Cell] != '.' {
		return Error("Ô này không đánh được.")
	}
	b := []byte(c.Board)
	b[d.Cell] = mark(me)
	c.Board = string(b)
	c.Last = d.Cell
	c.Moves++
	if line := c.winLine(d.Cell); line != nil {
		c.Win = line
		c.logf("%s thắng sau %d nước!", c.Players[me].Name, c.Moves)
		c.end(c.Players[me].Name+" thắng!", []int64{p})
		return nil
	}
	if c.Moves == caroSize*caroSize {
		c.logf("Hết chỗ đánh, hoà.")
		c.end("Hoà!", nil)
		return nil
	}
	c.Turn = 1 - c.Turn
	c.setDeadline(now, caroTurnTime)
	return nil
}

// winLine: các ô của hàng thắng đi qua cell (nil nếu chưa thắng).
func (c *caro) winLine(cell int) []int {
	r0, c0 := cell/caroSize, cell%caroSize
	m := c.Board[cell]
	at := func(r, col int) byte {
		if r < 0 || col < 0 || r >= caroSize || col >= caroSize {
			return 0 // mép bàn: không tính là bị chặn
		}
		return c.Board[r*caroSize+col]
	}
	for _, d := range [][2]int{{0, 1}, {1, 0}, {1, 1}, {1, -1}} {
		line := []int{cell}
		r, col := r0-d[0], c0-d[1]
		for at(r, col) == m {
			line = append([]int{r*caroSize + col}, line...)
			r, col = r-d[0], col-d[1]
		}
		endA := at(r, col)
		r, col = r0+d[0], c0+d[1]
		for at(r, col) == m {
			line = append(line, r*caroSize+col)
			r, col = r+d[0], col+d[1]
		}
		endB := at(r, col)
		if len(line) < 5 {
			continue
		}
		blocked := endA != 0 && endA != '.' && endB != 0 && endB != '.'
		if blocked && !c.Opts.FreeStyle {
			continue
		}
		return line
	}
	return nil
}

func (c *caro) tick(now time.Time) bool {
	slow := c.Players[c.Turn]
	other := c.Players[1-c.Turn]
	c.logf("%s hết giờ (%d phút không đi).", slow.Name, int(caroTurnTime.Minutes()))
	c.end(fmt.Sprintf("%s thắng (đối thủ hết giờ).", other.Name), []int64{other.ID})
	return true
}

type caroPublic struct {
	Size      int    `json:"size"`
	Board     string `json:"board,omitempty"`
	Turn      int64  `json:"turn,omitempty"` // id người tới lượt
	Last      int    `json:"last"`
	Win       []int  `json:"win,omitempty"`
	Moves     int    `json:"moves"`
	FreeStyle bool   `json:"free_style"`
}

func (c *caro) view(viewer int64) (any, any) {
	pub := caroPublic{Size: caroSize, Board: c.Board, Last: c.Last, Win: c.Win, Moves: c.Moves, FreeStyle: c.Opts.FreeStyle}
	if c.Status == Playing {
		pub.Turn = c.Players[c.Turn].ID
	}
	if c.Board == "" {
		pub.Last = -1
	}
	return pub, nil
}
