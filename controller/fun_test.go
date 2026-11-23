package controller_test

import (
	"strings"
	"testing"
)

func TestThrowAndNickname(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol") // không ở trong kênh
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "tro-dua"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	base := "/api/channels/" + itoa(ch.ID)
	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	if code := e.do("POST", base+"/throw", alice.Token, map[string]any{"user_id": bob.User.ID, "item": "dep"}, nil); code != 201 {
		t.Fatalf("throw: %d", code)
	}
	ev := wb.waitFor("message.new")
	emb := ev["message"].(map[string]any)["embed"].(map[string]any)
	if emb["type"] != "throw" || emb["id"] != "dep" || emb["target_id"] != float64(bob.User.ID) {
		t.Fatalf("throw message: %v", emb)
	}
	for _, c := range []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"user_id": bob.User.ID, "item": "dep"}, 429},   // ném liên tục
		{map[string]any{"user_id": alice.User.ID, "item": "dep"}, 400}, // tự ném mình
		{map[string]any{"user_id": carol.User.ID, "item": "dep"}, 400}, // người ngoài kênh
		{map[string]any{"user_id": bob.User.ID, "item": "bomb"}, 400},  // món không có
	} {
		if code := e.do("POST", base+"/throw", alice.Token, c.body, nil); code != c.want {
			t.Fatalf("throw %v: %d, want %d", c.body, code, c.want)
		}
	}

	nick := base + "/nicknames/" + itoa(bob.User.ID)
	if code := e.do("PUT", nick, alice.Token, map[string]string{"nickname": "  Thánh   Lag "}, nil); code != 204 {
		t.Fatalf("set nickname: %d", code)
	}
	if ev := wb.waitFor("nickname.updated"); ev["nickname"] != "Thánh Lag" {
		t.Fatalf("nickname.updated: %v", ev)
	}
	if code := e.do("PUT", nick, alice.Token, map[string]string{"nickname": strings.Repeat("a", 33)}, nil); code != 400 {
		t.Fatalf("too long nickname: %d", code)
	}
	if code := e.do("PUT", base+"/nicknames/"+itoa(carol.User.ID), alice.Token, map[string]string{"nickname": "x"}, nil); code != 400 {
		t.Fatalf("nickname for outsider: %d", code)
	}
	var got map[string]string
	e.do("GET", base+"/nicknames", bob.Token, nil, &got)
	if got[itoa(bob.User.ID)] != "Thánh Lag" {
		t.Fatalf("nicknames: %v", got)
	}
	e.do("PUT", nick, bob.Token, map[string]string{"nickname": ""}, nil)
	got = nil
	e.do("GET", base+"/nicknames", bob.Token, nil, &got)
	if len(got) != 0 {
		t.Fatalf("nickname not cleared: %v", got)
	}
	if code := e.do("GET", base+"/nicknames", carol.Token, nil, nil); code != 403 {
		t.Fatalf("outsider reads nicknames: %d", code)
	}
}
