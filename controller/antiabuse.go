package controller

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"chat/model"
)

// Chống tạo tài khoản clone để gom xu: mỗi tài khoản mới có sẵn 1.000 xu, nên nếu tạo hàng loạt
// rồi chuyển về một tài khoản thì in được xu vô hạn. Các lớp chặn:
//   - mỗi máy (mã thiết bị app gửi trong header X-Device-ID) tạo tối đa maxAccountsPerDevice tài khoản;
//   - hai tài khoản từng dùng chung một máy không đưa xu cho nhau được;
//   - tài khoản chưa đủ newAccountHold tuổi không đưa xu cho ai được.
//
// "Đưa xu" gồm chuyển xu và cược đấu với người khác (thua cố ý cũng là một cách chuyển xu).
// Không giới hạn theo IP vì cả công ty dùng chung một IP; chỉ ghi lại IP tạo tài khoản để tra khi cần.
const (
	defaultNewAccountHold = 7 * 24 * time.Hour
	maxAccountsPerDevice  = 2
)

// deviceIDRe: mã máy app tự sinh (hex/uuid). Sai định dạng thì coi như không có.
var deviceIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{16,64}$`)

// deviceID: mã máy của request, "" nếu không có (app cũ, script, test).
func deviceID(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("X-Device-ID")); deviceIDRe.MatchString(id) {
		return strings.ToLower(id)
	}
	return ""
}

// touchDevice ghi nhận userID đang dùng máy của request; mỗi cặp (tài khoản, máy) chỉ ghi một lần
// mỗi lần chạy server để không ghi DB ở mọi request.
func (a *API) touchDevice(r *http.Request, userID int64) {
	dev := deviceID(r)
	if dev == "" {
		return
	}
	key := strconv.FormatInt(userID, 10) + ":" + dev
	if _, seen := a.devicesSeen.LoadOrStore(key, struct{}{}); seen {
		return
	}
	if err := a.store.TouchDevice(context.WithoutCancel(r.Context()), userID, dev); err != nil {
		a.devicesSeen.Delete(key)
		log.Printf("touch device %d: %v", userID, err)
	}
}

// sharedDeviceMsg: lời báo khi hai tài khoản dùng chung máy định đưa xu cho nhau.
const sharedDeviceMsg = "Hai tài khoản đã từng đăng nhập trên cùng một máy nên không chuyển xu hay cược với nhau được (chống tạo tài khoản ảo gom xu)."

// sharesDeviceWith: me từng dùng chung máy với ai trong others.
func (a *API) sharesDeviceWith(ctx context.Context, me int64, others ...int64) (bool, error) {
	for _, o := range others {
		if o == me {
			continue
		}
		shared, err := a.store.SharesDevice(ctx, me, o)
		if err != nil || shared {
			return shared, err
		}
	}
	return false, nil
}

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
