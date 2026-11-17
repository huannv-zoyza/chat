// Package socket quản lý các kết nối websocket đang mở và việc phát sự kiện tới chúng.
package socket

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	// Số sự kiện tối đa xếp hàng chờ gửi cho 1 kết nối. Kết nối đọc chậm tới mức
	// đầy hàng đợi sẽ bị ngắt để không làm nghẽn người khác.
	sendBufferSize = 256
	writeTimeout   = 10 * time.Second
	// Server ping định kỳ để phát hiện kết nối chết (mất mạng, máy ngủ...).
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

// Client là MỘT kết nối websocket. Một user có thể có nhiều Client (nhiều máy/cửa sổ).
type Client struct {
	UserID int64
	// SessionHash: phiên đăng nhập đã mở kết nối này, để đóng đúng kết nối khi phiên bị huỷ.
	SessionHash string
	Conn        *websocket.Conn

	send      chan []byte
	closeOnce sync.Once
	channels  map[int64]struct{} // chỉ đọc/ghi khi giữ Hub.mu
}

func NewClient(userID int64, sessionHash string, conn *websocket.Conn) *Client {
	return &Client{
		UserID:      userID,
		SessionHash: sessionHash,
		Conn:        conn,
		send:        make(chan []byte, sendBufferSize),
		channels:    map[int64]struct{}{},
	}
}

func (c *Client) closeSend() {
	c.closeOnce.Do(func() { close(c.send) })
}

// enqueue không bao giờ chặn. Hàng đợi đầy thì ngắt kết nối này.
// Phải gọi khi đang giữ Hub.mu (đọc hoặc ghi) để không gửi vào channel đã đóng.
func (c *Client) enqueue(msg []byte) {
	select {
	case c.send <- msg:
	default:
		go c.Conn.Close(websocket.StatusPolicyViolation, "client too slow")
	}
}

// WritePump gửi các sự kiện trong hàng đợi xuống websocket và ping định kỳ.
// Kết thúc khi hàng đợi bị đóng (Hub.Remove) hoặc ghi lỗi; khi đó đóng luôn kết nối.
func (c *Client) WritePump(ctx context.Context) {
	defer c.Conn.CloseNow()

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := c.Conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// Hub giữ danh sách kết nối theo user và theo kênh. An toàn khi gọi từ nhiều goroutine.
type Hub struct {
	mu       sync.RWMutex
	byUser   map[int64]map[*Client]struct{}
	byChan   map[int64]map[*Client]struct{}
	shutdown bool
}

func NewHub() *Hub {
	return &Hub{
		byUser: map[int64]map[*Client]struct{}{},
		byChan: map[int64]map[*Client]struct{}{},
	}
}

func addTo(m map[int64]map[*Client]struct{}, key int64, c *Client) {
	set, ok := m[key]
	if !ok {
		set = map[*Client]struct{}{}
		m[key] = set
	}
	set[c] = struct{}{}
}

func removeFrom(m map[int64]map[*Client]struct{}, key int64, c *Client) {
	if set, ok := m[key]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(m, key)
		}
	}
}

// Add đăng ký kết nối và đăng ký nhận tin của các kênh channelIDs.
// firstConn = true nếu đây là kết nối đầu tiên của user (vừa online).
func (h *Hub) Add(c *Client, channelIDs []int64) (firstConn bool, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.shutdown {
		c.closeSend()
		return false, false
	}
	firstConn = len(h.byUser[c.UserID]) == 0
	addTo(h.byUser, c.UserID, c)
	for _, id := range channelIDs {
		c.channels[id] = struct{}{}
		addTo(h.byChan, id, c)
	}
	return firstConn, true
}

// Remove huỷ đăng ký kết nối. lastConn = true nếu user không còn kết nối nào (vừa offline).
func (h *Hub) Remove(c *Client) (lastConn bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.byUser[c.UserID][c]; !ok {
		return false
	}
	removeFrom(h.byUser, c.UserID, c)
	for id := range c.channels {
		removeFrom(h.byChan, id, c)
	}
	c.closeSend()
	return len(h.byUser[c.UserID]) == 0
}

// Subscribe cho mọi kết nối của userID nhận tin của channelID (khi vừa tham gia/tạo kênh).
func (h *Hub) Subscribe(userID, channelID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.byUser[userID] {
		c.channels[channelID] = struct{}{}
		addTo(h.byChan, channelID, c)
	}
}

// Unsubscribe ngừng gửi tin của channelID tới mọi kết nối của userID (khi rời kênh).
func (h *Hub) Unsubscribe(userID, channelID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.byUser[userID] {
		delete(c.channels, channelID)
		removeFrom(h.byChan, channelID, c)
	}
}

// DropChannel huỷ mọi đăng ký của kênh (khi kênh bị xoá).
func (h *Hub) DropChannel(channelID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.byChan[channelID] {
		delete(c.channels, channelID)
	}
	delete(h.byChan, channelID)
}

// IsSubscribed: kết nối c có đang nhận tin của channelID không.
func (h *Hub) IsSubscribed(c *Client, channelID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := c.channels[channelID]
	return ok
}

// ToChannel gửi msg tới mọi kết nối đang ở channelID, trừ except (có thể nil).
func (h *Hub) ToChannel(channelID int64, msg []byte, except *Client) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.byChan[channelID] {
		if c != except {
			c.enqueue(msg)
		}
	}
}

// ToUser gửi msg tới mọi kết nối của userID.
func (h *Hub) ToUser(userID int64, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.byUser[userID] {
		c.enqueue(msg)
	}
}

// ToClient gửi msg tới đúng một kết nối.
func (h *Hub) ToClient(c *Client, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, ok := h.byUser[c.UserID][c]; ok {
		c.enqueue(msg)
	}
}

// ToAll gửi msg tới mọi kết nối.
func (h *Hub) ToAll(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, set := range h.byUser {
		for c := range set {
			c.enqueue(msg)
		}
	}
}

func (h *Hub) OnlineUserIDs() []int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]int64, 0, len(h.byUser))
	for id := range h.byUser {
		ids = append(ids, id)
	}
	return ids
}

// CloseSessions đóng các kết nối của userID mở bằng phiên khớp match (đăng xuất, đổi mật khẩu).
// Client nhận mã đóng code và tự quay về màn đăng nhập.
func (h *Hub) CloseSessions(userID int64, match func(sessionHash string) bool, code websocket.StatusCode) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.byUser[userID] {
		if match(c.SessionHash) {
			go c.Conn.Close(code, "session ended")
		}
	}
}

// Shutdown đóng mọi kết nối và từ chối kết nối mới (khi tắt server).
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shutdown = true
	for _, set := range h.byUser {
		for c := range set {
			c.closeSend()
		}
	}
	h.byUser = map[int64]map[*Client]struct{}{}
	h.byChan = map[int64]map[*Client]struct{}{}
}
