package game

import (
	"encoding/json"
	"fmt"
	"time"
)

// Cờ caro tự do (bàn cờ không giới hạn, kéo thả di chuyển), hai người:
// X (ngẫu nhiên một trong hai người) đi trước, ai có 5 quân liền hàng ngang/dọc/chéo
// trước thì thắng. Luật "chặn hai đầu" (mặc định bật, như chơi ngoài đời): hàng 5 bị quân
// đối phương chặn cả hai đầu thì không tính.

const (
	caroSize     = 15
	caroTurnTime = 3 * time.Minute
	caroMaxCoord = 1000
)

type caroOptions struct {
	// Tắt luật chặn hai đầu.
	FreeStyle bool `json:"free_style"`
}

type Coord struct {
	R int `json:"r"`
	C int `json:"c"`
}

func coordKey(r, c int) string {
	return fmt.Sprintf("%d,%d", r, c)
}

type caro struct {
	Base
	Opts   caroOptions       `json:"opts"`
	Pieces map[string]string `json:"pieces,omitempty"` // "r,c" -> "x" | "o"
	Turn   int               `json:"turn"`             // 0: X (Players[0]), 1: O
	Last   *Coord            `json:"last,omitempty"`
	Win    []Coord           `json:"win,omitempty"`
	Moves  int               `json:"moves"`

	// Legacy field for loading games saved prior to infinite board support
	Board string `json:"board,omitempty"`
}

func (c *caro) limits() (int, int) { return 2, 2 }
func (c *caro) autoStart() bool    { return true }
func (c *caro) options() any       { return &c.Opts }

func (c *caro) normalize() {
	if c.Pieces == nil {
		c.Pieces = make(map[string]string)
		if len(c.Board) == caroSize*caroSize {
			for i, ch := range c.Board {
				if ch == 'x' || ch == 'o' {
					c.Pieces[coordKey(i/caroSize, i%caroSize)] = string(ch)
				}
			}
		}
	}
}

func (c *caro) start(now time.Time) error {
	c.Pieces = make(map[string]string)
	c.Board = ""
	shuffle(c.Players)
	c.Turn, c.Last, c.Moves, c.Win = 0, nil, 0, nil
	c.logf("%s (X) đấu với %s (O). X đi trước.", c.Players[0].Name, c.Players[1].Name)
	c.setDeadline(now, caroTurnTime)
	return nil
}

func mark(turn int) byte { return "xo"[turn] }

type moveData struct {
	Cell *int `json:"cell,omitempty"`
	R    *int `json:"r,omitempty"`
	C    *int `json:"c,omitempty"`
}

func (c *caro) act(p int64, action string, data json.RawMessage, now time.Time) error {
	c.normalize()
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

	var r, col int
	if d.R != nil && d.C != nil {
		r, col = *d.R, *d.C
	} else if d.Cell != nil {
		r, col = *d.Cell / caroSize, *d.Cell % caroSize
	} else {
		return Error("Thiếu toạ độ nước đi.")
	}

	if r < -caroMaxCoord || r > caroMaxCoord || col < -caroMaxCoord || col > caroMaxCoord {
		return Error("Toạ độ vượt quá giới hạn bàn cờ.")
	}
	key := coordKey(r, col)
	if _, ok := c.Pieces[key]; ok {
		return Error("Ô này không đánh được.")
	}

	c.Pieces[key] = string(mark(me))
	c.Last = &Coord{R: r, C: col}
	c.Moves++
	if line := c.winLine(r, col); line != nil {
		c.Win = line
		c.logf("%s thắng sau %d nước!", c.Players[me].Name, c.Moves)
		c.end(c.Players[me].Name+" thắng!", []int64{p})
		return nil
	}
	if c.Moves >= 1000 {
		c.logf("Đã đạt 1000 nước đi, hoà.")
		c.end("Hoà!", nil)
		return nil
	}
	c.Turn = 1 - c.Turn
	c.setDeadline(now, caroTurnTime)
	return nil
}

// winLine: các ô của hàng thắng đi qua (r0, c0) (nil nếu chưa thắng).
func (c *caro) winLine(r0, c0 int) []Coord {
	m := c.Pieces[coordKey(r0, c0)]
	if m == "" {
		return nil
	}
	for _, d := range [][2]int{{0, 1}, {1, 0}, {1, 1}, {1, -1}} {
		line := []Coord{{R: r0, C: c0}}
		r, col := r0-d[0], c0-d[1]
		for c.Pieces[coordKey(r, col)] == m {
			line = append([]Coord{{R: r, C: col}}, line...)
			r, col = r-d[0], col-d[1]
		}
		endA := c.Pieces[coordKey(r, col)]

		r, col = r0+d[0], c0+d[1]
		for c.Pieces[coordKey(r, col)] == m {
			line = append(line, Coord{R: r, C: col})
			r, col = r+d[0], col+d[1]
		}
		endB := c.Pieces[coordKey(r, col)]

		if len(line) < 5 {
			continue
		}
		other := "x"
		if m == "x" {
			other = "o"
		}
		blocked := endA == other && endB == other
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

func (c *caro) board15() string {
	b := make([]byte, caroSize*caroSize)
	for i := range b {
		b[i] = '.'
	}
	for key, m := range c.Pieces {
		var r, col int
		if _, err := fmt.Sscanf(key, "%d,%d", &r, &col); err == nil {
			if r >= 0 && r < caroSize && col >= 0 && col < caroSize && len(m) > 0 {
				b[r*caroSize+col] = m[0]
			}
		}
	}
	return string(b)
}

type caroPublic struct {
	Size      int               `json:"size"`
	Board     string            `json:"board"`
	Pieces    map[string]string `json:"pieces,omitempty"`
	Turn      int64             `json:"turn,omitempty"` // id người tới lượt
	Last      int               `json:"last"`
	LastCoord *Coord            `json:"last_coord,omitempty"`
	Win       []int             `json:"win,omitempty"`
	WinCoords []Coord           `json:"win_coords,omitempty"`
	Moves     int               `json:"moves"`
	FreeStyle bool              `json:"free_style"`
}

func (c *caro) view(viewer int64) (any, any) {
	c.normalize()
	pub := caroPublic{
		Size:      caroSize,
		Board:     c.board15(),
		Pieces:    c.Pieces,
		Last:      -1,
		LastCoord: c.Last,
		Moves:     c.Moves,
		FreeStyle: c.Opts.FreeStyle,
	}
	if c.Status == Playing {
		pub.Turn = c.Players[c.Turn].ID
	}
	if c.Last != nil && c.Last.R >= 0 && c.Last.R < caroSize && c.Last.C >= 0 && c.Last.C < caroSize {
		pub.Last = c.Last.R*caroSize + c.Last.C
	}
	if len(c.Win) > 0 {
		pub.WinCoords = c.Win
		for _, pt := range c.Win {
			if pt.R >= 0 && pt.R < caroSize && pt.C >= 0 && pt.C < caroSize {
				pub.Win = append(pub.Win, pt.R*caroSize+pt.C)
			}
		}
	}
	return pub, nil
}
