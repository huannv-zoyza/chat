package controller

import (
	"regexp"
	"strings"
)

// @username (3–32 ký tự a-z 0-9 _ .) hoặc @all, đứng đầu chuỗi hoặc sau ký tự không phải chữ/số
// (để "email@cong.ty" không bị coi là nhắc tên).
var mentionRe = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}_.@])@([a-z0-9_.]{3,32})`)

// parseMentions trả về các username được nhắc (đã chữ thường, không trùng) và có @all hay không.
func parseMentions(content string) (usernames []string, all bool) {
	seen := map[string]bool{}
	add := func(u string) {
		if len(u) >= 3 && !seen[u] {
			seen[u] = true
			usernames = append(usernames, u)
		}
	}
	for _, m := range mentionRe.FindAllStringSubmatch(content, -1) {
		name := strings.ToLower(m[1])
		// "@minh." ở cuối câu: thử cả "minh." lẫn "minh" (username được phép chứa dấu chấm).
		trimmed := strings.TrimRight(name, ".")
		if trimmed == "all" {
			all = true
			continue
		}
		add(name)
		add(trimmed)
	}
	return usernames, all
}
