package muse

// Lớp REST của muse.ai: đọc phiên, đánh thức máy ảo, lấy mã kết nối rồi mở gateway.
// Cổng từ desktop/src/shared/gateway/client.ts (connectOnce) và src/main/adapter/muse/selectors.ts.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultOrigin  = "https://muse.ai"
	DefaultGateway = "wss://hatch.metaaivm.com/v1/noise"
	// VMWakeWait: thời gian chờ máy ảo báo đang chạy sau lệnh đánh thức.
	VMWakeWait = 90 * time.Second
)

// ConnectError nói rõ hỏng ở bước nào: session | wake | token | socket | handshake | challenge.
type ConnectError struct {
	Step   string
	Msg    string
	Status int
}

func (e *ConnectError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("muse [%s] %s (HTTP %d)", e.Step, e.Msg, e.Status)
	}
	return fmt.Sprintf("muse [%s] %s", e.Step, e.Msg)
}

// vmSuspendedRe: những trạng thái chắc chắn nghĩa là máy ảo đang ngủ/tắt, phải đánh thức trước.
var vmSuspendedRe = regexp.MustCompile(`(?i)^(suspend|suspended|suspending|stopped|stopping|paused|pausing|hibernated|hibernating|terminated|shutdown|off)$`)

// VMRunning quyết định có cần đánh thức máy ảo trước khi nối hay không.
//
// Danh sách là *danh sách chặn*, không phải danh sách cho phép. Lý do: quan sát trên tài khoản thật
// (21/11/2026) cho thấy /api/session trả `vm_state: "UNKNOWN"` với `vm_state_source: "live"` trong khi
// web app của Muse vẫn nối và chat bình thường. "UNKNOWN" nghĩa là *không xác định được*, không phải
// *đang ngủ*. Dùng danh sách cho phép thì mọi giá trị lạ đều biến thành "chưa khởi động xong" và kẹt
// vĩnh viễn — đúng lỗi của bản desktop (so khớp cứng `== "RUNNING"`).
//
// Trạng thái lạ thì cứ nối thử; socket hỏng đã có nhánh thử lại kèm ép đánh thức trong Connect().
func VMRunning(state any) bool {
	s, ok := state.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return true
	}
	return !vmSuspendedRe.MatchString(strings.TrimSpace(s))
}

func urlEscape(s string) string { return url.QueryEscape(s) }

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Config là cấu hình kết nối tới Muse.
type Config struct {
	// Cookie: chuỗi Cookie đầy đủ copy từ trình duyệt đã đăng nhập muse.ai (bắt buộc).
	Cookie string
	// Origin mặc định https://muse.ai.
	Origin string
	// GatewayURL mặc định wss://hatch.metaaivm.com/v1/noise.
	GatewayURL string
	// UserAgent nên giống trình duyệt đã lấy cookie.
	UserAgent string
	// ProxyURL (tuỳ chọn) ví dụ http://127.0.0.1:8888 hoặc socks5://...
	ProxyURL string
	// HTTPTimeout cho các lệnh REST (không áp cho WebSocket).
	HTTPTimeout time.Duration
}

func (c *Config) fill() {
	if c.Origin == "" {
		c.Origin = DefaultOrigin
	}
	if c.GatewayURL == "" {
		c.GatewayURL = DefaultGateway
	}
	if c.UserAgent == "" {
		c.UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
	}
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = 30 * time.Second
	}
}

// Session giữ cookie và HTTP client dùng chung cho mọi lệnh REST lẫn WebSocket.
type Session struct {
	cfg Config
	hc  *http.Client
}

func NewSession(cfg Config) (*Session, error) {
	cfg.fill()
	if strings.TrimSpace(cfg.Cookie) == "" {
		return nil, fmt.Errorf("muse: thiếu cookie phiên muse.ai")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.ProxyURL != "" {
		u, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("muse: ProxyURL sai: %w", err)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &Session{cfg: cfg, hc: &http.Client{Transport: tr, Timeout: cfg.HTTPTimeout}}, nil
}

func (s *Session) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Origin+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("cookie", s.cfg.Cookie)
	req.Header.Set("x-app-id", AppID)
	req.Header.Set("user-agent", s.cfg.UserAgent)
	// Trình duyệt gửi */* cho /api/session (xem request thật), không phải application/json.
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "en-US,en;q=0.9,vi;q=0.8")
	req.Header.Set("origin", s.cfg.Origin)
	req.Header.Set("referer", s.cfg.Origin+"/")
	// Fetch Metadata — BẮT BUỘC. Muse chặn /api/hatch/* bằng 403 {"error":"Forbidden"} nếu thiếu
	// (kiểm chứng trên tài khoản thật 21/11/2026: thêm đúng ba header này là 403 → 200).
	// Bản desktop không bao giờ gặp lỗi này vì chạy trong trang Chromium thật, trình duyệt tự gắn.
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, buf.Bytes(), nil
}

// Info là phần thông tin phiên mà quá trình kết nối cần.
type Info struct {
	EndpointURL string `json:"endpoint_url"`
	VMID        string `json:"vm_id"`
	VMState     any    `json:"vm_state"`
	Status      string `json:"status"`
}

// ReadSession đọc /api/session: địa chỉ máy ảo và trạng thái của nó.
func (s *Session) ReadSession(ctx context.Context) (*Info, int, error) {
	code, body, err := s.do(ctx, "GET", "/api/session?vm_state=live", nil)
	if err != nil {
		return nil, code, err
	}
	if code == 401 {
		return nil, code, &ConnectError{Step: "session", Msg: "cookie hết hạn — đăng nhập lại muse.ai", Status: code}
	}
	if code == 403 {
		return nil, code, &ConnectError{Step: "session", Msg: "Muse từ chối truy cập (cần xác minh hoặc vùng không hỗ trợ)", Status: code}
	}
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	in := &Info{}
	if v, ok := raw["endpoint_url"].(string); ok {
		in.EndpointURL = v
	} else if v, ok := raw["endpointUrl"].(string); ok {
		in.EndpointURL = v
	}
	if v, ok := raw["vm_id"].(string); ok {
		in.VMID = v
	} else if v, ok := raw["vmId"].(string); ok {
		in.VMID = v
	}
	in.VMState = raw["vm_state"]
	if v, ok := raw["status"].(string); ok {
		in.Status = v
	}
	return in, code, nil
}

// wake đánh thức máy ảo: gửi lệnh, 2 giây hỏi lại một lần, 10 giây thúc lại một lần, bỏ cuộc sau VMWakeWait.
func (s *Session) wake(ctx context.Context, vmID string) (*Info, error) {
	t0 := time.Now()
	retry := 0
	lastWake := time.Time{}
	for time.Since(t0) < VMWakeWait {
		if lastWake.IsZero() || time.Since(lastWake) >= 10*time.Second {
			lastWake = time.Now()
			_, _, _ = s.do(ctx, "POST", "/api/hatch/vm/wake", map[string]any{"vm_id": vmID, "retry_count": retry})
			retry++
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		in, _, err := s.ReadSession(ctx)
		if err != nil {
			continue
		}
		if VMRunning(in.VMState) {
			return in, nil
		}
	}
	return nil, &ConnectError{Step: "wake", Msg: "máy chạy Muse của tài khoản chưa thức dậy"}
}

// Connect chạy trọn chuỗi: đọc phiên → đánh thức nếu cần → lấy mã → mở WebSocket → bắt tay Noise.
// Socket không mở được sẽ thử lại một lần sau khi ép đánh thức, giống bản desktop.
func (s *Session) Connect(ctx context.Context) (*Gateway, error) {
	g, err := s.connectOnce(ctx, false)
	if err != nil {
		var ce *ConnectError
		if ok := asConnectError(err, &ce); ok && ce.Step == "socket" {
			return s.connectOnce(ctx, true)
		}
	}
	return g, err
}

func asConnectError(err error, out **ConnectError) bool {
	if ce, ok := err.(*ConnectError); ok {
		*out = ce
		return true
	}
	return false
}

func (s *Session) connectOnce(ctx context.Context, forceWake bool) (*Gateway, error) {
	in, code, err := s.ReadSession(ctx)
	if err != nil {
		return nil, err
	}
	if in.EndpointURL == "" {
		return nil, &ConnectError{Step: "session", Msg: "Muse chưa cấp máy chạy cho phiên này", Status: code}
	}
	u, err := url.Parse(in.EndpointURL)
	if err != nil {
		return nil, &ConnectError{Step: "session", Msg: "địa chỉ máy chạy Muse không hợp lệ"}
	}
	vm := in.VMID
	if vm == "" && strings.HasSuffix(u.Hostname(), ".metaaivm.com") {
		vm = strings.Split(u.Hostname(), ".")[0]
	}
	if forceWake || !VMRunning(in.VMState) {
		if vm == "" {
			return nil, &ConnectError{Step: "session", Msg: "Muse chưa cấp máy chạy cho phiên này", Status: code}
		}
		woken, err := s.wake(ctx, vm)
		if err != nil {
			return nil, err
		}
		if woken.EndpointURL != "" {
			in.EndpointURL = woken.EndpointURL
		}
	}

	code, body, err := s.do(ctx, "POST", "/api/hatch/token", map[string]any{"vmAddress": in.EndpointURL, "vmName": vm})
	if err != nil {
		return nil, &ConnectError{Step: "token", Msg: err.Error()}
	}
	var tok struct {
		Token       string `json:"token"`
		NotaryToken string `json:"notary_token"`
	}
	_ = json.Unmarshal(body, &tok)
	if tok.Token == "" {
		return nil, &ConnectError{Step: "token", Msg: "không lấy được mã kết nối của Muse", Status: code}
	}
	notary := tok.NotaryToken
	if _, nb, err := s.do(ctx, "POST", "/api/hatch/noise-notary-token", map[string]any{"vmName": vm}); err == nil {
		var nt struct {
			NotaryToken string `json:"notaryToken"`
		}
		if json.Unmarshal(nb, &nt) == nil && nt.NotaryToken != "" {
			notary = strings.TrimSpace(nt.NotaryToken)
		}
	}
	return DialGateway(ctx, s.cfg.GatewayURL, vm, tok.Token, notary, s.hc)
}
