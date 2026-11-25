// museapi phơi tài khoản Muse của bạn ra thành API tương thích OpenAI.
//
//	MUSE_COOKIE=... go run ./cmd/museapi
//
// Biến môi trường:
//
//	MUSE_COOKIE   (bắt buộc) chuỗi Cookie copy từ trình duyệt đã đăng nhập muse.ai
//	MUSE_API_KEY  khoá bảo vệ API này; để trống là không kiểm tra (chỉ nên vậy khi nghe 127.0.0.1)
//	MUSE_ADDR     địa chỉ lắng nghe, mặc định 127.0.0.1:8099
//	MUSE_PROXY    proxy đi ra, ví dụ http://127.0.0.1:8888
//	MUSE_ORIGIN   mặc định https://muse.ai
//	MUSE_GATEWAY  mặc định wss://hatch.metaaivm.com/v1/noise
//	MUSE_UA       User-Agent, nên giống trình duyệt đã lấy cookie
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chat/env"
	"chat/muse"
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	_, _ = env.LoadDefault()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("museapi ")

	cookie := os.Getenv("MUSE_COOKIE")
	if cookie == "" {
		log.Fatal("thiếu MUSE_COOKIE — mở muse.ai đã đăng nhập, DevTools → Network → copy header Cookie")
	}
	c, err := muse.New(muse.Config{
		Cookie:     cookie,
		Origin:     envOr("MUSE_ORIGIN", ""),
		GatewayURL: envOr("MUSE_GATEWAY", ""),
		UserAgent:  envOr("MUSE_UA", ""),
		ProxyURL:   envOr("MUSE_PROXY", ""),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	addr := envOr("MUSE_ADDR", "127.0.0.1:8099")
	key := os.Getenv("MUSE_API_KEY")
	if key == "" {
		log.Print("CẢNH BÁO: MUSE_API_KEY trống — API không có xác thực, chỉ nên nghe trên 127.0.0.1")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           (&muse.Server{C: c, APIKey: key}).Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		// Không đặt WriteTimeout: một lượt Muse có thể chạy vài phút và SSE phải giữ kết nối.
	}

	// Kết nối thử ngay để báo lỗi cấu hình sớm, nhưng không chặn việc phục vụ.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if _, err := c.Gateway(ctx); err != nil {
			log.Printf("chưa kết nối được tới Muse: %v", err)
			return
		}
		log.Print("đã kết nối gateway Muse")
	}()

	go func() {
		log.Printf("nghe trên http://%s — model %q", addr, muse.ModelName)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Print("đang tắt…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
