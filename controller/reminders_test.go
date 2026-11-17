package controller_test

import (
	"testing"
	"time"
)

func TestRemindersAndPollDeadline(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "viec"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	wa := e.dial(alice.Token)
	defer wa.conn.CloseNow()
	wb := e.dial(bob.Token)
	defer wb.conn.CloseNow()

	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "Nộp báo cáo trước 5h", "client_id": "r"})
	msg := wa.waitFor("message.new")["message"].(map[string]any)
	mid := itoa(int64(msg["id"].(float64)))

	// thời gian đã qua / quá xa bị từ chối
	if code := e.do("POST", "/api/messages/"+mid+"/remind", alice.Token, map[string]any{"at": time.Now().Add(-time.Hour)}, nil); code != 400 {
		t.Fatalf("past reminder: %d", code)
	}
	at := time.Now().Add(30 * time.Minute)
	var rem map[string]any
	if code := e.do("POST", "/api/messages/"+mid+"/remind", alice.Token, map[string]any{"at": at, "note": "nhớ gửi sếp"}, &rem); code != 201 {
		t.Fatalf("create reminder: %d %v", code, rem)
	}
	rid := itoa(int64(rem["id"].(float64)))
	var list []map[string]any
	e.do("GET", "/api/reminders", alice.Token, nil, &list)
	if len(list) != 1 || list[0]["message"] == nil || list[0]["fired_at"] != nil {
		t.Fatalf("list: %v", list)
	}
	// bob không thấy, không xoá được nhắc của alice
	list = nil
	e.do("GET", "/api/reminders", bob.Token, nil, &list)
	if len(list) != 0 {
		t.Fatalf("bob sees alice reminders")
	}
	if code := e.do("DELETE", "/api/reminders/"+rid, bob.Token, nil, nil); code != 404 {
		t.Fatalf("bob deletes alice reminder: %d", code)
	}

	// chưa tới giờ: không nhắc; tới giờ: alice nhận reminder.due (bob không)
	e.api.Tick(time.Now())
	e.api.Tick(at.Add(time.Second))
	ev := wa.waitFor("reminder.due")
	if ev["reminder"].(map[string]any)["note"] != "nhớ gửi sếp" {
		t.Fatalf("reminder.due: %v", ev)
	}
	list = nil
	e.do("GET", "/api/reminders", alice.Token, nil, &list)
	if list[0]["fired_at"] == nil {
		t.Fatalf("fired_at not set: %v", list)
	}
	// hoãn 1 giờ: chưa nhắc lại
	if code := e.do("PATCH", "/api/reminders/"+rid, alice.Token, map[string]any{"at": time.Now().Add(time.Hour)}, nil); code != 200 {
		t.Fatalf("snooze: %d", code)
	}
	list = nil // decode vào slice cũ sẽ giữ lại khoá cũ của map
	e.do("GET", "/api/reminders", alice.Token, nil, &list)
	if list[0]["fired_at"] != nil {
		t.Fatalf("snoozed reminder still fired")
	}
	if code := e.do("DELETE", "/api/reminders/"+rid, alice.Token, nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}

	// bình chọn có hạn chót + cho thêm lựa chọn
	closes := time.Now().Add(2 * time.Hour)
	var pm struct {
		ID   int64    `json:"id"`
		Poll pollResp `json:"poll"`
	}
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/polls", alice.Token, map[string]any{
		"question": "Đi đâu?", "options": []string{"Biển", "Núi"}, "closes_at": closes, "allow_add": true,
	}, &pm)
	pid := itoa(pm.ID)
	var p pollResp
	if code := e.do("POST", "/api/messages/"+pid+"/poll/options", bob.Token, map[string]any{"text": "Ở nhà"}, &p); code != 200 || len(p.Options) != 3 {
		t.Fatalf("add option: %d %+v", code, p)
	}
	if code := e.do("POST", "/api/messages/"+pid+"/poll/options", bob.Token, map[string]any{"text": "biển"}, nil); code != 400 {
		t.Fatalf("duplicate option: %d", code)
	}
	// tới hạn chót: tự kết thúc, cả kênh được báo
	e.api.Tick(closes.Add(time.Second))
	for {
		ev := wb.waitFor("poll.updated")
		if ev["poll"].(map[string]any)["closed_at"] != nil {
			break
		}
	}
	if code := e.do("POST", "/api/messages/"+pid+"/votes", bob.Token, map[string]any{"option_ids": []int64{p.Options[0].ID}}, nil); code != 409 {
		t.Fatalf("vote after deadline: %d", code)
	}
	if code := e.do("POST", "/api/messages/"+pid+"/poll/options", bob.Token, map[string]any{"text": "Sông"}, nil); code != 409 {
		t.Fatalf("add option after deadline: %d", code)
	}
	// hạn chót không hợp lệ
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/polls", alice.Token, map[string]any{
		"question": "x", "options": []string{"a", "b"}, "closes_at": time.Now().Add(-time.Hour),
	}, nil); code != 400 {
		t.Fatalf("past deadline: %d", code)
	}
}
