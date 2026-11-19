package controller

// Tìm ảnh trên mạng cho Trợ lý AI, giống cách một người làm: tìm web, mở các trang kết quả, lấy ảnh
// trong trang (ảnh đại diện og:image và các ảnh lớn), rồi tải về gửi vào kênh hoặc chỉ đưa link.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"chat/model"

	"golang.org/x/net/html"
)

const (
	imgSearchPages   = 8       // số trang kết quả mở ra tìm ảnh
	imgPerPage       = 4       // số ảnh lấy tối đa mỗi trang
	imgMaxBytes      = 8 << 20 // ảnh tải về tối đa 8 MB
	imgMinSide       = 300     // bỏ ảnh nhỏ (icon, logo, ảnh thu nhỏ)
	imgPageMaxBody   = 2 << 20
	imgSearchTimeout = 45 * time.Second
)

// imgSkipWords: tên file kiểu biểu tượng / quảng cáo, không phải ảnh nội dung.
var imgSkipWords = []string{"logo", "icon", "favicon", "avatar", "sprite", "emoji", "banner", "pixel", "placeholder", "loading", "blank.", "spacer", "/ads/", "badge", "button"}

type webImage struct {
	URL, Page, Site string
}

// pageImages: link ảnh trong một trang (og:image trước, rồi các <img>), tối đa n. Link trỏ thẳng tới ảnh thì trả chính nó.
func (a *API) pageImages(ctx context.Context, page string, n int) []webImage {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,image/*;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "vi,en;q=0.8")
	res, err := a.unfurlClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil
	}
	base := res.Request.URL
	site := strings.TrimPrefix(base.Hostname(), "www.")
	ctype, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if strings.HasPrefix(ctype, "image/") {
		return []webImage{{URL: base.String(), Page: page, Site: site}}
	}
	if ctype != "text/html" && ctype != "application/xhtml+xml" {
		return nil
	}
	var out []webImage
	seen := map[string]bool{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "data:") || len(out) >= n {
			return
		}
		u, err := base.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
		s := u.String()
		low := strings.ToLower(u.Path)
		if seen[s] || strings.HasSuffix(low, ".svg") || strings.HasSuffix(low, ".ico") {
			return
		}
		for _, w := range imgSkipWords {
			if strings.Contains(strings.ToLower(s), w) {
				return
			}
		}
		seen[s] = true
		out = append(out, webImage{URL: s, Page: page, Site: site})
	}
	var og []string
	z := html.NewTokenizer(io.LimitReader(res.Body, imgPageMaxBody))
	for len(out) < n {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		attrs := map[string]string{}
		for hasAttr {
			var k, v []byte
			k, v, hasAttr = z.TagAttr()
			attrs[strings.ToLower(string(k))] = string(v)
		}
		switch string(name) {
		case "meta":
			key := strings.ToLower(attrs["property"] + attrs["name"])
			if key == "og:image" || key == "og:image:secure_url" || key == "twitter:image" {
				og = append(og, attrs["content"])
			}
		case "body":
			for _, s := range og { // ảnh đại diện của trang thường là ảnh chính
				add(s)
			}
			og = nil
		case "img", "source":
			if small(attrs["width"]) || small(attrs["height"]) {
				continue
			}
			src := firstNonEmpty(attrs["data-src"], attrs["data-original"], attrs["data-lazy-src"], largestSrc(attrs["srcset"]),
				largestSrc(attrs["data-srcset"]), attrs["src"])
			add(src)
		}
	}
	for _, s := range og { // trang không có <body> rõ ràng
		add(s)
	}
	return out
}

// small: thuộc tính width/height ghi rõ là ảnh nhỏ.
func small(v string) bool {
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), "px"))
	return err == nil && n > 0 && n < imgMinSide/2
}

// largestSrc: ảnh cuối trong srcset (thường là bản lớn nhất).
func largestSrc(srcset string) string {
	parts := strings.Split(srcset, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if f := strings.Fields(parts[i]); len(f) > 0 {
			return f[0]
		}
	}
	return ""
}

// downloadImage tải một ảnh (qua client chặn địa chỉ nội bộ): chỉ nhận JPEG/PNG/WebP/GIF đủ lớn.
func (a *API) downloadImage(ctx context.Context, im webImage) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, im.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8")
	req.Header.Set("Referer", im.Page) // nhiều trang chặn tải ảnh khi không có Referer
	res, err := a.unfurlClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if res.ContentLength > imgMaxBytes {
		return nil, fmt.Errorf("ảnh quá lớn")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, imgMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > imgMaxBytes {
		return nil, fmt.Errorf("ảnh quá lớn")
	}
	switch http.DetectContentType(data) {
	case "image/webp": // không có bộ đọc WebP: đoán theo dung lượng
		if len(data) < 40<<10 {
			return nil, fmt.Errorf("ảnh nhỏ")
		}
		return data, nil
	case "image/jpeg", "image/png", "image/gif":
	default:
		return nil, fmt.Errorf("không phải ảnh")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < imgMinSide || cfg.Height < imgMinSide {
		return nil, fmt.Errorf("ảnh nhỏ hoặc hỏng")
	}
	return data, nil
}

// findWebImages: tìm web rồi gom ảnh từ các trang kết quả, xen kẽ giữa các trang cho đa dạng.
func (a *API) findWebImages(ctx context.Context, query string) []webImage {
	results, _, err := a.ai.search.Search(ctx, query, imgSearchPages)
	if err != nil || len(results) == 0 {
		return nil
	}
	per := make([][]webImage, len(results))
	var wg sync.WaitGroup
	for i, r := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			per[i] = a.pageImages(pctx, r.URL, imgPerPage)
		}()
	}
	wg.Wait()
	var out []webImage
	seen := map[string]bool{}
	for k := 0; k < imgPerPage; k++ {
		for _, list := range per {
			if k < len(list) && !seen[list[k].URL] {
				seen[list[k].URL] = true
				out = append(out, list[k])
			}
		}
	}
	return out
}

// searchImages: công cụ search_images của agent.
func (r *agentRun) searchImages(ctx context.Context, query string, count int, send bool) string {
	if query = strings.TrimSpace(query); query == "" {
		return "Lỗi: cần nội dung ảnh cần tìm."
	}
	count = max(1, min(count, 4))
	if send && r.imageSends >= 1 {
		return "Đã gửi ảnh cho câu hỏi này rồi."
	}
	ctx, cancel := context.WithTimeout(ctx, imgSearchTimeout)
	defer cancel()
	r.status("Đang tìm ảnh trên web: «" + shorten(query, 60) + "»…")
	cands := r.a.findWebImages(ctx, query)
	if len(cands) == 0 {
		return "Không tìm được ảnh nào trên web cho «" + query + "»."
	}
	r.got.Web += len(cands)
	if !send {
		var b strings.Builder
		fmt.Fprintf(&b, "Link ảnh tìm được cho «%s» (ảnh — trang chứa ảnh):\n", query)
		for i, c := range cands {
			if i == count*2 {
				break
			}
			fmt.Fprintf(&b, "- %s — %s\n", c.URL, c.Page)
		}
		return clipTool(b.String())
	}

	r.status("Đang tải ảnh về…")
	type got struct {
		data []byte
		from webImage
	}
	var picked []got
	hashes := map[[32]byte]bool{}
	for _, c := range cands {
		if len(picked) == count || ctx.Err() != nil {
			break
		}
		data, err := r.a.downloadImage(ctx, c)
		if err != nil {
			continue
		}
		h := sha256.Sum256(data)
		if hashes[h] {
			continue
		}
		hashes[h] = true
		picked = append(picked, got{data, c})
	}
	if len(picked) == 0 {
		return fmt.Sprintf("Tìm thấy %d link ảnh nhưng không tải được ảnh nào (trang chặn tải hoặc ảnh quá nhỏ).", len(cands))
	}
	r.imageSends++
	m, err := r.reply("🖼️ Đang gửi ảnh…")
	if err != nil {
		return "Lỗi khi gửi ảnh."
	}
	var atts []model.Attachment
	var sources []string
	seenSite := map[string]bool{}
	for i, p := range picked {
		att, err := r.a.saveBytes(p.data, fmt.Sprintf("anh-%s-%d", fileSlug(query), i+1))
		if err != nil {
			continue
		}
		atts = append(atts, att)
		if !seenSite[p.from.Site] {
			seenSite[p.from.Site] = true
			sources = append(sources, fmt.Sprintf("[%s](%s)", p.from.Site, p.from.Page))
		}
	}
	if len(atts) == 0 {
		r.a.finishAI(ctx, m, "⚠️ Không lưu được ảnh, thử lại nhé.")
		return "Lỗi khi lưu ảnh."
	}
	if err := r.a.store.AddAttachments(ctx, m.ID, atts); err != nil {
		for _, at := range atts {
			r.a.removeUpload(at.StorageKey)
		}
		log.Printf("ai web image attach: %v", err)
		return "Lỗi khi lưu ảnh."
	}
	r.a.finishAI(context.WithoutCancel(ctx), m, "🖼️ Ảnh trên mạng cho “"+query+"”\n\n_— nguồn: "+strings.Join(sources, ", ")+"_")
	log.Printf("ai: gửi %d ảnh web cho %s ở kênh %d (%s)", len(atts), r.asker.Username, r.ch.ID, strings.Join(sources, ", "))
	return fmt.Sprintf("Đã gửi %d ảnh vào kênh (nguồn: %s). Chỉ cần nói ngắn gọn, không chép lại link ảnh.", len(atts), strings.Join(sources, ", "))
}
