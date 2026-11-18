package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ref: một ảnh gốc gửi kèm yêu cầu vẽ lại.
type Ref struct {
	Data []byte
	Type string // image/png, image/jpeg, image/webp
}

func (r Ref) dataURI() string {
	return "data:" + r.Type + ";base64," + base64.StdEncoding.EncodeToString(r.Data)
}

// ImageEditor: dịch vụ vẽ lại / sửa ảnh từ ảnh gốc theo lời dặn.
type ImageEditor interface {
	Name() string
	Edit(ctx context.Context, prompt string, refs []Ref, w, h int) ([]byte, error)
}

// CanEdit: có dịch vụ nào nhận ảnh gốc không.
func (im *Images) CanEdit() bool {
	for _, p := range im.Providers {
		if _, ok := p.(ImageEditor); ok {
			return true
		}
	}
	return false
}

// Edit vẽ lại từ ảnh gốc, thử lần lượt các dịch vụ biết sửa ảnh.
func (im *Images) Edit(ctx context.Context, prompt string, refs []Ref, w, h int) ([]byte, string, error) {
	var errs []string
	for _, p := range im.Providers {
		ed, ok := p.(ImageEditor)
		if !ok {
			continue
		}
		b, err := ed.Edit(ctx, prompt, refs, w, h)
		for try := 0; try < 2 && err != nil; try++ {
			d, ok := retryWait(err, 50*time.Second)
			if !ok || !sleepCtx(ctx, d) {
				break
			}
			b, err = ed.Edit(ctx, prompt, refs, w, h)
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
		errs = append(errs, p.Name()+": "+err.Error())
	}
	if len(errs) == 0 {
		return nil, "", errors.New("chưa có dịch vụ nào vẽ lại được từ ảnh gốc")
	}
	return nil, "", errors.New(strings.Join(errs, "; "))
}

/* ---------- Pollinations (cần key): /v1/images/edits dạng OpenAI ---------- */

// PollinationsEditor dùng API mới gen.pollinations.ai (cần key "pollen").
// Model mặc định flux.1-kontext-pro: vẽ lại / đổi phong cách mà vẫn giữ nhân vật trong ảnh gốc.
type PollinationsEditor struct {
	PollinationsImage
	EditModel string
	GenURL    string // mặc định https://gen.pollinations.ai
}

func (p PollinationsEditor) Edit(ctx context.Context, prompt string, refs []Ref, w, h int) ([]byte, error) {
	if p.Key == "" {
		return nil, errors.New("cần POLLINATIONS_API_KEY")
	}
	model, base := p.EditModel, p.GenURL
	if model == "" {
		model = "black-forest-labs/flux.1-kontext-pro"
	}
	if base == "" {
		base = "https://gen.pollinations.ai"
	}
	imgs := make([]map[string]string, 0, len(refs))
	for _, r := range refs {
		imgs = append(imgs, map[string]string{"image_url": r.dataURI()})
	}
	size := "auto"
	if w > 0 && h > 0 && w != h {
		size = fmt.Sprintf("%dx%d", w, h)
	}
	res, err := postJSON(ctx, base+"/v1/images/edits", map[string]string{"Authorization": "Bearer " + p.Key}, map[string]any{
		"model": model, "prompt": prompt, "image": imgs, "size": size, "response_format": "b64_json",
	})
	if err != nil {
		return nil, err
	}
	return readImagesResponse(ctx, res)
}

// readImagesResponse: {"data":[{"b64_json":...}|{"url":...}]} dạng OpenAI.
func readImagesResponse(ctx context.Context, res *http.Response) ([]byte, error) {
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

/* ---------- OpenRouter: model ảnh nhận ảnh đầu vào (trả phí) ---------- */

func (o OpenRouterImage) Edit(ctx context.Context, prompt string, refs []Ref, w, h int) ([]byte, error) {
	parts := []map[string]any{{"type": "text", "text": "Edit or redraw the attached image(s): " + prompt}}
	for _, r := range refs {
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": r.dataURI()}})
	}
	return o.request(ctx, parts)
}
