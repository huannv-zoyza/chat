package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"chat/model"
)

/* ---------- Kiểm tra sticker / GIF client gửi lên ---------- */

// Sticker: "<bộ>/<tên>", ví dụ "text/oke". Danh sách sticker nằm ở app; server chỉ kiểm tra định dạng.
var stickerIDRe = regexp.MustCompile(`^[a-z0-9]{1,20}/[a-z0-9-]{1,40}$`)
var gifIDRe = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

// GIF chỉ được trỏ tới máy chủ ảnh của GIPHY (không cho nhúng ảnh tuỳ ý để theo dõi người xem).
var giphyMediaHostRe = regexp.MustCompile(`^(media[0-9]*|i)\.giphy\.com$`)

func validGiphyURL(raw string) bool {
	if raw == "" || len(raw) > 1000 {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && giphyMediaHostRe.MatchString(u.Hostname()) && u.Port() == ""
}

// validateEmbed trả về bản đã làm sạch, hoặc câu báo lỗi.
func validateEmbed(e *model.Embed) (*model.Embed, string) {
	switch e.Type {
	case "sticker":
		if !stickerIDRe.MatchString(e.ID) {
			return nil, "Sticker không hợp lệ."
		}
		return &model.Embed{Type: "sticker", ID: e.ID}, ""
	case "gif":
		if !gifIDRe.MatchString(e.ID) || !validGiphyURL(e.URL) || (e.Preview != "" && !validGiphyURL(e.Preview)) ||
			e.Width < 1 || e.Width > 2000 || e.Height < 1 || e.Height > 2000 {
			return nil, "GIF không hợp lệ."
		}
		title := strings.TrimSpace(e.Title)
		if utf8.RuneCountInString(title) > 200 {
			title = string([]rune(title)[:200])
		}
		return &model.Embed{Type: "gif", ID: e.ID, URL: e.URL, Preview: e.Preview, Width: e.Width, Height: e.Height, Title: title}, ""
	}
	return nil, "Loại nội dung không hỗ trợ."
}

/* ---------- GET /api/gifs?q=&offset=: tìm GIF qua GIPHY ---------- */

// GifResult: một GIF trong kết quả tìm kiếm (đúng dạng Embed để client gửi lại nguyên văn).
type GifResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Preview string `json:"preview"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

type gifsResponse struct {
	// false: server chưa có GIPHY API key, client hiện hướng dẫn thay vì kết quả.
	Enabled    bool        `json:"enabled"`
	Results    []GifResult `json:"results"`
	NextOffset int         `json:"next_offset,omitempty"`
}

const (
	gifPageSize = 24
	gifCacheTTL = 10 * time.Minute
	gifCacheMax = 500
)

type gifCache struct {
	mu sync.Mutex
	m  map[string]gifCacheEntry
}

type gifCacheEntry struct {
	res     gifsResponse
	expires time.Time
}

func (c *gifCache) get(k string) (gifsResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || time.Now().After(e.expires) {
		return gifsResponse{}, false
	}
	return e.res, true
}

func (c *gifCache) put(k string, r gifsResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]gifCacheEntry{}
	}
	if len(c.m) >= gifCacheMax {
		for key := range c.m {
			delete(c.m, key)
			break
		}
	}
	c.m[k] = gifCacheEntry{res: r, expires: time.Now().Add(gifCacheTTL)}
}

// SetGiphyKey bật tìm GIF (key lấy miễn phí tại developers.giphy.com). Rỗng = tắt.
func (a *API) SetGiphyKey(key string) {
	a.giphyKey = strings.TrimSpace(key)
	a.giphyBase = "https://api.giphy.com"
	a.giphyClient = &http.Client{Timeout: 8 * time.Second}
}

func (a *API) searchGifs(w http.ResponseWriter, r *http.Request) {
	if a.giphyKey == "" {
		writeJSON(w, http.StatusOK, gifsResponse{Enabled: false, Results: []GifResult{}})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 50 {
		q = string([]rune(q)[:50])
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 || offset > 4999 {
		offset = 0
	}
	key := strings.ToLower(q) + "|" + strconv.Itoa(offset)
	if res, ok := a.gifs.get(key); ok {
		writeJSON(w, http.StatusOK, res)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	res, err := a.fetchGifs(ctx, q, offset)
	if err != nil {
		log.Printf("giphy: %v", err)
		writeError(w, http.StatusBadGateway, "Không tải được GIF lúc này, thử lại sau.")
		return
	}
	a.gifs.put(key, res)
	writeJSON(w, http.StatusOK, res)
}

type giphyImage struct {
	URL    string `json:"url"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

type giphyResponse struct {
	Data []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Images struct {
			FixedHeight      giphyImage `json:"fixed_height"`
			FixedHeightSmall giphyImage `json:"fixed_height_small"`
		} `json:"images"`
	} `json:"data"`
	Pagination struct {
		TotalCount int `json:"total_count"`
		Count      int `json:"count"`
		Offset     int `json:"offset"`
	} `json:"pagination"`
}

func (a *API) fetchGifs(ctx context.Context, q string, offset int) (gifsResponse, error) {
	v := url.Values{}
	v.Set("api_key", a.giphyKey)
	v.Set("limit", strconv.Itoa(gifPageSize))
	v.Set("offset", strconv.Itoa(offset))
	v.Set("rating", "pg-13")
	endpoint := "/v1/gifs/trending"
	if q != "" {
		endpoint = "/v1/gifs/search"
		v.Set("q", q)
		v.Set("lang", "vi")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.giphyBase+endpoint+"?"+v.Encode(), nil)
	if err != nil {
		return gifsResponse{}, err
	}
	res, err := a.giphyClient.Do(req)
	if err != nil {
		return gifsResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return gifsResponse{}, errors.New("giphy trả " + res.Status)
	}
	var g giphyResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&g); err != nil {
		return gifsResponse{}, err
	}
	out := gifsResponse{Enabled: true, Results: []GifResult{}}
	for _, d := range g.Data {
		img := d.Images.FixedHeight
		wd, _ := strconv.Atoi(img.Width)
		ht, _ := strconv.Atoi(img.Height)
		if !gifIDRe.MatchString(d.ID) || !validGiphyURL(img.URL) || wd <= 0 || ht <= 0 {
			continue
		}
		preview := d.Images.FixedHeightSmall.URL
		if !validGiphyURL(preview) {
			preview = img.URL
		}
		out.Results = append(out.Results, GifResult{
			ID: d.ID, Title: d.Title, URL: img.URL, Preview: preview, Width: wd, Height: ht,
		})
	}
	if next := g.Pagination.Offset + g.Pagination.Count; g.Pagination.Count > 0 && next < g.Pagination.TotalCount && next < 5000 {
		out.NextOffset = next
	}
	return out, nil
}
