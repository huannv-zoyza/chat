package controller_test

import "testing"

type pollResp struct {
	Question  string  `json:"question"`
	Multiple  bool    `json:"multiple"`
	Anonymous bool    `json:"anonymous"`
	ClosedAt  *string `json:"closed_at"`
	Voters    int     `json:"voters"`
	MyVotes   []int64 `json:"my_votes"`
	Options   []struct {
		ID       int64   `json:"id"`
		Text     string  `json:"text"`
		Votes    int     `json:"votes"`
		VoterIDs []int64 `json:"voter_ids"`
	} `json:"options"`
	Error string `json:"error"`
}

func TestPolls(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "an-trua"}, &ch)
	for _, tk := range []string{bob.Token, carol.Token} {
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", tk, nil, nil)
	}
	wc := e.dial(carol.Token)
	defer wc.conn.CloseNow()
	path := "/api/channels/" + itoa(ch.ID) + "/polls"

	// không hợp lệ: 1 lựa chọn, trùng lựa chọn, câu hỏi trống
	for _, bad := range []map[string]any{
		{"question": "Ăn gì?", "options": []string{"Phở"}},
		{"question": "Ăn gì?", "options": []string{"Phở", " phở "}},
		{"question": "  ", "options": []string{"Phở", "Bún"}},
	} {
		if code := e.do("POST", path, alice.Token, bad, nil); code != 400 {
			t.Fatalf("bad poll %v: %d", bad, code)
		}
	}

	var m struct {
		ID   int64    `json:"id"`
		Poll pollResp `json:"poll"`
	}
	if code := e.do("POST", path, alice.Token, map[string]any{
		"question": "Trưa nay ăn gì?", "options": []string{"Phở", "Bún chả", "Cơm tấm", ""}, "client_id": "p1",
	}, &m); code != 201 || len(m.Poll.Options) != 3 {
		t.Fatalf("create poll: %d %+v", code, m)
	}
	if ev := wc.waitFor("message.new"); ev["message"].(map[string]any)["poll"] == nil {
		t.Fatalf("message.new without poll: %v", ev)
	}
	opt := func(i int) int64 { return m.Poll.Options[i].ID }
	vote := func(tok string, ids ...int64) (int, pollResp) {
		var p pollResp
		if ids == nil {
			ids = []int64{}
		}
		code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/votes", tok, map[string]any{"option_ids": ids}, &p)
		return code, p
	}

	// một đáp án: chọn 2 cái bị từ chối; đổi phiếu được
	if code, _ := vote(bob.Token, opt(0), opt(1)); code != 400 {
		t.Fatalf("multi vote on single poll: %d", code)
	}
	vote(bob.Token, opt(0))
	code, p := vote(bob.Token, opt(1))
	if code != 200 || p.Options[0].Votes != 0 || p.Options[1].Votes != 1 || len(p.MyVotes) != 1 || p.MyVotes[0] != opt(1) {
		t.Fatalf("change vote: %d %+v", code, p)
	}
	_, p = vote(carol.Token, opt(1))
	if p.Voters != 2 || len(p.Options[1].VoterIDs) != 2 {
		t.Fatalf("voters: %+v", p)
	}
	// sự kiện cho cả kênh không kèm my_votes
	ev := wc.waitFor("poll.updated")
	if _, has := ev["poll"].(map[string]any)["my_votes"]; has {
		t.Fatalf("poll.updated leaks my_votes: %v", ev)
	}
	// rút phiếu
	if _, p = vote(carol.Token); p.Options[1].Votes != 1 || len(p.MyVotes) != 0 {
		t.Fatalf("retract: %+v", p)
	}
	// lịch sử trả my_votes của người xem
	var hist struct {
		Messages []struct {
			Poll pollResp `json:"poll"`
		} `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", bob.Token, nil, &hist)
	if len(hist.Messages[0].Poll.MyVotes) != 1 {
		t.Fatalf("history my_votes: %+v", hist.Messages[0].Poll)
	}
	// chỉ người tạo mới kết thúc được; kết thúc rồi thì không bỏ phiếu được
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/poll/close", bob.Token, map[string]any{"closed": true}, nil); code != 403 {
		t.Fatalf("non-owner close: %d", code)
	}
	var closed pollResp
	e.do("POST", "/api/messages/"+itoa(m.ID)+"/poll/close", alice.Token, map[string]any{"closed": true}, &closed)
	if closed.ClosedAt == nil {
		t.Fatalf("close: %+v", closed)
	}
	if code, _ := vote(carol.Token, opt(2)); code != 409 {
		t.Fatalf("vote on closed poll: %d", code)
	}
	// không sửa được bình chọn
	if code := e.do("PATCH", "/api/messages/"+itoa(m.ID), alice.Token, map[string]any{"content": "x"}, nil); code != 400 {
		t.Fatalf("edit poll: %d", code)
	}

	// ẩn danh: không lộ ai chọn gì, nhưng người chọn vẫn biết mình chọn gì
	var anon struct {
		ID   int64    `json:"id"`
		Poll pollResp `json:"poll"`
	}
	e.do("POST", path, alice.Token, map[string]any{
		"question": "Đánh giá sếp?", "options": []string{"Tốt", "Ổn", "Cần cải thiện"}, "anonymous": true, "multiple": true,
	}, &anon)
	var ap pollResp
	e.do("POST", "/api/messages/"+itoa(anon.ID)+"/votes", bob.Token, map[string]any{"option_ids": []int64{anon.Poll.Options[0].ID, anon.Poll.Options[2].ID}}, &ap)
	if ap.Options[0].Votes != 1 || ap.Options[0].VoterIDs != nil || len(ap.MyVotes) != 2 || ap.Voters != 1 {
		t.Fatalf("anonymous multi vote: %+v", ap)
	}
	var thread struct {
		Parent struct {
			Poll pollResp `json:"poll"`
		} `json:"parent"`
	}
	e.do("GET", "/api/messages/"+itoa(anon.ID)+"/thread", carol.Token, nil, &thread)
	if thread.Parent.Poll.Options[0].VoterIDs != nil || len(thread.Parent.Poll.MyVotes) != 0 {
		t.Fatalf("anonymous leak via thread: %+v", thread.Parent.Poll)
	}

	// tin nhắn riêng không có bình chọn
	var dm channel
	e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": bob.User.ID}, &dm)
	if code := e.do("POST", "/api/channels/"+itoa(dm.ID)+"/polls", alice.Token, map[string]any{"question": "x", "options": []string{"a", "b"}}, nil); code != 400 {
		t.Fatalf("poll in dm: %d", code)
	}
	// xoá tin thì mất bình chọn
	var del map[string]any
	e.do("DELETE", "/api/messages/"+itoa(m.ID), alice.Token, nil, &del)
	if del["poll"] != nil {
		t.Fatalf("deleted message keeps poll: %v", del)
	}
}
