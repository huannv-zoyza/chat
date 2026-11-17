package controller_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chat/controller"
	"chat/database"
	"chat/socket"
	"chat/store"
)

// newEnvWithUploads giống newEnv nhưng trả về thêm thư mục uploads để kiểm tra file trên đĩa.
func newEnvWithUploads(t *testing.T) (*env, string) {
	t.Helper()
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(database.MigrationOpt{DB: db, Mode: database.ModeUp}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	hub := socket.NewHub()
	srv := httptest.NewServer(controller.NewAPI(store.New(db), hub, nil, dir).Handler(nil))
	t.Cleanup(func() { hub.Shutdown(); srv.Close() })
	return &env{t: t, srv: srv}, dir
}

type upFile struct {
	name string
	data []byte
}

func (e *env) upload(channelID int64, token, content, clientID string, files ...upFile) (int, map[string]any) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("content", content)
	mw.WriteField("client_id", clientID)
	for _, f := range files {
		w, _ := mw.CreateFormFile("files", f.name)
		w.Write(f.data)
	}
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/channels/"+itoa(channelID)+"/messages", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestUploadAndServeFiles(t *testing.T) {
	e, dir := newEnvWithUploads(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "chung"}, &ch)

	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	// người ngoài kênh không gửi được
	if code, _ := e.upload(ch.ID, bob.Token, "hi", "x", upFile{"a.txt", []byte("x")}); code != 403 {
		t.Fatalf("non-member upload: %d", code)
	}
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	wb.waitFor("channel.joined")

	html := []byte("<html><script>alert(1)</script></html>")
	code, msg := e.upload(ch.ID, alice.Token, "Ảnh và file nè", "c-up",
		upFile{"ảnh chụp.png", pngBytes(64, 48)},
		upFile{"../../etc/báo cáo.html", html})
	if code != 201 {
		t.Fatalf("upload: %d %v", code, msg)
	}
	atts := msg["attachments"].([]any)
	if len(atts) != 2 {
		t.Fatalf("attachments: %v", atts)
	}
	img := atts[0].(map[string]any)
	if img["content_type"] != "image/png" || img["width"].(float64) != 64 || img["height"].(float64) != 48 || img["file_name"] != "ảnh chụp.png" {
		t.Fatalf("image attachment: %v", img)
	}
	doc := atts[1].(map[string]any)
	if doc["file_name"] != "báo cáo.html" {
		t.Fatalf("file name not sanitized: %v", doc["file_name"])
	}

	// bob nhận message.new kèm attachments qua websocket
	ev := wb.waitFor("message.new")
	if len(ev["message"].(map[string]any)["attachments"].([]any)) != 2 {
		t.Fatalf("ws attachments: %v", ev)
	}

	get := func(url string) *http.Response {
		res, err := http.Get(e.srv.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res
	}
	r := get(img["url"].(string))
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/png" || !strings.HasPrefix(r.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("serve image: %d %v", r.StatusCode, r.Header)
	}
	if r := get(img["url"].(string) + "?download=1"); !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download param: %v", r.Header.Get("Content-Disposition"))
	}
	r = get(doc["url"].(string))
	if !strings.HasPrefix(r.Header.Get("Content-Disposition"), "attachment") || r.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(r.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("html must be download-only and sandboxed: %v", r.Header)
	}
	if r := get("/api/files/00000000000000000000000000000000"); r.StatusCode != 404 {
		t.Fatalf("unknown file: %d", r.StatusCode)
	}

	// lịch sử cũng có attachments
	var hist struct {
		Messages []map[string]any `json:"messages"`
	}
	e.do("GET", "/api/channels/"+itoa(ch.ID)+"/messages", bob.Token, nil, &hist)
	if len(hist.Messages) != 1 || len(hist.Messages[0]["attachments"].([]any)) != 2 {
		t.Fatalf("history attachments: %v", hist.Messages)
	}

	// file quá 20 MB bị từ chối, không để lại file rác
	if code, _ := e.upload(ch.ID, alice.Token, "", "big", upFile{"big.bin", make([]byte, 20<<20+1)}); code != 413 {
		t.Fatalf("oversize: %d", code)
	}
	// tin trống (không chữ, không file) bị từ chối
	if code, _ := e.upload(ch.ID, alice.Token, "  ", "empty"); code != 400 {
		t.Fatalf("empty: %d", code)
	}
	if n, _ := os.ReadDir(dir); len(n) != 2 {
		t.Fatalf("files on disk = %d, want 2", len(n))
	}

	// xoá kênh thì xoá luôn file trên đĩa
	if code := e.do("DELETE", "/api/channels/"+itoa(ch.ID), alice.Token, nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if n, _ := os.ReadDir(dir); len(n) != 0 {
		t.Fatalf("files left after channel delete: %d", len(n))
	}
}

func TestMentions(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol.t", "Carol")
	e.register("dave", "Dave") // không ở trong kênh
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "chung"}, &ch)
	for _, u := range []authResp{bob, carol} {
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", u.Token, nil, nil)
	}

	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	send := func(content string) []any {
		wa.send(map[string]any{"type": "message.send", "channel_id": ch.ID, "content": content, "client_id": "m"})
		return wa.waitFor("message.new")["message"].(map[string]any)["mentions"].([]any)
	}
	ids := func(list []any) map[int64]bool {
		out := map[int64]bool{}
		for _, v := range list {
			out[int64(v.(float64))] = true
		}
		return out
	}

	got := ids(send("@Bob xem giúp, cả @carol.t. nữa, còn @dave thì không ở kênh, @alice là chính mình"))
	if len(got) != 2 || !got[bob.User.ID] || !got[carol.User.ID] {
		t.Fatalf("mentions = %v", got)
	}
	if got := send("gửi mail cho bob@example.com nhé"); len(got) != 0 {
		t.Fatalf("email treated as mention: %v", got)
	}
	got = ids(send("@all họp lúc 3h"))
	if len(got) != 2 || !got[bob.User.ID] || !got[carol.User.ID] {
		t.Fatalf("@all = %v", got)
	}

	// bob: 3 tin chưa đọc, 2 tin nhắc tên mình
	var list []channel
	e.do("GET", "/api/channels", bob.Token, nil, &list)
	var raw []map[string]any
	e.do("GET", "/api/channels", bob.Token, nil, &raw)
	if list[0].UnreadCount != 3 || raw[0]["mention_count"].(float64) != 2 {
		t.Fatalf("bob unread=%d mention_count=%v", list[0].UnreadCount, raw[0]["mention_count"])
	}
}
