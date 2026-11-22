package muse

// Cầu nối nhúng: chỉ nghe loopback, bắt buộc khoá ngẫu nhiên của lần chạy.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLoopbackRequiresRandomKey(t *testing.T) {
	gw := newFakeGateway(t, (&scenario{}).handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()

	lb, err := StartLoopback(c)
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()

	if !strings.HasPrefix(lb.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("phải bind loopback, đang là %q", lb.BaseURL)
	}
	if !strings.HasSuffix(lb.BaseURL, "/v1") {
		t.Fatalf("BaseURL phải kết thúc bằng /v1 cho ai.Provider, đang là %q", lb.BaseURL)
	}
	if len(lb.Key) < 20 {
		t.Fatalf("khoá quá ngắn: %q", lb.Key)
	}

	get := func(key string) int {
		req, _ := http.NewRequest("GET", lb.BaseURL+"/models", nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(""); got != 401 {
		t.Fatalf("không khoá phải 401, nhận %d", got)
	}
	if got := get("sk-sai"); got != 401 {
		t.Fatalf("sai khoá phải 401, nhận %d", got)
	}
	if got := get(lb.Key); got != 200 {
		t.Fatalf("đúng khoá phải 200, nhận %d", got)
	}

	// Mỗi lần chạy một khoá khác nhau.
	lb2, err := StartLoopback(c)
	if err != nil {
		t.Fatal(err)
	}
	defer lb2.Close()
	if lb2.Key == lb.Key {
		t.Fatal("khoá phải ngẫu nhiên theo từng lần chạy")
	}

	if err := lb.Close(); err != nil {
		t.Fatalf("đóng: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := http.Get(lb.BaseURL + "/models"); err == nil {
		t.Fatal("đóng rồi mà vẫn gọi được")
	}
}

// Gọi qua cầu nối nhúng đúng như ai.Provider sẽ gọi: POST /v1/chat/completions kèm Bearer.
func TestLoopbackChatCompletions(t *testing.T) {
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("hỏi"), assistantEvent("đáp", "completed", true))
	}}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()
	lb, err := StartLoopback(c)
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()

	b, _ := json.Marshal(map[string]any{
		"model": "muse", "messages": []any{map[string]any{"role": "user", "content": "hỏi"}},
	})
	req, _ := http.NewRequest("POST", lb.BaseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+lb.Key)
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("mã %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content != "đáp" {
		t.Fatalf("nội dung = %+v", out)
	}
}
