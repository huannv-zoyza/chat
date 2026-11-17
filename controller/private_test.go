package controller_test

import (
	"net/http"
	"testing"
)

type fullChannel struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	IsPrivate   bool   `json:"is_private"`
	InviteCode  string `json:"invite_code"`
	MemberCount int    `json:"member_count"`
}

func TestPrivateChannelsAndInvites(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")

	var pub, priv fullChannel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "chung"}, &pub)
	if code := e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "ban-giam-doc", "is_private": true}, &priv); code != 201 {
		t.Fatalf("create private: %d", code)
	}
	if !priv.IsPrivate || len(priv.InviteCode) != 24 || len(pub.InviteCode) != 24 {
		t.Fatalf("invite codes: pub=%q priv=%+v", pub.InviteCode, priv)
	}
	id := itoa(priv.ID)

	// Người ngoài: không thấy kênh riêng tư ở đâu cả, kể cả khi đoán id.
	var discover []fullChannel
	e.do("GET", "/api/channels/discover", bob.Token, nil, &discover)
	if len(discover) != 1 || discover[0].ID != pub.ID || discover[0].InviteCode != "" {
		t.Fatalf("discover must list only public, without invite code: %+v", discover)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/channels/" + id},
		{"POST", "/api/channels/" + id + "/join"},
		{"GET", "/api/channels/" + id + "/messages"},
		{"GET", "/api/channels/" + id + "/members"},
	} {
		if code := e.do(c.method, c.path, bob.Token, nil, nil); code != http.StatusNotFound {
			t.Fatalf("%s %s by outsider: %d, want 404", c.method, c.path, code)
		}
	}
	// Kênh công khai xem được nhưng không lộ mã mời cho người ngoài.
	var pubView fullChannel
	e.do("GET", "/api/channels/"+itoa(pub.ID), bob.Token, nil, &pubView)
	if pubView.InviteCode != "" {
		t.Fatalf("public channel leaked invite code to non-member")
	}

	// Link mời: xem trước rồi tham gia.
	var preview struct {
		Channel  fullChannel `json:"channel"`
		IsMember bool        `json:"is_member"`
	}
	if code := e.do("GET", "/api/invites/"+priv.InviteCode, bob.Token, nil, &preview); code != 200 {
		t.Fatalf("preview: %d", code)
	}
	if preview.Channel.Name != "ban-giam-doc" || preview.IsMember || preview.Channel.InviteCode != "" {
		t.Fatalf("preview: %+v", preview)
	}
	if code := e.do("GET", "/api/invites/ffffffffffffffffffffffff", bob.Token, nil, nil); code != 404 {
		t.Fatalf("bad invite: %d", code)
	}
	var joined fullChannel
	if code := e.do("POST", "/api/invites/"+priv.InviteCode+"/join", bob.Token, nil, &joined); code != 200 {
		t.Fatalf("join by invite: %d", code)
	}
	if joined.MemberCount != 2 || joined.InviteCode != priv.InviteCode {
		t.Fatalf("joined channel: %+v", joined)
	}
	if code := e.do("GET", "/api/channels/"+id+"/messages", bob.Token, nil, nil); code != 200 {
		t.Fatalf("member reads private history: %d", code)
	}

	// Chỉ chủ kênh tạo được link mới; link cũ hết hiệu lực.
	if code := e.do("POST", "/api/channels/"+id+"/invite", bob.Token, nil, nil); code != 403 {
		t.Fatalf("non-owner regenerate: %d", code)
	}
	var regen fullChannel
	e.do("POST", "/api/channels/"+id+"/invite", alice.Token, nil, &regen)
	if regen.InviteCode == priv.InviteCode || len(regen.InviteCode) != 24 {
		t.Fatalf("regenerate: %+v", regen)
	}
	if code := e.do("POST", "/api/invites/"+priv.InviteCode+"/join", carol.Token, nil, nil); code != 404 {
		t.Fatalf("old invite still works: %d", code)
	}
	if code := e.do("POST", "/api/invites/"+regen.InviteCode+"/join", carol.Token, nil, nil); code != 200 {
		t.Fatalf("new invite: %d", code)
	}

	// Chủ kênh đổi công khai -> riêng tư: biến mất khỏi "Khám phá".
	e.do("PATCH", "/api/channels/"+itoa(pub.ID), alice.Token, map[string]any{"is_private": true}, nil)
	discover = nil
	e.do("GET", "/api/channels/discover", carol.Token, nil, &discover)
	if len(discover) != 0 {
		t.Fatalf("channel switched to private still discoverable: %+v", discover)
	}
}
