package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

// LinkPreview: thông tin hiển thị cho một link trong tin nhắn.
//
//	kind = page     trang web có tiêu đề/mô tả/ảnh (OpenGraph)
//	       image    link trực tiếp tới ảnh
//	       video    link trực tiếp tới video  -> phát bằng <video>
//	       audio    link trực tiếp tới âm thanh -> phát bằng <audio>
//	       youtube  video YouTube -> thumbnail, bấm thì nhúng trình phát
//	       none     không lấy được gì đáng hiển thị
type LinkPreview struct {
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
	Image       string `json:"image,omitempty"`
	EmbedURL    string `json:"embed_url,omitempty"`
}

const (
	unfurlMaxBody  = 1 << 20 // chỉ đọc 1 MB đầu của trang
	unfurlCacheTTL = 6 * time.Hour
	unfurlFailTTL  = 10 * time.Minute
	unfurlCacheMax = 2000
)

/* ---------- Chặn SSRF: không cho server mở kết nối tới mạng nội bộ ---------- */

var errBlockedAddress = errors.New("địa chỉ nội bộ bị chặn")

// 100.64.0.0/10 (CGNAT) không nằm trong IsPrivate nhưng cũng là mạng nội bộ.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// publicIP: chỉ cho phép địa chỉ internet công khai.
func publicIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
		if ip4[0] == 0 || cgnat.Contains(ip4) {
			return false
		}
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast())
}

// newUnfurlClient: HTTP client kiểm tra IP ở bước kết nối (sau khi phân giải DNS, áp dụng cả cho
// từng lần chuyển hướng), nên tên miền trỏ về 127.0.0.1/192.168.x.x cũng bị chặn.
func newUnfurlClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !publicIP(ip) {
				return errBlockedAddress
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil, // không đi qua proxy hệ thống (proxy có thể truy cập mạng nội bộ)
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
			MaxIdleConns:          20,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("chuyển hướng quá nhiều lần")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("chỉ hỗ trợ http/https")
			}
			return nil
		},
	}
}

/* ---------- Bộ nhớ đệm ---------- */

type unfurlEntry struct {
	preview LinkPreview
	expires time.Time
}

type unfurlCache struct {
	mu sync.Mutex
	m  map[string]unfurlEntry
}

func (c *unfurlCache) get(key string) (LinkPreview, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.expires) {
		return LinkPreview{}, false
	}
	return e.preview, true
}

func (c *unfurlCache) put(key string, p LinkPreview, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]unfurlEntry{}
	}
	if len(c.m) >= unfurlCacheMax {
		for k := range c.m { // bỏ bớt một mục bất kỳ khi đầy
			delete(c.m, k)
			break
		}
	}
	c.m[key] = unfurlEntry{preview: p, expires: time.Now().Add(ttl)}
}

/* ---------- GET /api/unfurl?url= ---------- */

func (a *API) unfurl(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 2048 {
		writeError(w, http.StatusBadRequest, "Link không hợp lệ.")
		return
	}
	if p, ok := a.unfurls.get(raw); ok {
		writeJSON(w, http.StatusOK, p)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	p := a.fetchPreview(ctx, u)
	ttl := unfurlCacheTTL
	if p.Kind == "none" {
		ttl = unfurlFailTTL
	}
	a.unfurls.put(raw, p, ttl)
	writeJSON(w, http.StatusOK, p)
}

var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// youtubeID nhận các dạng youtube.com/watch?v=ID, youtu.be/ID, youtube.com/shorts/ID, /embed/ID.
func youtubeID(u *url.URL) string {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else if parts := strings.Split(strings.Trim(u.Path, "/"), "/"); len(parts) == 2 &&
			(parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live") {
			id = parts[1]
		}
	}
	if youtubeIDRe.MatchString(id) {
		return id
	}
	return ""
}

func (a *API) fetchPreview(ctx context.Context, u *url.URL) LinkPreview {
	p := LinkPreview{URL: u.String(), Kind: "none"}

	if id := youtubeID(u); id != "" {
		p.Kind = "youtube"
		p.SiteName = "YouTube"
		p.Image = "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"
		p.EmbedURL = "https://www.youtube-nocookie.com/embed/" + id + "?autoplay=1&rel=0"
		// tiêu đề qua oEmbed (nhẹ hơn tải cả trang)
		var oe struct {
			Title      string `json:"title"`
			AuthorName string `json:"author_name"`
		}
		q := "https://www.youtube.com/oembed?format=json&url=" + url.QueryEscape(u.String())
		if err := a.getJSON(ctx, q, &oe); err == nil {
			p.Title = oe.Title
			p.Description = oe.AuthorName
		}
		return p
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return p
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GoChatBot/1.0; +link-preview)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("Accept-Language", "vi,en;q=0.8")
	res, err := a.unfurlClient.Do(req)
	if err != nil {
		return p
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return p
	}

	ctype, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(ctype, "image/") && ctype != "image/svg+xml":
		p.Kind = "image"
		p.Image = res.Request.URL.String()
		return p
	case strings.HasPrefix(ctype, "video/"):
		p.Kind = "video"
		return p
	case strings.HasPrefix(ctype, "audio/"):
		p.Kind = "audio"
		return p
	case ctype != "text/html" && ctype != "application/xhtml+xml":
		return p
	}

	meta := parseMeta(io.LimitReader(res.Body, unfurlMaxBody))
	p.Title = firstNonEmpty(meta["og:title"], meta["twitter:title"], meta["title"])
	p.Description = firstNonEmpty(meta["og:description"], meta["twitter:description"], meta["description"])
	p.SiteName = firstNonEmpty(meta["og:site_name"], res.Request.URL.Hostname())
	if img := firstNonEmpty(meta["og:image:secure_url"], meta["og:image"], meta["twitter:image"]); img != "" {
		if abs, err := res.Request.URL.Parse(img); err == nil && (abs.Scheme == "http" || abs.Scheme == "https") {
			p.Image = abs.String()
		}
	}
	p.Title = truncateRunes(p.Title, 200)
	p.Description = truncateRunes(p.Description, 300)
	if p.Title != "" || p.Description != "" || p.Image != "" {
		p.Kind = "page"
	}
	return p
}

func (a *API) getJSON(ctx context.Context, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := a.unfurlClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(dst)
}

// parseMeta đọc <title> và các thẻ <meta property|name=... content=...> trong <head>.
func parseMeta(r io.Reader) map[string]string {
	out := map[string]string{}
	z := html.NewTokenizer(r)
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "title":
				inTitle = true
			case "meta":
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "property", "name":
						key = strings.ToLower(strings.TrimSpace(string(v)))
					case "content":
						content = strings.TrimSpace(string(v))
					}
				}
				if key != "" && content != "" {
					if _, seen := out[key]; !seen {
						out[key] = content
					}
				}
			case "body":
				return out // meta chỉ nằm trong <head>
			}
		case html.TextToken:
			if inTitle {
				if _, seen := out["title"]; !seen {
					out["title"] = strings.TrimSpace(string(z.Text()))
				}
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "title" {
				inTitle = false
			}
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
