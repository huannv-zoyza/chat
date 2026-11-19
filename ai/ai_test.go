package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func tinyPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// fakeOpenAI: /models và /chat/completions dạng OpenAI, trả lời bằng các đoạn words.
func fakeOpenAI(t *testing.T, models []string, words []string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			var data []map[string]string
			for _, m := range models {
				data = append(data, map[string]string{"id": m})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": data})
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			if status != 200 {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
				return
			}
			var req struct {
				Model    string    `json:"model"`
				Messages []Message `json:"messages"`
				Stream   bool      `json:"stream"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if !req.Stream || len(req.Messages) == 0 {
				t.Errorf("bad request: %+v", req)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, ": keep-alive\n\n")
			fmt.Fprintf(w, "data: {\"model\":%q,\"choices\":[{\"delta\":{\"reasoning\":\"hmm\"}}]}\n\n", req.Model)
			for _, s := range words {
				b, _ := json.Marshal(map[string]any{"model": req.Model, "choices": []any{map[string]any{"delta": map[string]string{"content": s}}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestStreamAndFallback(t *testing.T) {
	down := fakeOpenAI(t, nil, nil, 429)
	defer down.Close()
	up := fakeOpenAI(t, []string{"models/gemini-2.0-flash-lite", "models/gemini-2.5-flash", "models/gemini-3-flash-preview", "models/gemini-2.5-pro"}, []string{"Xin ", "chào", "!"}, 200)
	defer up.Close()

	first := &Provider{Name: "down", BaseURL: down.URL, Model: "x"}
	second := Gemini("k", "")
	second.BaseURL = up.URL
	c := NewChat(first, second)
	var got strings.Builder
	res, err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) { got.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "Xin chào!" {
		t.Fatalf("content = %q", got.String())
	}
	// ưu tiên "flash", bỏ "lite", bản chính trước bản preview
	if res.Provider != "gemini" || res.Model != "gemini-2.5-flash" {
		t.Fatalf("result = %+v", res)
	}

	only := NewChat(&Provider{Name: "down", BaseURL: down.URL, Model: "x"})
	if _, err := only.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected provider error, got %v", err)
	}
}

func TestPickModel(t *testing.T) {
	ids := []string{"whisper-large-v3", "llama-3.1-8b-instant", "llama-3.3-70b-versatile", "openai/gpt-oss-120b", "meta-llama/llama-guard-4"}
	g := Groq("k", "")
	if m := pickModel(ids, g.Prefer, g.Avoid); m != "openai/gpt-oss-120b" {
		t.Fatalf("groq pick = %s", m)
	}
	if m := pickModel([]string{"a", "b"}, []string{"zzz"}, nil); m != "b" {
		t.Fatalf("fallback pick = %s", m)
	}
}

func TestImageProviders(t *testing.T) {
	pngData := tinyPNG()
	b64 := base64.StdEncoding.EncodeToString(pngData)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/prompt/"):
			if r.URL.Query().Get("width") != "768" || !strings.Contains(r.URL.Path, "con mèo") {
				t.Errorf("pollinations request: %s", r.URL)
			}
			w.Write(pngData)
		case r.URL.Path == "/images/generations":
			fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, b64)
		case strings.Contains(r.URL.Path, "/ai/run/"):
			fmt.Fprintf(w, `{"success":true,"result":{"image":%q}}`, b64)
		case r.URL.Path == "/chat/completions":
			fmt.Fprintf(w, `{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,%s"}}]}}]}`, b64)
		case r.URL.Path == "/broken/images/generations":
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":{"message":"model retired"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	for _, p := range []ImageProvider{
		PollinationsImage{BaseURL: srv.URL},
		TogetherImage{Key: "k", BaseURL: srv.URL},
		CloudflareImage{AccountID: "acc", Token: "t", BaseURL: srv.URL},
		OpenRouterImage{Key: "k", Model: "m", BaseURL: srv.URL},
	} {
		b, err := p.Generate(ctx, "con mèo", 768, 768)
		if err != nil || !bytes.Equal(b, pngData) {
			t.Fatalf("%s: err=%v len=%d", p.Name(), err, len(b))
		}
	}
	im := &Images{Providers: []ImageProvider{TogetherImage{Key: "k", BaseURL: srv.URL + "/broken"}, PollinationsImage{BaseURL: srv.URL}}}
	b, name, err := im.Generate(ctx, "con mèo", 768, 768)
	if err != nil || name != "pollinations" || len(b) == 0 {
		t.Fatalf("fallback: %v %s", err, name)
	}
}

func TestFromEnv(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	c, im := FromEnv(get)
	if c == nil || strings.Join(c.Names(), ",") != "pollinations" || strings.Join(im.Names(), ",") != "pollinations" {
		t.Fatalf("no keys -> pollinations only, got %v %v", c.Names(), im.Names())
	}
	env = map[string]string{"GEMINI_API_KEY": "g", "GROQ_API_KEY": "q", "OPENROUTER_API_KEY": "o", "TOGETHER_API_KEY": "t",
		"CF_ACCOUNT_ID": "a", "CF_API_TOKEN": "b", "OPENROUTER_IMAGE_MODEL": "m"}
	c, im = FromEnv(get)
	if got := strings.Join(c.Names(), ","); got != "gemini,groq,openrouter,pollinations" {
		t.Fatalf("chat order = %s", got)
	}
	if got := strings.Join(im.Names(), ","); got != "together,cloudflare,openrouter,pollinations" {
		t.Fatalf("image order = %s", got)
	}
	env["AI_NO_POLLINATIONS"] = "1"
	if c, _ = FromEnv(get); strings.Contains(strings.Join(c.Names(), ","), "pollinations") {
		t.Fatal("AI_NO_POLLINATIONS")
	}
	env["AI_DISABLE"] = "true"
	if c, im = FromEnv(get); c != nil || im != nil {
		t.Fatal("AI_DISABLE")
	}
}

func TestRetryOnOverload(t *testing.T) {
	calls := 0
	pngData := tinyPNG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"message":"The Classic API is at its limit right now. Retry after 1s"}}`)
			return
		}
		w.Write(pngData)
	}))
	defer srv.Close()
	im := &Images{Providers: []ImageProvider{PollinationsImage{BaseURL: srv.URL}}}
	b, _, err := im.Generate(context.Background(), "x", 64, 64)
	if err != nil || calls != 2 || len(b) == 0 {
		t.Fatalf("retry: err=%v calls=%d", err, calls)
	}
	var he *HTTPError
	err = readError(&http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"7"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"slow down"}}`))})
	if !errors.As(err, &he) || he.RetryAfter != 7*time.Second || !he.Retry() {
		t.Fatalf("parse retry-after: %#v", err)
	}
}

func TestImageEditors(t *testing.T) {
	src := tinyPNG()
	out := tinyPNG()
	srcURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(src)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v1/images/edits":
			imgs, _ := body["image"].([]any)
			if r.Header.Get("Authorization") != "Bearer pk" || len(imgs) != 1 || imgs[0].(map[string]any)["image_url"] != srcURI ||
				body["model"] != "black-forest-labs/flux.1-kontext-pro" || body["size"] != "832x1216" {
				t.Errorf("pollinations edit request: %v", body)
			}
			fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(out))
		case "/chat/completions":
			msgs := body["messages"].([]any)
			parts := msgs[0].(map[string]any)["content"].([]any)
			if len(parts) != 2 || parts[1].(map[string]any)["image_url"].(map[string]any)["url"] != srcURI {
				t.Errorf("openrouter edit content: %v", parts)
			}
			fmt.Fprintf(w, `{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,%s"}}]}}]}`, base64.StdEncoding.EncodeToString(out))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	refs := []Ref{{Data: src, Type: "image/png"}}
	ctx := context.Background()

	pe := PollinationsEditor{PollinationsImage: PollinationsImage{Key: "pk"}, GenURL: srv.URL}
	if b, err := pe.Edit(ctx, "make it chibi", refs, 832, 1216); err != nil || !bytes.Equal(b, out) {
		t.Fatalf("pollinations edit: %v", err)
	}
	oe := OpenRouterImage{Key: "k", Model: "m", BaseURL: srv.URL}
	if b, err := oe.Edit(ctx, "make it chibi", refs, 1024, 1024); err != nil || !bytes.Equal(b, out) {
		t.Fatalf("openrouter edit: %v", err)
	}
	// chuỗi: bên không biết sửa ảnh thì bỏ qua; không key thì không có editor
	im := &Images{Providers: []ImageProvider{PollinationsImage{}, pe}}
	if !im.CanEdit() {
		t.Fatal("CanEdit")
	}
	if _, name, err := im.Edit(ctx, "x", refs, 832, 1216); err != nil || name != "pollinations" {
		t.Fatalf("chain edit: %v %s", err, name)
	}
	if (&Images{Providers: []ImageProvider{PollinationsImage{}}}).CanEdit() {
		t.Fatal("keyless pollinations cannot edit")
	}
	_, im2 := FromEnv(func(k string) string { return map[string]string{"POLLINATIONS_API_KEY": "pk"}[k] })
	if !im2.CanEdit() {
		t.Fatal("pollinations key -> editor")
	}
}

func TestSearchAndPageText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html/":
			r.ParseForm()
			if r.Form.Get("q") != "giá vàng" {
				t.Errorf("ddg q = %q", r.Form.Get("q"))
			}
			fmt.Fprint(w, `<div><a rel="nofollow" class="result__a" href="https://duckduckgo.com/y.js?ad=1">Quảng cáo</a>
<a class="result__snippet" href="#">ad</a></div>
<div><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fsjc.com.vn%2Fgia&amp;rut=x">Giá <b>vàng</b> SJC</a>
<a class="result__snippet" href="#">Giá vàng hôm nay <b>139</b> triệu</a></div>`)
		case "/search":
			fmt.Fprint(w, `{"results":[{"title":"T","url":"https://t.vn","content":"nội dung đầy đủ"}]}`)
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<html><head><title>Trang</title><script>var x="<p>không lấy</p>"</script></head><body>
<nav><li>Menu trang chủ dài dòng không cần thiết ở đây</li></nav>
<h1>Giá vàng SJC ngày 18/11/2026</h1><p>Vàng miếng SJC bán ra 139 triệu đồng/lượng, giảm 1 triệu.</p>
<table><tr><td>Vàng nhẫn 9999 mua vào 137,5 triệu đồng</td></tr></table><p>ngắn</p></body></html>`)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	rs, err := DuckDuckGo{BaseURL: srv.URL}.Search(ctx, "giá vàng", 5)
	if err != nil || len(rs) != 1 || rs[0].URL != "https://sjc.com.vn/gia" || rs[0].Title != "Giá vàng SJC" || rs[0].Snippet != "Giá vàng hôm nay 139 triệu" {
		t.Fatalf("ddg: %v %+v", err, rs)
	}
	s := &Search{Providers: []Searcher{Tavily{Key: "k", BaseURL: srv.URL + "/broken"}, Tavily{Key: "k", BaseURL: srv.URL}}}
	if rs, name, err := s.Search(ctx, "x", 3); err != nil || name != "tavily" || rs[0].Content != "nội dung đầy đủ" {
		t.Fatalf("chain: %v %v", err, rs)
	}
	text, err := PageText(ctx, http.DefaultClient, srv.URL+"/page", 1000)
	if err != nil || !strings.Contains(text, "139 triệu đồng/lượng") || !strings.Contains(text, "137,5") ||
		strings.Contains(text, "không lấy") || strings.Contains(text, "Menu") || strings.Contains(text, "ngắn") {
		t.Fatalf("page text: %v %q", err, text)
	}
}

func TestDescribeSendsImages(t *testing.T) {
	var got []Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string            `json:"model"`
			Messages []json.RawMessage `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "vision-1" || !strings.Contains(string(req.Messages[0]), `"image_url"`) {
			t.Errorf("describe request: %s %s", req.Model, req.Messages[0])
		}
		for _, m := range req.Messages {
			var mm Message
			json.Unmarshal(m, &mm)
			got = append(got, mm)
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Một ngọn hải đăng đỏ\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c := NewChat(&Provider{Name: "noeyes", BaseURL: srv.URL, Model: "x"}, &Provider{Name: "eyes", BaseURL: srv.URL, Model: "chat-1", Vision: true, VisionModel: "vision-1"})
	text, res, err := c.Describe(context.Background(), "Mô tả ảnh", []Ref{{Data: tinyPNG(), Type: "image/png"}})
	if err != nil || text != "Một ngọn hải đăng đỏ" || res.Provider != "eyes" || got[0].Content != "Mô tả ảnh" {
		t.Fatalf("describe: %v %q %+v", err, text, got)
	}
	big := []Message{{Role: "user", Content: strings.Repeat("chữ ", 20000)}}
	if EstimateTokens(big) < 30000 {
		t.Fatal("estimate")
	}
}

func TestPageTextTables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<table><tr><th>Loại</th><th>Mua vào</th><th>Bán ra</th></tr>
<tr><td>Vàng SJC 1L</td><td>137.000</td><td>139.000</td></tr><tr><td>Chỉ chữ</td><td>không số</td></tr></table>`)
	}))
	defer srv.Close()
	text, err := PageText(context.Background(), http.DefaultClient, srv.URL, 1000)
	if err != nil || !strings.Contains(text, "Vàng SJC 1L | 137.000 | 139.000") || strings.Contains(text, "không số") {
		t.Fatalf("table rows: %v %q", err, text)
	}
}

func TestOutOfCredit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		fmt.Fprint(w, `{"error":{"message":"Insufficient balance. This request costs about $0.0300"}}`)
	}))
	defer srv.Close()
	refs := []Ref{{Data: tinyPNG(), Type: "image/png"}}
	im := &Images{Providers: []ImageProvider{PollinationsEditor{PollinationsImage: PollinationsImage{Key: "pk"}, GenURL: srv.URL}}}
	if _, _, err := im.Edit(context.Background(), "x", refs, 1024, 1024); !OutOfCredit(err) {
		t.Fatalf("edit 402 through chain: %v", err)
	}
	// "1 ảnh mỗi 45 giây, thử lại sau" cũng là 402 nhưng không phải hết tiền
	if OutOfCredit(&HTTPError{Status: 402, RetryAfter: 30 * time.Second}) {
		t.Fatal("rate limit is not out of credit")
	}
}

func TestCloudflareFlux2(t *testing.T) {
	pngData := tinyPNG()
	b64 := base64.StdEncoding.EncodeToString(pngData)
	var lastRefs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/accounts/acc/ai/run/@cf/black-forest-labs/flux-1-schnell" {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["prompt"] != "con mèo" {
				t.Errorf("schnell wants JSON, got %v", body)
			}
			fmt.Fprintf(w, `{"success":true,"result":{"image":%q}}`, b64)
			return
		}
		if r.URL.Path != "/accounts/acc/ai/run/"+cfDefaultImageModel || r.Header.Get("Authorization") != "Bearer t" {
			t.Errorf("flux-2 request: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("flux-2 wants multipart: %v", err)
		}
		if r.FormValue("prompt") != "con mèo" || r.FormValue("width") != "832" || r.FormValue("height") != "1216" {
			t.Errorf("fields: %v", r.MultipartForm.Value)
		}
		lastRefs = len(r.MultipartForm.File)
		for i := 0; i < lastRefs; i++ {
			fh := r.MultipartForm.File[fmt.Sprintf("input_image_%d", i)]
			if len(fh) != 1 || fh[0].Header.Get("Content-Type") != "image/png" {
				t.Errorf("input_image_%d: %v", i, fh)
			}
		}
		fmt.Fprintf(w, `{"success":true,"result":{"image":%q}}`, b64)
	}))
	defer srv.Close()
	ctx := context.Background()

	cf := CloudflareImage{AccountID: "acc", Token: "t", BaseURL: srv.URL}
	if b, err := cf.Generate(ctx, "con mèo", 832, 1216); err != nil || !bytes.Equal(b, pngData) || lastRefs != 0 {
		t.Fatalf("generate: %v refs=%d", err, lastRefs)
	}
	refs := make([]Ref, 6)
	for i := range refs {
		refs[i] = Ref{Data: pngData, Type: "image/png"}
	}
	if b, err := (CloudflareEditor{cf}).Edit(ctx, "con mèo", refs, 832, 1216); err != nil || !bytes.Equal(b, pngData) || lastRefs != cfMaxRefs {
		t.Fatalf("edit: %v refs=%d", err, lastRefs)
	}
	old := CloudflareImage{AccountID: "acc", Token: "t", BaseURL: srv.URL, Model: "@cf/black-forest-labs/flux-1-schnell"}
	if _, err := old.Generate(ctx, "con mèo", 832, 1216); err != nil {
		t.Fatalf("schnell: %v", err)
	}

	// có Cloudflare (FLUX.2) thì vẽ lại từ ảnh được kể cả khi không có key Pollinations
	env := map[string]string{"CF_ACCOUNT_ID": "acc", "CF_API_TOKEN": "t"}
	_, im := FromEnv(func(k string) string { return env[k] })
	if !im.CanEdit() || im.Names()[0] != "cloudflare" {
		t.Fatalf("cf editor: %v", im.Names())
	}
	env["CF_IMAGE_MODEL"] = "@cf/black-forest-labs/flux-1-schnell"
	if _, im = FromEnv(func(k string) string { return env[k] }); im.CanEdit() {
		t.Fatal("schnell cannot edit")
	}
}
