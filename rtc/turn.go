// Package rtc: máy chủ TURN nhúng (chuyển tiếp âm thanh/hình khi hai máy không nối thẳng được)
// và chứng chỉ HTTPS tự ký cho mạng LAN (trình duyệt chỉ cho dùng micro/camera trên https).
package rtc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v4"
)

// Cổng UDP dùng để chuyển tiếp media (mở trên tường lửa nếu có bật). Mỗi kết nối WebRTC xin
// một cổng cho mỗi card mạng × (UDP, TCP) của máy người dùng, kể cả khi rốt cuộc nối thẳng; thoại
// trong ván kiểu mesh 10 người đã là 90 kết nối, nên dải phải rộng (41 cổng cũ chỉ đủ ~9 kết nối,
// cặp nào cần chuyển tiếp mà hết cổng thì không nghe được nhau).
const (
	RelayMinPort = 49160
	RelayMaxPort = 50999
)

type TURN struct {
	server *turn.Server
	secret string
	host   string // IP client dùng để tới máy chủ TURN
	port   int
}

// StartTURN mở TURN trên addr (vd ":3478", cả UDP lẫn TCP), chuyển tiếp qua relayIP.
// Mật khẩu là loại tạm thời (TURN REST): mỗi người dùng nhận một cặp hết hạn sau vài giờ.
func StartTURN(addr string, relayIP net.IP) (*TURN, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(portStr)
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	secret := hex.EncodeToString(b)

	udp, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return nil, fmt.Errorf("TURN udp: %w", err)
	}
	tcp, err := net.Listen("tcp4", addr)
	if err != nil {
		udp.Close()
		return nil, fmt.Errorf("TURN tcp: %w", err)
	}
	gen := func() turn.RelayAddressGenerator {
		return &turn.RelayAddressGeneratorPortRange{
			RelayAddress: relayIP, Address: "0.0.0.0", MinPort: RelayMinPort, MaxPort: RelayMaxPort,
		}
	}
	lf := logging.NewDefaultLoggerFactory()
	lf.DefaultLogLevel = logging.LogLevelWarn
	srv, err := turn.NewServer(turn.ServerConfig{
		Realm:             "gochat",
		AuthHandler:       turn.LongTermTURNRESTAuthHandler(secret, lf.NewLogger("turn")),
		LoggerFactory:     lf,
		PacketConnConfigs: []turn.PacketConnConfig{{PacketConn: udp, RelayAddressGenerator: gen()}},
		ListenerConfigs:   []turn.ListenerConfig{{Listener: tcp, RelayAddressGenerator: gen()}},
	})
	if err != nil {
		udp.Close()
		tcp.Close()
		return nil, err
	}
	return &TURN{server: srv, secret: secret, host: relayIP.String(), port: port}, nil
}

func (t *TURN) Close() error { return t.server.Close() }

// Credentials: địa chỉ + tài khoản tạm (12 giờ) cho user, dạng RTCIceServer.
func (t *TURN) Credentials(user string) (urls []string, username, credential string, err error) {
	username, credential, err = turn.GenerateLongTermTURNRESTCredentials(t.secret, user, 12*time.Hour)
	if err != nil {
		return nil, "", "", err
	}
	hp := net.JoinHostPort(t.host, strconv.Itoa(t.port))
	return []string{"turn:" + hp + "?transport=udp", "turn:" + hp + "?transport=tcp"}, username, credential, nil
}

// DetectLANIP đoán IP LAN của máy: bỏ loopback và card ảo của Docker/VPN, ưu tiên
// 192.168.x.x, rồi 10.x.x.x, rồi 172.16–31.x.x.
func DetectLANIP() net.IP {
	ifaces, _ := net.Interfaces()
	var best net.IP
	bestRank := 99
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		n := ifc.Name
		if strings.HasPrefix(n, "docker") || strings.HasPrefix(n, "br-") || strings.HasPrefix(n, "veth") ||
			strings.HasPrefix(n, "virbr") || strings.HasPrefix(n, "tun") || strings.HasPrefix(n, "wg") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ip := ipn.IP.To4()
			rank := 3
			switch {
			case ip[0] == 192 && ip[1] == 168:
				rank = 0
			case ip[0] == 10:
				rank = 1
			case ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31:
				rank = 2
			}
			if rank < bestRank {
				best, bestRank = ip, rank
			}
		}
	}
	return best
}
