package main

import (
	"chat/controller"
	"chat/database"
	"chat/socket"
	"chat/store"
	"chat/web"
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":4000", "địa chỉ HTTP server lắng nghe, ví dụ :8080")
	// localhost / tauri.localhost: origin của app desktop Tauri sau khi build
	// (tauri://localhost trên Linux/macOS, http://tauri.localhost trên Windows).
	origins := flag.String("origins", "localhost,localhost:*,127.0.0.1:*,tauri.localhost", "các origin được phép gọi API và mở websocket, phân tách bằng dấu phẩy")
	dbPath := flag.String("db", "chat.db", "đường dẫn file SQLite")
	uploadsDir := flag.String("uploads", "uploads", "thư mục lưu file/ảnh người dùng gửi")
	mode := flag.String("migrate", "up", "up | down (down: rollback rồi thoát, không start server)")
	steps := flag.Int("steps", 0, "số migration (0 = mặc định)")
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
	// nhắc việc tới giờ, bình chọn hết hạn
	go api.RunScheduler(ctx, 15*time.Second)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.Handler(web.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
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
