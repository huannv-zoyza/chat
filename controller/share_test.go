package controller_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

type shareEmbed struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Author    string `json:"author"`
	Channel   string `json:"channel"`
	ChannelID int64  `json:"channel_id"`
}

func TestShareMessages(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")
	var pub, secret, other channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "chung"}, &pub)
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "bi-mat", "is_private": true}, &secret)
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "noi-khac"}, &other)
	e.do("POST", "/api/channels/"+itoa(other.ID)+"/join", bob.Token, nil, nil)
	e.do("POST", "/api/channels/"+itoa(pub.ID)+"/join", bob.Token, nil, nil)

	var poll struct{ ID int64 }
	e.do("POST", "/api/channels/"+itoa(pub.ID)+"/polls", alice.Token, map[string]any{
		"question": "Trưa nay ăn gì?", "options": []string{"Phở", "Bún"},
	}, &poll)
	var secretMsg struct{ ID int64 }
	code, out := e.upload(secret.ID, alice.Token, "kế hoạch bí mật", "s1")
	if code != 201 {
		t.Fatalf("upload: %d", code)
	}
	secretMsg.ID = int64(out["id"].(float64))

	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	// 1. chia sẻ bình chọn sang kênh khác: thẻ do máy chủ dựng (bỏ qua chữ client tự điền)
	wa.send(map[string]any{"type": "message.send", "channel_id": other.ID, "client_id": "sh1",
		"embed": map[string]any{"type": "share", "id": fmt.Sprint(poll.ID), "text": "bịa đặt", "author": "Ai đó"}})
	ev := wb.waitFor("message.new")
	var got struct {
		Message struct {
			ID    int64      `json:"id"`
			Embed shareEmbed `json:"embed"`
		} `json:"message"`
	}
	b, _ := json.Marshal(ev)
	json.Unmarshal(b, &got)
	sh := got.Message.Embed
	if sh.Type != "share" || sh.Kind != "poll" || sh.Text != "Trưa nay ăn gì?" || sh.Author != "Alice" ||
		sh.Channel != "chung" || sh.ChannelID != pub.ID || sh.ID != fmt.Sprint(poll.ID) {
		t.Fatalf("share embed: %+v", sh)
	}

	// 2. chia sẻ lại thẻ chia sẻ: vẫn trỏ về tin gốc
	wb.send(map[string]any{"type": "message.send", "channel_id": other.ID, "client_id": "sh2",
		"embed": map[string]any{"type": "share", "id": fmt.Sprint(got.Message.ID)}})
	ev = wa.waitFor("message.new")
	b, _ = json.Marshal(ev)
	json.Unmarshal(b, &got)
	if got.Message.Embed.ID != fmt.Sprint(poll.ID) || got.Message.Embed.Kind != "poll" {
		t.Fatalf("reshare should point to original: %+v", got.Message.Embed)
	}

	// 3. không xem được tin gốc (kênh riêng tư chưa vào) thì không chia sẻ được
	wb.send(map[string]any{"type": "message.send", "channel_id": other.ID, "client_id": "sh3",
		"embed": map[string]any{"type": "share", "id": fmt.Sprint(secretMsg.ID)}})
	if ev := wb.waitFor("error"); ev["client_id"] != "sh3" {
		t.Fatalf("expected error for sh3: %v", ev)
	}

	// 4. tin nhắn riêng chỉ chia sẻ được dạng bản sao
	var dm struct{ ID int64 }
	e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": bob.User.ID}, &dm)
	code, out = e.upload(dm.ID, alice.Token, "chuyện riêng", "d1")
	if code != 201 {
		t.Fatalf("dm upload: %d", code)
	}
	wa.send(map[string]any{"type": "message.send", "channel_id": other.ID, "client_id": "sh4",
		"embed": map[string]any{"type": "share", "id": fmt.Sprint(int64(out["id"].(float64)))}})
	if ev := wa.waitFor("error"); ev["client_id"] != "sh4" {
		t.Fatalf("expected error for sh4: %v", ev)
	}

	// 5. /link: thành viên thấy đủ; kênh công khai chưa vào thì chỉ thấy tên kênh; kênh riêng tư thì bị chặn
	type link struct {
		Channel struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"channel"`
		Member  bool `json:"member"`
		Message *struct {
			Poll *struct{ Question string } `json:"poll"`
		} `json:"message"`
		Error string `json:"error"`
	}
	var l link
	if code := e.do("GET", "/api/messages/"+itoa(poll.ID)+"/link", bob.Token, nil, &l); code != 200 || !l.Member ||
		l.Message == nil || l.Message.Poll == nil || l.Message.Poll.Question != "Trưa nay ăn gì?" {
		t.Fatalf("member link: %d %+v", code, l)
	}
	l = link{}
	if code := e.do("GET", "/api/messages/"+itoa(poll.ID)+"/link", carol.Token, nil, &l); code != 200 || l.Member ||
		l.Message != nil || l.Channel.Name != "chung" {
		t.Fatalf("public non-member link: %d %+v", code, l)
	}
	if code := e.do("GET", "/api/messages/"+itoa(secretMsg.ID)+"/link", bob.Token, nil, nil); code != 403 {
		t.Fatalf("private link: %d", code)
	}
	if code := e.do("GET", "/api/messages/"+itoa(int64(out["id"].(float64)))+"/link", carol.Token, nil, nil); code != 404 {
		t.Fatalf("dm link for outsider: %d", code)
	}
	if code := e.do("GET", "/api/messages/999999/link", bob.Token, nil, nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
}
