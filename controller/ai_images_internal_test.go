package controller

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat/ai"
)

type fakeSearch struct{ urls []string }

func (fakeSearch) Name() string { return "fake" }
func (f fakeSearch) Search(_ context.Context, _ string, n int) ([]ai.SearchResult, error) {
	var out []ai.SearchResult
	for _, u := range f.urls {
		out = append(out, ai.SearchResult{Title: u, URL: u})
	}
	return out, nil
}

func pngOf(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, c)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestFindAndDownloadWebImages(t *testing.T) {
	big1, big2 := pngOf(800, 600, color.White), pngOf(640, 640, color.Black)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			fmt.Fprintf(w, `<html><head><meta property="og:image" content="/img/big1.png"></head><body>
<img src="/img/logo.png"><img src="/img/tiny.png" width="40"><img data-src="%s/img/big2.png" src="data:,">
<img srcset="/img/s.png 300w, /img/big1.png 1200w"></body></html>`, srv.URL)
		case "/b":
			fmt.Fprint(w, `<html><body><img src="/img/small.png"><img src="/img/page.html"></body></html>`)
		case "/direct.png":
			w.Write(big2)
		case "/img/big1.png":
			w.Write(big1)
		case "/img/big2.png":
			if r.Referer() == "" {
				w.WriteHeader(403) // chặn tải khi không có Referer
				return
			}
			w.Write(big2)
		case "/img/small.png":
			w.Write(pngOf(100, 100, color.White))
		case "/img/page.html":
			fmt.Fprint(w, "<html>not an image</html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := &API{unfurlClient: srv.Client(), ai: &aiService{search: &ai.Search{Providers: []ai.Searcher{
		fakeSearch{[]string{srv.URL + "/a", srv.URL + "/b", srv.URL + "/direct.png"}}}}}}
	ctx := context.Background()

	imgs := a.pageImages(ctx, srv.URL+"/a", 4)
	var got []string
	for _, im := range imgs {
		got = append(got, strings.TrimPrefix(im.URL, srv.URL))
	}
	if strings.Join(got, ",") != "/img/big1.png,/img/big2.png" {
		t.Fatalf("page images: %v", got) // og:image trước, bỏ logo / ảnh nhỏ / data:, không trùng
	}

	cands := a.findWebImages(ctx, "mèo")
	if len(cands) < 3 {
		t.Fatalf("candidates: %+v", cands)
	}
	ok := 0
	for _, c := range cands {
		if data, err := a.downloadImage(ctx, c); err == nil {
			if !bytes.Equal(data, big1) && !bytes.Equal(data, big2) {
				t.Fatalf("unexpected image from %s", c.URL)
			}
			ok++
		} else if strings.HasSuffix(c.URL, "big1.png") || strings.HasSuffix(c.URL, "big2.png") {
			t.Fatalf("download %s: %v", c.URL, err)
		}
	}
	if ok < 3 { // big1, big2 (có Referer), direct.png; small.png và page.html bị loại
		t.Fatalf("downloaded %d images", ok)
	}
}
