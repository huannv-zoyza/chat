package muse

// Toàn tuyến qua gateway giả: REST → đánh thức → Noise → đóng khung → hội thoại → mặt ngoài OpenAI.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func historyJSON(events ...map[string]any) map[string]any {
	return map[string]any{"result": map[string]any{"chat_events": events}}
}

func userEvent(text string) map[string]any {
	return map[string]any{"event_name": "message.user", "display_text": text}
}

func assistantEvent(text, status string, ready bool) map[string]any {
	return map[string]any{
		"event_name": "message.assistant", "display_text": text,
		"display_text_ready": ready, "payload": map[string]any{"status": status},
	}
}

// handler dựng một gateway giả trả lời theo kịch bản.
type scenario struct {
	history   func() map[string]any
	subEvents []map[string]any
	files     map[string][]byte
}

func (s *scenario) handle(req gwRequest, w *fakeResponder) {
	switch {
	case req.Path == "/chat/stream":
		ack, _ := json.Marshal(map[string]any{"session_id": "sess-1", "ok": true, "is_thread": true})
		w.Head(200, "application/x-ndjson", append(ack, '\n'), true)
	case req.Path == "/chat/subscribe":
		w.Head(200, "application/x-ndjson", nil, false)
		go func() {
			for _, e := range s.subEvents {
				time.Sleep(20 * time.Millisecond)
				b, _ := json.Marshal(e)
				w.Body(append(b, '\n'), false)
			}
		}()
	case strings.HasPrefix(req.Path, "/chat/history"):
		w.JSON(200, s.history())
	case strings.HasPrefix(req.Path, "/fs/raw"):
		p := strings.TrimPrefix(req.Path, "/fs/raw")
		if data, ok := s.files[p]; ok {
			w.Head(200, "image/png", data, true)
		} else {
			w.Head(404, "text/plain", []byte("không có"), true)
		}
	default:
		w.JSON(404, map[string]any{"error": "không rõ đường dẫn " + req.Path})
	}
}

func TestAskReturnsAnswer(t *testing.T) {
	sc := &scenario{
		history: func() map[string]any {
			return historyJSON(userEvent("2+2?"), assistantEvent("4", "completed", true))
		},
		subEvents: []map[string]any{{"event": "task.status", "payload": map[string]any{"status": "completed"}}},
	}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()

	ans, err := c.Ask(context.Background(), []map[string]any{TextItem("2+2?")}, AskOptions{PollEvery: 50 * time.Millisecond, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "4" {
		t.Fatalf("câu trả lời = %q, muốn %q", ans.Text, "4")
	}
	if ans.SessionID != "sess-1" {
		t.Fatalf("session = %q, muốn sess-1 (lấy từ ack)", ans.SessionID)
	}
	// Lượt gửi phải mang đúng capabilities và node_id.
	var sent map[string]any
	for _, r := range gw.seen() {
		if r.Path == "/chat/stream" {
			_ = json.Unmarshal(r.Body, &sent)
		}
	}
	if sent == nil {
		t.Fatal("không thấy yêu cầu /chat/stream")
	}
	caps, _ := sent["capabilities"].([]any)
	if len(caps) != len(ChatCapabilities) {
		t.Fatalf("capabilities = %v", sent["capabilities"])
	}
	if _, ok := sent["node_id"].(string); !ok {
		t.Fatal("thiếu node_id")
	}
}

// vm_state "ready" phải được coi là đang chạy — chính lỗi mà bản desktop mắc phải.
func TestWakeAcceptsReadyAndLive(t *testing.T) {
	// "UNKNOWN" là giá trị thật quan sát được trên tài khoản đang chạy tốt (21/11/2026).
	for _, state := range []any{"ready", "live", "running", "RUNNING", "UNKNOWN", "unknown", "", nil} {
		sc := &scenario{history: func() map[string]any {
			return historyJSON(userEvent("hi"), assistantEvent("ok", "completed", true))
		}}
		gw := newFakeGateway(t, sc.handle)
		c, done := newTestClient(t, gw, state)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		ans, err := c.Ask(ctx, []map[string]any{TextItem("hi")}, AskOptions{PollEvery: 50 * time.Millisecond})
		cancel()
		if err != nil {
			t.Fatalf("vm_state=%v: %v", state, err)
		}
		if ans.Text != "ok" {
			t.Fatalf("vm_state=%v: trả lời %q", state, ans.Text)
		}
		done()
		gw.Close()
	}
}

// Chỉ những trạng thái chắc chắn là ngủ mới được coi là cần đánh thức.
func TestVMRunningBlocklist(t *testing.T) {
	for _, s := range []any{"RUNNING", "running", "ready", "live", "UNKNOWN", "STARTING", "weird_new_state", "", nil} {
		if !VMRunning(s) {
			t.Errorf("VMRunning(%v) = false, phải coi là nối được", s)
		}
	}
	for _, s := range []string{"SUSPENDED", "suspended", "stopped", "PAUSED", "hibernated", "terminated", "off"} {
		if VMRunning(s) {
			t.Errorf("VMRunning(%q) = true, trạng thái này phải đánh thức trước", s)
		}
	}
}

func TestStreamingDeltas(t *testing.T) {
	step := 0
	sc := &scenario{
		history: func() map[string]any {
			step++
			switch {
			case step <= 1:
				return historyJSON(userEvent("kể chuyện"), assistantEvent("Ngày", "running", false))
			case step == 2:
				return historyJSON(userEvent("kể chuyện"), assistantEvent("Ngày xửa", "running", false))
			default:
				return historyJSON(userEvent("kể chuyện"), assistantEvent("Ngày xửa ngày xưa", "completed", true))
			}
		},
	}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()

	var deltas []string
	ans, err := c.Ask(context.Background(), []map[string]any{TextItem("kể chuyện")}, AskOptions{
		PollEvery: 30 * time.Millisecond,
		Timeout:   20 * time.Second,
		OnDelta:   func(d string) { deltas = append(deltas, d) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Text != "Ngày xửa ngày xưa" {
		t.Fatalf("văn bản cuối = %q", ans.Text)
	}
	if got := strings.Join(deltas, ""); got != "Ngày xửa ngày xưa" {
		t.Fatalf("ghép delta = %q, muốn %q (deltas=%q)", got, "Ngày xửa ngày xưa", deltas)
	}
}

// Lời dẫn (commentary) không được tính là câu trả lời khi đã có câu trả lời thật.
func TestCommentaryNotTreatedAsAnswer(t *testing.T) {
	body, _ := json.Marshal(historyJSON(
		userEvent("viết hộ"),
		map[string]any{"event_name": "message.assistant", "display_text": "Để tôi viết nhé…",
			"display_text_ready": true, "payload": map[string]any{"metadata": map[string]any{"message_kind": "commentary"}}},
		assistantEvent("Xong rồi đây.", "completed", true),
	))
	turns, err := parseHistory(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("có %d lượt", len(turns))
	}
	if turns[0].Text != "Xong rồi đây." {
		t.Fatalf("câu trả lời = %q (lời dẫn lọt vào)", turns[0].Text)
	}
	if !turnLooksDone(turns[0]) {
		t.Fatal("lượt phải được coi là xong")
	}
}

func TestStripAttachmentMarkersAndWorkspacePath(t *testing.T) {
	in := "[image:image/png workspace/user/a.png]\n[file:text/csv workspace/user/b.csv]\nnội dung thật"
	if got := StripAttachmentMarkers(in); got != "nội dung thật" {
		t.Fatalf("còn lại %q", got)
	}
	if got := WorkspacePath("sandbox:///workspace/out/x.png"); got != "/workspace/out/x.png" {
		t.Fatalf("sandbox → %q", got)
	}
	if got := WorkspacePath("https://vm/media/raw/workspace/out/y.png?token=1"); got != "/workspace/out/y.png" {
		t.Fatalf("media/raw → %q", got)
	}
}

// Khung lớn hơn MaxFramePayload phải cắt mảnh rồi ghép lại đúng.
func TestLargePayloadChunking(t *testing.T) {
	big := strings.Repeat("đ", 60000) // ~180 KB UTF-8, vượt 1 khung
	var got string
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("x"), assistantEvent("ok", "completed", true))
	}}
	handle := func(req gwRequest, w *fakeResponder) {
		if req.Path == "/chat/stream" {
			var b struct {
				Items []map[string]any `json:"items"`
			}
			_ = json.Unmarshal(req.Body, &b)
			if len(b.Items) > 0 {
				got, _ = b.Items[0]["text"].(string)
			}
		}
		sc.handle(req, w)
	}
	gw := newFakeGateway(t, handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()

	if _, err := c.Ask(context.Background(), []map[string]any{TextItem(big)}, AskOptions{PollEvery: 50 * time.Millisecond, Timeout: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if got != big {
		t.Fatalf("máy chủ nhận %d ký tự, gửi %d — ghép mảnh sai", len([]rune(got)), len([]rune(big)))
	}
}

func TestOpenAIChatCompletions(t *testing.T) {
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("chào"), assistantEvent("Chào bạn!", "completed", true))
	}}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()

	srv := httptest.NewServer((&Server{C: c, APIKey: "sk-test"}).Handler())
	defer srv.Close()

	post := func(path, key string, body any) *http.Response {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(b))
		req.Header.Set("content-type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Thiếu khoá → 401
	if r := post("/v1/chat/completions", "", map[string]any{"messages": []any{}}); r.StatusCode != 401 {
		t.Fatalf("không có khoá phải 401, nhận %d", r.StatusCode)
	}
	// Sai khoá → 401
	if r := post("/v1/chat/completions", "sk-wrong", map[string]any{"messages": []any{}}); r.StatusCode != 401 {
		t.Fatalf("sai khoá phải 401, nhận %d", r.StatusCode)
	}

	resp := post("/v1/chat/completions", "sk-test", map[string]any{
		"model":    "muse",
		"messages": []any{map[string]any{"role": "user", "content": "chào"}},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("mã %d", resp.StatusCode)
	}
	var out struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Role, Content string
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Object != "chat.completion" || len(out.Choices) != 1 {
		t.Fatalf("hình dạng phản hồi sai: %+v", out)
	}
	if out.Choices[0].Message.Content != "Chào bạn!" {
		t.Fatalf("nội dung = %q", out.Choices[0].Message.Content)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q", out.Choices[0].FinishReason)
	}
	if resp.Header.Get(museSessionHeader) == "" {
		t.Fatal("thiếu header phiên để client nối tiếp hội thoại")
	}
}

func TestOpenAIStreamSSE(t *testing.T) {
	step := 0
	sc := &scenario{history: func() map[string]any {
		step++
		if step <= 1 {
			return historyJSON(userEvent("hi"), assistantEvent("Xin", "running", false))
		}
		return historyJSON(userEvent("hi"), assistantEvent("Xin chào", "completed", true))
	}}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()
	srv := httptest.NewServer((&Server{C: c}).Handler())
	defer srv.Close()

	b, _ := json.Marshal(map[string]any{"stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}})
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("content-type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	body := buf.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("thiếu [DONE]:\n%s", body)
	}
	var text strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		var ch struct {
			Object  string `json:"object"`
			Choices []struct {
				Delta struct{ Content string } `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ch) != nil {
			continue
		}
		if ch.Object != "chat.completion.chunk" {
			t.Fatalf("object = %q", ch.Object)
		}
		for _, c := range ch.Choices {
			text.WriteString(c.Delta.Content)
		}
	}
	if text.String() != "Xin chào" {
		t.Fatalf("ghép SSE = %q, muốn %q", text.String(), "Xin chào")
	}
}

func TestOpenAIImagesGenerations(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("A", 100))
	sc := &scenario{
		files: map[string][]byte{"/workspace/out/teapot.png": png},
		history: func() map[string]any {
			return historyJSON(
				userEvent("Vẽ ảnh: ấm trà"),
				map[string]any{
					"event_name": "message.assistant", "display_text": "Đây rồi.", "display_text_ready": true,
					"payload": map[string]any{"status": "completed", "data": map[string]any{
						"images": []any{map[string]any{"path": "sandbox:///workspace/out/teapot.png", "mime": "image/png"}},
					}},
				},
			)
		},
	}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()
	srv := httptest.NewServer((&Server{C: c}).Handler())
	defer srv.Close()

	b, _ := json.Marshal(map[string]any{"prompt": "ấm trà", "n": 1})
	resp, err := http.Post(srv.URL+"/v1/images/generations", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("mã %d: %s", resp.StatusCode, buf.String())
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 1 || out.Data[0].B64 == "" {
		t.Fatalf("không nhận được ảnh: %+v", out)
	}
	got, err := decodeB64(out.Data[0].B64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("byte ảnh không khớp (%d vs %d)", len(got), len(png))
	}
}

func TestModelsEndpoint(t *testing.T) {
	gw := newFakeGateway(t, (&scenario{}).handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()
	srv := httptest.NewServer((&Server{C: c}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct{ ID string } `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 1 || out.Data[0].ID != ModelName {
		t.Fatalf("models = %+v", out)
	}
}

// Cookie sai phải báo rõ là hết hạn chứ không im lặng treo.
func TestExpiredCookie(t *testing.T) {
	gw := newFakeGateway(t, (&scenario{}).handle)
	defer gw.Close()
	rest := fakeMuse(t, gw, "RUNNING")
	defer rest.Close()
	c, err := New(Config{Cookie: " ", Origin: rest.URL, GatewayURL: gw.wsURL()})
	if err == nil {
		_ = c
		t.Fatal("cookie rỗng phải bị từ chối ngay")
	}
	if !strings.Contains(err.Error(), "cookie") {
		t.Fatalf("thông báo lỗi = %v", err)
	}
}

func decodeB64(s string) ([]byte, error) { return b64Decode(s) }

// Provider ảnh phải thoả đúng interface mà package ai cần, và trả về byte ảnh thật.
func TestImageProviderGenerates(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("B", 64))
	sc := &scenario{
		files: map[string][]byte{"/workspace/out/a.png": png},
		history: func() map[string]any {
			return historyJSON(userEvent("Vẽ ảnh: mèo"), map[string]any{
				"event_name": "message.assistant", "display_text": "xong", "display_text_ready": true,
				"payload": map[string]any{"status": "completed", "data": map[string]any{
					"images": []any{map[string]any{"path": "sandbox:///workspace/out/a.png", "mime": "image/png"}},
				}},
			})
		},
	}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()

	// Khai báo qua biến interface để sai chữ ký là hỏng biên dịch ngay.
	var p interface {
		Name() string
		Generate(ctx context.Context, prompt string, w, h int) ([]byte, error)
	} = NewImageProvider(c)

	if p.Name() != "muse" {
		t.Fatalf("Name = %q", p.Name())
	}
	got, err := p.Generate(context.Background(), "mèo", 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("byte ảnh không khớp (%d vs %d)", len(got), len(png))
	}
}

// Không có ảnh thì báo lỗi rõ ràng kèm câu Muse đã trả lời.
func TestImageProviderNoImage(t *testing.T) {
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("Vẽ ảnh: x"), assistantEvent("Xin lỗi, tôi không vẽ được.", "completed", true))
	}}
	gw := newFakeGateway(t, sc.handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()
	_, err := NewImageProvider(c).Generate(context.Background(), "x", 0, 0)
	if err == nil {
		t.Fatal("phải báo lỗi khi không có ảnh")
	}
	if !strings.Contains(err.Error(), "không vẽ được") {
		t.Fatalf("lỗi phải kèm câu trả lời của Muse, đang là: %v", err)
	}
}

// Ảnh đính kèm trong /v1/chat/completions phải tới Muse dưới dạng mục ảnh, đứng TRƯỚC chữ.
func TestAttachedImageReachesMuse(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nNOIDUNG")
	var sent []map[string]any
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("ảnh này là gì?"), assistantEvent("Một con mèo.", "completed", true))
	}}
	handle := func(req gwRequest, w *fakeResponder) {
		if req.Path == "/chat/stream" && sent == nil {
			var b struct {
				Items []map[string]any `json:"items"`
			}
			_ = json.Unmarshal(req.Body, &b)
			sent = b.Items
		}
		sc.handle(req, w)
	}
	gw := newFakeGateway(t, handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()
	srv := httptest.NewServer((&Server{C: c}).Handler())
	defer srv.Close()

	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{
		"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "ảnh này là gì?"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": uri}},
		},
	}}})
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(resp.Body)
		t.Fatalf("mã %d: %s", resp.StatusCode, b.String())
	}
	if len(sent) != 2 {
		t.Fatalf("gửi %d mục, muốn 2 (ảnh + chữ): %+v", len(sent), sent)
	}
	if sent[0]["type"] != "image" {
		t.Fatalf("mục đầu phải là ảnh, đang là %v", sent[0]["type"])
	}
	if sent[0]["mime_type"] != "image/png" {
		t.Fatalf("mime = %v", sent[0]["mime_type"])
	}
	got, err := base64.StdEncoding.DecodeString(sent[0]["data_base64"].(string))
	if err != nil || !bytes.Equal(got, png) {
		t.Fatalf("byte ảnh gửi đi không khớp (err=%v)", err)
	}
	if sent[1]["type"] != "text" || sent[1]["text"] != "ảnh này là gì?" {
		t.Fatalf("mục chữ sai: %+v", sent[1])
	}
}

// Vẽ lại từ ảnh gốc: ảnh vào, ảnh mới ra.
func TestEditImageFromReference(t *testing.T) {
	src := []byte("\x89PNG\r\n\x1aGOC")
	out := []byte("\x89PNG\r\n\x1aMOI")
	var sent []map[string]any
	sc := &scenario{
		files: map[string][]byte{"/workspace/out/new.png": out},
		history: func() map[string]any {
			return historyJSON(userEvent("đổi nền"), map[string]any{
				"event_name": "message.assistant", "display_text": "xong", "display_text_ready": true,
				"payload": map[string]any{"status": "completed", "data": map[string]any{
					"images": []any{map[string]any{"path": "sandbox:///workspace/out/new.png", "mime": "image/png"}},
				}},
			})
		},
	}
	handle := func(req gwRequest, w *fakeResponder) {
		if req.Path == "/chat/stream" && sent == nil {
			var b struct {
				Items []map[string]any `json:"items"`
			}
			_ = json.Unmarshal(req.Body, &b)
			sent = b.Items
		}
		sc.handle(req, w)
	}
	gw := newFakeGateway(t, handle)
	defer gw.Close()
	c, done := newTestClient(t, gw, "RUNNING")
	defer done()

	data, ctype, _, err := c.EditImage(context.Background(), "đổi nền thành xanh",
		[]ImageRef{{Data: src, Mime: "image/png"}}, 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, out) {
		t.Fatalf("ảnh trả về không khớp")
	}
	if ctype != "image/png" {
		t.Fatalf("ctype = %q", ctype)
	}
	if len(sent) != 2 || sent[0]["type"] != "image" {
		t.Fatalf("phải gửi ảnh gốc trước chữ: %+v", sent)
	}
	gotSrc, _ := base64.StdEncoding.DecodeString(sent[0]["data_base64"].(string))
	if !bytes.Equal(gotSrc, src) {
		t.Fatal("ảnh gốc gửi đi không khớp")
	}
	if txt, _ := sent[1]["text"].(string); !strings.Contains(txt, "đổi nền thành xanh") || !strings.Contains(txt, "1024x1024") {
		t.Fatalf("chữ gửi đi = %q", txt)
	}
}

func TestParseDataURI(t *testing.T) {
	r, err := parseDataURI("data:image/webp;base64," + base64.StdEncoding.EncodeToString([]byte("xy")))
	if err != nil || r.Mime != "image/webp" || string(r.Data) != "xy" {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	// URL ngoài bị từ chối: cầu nối không tự đi tải ảnh từ internet.
	if _, err := parseDataURI("https://example.com/a.png"); err == nil {
		t.Fatal("phải từ chối URL http")
	}
	if _, err := parseDataURI("data:image/png,khongphaibase64"); err == nil {
		t.Fatal("phải từ chối data URI không base64")
	}
}
