package game

import (
	"encoding/json"
	"testing"
	"time"
)

func newStation(t *testing.T, n int, opts string) (*Game, *station, []Player) {
	t.Helper()
	g, ps := newGame(t, "station", n, opts)
	act(t, g, ps[0], "start", nil)
	s := g.r.(*station)
	if s.Phase != "intro" {
		t.Fatalf("phase = %s", s.Phase)
	}
	if g.Step(t0.Add(stIntro+time.Second)) == false || s.Phase != "play" {
		t.Fatalf("intro should end, phase=%s", s.Phase)
	}
	return g, s, ps
}

func actAt(g *Game, p Player, action string, data any, now time.Time) error {
	var raw []byte
	if data != nil {
		raw = mustMarshal(data)
	}
	return g.Act(p, action, raw, now)
}

func mustMarshal(v any) []byte {
	b, _ := jsonMarshal(v)
	return b
}

func split(s *station, ps []Player) (bad, good []Player) {
	for _, p := range ps {
		if s.P[p.ID].Traitor {
			bad = append(bad, p)
		} else {
			good = append(good, p)
		}
	}
	return
}

// place đặt người chơi vào (x, y) như thể họ tự đi tới.
func place(s *station, p Player, x, y float64) {
	s.P[p.ID].X, s.P[p.ID].Y = x, y
}

func TestStationMapIsConnected(t *testing.T) {
	// từ sảnh phải đi tới được mọi trạm nhiệm vụ (BFS trên lưới 10 đơn vị)
	const step = 10.0
	type pt struct{ x, y int }
	start := pt{int(stationMap.Spawn[0] / step), int(stationMap.Spawn[1] / step)}
	seen := map[pt]bool{start: true}
	queue := []pt{start}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, d := range []pt{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := pt{c.x + d.x, c.y + d.y}
			if !seen[n] && walkable(float64(n.x)*step, float64(n.y)*step) {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	for _, st := range stationMap.Stations {
		if !walkable(st.X, st.Y) {
			t.Errorf("station %s is not on walkable floor", st.ID)
			continue
		}
		reach := false
		for dx := -10; dx <= 10 && !reach; dx++ {
			for dy := -10; dy <= 10 && !reach; dy++ {
				p := pt{int(st.X/step) + dx, int(st.Y/step) + dy}
				if seen[p] && dist(float64(p.x)*step, float64(p.y)*step, st.X, st.Y) < stUseRange {
					reach = true
				}
			}
		}
		if !reach {
			t.Errorf("station %s unreachable from spawn", st.ID)
		}
	}
	for _, r := range stationMap.Rooms {
		if !seen[pt{int((r.X + r.W/2) / step), int((r.Y + r.H/2) / step)}] {
			t.Errorf("room %s unreachable", r.ID)
		}
	}
}

func TestStationMovement(t *testing.T) {
	g, s, ps := newStation(t, 5, "")
	p := ps[0]
	me := s.P[p.ID]
	x0, y0 := me.X, me.Y
	now := t0.Add(10 * time.Second)
	g.Move(p.ID, x0+20, y0, now)
	if me.X != x0+20 {
		t.Fatal("small step must be accepted")
	}
	c := me.Correction
	g.Move(p.ID, x0+2000, y0, now.Add(100*time.Millisecond))
	if me.X != x0+20 || me.Correction != c+1 {
		t.Fatal("teleport must be rejected with a correction")
	}
	// ra ngoài tường (vùng trống giữa sảnh và đài chỉ huy, ngoài hành lang)
	place(s, p, 1250, 730)
	g.Move(p.ID, 1250, 690, now.Add(300*time.Millisecond))
	if me.Y != 730 {
		t.Fatal("walking into a wall must be rejected")
	}
}

func TestStationKillReportVote(t *testing.T) {
	g, s, ps := newStation(t, 6, `{"kill_cooldown":20,"discuss":10,"vote":30}`)
	bad, good := split(s, ps)
	if len(bad) != 1 || len(good) != 5 {
		t.Fatalf("6 players -> 1 traitor, got %d", len(bad))
	}
	k, v := bad[0], good[0]
	now := t0.Add(stIntro + 2*time.Second)
	place(s, k, 300, 900)
	place(s, v, 340, 900)
	if err := actAt(g, k, "kill", map[string]int64{"target": v.ID}, now); err == nil {
		t.Fatal("kill before first cooldown")
	}
	now = now.Add(stFirstKill)
	if err := actAt(g, good[1], "kill", map[string]int64{"target": v.ID}, now); err == nil {
		t.Fatal("crew cannot kill")
	}
	if err := actAt(g, k, "kill", map[string]int64{"target": v.ID}, now); err != nil {
		t.Fatal(err)
	}
	if !s.P[v.ID].Dead || len(s.Bodies) != 1 {
		t.Fatal("victim should be dead with a body")
	}
	// người sống ở xa không thấy xác, không thấy hồn ma
	far := good[1]
	place(s, far, 2600, 900)
	f := g.Frame(far.ID, now).(stFrame)
	if len(f.Bodies) != 0 {
		t.Fatal("far player sees body")
	}
	for _, o := range f.Players {
		if o.ID == v.ID || o.Traitor {
			t.Fatalf("crew frame leaks ghost/traitor: %+v", o)
		}
	}
	if gf := g.Frame(v.ID, now).(stFrame); !gf.Me.Ghost || len(gf.Players) != 5 {
		t.Fatalf("ghost should see everyone: %+v", gf.Players)
	}
	if err := actAt(g, far, "report", map[string]int64{"target": v.ID}, now); err == nil {
		t.Fatal("report from far away")
	}
	place(s, far, 360, 950)
	if err := actAt(g, far, "report", map[string]int64{"target": v.ID}, now); err != nil {
		t.Fatal(err)
	}
	if s.Phase != "meeting" {
		t.Fatal("report starts a meeting")
	}
	if err := actAt(g, far, "vote", map[string]int64{"target": k.ID}, now); err == nil {
		t.Fatal("cannot vote during discussion")
	}
	act(t, g, far, "chat", map[string]string{"text": "Tôi thấy nó ở phòng máy!"})
	act(t, g, v, "chat", map[string]string{"text": "boo"})
	if fm := g.Frame(far.ID, now).(stFrame).Meeting; len(fm.Chat) != 1 {
		t.Fatalf("living must not see ghost chat: %+v", fm.Chat)
	}
	if err := actAt(g, v, "vote", map[string]int64{"target": k.ID}, now.Add(11*time.Second)); err == nil {
		t.Fatal("ghost cannot vote")
	}
	vt := now.Add(11 * time.Second)
	for _, p := range ps {
		if !s.P[p.ID].Dead {
			target := k.ID
			if p.ID == k.ID {
				target = far.ID
			}
			if err := actAt(g, p, "vote", map[string]int64{"target": target}, vt); err != nil {
				t.Fatal(err)
			}
		}
	}
	if s.Phase != "eject" || s.Meeting.Ejected != k.ID {
		t.Fatalf("traitor should be ejected: phase=%s", s.Phase)
	}
	g.Step(vt.Add(stEject + time.Second))
	if g.Status() != Ended || g.Result() != "Thuyền viên thắng!" {
		t.Fatalf("crew should win: %s %q", g.Status(), g.Result())
	}
}

func TestStationTasksWin(t *testing.T) {
	g, s, ps := newStation(t, 4, `{"tasks":2}`)
	_, good := split(s, ps)
	now := t0.Add(time.Minute)
	for _, p := range good {
		for _, task := range s.P[p.ID].Tasks {
			st, _ := stStationByID(task.Station)
			place(s, p, st.X, st.Y)
			if err := actAt(g, p, "task", map[string]string{"station": st.ID}, now); err == nil {
				t.Fatal("task done without starting")
			}
			if err := actAt(g, p, "use", map[string]string{"station": st.ID}, now); err != nil {
				t.Fatal(err)
			}
			if err := actAt(g, p, "task", map[string]string{"station": st.ID}, now.Add(time.Second/2)); err == nil && st.Kind != "align" {
				t.Fatal("task done too fast")
			}
			now = now.Add(7 * time.Second)
			if err := actAt(g, p, "task", map[string]string{"station": st.ID}, now); err != nil && g.Status() == Playing {
				t.Fatalf("%s: %v", st.ID, err)
			}
		}
	}
	if g.Status() != Ended || g.Result() != "Thuyền viên thắng!" {
		t.Fatalf("tasks should win: %s %q", g.Status(), g.Result())
	}
}

func TestStationOxygenSabotage(t *testing.T) {
	g, s, ps := newStation(t, 5, "")
	bad, good := split(s, ps)
	now := t0.Add(stIntro + stFirstKill + time.Second)
	if err := actAt(g, good[0], "sabotage", map[string]string{"kind": "oxygen"}, now); err == nil {
		t.Fatal("crew cannot sabotage")
	}
	if err := actAt(g, bad[0], "sabotage", map[string]string{"kind": "oxygen"}, now); err != nil {
		t.Fatal(err)
	}
	if s.Sab != "oxygen" {
		t.Fatal("oxygen sabotage")
	}
	place(s, good[0], stationMap.Button[0], stationMap.Button[1])
	if err := actAt(g, good[0], "emergency", nil, now); err == nil {
		t.Fatal("no emergency meeting during sabotage")
	}
	// sửa kịp
	fix, _ := stStationByID("fix-oxygen")
	place(s, good[1], fix.X, fix.Y)
	if err := actAt(g, good[1], "use", map[string]string{"station": fix.ID}, now); err != nil {
		t.Fatal(err)
	}
	if err := actAt(g, good[1], "fix", map[string]string{"station": fix.ID}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if s.Sab != "" {
		t.Fatal("oxygen should be fixed")
	}
	// lần sau không sửa: hết giờ thì kẻ phản bội thắng
	later := now.Add(3*time.Second + stSabCooldown + time.Second)
	if err := actAt(g, bad[0], "sabotage", map[string]string{"kind": "oxygen"}, later); err != nil {
		t.Fatal(err)
	}
	g.Step(later.Add(stO2Time + time.Second))
	if g.Result() != "Kẻ phản bội thắng!" {
		t.Fatalf("result = %q", g.Result())
	}
}

func TestStationTraitorsOutnumber(t *testing.T) {
	g, s, ps := newStation(t, 4, `{"kill_cooldown":10}`)
	bad, good := split(s, ps)
	now := t0.Add(time.Minute)
	for i := 0; i < 2; i++ {
		place(s, bad[0], 300, 900)
		place(s, good[i], 320, 900)
		if err := actAt(g, bad[0], "kill", map[string]int64{"target": good[i].ID}, now); err != nil {
			t.Fatal(err)
		}
		now = now.Add(11 * time.Second)
	}
	if g.Result() != "Kẻ phản bội thắng!" {
		t.Fatalf("1 traitor vs 1 crew -> traitors win, got %q", g.Result())
	}
	if pub := g.View(0).State.(stPublic); len(pub.Traitors) != 1 {
		t.Fatal("traitors revealed at end")
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
