package controller

import (
	"strings"
	"testing"

	"chat/ai"
)

func TestStripThinking(t *testing.T) {
	cases := map[string]string{
		"<think>đếm số ván…</think>\nXuân Hoàng đang là vua.": "Xuân Hoàng đang là vua.",
		"<THINKING>a\nb</THINKING> Chào!":                     "Chào!",
		"Trả lời <think>nháp</think>xong":                     "Trả lời xong",
		"<think>đang nghĩ dở, chưa đóng thẻ":                  "", // đang stream: ẩn luôn
		"So sánh a < b là đúng":                               "So sánh a < b là đúng",
	}
	for in, want := range cases {
		if got := stripThinking(in); got != want {
			t.Errorf("stripThinking(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWithStyleReminder(t *testing.T) {
	hist := []ai.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hỏi"}}
	out := withStyleReminder(hist)
	if hist[1].Content != "hỏi" {
		t.Fatal("history modified")
	}
	if len(out) != 2 || !strings.HasPrefix(out[1].Content, "hỏi\n\n(Nhắc cách trả lời") {
		t.Fatalf("user turn: %+v", out)
	}
	// sau kết quả công cụ: ghép vào kết quả; sau tin assistant: thêm tin system
	if out := withStyleReminder(append(hist, ai.Message{Role: "tool", Content: "kq"})); !strings.Contains(out[2].Content, "Nhắc cách trả lời") {
		t.Fatalf("tool turn: %+v", out)
	}
	if out := withStyleReminder(append(hist, ai.Message{Role: "assistant", Content: "x"})); out[3].Role != "system" {
		t.Fatalf("assistant tail: %+v", out)
	}
}
