package controller_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// registerFrom tạo tài khoản như đi qua Cloudflare Tunnel từ IP cfIP ("" = gọi thẳng từ máy chủ).
func (e *env) registerFrom(username, cfIP string) (int, string) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]string{
		"username": username, "email": username + "@example.com", "password": "matkhau123",
	})
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cfIP != "" {
		req.Header.Set("CF-Connecting-IP", cfIP)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var r authResp
	json.NewDecoder(res.Body).Decode(&r)
	return res.StatusCode, r.Error
}

// Cả công ty ra Internet qua cùng một IP: đăng ký từ một IP bao nhiêu tài khoản cũng được.
func TestSignupNotLimitedPerIP(t *testing.T) {
	e := newEnv(t)
	for i := range 6 {
		u := "staff" + itoa(int64(i))
		if code, msg := e.registerFrom(u, "203.0.113.7"); code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", u, code, msg)
		}
	}
}

func TestNewAccountCannotMoveCoins(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "cuoc"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)

	// Bàn cược mở trước khi bật luật (như chủ bàn là tài khoản cũ).
	var table gameMsg
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{
		"kind": "xiangqi", "options": map[string]any{"bet_amount": 500, "first": "host"},
	}, &table); code != http.StatusCreated {
		t.Fatalf("create bet table: %d", code)
	}

	e.api.SetNewAccountHold(7 * 24 * time.Hour)

	var errResp struct {
		Error string `json:"error"`
	}
	if code := e.do("POST", "/api/wallet/transfer", bob.Token, map[string]any{
		"to_user_id": alice.User.ID, "amount": 1000,
	}, &errResp); code != http.StatusForbidden || !strings.Contains(errResp.Error, "7 ngày") {
		t.Fatalf("new account transfer: want 403, got %d %q", code, errResp.Error)
	}
	if code := e.do("POST", "/api/messages/"+itoa(table.ID)+"/game", bob.Token,
		map[string]any{"action": "join"}, nil); code != http.StatusForbidden {
		t.Fatalf("new account joining bet table: want 403, got %d", code)
	}
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", bob.Token, map[string]any{
		"kind": "xiangqi", "options": map[string]any{"bet_amount": 500},
	}, nil); code != http.StatusBadRequest {
		t.Fatalf("new account opening bet table: want 400, got %d", code)
	}
	// Ván không cược vẫn chơi bình thường.
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", bob.Token, map[string]any{
		"kind": "xiangqi",
	}, nil); code != http.StatusCreated {
		t.Fatalf("free game for new account: %d", code)
	}
	if w := e.wallet(bob.Token); w.Balance != 1000 {
		t.Fatalf("bob balance must be untouched: %+v", w)
	}

	// Đủ 7 ngày thì chuyển được.
	e.st.SetClock(func() time.Time { return time.Now().Add(8 * 24 * time.Hour) })
	if code := e.do("POST", "/api/wallet/transfer", bob.Token, map[string]any{
		"to_user_id": alice.User.ID, "amount": 100,
	}, nil); code != http.StatusOK {
		t.Fatalf("transfer after 7 days: %d", code)
	}
}
