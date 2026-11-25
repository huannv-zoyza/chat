package muse

// Mặt ngoài tương thích OpenAI: /v1/models, /v1/chat/completions, /v1/images/generations.
// Mọi thứ bên dưới đi qua Client → gateway Muse.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ModelName là tên model duy nhất mà máy chủ này phơi ra.
const ModelName = "muse"

// Server là bộ định tuyến HTTP tương thích OpenAI.
type Server struct {
	C *Client
	// APIKey: nếu đặt, mọi yêu cầu phải mang Authorization: Bearer <key>.
	APIKey string
	// MaxImageBytes giới hạn ảnh trả về (mặc định 32 MiB).
	MaxImageBytes int
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", s.auth(s.models))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.chatCompletions))
	mux.HandleFunc("POST /v1/images/generations", s.auth(s.imagesGenerations))
	return mux
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.APIKey != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if strings.TrimSpace(got) != s.APIKey {
				apiErr(w, 401, "invalid_api_key", "khoá API sai hoặc thiếu")
				return
			}
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, code int, kind, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]any{"message": msg, "type": kind, "code": kind}})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"object": "list",
		"data": []any{map[string]any{
			"id": ModelName, "object": "model", "created": time.Now().Unix(), "owned_by": "muse",
		}},
	})
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
	// Lượt agent: tin assistant mang lệnh gọi công cụ, tin tool mang kết quả trả về.
	ToolCalls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
	ToolCallID string `json:"tool_call_id"`
}

// text gộp phần chữ của tin nhắn, chấp nhận cả dạng chuỗi lẫn dạng mảng phần tử của OpenAI.
func (m chatMessage) text() string {
	t, _ := m.parts()
	return t
}

// parts tách tin nhắn thành chữ và các ảnh đính kèm.
// OpenAI gửi ảnh dưới dạng {"type":"image_url","image_url":{"url":"data:image/png;base64,…"}}.
func (m chatMessage) parts() (string, []ImageRef) {
	switch v := m.Content.(type) {
	case string:
		return v, nil
	case []any:
		var b strings.Builder
		var imgs []ImageRef
		for _, p := range v {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch t, _ := pm["type"].(string); t {
			case "text":
				if s, ok := pm["text"].(string); ok {
					b.WriteString(s)
				}
			case "image_url":
				iu, _ := pm["image_url"].(map[string]any)
				if iu == nil {
					continue
				}
				if u, ok := iu["url"].(string); ok {
					if r, err := parseDataURI(u); err == nil {
						imgs = append(imgs, r)
					}
				}
			}
		}
		return b.String(), imgs
	}
	return "", nil
}

// parseDataURI đọc "data:<mime>;base64,<dữ liệu>". URL http(s) bị bỏ qua: cầu nối không tự đi tải
// ảnh từ bên ngoài (tránh biến máy chủ thành proxy lấy URL tuỳ ý).
func parseDataURI(u string) (ImageRef, error) {
	if !strings.HasPrefix(u, "data:") {
		return ImageRef{}, fmt.Errorf("chỉ nhận data: URI")
	}
	rest := u[len("data:"):]
	i := strings.Index(rest, ",")
	if i < 0 {
		return ImageRef{}, fmt.Errorf("data URI thiếu dấu phẩy")
	}
	meta, payload := rest[:i], rest[i+1:]
	if !strings.Contains(meta, "base64") {
		return ImageRef{}, fmt.Errorf("chỉ nhận base64")
	}
	mime := strings.TrimSuffix(meta, ";base64")
	if mime == "" {
		mime = "image/png"
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ImageRef{}, err
	}
	return ImageRef{Data: data, Mime: mime}, nil
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	User     string        `json:"user"`
	Tools    []openaiTool  `json:"tools"`
}

// museSessionHeader cho phép client nối tiếp đúng hội thoại Muse giữa các lần gọi.
const museSessionHeader = "X-Muse-Session"

// buildTurn quyết định gửi gì sang Muse.
// Có session sẵn → chỉ gửi tin nhắn người dùng cuối (Muse tự nhớ phần trước).
// Chưa có → gửi phẳng cả hội thoại để lần gọi không trạng thái vẫn đủ ngữ cảnh.
func buildTurn(req *chatRequest, hasSession bool) (string, []ImageRef) {
	if hasSession {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				return req.Messages[i].parts()
			}
		}
		return "", nil
	}
	var b strings.Builder
	var imgs []ImageRef
	for _, m := range req.Messages {
		t, mi := m.parts()
		imgs = append(imgs, mi...)
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		switch m.Role {
		case "system":
			b.WriteString(t + "\n\n")
		case "user":
			b.WriteString(t + "\n")
		case "assistant":
			b.WriteString("(trả lời trước đó) " + t + "\n")
		}
	}
	return strings.TrimSpace(b.String()), imgs
}

// buildAgentTurn dựng lại cả vòng lặp công cụ thành một lượt phẳng: Muse không giữ trạng thái
// giữa các lần gọi của package ai, nên mỗi lượt phải mang đủ lệnh đã gọi và kết quả đã có.
func buildAgentTurn(req *chatRequest) (string, []ImageRef) {
	var b strings.Builder
	var imgs []ImageRef
	for _, m := range req.Messages {
		t, mi := m.parts()
		imgs = append(imgs, mi...)
		t = strings.TrimSpace(t)
		switch {
		case m.Role == "system":
			if t != "" {
				b.WriteString(t + "\n\n")
			}
		case m.Role == "user":
			if t != "" {
				b.WriteString(t + "\n")
			}
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			for _, c := range m.ToolCalls {
				args := c.Function.Arguments
				if args == "" {
					args = "{}"
				}
				fmt.Fprintf(&b, "(bạn đã gọi công cụ %s với tham số %s)\n", c.Function.Name, trim(args, 600))
			}
		case m.Role == "assistant":
			if t != "" {
				b.WriteString("(trả lời trước đó) " + t + "\n")
			}
		case m.Role == "tool":
			fmt.Fprintf(&b, "(kết quả công cụ: %s)\n", trim(t, 12000))
		}
	}
	return strings.TrimSpace(b.String()), imgs
}

// turnItems dựng danh sách mục gửi sang Muse: ảnh trước, chữ sau (đúng thứ tự bản desktop dùng).
func turnItems(prompt string, imgs []ImageRef) []map[string]any {
	items := make([]map[string]any, 0, len(imgs)+1)
	for i, r := range imgs {
		mime := r.Mime
		if mime == "" {
			mime = "image/png"
		}
		items = append(items, ImageItem(mime, base64.StdEncoding.EncodeToString(r.Data), fmt.Sprintf("anh-%d%s", i+1, extFor(mime))))
	}
	return append(items, TextItem(prompt))
}

func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
		apiErr(w, 400, "invalid_request_error", "thân JSON không hợp lệ: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		apiErr(w, 400, "invalid_request_error", "thiếu messages")
		return
	}
	session := r.Header.Get(museSessionHeader)
	useTools := len(req.Tools) > 0
	var prompt string
	var imgs []ImageRef
	if useTools {
		// Vòng lặp công cụ gửi lại toàn bộ hội thoại mỗi lượt, nên không dùng session
		// (dùng thì Muse nhận ngữ cảnh hai lần).
		session = ""
		prompt, imgs = buildAgentTurn(&req)
		prompt += toolPrompt(req.Tools)
	} else {
		prompt, imgs = buildTurn(&req, session != "")
	}
	if strings.TrimSpace(prompt) == "" && len(imgs) == 0 {
		apiErr(w, 400, "invalid_request_error", "không có nội dung người dùng để gửi")
		return
	}
	id := "chatcmpl-" + strings.ReplaceAll(newUUID(), "-", "")
	created := time.Now().Unix()

	// Có công cụ thì phải đọc trọn câu trả lời rồi mới quyết định: JSON gọi công cụ thì
	// phát thành tool_calls, không thì mới là chữ. Streaming từng đoạn sẽ làm lộ JSON ra màn hình.
	streamText := req.Stream && !useTools

	var flusher http.Flusher
	sendChunk := func(delta map[string]any, finish any, extra map[string]any) {}
	if req.Stream {
		f, ok := w.(http.Flusher)
		if !ok {
			apiErr(w, 500, "server_error", "máy chủ không hỗ trợ streaming")
			return
		}
		flusher = f
		w.Header().Set("content-type", "text/event-stream; charset=utf-8")
		w.Header().Set("cache-control", "no-cache")
		w.Header().Set("connection", "keep-alive")
		w.WriteHeader(200)
		sendChunk = func(delta map[string]any, finish any, extra map[string]any) {
			payload := map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": ModelName,
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
			}
			for k, v := range extra {
				payload[k] = v
			}
			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		sendChunk(map[string]any{"role": "assistant", "content": ""}, nil, nil)
	}

	opt := AskOptions{SessionID: session}
	if streamText {
		opt.OnDelta = func(d string) { sendChunk(map[string]any{"content": d}, nil, nil) }
	}
	ans, err := s.C.Ask(r.Context(), turnItems(prompt, imgs), opt)
	if err != nil {
		if req.Stream {
			b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
			return
		}
		apiErr(w, 502, "upstream_error", err.Error())
		return
	}

	var call *toolCall
	if useTools {
		if tc, ok := parseToolCall(ans.Text, req.Tools); ok {
			call = &tc
		}
	}
	// Link nguồn Muse để riêng ngoài chữ: nối vào cuối câu trả lời (chỉ thêm, nên stream vẫn đúng).
	sources := ""
	if call == nil {
		sources = SourcesSuffix(ans.Text, ans.Citations)
	}

	if !req.Stream {
		msg := map[string]any{"role": "assistant", "content": ans.Text + sources}
		finish := "stop"
		if call != nil {
			msg["content"] = nil
			msg["tool_calls"] = []any{map[string]any{
				"id": "call_" + strings.ReplaceAll(newUUID(), "-", "")[:16], "type": "function",
				"function": map[string]any{"name": call.Name, "arguments": call.Args},
			}}
			finish = "tool_calls"
		}
		w.Header().Set(museSessionHeader, ans.SessionID)
		writeJSON(w, 200, map[string]any{
			"id": id, "object": "chat.completion", "created": created, "model": ModelName,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
			"usage":   map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
			"muse": map[string]any{
				"session_id": ans.SessionID, "files": ans.Files, "status": ans.Status,
				"images": s.inlineImages(r.Context(), ans),
			},
		})
		return
	}

	switch {
	case call != nil:
		sendChunk(map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "call_" + strings.ReplaceAll(newUUID(), "-", "")[:16], "type": "function",
			"function": map[string]any{"name": call.Name, "arguments": call.Args},
		}}}, nil, nil)
		sendChunk(map[string]any{}, "tool_calls", nil)
	default:
		if !streamText && ans.Text != "" {
			sendChunk(map[string]any{"content": ans.Text}, nil, nil)
		}
		if sources != "" {
			sendChunk(map[string]any{"content": sources}, nil, nil)
		}
		// Muse vừa viết vừa vẽ được: ảnh sinh ra trong lượt này đi kèm khung cuối, dưới trường
		// mở rộng "muse" (client nào không biết thì bỏ qua, đúng tinh thần SSE của OpenAI).
		if imgs := s.inlineImages(r.Context(), ans); len(imgs) > 0 {
			sendChunk(map[string]any{}, nil, map[string]any{"muse": map[string]any{"images": imgs, "session_id": ans.SessionID}})
		}
		sendChunk(map[string]any{}, "stop", nil)
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

type imageRequest struct {
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
	Model          string `json:"model"`
}

var imageMimes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

func looksLikeImage(f File) bool {
	if imageMimes[strings.ToLower(f.Mime)] {
		return true
	}
	p := strings.ToLower(f.Path)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".gif"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

func (s *Server) imagesGenerations(w http.ResponseWriter, r *http.Request) {
	var req imageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		apiErr(w, 400, "invalid_request_error", "thân JSON không hợp lệ: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		apiErr(w, 400, "invalid_request_error", "thiếu prompt")
		return
	}
	prompt := req.Prompt
	if req.Size != "" && req.Size != "auto" {
		prompt += "\n(kích thước mong muốn: " + req.Size + ")"
	}
	width, height, _ := ParseSize(req.Size)
	n := req.N
	if n < 1 {
		n = 1
	}
	if n > 1 {
		// Muse trả một ảnh mỗi lượt; nhiều ảnh phải hỏi nhiều lượt, tốn thời gian gấp n lần.
		apiErr(w, 400, "invalid_request_error", "Muse chỉ vẽ một ảnh mỗi lượt — dùng n=1")
		return
	}
	data, ctype, session, err := s.C.Image(r.Context(), req.Prompt, width, height)
	if err != nil {
		apiErr(w, 502, "upstream_error", err.Error())
		return
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	item := map[string]any{"revised_prompt": req.Prompt}
	if req.ResponseFormat == "url" {
		item["url"] = "data:" + ctype + ";base64," + b64
	} else {
		item["b64_json"] = b64
	}
	out := []any{item}
	w.Header().Set(museSessionHeader, session)
	writeJSON(w, 200, map[string]any{"created": time.Now().Unix(), "data": out})
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ParseSize đọc "1024x1024" (dùng khi cần suy ra tỉ lệ).
func ParseSize(s string) (int, int, bool) {
	p := strings.SplitN(strings.ToLower(s), "x", 2)
	if len(p) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(p[0])
	h, err2 := strconv.Atoi(p[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// b64Decode giải mã base64 chuẩn (dùng lại trong test).
func b64Decode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

// inlineImages tải các ảnh Muse sinh ra trong lượt này và trả về dạng base64 để gửi kèm phản hồi.
// Ảnh hỏng hoặc quá lớn thì bỏ qua, không làm hỏng cả câu trả lời.
func (s *Server) inlineImages(ctx context.Context, ans *Answer) []map[string]any {
	limit := s.MaxImageBytes
	if limit <= 0 {
		limit = 32 << 20
	}
	var out []map[string]any
	for _, f := range ans.Files {
		if !looksLikeImage(f) {
			continue
		}
		ctype, data, err := s.C.FetchFile(ctx, f.Path, limit)
		if err != nil || len(data) == 0 {
			continue
		}
		if ctype == "" {
			ctype = f.Mime
		}
		if ctype == "" {
			ctype = "image/png"
		}
		out = append(out, map[string]any{"mime": ctype, "b64": base64.StdEncoding.EncodeToString(data)})
	}
	return out
}
