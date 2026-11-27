package game

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Poker Texas Hold'em không giới hạn (No-Limit), luật quốc tế (theo TDA). Đặc tả: specs/POKER_SPEC.md.
//
// Mỗi người mua vào đúng Opts.BuyIn xu (controller trừ ví), 1 xu = 1 chip. Ván gồm nhiều tay bài;
// người hết chip bị loại, còn một người có chip thì người đó thắng. Hết ván chip còn lại về ví.
//
// Một tay: nút dealer xoay theo chiều kim đồng hồ (thứ tự vào phòng), mù nhỏ / mù lớn đặt trước,
// chia 2 lá tẩy, rồi 4 vòng cược: preflop, flop (3 lá chung), turn (1), river (1); còn từ 2 người trở
// lên thì lật bài (showdown), 5 lá tốt nhất trong 7 lá thắng.

const (
	pokerShowTime = 7 * time.Second // xem kết quả một tay trước khi chia tay mới
	pokerFoldTime = 3 * time.Second // tay kết thúc vì mọi người bỏ bài
	pokerAwayAt   = 2               // hết giờ chừng này lượt liền nhau thì bị coi là vắng mặt
)

// PokerBuyIns: các mức mua vào được chọn (xu).
var PokerBuyIns = []int64{100, 200, 500, 1000, 2000, 5000}

// pokerBlindSteps: hệ số mù theo cấp (mù nhỏ = BuyIn/100 × hệ số, mù lớn gấp đôi).
var pokerBlindSteps = []int64{1, 2, 3, 4, 6, 8, 12, 16, 24, 32, 48, 64}

// pokerDeck: bộ bài đã xáo cho một tay; pokerFirstButton: ghế cầm nút tay đầu (bốc ngẫu nhiên).
// Test ghi đè để chia bài cố định.
var (
	pokerDeck = func() []string {
		d := newPokerDeck()
		shuffle(d)
		return d
	}
	pokerFirstButton = func(seats []int) int { return pick(seats) }
)

type pokerOptions struct {
	BuyIn       int64 `json:"buy_in"`
	TurnSeconds int   `json:"turn_seconds"` // 20 | 30 | 60
	BlindUp     int   `json:"blind_up"`     // tăng mù sau mỗi chừng này tay; 0 = không tăng
}

type pokerPot struct {
	Amount  int64   `json:"amount"`
	Winners []int64 `json:"winners"`
	Hand    string  `json:"hand,omitempty"` // tên bộ thắng (chỉ khi lật bài)
}

// pokerResult: kết quả tay vừa xong (hiện trong lúc chờ tay mới).
type pokerResult struct {
	Pots     []pokerPot         `json:"pots"`
	Shown    map[int64][]string `json:"shown,omitempty"` // lá tẩy của người lật bài
	Hands    map[int64]string   `json:"hands,omitempty"` // tên bộ của người lật bài
	Best     map[int64][]string `json:"best,omitempty"`  // 5 lá tạo nên bộ
	Won      map[int64]int64    `json:"won"`             // chip mỗi người nhận về
	Showdown bool               `json:"showdown"`
	Busted   []int64            `json:"busted,omitempty"`
}

type poker struct {
	Base
	Opts     pokerOptions    `json:"opts"`
	Seats    []int64         `json:"seats"` // thứ tự ngồi theo chiều kim đồng hồ
	Stack    map[int64]int64 `json:"stack"` // chip trước mặt (chưa tính phần đã cược trong tay)
	BuyCount map[int64]int64 `json:"buy_count"`

	HandNo   int                `json:"hand_no"`
	Button   int                `json:"button"` // chỉ số ghế của nút dealer
	SBID     int64              `json:"sb_id"`
	BBID     int64              `json:"bb_id"`
	SB       int64              `json:"sb"`
	BB       int64              `json:"bb"`
	Phase    string             `json:"phase,omitempty"` // preflop | flop | turn | river | showdown
	Deck     []string           `json:"deck,omitempty"`
	Board    []string           `json:"board"`
	Hole     map[int64][]string `json:"hole"`
	InHand   map[int64]bool     `json:"in_hand"` // được chia bài tay này
	Folded   map[int64]bool     `json:"folded"`
	AllIn    map[int64]bool     `json:"all_in"`
	Bet      map[int64]int64    `json:"bet"`   // đã cược ở vòng hiện tại
	Total    map[int64]int64    `json:"total"` // đã cược cả tay
	CurBet   int64              `json:"cur_bet"`
	MinRaise int64              `json:"min_raise"` // mức tố thêm tối thiểu (= lần tố đủ gần nhất, ít nhất 1 mù lớn)
	Acted    map[int64]bool     `json:"acted"`     // đã hành động kể từ lần tố đủ gần nhất
	Turn     int64              `json:"turn"`
	Last     map[int64]string   `json:"last"` // hành động gần nhất trong tay
	Result   *pokerResult       `json:"result,omitempty"`
	Timeouts map[int64]int      `json:"timeouts"`
	Away     map[int64]bool     `json:"away"`
}

func (g *poker) limits() (int, int) { return 2, 9 }
func (g *poker) autoStart() bool    { return false }
func (g *poker) options() any       { return &g.Opts }

func (g *poker) validateOptions() error {
	if g.Opts.BuyIn != 0 && !slices.Contains(PokerBuyIns, g.Opts.BuyIn) {
		return Error("Mức mua vào không hợp lệ.")
	}
	return nil
}

func (g *poker) normalize() {
	if g.Opts.BuyIn == 0 {
		g.Opts.BuyIn = 200
	}
	if g.Opts.TurnSeconds != 20 && g.Opts.TurnSeconds != 60 {
		g.Opts.TurnSeconds = 30
	}
	if g.Opts.BlindUp != 0 && g.Opts.BlindUp != 5 && g.Opts.BlindUp != 15 {
		g.Opts.BlindUp = 10
	}
	for _, m := range []*map[int64]int64{&g.Stack, &g.BuyCount, &g.Bet, &g.Total} {
		if *m == nil {
			*m = map[int64]int64{}
		}
	}
	for _, m := range []*map[int64]bool{&g.InHand, &g.Folded, &g.AllIn, &g.Acted, &g.Away} {
		if *m == nil {
			*m = map[int64]bool{}
		}
	}
	if g.Hole == nil {
		g.Hole = map[int64][]string{}
	}
	if g.Last == nil {
		g.Last = map[int64]string{}
	}
	if g.Timeouts == nil {
		g.Timeouts = map[int64]int{}
	}
	if g.Board == nil {
		g.Board = []string{}
	}
}

/* ---------- chip (interface staked) ---------- */

func (g *poker) isStaked() bool                  { return true }
func (g *poker) stakeRequirement() (bool, int64) { return true, g.Opts.BuyIn }

func (g *poker) buyIn(uid, amount int64) int64 {
	g.normalize()
	g.Stack[uid] += amount
	g.BuyCount[uid]++
	return g.BuyCount[uid]
}

// stakeOf: chip của uid; tay đang dở (ván bị huỷ giữa chừng) thì hoàn luôn phần đã cược trong tay.
func (g *poker) stakeOf(uid int64) Stake {
	chips := g.Stack[uid]
	if g.betting() {
		chips += g.Total[uid]
	}
	return Stake{UserID: uid, Chips: chips}
}

func (g *poker) buyIns(uid int64) int64 { return g.BuyCount[uid] }

func (g *poker) blockCancelPlaying() error {
	return Error("Ván đang chơi, không huỷ được. Bấm \"Kết thúc ván\" để dừng và đổi chip về ví.")
}

func (g *poker) removePlayer(uid int64) {
	delete(g.Stack, uid)
	delete(g.BuyCount, uid)
}

/* ---------- ghế và lượt ---------- */

func (g *poker) betting() bool {
	return g.Phase == "preflop" || g.Phase == "flop" || g.Phase == "turn" || g.Phase == "river"
}

func (g *poker) seatOf(id int64) int { return slices.Index(g.Seats, id) }

// nextSeat: ghế đầu tiên sau ghế from (vòng tròn) thoả ok; -1 nếu không có.
func (g *poker) nextSeat(from int, ok func(id int64) bool) int {
	n := len(g.Seats)
	for k := 1; k <= n; k++ {
		i := ((from+k)%n + n) % n
		if ok(g.Seats[i]) {
			return i
		}
	}
	return -1
}

func (g *poker) hasChips(id int64) bool { return g.Stack[id] > 0 }

func (g *poker) live(id int64) bool { return g.InHand[id] && !g.Folded[id] }

func (g *poker) canAct(id int64) bool { return g.live(id) && !g.AllIn[id] }

func (g *poker) needsAction(id int64) bool {
	return g.canAct(id) && (!g.Acted[id] || g.Bet[id] < g.CurBet)
}

func (g *poker) count(ok func(id int64) bool) int {
	n := 0
	for _, id := range g.Seats {
		if ok(id) {
			n++
		}
	}
	return n
}

// put: id đặt thêm amount chip (tối đa số đang có; hết chip là tất tay).
func (g *poker) put(id, amount int64) int64 {
	amount = min(amount, g.Stack[id])
	g.Stack[id] -= amount
	g.Bet[id] += amount
	g.Total[id] += amount
	if g.Stack[id] == 0 {
		g.AllIn[id] = true
	}
	return amount
}

func (g *poker) blinds() (sb, bb int64) {
	lv := 0
	if g.Opts.BlindUp > 0 {
		lv = min((g.HandNo-1)/g.Opts.BlindUp, len(pokerBlindSteps)-1)
	}
	sb = max(1, g.Opts.BuyIn/100) * pokerBlindSteps[lv]
	return sb, 2 * sb
}

/* ---------- vòng chơi ---------- */

func (g *poker) start(now time.Time) error {
	g.normalize()
	g.Seats = g.Seats[:0]
	for _, p := range g.Players {
		g.Seats = append(g.Seats, p.ID)
	}
	g.Button = -1
	g.startHand(now)
	return nil
}

func (g *poker) startHand(now time.Time) {
	if g.count(g.hasChips) <= 1 {
		g.finish("")
		return
	}
	g.HandNo++
	prevSB, _ := g.SB, g.BB
	g.SB, g.BB = g.blinds()
	if g.HandNo > 1 && g.SB != prevSB {
		g.logf("⬆️ Mù tăng lên %d/%d.", g.SB, g.BB)
	}
	if g.Button < 0 {
		alive := []int{}
		for i, id := range g.Seats {
			if g.hasChips(id) {
				alive = append(alive, i)
			}
		}
		g.Button = pokerFirstButton(alive)
	} else {
		g.Button = g.nextSeat(g.Button, g.hasChips)
	}
	sbSeat := g.nextSeat(g.Button, g.hasChips)
	if g.count(g.hasChips) == 2 { // đấu tay đôi: dealer là mù nhỏ
		sbSeat = g.Button
	}
	bbSeat := g.nextSeat(sbSeat, g.hasChips)
	g.SBID, g.BBID = g.Seats[sbSeat], g.Seats[bbSeat]

	g.Board = []string{}
	g.Hole = map[int64][]string{}
	g.InHand, g.Folded, g.AllIn, g.Acted = map[int64]bool{}, map[int64]bool{}, map[int64]bool{}, map[int64]bool{}
	g.Bet, g.Total = map[int64]int64{}, map[int64]int64{}
	g.Last = map[int64]string{}
	g.Result = nil
	for _, id := range g.Seats {
		if g.hasChips(id) {
			g.InHand[id] = true
		}
	}
	g.Deck = pokerDeck()
	// chia từng lá một, bắt đầu từ người bên trái nút
	for range 2 {
		for k := 1; k <= len(g.Seats); k++ {
			id := g.Seats[(g.Button+k)%len(g.Seats)]
			if g.InHand[id] {
				g.Hole[id] = append(g.Hole[id], g.draw())
			}
		}
	}
	g.Phase = "preflop"
	g.logf("🃏 Tay %d · %s cầm nút · mù %d/%d.", g.HandNo, g.name(g.Seats[g.Button]), g.SB, g.BB)
	g.Last[g.SBID] = fmt.Sprintf("Mù nhỏ %d", g.put(g.SBID, g.SB))
	g.Last[g.BBID] = fmt.Sprintf("Mù lớn %d", g.put(g.BBID, g.BB))
	// mù lớn thiếu (tất tay) thì người sau vẫn phải theo đủ mức mù lớn
	g.CurBet, g.MinRaise = g.BB, g.BB
	g.Turn = g.BBID
	g.advance(now)
}

func (g *poker) draw() string {
	c := g.Deck[0]
	g.Deck = g.Deck[1:]
	return c
}

// deal: đốt một lá rồi lật n lá chung.
func (g *poker) deal(n int) {
	g.draw()
	for range n {
		g.Board = append(g.Board, g.draw())
	}
}

// advance: tìm người hành động kế tiếp; hết vòng thì sang vòng sau; chỉ còn một người thì kết thúc tay.
func (g *poker) advance(now time.Time) {
	for {
		if g.count(g.live) == 1 {
			g.settle(now, false)
			return
		}
		if i := g.nextSeat(g.seatOf(g.Turn), g.needsAction); i >= 0 {
			g.Turn = g.Seats[i]
			if g.Away[g.Turn] {
				g.autoAct(g.Turn)
				continue
			}
			g.setDeadline(now, time.Duration(g.Opts.TurnSeconds)*time.Second)
			return
		}
		// hết vòng cược
		if g.Phase == "river" {
			g.settle(now, true)
			return
		}
		if g.count(g.canAct) < 2 { // còn tối đa một người cược được: lật hết bài chung
			for len(g.Board) < 5 {
				if len(g.Board) == 0 {
					g.deal(3)
				} else {
					g.deal(1)
				}
			}
			g.logf("Bài chung: %s", strings.Join(g.Board, " "))
			g.settle(now, true)
			return
		}
		switch g.Phase {
		case "preflop":
			g.Phase = "flop"
			g.deal(3)
		case "flop":
			g.Phase = "turn"
			g.deal(1)
		case "turn":
			g.Phase = "river"
			g.deal(1)
		}
		g.logf("%s: %s", strings.ToUpper(g.Phase[:1])+g.Phase[1:], strings.Join(g.Board, " "))
		g.Bet, g.Acted = map[int64]int64{}, map[int64]bool{}
		g.CurBet, g.MinRaise = 0, g.BB
		g.Turn = g.Seats[g.Button] // người đầu tiên bên trái nút hành động trước
	}
}

// autoAct: người vắng mặt / hết giờ: check nếu được, không thì bỏ bài.
func (g *poker) autoAct(id int64) {
	if g.Bet[id] >= g.CurBet {
		g.Acted[id] = true
		g.Last[id] = "Check"
		g.logf("%s check (vắng mặt).", g.name(id))
		return
	}
	g.Folded[id] = true
	g.Last[id] = "Bỏ bài"
	g.logf("%s bỏ bài (vắng mặt).", g.name(id))
}

// pots: chia tổng cược thành pot chính và các pot phụ. Mỗi pot: số chip và ai còn quyền ăn.
func (g *poker) pots() []struct {
	amount   int64
	eligible []int64
} {
	levels := []int64{}
	for _, t := range g.Total {
		if t > 0 && !slices.Contains(levels, t) {
			levels = append(levels, t)
		}
	}
	slices.Sort(levels)
	var out []struct {
		amount   int64
		eligible []int64
	}
	var prev int64
	for _, lv := range levels {
		var amount int64
		var el []int64
		for _, id := range g.Seats {
			amount += min(g.Total[id], lv) - min(g.Total[id], prev)
			if g.live(id) && g.Total[id] >= lv {
				el = append(el, id)
			}
		}
		prev = lv
		if amount == 0 {
			continue
		}
		if len(el) == 0 { // không ai còn quyền (không xảy ra khi còn người chưa bỏ bài): dồn vào pot trước
			if len(out) > 0 {
				out[len(out)-1].amount += amount
			}
			continue
		}
		if n := len(out); n > 0 && slices.Equal(out[n-1].eligible, el) {
			out[n-1].amount += amount
			continue
		}
		out = append(out, struct {
			amount   int64
			eligible []int64
		}{amount, el})
	}
	return out
}

// settle: trao các pot. showdown: lật bài so bộ; không thì người còn lại ăn hết (không phải lật).
func (g *poker) settle(now time.Time, showdown bool) {
	res := &pokerResult{Won: map[int64]int64{}, Showdown: showdown}
	hands := map[int64]pokerHand{}
	if showdown {
		res.Shown, res.Hands, res.Best = map[int64][]string{}, map[int64]string{}, map[int64][]string{}
		for _, id := range g.Seats {
			if g.live(id) {
				h := bestHand(append(slices.Clone(g.Hole[id]), g.Board...))
				hands[id] = h
				res.Shown[id], res.Hands[id], res.Best[id] = g.Hole[id], h.name(), h.Cards
			}
		}
	}
	for _, p := range g.pots() {
		winners := p.eligible
		hand := ""
		if showdown && len(p.eligible) > 1 {
			var top int64 = -1
			winners = nil
			for _, id := range p.eligible {
				switch s := hands[id].Score; {
				case s > top:
					top, winners = s, []int64{id}
				case s == top:
					winners = append(winners, id)
				}
			}
			hand = hands[winners[0]].name()
		}
		// chia đều; chip lẻ cho người thắng ngồi gần bên trái nút nhất
		share, odd := p.amount/int64(len(winners)), p.amount%int64(len(winners))
		order := slices.Clone(winners)
		slices.SortFunc(order, func(a, b int64) int { return g.leftOfButton(a) - g.leftOfButton(b) })
		for i, id := range order {
			won := share
			if int64(i) < odd {
				won++
			}
			g.Stack[id] += won
			res.Won[id] += won
		}
		res.Pots = append(res.Pots, pokerPot{Amount: p.amount, Winners: winners, Hand: hand})
		names := make([]string, len(winners))
		for i, id := range winners {
			names[i] = g.name(id)
		}
		if hand != "" {
			g.logf("🏆 %s ăn %d chip với %s.", strings.Join(names, ", "), p.amount, strings.ToLower(hand))
		} else {
			g.logf("🏆 %s ăn %d chip.", strings.Join(names, ", "), p.amount)
		}
	}
	for _, id := range g.Seats {
		if g.InHand[id] && g.Stack[id] == 0 {
			res.Busted = append(res.Busted, id)
			g.logf("💀 %s hết chip, rời bàn.", g.name(id))
		}
	}
	g.Total, g.Bet = map[int64]int64{}, map[int64]int64{}
	g.CurBet = 0
	g.Turn = 0
	g.Phase = "showdown"
	g.Result = res
	if showdown {
		g.setDeadline(now, pokerShowTime)
	} else {
		g.setDeadline(now, pokerFoldTime)
	}
}

// leftOfButton: khoảng cách ghế của id tính từ bên trái nút (1 = ngay bên trái, nút là xa nhất).
func (g *poker) leftOfButton(id int64) int {
	n := len(g.Seats)
	if d := ((g.seatOf(id)-g.Button)%n + n) % n; d > 0 {
		return d
	}
	return n
}

func (g *poker) net(id int64) int64 { return g.Stack[id] - g.Opts.BuyIn*g.BuyCount[id] }

// finish: kết thúc ván. reason rỗng: còn đúng một người có chip.
func (g *poker) finish(reason string) {
	g.Phase = ""
	g.Turn = 0
	var top []int64
	var best int64
	for _, id := range g.Seats {
		switch n := g.net(id); {
		case n > best:
			best, top = n, []int64{id}
		case n == best && n > 0:
			top = append(top, id)
		}
	}
	result := reason
	if reason == "" {
		for _, id := range g.Seats {
			if g.hasChips(id) {
				result = fmt.Sprintf("%s thắng cả bàn, ôm trọn %d chip.", g.name(id), g.Stack[id])
				top = []int64{id}
			}
		}
	} else if len(top) > 0 {
		result += fmt.Sprintf(" Lãi nhiều nhất: %s (+%d xu).", g.name(top[0]), best)
	}
	g.logf("%s", result)
	g.end(result, top)
}

/* ---------- hành động ---------- */

type pokerRaise struct {
	To int64 `json:"to"` // tổng mức cược của mình ở vòng này sau khi tố
}

func (g *poker) act(p int64, action string, data json.RawMessage, now time.Time) error {
	g.normalize()
	switch action {
	case "back":
		if !g.Away[p] {
			return Error("Bạn đang ở bàn rồi.")
		}
		delete(g.Away, p)
		g.Timeouts[p] = 0
		g.logf("%s đã quay lại bàn.", g.name(p))
		return nil
	case "finish":
		if p != g.HostID {
			return Error("Chỉ chủ phòng mới kết thúc được.")
		}
		if g.betting() { // tay đang dở: huỷ tay, ai cược bao nhiêu nhận lại bấy nhiêu
			for id, t := range g.Total {
				g.Stack[id] += t
			}
			g.Total, g.Bet = map[int64]int64{}, map[int64]int64{}
			g.logf("Tay %d bị huỷ, hoàn lại tiền cược.", g.HandNo)
		}
		g.finish("Chủ phòng kết thúc ván.")
		return nil
	case "fold", "check", "call", "raise", "allin":
	default:
		return Error("Hành động không hợp lệ.")
	}
	if !g.betting() {
		return Error("Chờ chia tay bài mới.")
	}
	if p != g.Turn {
		return Error("Chưa tới lượt bạn.")
	}
	toCall := g.CurBet - g.Bet[p]
	switch action {
	case "fold":
		g.Folded[p] = true
		g.Last[p] = "Bỏ bài"
		g.logf("%s bỏ bài.", g.name(p))
	case "check":
		if toCall > 0 {
			return Error(fmt.Sprintf("Phải theo %d chip hoặc bỏ bài, không check được.", toCall))
		}
		g.Last[p] = "Check"
		g.logf("%s check.", g.name(p))
	case "call":
		if toCall <= 0 {
			return Error("Không có gì để theo, hãy check.")
		}
		n := g.put(p, toCall)
		g.Last[p] = fmt.Sprintf("Theo %d", n)
		if g.AllIn[p] {
			g.Last[p] = fmt.Sprintf("Tất tay %d", g.Bet[p])
		}
		g.logf("%s theo %d.", g.name(p), n)
	case "raise", "allin":
		to := g.Bet[p] + g.Stack[p] // tất tay
		if action == "raise" {
			var d pokerRaise
			if err := decode(data, &d); err != nil {
				return err
			}
			to = d.To
		}
		allIn := to == g.Bet[p]+g.Stack[p]
		if action == "allin" && to <= g.CurBet { // tất tay không đủ theo: chỉ là theo hết chip
			n := g.put(p, g.Stack[p])
			g.Last[p] = fmt.Sprintf("Tất tay %d", g.Bet[p])
			g.logf("%s tất tay %d.", g.name(p), n)
			break
		}
		if err := g.raise(p, to, allIn); err != nil {
			return err
		}
	}
	g.Acted[p] = true
	g.Timeouts[p] = 0
	delete(g.Away, p)
	g.advance(now)
	return nil
}

// raise: p tố lên tổng to ở vòng này (cược khi chưa ai cược). Tố đủ (thêm ít nhất MinRaise) thì mở lại
// quyền tố cho mọi người; tất tay thiếu mức tố tối thiểu thì không mở lại (người đã hành động chỉ được
// theo hoặc bỏ).
func (g *poker) raise(p, to int64, allIn bool) error {
	if g.Acted[p] {
		return Error("Lượt tố chưa đủ mức nên không mở lại quyền tố, bạn chỉ được theo hoặc bỏ bài.")
	}
	others := 0
	for _, id := range g.Seats {
		if id != p && g.canAct(id) {
			others++
		}
	}
	if others == 0 {
		return Error("Các đối thủ đã tất tay, bạn chỉ cần theo.")
	}
	if to > g.Bet[p]+g.Stack[p] {
		return Error("Không đủ chip để tố mức này.")
	}
	minTo := g.CurBet + g.MinRaise
	if to < minTo && !(allIn && to > g.CurBet) {
		if g.CurBet == 0 {
			return Error(fmt.Sprintf("Cược tối thiểu %d chip.", minTo))
		}
		return Error(fmt.Sprintf("Tố tối thiểu lên %d chip.", minTo))
	}
	label, verb := "Tố", "tố lên"
	if g.CurBet == 0 {
		label, verb = "Cược", "cược"
	}
	if inc := to - g.CurBet; inc >= g.MinRaise {
		g.MinRaise = inc
		g.Acted = map[int64]bool{} // tố đủ: mọi người phải hành động lại
	}
	g.CurBet = to
	g.put(p, to-g.Bet[p])
	if g.AllIn[p] {
		g.Last[p] = fmt.Sprintf("Tất tay %d", to)
		g.logf("%s tất tay %d.", g.name(p), to)
	} else {
		g.Last[p] = fmt.Sprintf("%s %d", label, to)
		g.logf("%s %s %d.", g.name(p), verb, to)
	}
	return nil
}

func (g *poker) tick(now time.Time) bool {
	g.normalize()
	switch {
	case g.betting():
		id := g.Turn
		g.Timeouts[id]++
		if g.Timeouts[id] >= pokerAwayAt && !g.Away[id] {
			g.Away[id] = true
			g.logf("%s hết giờ %d lượt liền, tạm coi là vắng mặt.", g.name(id), pokerAwayAt)
		}
		if g.Bet[id] >= g.CurBet {
			g.Last[id] = "Check"
			g.logf("%s hết giờ, tự check.", g.name(id))
		} else {
			g.Folded[id] = true
			g.Last[id] = "Bỏ bài"
			g.logf("%s hết giờ, tự bỏ bài.", g.name(id))
		}
		g.Acted[id] = true
		g.advance(now)
	case g.Phase == "showdown":
		g.startHand(now)
	default:
		return false
	}
	return true
}

/* ---------- hiển thị ---------- */

type pokerSeat struct {
	ID     int64  `json:"id"`
	Stack  int64  `json:"stack"`
	Bet    int64  `json:"bet"`
	InHand bool   `json:"in_hand"`
	Folded bool   `json:"folded"`
	AllIn  bool   `json:"all_in"`
	Away   bool   `json:"away"`
	Out    bool   `json:"out"` // hết chip, bị loại
	Last   string `json:"last,omitempty"`
	Cards  int    `json:"cards"` // số lá tẩy đang cầm (úp)
	Net    int64  `json:"net"`
}

type pokerView struct {
	Opts       pokerOptions `json:"opts"`
	Phase      string       `json:"phase"`
	HandNo     int          `json:"hand_no"`
	SB         int64        `json:"sb"`
	BB         int64        `json:"bb"`
	NextBlinds int          `json:"next_blinds"` // còn bao nhiêu tay nữa thì tăng mù (0: không tăng)
	ButtonID   int64        `json:"button_id"`
	SBID       int64        `json:"sb_id"`
	BBID       int64        `json:"bb_id"`
	Seats      []pokerSeat  `json:"seats"`
	Board      []string     `json:"board"`
	Pot        int64        `json:"pot"`
	CurBet     int64        `json:"cur_bet"`
	TurnID     int64        `json:"turn_id"`
	Result     *pokerResult `json:"result,omitempty"`
}

// pokerMe: phần riêng của người chơi: lá tẩy và các nút được bấm.
type pokerMe struct {
	Hole     []string `json:"hole"`
	HandName string   `json:"hand_name,omitempty"`
	ToCall   int64    `json:"to_call"`
	CanCheck bool     `json:"can_check"`
	CanCall  bool     `json:"can_call"`
	CanRaise bool     `json:"can_raise"`
	MinTo    int64    `json:"min_to"`
	MaxTo    int64    `json:"max_to"`
	Away     bool     `json:"away"`
}

func (g *poker) view(viewer int64) (any, any) {
	g.normalize()
	v := pokerView{
		Opts: g.Opts, Phase: g.Phase, HandNo: g.HandNo, SB: g.SB, BB: g.BB, SBID: g.SBID, BBID: g.BBID,
		Board: g.Board, CurBet: g.CurBet, TurnID: g.Turn, Result: g.Result, Seats: []pokerSeat{},
	}
	if g.Opts.BlindUp > 0 && g.HandNo > 0 && (g.HandNo-1)/g.Opts.BlindUp < len(pokerBlindSteps)-1 {
		v.NextBlinds = g.Opts.BlindUp - (g.HandNo-1)%g.Opts.BlindUp
	}
	if g.Button >= 0 && g.Button < len(g.Seats) {
		v.ButtonID = g.Seats[g.Button]
	}
	seats := g.Seats
	if len(seats) == 0 { // phòng chờ: ghế theo thứ tự vào phòng
		for _, p := range g.Players {
			seats = append(seats, p.ID)
		}
	}
	for _, id := range seats {
		v.Pot += g.Total[id]
		s := pokerSeat{ID: id, Stack: g.Stack[id], Bet: g.Bet[id], InHand: g.InHand[id], Folded: g.Folded[id],
			AllIn: g.AllIn[id], Away: g.Away[id], Last: g.Last[id], Net: g.net(id)}
		s.Out = g.Status == Playing && g.Stack[id] == 0 && !g.live(id)
		if g.betting() && g.live(id) {
			s.Cards = len(g.Hole[id])
		}
		v.Seats = append(v.Seats, s)
	}
	if viewer == 0 || !g.isPlayer(viewer) {
		return v, nil
	}
	me := pokerMe{Hole: []string{}, Away: g.Away[viewer]}
	if g.InHand[viewer] && (g.betting() || g.Phase == "showdown") {
		me.Hole = g.Hole[viewer]
		me.HandName = partialHandName(append(slices.Clone(me.Hole), g.Board...))
	}
	if g.betting() && g.Turn == viewer {
		me.ToCall = min(g.CurBet-g.Bet[viewer], g.Stack[viewer])
		me.CanCheck = g.Bet[viewer] >= g.CurBet
		me.CanCall = me.ToCall > 0
		me.MaxTo = g.Bet[viewer] + g.Stack[viewer]
		others := 0
		for _, id := range g.Seats {
			if id != viewer && g.canAct(id) {
				others++
			}
		}
		me.CanRaise = !g.Acted[viewer] && others > 0 && me.MaxTo > g.CurBet
		me.MinTo = min(g.CurBet+g.MinRaise, me.MaxTo)
	}
	return v, me
}
