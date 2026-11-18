package controller_test

import (
	"testing"
	"time"
)

type gameResp struct {
	Kind    string               `json:"kind"`
	Status  string               `json:"status"`
	Players []struct{ ID int64 } `json:"players"`
	State   map[string]any       `json:"state"`
	Me      map[string]any       `json:"me"`
	Version int                  `json:"version"`
	Error   string               `json:"error"`
}

type gameMsg struct {
	ID    int64 `json:"id"`
	Embed struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"embed"`
	Game  gameResp `json:"game"`
	Error string   `json:"error"`
}

func TestGamesCaro(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")
	dave := e.register("dave", "Dave") // không ở trong kênh
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "giai-tri"}, &ch)
	for _, tk := range []string{bob.Token, carol.Token} {
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", tk, nil, nil)
	}
	wc := e.dial(carol.Token)
	path := "/api/channels/" + itoa(ch.ID) + "/games"

	if code := e.do("POST", path, alice.Token, map[string]any{"kind": "tetris"}, nil); code != 400 {
		t.Fatalf("unknown game: %d", code)
	}
	if code := e.do("POST", path, dave.Token, map[string]any{"kind": "caro"}, nil); code == 201 {
		t.Fatal("non-member created a game")
	}
	var m gameMsg
	if code := e.do("POST", path, alice.Token, map[string]any{"kind": "caro", "client_id": "g1"}, &m); code != 201 {
		t.Fatalf("create: %d %s", code, m.Error)
	}
	if m.Embed.Type != "game" || m.Game.Status != "lobby" || len(m.Game.Players) != 1 {
		t.Fatalf("created: %+v", m)
	}
	if ev := wc.waitFor("message.new"); ev["message"].(map[string]any)["game"] == nil {
		t.Fatalf("message.new without game: %v", ev)
	}
	act := func(tok, action string, data any) (int, gameResp) {
		var g gameResp
		body := map[string]any{"action": action}
		if data != nil {
			body["data"] = data
		}
		code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", tok, body, &g)
		return code, g
	}
	if code, _ := act(dave.Token, "join", nil); code != 404 {
		t.Fatalf("non-member join: %d", code)
	}
	code, g := act(bob.Token, "join", nil)
	if code != 200 || g.Status != "playing" {
		t.Fatalf("join: %d %+v", code, g)
	}
	ev := wc.waitFor("game.updated")
	if ev["game"].(map[string]any)["status"] != "playing" {
		t.Fatalf("spectator update: %v", ev)
	}
	if code, g := act(bob.Token, "move", map[string]int{"cell": 0}); code != 400 || g.Error == "" {
		t.Fatalf("O moved first: %d", code)
	}
	if code, _ := act(alice.Token, "move", map[string]int{"cell": 112}); code != 200 {
		t.Fatalf("move: %d", code)
	}
	if code, g := act(carol.Token, "move", map[string]int{"cell": 113}); code != 400 || g.Error != "Bạn không chơi ván này." {
		t.Fatalf("spectator move: %d %q", code, g.Error)
	}

	// lịch sử tin có kèm ván
	var list struct {
		Messages []gameMsg `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", carol.Token, nil, &list)
	if len(list.Messages) == 0 || list.Messages[len(list.Messages)-1].Game.State["board"] == nil {
		t.Fatalf("history without game: %+v", list)
	}

	// xoá tin -> ván không còn
	if code := e.do("DELETE", "/api/messages/"+itoa(m.ID), alice.Token, nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := act(bob.Token, "move", map[string]int{"cell": 0}); code != 404 {
		t.Fatalf("move on deleted game: %d", code)
	}
}

func TestGamesSecretsStayPrivate(t *testing.T) {
	e := newEnv(t)
	names := []string{"anh", "binh", "chi", "dung", "emm"}
	var toks []string
	for _, n := range names {
		toks = append(toks, e.register(n, n).Token)
	}
	var ch channel
	e.do("POST", "/api/channels", toks[0], map[string]any{"name": "ma-soi"}, &ch)
	for _, tk := range toks[1:] {
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", tk, nil, nil)
	}
	var m gameMsg
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", toks[0], map[string]any{"kind": "spy"}, &m)
	act := func(tok, action string) (int, gameResp) {
		var g gameResp
		code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", tok, map[string]any{"action": action}, &g)
		return code, g
	}
	for _, tk := range toks[1:4] {
		if code, g := act(tk, "join"); code != 200 {
			t.Fatalf("join: %d %s", code, g.Error)
		}
	}
	// toks[4] chỉ xem
	spectator := e.dial(toks[4])
	if code, g := act(toks[0], "start"); code != 200 || g.Me["word"] == nil {
		t.Fatalf("start: %d %+v", code, g)
	}
	ev := spectator.waitFor("game.updated")
	gm := ev["game"].(map[string]any)
	if gm["me"] != nil || gm["state"].(map[string]any)["words"] != nil {
		t.Fatalf("spectator sees secrets: %v", gm)
	}
	var list struct {
		Messages []gameMsg `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", toks[1], nil, &list)
	if g := list.Messages[len(list.Messages)-1].Game; g.Me["word"] == nil {
		t.Fatalf("player should see own word in history: %+v", g)
	}
	var list2 struct {
		Messages []gameMsg `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", toks[4], nil, &list2)
	if g := list2.Messages[len(list2.Messages)-1].Game; g.Me != nil {
		t.Fatalf("spectator has me: %+v", g)
	}
}

func TestStationRealtime(t *testing.T) {
	e := newEnv(t)
	var toks []string
	var ids []int64
	for _, n := range []string{"anh", "binh", "chi", "dung", "khach"} {
		r := e.register(n, n)
		toks = append(toks, r.Token)
		ids = append(ids, r.User.ID)
	}
	var ch channel
	e.do("POST", "/api/channels", toks[0], map[string]any{"name": "tram"}, &ch)
	for _, tk := range toks[1:] {
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", tk, nil, nil)
	}
	var m gameMsg
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", toks[0], map[string]any{"kind": "station"}, &m); code != 201 {
		t.Fatalf("create station: %d %s", code, m.Error)
	}
	act := func(tok, action string, data any) (int, gameResp) {
		var g gameResp
		body := map[string]any{"action": action}
		if data != nil {
			body["data"] = data
		}
		return e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", tok, body, &g), g
	}
	for _, tk := range toks[1:4] {
		act(tk, "join", nil)
	}
	if code, g := act(toks[0], "start", nil); code != 200 || g.Status != "playing" {
		t.Fatalf("start: %d %+v", code, g)
	}

	ws := e.dial(toks[1])
	ws.waitFor("ready")
	ws.send(map[string]any{"type": "game.watch", "message_id": m.ID})
	if ev := ws.waitFor("game.map"); ev["map"].(map[string]any)["rooms"] == nil {
		t.Fatalf("map: %v", ev)
	}
	f := ws.waitFor("game.frame")["frame"].(map[string]any)
	if f["phase"] != "intro" {
		t.Fatalf("first phase: %v", f["phase"])
	}
	me := f["me"].(map[string]any)
	x, y := me["x"].(float64), me["y"].(float64)
	// trong intro không đi được; chờ sang "play" rồi đi 20 đơn vị
	for f["phase"] != "play" {
		f = ws.waitForTimeout("game.frame", 8*time.Second)["frame"].(map[string]any)
	}
	ws.send(map[string]any{"type": "game.pos", "message_id": m.ID, "data": map[string]float64{"x": x + 20, "y": y}})
	deadline := time.Now().Add(2 * time.Second)
	for {
		f = ws.waitFor("game.frame")["frame"].(map[string]any)
		if f["me"].(map[string]any)["x"].(float64) == x+20 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("position was not accepted")
		}
	}

	// người không chơi mở màn hình: không nhận gì
	spectator := e.dial(toks[4])
	spectator.waitFor("ready")
	spectator.send(map[string]any{"type": "game.watch", "message_id": m.ID})
	if ev, ok := spectator.tryWait("game.map", 500*time.Millisecond); ok {
		t.Fatalf("spectator got map: %v", ev)
	}
	_ = ids
}
