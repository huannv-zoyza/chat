package controller_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditAndDeleteMessages(t *testing.T) {
	e, dir := newEnvWithUploads(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")

	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "chung"}, &ch)
	for _, tk := range []string{bob.Token, carol.Token} {
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", tk, nil, nil)
	}
	wa := e.dial(alice.Token)
	defer wa.conn.CloseNow()

	_, bobMsg := e.upload(ch.ID, bob.Token, "Chào mọi người", "c1", upFile{"a.png", pngBytes(4, 4)})
	bobID := itoa(int64(bobMsg["id"].(float64)))
	wa.waitFor("message.new")

	// người khác (không phải chủ kênh) không sửa/xoá được
	if code := e.do("PATCH", "/api/messages/"+bobID, carol.Token, map[string]any{"content": "hack"}, nil); code != 403 {
		t.Fatalf("carol edit bob: %d", code)
	}
	if code := e.do("DELETE", "/api/messages/"+bobID, carol.Token, nil, nil); code != 403 {
		t.Fatalf("carol delete bob: %d", code)
	}

	// bob sửa tin, thêm nhắc tên @carol
	var edited map[string]any
	if code := e.do("PATCH", "/api/messages/"+bobID, bob.Token, map[string]any{"content": "Chào @carol nhé"}, &edited); code != 200 {
		t.Fatalf("edit: %d %v", code, edited)
	}
	if edited["content"] != "Chào @carol nhé" || edited["edited_at"] == nil || len(edited["mentions"].([]any)) != 1 {
		t.Fatalf("edited: %v", edited)
	}
	if ev := wa.waitFor("message.updated"); ev["message"].(map[string]any)["content"] != "Chào @carol nhé" {
		t.Fatalf("message.updated: %v", ev)
	}

	// chủ kênh (alice) xoá được tin của bob; file đính kèm bị xoá khỏi đĩa
	key := filepath.Base(edited["attachments"].([]any)[0].(map[string]any)["url"].(string))
	var deleted map[string]any
	if code := e.do("DELETE", "/api/messages/"+bobID, alice.Token, nil, &deleted); code != 200 {
		t.Fatalf("owner delete: %d", code)
	}
	if deleted["deleted_at"] == nil || deleted["content"] != "" || len(deleted["attachments"].([]any)) != 0 {
		t.Fatalf("deleted: %v", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, key)); !os.IsNotExist(err) {
		t.Fatalf("attachment file still on disk: %v", err)
	}
	if ev := wa.waitFor("message.updated"); ev["message"].(map[string]any)["deleted_at"] == nil {
		t.Fatalf("delete event: %v", ev)
	}
	// tin đã xoá thì không sửa được, và không tính là chưa đọc
	if code := e.do("PATCH", "/api/messages/"+bobID, bob.Token, map[string]any{"content": "x"}, nil); code != 400 {
		t.Fatalf("edit deleted: %d", code)
	}
	var list []channel
	e.do("GET", "/api/channels", carol.Token, nil, &list)
	if list[0].UnreadCount != 0 {
		t.Fatalf("deleted message still unread: %d", list[0].UnreadCount)
	}
}

func TestDirectMessages(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob Trần")
	carol := e.register("carol", "Carol")
	wb := e.dial(bob.Token)
	defer wb.conn.CloseNow()

	// tìm người
	var found []map[string]any
	e.do("GET", "/api/users?q=tr%E1%BA%A7n", alice.Token, nil, &found)
	if len(found) != 1 || found[0]["username"] != "bob" {
		t.Fatalf("search: %v", found)
	}

	type dm struct {
		ID        int64            `json:"id"`
		Kind      string           `json:"kind"`
		IsPrivate bool             `json:"is_private"`
		DMMembers []map[string]any `json:"dm_members"`
	}
	var d dm
	if code := e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": bob.User.ID}, &d); code != 201 {
		t.Fatalf("open dm: %d", code)
	}
	if d.Kind != "dm" || !d.IsPrivate || len(d.DMMembers) != 2 {
		t.Fatalf("dm: %+v", d)
	}
	// bob được báo có cuộc trò chuyện mới; mở lại (từ phía bob) ra đúng cuộc cũ
	if ev := wb.waitFor("channel.joined"); ev["channel"].(map[string]any)["kind"] != "dm" {
		t.Fatalf("channel.joined: %v", ev)
	}
	var again dm
	if code := e.do("POST", "/api/dms", bob.Token, map[string]any{"user_id": alice.User.ID}, &again); code != 200 || again.ID != d.ID {
		t.Fatalf("reopen dm: %d %+v", code, again)
	}

	// nhắn và nhận realtime
	wb.send(map[string]any{"type": "message.send", "channel_id": d.ID, "content": "Chào Alice", "client_id": "x1"})
	if ev := wb.waitFor("message.new"); ev["message"].(map[string]any)["channel_id"].(float64) != float64(d.ID) {
		t.Fatalf("dm message: %v", ev)
	}

	// người thứ ba không thấy, không vào được, không đọc được
	if code := e.do("GET", "/api/channels/"+itoa(d.ID)+"/messages", carol.Token, nil, nil); code != 404 {
		t.Fatalf("carol reads dm: %d", code)
	}
	if code := e.do("POST", "/api/channels/"+itoa(d.ID)+"/join", carol.Token, nil, nil); code != 404 {
		t.Fatalf("carol joins dm: %d", code)
	}
	var disc []channel
	e.do("GET", "/api/channels/discover", carol.Token, nil, &disc)
	if len(disc) != 0 {
		t.Fatalf("dm listed in discover: %+v", disc)
	}

	// không đổi tên, không lấy link mời, không rời, không xoá được cuộc trò chuyện riêng
	id := "/api/channels/" + itoa(d.ID)
	for _, c := range []struct{ method, path string }{
		{"PATCH", id}, {"POST", id + "/invite"}, {"POST", id + "/leave"}, {"DELETE", id},
	} {
		if code := e.do(c.method, c.path, alice.Token, map[string]any{"name": "x"}, nil); code != 400 {
			t.Fatalf("%s %s on dm: %d", c.method, c.path, code)
		}
	}

	// tự nhắn cho mình: một thành viên
	var self dm
	if code := e.do("POST", "/api/dms", carol.Token, map[string]any{"user_id": carol.User.ID}, &self); code != 201 || len(self.DMMembers) != 1 {
		t.Fatalf("self dm: %d %+v", code, self)
	}
	if code := e.do("POST", "/api/dms", carol.Token, map[string]any{"user_id": 9999}, nil); code != 404 {
		t.Fatalf("dm unknown user: %d", code)
	}
}
