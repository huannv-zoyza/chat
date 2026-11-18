package controller_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"chat/ai"
)

// fakeAI: dịch vụ chat dạng OpenAI + dịch vụ ảnh kiểu Pollinations, ghi lại yêu cầu nhận được.
type fakeAI struct {
	mu        sync.Mutex
	systems   []string
	asks      []string
	imagePro  []string
	edits     []string
	plans     int
	described int
	srv       *httptest.Server
}

func newFakeAI(t *testing.T) *fakeAI {
	f := &fakeAI{}
	img := image.NewRGBA(image.Rect(0, 0, 64, 40))
	img.Set(1, 1, color.RGBA{0, 0, 255, 255})
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, img)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			var body struct {
				Prompt string `json:"prompt"`
				Image  []struct {
					URL string `json:"image_url"`
				} `json:"image"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			for _, im := range body.Image {
				f.edits = append(f.edits, body.Prompt+" | "+im.URL[:min(len(im.URL), 30)])
			}
			f.mu.Unlock()
			fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(pngBuf.Bytes()))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/prompt/") {
			f.mu.Lock()
			f.imagePro = append(f.imagePro, strings.TrimPrefix(r.URL.Path, "/prompt/")+"?"+r.URL.RawQuery)
			f.mu.Unlock()
			w.Write(pngBuf.Bytes())
			return
		}
		var req struct {
			Messages []ai.Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		sys, ask := req.Messages[0].Content, req.Messages[len(req.Messages)-1].Content
		stream := func(parts ...string) {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, s := range parts {
				b, _ := json.Marshal(map[string]any{"model": "fake-1", "choices": []any{map[string]any{"delta": map[string]string{"content": s}}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
		if strings.Contains(sys, "You decide what context") { // bước lập kế hoạch
			f.mu.Lock()
			f.plans++
			f.mu.Unlock()
			if strings.Contains(ask, "tóm tắt") {
				stream(`{"history_hours": 24, "search_chat": "", "web_search": "", "look_at_images": true}`)
			} else {
				stream(`{"history_hours": 0, "search_chat": "", "web_search": "", "look_at_images": false}`)
			}
			return
		}
		if strings.HasPrefix(sys, "Mô tả bức ảnh") { // model đọc ảnh (chỉ có 1 tin user kèm ảnh)
			f.mu.Lock()
			f.described++
			f.mu.Unlock()
			stream("Một hình chữ nhật đỏ nhỏ trên nền trong suốt")
			return
		}
		f.mu.Lock()
		f.systems = append(f.systems, sys)
		f.asks = append(f.asks, ask)
		f.mu.Unlock()
		reply := []string{"Tóm tắt: ", "mọi người ", "đang bàn ", "chuyện **phở**."}
		if strings.Contains(sys, "text-to-image") {
			reply = []string{"a cute cat astronaut, watercolor"}
		}
		if strings.Contains(sys, "image-editing") {
			reply = []string{"redraw as a chibi character, keep the face"}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, s := range reply {
			b, _ := json.Marshal(map[string]any{"model": "fake-1", "choices": []any{map[string]any{"delta": map[string]string{"content": s}}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
			time.Sleep(150 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type wsMsg struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Content     string `json:"content"`
	ReplyToID   *int64 `json:"reply_to_id"`
	Attachments []struct {
		Width       int    `json:"width"`
		ContentType string `json:"content_type"`
		FileName    string `json:"file_name"`
	} `json:"attachments"`
}

// nextMsg: đợi sự kiện typ có message thoả cond.
func nextMsg(t *testing.T, c *wsClient, typ string, cond func(wsMsg) bool) wsMsg {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		ev := c.waitForTimeout(typ, 8*time.Second)
		b, _ := json.Marshal(ev["message"])
		var m wsMsg
		json.Unmarshal(b, &m)
		if cond(m) {
			return m
		}
	}
	t.Fatalf("no %s matching", typ)
	return wsMsg{}
}

func TestAIAssistant(t *testing.T) {
	e := newEnv(t)
	f := newFakeAI(t)
	chat := ai.NewChat(&ai.Provider{Name: "fake", BaseURL: f.srv.URL, Model: "fake-1", Vision: true})
	images := &ai.Images{Providers: []ai.ImageProvider{ai.PollinationsImage{BaseURL: f.srv.URL}}}
	if err := e.api.SetAI(context.Background(), chat, images, nil); err != nil {
		t.Fatal(err)
	}
	// gọi lại không tạo bot thứ hai
	if err := e.api.SetAI(context.Background(), chat, images, nil); err != nil {
		t.Fatal(err)
	}

	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "an-trua"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)

	wa := e.dial(alice.Token)
	ready := wa.waitFor("ready")
	info, _ := ready["ai"].(map[string]any)
	if info == nil || info["chat"] != true || info["image"] != true {
		t.Fatalf("ready without ai info: %v", ready["ai"])
	}
	botID := int64(info["bot_id"].(float64))
	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	wb.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "Trưa nay ăn phở Thìn nhé", "client_id": "b1"})
	nextMsg(t, wa, "message.new", func(m wsMsg) bool { return m.UserID == bob.User.ID })

	// hỏi: bot trả lời ngay trong kênh, trích tin hỏi, chữ hiện dần rồi chốt kèm tên model
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "@ai tóm tắt giúp mình", "client_id": "a1"})
	q := nextMsg(t, wb, "message.new", func(m wsMsg) bool { return m.UserID == alice.User.ID })
	bot := nextMsg(t, wb, "message.new", func(m wsMsg) bool { return m.UserID == botID })
	if bot.Content != "…" || bot.ReplyToID == nil || *bot.ReplyToID != q.ID {
		t.Fatalf("bot placeholder: %+v", bot)
	}
	final := nextMsg(t, wb, "message.updated", func(m wsMsg) bool { return m.ID == bot.ID && strings.Contains(m.Content, "fake-1") })
	if !strings.HasPrefix(final.Content, "Tóm tắt: mọi người đang bàn chuyện **phở**.") {
		t.Fatalf("final content: %q", final.Content)
	}
	f.mu.Lock()
	sys, ask := f.systems[len(f.systems)-1], f.asks[len(f.asks)-1]
	plans := f.plans
	f.mu.Unlock()
	if !strings.Contains(sys, "Bob: Trưa nay ăn phở Thìn nhé") || !strings.Contains(sys, "#an-trua") || !strings.Contains(sys, "từ ") {
		t.Fatalf("context missing history: %s", sys)
	}
	if ask != "Alice hỏi: tóm tắt giúp mình" || plans != 1 {
		t.Fatalf("ask = %q, plans = %d", ask, plans)
	}
	if !strings.Contains(final.Content, "đọc ") {
		t.Fatalf("footer should say what was read: %q", final.Content)
	}

	// vẽ: nhờ AI viết lại mô tả tiếng Anh, ảnh gắn vào tin của bot
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "@ai vẽ chú mèo phi hành gia --ngang", "client_id": "a2"})
	draw := nextMsg(t, wb, "message.new", func(m wsMsg) bool { return m.UserID == botID && strings.HasPrefix(m.Content, "🎨 Đang vẽ") })
	if !strings.Contains(draw.Content, "chú mèo phi hành gia") || strings.Contains(draw.Content, "--ngang") {
		t.Fatalf("draw placeholder: %q", draw.Content)
	}
	done := nextMsg(t, wb, "message.updated", func(m wsMsg) bool { return m.ID == draw.ID && len(m.Attachments) == 1 })
	if done.Attachments[0].Width != 64 || done.Attachments[0].ContentType != "image/png" || !strings.HasPrefix(done.Attachments[0].FileName, "ai-chu-meo") {
		t.Fatalf("attachment: %+v", done.Attachments)
	}
	f.mu.Lock()
	imgReq := f.imagePro[0]
	f.mu.Unlock()
	if !strings.Contains(imgReq, "cute cat astronaut") || !strings.Contains(imgReq, "width=1216") {
		t.Fatalf("image request: %s", imgReq)
	}

	// tin không nhắc @ai, @aixyz, tin nhắn riêng: bot im lặng
	var dm struct{ ID int64 }
	e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": bob.User.ID}, &dm)
	wa.send(map[string]any{"type": "message.send", "channel_id": dm.ID, "content": "@ai chào", "client_id": "a3"})
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "email me@ai.vn hoặc @aixyz", "client_id": "a4"})
	time.Sleep(700 * time.Millisecond)
	f.mu.Lock()
	n := len(f.asks)
	f.mu.Unlock()
	if n != 2 { // câu trả lời + 1 lần viết lại mô tả ảnh (bước lập kế hoạch đếm riêng)
		t.Fatalf("unexpected AI calls: %d", n)
	}
}

func TestAIRedrawFromImage(t *testing.T) {
	e := newEnv(t)
	f := newFakeAI(t)
	chat := ai.NewChat(&ai.Provider{Name: "fake", BaseURL: f.srv.URL, Model: "fake-1", Vision: true})
	editor := ai.PollinationsEditor{PollinationsImage: ai.PollinationsImage{Key: "k", BaseURL: f.srv.URL}, GenURL: f.srv.URL}
	if err := e.api.SetAI(context.Background(), chat, &ai.Images{Providers: []ai.ImageProvider{editor}}, nil); err != nil {
		t.Fatal(err)
	}
	alice := e.register("alice", "Alice")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "ve"}, &ch)
	wa := e.dial(alice.Token)
	info := wa.waitFor("ready")["ai"].(map[string]any)
	botID := int64(info["bot_id"].(float64))
	src := pngBytes(30, 20)

	// 1. ảnh gửi kèm tin "@ai biến …"
	code, msg := e.upload(ch.ID, alice.Token, "@ai biến nhân vật này thành chibi", "u1", upFile{"nhan-vat.png", src})
	if code != 201 {
		t.Fatalf("upload: %d %v", code, msg)
	}
	draw := nextMsg(t, wa, "message.new", func(m wsMsg) bool { return m.UserID == botID })
	if !strings.Contains(draw.Content, "vẽ lại từ ảnh gốc") {
		t.Fatalf("placeholder: %q", draw.Content)
	}
	done := nextMsg(t, wa, "message.updated", func(m wsMsg) bool { return m.ID == draw.ID && len(m.Attachments) == 1 })
	if !strings.Contains(done.Content, "vẽ lại từ ảnh gốc bởi pollinations") {
		t.Fatalf("final: %q", done.Content)
	}

	// 2. trả lời trích dẫn tin có ảnh rồi "@ai vẽ …"
	srcID := int64(msg["id"].(float64))
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "@ai vẽ lại kiểu anime", "client_id": "a2", "reply_to_id": srcID})
	draw2 := nextMsg(t, wa, "message.new", func(m wsMsg) bool { return m.UserID == botID && m.ID != draw.ID })
	nextMsg(t, wa, "message.updated", func(m wsMsg) bool { return m.ID == draw2.ID && len(m.Attachments) == 1 })

	f.mu.Lock()
	edits := append([]string(nil), f.edits...)
	f.mu.Unlock()
	if len(edits) != 2 || !strings.HasPrefix(edits[0], "redraw as a chibi character, keep the face | data:image/png;base64,") {
		t.Fatalf("edit requests: %v", edits)
	}

	// 3. có ảnh nhưng chỉ hỏi (không nhờ sửa): trả lời bằng chữ, báo là không xem được ảnh
	e.upload(ch.ID, alice.Token, "@ai đây là ai?", "u3", upFile{"x.png", src})
	nextMsg(t, wa, "message.updated", func(m wsMsg) bool { return m.UserID == botID && strings.Contains(m.Content, "fake-1") })
	f.mu.Lock()
	lastAsk := f.asks[len(f.asks)-1]
	f.mu.Unlock()
	if !strings.Contains(lastAsk, "Ảnh x.png: Một hình chữ nhật đỏ nhỏ") {
		t.Fatalf("ask with image: %q", lastAsk)
	}
}

// Tóm tắt "1 ngày" phải đọc hết tin trong ngày (không chỉ vài chục tin gần nhất), kể cả trả lời
// trong luồng, ảnh được mô tả một lần rồi dùng lại.
func TestAISummaryReadsWholeRange(t *testing.T) {
	e := newEnv(t)
	f := newFakeAI(t)
	chat := ai.NewChat(&ai.Provider{Name: "fake", BaseURL: f.srv.URL, Model: "fake-1", Vision: true})
	if err := e.api.SetAI(context.Background(), chat, nil, nil); err != nil {
		t.Fatal(err)
	}
	alice := e.register("alice", "Alice")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "team-h"}, &ch)
	wa := e.dial(alice.Token)
	botID := int64(wa.waitFor("ready")["ai"].(map[string]any)["bot_id"].(float64))
	for i := 1; i <= 80; i++ {
		wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": fmt.Sprintf("tin số %d", i), "client_id": fmt.Sprint("m", i)})
	}
	code, img := e.upload(ch.ID, alice.Token, "ảnh team building", "img", upFile{"team.png", pngBytes(8, 8)})
	if code != 201 {
		t.Fatal(code)
	}
	wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "trả lời trong luồng nè", "client_id": "th", "parent_id": int64(img["id"].(float64))})
	time.Sleep(300 * time.Millisecond)

	for round := 0; round < 2; round++ {
		wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": "@ai tóm tắt 1 ngày qua", "client_id": fmt.Sprint("q", round)})
		nextMsg(t, wa, "message.updated", func(m wsMsg) bool { return m.UserID == botID && strings.Contains(m.Content, "fake-1") })
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sys := f.systems[len(f.systems)-1]
	for _, want := range []string{"tin số 1\n", "tin số 80", "↳ ", "trả lời trong luồng nè", "[ảnh team.png: Một hình chữ nhật đỏ nhỏ"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("summary context missing %q:\n%s", want, sys)
		}
	}
	if f.described != 1 {
		t.Fatalf("image should be described once and cached, got %d", f.described)
	}
}
