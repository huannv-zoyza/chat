package controller_test

import (
	"net/url"
	"testing"
)

func TestPinsQuotesSearchForward(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")
	var ch, other channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "chung"}, &ch)
	e.do("POST", "/api/channels", carol.Token, map[string]any{"name": "rieng-carol"}, &other)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	wb := e.dial(bob.Token)
	defer wb.conn.CloseNow()

	send := func(w *wsClient, content string, extra map[string]any) map[string]any {
		req := map[string]any{"type": "message.send", "channel_id": ch.ID, "content": content, "client_id": content}
		for k, v := range extra {
			req[k] = v
		}
		w.send(req)
		return w.waitFor("message.new")["message"].(map[string]any)
	}
	m1 := send(wb, "Trưa nay đi ăn phở bò ở Lý Quốc Sư", nil)
	id1 := int64(m1["id"].(float64))

	// trả lời trích dẫn: tin mới kèm bản trích
	m2 := send(wb, "Đồng ý luôn", map[string]any{"reply_to_id": id1})
	q := m2["reply_to"].(map[string]any)
	if int64(m2["reply_to_id"].(float64)) != id1 || q["content"] != m1["content"] {
		t.Fatalf("quote: %v", m2)
	}
	// trả lời tin ở kênh khác bị từ chối
	var cm map[string]any
	e.do("POST", "/api/channels/"+itoa(other.ID)+"/join", bob.Token, nil, nil) // kênh công khai
	wb.send(map[string]any{"type": "message.send", "channel_id": other.ID, "content": "x", "client_id": "bad", "reply_to_id": id1})
	if ev := wb.waitFor("error"); ev["client_id"] != "bad" {
		t.Fatalf("cross-channel quote: %v", ev)
	}
	_ = cm

	// ghim / bỏ ghim
	var pinned map[string]any
	if code := e.do("POST", "/api/messages/"+itoa(id1)+"/pin", alice.Token, map[string]any{"pinned": true}, &pinned); code != 200 || pinned["pinned_at"] == nil {
		t.Fatalf("pin: %d %v", code, pinned)
	}
	if ev := wb.waitFor("message.updated"); ev["message"].(map[string]any)["pinned_by"].(float64) != float64(alice.User.ID) {
		t.Fatalf("pin event: %v", ev)
	}
	var pins []map[string]any
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/pins", bob.Token, nil, &pins)
	if len(pins) != 1 {
		t.Fatalf("pins: %v", pins)
	}
	if code := e.do("GET", "/api/channels/"+itoa(ch.ID)+"/pins", carol.Token, nil, nil); code != 403 && code != 404 {
		t.Fatalf("non-member pins: %d", code)
	}

	// tìm kiếm: không dấu, chữ đ, tiền tố; không thấy kênh mình không ở
	type sr struct {
		Messages []map[string]any `json:"messages"`
	}
	for q, want := range map[string]int{"pho bo": 1, "dong y": 1, "Đồng": 1, "ly quoc": 1, "phở": 1, "bánh mì": 0} {
		var r sr
		e.do("GET", "/api/search?q="+urlq(q), bob.Token, nil, &r)
		if len(r.Messages) != want {
			t.Fatalf("search %q: got %d", q, len(r.Messages))
		}
	}
	var none sr
	e.do("GET", "/api/search?q=pho", carol.Token, nil, &none)
	if len(none.Messages) != 0 {
		t.Fatalf("search leaks other channels: %v", none.Messages)
	}
	// sửa / xoá cập nhật chỉ mục
	e.do("PATCH", "/api/messages/"+itoa(id1), bob.Token, map[string]any{"content": "Trưa nay ăn bún chả"}, nil)
	var r1, r2 sr
	e.do("GET", "/api/search?q=pho", bob.Token, nil, &r1)
	e.do("GET", "/api/search?q=bun+cha", bob.Token, nil, &r2)
	if len(r1.Messages) != 0 || len(r2.Messages) != 1 {
		t.Fatalf("index after edit: %d %d", len(r1.Messages), len(r2.Messages))
	}
	e.do("DELETE", "/api/messages/"+itoa(id1), bob.Token, nil, nil)
	var r3 sr
	e.do("GET", "/api/search?q=bun", bob.Token, nil, &r3)
	if len(r3.Messages) != 0 {
		t.Fatalf("deleted message still searchable")
	}
	// chuyển tiếp
	m3 := send(wb, "Tin chuyển tiếp", map[string]any{"forwarded": true})
	if m3["forwarded"] != true {
		t.Fatalf("forwarded flag: %v", m3)
	}
}

func urlq(s string) string {
	return url.QueryEscape(s)
}
