package muse

// Đi đúng đường của Ask AI: ai.Chat → provider "muse" → cầu nối loopback → gateway.
// Package ai không import muse (chỉ main nối hai bên) nên test ở đây import ai được, không vòng lặp.

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"chat/ai"
)

func museChat(t *testing.T, sc *scenario) (*ai.Chat, func()) {
	t.Helper()
	gw := newFakeGateway(t, sc.handle)
	c, doneClient := newTestClient(t, gw, "RUNNING")
	lb, err := StartLoopback(c)
	if err != nil {
		t.Fatal(err)
	}
	chat := ai.NewChat(ai.Muse(lb.BaseURL, lb.Key))
	return chat, func() { lb.Close(); doneClient(); gw.Close() }
}

func TestAskAIThroughMuseProvider(t *testing.T) {
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("thủ đô Việt Nam?"), assistantEvent("Hà Nội.", "completed", true))
	}}
	chat, cleanup := museChat(t, sc)
	defer cleanup()

	if names := chat.Names(); len(names) != 1 || names[0] != "muse" {
		t.Fatalf("chuỗi nhà cung cấp = %v", names)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := chat.Complete(ctx, []ai.Message{{Role: "user", Content: "thủ đô Việt Nam?"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "Hà Nội." {
		t.Fatalf("Ask AI trả %q, muốn %q", got, "Hà Nội.")
	}
}

func TestAskAIStreamsThroughMuseProvider(t *testing.T) {
	step := 0
	sc := &scenario{history: func() map[string]any {
		step++
		switch {
		case step <= 1:
			return historyJSON(userEvent("đếm"), assistantEvent("một", "running", false))
		case step == 2:
			return historyJSON(userEvent("đếm"), assistantEvent("một hai", "running", false))
		default:
			return historyJSON(userEvent("đếm"), assistantEvent("một hai ba", "completed", true))
		}
	}}
	chat, cleanup := museChat(t, sc)
	defer cleanup()

	var sb strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := chat.Stream(ctx, []ai.Message{{Role: "user", Content: "đếm"}}, func(d string) { sb.WriteString(d) }); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(sb.String()); got != "một hai ba" {
		t.Fatalf("ghép delta qua Ask AI = %q, muốn %q", got, "một hai ba")
	}
}

// Tin system phải tới được Muse khi mở hội thoại mới (không có X-Muse-Session).
func TestSystemPromptReachesMuse(t *testing.T) {
	var sentText string
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("x"), assistantEvent("ok", "completed", true))
	}}
	handle := func(req gwRequest, w *fakeResponder) {
		if req.Path == "/chat/stream" && sentText == "" {
			sentText = string(req.Body)
		}
		sc.handle(req, w)
	}
	gw := newFakeGateway(t, handle)
	defer gw.Close()
	c, doneClient := newTestClient(t, gw, "RUNNING")
	defer doneClient()
	lb, err := StartLoopback(c)
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()
	chat := ai.NewChat(ai.Muse(lb.BaseURL, lb.Key))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := chat.Complete(ctx, []ai.Message{
		{Role: "system", Content: "Bạn là trợ lý của kênh chat."},
		{Role: "user", Content: "chào"},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sentText, "trợ lý của kênh chat") {
		t.Fatalf("tin system không tới Muse, gửi đi: %s", trim(sentText, 300))
	}
	if !strings.Contains(sentText, "chào") {
		t.Fatalf("tin user không tới Muse, gửi đi: %s", trim(sentText, 300))
	}
}

// Provider muse phải khai NoTools: chạy agent sẽ bỏ qua nó thay vì gửi tools rồi hỏng.
func TestMuseProviderFlags(t *testing.T) {
	p := ai.Muse("http://127.0.0.1:1/v1", "sk-x")
	if p.Name != "muse" || p.Model != "muse" {
		t.Fatalf("provider = %+v", p)
	}
	if p.NoTools {
		t.Fatal("cầu nối đã dịch được tools nên không còn NoTools")
	}
	if !p.Vision {
		t.Fatal("phải khai Vision: cầu nối đã đổi image_url thành mục ảnh của Muse")
	}
}

// Muse vừa viết vừa vẽ: ảnh sinh ra trong lượt chat thường phải về tới ai.Result.Images,
// để controller đính được vào tin. Trước đây ans.Files bị vứt đi — ảnh bốc hơi.
func TestGeneratedImageReachesAIResult(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nANHMUSEVE")
	sc := &scenario{
		files: map[string][]byte{"/workspace/out/gen.png": png},
		history: func() map[string]any {
			return historyJSON(userEvent("thêm người đứng cạnh"), map[string]any{
				"event_name": "message.assistant", "display_text": "Xong rồi nè 😄", "display_text_ready": true,
				"payload": map[string]any{"status": "completed", "data": map[string]any{
					"images": []any{map[string]any{"path": "sandbox:///workspace/out/gen.png", "mime": "image/png"}},
				}},
			})
		},
	}
	chat, cleanup := museChat(t, sc)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var sb strings.Builder
	res, err := chat.Stream(ctx, []ai.Message{{Role: "user", Content: "thêm người đứng cạnh"}},
		func(d string) { sb.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(sb.String()); got != "Xong rồi nè 😄" {
		t.Fatalf("chữ = %q", got)
	}
	if len(res.Images) != 1 {
		t.Fatalf("Result.Images có %d ảnh, muốn 1 — ảnh lại bị vứt mất", len(res.Images))
	}
	if res.Images[0].Type != "image/png" {
		t.Fatalf("mime = %q", res.Images[0].Type)
	}
	if !bytes.Equal(res.Images[0].Data, png) {
		t.Fatalf("byte ảnh không khớp (%d vs %d)", len(res.Images[0].Data), len(png))
	}
}

// Lượt chat không sinh ảnh thì Images phải rỗng — không được bịa ra ảnh.
func TestNoImageMeansNoImages(t *testing.T) {
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("2+2?"), assistantEvent("4", "completed", true))
	}}
	chat, cleanup := museChat(t, sc)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := chat.Stream(ctx, []ai.Message{{Role: "user", Content: "2+2?"}}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 0 {
		t.Fatalf("không có ảnh mà Result.Images có %d", len(res.Images))
	}
}

// Agent thật: ai.StreamTools gửi tools → cầu nối dịch sang prompt → Muse trả JSON →
// cầu nối đọc ngược thành tool_calls → package ai nhận được lệnh gọi.
func TestToolCallRoundTrip(t *testing.T) {
	var sentPrompt string
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("x"),
			assistantEvent(`{"tool":"read_messages","args":{"hours":24}}`, "completed", true))
	}}
	handle := func(req gwRequest, w *fakeResponder) {
		if req.Path == "/chat/stream" && sentPrompt == "" {
			var b struct {
				Items []map[string]any `json:"items"`
			}
			_ = json.Unmarshal(req.Body, &b)
			for _, it := range b.Items {
				if s, _ := it["text"].(string); s != "" {
					sentPrompt = s
				}
			}
		}
		sc.handle(req, w)
	}
	gw := newFakeGateway(t, handle)
	defer gw.Close()
	c, doneClient := newTestClient(t, gw, "RUNNING")
	defer doneClient()
	lb, err := StartLoopback(c)
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()
	chat := ai.NewChat(ai.Muse(lb.BaseURL, lb.Key))

	tools := []ai.Tool{{
		Name: "read_messages", Description: "Đọc tin nhắn trong kênh",
		Params: map[string]any{"type": "object", "properties": map[string]any{"hours": map[string]any{"type": "number"}}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var text strings.Builder
	_, calls, err := chat.StreamTools(ctx, []ai.Message{{Role: "user", Content: "tóm tắt hôm nay"}}, tools,
		func(d string) { text.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("nhận %d lệnh gọi, muốn 1: %+v (chữ=%q)", len(calls), calls, text.String())
	}
	if calls[0].Name != "read_messages" {
		t.Fatalf("tên công cụ = %q", calls[0].Name)
	}
	if !strings.Contains(calls[0].Args, `"hours"`) {
		t.Fatalf("tham số = %q", calls[0].Args)
	}
	// JSON của lệnh gọi TUYỆT ĐỐI không được lọt ra màn hình người dùng.
	if strings.Contains(text.String(), "read_messages") || strings.Contains(text.String(), "{") {
		t.Fatalf("JSON lệnh gọi lọt ra chữ trả lời: %q", text.String())
	}
	// Mô tả công cụ phải tới được Muse.
	if !strings.Contains(sentPrompt, "read_messages") || !strings.Contains(sentPrompt, "Đọc tin nhắn trong kênh") {
		t.Fatalf("prompt thiếu mô tả công cụ: %q", trim(sentPrompt, 400))
	}
	if !strings.Contains(sentPrompt, `{"tool": "<tên công cụ>", "args": {<tham số>}}`) {
		t.Fatal("prompt thiếu khuôn JSON")
	}
}

// Trả lời bằng lời (không gọi công cụ) thì phải ra chữ, không được hiểu nhầm thành tool call.
func TestToolsPresentButPlainAnswer(t *testing.T) {
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("x"), assistantEvent("Hôm nay mọi người bàn chuyện phở.", "completed", true))
	}}
	chat, cleanup := museChat(t, sc)
	defer cleanup()
	tools := []ai.Tool{{Name: "read_messages", Description: "Đọc tin"}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var sb strings.Builder
	_, calls, err := chat.StreamTools(ctx, []ai.Message{{Role: "user", Content: "tóm tắt"}}, tools,
		func(d string) { sb.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("không được bịa ra lệnh gọi: %+v", calls)
	}
	if strings.TrimSpace(sb.String()) != "Hôm nay mọi người bàn chuyện phở." {
		t.Fatalf("chữ = %q", sb.String())
	}
}

// Kết quả công cụ quay lại phải được dựng vào lượt sau để Muse dùng.
func TestToolResultFedBack(t *testing.T) {
	var sent string
	sc := &scenario{history: func() map[string]any {
		return historyJSON(userEvent("x"), assistantEvent("Mọi người bàn chuyện phở.", "completed", true))
	}}
	handle := func(req gwRequest, w *fakeResponder) {
		if req.Path == "/chat/stream" && sent == "" {
			var b struct {
				Items []map[string]any `json:"items"`
			}
			_ = json.Unmarshal(req.Body, &b)
			for _, it := range b.Items {
				if s, _ := it["text"].(string); s != "" {
					sent = s
				}
			}
		}
		sc.handle(req, w)
	}
	gw := newFakeGateway(t, handle)
	defer gw.Close()
	c, doneClient := newTestClient(t, gw, "RUNNING")
	defer doneClient()
	lb, err := StartLoopback(c)
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()
	chat := ai.NewChat(ai.Muse(lb.BaseURL, lb.Key))

	msgs := []ai.Message{
		{Role: "user", Content: "tóm tắt hôm nay"},
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "call0", Name: "read_messages", Args: `{"hours":24}`}}},
		{Role: "tool", ToolCallID: "call0", Content: "An: ăn phở không? · Bình: ok"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, _, err := chat.StreamTools(ctx, msgs, []ai.Tool{{Name: "read_messages", Description: "Đọc tin"}}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sent, "bạn đã gọi công cụ read_messages") {
		t.Fatalf("lượt gửi thiếu lệnh đã gọi: %q", trim(sent, 500))
	}
	if !strings.Contains(sent, "ăn phở không") {
		t.Fatalf("lượt gửi thiếu kết quả công cụ: %q", trim(sent, 500))
	}
}

func TestParseToolCallStrict(t *testing.T) {
	tools := []openaiTool{}
	var tl openaiTool
	tl.Type, tl.Function.Name = "function", "web_search"
	tools = append(tools, tl)

	if c, ok := parseToolCall(`{"tool":"web_search","args":{"q":"giá vàng"}}`, tools); !ok || c.Name != "web_search" {
		t.Fatalf("JSON thuần phải nhận: %+v ok=%v", c, ok)
	}
	if c, ok := parseToolCall("```json\n{\"tool\":\"web_search\",\"args\":{}}\n```", tools); !ok || c.Name != "web_search" {
		t.Fatalf("bọc khối code vẫn phải nhận: %+v ok=%v", c, ok)
	}
	if c, ok := parseToolCall(`{"tool":"web_search"}`, tools); !ok || c.Args != "{}" {
		t.Fatalf("thiếu args thì mặc định {}: %+v ok=%v", c, ok)
	}
	// Câu văn nhắc tên công cụ không được thành lệnh gọi.
	for _, s := range []string{
		"Mình sẽ dùng web_search để tra giá vàng nhé.",
		`Kết quả: {"tool":"web_search"} là cú pháp gọi công cụ.`,
		`{"tool":"khong_ton_tai","args":{}}`,
		"",
	} {
		if _, ok := parseToolCall(s, tools); ok {
			t.Errorf("không được coi là lệnh gọi: %q", s)
		}
	}
}
