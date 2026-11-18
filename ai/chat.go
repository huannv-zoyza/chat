// Package ai: gọi các dịch vụ AI bên ngoài (chat và tạo ảnh).
//
// Chat: mọi nhà cung cấp đều có API dạng OpenAI (/chat/completions, stream SSE), nên dùng
// chung một client. Nhiều nhà cung cấp xếp thành chuỗi: bên trước lỗi (hết lượt miễn phí,
// quá tải) trước khi kịp trả chữ nào thì thử bên sau.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Message struct {
	Role    string // system | user | assistant
	Content string
	// Images: ảnh gửi kèm (chỉ gửi cho model đọc được ảnh).
	Images []Ref
}

// MarshalJSON: có ảnh thì content là mảng [text, image_url…] theo định dạng OpenAI.
func (m Message) MarshalJSON() ([]byte, error) {
	if len(m.Images) == 0 {
		return json.Marshal(map[string]string{"role": m.Role, "content": m.Content})
	}
	parts := []map[string]any{{"type": "text", "text": m.Content}}
	for _, r := range m.Images {
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": r.dataURI()}})
	}
	return json.Marshal(map[string]any{"role": m.Role, "content": parts})
}

func (m *Message) UnmarshalJSON(b []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	if err := json.Unmarshal(raw.Content, &m.Content); err != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		json.Unmarshal(raw.Content, &parts)
		for _, p := range parts {
			m.Content += p.Text
		}
	}
	return nil
}

// EstimateTokens: ước lượng số token đầu vào (tiếng Việt ~2.5 ký tự/token, mỗi ảnh ~1000).
func EstimateTokens(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += len([]rune(m.Content))*2/5 + 1000*len(m.Images)
	}
	return n
}

// Provider: một nhà cung cấp chat dạng OpenAI.
type Provider struct {
	Name    string // hiển thị và ghi log, ví dụ "gemini"
	BaseURL string // tới trước /chat/completions, ví dụ https://api.groq.com/openai/v1
	Key     string // rỗng: không gửi Authorization (Pollinations)
	Model   string // rỗng: tự chọn từ /models theo Prefer
	// Prefer: các chuỗi con ưu tiên khi tự chọn model (theo thứ tự), Avoid: bỏ qua model chứa chuỗi này.
	Prefer []string
	Avoid  []string
	Header map[string]string
	// MaxInput: số token đầu vào tối đa nên gửi (0 = không giới hạn). Gói miễn phí của Groq chỉ
	// cho ~8000 token/phút nên ngữ cảnh lớn chuyển sang bên khác.
	MaxInput int
	// Vision: đọc được ảnh; VisionModel: model dùng cho ảnh (rỗng = model chat).
	Vision      bool
	VisionModel string

	mu     sync.Mutex
	picked string
	pickAt time.Time
}

// Presets: cấu hình sẵn cho các nhà cung cấp hay dùng (key để trống, main điền vào).
func Gemini(key, model string) *Provider {
	return &Provider{
		Name: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Key: key, Model: model,
		Vision: true,
		Prefer: []string{"flash"},
		Avoid:  []string{"lite", "image", "tts", "audio", "live", "embedding", "aqa", "thinking-exp", "learnlm", "gemma", "veo", "imagen"},
	}
}

func Groq(key, model string) *Provider {
	return &Provider{
		Name: "groq", BaseURL: "https://api.groq.com/openai/v1", Key: key, Model: model, MaxInput: 5500,
		Prefer: []string{"gpt-oss-120b", "llama-3.3-70b", "llama-4", "qwen", "gpt-oss-20b"},
		Avoid:  []string{"whisper", "guard", "tts", "playai", "compound", "prompt-guard"},
	}
}

func OpenRouter(key, model string) *Provider {
	if model == "" {
		model = "openrouter/free" // bộ định tuyến tự chọn một model miễn phí đang chạy
	}
	return &Provider{
		Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Key: key, Model: model,
		Header: map[string]string{"X-Title": "Go Chat"},
		Vision: true, VisionModel: "openrouter/free", // bộ định tuyến free có model nhận ảnh
	}
}

// PollinationsKey: API mới gen.pollinations.ai (cần key "pollen", tính phí rất rẻ theo token).
// Mặc định deepseek-v4.1-flash: ngữ cảnh 1 triệu token, đọc được ảnh.
func PollinationsKey(key, model string) *Provider {
	if model == "" {
		model = "deepseek/deepseek-v4.1-flash"
	}
	return &Provider{Name: "pollinations", BaseURL: "https://gen.pollinations.ai/v1", Key: key, Model: model, Vision: true}
}

func Pollinations(model string) *Provider {
	if model == "" {
		model = "openai"
	}
	return &Provider{Name: "pollinations", BaseURL: "https://text.pollinations.ai/openai", Model: model}
}

// Chat: chuỗi nhà cung cấp.
type Chat struct {
	Providers []*Provider
	HTTP      *http.Client
	MaxTokens int
}

func NewChat(providers ...*Provider) *Chat {
	return &Chat{Providers: providers, HTTP: &http.Client{Timeout: 3 * time.Minute}, MaxTokens: 1800}
}

// Names: tên các nhà cung cấp theo thứ tự thử.
func (c *Chat) Names() []string {
	var out []string
	for _, p := range c.Providers {
		out = append(out, p.Name)
	}
	return out
}

// Result: model đã trả lời ("gemini/gemini-2.5-flash").
type Result struct {
	Provider string
	Model    string
}

// Stream gửi hội thoại, gọi onDelta cho từng đoạn chữ. Bên đầu tiên lỗi trước khi trả chữ nào
// thì chuyển sang bên tiếp theo; đã trả chữ rồi mà lỗi giữa chừng thì báo lỗi luôn.
func (c *Chat) Stream(ctx context.Context, msgs []Message, onDelta func(string)) (Result, error) {
	var errs []string
	need := EstimateTokens(msgs)
	for i, p := range c.Providers {
		// ngữ cảnh quá lớn cho bên này (vẫn thử nếu là bên cuối cùng)
		if p.MaxInput > 0 && need > p.MaxInput && i < len(c.Providers)-1 {
			continue
		}
		got := false
		model, err := c.streamOne(ctx, p, msgs, func(s string) {
			got = true
			onDelta(s)
		})
		// quá tải / giới hạn tốc độ: đợi rồi thử lại bên này một lần nữa
		if d, ok := retryWait(err, 15*time.Second); ok && !got && sleepCtx(ctx, d) {
			model, err = c.streamOne(ctx, p, msgs, func(s string) {
				got = true
				onDelta(s)
			})
		}
		if err == nil {
			return Result{Provider: p.Name, Model: model}, nil
		}
		if got || ctx.Err() != nil {
			return Result{Provider: p.Name, Model: model}, err
		}
		errs = append(errs, p.Name+": "+err.Error())
	}
	if len(errs) == 0 {
		return Result{}, errors.New("chưa cấu hình nhà cung cấp AI nào")
	}
	return Result{}, errors.New(strings.Join(errs, "; "))
}

// Complete: như Stream nhưng trả về cả câu.
func (c *Chat) Complete(ctx context.Context, msgs []Message) (string, error) {
	var b strings.Builder
	_, err := c.Stream(ctx, msgs, func(s string) { b.WriteString(s) })
	return strings.TrimSpace(b.String()), err
}

type apiError struct {
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func (c *Chat) streamOne(ctx context.Context, p *Provider, msgs []Message, onDelta func(string)) (string, error) {
	model, err := c.model(ctx, p)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{
		"model": model, "messages": msgs, "stream": true, "max_tokens": c.MaxTokens, "temperature": 0.6,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return model, err
	}
	p.headers(req)
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return model, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return model, readError(res)
	}

	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, ":") { // dòng trống / chú thích keep-alive
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return model, nil
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			apiError
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			return model, errors.New(chunk.Error.Message)
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				onDelta(ch.Delta.Content)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return model, err
	}
	return model, nil
}

func (p *Provider) headers(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.Key != "" {
		req.Header.Set("Authorization", "Bearer "+p.Key)
	}
	for k, v := range p.Header {
		req.Header.Set(k, v)
	}
}

// HTTPError: dịch vụ trả mã lỗi HTTP. Retry: nên thử lại (quá tải / giới hạn tốc độ), sau RetryAfter.
type HTTPError struct {
	Status     int
	Msg        string
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg) }

func (e *HTTPError) Retry() bool {
	switch e.Status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway:
		return true
	case http.StatusPaymentRequired: // Pollinations: "Free limit reached… Retry after 14s"
		return e.RetryAfter > 0
	}
	return false
}

func readError(res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
	e := &HTTPError{Status: res.StatusCode}
	if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s > 0 {
		e.RetryAfter = time.Duration(s) * time.Second
	}
	var one apiError
	var arr []apiError // Gemini đôi khi trả mảng [{error: ...}]
	switch {
	case json.Unmarshal(b, &one) == nil && one.Error != nil && one.Error.Message != "":
		e.Msg = one.Error.Message
	case json.Unmarshal(b, &arr) == nil && len(arr) > 0 && arr[0].Error != nil:
		e.Msg = arr[0].Error.Message
	default:
		e.Msg = strings.TrimSpace(string(b))
	}
	if len(e.Msg) > 300 {
		e.Msg = e.Msg[:300]
	}
	// "Retry after 10s" trong nội dung (Pollinations)
	if m := retryInText.FindStringSubmatch(e.Msg); m != nil && e.RetryAfter == 0 {
		if s, err := strconv.Atoi(m[1]); err == nil {
			e.RetryAfter = time.Duration(s) * time.Second
		}
	}
	return e
}

var retryInText = regexp.MustCompile(`(?i)retry after (\d+)\s*s`)

// retryWait: lỗi có nên thử lại không, và đợi bao lâu (không quá limit).
func retryWait(err error, limit time.Duration) (time.Duration, bool) {
	var he *HTTPError
	if !errors.As(err, &he) || !he.Retry() {
		return 0, false
	}
	d := he.RetryAfter
	if d <= 0 {
		if he.Status == http.StatusTooManyRequests {
			return 0, false // hết hạn mức, không nói đợi bao lâu: chuyển sang bên khác luôn
		}
		d = 3 * time.Second
	}
	if d > limit {
		return 0, false
	}
	return d + 500*time.Millisecond, true
}

// sleepCtx đợi d hoặc tới khi ctx bị huỷ.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// model: model cấu hình sẵn, hoặc tự chọn từ /models (nhớ 6 giờ) để không phụ thuộc tên model cũ.
func (c *Chat) model(ctx context.Context, p *Provider) (string, error) {
	if p.Model != "" {
		return p.Model, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.picked != "" && time.Since(p.pickAt) < 6*time.Hour {
		return p.picked, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/models", nil)
	if err != nil {
		return "", err
	}
	p.headers(req)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", readError(res)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&list); err != nil {
		return "", err
	}
	var ids []string
	for _, m := range list.Data {
		ids = append(ids, strings.TrimPrefix(m.ID, "models/"))
	}
	id := pickModel(ids, p.Prefer, p.Avoid)
	if id == "" {
		return "", errors.New("không tìm được model phù hợp")
	}
	p.picked, p.pickAt = id, time.Now()
	return id, nil
}

// pickModel: model đầu tiên khớp chuỗi ưu tiên sớm nhất; cùng mức thì lấy tên "mới" nhất
// (sắp xếp giảm dần, ví dụ gemini-3-flash trước gemini-2.5-flash). Bản preview/exp xếp sau bản chính.
func pickModel(ids, prefer, avoid []string) string {
	var ok []string
	for _, id := range ids {
		l := strings.ToLower(id)
		bad := false
		for _, a := range avoid {
			if strings.Contains(l, a) {
				bad = true
				break
			}
		}
		if !bad {
			ok = append(ok, id)
		}
	}
	unstable := func(s string) bool {
		s = strings.ToLower(s)
		return strings.Contains(s, "preview") || strings.Contains(s, "exp")
	}
	sort.Slice(ok, func(i, j int) bool {
		if unstable(ok[i]) != unstable(ok[j]) {
			return !unstable(ok[i])
		}
		return ok[i] > ok[j]
	})
	for _, want := range prefer {
		for _, id := range ok {
			if strings.Contains(strings.ToLower(id), want) {
				return id
			}
		}
	}
	if len(ok) > 0 {
		return ok[0]
	}
	return ""
}

// Describe: nhờ model đọc ảnh mô tả / trả lời về các ảnh. Thử các bên có Vision theo thứ tự.
func (c *Chat) Describe(ctx context.Context, prompt string, refs []Ref) (string, Result, error) {
	var errs []string
	for _, p := range c.Providers {
		if !p.Vision {
			continue
		}
		model := p.VisionModel
		if model == "" {
			m, err := c.model(ctx, p)
			if err != nil {
				errs = append(errs, p.Name+": "+err.Error())
				continue
			}
			model = m
		}
		vp := &Provider{Name: p.Name, BaseURL: p.BaseURL, Key: p.Key, Model: model, Header: p.Header}
		var b strings.Builder
		model, err := c.streamOne(ctx, vp, []Message{{Role: "user", Content: prompt, Images: refs}}, func(s string) { b.WriteString(s) })
		if err == nil && strings.TrimSpace(b.String()) != "" {
			return strings.TrimSpace(b.String()), Result{Provider: p.Name, Model: model}, nil
		}
		if err == nil {
			err = errors.New("không trả lời")
		}
		if ctx.Err() != nil {
			return "", Result{}, ctx.Err()
		}
		errs = append(errs, p.Name+": "+err.Error())
	}
	if len(errs) == 0 {
		return "", Result{}, errors.New("không có model nào đọc được ảnh")
	}
	return "", Result{}, errors.New(strings.Join(errs, "; "))
}

// CanSee: có model đọc được ảnh.
func (c *Chat) CanSee() bool {
	for _, p := range c.Providers {
		if p.Vision {
			return true
		}
	}
	return false
}
