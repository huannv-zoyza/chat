package game

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Ma Sói, luật như chơi ngoài đời, máy chủ làm quản trò.
//
// Đêm: Sói chọn chung một người để cắn (đồng lòng thì đêm kết thúc sớm, không thì lấy người
// nhiều phiếu nhất khi hết giờ); Tiên tri soi một người (biết có phải Sói không); Bảo vệ che
// một người (không được che cùng một người hai đêm liền). Sau đó Phù thủy biết ai bị cắn,
// được dùng bình cứu (1 lần) và bình độc (1 lần).
// Ngày: cả làng thảo luận ngay trong kênh chat, bỏ phiếu treo cổ; người nhiều phiếu nhất (không
// hoà, nhiều hơn số phiếu bỏ qua) bị treo. Thợ săn chết thì được bắn chết một người.
// Dân thắng khi hết Sói; Sói thắng khi số Sói >= số người còn lại.

const (
	roleWolf     = "wolf"
	roleVillager = "villager"
	roleSeer     = "seer"
	roleGuard    = "guard"
	roleWitch    = "witch"
	roleHunter   = "hunter"
)

var roleNames = map[string]string{
	roleWolf: "Sói", roleVillager: "Dân làng", roleSeer: "Tiên tri",
	roleGuard: "Bảo vệ", roleWitch: "Phù thủy", roleHunter: "Thợ săn",
}

const (
	witchTime  = 25 * time.Second
	hunterTime = 25 * time.Second
	// mọi người đã bỏ phiếu: chốt sau chừng này (để kịp đổi ý)
	voteGrace = 5 * time.Second
)

type wwOptions struct {
	DaySeconds   int  `json:"day_seconds"`
	NightSeconds int  `json:"night_seconds"`
	HideRoles    bool `json:"hide_roles"` // không lộ vai người chết cho tới hết ván
	// Cast: bộ vai chủ phòng tự chọn (số Sói, có/không các vai đặc biệt); còn lại là Dân làng.
	// Rỗng = tự động theo số người (wwComposition).
	Cast map[string]int `json:"cast,omitempty"`
}

// Vai chủ phòng chọn được và số tối đa mỗi vai.
var castMax = map[string]int{roleWolf: 5, roleSeer: 1, roleGuard: 1, roleWitch: 1, roleHunter: 1}

type seerCheck struct {
	Round  int   `json:"round"`
	Target int64 `json:"target"`
	Wolf   bool  `json:"wolf"`
}

type werewolf struct {
	Base
	Opts  wwOptions        `json:"opts"`
	Roles map[int64]string `json:"roles"`
	Dead  map[int64]bool   `json:"dead"`
	// night | witch | day | hunter
	Phase string `json:"phase,omitempty"`
	Round int    `json:"round,omitempty"`

	WolfVotes   map[int64]int64 `json:"wolf_votes"`
	SeerTarget  int64           `json:"seer_target,omitempty"`
	SeerLog     []seerCheck     `json:"seer_log,omitempty"`
	GuardTarget int64           `json:"guard_target,omitempty"`
	LastGuard   int64           `json:"last_guard,omitempty"`
	HealUsed    bool            `json:"heal_used,omitempty"`
	PoisonUsed  bool            `json:"poison_used,omitempty"`
	Victim      int64           `json:"victim,omitempty"` // người bị Sói cắn đêm nay
	WitchHeal   bool            `json:"witch_heal,omitempty"`
	WitchPoison int64           `json:"witch_poison,omitempty"`

	DayVotes map[int64]int64 `json:"day_votes"`
	// Thợ săn đang chọn người bắn, và pha tiếp theo sau khi bắn (day | night).
	Hunter      int64  `json:"hunter,omitempty"`
	HunterAfter string `json:"hunter_after,omitempty"`

	// Kênh chat riêng của bầy Sói: chỉ Sói đọc/viết, hết ván mới lộ cho mọi người.
	WolfChat []wolfLine `json:"wolf_chat,omitempty"`
}

type wolfLine struct {
	From int64     `json:"from"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

const (
	wolfChatMax  = 200 // giữ chừng này dòng gần nhất
	wolfLineMax  = 300 // ký tự mỗi dòng
	wolfChatRate = 600 * time.Millisecond
)

func (w *werewolf) limits() (int, int) { return 4, 16 }
func (w *werewolf) autoStart() bool    { return false }
func (w *werewolf) options() any       { return &w.Opts }

func (w *werewolf) normalize() {
	if w.Opts.DaySeconds == 0 {
		w.Opts.DaySeconds = 120
	}
	if w.Opts.NightSeconds == 0 {
		w.Opts.NightSeconds = 45
	}
	w.Opts.DaySeconds = min(max(w.Opts.DaySeconds, 30), 600)
	w.Opts.NightSeconds = min(max(w.Opts.NightSeconds, 20), 180)
	if len(w.Opts.Cast) > 0 {
		cast := map[string]int{}
		for role, most := range castMax {
			if k := min(max(w.Opts.Cast[role], 0), most); k > 0 {
				cast[role] = k
			}
		}
		cast[roleWolf] = max(cast[roleWolf], 1)
		w.Opts.Cast = cast
	}
}

// castMin: số người ít nhất để chơi được bộ vai tự chọn (đủ chỗ cho mọi vai, Sói ít hơn phe còn lại).
func castMin(cast map[string]int) int {
	used := 0
	for _, k := range cast {
		used += k
	}
	return max(used, 2*cast[roleWolf]+1, 4)
}

// composition: bộ vai sẽ dùng cho n người (tự chọn hoặc tự động).
func (w *werewolf) composition(n int) map[string]int {
	if len(w.Opts.Cast) == 0 {
		return wwComposition(n)
	}
	c := map[string]int{}
	used := 0
	for role, k := range w.Opts.Cast {
		c[role] = k
		used += k
	}
	if n > used {
		c[roleVillager] = n - used
	}
	return c
}

// setup: chủ phòng đổi tuỳ chọn trong phòng chờ (gửi đủ bộ tuỳ chọn, thay hẳn bộ cũ).
func (w *werewolf) setup(data json.RawMessage) error {
	opts := wwOptions{}
	if err := json.Unmarshal(data, &opts); err != nil {
		return Error("Tuỳ chọn không hợp lệ.")
	}
	w.Opts = opts
	w.normalize()
	return nil
}

// composition: số người mỗi vai cho n người chơi.
func wwComposition(n int) map[string]int {
	c := map[string]int{}
	switch {
	case n <= 5:
		c[roleWolf] = 1
	case n <= 8:
		c[roleWolf] = 2
	case n <= 12:
		c[roleWolf] = 3
	default:
		c[roleWolf] = 4
	}
	c[roleSeer] = 1
	if n >= 5 {
		c[roleGuard] = 1
	}
	if n >= 7 {
		c[roleWitch] = 1
	}
	if n >= 9 {
		c[roleHunter] = 1
	}
	used := 0
	for _, k := range c {
		used += k
	}
	if n-used > 0 {
		c[roleVillager] = n - used
	}
	return c
}

func (w *werewolf) start(now time.Time) error {
	n := len(w.Players)
	if len(w.Opts.Cast) > 0 && n < castMin(w.Opts.Cast) {
		return Error(fmt.Sprintf("Bộ vai này cần ít nhất %d người (Sói phải ít hơn phe Dân).", castMin(w.Opts.Cast)))
	}
	comp := w.composition(n)
	var deck []string
	for _, role := range []string{roleWolf, roleSeer, roleGuard, roleWitch, roleHunter, roleVillager} {
		for range comp[role] {
			deck = append(deck, role)
		}
	}
	shuffle(deck)
	w.Roles = map[int64]string{}
	for i, p := range w.Players {
		w.Roles[p.ID] = deck[i]
	}
	w.Dead = map[int64]bool{}
	w.logf("Ván bắt đầu với %d người: %s.", n, compositionText(comp))
	w.startNight(now)
	return nil
}

func compositionText(c map[string]int) string {
	var parts []string
	for _, role := range []string{roleWolf, roleSeer, roleGuard, roleWitch, roleHunter, roleVillager} {
		if c[role] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[role], roleNames[role]))
		}
	}
	return strings.Join(parts, ", ")
}

func (w *werewolf) alive(id int64) bool { return w.isPlayer(id) && !w.Dead[id] }

func (w *werewolf) aliveWith(role string) []int64 {
	var out []int64
	for _, p := range w.Players {
		if !w.Dead[p.ID] && w.Roles[p.ID] == role {
			out = append(out, p.ID)
		}
	}
	return out
}

func (w *werewolf) startNight(now time.Time) {
	w.Round++
	w.Phase = "night"
	w.WolfVotes = map[int64]int64{}
	w.SeerTarget, w.GuardTarget, w.Victim = 0, 0, 0
	w.WitchHeal, w.WitchPoison = false, 0
	w.DayVotes = nil
	w.setDeadline(now, time.Duration(w.Opts.NightSeconds)*time.Second)
	w.logf("🌙 Đêm %d buông xuống. Cả làng đi ngủ.", w.Round)
}

func (w *werewolf) startDay(now time.Time) {
	w.Phase = "day"
	w.DayVotes = map[int64]int64{}
	w.setDeadline(now, time.Duration(w.Opts.DaySeconds)*time.Second)
}

// wolvesAgree: mọi Sói còn sống đã chọn cùng một người.
func (w *werewolf) wolvesAgree() bool {
	wolves := w.aliveWith(roleWolf)
	var t int64
	for i, id := range wolves {
		v, ok := w.WolfVotes[id]
		if !ok || (i > 0 && v != t) {
			return false
		}
		t = v
	}
	return true
}

func (w *werewolf) nightReady() bool {
	if !w.wolvesAgree() {
		return false
	}
	if len(w.aliveWith(roleSeer)) > 0 && w.SeerTarget == 0 {
		return false
	}
	if len(w.aliveWith(roleGuard)) > 0 && w.GuardTarget == 0 {
		return false
	}
	return true
}

func (w *werewolf) roleText(id int64) string {
	if w.Opts.HideRoles {
		return ""
	}
	return " (" + roleNames[w.Roles[id]] + ")"
}

// endWolves: hết lượt Sói/Tiên tri/Bảo vệ. Chốt người bị cắn rồi tới lượt Phù thủy (nếu có).
func (w *werewolf) endWolves(now time.Time) {
	target, _, counts := tally(w.WolfVotes)
	if target == 0 {
		// hoà phiếu giữa các Sói: chọn ngẫu nhiên trong số nhiều phiếu nhất
		best := 0
		var top []int64
		for t, c := range counts {
			if t == 0 {
				continue
			}
			if c > best {
				best, top = c, []int64{t}
			} else if c == best {
				top = append(top, t)
			}
		}
		if len(top) > 0 {
			slices.Sort(top)
			target = pick(top)
		}
	}
	w.Victim = target
	if witches := w.aliveWith(roleWitch); len(witches) > 0 && (!w.HealUsed || !w.PoisonUsed) {
		w.Phase = "witch"
		w.setDeadline(now, witchTime)
		return
	}
	w.resolveNight(now)
}

func (w *werewolf) resolveNight(now time.Time) {
	var deaths []int64
	if v := w.Victim; v != 0 && v != w.GuardTarget && !w.WitchHeal {
		deaths = append(deaths, v)
	}
	if p := w.WitchPoison; p != 0 && !slices.Contains(deaths, p) {
		deaths = append(deaths, p)
	}
	w.LastGuard = w.GuardTarget
	for _, id := range deaths {
		w.Dead[id] = true
	}
	if len(deaths) == 0 {
		w.logf("☀️ Trời sáng. Đêm qua bình yên, không ai chết.")
	} else {
		names := make([]string, len(deaths))
		for i, id := range deaths {
			names[i] = w.name(id) + w.roleText(id)
		}
		w.logf("☀️ Trời sáng. Đêm qua %s đã chết.", strings.Join(names, ", "))
	}
	if w.checkWin() {
		return
	}
	if w.hunterDied(deaths, "day", now) {
		return
	}
	w.startDay(now)
}

func (w *werewolf) resolveDay(now time.Time) {
	target, count, _ := tally(w.DayVotes)
	if target == 0 {
		w.logf("⚖️ Ngày %d: không ai bị treo cổ (hoà phiếu hoặc bỏ qua).", w.Round)
	} else {
		w.Dead[target] = true
		w.logf("⚖️ Ngày %d: dân làng treo cổ %s%s với %d phiếu.", w.Round, w.name(target), w.roleText(target), count)
	}
	if w.checkWin() {
		return
	}
	if target != 0 && w.hunterDied([]int64{target}, "night", now) {
		return
	}
	w.startNight(now)
}

// hunterDied: nếu Thợ săn nằm trong số người vừa chết thì chuyển sang lượt bắn.
func (w *werewolf) hunterDied(deaths []int64, after string, now time.Time) bool {
	for _, id := range deaths {
		if w.Roles[id] == roleHunter {
			w.Phase = "hunter"
			w.Hunter = id
			w.HunterAfter = after
			w.setDeadline(now, hunterTime)
			w.logf("🏹 %s là Thợ săn, được bắn một phát trước khi chết.", w.name(id))
			return true
		}
	}
	return false
}

func (w *werewolf) afterHunter(target int64, now time.Time) {
	if target != 0 {
		w.Dead[target] = true
		w.logf("🏹 Thợ săn %s bắn chết %s%s.", w.name(w.Hunter), w.name(target), w.roleText(target))
	} else {
		w.logf("🏹 Thợ săn %s không bắn ai.", w.name(w.Hunter))
	}
	w.Hunter = 0
	if w.checkWin() {
		return
	}
	if w.HunterAfter == "night" {
		w.startNight(now)
	} else {
		w.startDay(now)
	}
}

func (w *werewolf) checkWin() bool {
	wolves, others := 0, 0
	for _, p := range w.Players {
		if w.Dead[p.ID] {
			continue
		}
		if w.Roles[p.ID] == roleWolf {
			wolves++
		} else {
			others++
		}
	}
	if wolves > 0 && wolves < others {
		return false
	}
	var winners []int64
	wolvesWin := wolves > 0
	for _, p := range w.Players {
		if (w.Roles[p.ID] == roleWolf) == wolvesWin {
			winners = append(winners, p.ID)
		}
	}
	w.Phase = ""
	if wolvesWin {
		w.logf("🐺 Sói đã áp đảo dân làng. Phe Sói thắng!")
		w.end("Phe Sói thắng!", winners)
	} else {
		w.logf("🎉 Đã tiêu diệt hết Sói. Dân làng thắng!")
		w.end("Dân làng thắng!", winners)
	}
	return true
}

type targetData struct {
	Target int64 `json:"target"`
}

type witchData struct {
	Heal   bool  `json:"heal"`
	Poison int64 `json:"poison"`
}

func (w *werewolf) act(p int64, action string, data json.RawMessage, now time.Time) error {
	role := w.Roles[p]
	if action == "skip_day" {
		if p != w.HostID {
			return Error("Chỉ chủ phòng mới chốt phiếu sớm được.")
		}
		if w.Phase != "day" {
			return Error("Chỉ chốt phiếu được vào ban ngày.")
		}
		w.resolveDay(now)
		return nil
	}
	if action == "wolf_chat" {
		return w.wolfChat(p, data, now)
	}
	if w.Dead[p] && action != "shoot" { // Thợ săn bắn sau khi đã chết
		return Error("Bạn đã chết, chỉ được xem.")
	}
	switch action {
	case "night":
		if w.Phase != "night" {
			return Error("Chưa tới lượt hành động ban đêm.")
		}
		var d targetData
		if err := decode(data, &d); err != nil {
			return err
		}
		if !w.alive(d.Target) {
			return Error("Người này không còn sống.")
		}
		switch role {
		case roleWolf:
			if w.Roles[d.Target] == roleWolf {
				return Error("Sói không cắn đồng bọn.")
			}
			w.WolfVotes[p] = d.Target
		case roleSeer:
			if w.SeerTarget != 0 {
				return Error("Đêm nay bạn đã soi rồi.")
			}
			if d.Target == p {
				return Error("Hãy soi người khác.")
			}
			w.SeerTarget = d.Target
			w.SeerLog = append(w.SeerLog, seerCheck{Round: w.Round, Target: d.Target, Wolf: w.Roles[d.Target] == roleWolf})
		case roleGuard:
			if w.GuardTarget != 0 {
				return Error("Đêm nay bạn đã bảo vệ rồi.")
			}
			if d.Target == w.LastGuard {
				return Error("Không được bảo vệ cùng một người hai đêm liền.")
			}
			w.GuardTarget = d.Target
		default:
			return Error("Vai của bạn không có hành động ban đêm.")
		}
		if w.nightReady() {
			w.endWolves(now)
		}
	case "witch":
		if w.Phase != "witch" || role != roleWitch {
			return Error("Chưa tới lượt Phù thủy.")
		}
		var d witchData
		if err := decode(data, &d); err != nil {
			return err
		}
		if d.Heal && (w.HealUsed || w.Victim == 0) {
			return Error("Không dùng bình cứu được.")
		}
		if d.Poison != 0 {
			if w.PoisonUsed {
				return Error("Bạn đã dùng bình độc rồi.")
			}
			if !w.alive(d.Poison) || d.Poison == p {
				return Error("Không đầu độc người này được.")
			}
		}
		if d.Heal {
			w.HealUsed, w.WitchHeal = true, true
		}
		if d.Poison != 0 {
			w.PoisonUsed, w.WitchPoison = true, d.Poison
		}
		w.resolveNight(now)
	case "vote":
		if w.Phase != "day" {
			return Error("Chỉ bỏ phiếu vào ban ngày.")
		}
		var d targetData
		if err := decode(data, &d); err != nil {
			return err
		}
		if d.Target != 0 && (!w.alive(d.Target) || d.Target == p) {
			return Error("Không bỏ phiếu cho người này được.")
		}
		w.DayVotes[p] = d.Target
		if len(w.DayVotes) == w.aliveCount() && w.Deadline != nil && w.Deadline.Sub(now) > voteGrace {
			w.setDeadline(now, voteGrace)
		}
	case "shoot":
		if w.Phase != "hunter" || p != w.Hunter {
			return Error("Chưa tới lượt Thợ săn.")
		}
		var d targetData
		if err := decode(data, &d); err != nil {
			return err
		}
		if d.Target != 0 && (!w.alive(d.Target) || d.Target == p) {
			return Error("Không bắn người này được.")
		}
		w.afterHunter(d.Target, now)
	default:
		return Error("Hành động không hợp lệ.")
	}
	return nil
}

func (w *werewolf) wolfChat(p int64, data json.RawMessage, now time.Time) error {
	if w.Roles[p] != roleWolf {
		return Error("Chỉ Sói mới vào được kênh này.")
	}
	if w.Dead[p] {
		return Error("Bạn đã chết, chỉ được đọc.")
	}
	var d struct {
		Text string `json:"text"`
	}
	if err := decode(data, &d); err != nil {
		return err
	}
	text := strings.TrimSpace(d.Text)
	if text == "" {
		return Error("Tin nhắn trống.")
	}
	if r := []rune(text); len(r) > wolfLineMax {
		text = string(r[:wolfLineMax])
	}
	for i := len(w.WolfChat) - 1; i >= 0; i-- {
		if w.WolfChat[i].From == p {
			if now.Sub(w.WolfChat[i].At) < wolfChatRate {
				return Error("Nhắn chậm lại chút.")
			}
			break
		}
	}
	w.WolfChat = append(w.WolfChat, wolfLine{From: p, Text: text, At: now})
	if len(w.WolfChat) > wolfChatMax {
		w.WolfChat = w.WolfChat[len(w.WolfChat)-wolfChatMax:]
	}
	return nil
}

// Thợ săn đang bắn thì đã được tính là chết; pha hunter vẫn cho người đó hành động.
func (w *werewolf) aliveCount() int {
	n := 0
	for _, p := range w.Players {
		if !w.Dead[p.ID] {
			n++
		}
	}
	return n
}

func (w *werewolf) tick(now time.Time) bool {
	switch w.Phase {
	case "night":
		w.endWolves(now)
	case "witch":
		w.resolveNight(now)
	case "day":
		w.resolveDay(now)
	case "hunter":
		w.afterHunter(0, now)
	default:
		return false
	}
	return true
}

type wwPublic struct {
	Phase       string           `json:"phase,omitempty"`
	Round       int              `json:"round,omitempty"`
	Dead        []int64          `json:"dead"`
	Roles       map[int64]string `json:"roles"` // vai đã lộ
	Votes       map[int64]int64  `json:"votes,omitempty"`
	Composition map[string]int   `json:"composition"`
	CastMin     int              `json:"cast_min,omitempty"` // bộ vai tự chọn: cần ít nhất chừng này người
	Hunter      int64            `json:"hunter,omitempty"`
	Opts        wwOptions        `json:"opts"`
	WolfChat    []wolfLine       `json:"wolf_chat,omitempty"` // chỉ có khi hết ván
}

type wwMe struct {
	Role       string          `json:"role,omitempty"`
	Alive      bool            `json:"alive"`
	Step       string          `json:"step,omitempty"` // việc cần làm lúc này: wolf | seer | guard | witch | vote | shoot
	Target     int64           `json:"target,omitempty"`
	Wolves     []int64         `json:"wolves,omitempty"`
	WolfVotes  map[int64]int64 `json:"wolf_votes"`
	SeerLog    []seerCheck     `json:"seer_log,omitempty"`
	LastGuard  int64           `json:"last_guard,omitempty"`
	HealLeft   bool            `json:"heal_left,omitempty"`
	PoisonLeft bool            `json:"poison_left,omitempty"`
	Victim     int64           `json:"victim,omitempty"`
	WolfChat   []wolfLine      `json:"wolf_chat,omitempty"`
}

func (w *werewolf) view(viewer int64) (any, any) {
	pub := wwPublic{Phase: w.Phase, Round: w.Round, Dead: []int64{}, Roles: map[int64]string{}, Opts: w.Opts}
	if pub.Phase == "witch" {
		pub.Phase = "night" // không để lộ có Phù thủy đang hành động
	}
	n := len(w.Players)
	if w.Status == Lobby {
		n = max(n, 4)
		if len(w.Opts.Cast) > 0 {
			n = max(n, castMin(w.Opts.Cast))
		}
	}
	pub.Composition = w.composition(n)
	if len(w.Opts.Cast) > 0 {
		pub.CastMin = castMin(w.Opts.Cast)
	}
	for _, p := range w.Players {
		if w.Dead[p.ID] {
			pub.Dead = append(pub.Dead, p.ID)
			if !w.Opts.HideRoles {
				pub.Roles[p.ID] = w.Roles[p.ID]
			}
		}
	}
	if w.Status == Ended {
		maps := map[int64]string{}
		for id, r := range w.Roles {
			maps[id] = r
		}
		pub.Roles = maps
		pub.WolfChat = w.WolfChat
	}
	if w.Phase == "day" {
		pub.Votes = w.DayVotes
	}
	if w.Phase == "hunter" {
		pub.Hunter = w.Hunter
	}
	if viewer == 0 || w.Roles == nil {
		return pub, nil
	}
	role := w.Roles[viewer]
	me := wwMe{Role: role, Alive: !w.Dead[viewer]}
	switch role {
	case roleWolf:
		me.Wolves = w.aliveOrDeadWith(roleWolf)
		me.WolfChat = w.WolfChat
		if w.Phase == "night" {
			me.WolfVotes = w.WolfVotes
			me.Target = w.WolfVotes[viewer]
		}
	case roleSeer:
		me.SeerLog = w.SeerLog
		if w.Phase == "night" {
			me.Target = w.SeerTarget
		}
	case roleGuard:
		me.LastGuard = w.LastGuard
		if w.Phase == "night" {
			me.Target = w.GuardTarget
		}
	case roleWitch:
		me.HealLeft, me.PoisonLeft = !w.HealUsed, !w.PoisonUsed
		if w.Phase == "witch" {
			me.Victim = w.Victim
		}
	}
	if w.Status == Playing {
		switch {
		case w.Phase == "hunter" && viewer == w.Hunter:
			me.Step = "shoot"
		case !me.Alive:
		case w.Phase == "night" && role == roleWolf:
			me.Step = "wolf"
		case w.Phase == "night" && role == roleSeer && w.SeerTarget == 0:
			me.Step = "seer"
		case w.Phase == "night" && role == roleGuard && w.GuardTarget == 0:
			me.Step = "guard"
		case w.Phase == "witch" && role == roleWitch:
			me.Step = "witch"
		case w.Phase == "day":
			me.Step = "vote"
		}
	}
	return pub, me
}

func (w *werewolf) aliveOrDeadWith(role string) []int64 {
	var out []int64
	for _, p := range w.Players {
		if w.Roles[p.ID] == role {
			out = append(out, p.ID)
		}
	}
	return out
}
