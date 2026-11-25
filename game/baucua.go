package game

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

// Bầu cua tôm cá: một người làm cái, cả bàn đặt chip vào 6 con vật, lắc 3 xúc xắc.
// Ô ra k mặt thì người cược nhận lại tiền cược cộng k lần; ô không ra thì cái ăn.
// Cái xoay vòng theo thứ tự vào phòng, mỗi người cầm cái RollsPerDealer lượt; ai cũng
// cầm cái một vòng là hết ván. Cái không đủ trả thì "cháy túi": phần thiếu thành nợ ngân hàng
// của cái và mất cái ngay.
//
// Chip mua bằng xu trong ví (controller lo phần ví). Chips[uid] là toàn bộ chip của người đó,
// kể cả phần đang đặt cược (cược chỉ trừ/cộng lúc mở bát).

const (
	baucuaBetTime    = 20 * time.Second
	baucuaRevealTime = 5 * time.Second
	baucuaHistory    = 10
)

var baucuaSymbols = []string{"bau", "cua", "tom", "ca", "ga", "nai"}

var baucuaNames = map[string]string{"bau": "Bầu", "cua": "Cua", "tom": "Tôm", "ca": "Cá", "ga": "Gà", "nai": "Nai"}

var baucuaChips = []int64{10, 50, 100, 500}

// rollDie: một mặt xúc xắc ngẫu nhiên (test ghi đè để cố định kết quả).
var rollDie = func() string { return baucuaSymbols[rand.IntN(len(baucuaSymbols))] }

type baucuaOptions struct {
	RollsPerDealer int `json:"rolls_per_dealer"` // 1, 3, 5
}

type baucua struct {
	Base
	Opts           baucuaOptions              `json:"opts"`
	Phase          string                     `json:"phase,omitempty"` // betting | reveal
	DealerOrder    []int64                    `json:"dealer_order,omitempty"`
	DealerIdx      int                        `json:"dealer_idx"`
	RollNo         int                        `json:"roll_no"`
	Chips          map[int64]int64            `json:"chips"`
	Loans          map[int64]int64            `json:"loans"`
	BuyIns         map[int64]int64            `json:"buy_ins"`
	BuyCount       map[int64]int64            `json:"buy_count"`
	Bets           map[int64]map[string]int64 `json:"bets"`
	Dice           []string                   `json:"dice,omitempty"`
	LastResult     map[int64]int64            `json:"last_result,omitempty"`
	History        [][]string                 `json:"history"`
	Busts          map[int64]int              `json:"busts"`
	LastBust       int64                      `json:"last_bust,omitempty"`
	LastBustAmount int64                      `json:"last_bust_amount,omitempty"`
}

func (g *baucua) limits() (int, int) { return 2, 10 }
func (g *baucua) autoStart() bool    { return false }
func (g *baucua) options() any       { return &g.Opts }

func (g *baucua) normalize() {
	if g.Opts.RollsPerDealer != 1 && g.Opts.RollsPerDealer != 5 {
		g.Opts.RollsPerDealer = 3
	}
	if g.Chips == nil {
		g.Chips = map[int64]int64{}
	}
	if g.Loans == nil {
		g.Loans = map[int64]int64{}
	}
	if g.BuyIns == nil {
		g.BuyIns = map[int64]int64{}
	}
	if g.BuyCount == nil {
		g.BuyCount = map[int64]int64{}
	}
	if g.Bets == nil {
		g.Bets = map[int64]map[string]int64{}
	}
	if g.Busts == nil {
		g.Busts = map[int64]int{}
	}
}

/* ---------- chip (interface staked) ---------- */

func (g *baucua) buyIn(uid, amount int64) int64 {
	g.normalize()
	g.Chips[uid] += amount
	g.BuyIns[uid] += amount
	g.BuyCount[uid]++
	return g.BuyCount[uid]
}

func (g *baucua) stakeOf(uid int64) Stake {
	return Stake{UserID: uid, Chips: g.Chips[uid], Loans: g.Loans[uid]}
}

func (g *baucua) buyIns(uid int64) int64 { return g.BuyCount[uid] }

func (g *baucua) removePlayer(uid int64) {
	delete(g.Chips, uid)
	delete(g.Loans, uid)
	delete(g.BuyIns, uid)
	delete(g.Bets, uid)
}

/* ---------- vòng chơi ---------- */

func (g *baucua) dealer() int64 {
	if g.DealerIdx < 0 || g.DealerIdx >= len(g.DealerOrder) {
		return 0
	}
	return g.DealerOrder[g.DealerIdx]
}

func (g *baucua) start(now time.Time) error {
	g.normalize()
	g.DealerOrder = g.DealerOrder[:0]
	for _, p := range g.Players {
		g.DealerOrder = append(g.DealerOrder, p.ID)
	}
	g.DealerIdx = -1
	g.History = [][]string{}
	g.nextDealer(now)
	return nil
}

// nextDealer chuyển cái cho người kế tiếp còn chip; hết người thì kết thúc ván.
func (g *baucua) nextDealer(now time.Time) {
	for {
		g.DealerIdx++
		if g.DealerIdx >= len(g.DealerOrder) {
			g.finish("Hết vòng cái.")
			return
		}
		d := g.dealer()
		if g.Chips[d] > 0 {
			g.RollNo = 1
			g.logf("%s cầm cái.", g.name(d))
			g.beginBetting(now)
			return
		}
		g.logf("%s không đủ xu làm cái.", g.name(d))
	}
}

func (g *baucua) beginBetting(now time.Time) {
	g.Phase = "betting"
	g.Bets = map[int64]map[string]int64{}
	g.setDeadline(now, baucuaBetTime)
}

func (g *baucua) hasBets() bool {
	for _, b := range g.Bets {
		if len(b) > 0 {
			return true
		}
	}
	return false
}

func (g *baucua) roll(now time.Time) {
	dice := []string{rollDie(), rollDie(), rollDie()}
	g.Dice = dice
	g.History = append([][]string{dice}, g.History...)
	if len(g.History) > baucuaHistory {
		g.History = g.History[:baucuaHistory]
	}
	count := map[string]int64{}
	names := make([]string, len(dice))
	for i, s := range dice {
		count[s]++
		names[i] = baucuaNames[s]
	}
	g.logf("🎲 Kết quả: %s", strings.Join(names, " · "))

	d := g.dealer()
	res := map[int64]int64{}
	// cái ăn các ô không ra trước, rồi trả các ô ra theo thứ tự vào phòng
	owed := map[int64]int64{}
	for _, uid := range g.DealerOrder {
		for sym, amt := range g.Bets[uid] {
			if k := count[sym]; k > 0 {
				owed[uid] += amt * k
			} else {
				g.Chips[uid] -= amt
				g.Chips[d] += amt
				res[uid] -= amt
				res[d] += amt
			}
		}
	}
	// Cái không đủ chip đền thì "cháy túi": chia số chip còn lại cho người thắng theo tỉ lệ,
	// không vay ngân hàng (vay chỉ ở Ví, theo luật vay chung).
	g.LastBust, g.LastBustAmount = 0, 0
	var total int64
	for _, w := range owed {
		total += w
	}
	avail := g.Chips[d]
	var paid int64
	for _, uid := range g.DealerOrder {
		win := owed[uid]
		if win == 0 {
			continue
		}
		if avail < total {
			win = win * avail / total
		}
		g.Chips[uid] += win
		res[uid] += win
		res[d] -= win
		paid += win
	}
	g.Chips[d] -= paid
	if avail < total {
		g.LastBust = d
		g.LastBustAmount = total - paid
		g.Busts[d]++
		g.logf("💸 %s cháy túi: chỉ đền được %d/%d xu, chia theo tỉ lệ.", g.name(d), paid, total)
	}
	g.LastResult = res
	g.Bets = map[int64]map[string]int64{}
	g.Phase = "reveal"
	g.setDeadline(now, baucuaRevealTime)
}

// afterReveal: lượt tiếp của cái hiện tại, hoặc chuyển cái.
func (g *baucua) afterReveal(now time.Time) {
	if g.LastBust != 0 || g.RollNo >= g.Opts.RollsPerDealer || g.Chips[g.dealer()] <= 0 {
		g.nextDealer(now)
		return
	}
	g.RollNo++
	g.beginBetting(now)
}

func (g *baucua) net(uid int64) int64 { return g.Chips[uid] - g.BuyIns[uid] - g.Loans[uid] }

func (g *baucua) finish(reason string) {
	g.Phase = ""
	g.Bets = map[int64]map[string]int64{}
	var best int64
	var top []int64
	for _, p := range g.Players {
		n := g.net(p.ID)
		switch {
		case n > best:
			best, top = n, []int64{p.ID}
		case n == best && n > 0:
			top = append(top, p.ID)
		}
	}
	result := reason
	if len(top) > 0 {
		result += fmt.Sprintf(" Ăn đậm nhất: %s (+%d xu).", g.name(top[0]), best)
	}
	g.logf("%s", result)
	g.end(result, top)
}

type baucuaBet struct {
	Symbol string `json:"symbol"`
	Amount int64  `json:"amount"`
}

func (g *baucua) act(p int64, action string, data json.RawMessage, now time.Time) error {
	switch action {
	case "bet":
		var d baucuaBet
		if err := decode(data, &d); err != nil {
			return err
		}
		if g.Phase != "betting" {
			return Error("Đang mở bát, chờ lượt sau.")
		}
		if p == g.dealer() {
			return Error("Cái không được đặt cược.")
		}
		if !slices.Contains(baucuaChips, d.Amount) {
			return Error("Mệnh giá không hợp lệ.")
		}
		if _, ok := baucuaNames[d.Symbol]; !ok {
			return Error("Ô cược không hợp lệ.")
		}
		var total int64
		for _, a := range g.Bets[p] {
			total += a
		}
		if total+d.Amount > g.Chips[p] {
			return Error("Không đủ xu để cược.")
		}
		if g.Bets[p] == nil {
			g.Bets[p] = map[string]int64{}
		}
		g.Bets[p][d.Symbol] += d.Amount
	case "clear":
		if g.Phase != "betting" {
			return Error("Đang mở bát, chờ lượt sau.")
		}
		delete(g.Bets, p)
	case "roll":
		if p != g.dealer() {
			return Error("Chỉ cái mới được lắc.")
		}
		if g.Phase != "betting" {
			return Error("Đang mở bát rồi.")
		}
		if !g.hasBets() {
			return Error("Chưa ai đặt cược.")
		}
		g.roll(now)
	case "loan":
		// Trước đây được vay 500 xu ngay trong ván, không giới hạn. Giờ vay chỉ ở Ví (hết nợ mới vay,
		// 3 ngày một lần); hết chip thì ngồi xem hoặc ra Nông trại kiếm thêm.
		return Error("Không vay trong ván nữa. Hết xu thì vào Ví hoặc ra Nông trại kiếm thêm nhé.")
	case "finish":
		if p != g.HostID {
			return Error("Chỉ chủ phòng mới kết thúc được.")
		}
		if g.Phase == "betting" && g.hasBets() {
			return Error("Đang có cược, chờ mở bát rồi kết thúc.")
		}
		g.finish("Chủ phòng kết thúc ván.")
	default:
		return Error("Hành động không hợp lệ.")
	}
	return nil
}

func (g *baucua) tick(now time.Time) bool {
	switch g.Phase {
	case "betting":
		g.roll(now)
	case "reveal":
		g.afterReveal(now)
	default:
		return false
	}
	return true
}

type baucuaView struct {
	Phase          string                     `json:"phase"`
	DealerID       int64                      `json:"dealer_id"`
	RollNo         int                        `json:"roll_no"`
	RollsPerDealer int                        `json:"rolls_per_dealer"`
	DealerOrder    []int64                    `json:"dealer_order"`
	Chips          map[int64]int64            `json:"chips"`
	Loans          map[int64]int64            `json:"loans"`
	Bets           map[int64]map[string]int64 `json:"bets"`
	Dice           []string                   `json:"dice"`
	LastResult     map[int64]int64            `json:"last_result"`
	History        [][]string                 `json:"history"`
	Net            map[int64]int64            `json:"net"`
	Busts          map[int64]int              `json:"busts"`
	LastBust       int64                      `json:"last_bust,omitempty"`
	LastBustAmount int64                      `json:"last_bust_amount,omitempty"`
}

func (g *baucua) view(int64) (any, any) {
	g.normalize()
	net := map[int64]int64{}
	for _, p := range g.Players {
		net[p.ID] = g.net(p.ID)
	}
	v := baucuaView{
		Phase: g.Phase, DealerID: g.dealer(), RollNo: g.RollNo, RollsPerDealer: g.Opts.RollsPerDealer,
		DealerOrder: g.DealerOrder, Chips: g.Chips, Loans: g.Loans, Bets: g.Bets, Dice: g.Dice,
		LastResult: g.LastResult, History: g.History, Net: net, Busts: g.Busts,
		LastBust: g.LastBust, LastBustAmount: g.LastBustAmount,
	}
	if v.DealerOrder == nil {
		v.DealerOrder = []int64{}
	}
	if v.History == nil {
		v.History = [][]string{}
	}
	if v.Dice == nil {
		v.Dice = []string{}
	}
	if v.LastResult == nil {
		v.LastResult = map[int64]int64{}
	}
	return v, nil
}
