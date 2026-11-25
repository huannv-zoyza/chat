package controller

import (
	"strings"
	"testing"
)

// Định tuyến ý định vẽ: "gen ảnh" và link ảnh đứng đầu đều phải nhận ra,
// nhưng câu hỏi *về* việc vẽ thì không được biến thành lệnh vẽ.
func TestDrawIntentRouting(t *testing.T) {
	route := func(text string) (bool, string) {
		intent := aiLeadURLRe.ReplaceAllString(text, "")
		if m := aiDrawRe.FindStringSubmatch(intent); m != nil {
			return true, strings.TrimSpace(m[1])
		}
		return false, ""
	}
	nhan := []struct{ in, prompt string }{
		{"gen ảnh con mèo", "con mèo"},
		{"gen con mèo", "con mèo"},
		{"gen hình con mèo", "con mèo"},
		{"vẽ con mèo", "con mèo"},
		{"tạo ảnh con mèo", "con mèo"},
		{"làm ảnh con mèo", "con mèo"},
		{"/img con mèo", "con mèo"},
		{"draw a cat", "a cat"},
		// link đứng đầu rồi mới tới lời nhờ — đúng tình huống thật trong kênh
		{"https://cdn.shopify.com/a/image_3.png?v=1 gen ảnh thêm người đứng cạnh", "thêm người đứng cạnh"},
		{"https://a/b.png https://c/d.png vẽ lại cho vui", "lại cho vui"},
	}
	for _, c := range nhan {
		ok, got := route(c.in)
		if !ok {
			t.Errorf("%q: không nhận ra là lệnh vẽ", c.in)
			continue
		}
		if got != c.prompt {
			t.Errorf("%q: prompt = %q, muốn %q", c.in, got, c.prompt)
		}
	}
	// Không được nhận nhầm: đây là câu hỏi, không phải lệnh.
	for _, s := range []string{
		"ai vẽ cái bìa này thế?",
		"hôm qua ai gen cái ảnh đó nhỉ",
		"tóm tắt kênh giúp mình",
		"generate một báo cáo doanh thu",
	} {
		if ok, _ := route(s); ok {
			t.Errorf("%q: nhận nhầm thành lệnh vẽ", s)
		}
	}
}
