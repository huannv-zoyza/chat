package controller

import (
	"strings"
	"sync"
	"time"
)

// loginLimiter chặn dò mật khẩu: một tên đăng nhập sai quá maxFails lần trong window
// thì tạm khoá tới hết window. Đếm theo tên đăng nhập chứ không theo IP, vì khi chạy
// sau Cloudflare Tunnel mọi request đều đến từ 127.0.0.1.
type loginLimiter struct {
	mu       sync.Mutex
	fails    map[string][]time.Time
	maxFails int
	window   time.Duration
}

func newLoginLimiter(maxFails int, window time.Duration) *loginLimiter {
	return &loginLimiter{fails: map[string][]time.Time{}, maxFails: maxFails, window: window}
}

func (l *loginLimiter) key(login string) string { return strings.ToLower(strings.TrimSpace(login)) }

// recent bỏ các lần sai đã quá window. Phải giữ l.mu.
func (l *loginLimiter) recent(k string, now time.Time) []time.Time {
	list := l.fails[k]
	i := 0
	for i < len(list) && now.Sub(list[i]) > l.window {
		i++
	}
	list = list[i:]
	if len(list) == 0 {
		delete(l.fails, k)
	} else {
		l.fails[k] = list
	}
	return list
}

// Blocked trả về thời gian còn phải chờ (0 = được thử).
func (l *loginLimiter) Blocked(login string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	list := l.recent(l.key(login), now)
	if len(list) < l.maxFails {
		return 0
	}
	return l.window - now.Sub(list[0])
}

func (l *loginLimiter) Fail(login string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := l.key(login)
	l.fails[k] = append(l.recent(k, time.Now()), time.Now())
}

func (l *loginLimiter) Reset(login string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, l.key(login))
}
