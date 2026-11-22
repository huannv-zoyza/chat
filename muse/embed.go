package muse

// Nhúng mặt ngoài OpenAI vào chính tiến trình đang chạy, trên một cổng loopback riêng.
//
// Vì sao không gắn thẳng vào router công khai của go-chat: làm vậy là phơi cầu nối Muse ra
// cho mọi người tới được máy chủ chat. Ở đây listener bind 127.0.0.1 với cổng do hệ điều hành
// cấp, kèm một khoá ngẫu nhiên sinh lúc khởi động và chỉ trao cho provider trong bộ nhớ —
// ngoài tiến trình này không ai gọi tới được. Vẫn đúng tinh thần "một tiến trình".

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Loopback là cầu nối Muse đang chạy trên cổng loopback nội bộ.
type Loopback struct {
	// BaseURL dùng cho ai.Provider, đã gồm hậu tố /v1.
	BaseURL string
	// Key là khoá Bearer ngẫu nhiên của lần chạy này.
	Key string

	srv *http.Server
	ln  net.Listener
}

func randomKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sk-loopback-" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// StartLoopback mở listener trên 127.0.0.1 (cổng tuỳ hệ điều hành) và phục vụ API OpenAI của Muse.
func StartLoopback(c *Client) (*Loopback, error) {
	if c == nil {
		return nil, fmt.Errorf("muse: thiếu client")
	}
	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("muse: không mở được cổng loopback: %w", err)
	}
	srv := &http.Server{
		Handler:           (&Server{C: c, APIKey: key}).Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		// Không đặt WriteTimeout: một lượt Muse chạy lâu và SSE phải giữ kết nối.
	}
	l := &Loopback{
		BaseURL: "http://" + ln.Addr().String() + "/v1",
		Key:     key,
		srv:     srv,
		ln:      ln,
	}
	go func() { _ = srv.Serve(ln) }()
	return l, nil
}

// Close tắt listener nội bộ.
func (l *Loopback) Close() error {
	if l == nil || l.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return l.srv.Shutdown(ctx)
}
