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

func TestSignupLimitedPerIP(t *testing.T) {
	e := newEnv(t)
	for _, u := range []string{"clone1", "clone2", "clone3"} {
		if code, msg := e.registerFrom(u, "203.0.113.7"); code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", u, code, msg)
		}
	}
	code, msg := e.registerFrom("clone4", "203.0.113.7")
	if code != http.StatusTooManyRequests || !strings.Contains(msg, "quá nhiều tài khoản") {
		t.Fatalf("4th signup from same IP: want 429, got %d %q", code, msg)
	}
	// Bị chặn thì không giữ chỗ tên đăng nhập.
	if code, _ := e.registerFrom("clone4", "198.51.100.9"); code != http.StatusCreated {
		t.Fatalf("other IP must still sign up, got %d", code)
	}
	// IPv6 cùng dải /64 tính chung một mạng.
	for i, ip := range []string{"2001:db8:1:2::a", "2001:db8:1:2::b", "2001:db8:1:2:ffff::1"} {
		if code, msg := e.registerFrom("six"+itoa(int64(i)), ip); code != http.StatusCreated {
			t.Fatalf("ipv6 signup %d: %d %s", i, code, msg)
		}
	}
	if code, _ := e.registerFrom("six9", "2001:db8:1:2:dead::beef"); code != http.StatusTooManyRequests {
		t.Fatalf("4th signup from same /64: want 429, got %d", code)
	}
	// Gọi thẳng từ máy chủ (không qua tunnel) không bị giới hạn theo IP.
	for _, u := range []string{"admin1", "admin2", "admin3", "admin4"} {
		if code, msg := e.registerFrom(u, ""); code != http.StatusCreated {
			t.Fatalf("local register %s: %d %s", u, code, msg)
		}
	}
	// Qua 24 giờ thì IP đó tạo tiếp được.
	e.st.SetClock(func() time.Time { return time.Now().Add(25 * time.Hour) })
	if code, msg := e.registerFrom("clone5", "203.0.113.7"); code != http.StatusCreated {
		t.Fatalf("signup after 24h: %d %s", code, msg)
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

	e.api.SetNewAccountRules(3, 7*24*time.Hour)

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
