package muse

// Sinh ảnh qua Muse. Tách riêng khỏi openai.go để vừa phục vụ /v1/images/generations
// vừa cắm thẳng vào chuỗi vẽ ảnh của package ai (interface khớp theo cấu trúc, không cần import chéo).

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// Image bảo Muse vẽ rồi tải tấm ảnh đầu tiên về. Trả về byte ảnh, kiểu MIME và id hội thoại.
// w/h bằng 0 nghĩa là không gợi ý kích thước.
func (c *Client) Image(ctx context.Context, prompt string, w, h int) ([]byte, string, string, error) {
	p := "Vẽ ảnh: " + prompt
	if w > 0 && h > 0 {
		p += fmt.Sprintf("\n(kích thước mong muốn: %dx%d)", w, h)
	}
	ans, err := c.Ask(ctx, []map[string]any{TextItem(p)}, AskOptions{})
	if err != nil {
		return nil, "", "", err
	}
	for _, f := range ans.Files {
		if !looksLikeImage(f) {
			continue
		}
		ctype, data, err := c.FetchFile(ctx, f.Path, 32<<20)
		if err != nil {
			return nil, "", ans.SessionID, fmt.Errorf("tải ảnh về hỏng: %w", err)
		}
		if ctype == "" {
			ctype = f.Mime
		}
		if ctype == "" {
			ctype = "image/png"
		}
		return data, ctype, ans.SessionID, nil
	}
	note := strings.TrimSpace(ans.Text)
	if note == "" {
		note = "không có nội dung trả về"
	}
	return nil, "", ans.SessionID, fmt.Errorf("Muse không trả về ảnh nào (trả lời: %s)", trim(note, 200))
}

// ImageProvider cắm Muse vào chuỗi vẽ ảnh của package ai.
// Thoả interface ai.ImageProvider: Name() string + Generate(ctx, prompt, w, h) ([]byte, error).
type ImageProvider struct{ C *Client }

func NewImageProvider(c *Client) *ImageProvider { return &ImageProvider{C: c} }

func (p *ImageProvider) Name() string { return "muse" }

func (p *ImageProvider) Generate(ctx context.Context, prompt string, w, h int) ([]byte, error) {
	data, _, _, err := p.C.Image(ctx, prompt, w, h)
	return data, err
}

// ImageRef là một ảnh gốc gửi kèm để Muse vẽ lại / sửa.
type ImageRef struct {
	Data []byte
	Mime string // image/png, image/jpeg, image/webp
}

// EditImage gửi ảnh gốc kèm lời dặn rồi lấy ảnh Muse vẽ lại.
// Đây là chế độ image.reference mà bản desktop gọi là "vẽ từ ảnh tham chiếu".
func (c *Client) EditImage(ctx context.Context, prompt string, refs []ImageRef, w, h int) ([]byte, string, string, error) {
	if len(refs) == 0 {
		return c.Image(ctx, prompt, w, h)
	}
	items := make([]map[string]any, 0, len(refs)+1)
	for i, r := range refs {
		mime := r.Mime
		if mime == "" {
			mime = "image/png"
		}
		items = append(items, ImageItem(mime, base64.StdEncoding.EncodeToString(r.Data), fmt.Sprintf("anh-goc-%d%s", i+1, extFor(mime))))
	}
	p := prompt
	if w > 0 && h > 0 {
		p += fmt.Sprintf("\n(kích thước mong muốn: %dx%d)", w, h)
	}
	// Ảnh đứng trước, chữ đứng sau — giống thứ tự bản desktop gửi (httpRunner.items).
	items = append(items, TextItem(p))

	ans, err := c.Ask(ctx, items, AskOptions{})
	if err != nil {
		return nil, "", "", err
	}
	for _, f := range ans.Files {
		if !looksLikeImage(f) {
			continue
		}
		ctype, data, err := c.FetchFile(ctx, f.Path, 32<<20)
		if err != nil {
			return nil, "", ans.SessionID, fmt.Errorf("tải ảnh về hỏng: %w", err)
		}
		if ctype == "" {
			ctype = f.Mime
		}
		if ctype == "" {
			ctype = "image/png"
		}
		return data, ctype, ans.SessionID, nil
	}
	note := strings.TrimSpace(ans.Text)
	if note == "" {
		note = "không có nội dung trả về"
	}
	return nil, "", ans.SessionID, fmt.Errorf("Muse không vẽ lại ảnh nào (trả lời: %s)", trim(note, 200))
}

func extFor(mime string) string {
	switch strings.ToLower(mime) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}
