package controller_test

import (
	"testing"
	"time"
)

func TestGameVoice(t *testing.T) {
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
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", toks[0], map[string]any{"kind": "werewolf"}, &m)
	act := func(tok, action string) gameResp {
		var g gameResp
		if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", tok, map[string]any{"action": action}, &g); code != 200 {
			t.Fatalf("%s: %d %s", action, code, g.Error)
		}
		return g
	}
	for _, tk := range toks[1:4] {
		act(tk, "join")
	}
	ws := make([]*wsClient, len(toks))
	for i, tk := range toks {
		ws[i] = e.dial(tk)
		ws[i].waitFor("ready")
	}

	// người xem (không chơi) không vào thoại được
	ws[4].send(map[string]any{"type": "gamevoice.join", "message_id": m.ID})
	ws[4].waitFor("gamevoice.error")

	ws[0].send(map[string]any{"type": "gamevoice.join", "message_id": m.ID})
	wel := ws[0].waitFor("gamevoice.welcome")
	anhID := wel["self"].(string)
	if ps := wel["participants"].([]any); len(ps) != 0 {
		t.Fatalf("first joiner sees nobody: %v", wel)
	}
	// người chơi khác thấy có người trong thoại; người xem thì không nhận
	if ps := ws[1].waitFor("gamevoice.state")["participants"].([]any); len(ps) != 1 {
		t.Fatalf("state: %v", ps)
	}
	ws[1].send(map[string]any{"type": "gamevoice.join", "message_id": m.ID})
	wel = ws[1].waitFor("gamevoice.welcome")
	binhID := wel["self"].(string)
	if ps := wel["participants"].([]any); len(ps) != 1 || ps[0].(map[string]any)["peer_id"] != anhID {
		t.Fatalf("binh welcome: %v", wel)
	}
	// người ngoài thoại không gửi tín hiệu vào được: tín hiệu đầu tiên anh nhận là của binh
	ws[4].send(map[string]any{"type": "gamevoice.signal", "message_id": m.ID, "to": anhID, "data": map[string]any{"x": 1}})
	ws[1].send(map[string]any{"type": "gamevoice.signal", "message_id": m.ID, "to": anhID, "data": map[string]any{"sdp": "offer"}})
	if sig := ws[0].waitFor("gamevoice.signal"); sig["from"] != binhID {
		t.Fatalf("signal: %v", sig)
	}

	// bắt đầu ván: bản riêng của người chơi có luật thoại (đêm: chỉ Sói nghe nhau)
	g := act(toks[0], "start")
	if g.Voice == nil {
		t.Fatalf("player view has no voice: %+v", g)
	}
	if g.Me["role"] != "wolf" && len(g.Voice.Talk) != 0 {
		t.Fatalf("non-wolf talks at night: %+v", g.Voice)
	}

	// rời phòng chờ không áp dụng nữa; rời thoại thì người khác được báo
	ws[1].send(map[string]any{"type": "gamevoice.leave", "message_id": m.ID})
	for {
		if ps := ws[0].waitFor("gamevoice.state")["participants"].([]any); len(ps) == 1 {
			break
		}
	}
	// người xem không nhận trạng thái thoại (đọc tới hết giờ sẽ đóng kết nối nên để cuối)
	if _, got := ws[4].tryWait("gamevoice.state", 200*time.Millisecond); got {
		t.Fatal("non-player got voice state")
	}
}
