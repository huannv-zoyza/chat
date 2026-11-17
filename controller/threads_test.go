package controller_test

import (
	"testing"
)

type reaction struct {
	Emoji   string  `json:"emoji"`
	UserIDs []int64 `json:"user_ids"`
}

func TestReactions(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	outsider := e.register("carol", "Carol")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "chung"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)

	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	wb := e.dial(bob.Token)
	wb.waitFor("ready")
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "Chốt lịch họp 3h nhé", "client_id": "m1"})
	msgID := int64(wa.waitFor("message.new")["message"].(map[string]any)["id"].(float64))
	wb.waitFor("message.new")
	path := "/api/messages/" + itoa(msgID) + "/reactions"

	var res struct {
		Reactions []reaction `json:"reactions"`
	}
	if code := e.do("POST", path, bob.Token, map[string]string{"emoji": "👍"}, &res); code != 200 {
		t.Fatalf("react: %d", code)
	}
	e.do("POST", path, alice.Token, map[string]string{"emoji": "👍"}, nil)
	e.do("POST", path, alice.Token, map[string]string{"emoji": "❤️"}, &res)
	if len(res.Reactions) != 2 || res.Reactions[0].Emoji != "👍" || len(res.Reactions[0].UserIDs) != 2 || res.Reactions[1].Emoji != "❤️" {
		t.Fatalf("reactions: %+v", res.Reactions)
	}
	// người khác trong kênh nhận reaction.updated
	ev := wb.waitFor("reaction.updated")
	if int64(ev["message_id"].(float64)) != msgID {
		t.Fatalf("event: %v", ev)
	}

	// bấm lại thì bỏ
	e.do("POST", path, bob.Token, map[string]string{"emoji": "👍"}, &res)
	if len(res.Reactions[0].UserIDs) != 1 || res.Reactions[0].UserIDs[0] != alice.User.ID {
		t.Fatalf("toggle off: %+v", res.Reactions)
	}

	// emoji ghép (ZWJ, màu da) hợp lệ; chữ thường thì không
	if code := e.do("POST", path, bob.Token, map[string]string{"emoji": "👍🏽"}, nil); code != 200 {
		t.Fatalf("skin tone emoji: %d", code)
	}
	if code := e.do("POST", path, bob.Token, map[string]string{"emoji": "👨‍💻"}, nil); code != 200 {
		t.Fatalf("zwj emoji: %d", code)
	}
	for _, bad := range []string{"", "ok", "<b>", "👍 👍", "1"} {
		if code := e.do("POST", path, bob.Token, map[string]string{"emoji": bad}, nil); code != 400 {
			t.Fatalf("emoji %q: %d, want 400", bad, code)
		}
	}
	// người ngoài kênh: 404
	if code := e.do("POST", path, outsider.Token, map[string]string{"emoji": "👍"}, nil); code != 404 {
		t.Fatalf("outsider: %d", code)
	}

	// lịch sử có reactions
	var hist struct {
		Messages []struct {
			Reactions []reaction `json:"reactions"`
		} `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", bob.Token, nil, &hist)
	if len(hist.Messages) != 1 || len(hist.Messages[0].Reactions) != 4 {
		t.Fatalf("history reactions: %+v", hist.Messages)
	}
}

func TestThreads(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch, other channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "chung"}, &ch)
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "khac"}, &other)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)

	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	send := func(w *wsClient, content string, parent int64) map[string]any {
		w.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": content, "client_id": "x", "parent_id": parent})
		return w.waitFor("message.new")["message"].(map[string]any)
	}
	root := send(wa, "Ai review PR #12 giúp mình?", 0)
	rootID := int64(root["id"].(float64))
	wb.waitFor("message.new")

	r1 := send(wb, "Để mình xem", rootID)
	if int64(r1["parent_id"].(float64)) != rootID || int64(r1["parent_user_id"].(float64)) != alice.User.ID {
		t.Fatalf("reply fields: %v", r1)
	}
	// cả kênh nhận thread.updated với tóm tắt luồng
	tu := wa.waitFor("thread.updated")
	if tu["thread"].(map[string]any)["reply_count"].(float64) != 1 {
		t.Fatalf("thread.updated: %v", tu)
	}
	send(wa, "Cảm ơn @bob", rootID)
	wb.waitFor("message.new")

	// không trả lời lồng, không trả lời tin ở kênh khác
	wb.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "lồng", "client_id": "n", "parent_id": int64(r1["id"].(float64))})
	if ev := wb.waitFor("error"); ev["client_id"] != "n" {
		t.Fatalf("nested reply: %v", ev)
	}

	// dòng chính của kênh chỉ có tin gốc, kèm tóm tắt luồng
	var hist struct {
		Messages []struct {
			ID     int64 `json:"id"`
			Thread *struct {
				ReplyCount     int     `json:"reply_count"`
				ParticipantIDs []int64 `json:"participant_ids"`
				LastReplyAt    string  `json:"last_reply_at"`
			} `json:"thread"`
		} `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", bob.Token, nil, &hist)
	if len(hist.Messages) != 1 || hist.Messages[0].ID != rootID {
		t.Fatalf("main timeline should hide replies: %+v", hist.Messages)
	}
	th := hist.Messages[0].Thread
	if th == nil || th.ReplyCount != 2 || len(th.ParticipantIDs) != 2 || th.ParticipantIDs[0] != alice.User.ID || th.LastReplyAt == "" {
		t.Fatalf("thread summary: %+v", th)
	}

	// mở luồng
	var thread struct {
		Parent  struct{ ID int64 } `json:"parent"`
		Replies []struct {
			Content string `json:"content"`
		} `json:"replies"`
	}
	if code := e.do("GET", "/api/messages/"+itoa(rootID)+"/thread", bob.Token, nil, &thread); code != 200 {
		t.Fatalf("get thread: %d", code)
	}
	if thread.Parent.ID != rootID || len(thread.Replies) != 2 || thread.Replies[1].Content != "Cảm ơn @bob" {
		t.Fatalf("thread: %+v", thread)
	}

	// số chưa đọc: trả lời không tính, nhưng trả lời có @tên mình thì tính vào mention
	var list []map[string]any
	e.do("GET", "/api/channels", bob.Token, nil, &list)
	for _, c := range list {
		if int64(c["id"].(float64)) == ch.ID {
			if c["unread_count"].(float64) != 1 || c["mention_count"].(float64) != 1 {
				t.Fatalf("bob unread=%v mention=%v, want 1 and 1", c["unread_count"], c["mention_count"])
			}
		}
	}
}
