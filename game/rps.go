package game

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Oẳn tù tì: hai người cùng ra Búa / Bao / Kéo (giấu tới khi cả hai đã ra), chạm số ván thắng
// trước thì thắng chung cuộc. Hết giờ một ván: ai đã ra thì thắng ván đó; cả hai không ra thì huỷ.
// Hỗ trợ cược xu ảo theo các mức cố định (0 = chơi vui miễn phí).

const rpsRoundTime = 30 * time.Second

// RpsBetLevels là danh sách các mức cược xu cố định được phép cho Oẳn tù tì.
var RpsBetLevels = []int64{0, 50, 100, 200, 500, 1000}

var rpsNames = map[string]string{"rock": "✊ Búa", "paper": "✋ Bao", "scissors": "✌️ Kéo"}

// rpsBeats[a] = b: a thắng b.
var rpsBeats = map[string]string{"rock": "scissors", "paper": "rock", "scissors": "paper"}

type rpsOptions struct {
	BestOf int   `json:"best_of"` // 1, 3, 5
	Bet    int64 `json:"bet"`     // 0, 50, 100, 200, 500, 1000
}

type rpsRound struct {
	Picks  [2]string `json:"picks"`
	Winner int64     `json:"winner"` // 0: hoà
}

type rps struct {
	Base
	Opts     rpsOptions      `json:"opts"`
	Picks    [2]string       `json:"picks"`
	Rounds   []rpsRound      `json:"rounds,omitempty"`
	Score    [2]int          `json:"score"`
	Escrow   map[int64]int64 `json:"escrow,omitempty"`
	BuyCount map[int64]int64 `json:"buy_count,omitempty"`
}

func (g *rps) limits() (int, int) { return 2, 2 }
func (g *rps) autoStart() bool    { return true }
func (g *rps) options() any       { return &g.Opts }

func (g *rps) validateOptions() error {
	if !slices.Contains(RpsBetLevels, g.Opts.Bet) {
		return Error("Mức cược không hợp lệ.")
	}
	return nil
}

func (g *rps) normalize() {
	if g.Opts.BestOf != 1 && g.Opts.BestOf != 5 {
		g.Opts.BestOf = 3
	}
	if !slices.Contains(RpsBetLevels, g.Opts.Bet) {
		g.Opts.Bet = 0
	}
}

func (g *rps) blockCancelPlaying() error {
	return Error("Ván đã bắt đầu, hãy dùng Chịu thua nếu muốn bỏ cuộc.")
}

/* ---------- cược xu cố định (interface staked) ---------- */

func (g *rps) isStaked() bool                  { return g.Opts.Bet > 0 }
func (g *rps) stakeRequirement() (bool, int64) { return g.Opts.Bet > 0, g.Opts.Bet }

func (g *rps) buyIn(uid, amount int64) int64 {
	if g.Escrow == nil {
		g.Escrow = map[int64]int64{}
	}
	if g.BuyCount == nil {
		g.BuyCount = map[int64]int64{}
	}
	g.Escrow[uid] += amount
	g.BuyCount[uid]++
	return g.BuyCount[uid]
}

func (g *rps) totalEscrow() int64 {
	var total int64
	for _, v := range g.Escrow {
		total += v
	}
	return total
}

func (g *rps) stakeOf(uid int64) Stake {
	if g.Opts.Bet <= 0 {
		return Stake{UserID: uid}
	}
	if g.Status == Ended {
		if len(g.Winners) == 1 {
			if g.Winners[0] == uid {
				return Stake{UserID: uid, Chips: g.totalEscrow()}
			}
			return Stake{UserID: uid, Chips: 0}
		}
		// Hoà, huỷ phòng, hết giờ chờ hoặc bị xoá: hoàn lại đúng phần mỗi người đã cược.
		return Stake{UserID: uid, Chips: g.Escrow[uid]}
	}
	return Stake{UserID: uid, Chips: g.Escrow[uid]}
}

func (g *rps) buyIns(uid int64) int64 { return g.BuyCount[uid] }

func (g *rps) removePlayer(uid int64) {
	delete(g.Escrow, uid)
}

func (g *rps) need() int { return g.Opts.BestOf/2 + 1 }

func (g *rps) start(now time.Time) error {
	if g.Opts.Bet > 0 {
		g.logf("%s đấu với %s, chạm %d ván thắng (cược %d xu).", g.Players[0].Name, g.Players[1].Name, g.need(), g.Opts.Bet)
	} else {
		g.logf("%s đấu với %s, chạm %d ván thắng.", g.Players[0].Name, g.Players[1].Name, g.need())
	}
	g.setDeadline(now, rpsRoundTime)
	return nil
}

type pickData struct {
	Choice string `json:"choice"`
}

func (g *rps) act(p int64, action string, data json.RawMessage, now time.Time) error {
	me := g.index(p)
	switch action {
	case "resign":
		other := g.Players[1-me]
		g.logf("%s chịu thua.", g.Players[me].Name)
		res := other.Name + " thắng (đối thủ chịu thua)."
		if g.Opts.Bet > 0 {
			res = fmt.Sprintf("%s thắng (đối thủ chịu thua, +%d xu).", other.Name, g.Opts.Bet)
		}
		g.end(res, []int64{other.ID})
		return nil
	case "pick":
	default:
		return Error("Hành động không hợp lệ.")
	}
	var d pickData
	if err := decode(data, &d); err != nil {
		return err
	}
	if _, ok := rpsBeats[d.Choice]; !ok {
		return Error("Chỉ được ra Búa, Bao hoặc Kéo.")
	}
	if g.Picks[me] != "" {
		return Error("Bạn đã ra rồi, chờ đối thủ.")
	}
	g.Picks[me] = d.Choice
	if g.Picks[0] != "" && g.Picks[1] != "" {
		g.finishRound(now)
	}
	return nil
}

func (g *rps) finishRound(now time.Time) {
	a, b := g.Picks[0], g.Picks[1]
	r := rpsRound{Picks: g.Picks}
	n := len(g.Rounds) + 1
	switch {
	case a == "" && b == "":
	case b == "" || (a != "" && rpsBeats[a] == b):
		r.Winner = g.Players[0].ID
		g.Score[0]++
	case a == "" || rpsBeats[b] == a:
		r.Winner = g.Players[1].ID
		g.Score[1]++
	}
	g.Rounds = append(g.Rounds, r)
	g.Picks = [2]string{}
	show := func(c string) string {
		if c == "" {
			return "không ra"
		}
		return rpsNames[c]
	}
	res := "hoà"
	if r.Winner != 0 {
		res = g.name(r.Winner) + " thắng"
	}
	g.logf("Ván %d: %s %s — %s %s → %s (%d–%d).", n, g.Players[0].Name, show(a), show(b), g.Players[1].Name, res, g.Score[0], g.Score[1])
	for i := range 2 {
		if g.Score[i] >= g.need() {
			msg := fmt.Sprintf("%s thắng chung cuộc %d–%d!", g.Players[i].Name, g.Score[i], g.Score[1-i])
			if g.Opts.Bet > 0 {
				msg = fmt.Sprintf("%s thắng chung cuộc %d–%d (+%d xu)!", g.Players[i].Name, g.Score[i], g.Score[1-i], g.Opts.Bet)
			}
			g.end(msg, []int64{g.Players[i].ID})
			return
		}
	}
	if len(g.Rounds) >= 30 {
		g.end("Hoà (quá nhiều ván).", nil)
		return
	}
	g.setDeadline(now, rpsRoundTime)
}

func (g *rps) tick(now time.Time) bool {
	if g.Picks[0] == "" && g.Picks[1] == "" {
		g.logf("Cả hai không ra tay, ván bị huỷ.")
		g.end("Đã huỷ (không ai ra tay).", nil)
		return true
	}
	g.finishRound(now)
	return true
}

type rpsPublic struct {
	BestOf int        `json:"best_of"`
	Bet    int64      `json:"bet"`
	Pot    int64      `json:"pot"`
	Score  [2]int     `json:"score"`
	Rounds []rpsRound `json:"rounds"`
	Ready  [2]bool    `json:"ready"` // đã ra trong ván hiện tại
}

type rpsMe struct {
	Pick string `json:"pick,omitempty"`
}

func (g *rps) view(viewer int64) (any, any) {
	pub := rpsPublic{
		BestOf: g.Opts.BestOf,
		Bet:    g.Opts.Bet,
		Pot:    g.totalEscrow(),
		Score:  g.Score,
		Rounds: g.Rounds,
		Ready:  [2]bool{g.Picks[0] != "", g.Picks[1] != ""},
	}
	if pub.Rounds == nil {
		pub.Rounds = []rpsRound{}
	}
	if i := g.index(viewer); i >= 0 {
		return pub, rpsMe{Pick: g.Picks[i]}
	}
	return pub, nil
}
