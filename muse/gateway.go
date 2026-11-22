package muse

// Kênh truyền tới gateway "hatch" của Muse: WebSocket + Noise XX, bên trong là HTTP đóng gói protobuf.
// Cổng từ desktop/src/shared/gateway/client.ts, giữ nguyên số hiệu trường và thứ tự thao tác.

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	AppID = "hatch-web"
	// MaxFramePayload: phần rõ lớn nhất mỗi khung, chừa chỗ cho tag AES-GCM trong giới hạn 64 KiB của gateway.
	MaxFramePayload = 65489
	// EventChanSize: số bản ghi NDJSON đệm sẵn cho mỗi luồng.
	EventChanSize = 8192
)

// ChatCapabilities phải khớp bộ mà web app của Muse khai báo, nếu không máy chủ trả sự kiện khác.
var ChatCapabilities = []string{"chat_cancel", "delta_stream", "custom_reactions", "custom_reactions_facebook_thumbs_up_v1"}

// Reset là lý do luồng bị cắt giữa chừng.
type Reset struct {
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

type stream struct {
	id      uint64
	ndjson  bool
	limit   int
	mu      sync.Mutex
	status  int
	headers [][2]string
	body    [][]byte
	nbytes  int
	buf     []byte
	last    map[string]any
	reset   *Reset
	recs    chan map[string]any
	head    chan struct{}
	fin     chan struct{}
	headOne sync.Once
	finOne  sync.Once
}

func (s *stream) markHead() { s.headOne.Do(func() { close(s.head) }) }
func (s *stream) markDone() { s.markHead(); s.finOne.Do(func() { close(s.fin) }) }

type assembly struct {
	parts map[uint64][]byte
	total uint64
}

// Gateway là một kết nối đang mở tới máy ảo Muse của tài khoản.
type Gateway struct {
	conn *websocket.Conn
	tx   *Transport

	mu         sync.Mutex
	nextStream uint64
	streams    map[uint64]*stream
	assembling map[uint64]*assembly

	sendMu sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}
	errMu     sync.Mutex
	lastErr   error
}

// DialGateway mở WebSocket tới gateway rồi bắt tay Noise XX.
// tok/notary lấy từ Session.Token; vmID là tên máy ảo Muse cấp cho phiên.
func DialGateway(ctx context.Context, gatewayURL, vmID, authToken, notaryToken string, hc *http.Client) (*Gateway, error) {
	q := make([]string, 0, 5)
	add := func(k, v string) {
		if v != "" {
			q = append(q, k+"="+urlEscape(v))
		}
	}
	add("vm_id", vmID)
	add("auth_token", authToken)
	add("notary_token", notaryToken)
	add("app_id", AppID)
	add("request_id", newUUID())
	url := gatewayURL + "?" + strings.Join(q, "&")

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{HTTPClient: hc})
	if err != nil {
		return nil, &ConnectError{Step: "socket", Msg: "không mở được kết nối tới Muse: " + err.Error()}
	}
	conn.SetReadLimit(80 << 20) // khung tệp lớn

	g := &Gateway{
		conn:       conn,
		nextStream: 1,
		streams:    map[uint64]*stream{},
		assembling: map[uint64]*assembly{},
		closed:     make(chan struct{}),
	}

	// Bắt tay: ba thông điệp đi/về trực tiếp trên socket, chưa qua vòng đọc chính.
	hsCtx, hsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer hsCancel()
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		conn.Close(websocket.StatusInternalError, "")
		return nil, err
	}
	tx, err := initiatorHandshake(handshakeIO{
		payload1: FieldBytes(1, nonce),
		exchange: func(m1 []byte) ([]byte, error) {
			if err := conn.Write(hsCtx, websocket.MessageBinary, m1); err != nil {
				return nil, err
			}
			_, data, err := conn.Read(hsCtx)
			return data, err
		},
		deliver:     func(m3 []byte) error { return conn.Write(hsCtx, websocket.MessageBinary, m3) },
		isChallenge: isChallengePayload,
	})
	if err != nil {
		conn.Close(websocket.StatusInternalError, "")
		if he, ok := err.(*HandshakeError); ok && he.Step == "challenge" {
			return nil, &ConnectError{Step: "challenge", Msg: "Muse yêu cầu xác minh thêm cho kết nối"}
		}
		return nil, &ConnectError{Step: "handshake", Msg: "bắt tay mã hoá với Muse thất bại: " + err.Error()}
	}
	g.tx = tx
	go g.readLoop()
	return g, nil
}

func (g *Gateway) noteErr(err error) {
	g.errMu.Lock()
	g.lastErr = err
	g.errMu.Unlock()
}

// Err trả lỗi cuối cùng khiến kênh hỏng (nil nếu đóng bình thường).
func (g *Gateway) Err() error {
	g.errMu.Lock()
	defer g.errMu.Unlock()
	return g.lastErr
}

// Closed đóng khi kênh ngừng hoạt động.
func (g *Gateway) Closed() <-chan struct{} { return g.closed }

func (g *Gateway) IsOpen() bool {
	select {
	case <-g.closed:
		return false
	default:
		return g.tx != nil
	}
}

func (g *Gateway) Close() {
	g.closeOnce.Do(func() {
		close(g.closed)
		if g.conn != nil {
			g.conn.Close(websocket.StatusNormalClosure, "")
		}
		g.mu.Lock()
		for _, st := range g.streams {
			st.mu.Lock()
			if st.reset == nil {
				st.reset = &Reset{Code: -1, Reason: "kênh đã đóng"}
			}
			st.mu.Unlock()
			st.markDone()
		}
		g.mu.Unlock()
	})
}

func (g *Gateway) readLoop() {
	defer g.Close()
	for {
		_, data, err := g.conn.Read(context.Background())
		if err != nil {
			g.noteErr(err)
			return
		}
		if err := g.onTransport(data); err != nil {
			g.noteErr(err)
			return
		}
	}
}

// onTransport giải mã một khung, ghép mảnh rồi đẩy vào đúng luồng.
func (g *Gateway) onTransport(raw []byte) error {
	pt, err := g.tx.recv.decrypt(nil, raw)
	if err != nil {
		return fmt.Errorf("giải mã khung hỏng: %w", err)
	}
	f, err := DecodeFields(pt)
	if err != nil {
		return err
	}
	cid := f.Num(1, 0)
	idx := f.Num(2, 0)
	total := f.Num(3, 1)
	if total == 0 {
		total = 1
	}
	payload := f.Bytes(4)
	whole := payload
	if total != 1 {
		if total > 100000 || idx >= total {
			return protoErr("chỉ số mảnh sai")
		}
		g.mu.Lock()
		a := g.assembling[cid]
		if a == nil {
			a = &assembly{parts: map[uint64][]byte{}, total: total}
			g.assembling[cid] = a
		}
		a.parts[idx] = append([]byte{}, payload...)
		if uint64(len(a.parts)) < total {
			g.mu.Unlock()
			return nil
		}
		delete(g.assembling, cid)
		g.mu.Unlock()
		parts := make([][]byte, 0, total)
		for i := uint64(0); i < total; i++ {
			parts = append(parts, a.parts[i])
		}
		whole = Concat(parts...)
	}
	sr, err := DecodeFields(whole)
	if err != nil {
		return err
	}
	inner := sr.Bytes(1)
	if inner == nil {
		return protoErr("thiếu khung bên trong")
	}
	frame, err := DecodeFields(inner)
	if err != nil {
		return err
	}
	sid := frame.Num(1, 0)
	g.mu.Lock()
	st := g.streams[sid]
	g.mu.Unlock()
	if st == nil {
		return nil // luồng đã bỏ theo dõi
	}

	var data []byte
	end := false
	if head := frame.Bytes(3); head != nil {
		r, err := DecodeFields(head)
		if err != nil {
			return err
		}
		st.mu.Lock()
		st.status = int(r.Num(1, 0))
		for _, h := range r[2] {
			if h.IsNum {
				continue
			}
			hf, err := DecodeFields(h.Bytes)
			if err != nil {
				st.mu.Unlock()
				return err
			}
			st.headers = append(st.headers, [2]string{hf.Str(1), hf.Str(2)})
		}
		st.mu.Unlock()
		st.markHead()
		data = r.Bytes(3)
		end = r.Num(4, 0) == 1
	} else if body := frame.Bytes(4); body != nil {
		b, err := DecodeFields(body)
		if err != nil {
			return err
		}
		data = b.Bytes(1)
		end = b.Num(2, 0) == 1
	} else if rst := frame.Bytes(5); rst != nil {
		r, err := DecodeFields(rst)
		if err != nil {
			return err
		}
		reason := r.Str(2)
		if len(reason) > 200 {
			reason = reason[:200]
		}
		st.mu.Lock()
		st.reset = &Reset{Code: int(r.Num(1, 0)), Reason: reason}
		st.mu.Unlock()
		st.markDone()
		return nil
	}

	if len(data) > 0 {
		if st.ndjson {
			st.mu.Lock()
			st.buf = append(st.buf, data...)
			if len(st.buf) > 8<<20 {
				st.mu.Unlock()
				return protoErr("dòng sự kiện quá dài")
			}
			lines := [][]byte{}
			for {
				i := indexByte(st.buf, '\n')
				if i < 0 {
					break
				}
				line := append([]byte{}, st.buf[:i]...)
				st.buf = st.buf[i+1:]
				if len(strings.TrimSpace(string(line))) > 0 {
					lines = append(lines, line)
				}
			}
			st.mu.Unlock()
			for _, l := range lines {
				st.push(l, false)
			}
		} else {
			st.mu.Lock()
			st.nbytes += len(data)
			over := st.limit > 0 && st.nbytes > st.limit
			if over {
				st.reset = &Reset{Code: -2, Reason: "tệp quá lớn"}
			} else {
				st.body = append(st.body, append([]byte{}, data...))
			}
			st.mu.Unlock()
			if over {
				st.markDone()
				return nil
			}
		}
	}
	if end {
		if st.ndjson {
			st.mu.Lock()
			tail := append([]byte{}, st.buf...)
			st.buf = nil
			st.mu.Unlock()
			if len(strings.TrimSpace(string(tail))) > 0 {
				st.push(tail, true)
			}
		}
		st.markDone()
	}
	return nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// push đẩy một bản ghi NDJSON vào kênh của luồng; đầy thì bỏ qua chứ không chặn vòng đọc.
func (s *stream) push(line []byte, final bool) {
	var rec map[string]any
	if err := json.Unmarshal(line, &rec); err != nil || rec == nil {
		raw := string(line)
		if len(raw) > 300 {
			raw = raw[:300]
		}
		rec = map[string]any{"raw": raw}
	}
	// Luôn giữ bản ghi mới nhất: ack của /chat/stream là bản ghi cuối của luồng, dù dòng đó
	// có kết thúc bằng '\n' hay không. (Bản TS chỉ ghi khi dòng cuối thiếu '\n' — dễ hụt ack.)
	s.mu.Lock()
	s.last = rec
	s.mu.Unlock()
	_ = final
	select {
	case s.recs <- rec:
	default:
	}
}

func (g *Gateway) newStream(ndjson bool, limit int) *stream {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := g.nextStream
	g.nextStream++
	st := &stream{
		id: id, ndjson: ndjson, limit: limit,
		head: make(chan struct{}), fin: make(chan struct{}),
	}
	if ndjson {
		st.recs = make(chan map[string]any, EventChanSize)
	}
	g.streams[id] = st
	return st
}

func (g *Gateway) forget(id uint64) {
	g.mu.Lock()
	delete(g.streams, id)
	g.mu.Unlock()
}

// enqueue mã hoá và gửi một khối dữ liệu, cắt mảnh nếu vượt MaxFramePayload.
func (g *Gateway) enqueue(ctx context.Context, plain []byte) error {
	g.sendMu.Lock()
	defer g.sendMu.Unlock()
	if !g.IsOpen() {
		return errClosed
	}
	var hi [8]byte
	if _, err := rand.Read(hi[:]); err != nil {
		return err
	}
	cid := binary.BigEndian.Uint64(hi[:]) & ((1 << 63) - 1)
	total := (len(plain) + MaxFramePayload - 1) / MaxFramePayload
	if total < 1 {
		total = 1
	}
	for i := 0; i < total; i++ {
		lo := i * MaxFramePayload
		hi2 := lo + MaxFramePayload
		if hi2 > len(plain) {
			hi2 = len(plain)
		}
		fr := Concat(FieldVarint(1, cid), FieldVarint(2, uint64(i)), FieldVarint(3, uint64(total)), FieldBytes(4, plain[lo:hi2]))
		ct, err := g.tx.send.encrypt(nil, fr)
		if err != nil {
			return err
		}
		if err := g.conn.Write(ctx, websocket.MessageBinary, ct); err != nil {
			return err
		}
	}
	return nil
}

// Request mở một luồng HTTP qua đường hầm. Trả về luồng để người gọi chờ kết quả.
func (g *Gateway) Request(ctx context.Context, method, path string, body any, ndjson bool, limit int) (*stream, error) {
	if !g.IsOpen() {
		return nil, errClosed
	}
	st := g.newStream(ndjson, limit)
	hdrs := [][2]string{{"x-app-id", AppID}, {"accept-encoding", "identity"}}
	var b []byte
	if body != nil {
		var err error
		if b, err = json.Marshal(body); err != nil {
			return nil, err
		}
		hdrs = append(hdrs, [2]string{"content-type", "application/json"})
	}
	app := Concat(FieldString(1, method), FieldString(2, path))
	for _, h := range hdrs {
		app = Concat(app, FieldBytes(3, Concat(FieldString(1, h[0]), FieldString(2, h[1]))))
	}
	if len(b) > 0 {
		app = Concat(app, FieldBytes(4, b))
	}
	app = Concat(app, FieldVarint(5, 1))
	plain := Concat(FieldVarint(1, 0), FieldBytes(2, Concat(FieldVarint(1, st.id), FieldBytes(2, app))))
	if err := g.enqueue(ctx, plain); err != nil {
		g.forget(st.id)
		return nil, err
	}
	return st, nil
}

// JSON gửi một yêu cầu và đọc trọn phản hồi JSON.
func (g *Gateway) JSON(ctx context.Context, method, path string, body any, timeout time.Duration) (int, []byte, *Reset, error) {
	st, err := g.Request(ctx, method, path, body, false, 64<<20)
	if err != nil {
		return 0, nil, nil, err
	}
	defer g.forget(st.id)
	select {
	case <-st.fin:
	case <-time.After(timeout):
		return 0, nil, &Reset{Code: -3, Reason: "quá thời gian"}, nil
	case <-g.closed:
		return 0, nil, &Reset{Code: -1, Reason: "kênh đã đóng"}, nil
	case <-ctx.Done():
		return 0, nil, nil, ctx.Err()
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.status, Concat(st.body...), st.reset, nil
}

// Subscribe mở luồng sự kiện của một hội thoại.
func (g *Gateway) Subscribe(ctx context.Context, sessionID string, afterStreamSeq int) (*stream, error) {
	return g.Request(ctx, "POST", "/chat/subscribe", map[string]any{
		"after_stream_seq":     afterStreamSeq,
		"after_chat_event_seq": 0,
		"capabilities":         ChatCapabilities,
		"session_id":           sessionID,
	}, true, 0)
}

// SendChat gửi một lượt vào hội thoại; session_id mới sẽ tạo hội thoại mới.
func (g *Gateway) SendChat(ctx context.Context, sessionID string, items []map[string]any, nodeID string) (*stream, error) {
	return g.Request(ctx, "POST", "/chat/stream", map[string]any{
		"items":        items,
		"node_id":      nodeID,
		"capabilities": ChatCapabilities,
		"session_id":   sessionID,
		"timezone":     "UTC",
	}, true, 0)
}

// FetchFile tải một tệp trong workspace của máy ảo.
func (g *Gateway) FetchFile(ctx context.Context, path string, timeout time.Duration, limit int) (int, string, []byte, error) {
	st, err := g.Request(ctx, "GET", "/fs/raw"+path, nil, false, limit)
	if err != nil {
		return 0, "", nil, err
	}
	defer g.forget(st.id)
	select {
	case <-st.fin:
	case <-time.After(timeout):
		return 0, "", nil, fmt.Errorf("muse: tải tệp quá thời gian")
	case <-g.closed:
		return 0, "", nil, errClosed
	case <-ctx.Done():
		return 0, "", nil, ctx.Err()
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	ctype := ""
	for _, h := range st.headers {
		if strings.EqualFold(h[0], "content-type") {
			ctype = h[1]
		}
	}
	return st.status, ctype, Concat(st.body...), nil
}
