package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SearchResult: một kết quả tìm kiếm web.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
	// Content: nội dung trang (nếu dịch vụ trả về sẵn, hoặc đã tải trang).
	Content string
}

type Searcher interface {
	Name() string
	Search(ctx context.Context, q string, n int) ([]SearchResult, error)
}

// Search: chuỗi dịch vụ tìm kiếm, bên trước lỗi / không có kết quả thì thử bên sau.
type Search struct {
	Providers []Searcher
}

func (s *Search) Names() []string {
	var out []string
	for _, p := range s.Providers {
		out = append(out, p.Name())
	}
	return out
}

func (s *Search) Search(ctx context.Context, q string, n int) ([]SearchResult, string, error) {
	var errs []string
	for _, p := range s.Providers {
		res, err := p.Search(ctx, q, n)
		if err == nil && len(res) > 0 {
			return res, p.Name(), nil
		}
		if err == nil {
			err = errors.New("không có kết quả")
		}
		errs = append(errs, p.Name()+": "+err.Error())
	}
	return nil, "", errors.New(strings.Join(errs, "; "))
}

var searchHTTP = &http.Client{Timeout: 20 * time.Second}

/* ---------- DuckDuckGo (bản HTML, không cần key) ---------- */

type DuckDuckGo struct{ BaseURL string }

func (DuckDuckGo) Name() string { return "duckduckgo" }

var (
	ddgLink    = regexp.MustCompile(`(?s)class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippet = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</a>`)
	tagRe      = regexp.MustCompile(`(?s)<[^>]+>`)
)

func stripTags(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, " "))), " ")
}

func (d DuckDuckGo) Search(ctx context.Context, q string, n int) ([]SearchResult, error) {
	base := d.BaseURL
	if base == "" {
		base = "https://html.duckduckgo.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/html/", strings.NewReader(url.Values{"q": {q}, "kl": {"vn-vi"}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) GoChat/1.0")
	res, err := searchHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	links := ddgLink.FindAllStringSubmatch(string(b), -1)
	snips := ddgSnippet.FindAllStringSubmatch(string(b), -1)
	var out []SearchResult
	for i, l := range links {
		u := html.UnescapeString(l[1])
		// link quảng cáo / chuyển hướng của DuckDuckGo: lấy địa chỉ thật trong uddg=
		if pu, err := url.Parse(u); err == nil && strings.Contains(pu.Host+pu.Path, "duckduckgo.com/l") {
			if real := pu.Query().Get("uddg"); real != "" {
				u = real
			}
		}
		if strings.Contains(u, "duckduckgo.com/y.js") || !strings.HasPrefix(u, "http") {
			continue // quảng cáo
		}
		r := SearchResult{Title: stripTags(l[2]), URL: u}
		if i < len(snips) {
			r.Snippet = stripTags(snips[i][1])
		}
		out = append(out, r)
		if len(out) == n {
			break
		}
	}
	return out, nil
}

/* ---------- Tavily (key miễn phí 1000 lần/tháng, trả sẵn nội dung) ---------- */

type Tavily struct{ Key, BaseURL string }

func (Tavily) Name() string { return "tavily" }

func (t Tavily) Search(ctx context.Context, q string, n int) ([]SearchResult, error) {
	base := t.BaseURL
	if base == "" {
		base = "https://api.tavily.com"
	}
	res, err := postJSON(ctx, base+"/search", map[string]string{"Authorization": "Bearer " + t.Key}, map[string]any{
		"query": q, "max_results": n, "search_depth": "basic", "include_answer": false,
	})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readError(res)
	}
	var out struct {
		Results []struct {
			Title, URL, Content string
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	var rs []SearchResult
	for _, r := range out.Results {
		rs = append(rs, SearchResult{Title: r.Title, URL: r.URL, Snippet: shortText(r.Content, 300), Content: r.Content})
	}
	return rs, nil
}

/* ---------- Brave Search (key miễn phí ~2000 lần/tháng) ---------- */

type Brave struct{ Key, BaseURL string }

func (Brave) Name() string { return "brave" }

func (b Brave) Search(ctx context.Context, q string, n int) ([]SearchResult, error) {
	base := b.BaseURL
	if base == "" {
		base = "https://api.search.brave.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/res/v1/web/search?"+url.Values{"q": {q}, "count": {fmt.Sprint(n)}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Subscription-Token", b.Key)
	req.Header.Set("Accept", "application/json")
	res, err := searchHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readError(res)
	}
	var out struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	var rs []SearchResult
	for _, r := range out.Web.Results {
		rs = append(rs, SearchResult{Title: stripTags(r.Title), URL: r.URL, Snippet: stripTags(r.Description)})
	}
	return rs, nil
}

/* ---------- Đọc nội dung chữ của một trang ---------- */

var (
	dropBlocks = regexp.MustCompile(`(?is)<(script|style|noscript|svg|nav|header|footer|form|iframe)\b.*?</(script|style|noscript|svg|nav|header|footer|form|iframe)>`)
	textBlocks = regexp.MustCompile(`(?is)<(p|h[1-4]|li|blockquote|pre|figcaption|tr)\b[^>]*>(.*?)</(p|h[1-4]|li|blockquote|pre|figcaption|tr)>`)
	cellRe     = regexp.MustCompile(`(?is)<t[dh]\b[^>]*>(.*?)</t[dh]>`)
	digitRe    = regexp.MustCompile(`\d`)
	titleRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// PageText tải trang (client do bên gọi đưa vào, nên dùng client chặn địa chỉ nội bộ) và lấy
// phần chữ chính: đoạn văn, tiêu đề, danh sách, bảng. Tối đa limit ký tự.
func PageText(ctx context.Context, client *http.Client, u string, limit int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) GoChat/1.0")
	req.Header.Set("Accept-Language", "vi,en;q=0.8")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "html") && !strings.HasPrefix(ct, "text/") {
		return "", fmt.Errorf("không phải trang web (%s)", ct)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 3<<20))
	if err != nil {
		return "", err
	}
	page := dropBlocks.ReplaceAllString(string(b), " ")
	var parts []string
	seen := map[string]bool{}
	total := 0
	for _, m := range textBlocks.FindAllStringSubmatch(page, -1) {
		var t string
		if strings.EqualFold(m[1], "tr") {
			// hàng bảng (giá, tỷ giá, lịch…): ghép các ô, giữ cả ô ngắn như con số
			var cells []string
			for _, c := range cellRe.FindAllStringSubmatch(m[2], -1) {
				if ct := stripTags(c[1]); ct != "" {
					cells = append(cells, ct)
				}
			}
			t = strings.Join(cells, " | ")
			ok := len(cells) >= 2 && digitRe.MatchString(t) || len(cells) == 1 && len([]rune(t)) >= 25
			if !ok || seen[t] {
				continue
			}
		} else if t = stripTags(m[2]); len([]rune(t)) < 25 || seen[t] {
			continue
		}
		seen[t] = true
		parts = append(parts, t)
		total += len(t)
		if total > limit*2 {
			break
		}
	}
	text := strings.Join(parts, "\n")
	if text == "" {
		if m := titleRe.FindStringSubmatch(string(b)); m != nil {
			text = stripTags(m[1])
		}
	}
	return shortText(text, limit), nil
}

func shortText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// SearchFromEnv: Tavily / Brave nếu có key, DuckDuckGo (không key) làm dự phòng.
func SearchFromEnv(get func(string) string) *Search {
	if on(get("AI_DISABLE")) || on(get("AI_NO_WEB")) {
		return nil
	}
	var ps []Searcher
	if k := get("TAVILY_API_KEY"); k != "" {
		ps = append(ps, Tavily{Key: k})
	}
	if k := get("BRAVE_API_KEY"); k != "" {
		ps = append(ps, Brave{Key: k})
	}
	ps = append(ps, DuckDuckGo{})
	return &Search{Providers: ps}
}
