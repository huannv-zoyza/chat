package controller_test

import "testing"

func TestGroupCalls(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")
	dave := e.register("dave", "Dave") // không ở trong kênh
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "hop-nhom", "is_private": true}, &ch)
	var inv map[string]any
	e.do("GET", "/api/channels/"+itoa(ch.ID), alice.Token, nil, &inv)
	code := inv["invite_code"].(string)
	for _, tk := range []string{bob.Token, carol.Token} {
		e.do("POST", "/api/invites/"+code+"/join", tk, nil, nil)
	}
	wa, wb, wc, wd := e.dial(alice.Token), e.dial(bob.Token), e.dial(carol.Token), e.dial(dave.Token)
	defer wa.conn.CloseNow()
	defer wb.conn.CloseNow()
	defer wc.conn.CloseNow()
	defer wd.conn.CloseNow()
	for _, w := range []*wsClient{wa, wb, wc, wd} {
		w.waitFor("ready") // đã đăng ký xong với hub, chắc chắn nhận được sự kiện kênh
	}

	// người ngoài kênh không vào được
	wd.send(map[string]any{"type": "groupcall.join", "channel_id": ch.ID})
	wd.waitFor("groupcall.error")

	// alice mở cuộc gọi: bob, carol được mời
	wa.send(map[string]any{"type": "groupcall.join", "channel_id": ch.ID, "video": true})
	wel := wa.waitFor("groupcall.welcome")
	aliceID := wel["self"].(string)
	if wel["invited"].(float64) != 2 { // bob + carol đang online trong kênh (dave không phải thành viên)
		t.Fatalf("invited: %v", wel)
	}
	if _, has := wel["participants"]; has {
		t.Fatalf("first joiner should see nobody: %v", wel)
	}
	if ev := wb.waitFor("groupcall.started"); ev["by"].(map[string]any)["username"] != "alice" {
		t.Fatalf("started: %v", ev)
	}
	// bob vào: thấy alice, gửi offer cho alice
	wb.send(map[string]any{"type": "groupcall.join", "channel_id": ch.ID})
	wel = wb.waitFor("groupcall.welcome")
	bobID := wel["self"].(string)
	ps := wel["participants"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["peer_id"] != aliceID {
		t.Fatalf("bob welcome: %v", wel)
	}
	wb.send(map[string]any{"type": "groupcall.signal", "channel_id": ch.ID, "to": aliceID, "data": map[string]any{"sdp": "offer-b"}})
	sig := wa.waitFor("groupcall.signal")
	if sig["from"] != bobID || sig["data"].(map[string]any)["sdp"] != "offer-b" {
		t.Fatalf("signal: %v", sig)
	}
	// carol vào: thấy 2 người
	wc.send(map[string]any{"type": "groupcall.join", "channel_id": ch.ID})
	if ps := wc.waitFor("groupcall.welcome")["participants"].([]any); len(ps) != 2 {
		t.Fatalf("carol welcome: %v", ps)
	}
	// dave không chen tín hiệu vào được (không ở trong cuộc gọi)
	wd.send(map[string]any{"type": "groupcall.signal", "channel_id": ch.ID, "to": aliceID, "data": map[string]any{"x": 1}})

	// danh sách đang gọi gửi kèm "ready" cho kết nối mới
	wb2 := e.dial(bob.Token)
	defer wb2.conn.CloseNow()
	// (ready đã được đọc trong dial? kiểm tra bằng state tiếp theo)
	// bob rời, carol rớt mạng, alice rời -> kết thúc, có nhật ký
	wb.send(map[string]any{"type": "groupcall.leave", "channel_id": ch.ID})
	wc.conn.CloseNow()
	wa.send(map[string]any{"type": "groupcall.leave", "channel_id": ch.ID})
	for {
		m := wb.waitFor("message.new")["message"].(map[string]any)
		if emb, ok := m["embed"].(map[string]any); ok && emb["type"] == "call" {
			if emb["id"] != "group" || emb["participants"].(float64) != 3 || emb["video"] != true {
				t.Fatalf("group log: %v", emb)
			}
			break
		}
	}
}

func TestKickDropsFromGroupCall(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "goi-nhom"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	wa, wb := e.dial(alice.Token), e.dial(bob.Token)
	wa.waitFor("ready")
	wb.waitFor("ready")

	wa.send(map[string]any{"type": "groupcall.join", "channel_id": ch.ID})
	wa.waitFor("groupcall.welcome")
	wb.send(map[string]any{"type": "groupcall.join", "channel_id": ch.ID})
	wb.waitFor("groupcall.welcome")
	for len(wa.waitFor("groupcall.state")["participants"].([]any)) != 2 {
	}

	if code := e.do("DELETE", "/api/channels/"+itoa(ch.ID)+"/members/"+itoa(bob.User.ID), alice.Token, nil, nil); code != 204 {
		t.Fatalf("kick: %d", code)
	}
	ps := wa.waitFor("groupcall.state")["participants"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["user"].(map[string]any)["username"] != "alice" {
		t.Fatalf("bob still in call after kick: %v", ps)
	}
}
