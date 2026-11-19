package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const maxImageBytes = 15 << 20

// ImageProvider: một dịch vụ tạo ảnh từ mô tả. w, h: kích thước mong muốn (có bên bỏ qua).
type ImageProvider interface {
	Name() string
	Generate(ctx context.Context, prompt string, w, h int) ([]byte, error)
}

// Images: chuỗi dịch vụ tạo ảnh, bên trước lỗi thì thử bên sau.
type Images struct {
	Providers []ImageProvider
}

func (im *Images) Names() []string {
	var out []string
	for _, p := range im.Providers {
		out = append(out, p.Name())
	}
	return out
}

// Generate trả về ảnh và tên dịch vụ đã vẽ.
func (im *Images) Generate(ctx context.Context, prompt string, w, h int) ([]byte, string, error) {
	var errs chainError
	for _, p := range im.Providers {
		b, err := p.Generate(ctx, prompt, w, h)
		// dịch vụ miễn phí hay báo "đang quá tải / 1 ảnh mỗi 45 giây, thử lại sau N giây": thử thêm tối đa 2 lần
		for try := 0; try < 2; try++ {
			d, ok := retryWait(err, 50*time.Second)
			if !ok || !sleepCtx(ctx, d) {
				break
			}
			b, err = p.Generate(ctx, prompt, w, h)
		}
		if err == nil && len(b) > 0 {
			return b, p.Name(), nil
		}
		if err == nil {
			err = errors.New("ảnh rỗng")
		}
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
	}
	if len(errs) == 0 {
		return nil, "", errors.New("chưa cấu hình dịch vụ tạo ảnh nào")
	}
	return nil, "", errs
}

// chainError: lỗi của từng dịch vụ trong chuỗi, giữ nguyên kiểu để errors.As tìm được.
type chainError []error

func (e chainError) Error() string {
	s := make([]string, len(e))
	for i, err := range e {
		s[i] = err.Error()
	}
	return strings.Join(s, "; ")
}

func (e chainError) Unwrap() []error { return e }

// OutOfCredit: dịch vụ báo tài khoản hết tiền (HTTP 402 không kèm "thử lại sau N giây").
func OutOfCredit(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.Status == http.StatusPaymentRequired && he.RetryAfter == 0
}

var imageHTTP = &http.Client{Timeout: 2 * time.Minute}

func readImage(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readError(res)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxImageBytes {
		return nil, errors.New("ảnh quá lớn")
	}
	if ct := http.DetectContentType(b); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("không phải ảnh (%s)", ct)
	}
	return b, nil
}

func decodeB64(s string) ([]byte, error) {
	if i := strings.Index(s, ","); strings.HasPrefix(s, "data:") && i > 0 {
		s = s[i+1:]
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) > maxImageBytes {
		return nil, errors.New("ảnh quá lớn")
	}
	if ct := http.DetectContentType(b); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("không phải ảnh (%s)", ct)
	}
	return b, nil
}

func postJSON(ctx context.Context, u string, headers map[string]string, body any) (*http.Response, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return imageHTTP.Do(req)
}

/* ---------- Pollinations: miễn phí, không cần key ---------- */

type PollinationsImage struct {
	BaseURL string // mặc định https://image.pollinations.ai
	Model   string // rỗng: model mặc định của gói miễn phí
	// Key "pollen" (miễn phí tại enter.pollinations.ai): bỏ giới hạn 1 ảnh/45 giây, dùng được model tốt hơn.
	Key string
}

func (p PollinationsImage) Name() string { return "pollinations" }

func (p PollinationsImage) Generate(ctx context.Context, prompt string, w, h int) ([]byte, error) {
	base := p.BaseURL
	if base == "" {
		base = "https://image.pollinations.ai"
	}
	q := url.Values{}
	q.Set("width", fmt.Sprint(w))
	q.Set("height", fmt.Sprint(h))
	q.Set("nologo", "true")
	q.Set("seed", fmt.Sprint(rand.IntN(1_000_000)))
	if p.Model != "" {
		q.Set("model", p.Model)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/prompt/"+url.PathEscape(prompt)+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if p.Key != "" {
		req.Header.Set("Authorization", "Bearer "+p.Key)
	}
	res, err := imageHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	return readImage(res)
}

/* ---------- Together AI: FLUX.1-schnell bản miễn phí ---------- */

type TogetherImage struct {
	Key, Model, BaseURL string
}

func (t TogetherImage) Name() string { return "together" }

func (t TogetherImage) Generate(ctx context.Context, prompt string, w, h int) ([]byte, error) {
	model, base := t.Model, t.BaseURL
	if model == "" {
		model = "black-forest-labs/FLUX.1-schnell-Free"
	}
	if base == "" {
		base = "https://api.together.xyz/v1"
	}
	res, err := postJSON(ctx, base+"/images/generations", map[string]string{"Authorization": "Bearer " + t.Key}, map[string]any{
		"model": model, "prompt": prompt, "width": w, "height": h, "steps": 4, "n": 1, "response_format": "b64_json",
	})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readError(res)
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxImageBytes*2)).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, errors.New("không có ảnh trả về")
	}
	if out.Data[0].B64 != "" {
		return decodeB64(out.Data[0].B64)
	}
	return fetchImage(ctx, out.Data[0].URL)
}

func fetchImage(ctx context.Context, u string) ([]byte, error) {
	if !strings.HasPrefix(u, "https://") {
		return nil, errors.New("không có ảnh trả về")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := imageHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	return readImage(res)
}

/* ---------- Cloudflare Workers AI: gói miễn phí theo ngày ---------- */

// Mặc định FLUX.2 dev: đẹp hơn hẳn schnell, nhận kích thước và vẽ lại được từ ảnh gốc.
const cfDefaultImageModel = "@cf/black-forest-labs/flux-2-dev"

type CloudflareImage struct {
	AccountID, Token, Model, BaseURL string
}

func (c CloudflareImage) Name() string { return "cloudflare" }

func (c CloudflareImage) model() string {
	if c.Model == "" {
		return cfDefaultImageModel
	}
	return c.Model
}

// flux2: họ FLUX.2 trên Workers AI nhận multipart (prompt, width, height, input_image_N).
func (c CloudflareImage) flux2() bool {
	return strings.HasPrefix(c.model(), "@cf/black-forest-labs/flux-2")
}

func (c CloudflareImage) url() string {
	base := c.BaseURL
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	return base + "/accounts/" + url.PathEscape(c.AccountID) + "/ai/run/" + c.model()
}

func (c CloudflareImage) Generate(ctx context.Context, prompt string, w, h int) ([]byte, error) {
	if c.flux2() {
		return c.runMultipart(ctx, prompt, nil, w, h)
	}
	res, err := postJSON(ctx, c.url(), map[string]string{"Authorization": "Bearer " + c.Token}, map[string]any{"prompt": prompt, "steps": 8})
	if err != nil {
		return nil, err
	}
	return readCloudflare(res)
}

// cfMaxRefs: FLUX.2 nhận tối đa 4 ảnh gốc.
const cfMaxRefs = 4

func (c CloudflareImage) runMultipart(ctx context.Context, prompt string, refs []Ref, w, h int) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("prompt", prompt)
	if w > 0 && h > 0 {
		_ = mw.WriteField("width", fmt.Sprint(w))
		_ = mw.WriteField("height", fmt.Sprint(h))
	}
	for i, r := range refs {
		if i == cfMaxRefs {
			break
		}
		hd := textproto.MIMEHeader{}
		hd.Set("Content-Disposition", fmt.Sprintf(`form-data; name="input_image_%d"; filename="ref%d"`, i, i))
		hd.Set("Content-Type", r.Type)
		part, err := mw.CreatePart(hd)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(r.Data); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(), &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := imageHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	return readCloudflare(res)
}

func readCloudflare(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); strings.HasPrefix(ct, "image/") {
		return readImage(res)
	}
	var out struct {
		Success bool `json:"success"`
		Result  struct {
			Image string `json:"image"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxImageBytes*2)).Decode(&out); err != nil {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if !out.Success || out.Result.Image == "" {
		if len(out.Errors) > 0 {
			return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, out.Errors[0].Message)
		}
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return decodeB64(out.Result.Image)
}

// CloudflareEditor: model FLUX.2 vẽ lại từ ảnh gốc (giữ nhân vật, đổi phong cách / thêm bớt chi tiết).
type CloudflareEditor struct {
	CloudflareImage
}

func (c CloudflareEditor) Edit(ctx context.Context, prompt string, refs []Ref, w, h int) ([]byte, error) {
	return c.runMultipart(ctx, prompt, refs, w, h)
}

/* ---------- OpenRouter: model ảnh (trả phí), ảnh nằm trong message.images ---------- */

type OpenRouterImage struct {
	Key, Model, BaseURL string
}

func (o OpenRouterImage) Name() string { return "openrouter" }

func (o OpenRouterImage) Generate(ctx context.Context, prompt string, w, h int) ([]byte, error) {
	shape := "square"
	if w > h {
		shape = "landscape (wide)"
	} else if h > w {
		shape = "portrait (tall)"
	}
	return o.request(ctx, "Generate one "+shape+" image: "+prompt)
}

// request gửi một tin (chữ, hoặc mảng chữ + ảnh) tới model ảnh và lấy ảnh trong message.images.
func (o OpenRouterImage) request(ctx context.Context, content any) ([]byte, error) {
	base := o.BaseURL
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	res, err := postJSON(ctx, base+"/chat/completions", map[string]string{"Authorization": "Bearer " + o.Key, "X-Title": "Go Chat"}, map[string]any{
		"model":      o.Model,
		"modalities": []string{"image", "text"},
		"messages":   []map[string]any{{"role": "user", "content": content}},
	})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readError(res)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Images []struct {
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxImageBytes*2)).Decode(&out); err != nil {
		return nil, err
	}
	for _, c := range out.Choices {
		for _, im := range c.Message.Images {
			if strings.HasPrefix(im.ImageURL.URL, "data:") {
				return decodeB64(im.ImageURL.URL)
			}
			return fetchImage(ctx, im.ImageURL.URL)
		}
	}
	return nil, errors.New("model không trả về ảnh")
}
