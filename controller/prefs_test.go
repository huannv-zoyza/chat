package controller_test

import "testing"

func TestUserPrefs(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")

	var p map[string]any
	if code := e.do("GET", "/api/me/prefs", alice.Token, nil, &p); code != 200 || len(p) != 0 {
		t.Fatalf("default prefs: %d %v", code, p)
	}

	// lưu được, đọc lại được
	if code := e.do("PATCH", "/api/me/prefs", alice.Token, map[string]any{"thread_width": 512}, &p); code != 200 || p["thread_width"] != 512.0 {
		t.Fatalf("save: %d %v", code, p)
	}
	p = nil
	e.do("GET", "/api/me/prefs", alice.Token, nil, &p)
	if p["thread_width"] != 512.0 {
		t.Fatalf("reload: %v", p)
	}

	// ngoài khoảng thì kẹp lại, số lẻ thì làm tròn
	e.do("PATCH", "/api/me/prefs", alice.Token, map[string]any{"thread_width": 5000}, &p)
	if p["thread_width"] != 900.0 {
		t.Fatalf("clamp max: %v", p)
	}
	e.do("PATCH", "/api/me/prefs", alice.Token, map[string]any{"thread_width": 100.4}, &p)
	if p["thread_width"] != 320.0 {
		t.Fatalf("clamp min: %v", p)
	}

	// khoá lạ / sai kiểu bị từ chối, không làm hỏng bản đã lưu
	if code := e.do("PATCH", "/api/me/prefs", alice.Token, map[string]any{"evil": 1}, nil); code != 400 {
		t.Fatalf("unknown key: %d", code)
	}
	if code := e.do("PATCH", "/api/me/prefs", alice.Token, map[string]any{"thread_width": "rộng"}, nil); code != 400 {
		t.Fatalf("bad type: %d", code)
	}
	p = nil
	e.do("GET", "/api/me/prefs", alice.Token, nil, &p)
	if p["thread_width"] != 320.0 {
		t.Fatalf("after rejected patch: %v", p)
	}

	// mỗi người một cấu hình
	p = nil
	e.do("GET", "/api/me/prefs", bob.Token, nil, &p)
	if len(p) != 0 {
		t.Fatalf("bob sees alice's prefs: %v", p)
	}

	// null = về mặc định
	e.do("PATCH", "/api/me/prefs", alice.Token, map[string]any{"thread_width": nil}, &p)
	if _, ok := p["thread_width"]; ok {
		t.Fatalf("reset: %v", p)
	}

	if code := e.do("GET", "/api/me/prefs", "", nil, nil); code != 401 {
		t.Fatalf("unauthenticated: %d", code)
	}
}
