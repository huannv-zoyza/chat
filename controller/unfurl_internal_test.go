package controller

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "104.21.77.41": true, "2606:4700::6810:84e5": true,
		"127.0.0.1": false, "10.1.2.3": false, "192.168.30.36": false, "172.17.0.1": false,
		"169.254.169.254": false, "100.64.1.1": false, "0.0.0.0": false, "::1": false,
		"fe80::1": false, "fd00::1": false, "::ffff:127.0.0.1": false,
	} {
		if got := publicIP(net.ParseIP(ip)); got != want {
			t.Errorf("publicIP(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestYoutubeID(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10": "dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ":                    "dQw4w9WgXcQ",
		"https://youtube.com/shorts/dQw4w9WgXcQ":          "dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ":       "dQw4w9WgXcQ",
		"https://www.youtube.com/@kenh":                   "",
		"https://youtu.be.evil.com/dQw4w9WgXcQ":           "",
	} {
		u, _ := url.Parse(raw)
		if got := youtubeID(u); got != want {
			t.Errorf("youtubeID(%s) = %q, want %q", raw, got, want)
		}
	}
}

const ogPage = `<!doctype html><html><head>
<meta charset="utf-8"><title>Tiêu đề thẻ title</title>
<meta property="og:title" content="Bản tin công nghệ">
<meta property="og:description" content="Mô tả ngắn &amp; gọn">
<meta property="og:image" content="/anh/thumb.jpg">
<meta property="og:site_name" content="Báo Mẫu">
</head><body><meta property="og:title" content="không được lấy"></body></html>`

func TestFetchPreview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clip.mp4":
			w.Header().Set("Content-Type", "video/mp4")
		case "/song.mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
		case "/redirect":
			http.Redirect(w, r, "/bai-viet", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(ogPage))
		}
	}))
	defer srv.Close()

	// Client cho phép kết nối localhost (chỉ trong test) để kiểm tra phần đọc trang.
	a := &API{unfurlClient: srv.Client()}
	get := func(path string) LinkPreview {
		u, _ := url.Parse(srv.URL + path)
		return a.fetchPreview(context.Background(), u)
	}
	p := get("/redirect")
	if p.Kind != "page" || p.Title != "Bản tin công nghệ" || p.Description != "Mô tả ngắn & gọn" ||
		p.SiteName != "Báo Mẫu" || p.Image != srv.URL+"/anh/thumb.jpg" {
		t.Fatalf("page preview: %+v", p)
	}
	if k := get("/clip.mp4").Kind; k != "video" {
		t.Fatalf("video kind: %s", k)
	}
	if k := get("/song.mp3").Kind; k != "audio" {
		t.Fatalf("audio kind: %s", k)
	}
}

// Client thật của server phải từ chối mọi kết nối tới mạng nội bộ (chống SSRF),
// kể cả khi link trỏ thẳng vào IP hay vào tên miền phân giải ra 127.0.0.1.
func TestUnfurlBlocksInternal(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(ogPage))
	}))
	defer srv.Close()
	a := &API{unfurlClient: newUnfurlClient()}
	for _, raw := range []string{srv.URL + "/", strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/"} {
		u, _ := url.Parse(raw)
		if p := a.fetchPreview(context.Background(), u); p.Kind != "none" || p.Title != "" {
			t.Fatalf("internal URL %s leaked: %+v", raw, p)
		}
	}
	if hit {
		t.Fatal("server made a request to an internal address")
	}
}
