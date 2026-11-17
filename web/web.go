// Package web nhúng bản build giao diện (desktop/ build ra web/ui/dist) vào binary Go,
// để mở http://<ip>:<port>/ trên trình duyệt là dùng được, không cần cài app.
package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Nhúng cả thư mục ui (luôn có README.md) để vẫn build được khi chưa build giao diện.
//
//go:embed all:ui
var uiFS embed.FS

// Available cho biết binary này có kèm giao diện web không.
func Available() bool {
	_, err := fs.Stat(uiFS, "ui/dist/index.html")
	return err == nil
}

// BuildID: mã bản build giao diện đang nhúng (build.json do Vite tạo), rỗng nếu không có.
func BuildID() string {
	b, err := fs.ReadFile(uiFS, "ui/dist/build.json")
	if err != nil {
		return ""
	}
	var v struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return v.ID
}

// Handler phục vụ giao diện kiểu SPA: file có thật thì trả file, còn lại trả index.html.
func Handler() http.Handler {
	if !Available() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Binary này chưa kèm giao diện web. Chạy `make web` rồi build lại server (`make server`).\n"))
		})
	}

	dist, _ := fs.Sub(uiFS, "ui/dist")
	files := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			// Đường dẫn không phải file tĩnh: trả về app, để phía client xử lý.
			p = "index.html"
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if strings.HasPrefix(p, "assets/") {
			// Tên file đã kèm hash nội dung, cache lâu thoải mái.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// index.html phải luôn lấy bản mới để nhận bản cập nhật ngay.
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
