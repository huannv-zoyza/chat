package controller_test

import "testing"

func TestCallSignaling(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")
	var dm, dmCarol channel
	e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": bob.User.ID}, &dm)
	e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": carol.User.ID}, &dmCarol)

	wa := e.dial(alice.Token)
	defer wa.conn.CloseNow()
	wb1 := e.dial(bob.Token) // bob có 2 thiết bị
	defer wb1.conn.CloseNow()
	wb2 := e.dial(bob.Token)
	defer wb2.conn.CloseNow()

	// gọi người offline -> báo offline + ghi cuộc gọi nhỡ
	wa.send(map[string]any{"type": "call.invite", "call_id": "call-offline-1", "channel_id": dmCarol.ID, "video": false})
	if ev := wa.waitFor("call.ended"); ev["reason"] != "offline" {
		t.Fatalf("offline: %v", ev)
	}
	if m := wa.waitFor("message.new")["message"].(map[string]any); m["embed"].(map[string]any)["id"] != "missed" {
		t.Fatalf("missed log: %v", m)
	}

	// gọi bob: cả 2 thiết bị đổ chuông
	wa.send(map[string]any{"type": "call.invite", "call_id": "call-0000-1", "channel_id": dm.ID, "video": true})
	in1 := wb1.waitFor("call.incoming")
	wb2.waitFor("call.incoming")
	if in1["video"] != true || in1["from"].(map[string]any)["username"] != "alice" {
		t.Fatalf("incoming: %v", in1)
	}
	// carol (không trong cuộc) không chen vào được
	wc := e.dial(carol.Token)
	defer wc.conn.CloseNow()
	wc.send(map[string]any{"type": "call.accept", "call_id": "call-0000-1"})
	if ev := wc.waitFor("call.ended"); ev["reason"] != "gone" {
		t.Fatalf("stranger accept: %v", ev)
	}
	// bận: carol gọi alice lúc alice đang gọi
	wc.send(map[string]any{"type": "call.invite", "call_id": "call-busy-01", "channel_id": dmCarol.ID})
	if ev := wc.waitFor("call.ended"); ev["reason"] != "busy" && ev["reason"] != "invalid" {
		t.Fatalf("busy: %v", ev)
	}

	// bob nghe ở thiết bị 1: thiết bị 2 ngừng đổ chuông, alice nhận accepted
	wb1.send(map[string]any{"type": "call.accept", "call_id": "call-0000-1"})
	wa.waitFor("call.accepted")
	if ev := wb2.waitFor("call.ended"); ev["reason"] != "answered_elsewhere" {
		t.Fatalf("other device: %v", ev)
	}
	// tín hiệu đi đúng 2 chiều
	wa.send(map[string]any{"type": "call.signal", "call_id": "call-0000-1", "data": map[string]any{"sdp": "offer"}})
	if ev := wb1.waitFor("call.signal"); ev["data"].(map[string]any)["sdp"] != "offer" {
		t.Fatalf("signal a->b: %v", ev)
	}
	wb1.send(map[string]any{"type": "call.signal", "call_id": "call-0000-1", "data": map[string]any{"sdp": "answer"}})
	if ev := wa.waitFor("call.signal"); ev["data"].(map[string]any)["sdp"] != "answer" {
		t.Fatalf("signal b->a: %v", ev)
	}
	// alice cúp máy: bob nhận ended, kênh có nhật ký "completed"
	wa.send(map[string]any{"type": "call.hangup", "call_id": "call-0000-1"})
	if ev := wb1.waitFor("call.ended"); ev["reason"] != "hangup" {
		t.Fatalf("hangup: %v", ev)
	}
	wa.waitFor("call.ended") // người cúp máy cũng nhận ended
	for {
		m := wb1.waitFor("message.new")["message"].(map[string]any)
		if emb, ok := m["embed"].(map[string]any); ok && emb["type"] == "call" {
			if emb["id"] != "completed" || emb["video"] != true {
				t.Fatalf("call log: %v", emb)
			}
			break
		}
	}

	// từ chối -> nhật ký "declined"
	wa.send(map[string]any{"type": "call.invite", "call_id": "call-0000-2", "channel_id": dm.ID})
	wb2.waitFor("call.incoming")
	wb2.send(map[string]any{"type": "call.decline", "call_id": "call-0000-2"})
	if ev := wa.waitFor("call.ended"); ev["reason"] != "declined" {
		t.Fatalf("declined: %v", ev)
	}

	// người gọi rớt mạng khi đang đổ chuông -> kết thúc
	wa.send(map[string]any{"type": "call.invite", "call_id": "call-0000-3", "channel_id": dm.ID})
	wb1.waitFor("call.incoming")
	wa.conn.CloseNow()
	for {
		ev := wb1.waitFor("call.ended")
		if ev["call_id"] != "call-0000-3" {
			continue // sự kiện của cuộc gọi trước
		}
		if ev["reason"] != "disconnected" {
			t.Fatalf("caller gone: %v", ev)
		}
		break
	}

	var cfg map[string]any
	if code := e.do("GET", "/api/rtc-config", bob.Token, nil, &cfg); code != 200 || cfg["ice_servers"] == nil {
		t.Fatalf("rtc-config: %d %v", code, cfg)
	}
}
