package game

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func players(n int) []Player {
	out := make([]Player, n)
	for i := range out {
		out[i] = Player{ID: int64(i + 1), Name: string(rune('A' + i))}
	}
	return out
}

func newGame(t *testing.T, kind string, n int, opts string) (*Game, []Player) {
	t.Helper()
	ps := players(n)
	var raw json.RawMessage
	if opts != "" {
		raw = json.RawMessage(opts)
	}
	g, err := New(kind, ps[0], raw, t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps[1:] {
		if err := g.Act(p, "join", nil, t0); err != nil {
			t.Fatalf("join %s: %v", p.Name, err)
		}
	}
	return g, ps
}

func act(t *testing.T, g *Game, p Player, action string, data any) {
	t.Helper()
	if err := tryAct(g, p, action, data); err != nil {
		t.Fatalf("%s %s: %v", p.Name, action, err)
	}
}

func tryAct(g *Game, p Player, action string, data any) error {
	var raw json.RawMessage
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	return g.Act(p, action, raw, t0)
}

func roundTrip(t *testing.T, g *Game) *Game {
	t.Helper()
	b, err := g.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	g2, err := Load(g.Kind(), b)
	if err != nil {
		t.Fatal(err)
	}
	return g2
}

func TestCaroWinAndBlockRule(t *testing.T) {
	g, _ := newGame(t, "caro", 2, "")
	if g.Status() != Playing {
		t.Fatalf("caro must auto-start when 2 players joined, got %s", g.Status())
	}
	x, o := g.Players()[0], g.Players()[1]
	if err := tryAct(g, o, "move", map[string]int{"cell": 0}); err == nil {
		t.Fatal("O must not move first")
	}
	// X: hàng 7, cột 3..7; O đánh rải rác
	for i, oc := range []int{0, 2, 4, 6} {
		act(t, g, x, "move", map[string]int{"cell": 7*caroSize + 3 + i})
		act(t, g, o, "move", map[string]int{"cell": oc})
	}
	g = roundTrip(t, g)
	act(t, g, x, "move", map[string]int{"cell": 7*caroSize + 7})
	if g.Status() != Ended || len(g.Winners()) != 1 || g.Winners()[0] != x.ID {
		t.Fatalf("X should win, status=%s winners=%v", g.Status(), g.Winners())
	}
	if err := tryAct(g, o, "move", map[string]int{"cell": 100}); err == nil {
		t.Fatal("no moves after the game ended")
	}

	// chặn hai đầu: O ở hai đầu hàng 5 của X thì X chưa thắng
	g, _ = newGame(t, "caro", 2, "")
	x, o = g.Players()[0], g.Players()[1]
	row := 3 * caroSize
	xs := []int{row + 2, row + 3, row + 4, row + 5, row + 6}
	os := []int{row + 1, row + 7, 100, 102, 104}
	for i := range xs {
		act(t, g, x, "move", map[string]int{"cell": xs[i]})
		if g.Status() == Ended {
			t.Fatalf("blocked five must not win (move %d)", i)
		}
		if i < 2 {
			act(t, g, o, "move", map[string]int{"cell": os[i]})
		} else {
			act(t, g, o, "move", map[string]int{"cell": os[i]})
		}
	}
	if g.Status() != Playing {
		t.Fatalf("status = %s, want playing", g.Status())
	}
}

func TestCaroTimeout(t *testing.T) {
	g, _ := newGame(t, "caro", 2, "")
	if g.Tick(t0.Add(time.Minute)) {
		t.Fatal("tick before deadline must do nothing")
	}
	if !g.Tick(t0.Add(caroTurnTime + time.Second)) {
		t.Fatal("tick after deadline must change the game")
	}
	if g.Winners()[0] != g.Players()[1].ID {
		t.Fatalf("O should win when X times out, winners=%v", g.Winners())
	}
}

func TestCaroRandomFirstPlayer(t *testing.T) {
	var hostFirst, guestFirst int
	for range 100 {
		g, ps := newGame(t, "caro", 2, "")
		if g.Players()[0].ID == ps[0].ID {
			hostFirst++
		} else {
			guestFirst++
		}
	}
	if hostFirst == 0 || guestFirst == 0 {
		t.Fatalf("caro first player is not random: hostFirst=%d guestFirst=%d", hostFirst, guestFirst)
	}
}

func TestCaroBeyondBoundaries(t *testing.T) {
	g, _ := newGame(t, "caro", 2, "")
	x, o := g.Players()[0], g.Players()[1]
	coordsX := [][2]int{{13, 13}, {14, 14}, {15, 15}, {16, 16}, {17, 17}}
	coordsO := [][2]int{{-1, -1}, {-2, -2}, {-3, -3}, {-4, -4}}
	for i := range 4 {
		act(t, g, x, "move", map[string]int{"r": coordsX[i][0], "c": coordsX[i][1]})
		act(t, g, o, "move", map[string]int{"r": coordsO[i][0], "c": coordsO[i][1]})
	}
	act(t, g, x, "move", map[string]int{"r": coordsX[4][0], "c": coordsX[4][1]})
	if g.Status() != Ended || len(g.Winners()) != 1 || g.Winners()[0] != x.ID {
		t.Fatalf("X should win across boundary, status=%s winners=%v", g.Status(), g.Winners())
	}
}

func TestRPS(t *testing.T) {
	g, ps := newGame(t, "rps", 2, `{"best_of":3}`)
	a, b := ps[0], ps[1]
	act(t, g, a, "pick", map[string]string{"choice": "rock"})
	v := g.View(b.ID)
	if v.Me.(rpsMe).Pick != "" {
		t.Fatal("B must not see A's pick")
	}
	if !v.State.(rpsPublic).Ready[0] {
		t.Fatal("A should be shown as ready")
	}
	if err := tryAct(g, a, "pick", map[string]string{"choice": "paper"}); err == nil {
		t.Fatal("cannot change pick")
	}
	act(t, g, b, "pick", map[string]string{"choice": "scissors"}) // A thắng ván 1
	act(t, g, a, "pick", map[string]string{"choice": "paper"})
	act(t, g, b, "pick", map[string]string{"choice": "paper"}) // hoà
	act(t, g, a, "pick", map[string]string{"choice": "scissors"})
	act(t, g, b, "pick", map[string]string{"choice": "paper"}) // A thắng ván 3
	if g.Status() != Ended || g.Winners()[0] != a.ID {
		t.Fatalf("A should win 2-0, got %s %v", g.Status(), g.Winners())
	}
}

func TestLobby(t *testing.T) {
	ps := players(5)
	g, _ := New("werewolf", ps[0], nil, t0)
	if err := tryAct(g, ps[0], "start", nil); err == nil {
		t.Fatal("cannot start with 1 player")
	}
	act(t, g, ps[1], "join", nil)
	if err := tryAct(g, ps[1], "join", nil); err == nil {
		t.Fatal("double join")
	}
	if err := tryAct(g, ps[1], "start", nil); err == nil {
		t.Fatal("only host starts")
	}
	act(t, g, ps[1], "leave", nil)
	if len(g.Players()) != 1 {
		t.Fatal("leave failed")
	}
	if !g.Tick(t0.Add(lobbyTimeout+time.Second)) || g.Status() != Ended {
		t.Fatal("idle lobby must be cancelled")
	}
	if _, err := New("nope", ps[0], nil, t0); err == nil {
		t.Fatal("unknown kind")
	}
	// tuỳ chọn không được ghi đè trạng thái
	g, _ = New("werewolf", ps[0], json.RawMessage(`{"day_seconds":5,"host_id":99,"roles":{"1":"wolf"}}`), t0)
	w := g.r.(*werewolf)
	if w.HostID != 1 || w.Roles != nil || w.Opts.DaySeconds != 30 {
		t.Fatalf("options leaked into state: host=%d roles=%v day=%d", w.HostID, w.Roles, w.Opts.DaySeconds)
	}
}

// byRole: người chơi đầu tiên có vai role.
func byRole(w *werewolf, ps []Player, role string) []Player {
	var out []Player
	for _, p := range ps {
		if w.Roles[p.ID] == role {
			out = append(out, p)
		}
	}
	return out
}

func TestWerewolfFullGame(t *testing.T) {
	g, ps := newGame(t, "werewolf", 7, "")
	act(t, g, ps[0], "start", nil)
	w := g.r.(*werewolf)
	wolves := byRole(w, ps, roleWolf)
	seer := byRole(w, ps, roleSeer)[0]
	guard := byRole(w, ps, roleGuard)[0]
	witch := byRole(w, ps, roleWitch)[0]
	villagers := byRole(w, ps, roleVillager)
	if len(wolves) != 2 || len(villagers) != 2 {
		t.Fatalf("composition for 7: wolves=%d villagers=%d", len(wolves), len(villagers))
	}

	// Phần riêng: Sói thấy đồng bọn, dân không thấy gì
	if me := g.View(wolves[0].ID).Me.(wwMe); len(me.Wolves) != 2 || me.Step != "wolf" {
		t.Fatalf("wolf view: %+v", me)
	}
	if v := g.View(villagers[0].ID); v.Me.(wwMe).Wolves != nil || v.Me.(wwMe).Step != "" {
		t.Fatalf("villager view leaks: %+v", v.Me)
	}
	if pub := g.View(0).State.(wwPublic); len(pub.Roles) != 0 {
		t.Fatal("roles must be hidden at the start")
	}

	// Đêm 1: Sói cắn dân 0, Bảo vệ che đúng người đó -> không ai chết
	victim := villagers[0]
	if err := tryAct(g, wolves[0], "night", map[string]int64{"target": wolves[1].ID}); err == nil {
		t.Fatal("wolves cannot bite wolves")
	}
	act(t, g, wolves[0], "night", map[string]int64{"target": victim.ID})
	act(t, g, seer, "night", map[string]int64{"target": wolves[0].ID})
	if log := g.View(seer.ID).Me.(wwMe).SeerLog; len(log) != 1 || !log[0].Wolf {
		t.Fatalf("seer should learn wolf: %+v", log)
	}
	act(t, g, guard, "night", map[string]int64{"target": victim.ID})
	if w.Phase != "night" {
		t.Fatal("night must wait until wolves agree")
	}
	act(t, g, wolves[1], "night", map[string]int64{"target": victim.ID})
	if w.Phase != "witch" {
		t.Fatalf("phase = %s, want witch", w.Phase)
	}
	if pub := g.View(0).State.(wwPublic); pub.Phase != "night" {
		t.Fatal("witch phase must look like night publicly")
	}
	if me := g.View(witch.ID).Me.(wwMe); me.Victim != victim.ID || me.Step != "witch" {
		t.Fatalf("witch view: %+v", me)
	}
	g = roundTrip(t, g)
	w = g.r.(*werewolf)
	act(t, g, witch, "witch", map[string]any{"heal": false, "poison": 0})
	if w.Phase != "day" || len(w.Dead) != 0 {
		t.Fatalf("guarded victim must survive: phase=%s dead=%v", w.Phase, w.Dead)
	}

	// Ngày 1: treo cổ Sói 0
	for _, p := range ps {
		target := wolves[0].ID
		if p.ID == wolves[0].ID {
			target = victim.ID
		}
		act(t, g, p, "vote", map[string]int64{"target": target})
	}
	if !g.Tick(w.Deadline.Add(time.Second)) {
		t.Fatal("day should resolve")
	}
	if !w.Dead[wolves[0].ID] || w.Phase != "night" || w.Round != 2 {
		t.Fatalf("wolf 0 should be lynched: dead=%v phase=%s", w.Dead, w.Phase)
	}
	if pub := g.View(0).State.(wwPublic); pub.Roles[wolves[0].ID] != roleWolf {
		t.Fatal("dead player's role should be revealed")
	}
	if err := tryAct(g, wolves[0], "night", map[string]int64{"target": victim.ID}); err == nil {
		t.Fatal("dead players cannot act")
	}

	// Đêm 2: Bảo vệ không được che lại người cũ; Sói còn lại cắn dân 1; Phù thủy độc Sói cuối -> dân thắng
	if err := tryAct(g, guard, "night", map[string]int64{"target": victim.ID}); err == nil {
		t.Fatal("guard cannot protect the same player twice in a row")
	}
	act(t, g, wolves[1], "night", map[string]int64{"target": villagers[1].ID})
	g.Tick(w.Deadline.Add(time.Second)) // hết giờ đêm
	if w.Phase != "witch" {
		t.Fatalf("phase=%s want witch", w.Phase)
	}
	act(t, g, witch, "witch", map[string]any{"heal": true, "poison": wolves[1].ID})
	if g.Status() != Ended || g.Result() != "Dân làng thắng!" {
		t.Fatalf("village should win: %s %q", g.Status(), g.Result())
	}
	if w.Dead[villagers[1].ID] {
		t.Fatal("healed victim must survive")
	}
	if pub := g.View(0).State.(wwPublic); len(pub.Roles) != 7 {
		t.Fatal("all roles revealed at the end")
	}
	for _, id := range g.Winners() {
		if w.Roles[id] == roleWolf {
			t.Fatal("wolves are not winners")
		}
	}
}

func TestWerewolfWolvesWinAndHunter(t *testing.T) {
	g, ps := newGame(t, "werewolf", 9, `{"day_seconds":60}`)
	act(t, g, ps[0], "start", nil)
	w := g.r.(*werewolf)
	hunter := byRole(w, ps, roleHunter)[0]
	wolves := byRole(w, ps, roleWolf)
	if len(wolves) != 3 {
		t.Fatalf("9 players -> 3 wolves, got %d", len(wolves))
	}
	for _, wf := range wolves {
		act(t, g, wf, "night", map[string]int64{"target": hunter.ID})
	}
	g.Tick(w.Deadline.Add(time.Second)) // hết đêm (Tiên tri/Bảo vệ không làm gì)
	if w.Phase == "witch" {
		g.Tick(w.Deadline.Add(time.Second))
	}
	if w.Phase != "hunter" || !w.Dead[hunter.ID] {
		t.Fatalf("hunter should get a shot, phase=%s", w.Phase)
	}
	if me := g.View(hunter.ID).Me.(wwMe); me.Step != "shoot" {
		t.Fatalf("hunter step = %q", me.Step)
	}
	act(t, g, hunter, "shoot", map[string]int64{"target": wolves[0].ID})
	if w.Phase != "day" || !w.Dead[wolves[0].ID] {
		t.Fatalf("after hunter shot: phase=%s", w.Phase)
	}
	// chủ phòng chốt sớm, không ai bỏ phiếu -> không ai chết
	if err := tryAct(g, ps[1], "skip_day", nil); err == nil && ps[1].ID != w.HostID {
		t.Fatal("only host can skip day")
	}
	act(t, g, ps[0], "skip_day", nil)
	if w.Phase != "night" || len(w.Dead) != 2 {
		t.Fatalf("skip day: phase=%s dead=%v", w.Phase, w.Dead)
	}
	// Sói giết dần tới khi áp đảo
	for g.Status() == Playing {
		var target int64
		for _, p := range ps {
			if !w.Dead[p.ID] && w.Roles[p.ID] != roleWolf {
				target = p.ID
				break
			}
		}
		switch w.Phase {
		case "night":
			for _, wf := range wolves {
				if !w.Dead[wf.ID] {
					act(t, g, wf, "night", map[string]int64{"target": target})
				}
			}
			if w.Phase == "night" {
				g.Tick(w.Deadline.Add(time.Second))
			}
		default:
			g.Tick(w.Deadline.Add(time.Second))
		}
	}
	if g.Result() != "Phe Sói thắng!" {
		t.Fatalf("result = %q", g.Result())
	}
}

func TestSpy(t *testing.T) {
	g, ps := newGame(t, "spy", 5, "")
	act(t, g, ps[0], "start", nil)
	s := g.r.(*spy)
	var spyP Player
	var civ []Player
	for _, p := range ps {
		if s.Spies[p.ID] {
			spyP = p
		} else {
			civ = append(civ, p)
		}
	}
	if spyP.ID == 0 || len(civ) != 4 {
		t.Fatal("expected 1 spy")
	}
	if g.View(spyP.ID).Me.(spyMe).Word != s.Words[1] || g.View(civ[0].ID).Me.(spyMe).Word != s.Words[0] {
		t.Fatal("wrong words")
	}
	if pub := g.View(0).State.(spyPublic); pub.Words != nil || len(pub.Spies) != 0 {
		t.Fatal("public view leaks spies/words")
	}
	// lượt mô tả
	speaker := g.View(0).State.(spyPublic).Speaker
	sp := ps[speaker-1]
	if err := tryAct(g, sp, "clue", map[string]string{"text": "Nó là " + strings.ToUpper(fold(s.word(sp.ID)))}); err == nil {
		t.Fatal("clue containing own word must be rejected")
	}
	for range ps {
		speaker := g.View(0).State.(spyPublic).Speaker
		other := ps[0]
		if other.ID == speaker {
			other = ps[1]
		}
		if err := tryAct(g, other, "clue", map[string]string{"text": "x"}); err == nil {
			t.Fatal("not your turn")
		}
		act(t, g, ps[speaker-1], "clue", map[string]string{"text": "một gợi ý"})
	}
	if s.Phase != "vote" {
		t.Fatalf("phase = %s", s.Phase)
	}
	for _, p := range ps {
		target := spyP.ID
		if p.ID == spyP.ID {
			target = civ[0].ID
		}
		act(t, g, p, "vote", map[string]int64{"target": target})
	}
	g.Tick(s.Deadline.Add(time.Second))
	if g.Status() != Ended || g.Result() != "Dân thường thắng!" {
		t.Fatalf("civilians should win: %q", g.Result())
	}
	if pub := g.View(0).State.(spyPublic); pub.Words == nil || len(pub.Spies) != 1 {
		t.Fatal("words and spies revealed at end")
	}
}

func TestSpyTimeoutsAndSpyWin(t *testing.T) {
	g, ps := newGame(t, "spy", 4, "")
	act(t, g, ps[0], "start", nil)
	s := g.r.(*spy)
	// cả vòng không ai nói, không ai bỏ phiếu -> hoà, sang vòng 2
	for s.Phase == "describe" {
		g.Tick(s.Deadline.Add(time.Second))
	}
	g.Tick(s.Deadline.Add(time.Second))
	if s.Round != 2 || len(s.Out) != 0 {
		t.Fatalf("round=%d out=%v", s.Round, s.Out)
	}
	// loại 2 dân -> còn 1 dân 1 gián điệp -> gián điệp thắng
	for g.Status() == Playing {
		for s.Phase == "describe" {
			g.Tick(s.Deadline.Add(time.Second))
		}
		var target int64
		for _, p := range ps {
			if !s.Out[p.ID] && !s.Spies[p.ID] {
				target = p.ID
				break
			}
		}
		for _, p := range ps {
			if !s.Out[p.ID] && p.ID != target {
				act(t, g, p, "vote", map[string]int64{"target": target})
			}
		}
		g.Tick(s.Deadline.Add(time.Second))
	}
	if g.Result() != "Gián điệp thắng!" {
		t.Fatalf("result = %q", g.Result())
	}
}

func TestFold(t *testing.T) {
	if fold("Cà Phê") != "caphe" || !strings.Contains(fold("uống CA PHE sữa"), fold("Cà phê")) {
		t.Fatal("fold")
	}
}
