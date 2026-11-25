package muse

// Lớp dùng được ngay: giữ một kết nối gateway, gửi một lượt hỏi và chờ câu trả lời.
// Quy tắc đọc lịch sử cổng từ desktop/src/shared/gateway/events.ts (normaliseHistory, turnLooksDone).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Trạng thái kết thúc và đang chạy của một lượt, giữ đúng danh sách bên TypeScript.
var (
	terminalStatus = map[string]bool{"completed": true, "failed": true, "cancelled": true, "canceled": true, "error": true}
	runningStatus  = map[string]bool{"running": true, "in_progress": true, "queued": true, "pending": true, "started": true, "working": true}
)

// File là một tệp Muse sinh ra, nằm trong workspace của máy ảo.
type File struct {
	Path string `json:"path"`
	Mime string `json:"mime,omitempty"`
	Name string `json:"name,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// Answer là kết quả một lượt hỏi.
type Answer struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Files     []File `json:"files,omitempty"`
	Status    string `json:"status,omitempty"`
}

// Client giữ phiên muse.ai và một kênh gateway dùng lại được.
type Client struct {
	s  *Session
	mu sync.Mutex
	gw *Gateway
}

func New(cfg Config) (*Client, error) {
	s, err := NewSession(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{s: s}, nil
}

// Gateway trả kênh đang mở, tự kết nối lại nếu đã đóng.
func (c *Client) Gateway(ctx context.Context) (*Gateway, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gw != nil && c.gw.IsOpen() {
		return c.gw, nil
	}
	if c.gw != nil {
		c.gw.Close()
		c.gw = nil
	}
	gw, err := c.s.Connect(ctx)
	if err != nil {
		return nil, err
	}
	c.gw = gw
	return gw, nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gw != nil {
		c.gw.Close()
		c.gw = nil
	}
}

// TextItem dựng một mục văn bản cho lượt gửi.
func TextItem(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

// ImageItem đính kèm một ảnh dạng base64 (đúng cách web app của Muse gửi ảnh).
func ImageItem(mime, b64, filename string) map[string]any {
	return map[string]any{"type": "image", "mime_type": mime, "data_base64": b64, "filename": filename}
}

// AskOptions điều chỉnh một lượt hỏi.
type AskOptions struct {
	// SessionID rỗng = mở hội thoại mới.
	SessionID string
	// Timeout cho cả lượt (mặc định 20 phút, bằng bản desktop).
	Timeout time.Duration
	// OnDelta nhận phần văn bản mới mỗi khi lịch sử dài thêm (dùng cho chế độ stream).
	OnDelta func(delta string)
	// PollEvery là nhịp đọc lịch sử (mặc định 1,5 giây).
	PollEvery time.Duration
}

// Ask gửi một lượt và chờ tới khi Muse trả lời xong.
func (c *Client) Ask(ctx context.Context, items []map[string]any, opt AskOptions) (*Answer, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("muse: lượt gửi rỗng")
	}
	if opt.Timeout == 0 {
		opt.Timeout = 20 * time.Minute
	}
	if opt.PollEvery == 0 {
		opt.PollEvery = 1500 * time.Millisecond
	}
	sessionID := opt.SessionID
	if sessionID == "" {
		sessionID = newUUID()
	}
	gw, err := c.Gateway(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	sub, err := gw.Subscribe(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	defer gw.forget(sub.id)

	send, err := gw.SendChat(ctx, sessionID, items, newUUID())
	if err != nil {
		return nil, err
	}
	defer gw.forget(send.id)

	// Chờ Muse xác nhận đã nhận lượt; bản ghi cuối của luồng gửi là ack.
	select {
	case <-send.fin:
	case <-time.After(90 * time.Second):
		return nil, fmt.Errorf("muse: Muse không xác nhận lượt gửi")
	case <-gw.closed:
		return nil, errClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	send.mu.Lock()
	ack := send.last
	ackReset := send.reset
	send.mu.Unlock()
	if ackReset != nil && ackReset.Code != 0 {
		return nil, fmt.Errorf("muse: luồng gửi bị cắt (%d %s)", ackReset.Code, ackReset.Reason)
	}
	if ack != nil {
		if e, ok := ack["error"]; ok && e != nil {
			return nil, fmt.Errorf("muse từ chối lượt gửi: %v", e)
		}
		if sid, ok := ack["session_id"].(string); ok && sid != "" {
			sessionID = sid
		}
	}

	// Theo dõi sự kiện để biết lúc xong, đồng thời đọc lịch sử để lấy văn bản (và phát delta).
	sentText := ""
	done := false
	tick := time.NewTicker(opt.PollEvery)
	defer tick.Stop()
	deadline := time.After(opt.Timeout)

	for {
		select {
		case rec := <-sub.recs:
			switch eventName(rec) {
			case "task.status":
				if st := payloadStatus(rec); st != "" && terminalStatus[st] {
					done = true
				}
			case "delta.message_done":
				done = true
			case "error":
				if msg := errorMessage(rec); msg != "" {
					return nil, fmt.Errorf("muse báo lỗi: %s", msg)
				}
			}
		case <-tick.C:
		case <-deadline:
			return nil, fmt.Errorf("muse: quá thời gian chờ trả lời")
		case <-gw.closed:
			return nil, errClosed
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		h, err := c.History(ctx, sessionID, 50)
		if err != nil {
			continue
		}
		turn := lastTurn(h)
		if turn == nil {
			continue
		}
		if opt.OnDelta != nil && len(turn.Text) > len(sentText) && strings.HasPrefix(turn.Text, sentText) {
			d := turn.Text[len(sentText):]
			sentText = turn.Text
			opt.OnDelta(d)
		} else if len(turn.Text) > 0 && !strings.HasPrefix(turn.Text, sentText) {
			sentText = turn.Text // Muse viết lại đoạn trả lời: lần sau phát tiếp từ đây
		}
		if done || turnLooksDone(turn) {
			return &Answer{SessionID: sessionID, Text: turn.Text, Files: turn.Files, Status: turn.Status}, nil
		}
	}
}

// Turn là một lượt trong lịch sử hội thoại.
type Turn struct {
	UserText string
	Text     string
	Ready    bool
	Final    bool
	Status   string
	Files    []File
}

func turnLooksDone(t *Turn) bool {
	if t.Status != "" && runningStatus[t.Status] {
		return false
	}
	if t.Status != "" && terminalStatus[t.Status] {
		return true
	}
	return t.Ready && t.Final
}

// History đọc /chat/history và tách thành các lượt.
func (c *Client) History(ctx context.Context, sessionID string, limit int) ([]*Turn, error) {
	gw, err := c.Gateway(ctx)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	path := fmt.Sprintf("/chat/history?session_id=%s&limit=%d", urlEscape(sessionID), limit)
	status, body, reset, err := gw.JSON(ctx, "GET", path, nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if reset != nil && reset.Code != 0 {
		return nil, fmt.Errorf("muse: đọc lịch sử bị cắt (%d %s)", reset.Code, reset.Reason)
	}
	if status == 404 {
		return nil, nil
	}
	if status != 200 {
		return nil, fmt.Errorf("muse: đọc lịch sử lỗi HTTP %d", status)
	}
	return parseHistory(body)
}

func lastTurn(turns []*Turn) *Turn {
	if len(turns) == 0 {
		return nil
	}
	return turns[len(turns)-1]
}

func eventName(rec map[string]any) string {
	if v, ok := rec["event"].(string); ok && v != "" {
		return v
	}
	if v, ok := rec["type"].(string); ok {
		return v
	}
	return ""
}

func payloadStatus(rec map[string]any) string {
	if p, ok := rec["payload"].(map[string]any); ok {
		if s, ok := p["status"].(string); ok {
			return s
		}
	}
	if s, ok := rec["status"].(string); ok {
		return s
	}
	return ""
}

func errorMessage(rec map[string]any) string {
	pick := func(v any) string {
		if m, ok := v.(map[string]any); ok {
			for _, k := range []string{"message", "msg", "code", "type"} {
				if s, ok := m[k].(string); ok && s != "" {
					return s
				}
			}
		}
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}
	if s := pick(rec["error"]); s != "" {
		return s
	}
	if p, ok := rec["payload"].(map[string]any); ok {
		return pick(p["error"])
	}
	return ""
}

// SessionList đọc danh sách hội thoại (id + trạng thái).
func (c *Client) SessionList(ctx context.Context, limit int) ([]map[string]any, error) {
	gw, err := c.Gateway(ctx)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	status, body, _, err := gw.JSON(ctx, "GET", fmt.Sprintf("/api/session/list?limit=%d", limit), nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("muse: danh sách hội thoại lỗi HTTP %d", status)
	}
	var out struct {
		Result struct {
			Sessions []map[string]any `json:"sessions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Result.Sessions, nil
}

// FetchFile tải một tệp kết quả về bộ nhớ.
func (c *Client) FetchFile(ctx context.Context, path string, limit int) (string, []byte, error) {
	gw, err := c.Gateway(ctx)
	if err != nil {
		return "", nil, err
	}
	if limit <= 0 {
		limit = 64 << 20
	}
	status, ctype, data, err := gw.FetchFile(ctx, path, 5*time.Minute, limit)
	if err != nil {
		return "", nil, err
	}
	if status != 200 {
		return "", nil, fmt.Errorf("muse: tải tệp lỗi HTTP %d", status)
	}
	return ctype, data, nil
}
