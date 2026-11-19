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
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Message struct {
	Role    string // system | user | assistant | tool
	Content string
	// Images: ảnh gửi kèm (chỉ gửi cho model đọc được ảnh).
	Images []Ref
	// ToolCalls: các lệnh gọi công cụ của model (tin assistant); ToolCallID: tin "tool" trả kết quả cho lệnh nào.
	ToolCalls  []ToolCall
	ToolCallID string
}

// Tool: một công cụ model được gọi (function calling dạng OpenAI). Params: JSON Schema của tham số.
type Tool struct {
	Name        string
	Description string
	Params      map[string]any
}

func (t Tool) MarshalJSON() ([]byte, error) {
	params := t.Params
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return json.Marshal(map[string]any{"type": "function", "function": map[string]any{
		"name": t.Name, "description": t.Description, "parameters": params,
	}})
}

// ToolCall: model nhờ gọi công cụ Name với tham số Args (chuỗi JSON).
type ToolCall struct {
	ID   string
	Name string
	Args string
}

// MarshalJSON: có ảnh thì content là mảng [text, image_url…] theo định dạng OpenAI.
func (m Message) MarshalJSON() ([]byte, error) {
	switch {
	case m.Role == "tool":
		return json.Marshal(map[string]string{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Content})
	case len(m.ToolCalls) > 0:
		calls := make([]map[string]any, len(m.ToolCalls))
		for i, c := range m.ToolCalls {
			args := c.Args
			if args == "" {
				args = "{}"
			}
			calls[i] = map[string]any{"id": c.ID, "type": "function", "function": map[string]string{"name": c.Name, "arguments": args}}
		}
		var content any // null khi chỉ gọi công cụ
		if m.Content != "" {
			content = m.Content
		}
		return json.Marshal(map[string]any{"role": m.Role, "content": content, "tool_calls": calls})
	case len(m.Images) == 0:
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
		for _, c := range m.ToolCalls {
			n += len([]rune(c.Args))*2/5 + 10
		}
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
	// NoTools: không gọi công cụ được (bị bỏ qua khi chạy agent).
	NoTools bool

	mu     sync.Mutex
	picked string
	pickAt time.Time
	// pausedUntil: hết tiền / hết hạn mức ngày thì bỏ qua bên này tới lúc đó
	pausedUntil time.Time
}

// creditPause: bên báo hết tiền thì tạm bỏ qua chừng này (đỡ tốn một lượt gọi chắc chắn lỗi).
const creditPause = 30 * time.Minute

func (p *Provider) paused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Now().Before(p.pausedUntil)
}

func (p *Provider) markBroke() {
	p.pauseFor(creditPause)
}

func (p *Provider) pauseFor(d time.Duration) {
	p.mu.Lock()
	if until := time.Now().Add(d); until.After(p.pausedUntil) {
		p.pausedUntil = until
	}
	p.mu.Unlock()
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
	return &Provider{Name: "pollinations", BaseURL: "https://text.pollinations.ai/openai", Model: model, NoTools: true}
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
	res, _, err := c.StreamTools(ctx, msgs, nil, onDelta)
	return res, err
}

// StreamTools: như Stream nhưng model được gọi công cụ. Trả về chữ (qua onDelta) và/hoặc các lệnh gọi
// công cụ của lượt này. Có tools thì chỉ dùng các bên gọi công cụ được.
func (c *Chat) StreamTools(ctx context.Context, msgs []Message, tools []Tool, onDelta func(string)) (Result, []ToolCall, error) {
	var errs []string
	need := EstimateTokens(msgs)
	if len(tools) > 0 {
		b, _ := json.Marshal(tools)
		need += len(b) / 3
	}
	usable := make([]*Provider, 0, len(c.Providers))
	for _, p := range c.Providers {
		if (len(tools) == 0 || !p.NoTools) && !p.paused() {
			usable = append(usable, p)
		}
	}
	for i, p := range usable {
		// ngữ cảnh quá lớn cho bên này (vẫn thử nếu là bên cuối cùng)
		if p.MaxInput > 0 && need > p.MaxInput && i < len(usable)-1 {
			errs = append(errs, fmt.Sprintf("%s/%s: ngữ cảnh %d token > %d", p.Name, p.Model, need, p.MaxInput))
			continue
		}
		got := false
		emit := func(s string) {
			got = true
			onDelta(s)
		}
		model, calls, err := c.streamOne(ctx, p, msgs, tools, emit)
		// quá tải / giới hạn tốc độ: đợi rồi thử lại bên này một lần nữa
		if d, ok := retryWait(err, 15*time.Second); ok && !got && sleepCtx(ctx, d) {
			model, calls, err = c.streamOne(ctx, p, msgs, tools, emit)
		}
		if err == nil {
			if len(errs) > 0 {
				log.Printf("ai: bỏ qua %s", strings.Join(errs, "; "))
			}
			return Result{Provider: p.Name, Model: model}, calls, nil
		}
		if got || ctx.Err() != nil {
			return Result{Provider: p.Name, Model: model}, nil, err
		}
		if OutOfCredit(err) {
			p.markBroke()
		} else if d := quotaWait(err); d > 0 {
			p.pauseFor(d) // hết hạn mức ngày (Groq: "tokens per day"): khỏi gửi thử mỗi lần
		}
		errs = append(errs, p.Name+"/"+p.Model+": "+err.Error())
	}
	if len(errs) == 0 && len(usable) == 0 && len(c.Providers) > 0 {
		return Result{}, nil, errors.New("các dịch vụ AI đều đang hết hạn mức, thử lại sau")
	}
	if len(errs) == 0 {
		return Result{}, nil, errors.New("chưa cấu hình nhà cung cấp AI nào")
	}
	return Result{}, nil, errors.New(strings.Join(errs, "; "))
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

func (c *Chat) streamOne(ctx context.Context, p *Provider, msgs []Message, tools []Tool, onDelta func(string)) (string, []ToolCall, error) {
	model, err := c.model(ctx, p)
	if err != nil {
		return "", nil, err
	}
	reqBody := map[string]any{
		"model": model, "messages": msgs, "stream": true, "max_tokens": c.MaxTokens, "temperature": 0.6,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return model, nil, err
	}
	p.headers(req)
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return model, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return model, nil, readError(res)
	}
	// lệnh gọi công cụ đến từng mảnh theo index: gom lại
	var calls []ToolCall
	addCall := func(i int, id, name, args string) {
		for len(calls) <= i {
			calls = append(calls, ToolCall{})
		}
		if id != "" {
			calls[i].ID = id
		}
		calls[i].Name += name
		calls[i].Args += args
	}
	finish := func() []ToolCall {
		out := calls[:0]
		for i, c := range calls {
			if c.Name == "" {
				continue
			}
			if c.ID == "" {
				c.ID = fmt.Sprintf("call_%d", i)
			}
			out = append(out, c)
		}
		return out
	}

	// Vài bên (Pollinations) báo hết tiền bằng HTTP 200 kèm câu trả lời "doesn't have enough credits…".
	// Giữ lại đoạn đầu để kiểm tra trước khi gửi cho người dùng; là thông báo đó thì coi như lỗi 402
	// để chuỗi chuyển sang bên tiếp theo.
	var head strings.Builder
	held := true
	emit := func(s string) error {
		if !held {
			onDelta(s)
			return nil
		}
		head.WriteString(s)
		if creditNotice(head.String()) {
			return &HTTPError{Status: http.StatusPaymentRequired, Msg: "hết credit (trả lời 200 kèm thông báo nạp tiền)"}
		}
		if head.Len() >= creditHold {
			held = false
			onDelta(head.String())
		}
		return nil
	}
	flush := func() {
		if held && head.Len() > 0 {
			onDelta(head.String())
		}
		held = false
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
			flush()
			return model, finish(), nil
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    *int   `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			apiError
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			return model, nil, errors.New(chunk.Error.Message)
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				if err := emit(ch.Delta.Content); err != nil {
					return model, nil, err
				}
			}
			for j, tc := range ch.Delta.ToolCalls {
				i := len(calls) + j // không có index: mỗi mảnh là một lệnh trọn vẹn
				if tc.Index != nil {
					i = *tc.Index
				}
				addCall(i, tc.ID, tc.Function.Name, tc.Function.Arguments)
			}
		}
	}
	if err := sc.Err(); err != nil {
		flush()
		return model, nil, err
	}
	flush()
	return model, finish(), nil
}

// creditHold: số byte đầu giữ lại để nhận ra thông báo hết tiền (thông báo dài ~300 byte,
// dấu hiệu nằm ngay câu đầu).
const creditHold = 160

var creditNoticeRe = regexp.MustCompile(`(?i)enter\.pollinations\.ai/(top-up|quests)|doesn['’]t have enough credits`)

func creditNotice(s string) bool { return creditNoticeRe.MatchString(s) }

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
	if m := tryAgainIn.FindStringSubmatch(e.Msg); m != nil && m[1] != "" && e.RetryAfter == 0 {
		if d, err := time.ParseDuration(m[1]); err == nil {
			e.RetryAfter = d.Round(time.Second)
		}
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

// "Please try again in 10m29.424s" (Groq, khi hết hạn mức token/ngày)
var tryAgainIn = regexp.MustCompile(`(?i)try again in ((?:\d+h)?(?:\d+m)?(?:[\d.]+s)?)`)

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

// quotaWait: bên này bảo đợi lâu (hơn 1 phút, tối đa 1 giờ) mới thử lại được.
func quotaWait(err error) time.Duration {
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusTooManyRequests || he.RetryAfter <= time.Minute {
		return 0
	}
	return min(he.RetryAfter, time.Hour)
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
				errs = append(errs, p.Name+"/"+p.Model+": "+err.Error())
				continue
			}
			model = m
		}
		vp := &Provider{Name: p.Name, BaseURL: p.BaseURL, Key: p.Key, Model: model, Header: p.Header}
		var b strings.Builder
		model, _, err := c.streamOne(ctx, vp, []Message{{Role: "user", Content: prompt, Images: refs}}, nil, func(s string) { b.WriteString(s) })
		if err == nil && strings.TrimSpace(b.String()) != "" {
			return strings.TrimSpace(b.String()), Result{Provider: p.Name, Model: model}, nil
		}
		if err == nil {
			err = errors.New("không trả lời")
		}
		if ctx.Err() != nil {
			return "", Result{}, ctx.Err()
		}
		errs = append(errs, p.Name+"/"+p.Model+": "+err.Error())
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
