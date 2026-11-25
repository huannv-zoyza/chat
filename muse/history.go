package muse

// Tách /chat/history thành các lượt. Cổng từ normaliseHistory trong
// desktop/src/shared/gateway/events.ts — kể cả quy tắc "commentary" (lời dẫn) không tính là câu trả lời.

import (
	"encoding/json"
	"regexp"
	"strings"
)

const textMaxFromPage = 8 << 20

var attachmentMarker = regexp.MustCompile(`^\[[a-z][a-z_-]{0,20}:[^\]\n]{1,1000}\]$`)

// StripAttachmentMarkers bỏ các dòng đánh dấu tệp đính kèm Muse chèn trước nội dung người dùng.
func StripAttachmentMarkers(text string) string {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines)-1 && attachmentMarker.MatchString(lines[i]) {
		i++
	}
	if i == 0 {
		return text
	}
	return strings.Join(lines[i:], "\n")
}

var rawPathRe = regexp.MustCompile(`/(?:media|fs)/raw/(.+)$`)

// WorkspacePath chuẩn hoá đường dẫn tệp của Muse về dạng /workspace/...
func WorkspacePath(p string) string {
	x := p
	if i := strings.LastIndex(x, "sandbox://"); i >= 0 {
		x = x[i+len("sandbox://"):]
	}
	if m := rawPathRe.FindStringSubmatch(x); m != nil {
		x = m[1]
	}
	if i := strings.IndexByte(x, '?'); i >= 0 {
		x = x[:i]
	}
	if !strings.HasPrefix(x, "/") {
		x = "/" + x
	}
	return x
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func obj(m map[string]any, k string) map[string]any {
	if v, ok := m[k].(map[string]any); ok {
		return v
	}
	return nil
}

func fileOf(v map[string]any) *File {
	p := str(v, "path")
	if p == "" {
		return nil
	}
	if !(strings.Contains(p, "/workspace") || strings.HasPrefix(p, "sandbox://") || strings.Contains(p, "/raw/")) {
		return nil
	}
	f := &File{Path: WorkspacePath(p)}
	for _, k := range []string{"mime", "mime_type"} {
		if s := str(v, k); s != "" {
			f.Mime = s
			break
		}
	}
	for _, k := range []string{"name", "filename", "file_name"} {
		if s := str(v, k); s != "" {
			f.Name = s
			break
		}
	}
	for _, k := range []string{"byte_len", "byteLen", "size"} {
		if n, ok := v[k].(float64); ok {
			f.Size = int64(n)
			break
		}
	}
	return f
}

// findFiles quét mọi tham chiếu tệp trong một giá trị, theo thứ tự xuất hiện, không trùng lặp.
func findFiles(v any, out *[]File, seen map[string]bool, depth int) {
	if depth > 10 || v == nil {
		return
	}
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			findFiles(x, out, seen, depth+1)
		}
	case map[string]any:
		if f := fileOf(t); f != nil && !seen[f.Path] {
			seen[f.Path] = true
			*out = append(*out, *f)
		}
		for _, x := range t {
			switch x.(type) {
			case map[string]any, []any:
				findFiles(x, out, seen, depth+1)
			}
		}
	}
}

// parseHistory đọc {result:{chat_events:[…]}} thành danh sách lượt.
func parseHistory(body []byte) ([]*Turn, error) {
	var data struct {
		Result struct {
			ChatEvents []map[string]any `json:"chat_events"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	var turns []*Turn
	var cur *Turn
	seen := map[string]bool{}
	commentary := map[*Turn]string{}
	answered := map[*Turn]bool{}

	for _, raw := range data.Result.ChatEvents {
		name := str(raw, "event_name")
		if name == "" {
			name = str(raw, "event")
		}
		if name == "message.user" {
			text := str(raw, "display_text")
			cur = &Turn{UserText: StripAttachmentMarkers(text)}
			turns = append(turns, cur)
			seen = map[string]bool{}
			findFiles(raw, &cur.Files, seen, 0)
			continue
		}
		if cur == nil {
			continue
		}
		switch name {
		case "message.assistant":
			text := str(raw, "display_text")
			if len(text) > textMaxFromPage {
				text = text[:textMaxFromPage]
			}
			ready, _ := raw["display_text_ready"].(bool)
			payload := obj(raw, "payload")
			meta := obj(payload, "metadata")
			if meta == nil {
				meta = obj(raw, "metadata")
			}
			status := str(payload, "status")
			if status == "" {
				status = str(raw, "status")
			}
			if str(meta, "message_kind") == "commentary" {
				// Lời dẫn ("để tôi viết…"): chỉ hiện khi lượt chưa có câu trả lời thật.
				c := commentary[cur]
				if text != "" {
					if c != "" {
						c += "\n\n" + text
					} else {
						c = text
					}
					commentary[cur] = c
				}
				if !answered[cur] {
					cur.Text, cur.Ready, cur.Final = c, false, false
				}
			} else {
				prev := ""
				if answered[cur] {
					prev = cur.Text
				}
				answered[cur] = true
				if text != "" {
					if prev != "" {
						cur.Text = prev + "\n\n" + text
					} else {
						cur.Text = text
					}
				} else {
					cur.Text = prev
				}
				cur.Ready = ready
				cur.Final = ready && status != "" && terminalStatus[status]
				cur.Citations = appendCitations(cur.Citations, raw["resources"])
			}
		case "task.status":
			st := str(obj(raw, "payload"), "status")
			if st == "" {
				st = str(raw, "status")
			}
			if st != "" {
				cur.Status = st
			}
		}
		findFiles(raw, &cur.Files, seen, 0)
	}
	return turns, nil
}

// appendCitations thêm các nguồn web (resources kind "citation") vào list, bỏ link trùng.
func appendCitations(list []Citation, resources any) []Citation {
	items, _ := resources.([]any)
	for _, it := range items {
		r, _ := it.(map[string]any)
		if r == nil || str(r, "kind") != "citation" {
			continue
		}
		uri := str(r, "uri")
		if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
			continue
		}
		dup := false
		for _, c := range list {
			if c.URI == uri {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, Citation{Label: str(r, "label"), URI: uri})
		}
	}
	return list
}
