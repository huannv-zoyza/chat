package controller_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// doFrom gửi request như app chạy trên máy device ("" = không gửi mã máy) và đi qua Cloudflare
// Tunnel từ IP cfIP ("" = gọi thẳng từ máy chủ).
func (e *env) doFrom(method, path, token, device, cfIP string, body, out any) int {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if device != "" {
		req.Header.Set("X-Device-ID", device)
	}
	if cfIP != "" {
		req.Header.Set("CF-Connecting-IP", cfIP)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func (e *env) registerOn(username, device, cfIP string) (int, authResp) {
	e.t.Helper()
	var r authResp
	code := e.doFrom("POST", "/api/auth/register", "", device, cfIP, map[string]string{
		"username": username, "email": username + "@example.com", "password": "matkhau123",
	}, &r)
	return code, r
}

func (e *env) registerFrom(username, cfIP string) (int, string) {
	e.t.Helper()
	code, r := e.registerOn(username, "", cfIP)
	return code, r.Error
}

const (
	devA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	devB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	devC = "cccccccccccccccccccccccccccccccc"
)

func TestSignupLimitedPerDevice(t *testing.T) {
	e := newEnv(t)
	for _, u := range []string{"main1", "alt1"} {
		if code, r := e.registerOn(u, devA, ""); code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", u, code, r.Error)
		}
	}
	code, r := e.registerOn("alt2", devA, "")
	if code != http.StatusTooManyRequests || !strings.Contains(r.Error, "Máy này") {
		t.Fatalf("3rd account on same device: want 429, got %d %q", code, r.Error)
	}
	// Máy khác, hay app cũ không gửi mã máy, vẫn tạo được; bị chặn thì không giữ chỗ tên.
	if code, _ := e.registerOn("alt2", devB, ""); code != http.StatusCreated {
		t.Fatalf("other device: %d", code)
	}
	if code, _ := e.registerOn("old_app", "", ""); code != http.StatusCreated {
		t.Fatalf("no device header: %d", code)
	}
	// Mã máy sai định dạng bị bỏ qua.
	if code, _ := e.registerOn("weird", "short", ""); code != http.StatusCreated {
		t.Fatalf("invalid device id must be ignored: %d", code)
	}
}

func TestSharedDeviceCannotMoveCoins(t *testing.T) {
	e := newEnv(t)
	_, alice := e.registerOn("alice", devA, "")
	_, bob := e.registerOn("bob", devB, "")
	_, carol := e.registerOn("carol", devA, "") // tài khoản thứ hai trên máy của alice
	_, dave := e.registerOn("dave", devC, "")

	transfer := func(from authResp, device string, to authResp) (int, string) {
		var r struct {
			Error string `json:"error"`
		}
		code := e.doFrom("POST", "/api/wallet/transfer", from.Token, device, "",
			map[string]any{"to_user_id": to.User.ID, "amount": 10}, &r)
		return code, r.Error
	}
	if code, msg := transfer(bob, devB, alice); code != http.StatusOK {
		t.Fatalf("unrelated devices: %d %s", code, msg)
	}
	if code, msg := transfer(carol, devA, alice); code != http.StatusForbidden || !strings.Contains(msg, "cùng một máy") {
		t.Fatalf("same device transfer: want 403, got %d %q", code, msg)
	}
	if code, _ := transfer(alice, devA, carol); code != http.StatusForbidden {
		t.Fatalf("same device transfer back: want 403, got %d", code)
	}
	if code, msg := transfer(carol, devA, bob); code != http.StatusOK {
		t.Fatalf("carol to bob: %d %s", code, msg)
	}

	// Đăng nhập trên máy của alice là bị tính dùng chung máy với alice.
	var login authResp
	if code := e.doFrom("POST", "/api/auth/login", "", devA, "",
		map[string]string{"login": "dave", "password": "matkhau123"}, &login); code != http.StatusOK {
		t.Fatalf("dave login: %d", code)
	}
	if code, _ := transfer(dave, devC, alice); code != http.StatusForbidden {
		t.Fatalf("dave logged in on alice's device: want 403, got %d", code)
	}

	// Cược đấu với tài khoản dùng chung máy cũng bị chặn.
	var ch channel
	e.doFrom("POST", "/api/channels", alice.Token, devA, "", map[string]any{"name": "cuoc"}, &ch)
	e.doFrom("POST", "/api/channels/"+itoa(ch.ID)+"/join", carol.Token, devA, "", nil, nil)
	e.doFrom("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, devB, "", nil, nil)
	var table gameMsg
	if code := e.doFrom("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, devA, "", map[string]any{
		"kind": "xiangqi", "options": map[string]any{"bet_amount": 100},
	}, &table); code != http.StatusCreated {
		t.Fatalf("open bet table: %d", code)
	}
	if code := e.doFrom("POST", "/api/messages/"+itoa(table.ID)+"/game", carol.Token, devA, "",
		map[string]any{"action": "join"}, nil); code != http.StatusForbidden {
		t.Fatalf("same-device join bet: want 403, got %d", code)
	}
	if code := e.doFrom("POST", "/api/messages/"+itoa(table.ID)+"/game", bob.Token, devB, "",
		map[string]any{"action": "join"}, nil); code != http.StatusOK {
		t.Fatalf("bob join bet: %d", code)
	}
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
