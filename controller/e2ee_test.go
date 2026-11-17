package controller_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func b64(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func envelope() string {
	b, _ := json.Marshal(map[string]any{"v": 1, "s": b64(32), "r": b64(32), "n": b64(24), "c": b64(64)})
	return string(b)
}

func TestE2EEKeysAndCipherMessages(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	wb := e.dial(bob.Token)
	defer wb.conn.CloseNow()

	// chưa có khoá
	if code := e.do("GET", "/api/keys/me", alice.Token, nil, nil); code != 404 {
		t.Fatalf("no key yet: %d", code)
	}
	pub := b64(32)
	backup := `{"v":1,"kdf":"argon2id13","salt":"x","nonce":"y","ct":"z"}`
	if code := e.do("PUT", "/api/keys/me", alice.Token, map[string]any{"public_key": pub, "backup": backup}, nil); code != 200 {
		t.Fatalf("put key: %d", code)
	}
	if ev := wb.waitFor("key.updated"); ev["public_key"] != pub {
		t.Fatalf("key.updated: %v", ev)
	}
	// người khác chỉ lấy được khoá công khai, không lấy được bản sao lưu
	var k map[string]any
	e.do("GET", "/api/users/"+itoa(alice.User.ID)+"/key", bob.Token, nil, &k)
	if k["public_key"] != pub || k["backup"] != nil {
		t.Fatalf("public key lookup leaks backup: %v", k)
	}
	// khoá sai định dạng / thay khoá mà không xác nhận
	if code := e.do("PUT", "/api/keys/me", alice.Token, map[string]any{"public_key": "abc", "backup": backup}, nil); code != 400 {
		t.Fatalf("bad key: %d", code)
	}
	if code := e.do("PUT", "/api/keys/me", alice.Token, map[string]any{"public_key": b64(32), "backup": backup}, nil); code != 409 {
		t.Fatalf("replace without confirm: %d", code)
	}
	// đổi PIN (cùng khoá công khai) được
	if code := e.do("PUT", "/api/keys/me", alice.Token, map[string]any{"public_key": pub, "backup": `{"v":1}`}, nil); code != 200 {
		t.Fatalf("change pin: %d", code)
	}

	// tin mã hoá trong tin nhắn riêng
	var dm channel
	e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": bob.User.ID}, &dm)
	wb.waitFor("channel.joined")
	env := envelope()
	wb.send(map[string]any{"type": "message.send", "channel_id": dm.ID, "client_id": "e1", "content": "lộ ra ngoài", "cipher": env})
	m := wb.waitFor("message.new")["message"].(map[string]any)
	if m["cipher"] != env || m["content"] != "" {
		t.Fatalf("cipher message: %v", m)
	}
	// sửa: phải là bản mã mới
	id := itoa(int64(m["id"].(float64)))
	if code := e.do("PATCH", "/api/messages/"+id, bob.Token, map[string]any{"content": "chữ thường"}, nil); code != 400 {
		t.Fatalf("plain edit of cipher msg: %d", code)
	}
	env2 := envelope()
	var edited map[string]any
	if code := e.do("PATCH", "/api/messages/"+id, bob.Token, map[string]any{"cipher": env2}, &edited); code != 200 || edited["cipher"] != env2 {
		t.Fatalf("cipher edit: %d %v", code, edited)
	}
	// vỏ sai định dạng bị từ chối
	wb.send(map[string]any{"type": "message.send", "channel_id": dm.ID, "client_id": "bad", "cipher": `{"v":1,"s":"x"}`})
	if ev := wb.waitFor("error"); ev["client_id"] != "bad" {
		t.Fatalf("bad envelope: %v", ev)
	}
	// kênh nhóm chưa hỗ trợ mã hoá đầu cuối
	var ch channel
	e.do("POST", "/api/channels", bob.Token, map[string]any{"name": "nhom"}, &ch)
	wb.waitFor("channel.joined")
	wb.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "client_id": "grp", "cipher": envelope()})
	if ev := wb.waitFor("error"); ev["client_id"] != "grp" {
		t.Fatalf("cipher in group channel: %v", ev)
	}
	// xoá: mất bản mã
	var del map[string]any
	e.do("DELETE", "/api/messages/"+id, bob.Token, nil, &del)
	if del["cipher"] != nil {
		t.Fatalf("deleted keeps cipher: %v", del)
	}
}
