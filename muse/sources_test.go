package muse

import (
	"strings"
	"testing"
)

func TestParseHistoryCitations(t *testing.T) {
	body := []byte(`{"result":{"chat_events":[
	  {"event_name":"message.user","display_text":"anime gì vậy"},
	  {"event_name":"message.assistant","display_text":"Đó là Overflow.\n\nNguồn:","display_text_ready":true,
	   "payload":{"status":"completed"},
	   "resources":[
	     {"kind":"citation","label":"Overflow","uri":"https://www.imdb.com/title/tt14962432"},
	     {"kind":"citation","label":"Overflow trùng","uri":"https://www.imdb.com/title/tt14962432"},
	     {"kind":"file","label":"x","uri":"sandbox://workspace/a.png"},
	     {"kind":"citation","label":"","uri":"https://en.namu.wiki/w/x"}]}]}}`)
	turns, err := parseHistory(body)
	if err != nil || len(turns) != 1 {
		t.Fatalf("parse: %v %v", err, turns)
	}
	c := turns[0].Citations
	if len(c) != 2 || c[0].URI != "https://www.imdb.com/title/tt14962432" || c[1].URI != "https://en.namu.wiki/w/x" {
		t.Fatalf("citations: %+v", c)
	}
}

func TestSourcesSuffix(t *testing.T) {
	cites := []Citation{
		{Label: "Overflow [anime]", URI: "https://a.example/x_(y)"},
		{Label: "", URI: "https://b.example/page"},
		{Label: "c", URI: "https://c.example"},
		{Label: "d", URI: "https://d.example"},
		{Label: "e", URI: "https://e.example"},
	}
	// đã có tiêu đề "Nguồn:" trống: nối link ngay sau
	got := SourcesSuffix("Đó là Overflow.\n\nNguồn:", cites)
	if !strings.HasPrefix(got, " [Overflow (anime)](https://a.example/x_%28y%29) · [b.example](https://b.example/page)") {
		t.Fatalf("after header: %q", got)
	}
	if strings.Count(got, "](") != maxSources {
		t.Fatalf("want %d links: %q", maxSources, got)
	}
	if got := SourcesSuffix("Câu trả lời.\n\n**Nguồn:**", cites[:1]); !strings.HasPrefix(got, " [") {
		t.Fatalf("bold header: %q", got)
	}
	// chưa có tiêu đề: thêm dòng Nguồn
	if got := SourcesSuffix("Câu trả lời.", cites[:1]); !strings.HasPrefix(got, "\n\nNguồn: [") {
		t.Fatalf("no header: %q", got)
	}
	// link đã có trong chữ thì thôi, không có nguồn thì rỗng
	if got := SourcesSuffix("xem https://a.example/x_(y)", cites[:1]); got != "" {
		t.Fatalf("already linked: %q", got)
	}
	if got := SourcesSuffix("abc", nil); got != "" {
		t.Fatalf("no cites: %q", got)
	}
}
