package muse

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxSources: số link nguồn tối đa nối vào câu trả lời (Muse hay trích cả chục trang).
const maxSources = 4

// sourcesHeadRe: câu trả lời đã kết thúc bằng tiêu đề "Nguồn:" (có thể in đậm) mà không có link.
var sourcesHeadRe = regexp.MustCompile(`(?i)(\*\*|__)?(nguồn|sources?)(\s+tham khảo)?\s*:?\s*(\*\*|__)?\s*:?\s*$`)

// SourcesSuffix trả về đoạn chữ cần NỐI THÊM vào cuối text để hiện link nguồn (rỗng nếu không có
// nguồn hoặc text đã có sẵn các link đó). Chỉ thêm chứ không sửa text, để dùng được cả khi stream.
func SourcesSuffix(text string, cites []Citation) string {
	var links []string
	for _, c := range cites {
		if len(links) == maxSources {
			break
		}
		if strings.Contains(text, c.URI) {
			continue
		}
		links = append(links, "["+citationLabel(c)+"]("+strings.NewReplacer("(", "%28", ")", "%29").Replace(c.URI)+")")
	}
	if len(links) == 0 {
		return ""
	}
	list := strings.Join(links, " · ")
	if sourcesHeadRe.MatchString(strings.TrimRight(text, " \n")) {
		return " " + list
	}
	return "\n\nNguồn: " + list
}

// citationLabel: tiêu đề ngắn gọn, không chứa ký tự phá cú pháp link markdown.
func citationLabel(c Citation) string {
	l := strings.Join(strings.Fields(strings.NewReplacer("[", "(", "]", ")").Replace(c.Label)), " ")
	if l == "" {
		l = c.URI
		if i := strings.Index(l, "://"); i >= 0 {
			l = l[i+3:]
		}
		if i := strings.IndexByte(l, '/'); i >= 0 {
			l = l[:i]
		}
	}
	if utf8.RuneCountInString(l) > 60 {
		r := []rune(l)
		l = string(r[:57]) + "…"
	}
	return l
}
