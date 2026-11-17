package controller_test

import (
	"testing"
)

func TestStickersAndGifs(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "vui"}, &ch)
	wa := e.dial(alice.Token)
	defer wa.conn.CloseNow()

	// sticker hợp lệ: nội dung chữ bị bỏ, embed được lưu
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "client_id": "s1", "content": "kèm chữ",
		"embed": map[string]any{"type": "sticker", "id": "text/oke", "url": "https://evil.example/x.png"}})
	m := wa.waitFor("message.new")["message"].(map[string]any)
	emb := m["embed"].(map[string]any)
	if m["content"] != "" || emb["type"] != "sticker" || emb["id"] != "text/oke" || emb["url"] != nil {
		t.Fatalf("sticker message: %v", m)
	}

	// GIF trỏ ra ngoài GIPHY / sticker sai định dạng: bị từ chối
	for _, bad := range []map[string]any{
		{"type": "gif", "id": "abc", "url": "https://evil.example/a.gif", "width": 100, "height": 100},
		{"type": "gif", "id": "abc", "url": "http://media.giphy.com/a.gif", "width": 100, "height": 100},
		{"type": "gif", "id": "abc", "url": "https://media.giphy.com.evil.io/a.gif", "width": 100, "height": 100},
		{"type": "sticker", "id": "../../etc"},
		{"type": "html", "id": "x"},
	} {
		wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "client_id": "bad", "embed": bad})
		if ev := wa.waitFor("error"); ev["client_id"] != "bad" {
			t.Fatalf("bad embed %v accepted: %v", bad, ev)
		}
	}

	// GIF hợp lệ
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "client_id": "g1",
		"embed": map[string]any{"type": "gif", "id": "xT9IgG50Fb7Mi0prBC", "url": "https://media2.giphy.com/media/x/200.gif?cid=1",
			"width": 267, "height": 200, "title": "Mèo vẫy tay"}})
	g := wa.waitFor("message.new")["message"].(map[string]any)
	if g["embed"].(map[string]any)["width"].(float64) != 267 {
		t.Fatalf("gif message: %v", g)
	}

	// không sửa được sticker/GIF; lịch sử trả về embed
	if code := e.do("PATCH", "/api/messages/"+itoa(int64(g["id"].(float64))), alice.Token, map[string]any{"content": "x"}, nil); code != 400 {
		t.Fatalf("edit gif: %d", code)
	}
	var hist struct {
		Messages []map[string]any `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", alice.Token, nil, &hist)
	if len(hist.Messages) != 2 || hist.Messages[0]["embed"] == nil {
		t.Fatalf("history: %v", hist.Messages)
	}
	// xoá thì mất embed
	var del map[string]any
	e.do("DELETE", "/api/messages/"+itoa(int64(m["id"].(float64))), alice.Token, nil, &del)
	if del["embed"] != nil {
		t.Fatalf("deleted message keeps embed: %v", del)
	}

	// chưa cấu hình GIPHY: tìm GIF trả enabled=false
	var gifs map[string]any
	if code := e.do("GET", "/api/gifs?q=meo", alice.Token, nil, &gifs); code != 200 || gifs["enabled"] != false {
		t.Fatalf("gifs disabled: %d %v", code, gifs)
	}
}
