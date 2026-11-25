package controller

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"chat/model"
)

// Chống tạo tài khoản clone để gom xu: mỗi tài khoản mới có sẵn 1.000 xu, nên nếu tạo hàng loạt
// rồi chuyển về một tài khoản thì in được xu vô hạn. Tài khoản chưa đủ newAccountHold tuổi không
// đưa xu cho người khác được: không chuyển xu, không mở hay vào bàn cược đấu với người khác (thua
// cố ý cũng là một cách chuyển xu). Không giới hạn số tài khoản theo IP vì cả công ty dùng chung
// một IP; chỉ ghi lại IP tạo tài khoản để tra khi cần.
const defaultNewAccountHold = 7 * 24 * time.Hour

// SetNewAccountHold đổi thời gian chờ của tài khoản mới (test dùng); <= 0 là tắt.
func (a *API) SetNewAccountHold(hold time.Duration) { a.newAccountHold = hold }

// clientIP: IP thật của người gửi request. Chạy sau Cloudflare Tunnel thì mọi request đến từ
// 127.0.0.1 và IP thật nằm ở header CF-Connecting-IP; header này chỉ tin khi request đến từ
// chính máy này (người ngoài gửi thẳng vào cổng LAN không giả được). Trả "" khi request đến từ
// máy chủ mà không qua tunnel (người quản trị, test).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	if !ip.IsLoopback() {
		return ipKey(ip)
	}
	if cf := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); cf != nil {
		return ipKey(cf)
	}
	return ""
}

// ipKey: IPv4 giữ nguyên; IPv6 gộp theo dải /64 (nhà mạng cấp cả dải /64 cho mỗi thuê bao).
func ipKey(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// coinsLockedUntil: tài khoản còn mới thì trả lúc được đưa xu cho người khác, không thì nil.
func (a *API) coinsLockedUntil(u model.User) *time.Time {
	if a.newAccountHold <= 0 || u.CreatedAt.IsZero() {
		return nil
	}
	until := u.CreatedAt.Add(a.newAccountHold)
	if !a.store.Now().Before(until) {
		return nil
	}
	return &until
}

// newAccountMsg: lời báo cho tài khoản mới chưa được đưa xu cho người khác.
func newAccountMsg(action string, until time.Time) string {
	t := until.In(vnZone)
	return fmt.Sprintf("Tài khoản mới cần đủ 7 ngày mới %s được (chống tạo tài khoản ảo gom xu). Bạn làm được từ %s ngày %s.",
		action, t.Format("15:04"), t.Format("02/01"))
}
