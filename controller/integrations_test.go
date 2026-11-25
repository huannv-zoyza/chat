package controller_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type integration struct {
	ID            int64  `json:"id"`
	BotUserID     int64  `json:"bot_user_id"`
	BotUsername   string `json:"bot_username"`
	BotName       string `json:"bot_name"`
	OutgoingURL   string `json:"outgoing_url"`
	SigningSecret string `json:"signing_secret"`
	HookPath      string `json:"hook_path"`
}

func TestIntegrationsIncomingWebhook(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "deploy"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	base := "/api/channels/" + itoa(ch.ID) + "/integrations"
	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	// chỉ chủ kênh tạo được bot
	if code := e.do("POST", base, bob.Token, map[string]string{"name": "CI"}, nil); code != 403 {
		t.Fatalf("non-owner create: %d", code)
	}
	if code := e.do("POST", base, alice.Token, map[string]string{"name": "  "}, nil); code != 400 {
		t.Fatalf("empty name: %d", code)
	}
	if code := e.do("POST", base, alice.Token, map[string]string{"name": "CI", "outgoing_url": "ftp://x"}, nil); code != 400 {
		t.Fatalf("bad url: %d", code)
	}
	var it integration
	if code := e.do("POST", base, alice.Token, map[string]string{"name": "Đơn hàng Bot"}, &it); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if it.BotUsername != "don_hang_bot" || it.BotName != "Đơn hàng Bot" || !strings.HasPrefix(it.HookPath, "/api/hooks/") || it.SigningSecret == "" {
		t.Fatalf("created: %+v", it)
	}
	if ev := wb.waitFor("member.joined"); ev["user"].(map[string]any)["username"] != "don_hang_bot" {
		t.Fatalf("member.joined: %v", ev)
	}
	// trùng tên thì thêm số
	var it2 integration
	e.do("POST", base, alice.Token, map[string]string{"name": "đơn hàng"}, &it2)
	if it2.BotUsername != "don_hang2_bot" {
		t.Fatalf("second bot username: %q", it2.BotUsername)
	}

	// thành viên khác thấy danh sách nhưng không thấy khoá ký
	var list []integration
	e.do("GET", base, bob.Token, nil, &list)
	if len(list) != 2 || list[0].SigningSecret != "" || list[0].HookPath != "" {
		t.Fatalf("list for member: %+v", list)
	}

	// webhook nhận tin: không cần đăng nhập
	var res map[string]any
	if code := e.do("POST", it.HookPath, "", map[string]any{"text": "Deploy xong ✅", "username": "ignored"}, &res); code != 201 {
		t.Fatalf("hook: %d %v", code, res)
	}
	msg := wb.waitFor("message.new")["message"].(map[string]any)
	if msg["content"] != "Deploy xong ✅" || msg["user_id"] != float64(it.BotUserID) {
		t.Fatalf("hook message: %v", msg)
	}
	if code := e.do("POST", it.HookPath, "", map[string]string{"text": " "}, nil); code != 400 {
		t.Fatalf("empty text: %d", code)
	}
	if code := e.do("POST", "/api/hooks/sai-ma", "", map[string]string{"text": "x"}, nil); code != 404 {
		t.Fatalf("wrong token: %d", code)
	}

	// đổi mã: link cũ hết dùng được
	var rotated integration
	if code := e.do("POST", base+"/"+itoa(it.ID)+"/token", alice.Token, nil, &rotated); code != 200 || rotated.HookPath == it.HookPath {
		t.Fatalf("rotate: %d %+v", code, rotated)
	}
	if code := e.do("POST", it.HookPath, "", map[string]string{"text": "x"}, nil); code != 404 {
		t.Fatalf("old token after rotate: %d", code)
	}
	if code := e.do("POST", rotated.HookPath, "", map[string]string{"text": "x"}, nil); code != 201 {
		t.Fatalf("new token: %d", code)
	}

	// xoá bot: rời kênh, link hết dùng được
	if code := e.do("DELETE", base+"/"+itoa(it.ID), bob.Token, nil, nil); code != 403 {
		t.Fatalf("non-owner delete: %d", code)
	}
	if code := e.do("DELETE", base+"/"+itoa(it.ID), alice.Token, nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code := e.do("POST", rotated.HookPath, "", map[string]string{"text": "x"}, nil); code != 404 {
		t.Fatalf("hook after delete: %d", code)
	}

	// không ai đăng ký được tên đuôi _bot
	var r authResp
	code := e.do("POST", "/api/auth/register", "", map[string]string{
		"username": "fake_bot", "email": "fake@example.com", "password": "matkhau123", "display_name": "Fake",
	}, &r)
	if code != 400 {
		t.Fatalf("register _bot: %d", code)
	}
}

func TestIntegrationsOutgoingReply(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "hoi-dap"}, &ch)
	base := "/api/channels/" + itoa(ch.ID) + "/integrations"

	var (
		mu      sync.Mutex
		gotBody []byte
		gotSig  string
		gotTS   string
	)
	botSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody, gotSig, gotTS = b, r.Header.Get("X-GoChat-Signature"), r.Header.Get("X-GoChat-Timestamp")
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"text": "pong"})
	}))
	t.Cleanup(botSrv.Close)

	var it integration
	if code := e.do("POST", base, alice.Token, map[string]string{"name": "Ping", "outgoing_url": botSrv.URL}, &it); code != 201 {
		t.Fatalf("create: %d", code)
	}
	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "@" + it.BotUsername + " ping", "client_id": "c1"})
	q := wa.waitFor("message.new")["message"].(map[string]any)

	// trả lời của bot: tin mới của bot, trích dẫn câu hỏi
	var reply map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for reply == nil && time.Now().Before(deadline) {
		ev, ok := wa.tryWait("message.new", time.Second)
		if ok {
			if m := ev["message"].(map[string]any); m["user_id"] == float64(it.BotUserID) {
				reply = m
			}
		}
	}
	if reply == nil || reply["content"] != "pong" {
		t.Fatalf("bot reply: %v", reply)
	}
	if rt, _ := reply["reply_to"].(map[string]any); rt == nil || rt["id"] != q["id"] {
		t.Fatalf("reply_to: %v", reply["reply_to"])
	}

	mu.Lock()
	defer mu.Unlock()
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(gotBody, &ev); err != nil || ev.Type != "mention" || ev.User.Username != "alice" || !strings.Contains(ev.Message.Text, "ping") {
		t.Fatalf("event body: %s (%v)", gotBody, err)
	}
	mac := hmac.New(sha256.New, []byte(it.SigningSecret))
	mac.Write([]byte(gotTS + "."))
	mac.Write(gotBody)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); gotSig != want {
		t.Fatalf("signature %q, want %q", gotSig, want)
	}
}
