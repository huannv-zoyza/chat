package muse

// Một gateway giả nói đúng giao thức thật: WebSocket + Noise XX (vai responder) + đóng khung protobuf,
// phục vụ /chat/subscribe, /chat/stream, /chat/history, /fs/raw. Nhờ nó mà toàn tuyến test được
// mà không cần tài khoản Muse thật — và bất kỳ sai lệch đóng khung nào cũng lộ ra ngay.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type gwRequest struct {
	Method  string
	Path    string
	Headers [][2]string
	Body    []byte
	Stream  uint64
}

type fakeGateway struct {
	t        *testing.T
	srv      *httptest.Server
	handle   func(req gwRequest, w *fakeResponder)
	mu       sync.Mutex
	requests []gwRequest
}

type fakeResponder struct {
	conn *websocket.Conn
	tx   *Transport
	sid  uint64
	mu   *sync.Mutex
}

func (w *fakeResponder) raw(plain []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fr := Concat(FieldVarint(1, 7), FieldVarint(2, 0), FieldVarint(3, 1), FieldBytes(4, plain))
	ct, err := w.tx.send.encrypt(nil, fr)
	if err != nil {
		return
	}
	_ = w.conn.Write(context.Background(), websocket.MessageBinary, ct)
}

func (w *fakeResponder) wrap(inner []byte) { w.raw(FieldBytes(1, inner)) }

// Head gửi phần đầu phản hồi (kèm dữ liệu đầu tiên nếu có).
func (w *fakeResponder) Head(status int, ctype string, data []byte, end bool) {
	head := FieldVarint(1, uint64(status))
	if ctype != "" {
		head = Concat(head, FieldBytes(2, Concat(FieldString(1, "content-type"), FieldString(2, ctype))))
	}
	if len(data) > 0 {
		head = Concat(head, FieldBytes(3, data))
	}
	if end {
		head = Concat(head, FieldVarint(4, 1))
	}
	w.wrap(Concat(FieldVarint(1, w.sid), FieldBytes(3, head)))
}

// Body gửi thêm một khúc dữ liệu.
func (w *fakeResponder) Body(data []byte, end bool) {
	b := FieldBytes(1, data)
	if end {
		b = Concat(b, FieldVarint(2, 1))
	}
	w.wrap(Concat(FieldVarint(1, w.sid), FieldBytes(4, b)))
}

// JSON trả một phản hồi JSON trọn gói.
func (w *fakeResponder) JSON(status int, v any) {
	b, _ := json.Marshal(v)
	w.Head(status, "application/json", b, true)
}

func newFakeGateway(t *testing.T, handle func(req gwRequest, w *fakeResponder)) *fakeGateway {
	g := &fakeGateway{t: t, handle: handle}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	return g
}

func (g *fakeGateway) wsURL() string { return "ws" + strings.TrimPrefix(g.srv.URL, "http") }
func (g *fakeGateway) Close()        { g.srv.Close() }

func (g *fakeGateway) seen() []gwRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]gwRequest{}, g.requests...)
}

func (g *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(80 << 20)
	ctx := context.Background()

	// Bắt tay vai responder.
	ss := newSymmetricState()
	ss.mixHash(nil)
	s, _ := generateKeyPair()
	e, _ := generateKeyPair()
	_, m1, err := conn.Read(ctx)
	if err != nil {
		return
	}
	re := m1[0:32]
	ss.mixHash(re)
	if _, err := ss.decryptAndHash(m1[32:]); err != nil {
		return
	}
	ss.mixHash(e.pub)
	sec, _ := dh(e, re)
	if ss.mixKey(sec) != nil {
		return
	}
	encS, err := ss.encryptAndHash(s.pub)
	if err != nil {
		return
	}
	sec2, _ := dh(s, re)
	if ss.mixKey(sec2) != nil {
		return
	}
	encP, err := ss.encryptAndHash([]byte{})
	if err != nil {
		return
	}
	if conn.Write(ctx, websocket.MessageBinary, Concat(e.pub, encS, encP)) != nil {
		return
	}
	_, m3, err := conn.Read(ctx)
	if err != nil || len(m3) < 48 {
		return
	}
	rs, err := ss.decryptAndHash(m3[0:48])
	if err != nil {
		return
	}
	sec3, _ := dh(e, rs)
	if ss.mixKey(sec3) != nil {
		return
	}
	if _, err := ss.decryptAndHash(m3[48:]); err != nil {
		return
	}
	c1, c2, err := ss.split()
	if err != nil {
		return
	}
	tx := &Transport{send: c2, recv: c1}
	var sendMu sync.Mutex

	asm := map[uint64]*assembly{}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		pt, err := tx.recv.decrypt(nil, data)
		if err != nil {
			g.t.Errorf("gateway giả không giải mã được khung: %v", err)
			return
		}
		f, err := DecodeFields(pt)
		if err != nil {
			g.t.Errorf("khung sai định dạng: %v", err)
			return
		}
		cid, idx, total := f.Num(1, 0), f.Num(2, 0), f.Num(3, 1)
		whole := f.Bytes(4)
		if total != 1 { // ghép mảnh giống client
			a := asm[cid]
			if a == nil {
				a = &assembly{parts: map[uint64][]byte{}, total: total}
				asm[cid] = a
			}
			a.parts[idx] = append([]byte{}, f.Bytes(4)...)
			if uint64(len(a.parts)) < total {
				continue
			}
			delete(asm, cid)
			parts := make([][]byte, 0, total)
			for i := uint64(0); i < total; i++ {
				parts = append(parts, a.parts[i])
			}
			whole = Concat(parts...)
		}
		outer, err := DecodeFields(whole)
		if err != nil {
			g.t.Errorf("vỏ ngoài sai: %v", err)
			return
		}
		sw, err := DecodeFields(outer.Bytes(2))
		if err != nil {
			g.t.Errorf("khung luồng sai: %v", err)
			return
		}
		sid := sw.Num(1, 0)
		app, err := DecodeFields(sw.Bytes(2))
		if err != nil {
			g.t.Errorf("gói yêu cầu sai: %v", err)
			return
		}
		req := gwRequest{Method: app.Str(1), Path: app.Str(2), Body: app.Bytes(4), Stream: sid}
		for _, h := range app[3] {
			hf, err := DecodeFields(h.Bytes)
			if err != nil {
				continue
			}
			req.Headers = append(req.Headers, [2]string{hf.Str(1), hf.Str(2)})
		}
		g.mu.Lock()
		g.requests = append(g.requests, req)
		g.mu.Unlock()
		go g.handle(req, &fakeResponder{conn: conn, tx: tx, sid: sid, mu: &sendMu})
	}
}

// fakeMuse dựng phần REST của muse.ai trỏ sang gateway giả.
func fakeMuse(t *testing.T, gw *fakeGateway, vmState any) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("cookie") == "" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint_url": "https://vm-test.metaaivm.com", "vm_id": "vm-test", "vm_state": vmState, "status": "assigned",
		})
	})
	mux.HandleFunc("/api/hatch/vm/wake", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/hatch/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok-test", "notary_token": "notary-test"})
	})
	mux.HandleFunc("/api/hatch/noise-notary-token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"notaryToken": "notary-better"})
	})
	return httptest.NewServer(mux)
}

func newTestClient(t *testing.T, gw *fakeGateway, vmState any) (*Client, func()) {
	t.Helper()
	rest := fakeMuse(t, gw, vmState)
	c, err := New(Config{
		Cookie:      "sid=abc",
		Origin:      rest.URL,
		GatewayURL:  gw.wsURL(),
		HTTPTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, func() { c.Close(); rest.Close() }
}
