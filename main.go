package main

import (
	"chat/ai"
	"chat/controller"
	"chat/database"
	"chat/env"
	"chat/muse"
	"chat/rtc"
	"chat/socket"
	"chat/store"
	"chat/web"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	// Nạp .env trước tiên: các flag bên dưới lấy giá trị mặc định từ biến môi trường,
	// mà mặc định của flag được tính ngay lúc khai báo chứ không phải lúc Parse.
	if p, err := env.LoadDefault(); err != nil {
		log.Printf("đọc %s: %v", p, err)
	}

	addr := flag.String("addr", ":4000", "địa chỉ HTTP server lắng nghe, ví dụ :8080")
	// localhost / tauri.localhost: origin của app desktop Tauri sau khi build
	// (tauri://localhost trên Linux/macOS, http://tauri.localhost trên Windows).
	origins := flag.String("origins", "localhost,localhost:*,127.0.0.1:*,tauri.localhost", "các origin được phép gọi API và mở websocket, phân tách bằng dấu phẩy")
	dbPath := flag.String("db", "chat.db", "đường dẫn file SQLite")
	uploadsDir := flag.String("uploads", "uploads", "thư mục lưu file/ảnh người dùng gửi")
	mode := flag.String("migrate", "up", "up | down (down: rollback rồi thoát, không start server)")
	steps := flag.Int("steps", 0, "số migration (0 = mặc định)")
	iceJSON := flag.String("ice-servers", envOr("ICE_SERVERS", `[{"urls":["stun:stun.cloudflare.com:3478"]}]`),
		"máy chủ STUN/TURN cho gọi điện (JSON giống RTCIceServer[]; mặc định lấy ICE_SERVERS)")
	tlsAddr := flag.String("tls-addr", envOr("TLS_ADDR", ":4443"), "cổng HTTPS cho mạng LAN (trình duyệt chỉ cho micro/camera trên https); rỗng = tắt")
	tlsCert := flag.String("tls-cert", os.Getenv("TLS_CERT"), "file chứng chỉ (rỗng = tự tạo chứng chỉ tự ký)")
	tlsKey := flag.String("tls-key", os.Getenv("TLS_KEY"), "file khoá của chứng chỉ")
	turnAddr := flag.String("turn-addr", envOr("TURN_ADDR", ":3478"), "TURN nhúng (UDP+TCP) để gọi điện qua máy chủ khi hai máy không nối thẳng được; rỗng = tắt")
	turnIP := flag.String("turn-ip", os.Getenv("TURN_IP"), "IP client dùng để tới TURN (rỗng = tự đoán IP LAN)")
	giphyKey := flag.String("giphy-key", os.Getenv("GIPHY_API_KEY"), "API key GIPHY để tìm GIF (mặc định lấy từ biến môi trường GIPHY_API_KEY; rỗng = tắt)")
	flag.Parse()

	db, err := database.InitDB(*dbPath)
	if err != nil {
		log.Fatalf("khởi tạo database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(database.MigrationOpt{
		DB:    db,
		Mode:  database.Mode(*mode),
		Steps: *steps,
	}); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if database.Mode(*mode) == database.ModeDown {
		return
	}

	// Ctrl+C / SIGTERM -> tắt server và Hub gọn gàng
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(*uploadsDir, 0o755); err != nil {
		log.Fatalf("tạo thư mục uploads: %v", err)
	}

	hub := socket.NewHub()
	api := controller.NewAPI(store.New(db), hub, splitList(*origins), *uploadsDir)
	api.SetGiphyKey(*giphyKey)
	if *giphyKey == "" {
		log.Printf("Tìm GIF đang tắt: đặt GIPHY_API_KEY (hoặc -giphy-key) để bật")
	}
	var ice []controller.ICEServer
	if err := json.Unmarshal([]byte(*iceJSON), &ice); err != nil {
		log.Fatalf("-ice-servers không phải JSON hợp lệ: %v", err)
	}
	api.SetICEServers(ice)
	api.SetUIBuild(web.BuildID())

	if *turnAddr != "" {
		// Lúc máy vừa khởi động mạng LAN có thể chưa lên: thử lại tới khi có IP thay vì tắt TURN
		// cả ngày (không có TURN thì máy nào không nối thẳng được với nhau là không nghe/thấy nhau).
		go func() {
			for wait := 0; ; wait++ {
				ip := rtc.DetectLANIP()
				if *turnIP != "" {
					ip = net.ParseIP(*turnIP)
				}
				if ip == nil {
					if wait == 0 {
						log.Printf("TURN chưa chạy: chưa có IP LAN, sẽ thử lại (hoặc đặt -turn-ip)")
					}
					time.Sleep(5 * time.Second)
					continue
				}
				t, err := rtc.StartTURN(*turnAddr, ip)
				if err != nil {
					log.Printf("TURN tắt: %v", err)
					return
				}
				api.SetTURN(t)
				log.Printf("TURN chạy tại %s:%s (relay UDP %d-%d)", ip, strings.TrimPrefix(*turnAddr, ":"), rtc.RelayMinPort, rtc.RelayMaxPort)
				return
			}
		}()
	}

	// Trợ lý AI: nhà cung cấp nào có key thì dùng (xem ai/config.go), Pollinations miễn phí làm dự phòng.
	aiChat, aiImages := ai.FromEnv(os.Getenv)
	aiSearch := ai.SearchFromEnv(os.Getenv)
	// Muse: có MUSE_COOKIE thì dựng cầu nối nội bộ và nối vào cuối chuỗi nhà cung cấp.
	if cookie := os.Getenv("MUSE_COOKIE"); cookie != "" {
		if mc, err := muse.New(muse.Config{
			Cookie:     cookie,
			Origin:     os.Getenv("MUSE_ORIGIN"),
			GatewayURL: os.Getenv("MUSE_GATEWAY"),
			UserAgent:  os.Getenv("MUSE_UA"),
			ProxyURL:   os.Getenv("MUSE_PROXY"),
		}); err != nil {
			log.Printf("Muse tắt: %v", err)
		} else if lb, err := muse.StartLoopback(mc); err != nil {
			mc.Close()
			log.Printf("Muse tắt: %v", err)
		} else {
			defer lb.Close()
			defer mc.Close()
			// Mặc định Muse đứng ĐẦU cả hai chuỗi: nó làm được cả chữ lẫn ảnh, nên để nó
			// nhận việc trước, các bên khác lùi về làm dự phòng. MUSE_ORDER=last để đảo lại.
			last := strings.EqualFold(os.Getenv("MUSE_ORDER"), "last")
			if aiChat == nil {
				aiChat = ai.NewChat()
			}
			cp := ai.Muse(lb.BaseURL, lb.Key)
			if last {
				aiChat.Providers = append(aiChat.Providers, cp)
			} else {
				aiChat.Providers = append([]*ai.Provider{cp}, aiChat.Providers...)
			}
			if aiImages == nil {
				aiImages = &ai.Images{}
			}
			var ip ai.ImageProvider = museEditor{muse.NewImageProvider(mc)}
			if last {
				aiImages.Providers = append(aiImages.Providers, ip)
			} else {
				aiImages.Providers = append([]ai.ImageProvider{ip}, aiImages.Providers...)
			}
			where := "đầu chuỗi"
			if last {
				where = "cuối chuỗi"
			}
			log.Printf("Muse bật: cầu nối nội bộ tại %s (chỉ loopback), đứng %s cho cả chat và vẽ ảnh", lb.BaseURL, where)
		}
	}

	if err := api.SetAI(ctx, aiChat, aiImages, aiSearch); err != nil {
		log.Printf("AI tắt: %v", err)
	} else if aiChat != nil || aiImages != nil {
		var chatNames, imageNames []string
		if aiChat != nil {
			chatNames = aiChat.Names()
		}
		if aiImages != nil {
			imageNames = aiImages.Names()
		}
		var searchNames []string
		if aiSearch != nil {
			searchNames = aiSearch.Names()
		}
		log.Printf("Trợ lý AI (@ai): chat %v, vẽ ảnh %v, tìm web %v", chatNames, imageNames, searchNames)
	}

	// nhắc việc tới giờ, bình chọn hết hạn
	go api.RunScheduler(ctx, 15*time.Second)
	// trò chơi: chạy tiếp ván đang dở, chuyển pha khi hết giờ
	go api.RunGames(ctx)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.Handler(web.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	var tlsSrv *http.Server
	if *tlsAddr != "" {
		cert, err := rtc.LoadOrCreateCert(filepath.Join(filepath.Dir(*dbPath), "tls"), *tlsCert, *tlsKey)
		if err != nil {
			log.Printf("HTTPS tắt: %v", err)
		} else {
			api.SetTLSPort(strings.TrimPrefix(*tlsAddr, ":"))
			tlsSrv = &http.Server{
				Addr:              *tlsAddr,
				Handler:           srv.Handler,
				ReadHeaderTimeout: 10 * time.Second,
				TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
			}
			go func() {
				log.Printf("HTTPS (LAN, chứng chỉ tự ký) tại https://<IP máy này>%s: dùng để gọi điện trong LAN", displayAddr(*tlsAddr))
				if err := tlsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Printf("HTTPS: %v", err)
				}
			}()
		}
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Server running at http://localhost%s (API: /api, websocket: /ws)", displayAddr(*addr))
		if web.Available() {
			log.Printf("Giao diện web: mở http://<IP máy này>%s trên trình duyệt", displayAddr(*addr))
		} else {
			log.Printf("Binary chưa kèm giao diện web (chạy `make web` rồi build lại)")
		}
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("start server: %v", err)
		}
	case <-ctx.Done():
		log.Println("shutting down...")
		hub.Shutdown() // đóng các websocket (Shutdown của http.Server không đợi kết nối đã hijack)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		if tlsSrv != nil {
			_ = tlsSrv.Shutdown(shutdownCtx)
		}
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ":3000" -> ":3000", "0.0.0.0:3000" -> ":3000"
func displayAddr(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return addr
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
