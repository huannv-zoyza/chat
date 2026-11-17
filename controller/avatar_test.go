package controller_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (e *env) uploadAvatar(token, name string, data []byte) (int, map[string]any) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, _ := mw.CreateFormFile("file", name)
	w.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/me/avatar", &buf)
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

func TestAvatarUpload(t *testing.T) {
	e, dir := newEnvWithUploads(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")

	// không phải ảnh (kể cả đặt tên .png) -> từ chối
	if code, _ := e.uploadAvatar(alice.Token, "x.png", []byte("<svg onload=alert(1)>")); code != 400 {
		t.Fatalf("non-image avatar: %d", code)
	}

	code, u := e.uploadAvatar(alice.Token, "me.png", pngBytes(64, 64))
	if code != 200 {
		t.Fatalf("upload avatar: %d %v", code, u)
	}
	first, _ := u["avatar"].(string)
	if !strings.HasPrefix(first, "/api/avatars/") {
		t.Fatalf("avatar path: %q", first)
	}
	// ai cũng xem được (thẻ <img> không gửi token), đúng loại ảnh
	res, err := http.Get(e.srv.URL + first)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, pngBytes(64, 64)) {
		t.Fatalf("serve avatar: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	// lưu hồ sơ với chính đường dẫn đó vẫn được; trỏ sang ảnh của người khác thì không
	if code := e.do("PATCH", "/api/me", alice.Token, map[string]any{"avatar": first}, nil); code != 200 {
		t.Fatalf("keep own avatar: %d", code)
	}
	if code := e.do("PATCH", "/api/me", bob.Token, map[string]any{"avatar": first}, nil); code != 400 {
		t.Fatalf("steal avatar path: %d", code)
	}

	// đổi ảnh mới: file cũ bị xoá
	_, u = e.uploadAvatar(alice.Token, "new.png", pngBytes(32, 32))
	second := u["avatar"].(string)
	oldFile := filepath.Join(dir, "avatars", strings.TrimPrefix(first, "/api/avatars/"))
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("old avatar file still exists: %v", err)
	}
	// xoá ảnh: file hiện tại cũng bị xoá
	if code := e.do("PATCH", "/api/me", alice.Token, map[string]any{"avatar": ""}, nil); code != 200 {
		t.Fatalf("clear avatar: %d", code)
	}
	if res, _ := http.Get(e.srv.URL + second); res.StatusCode != 404 {
		t.Fatalf("cleared avatar still served: %d", res.StatusCode)
	}
	if res, _ := http.Get(e.srv.URL + "/api/avatars/../../etc"); res.StatusCode != 404 {
		t.Fatalf("bad key: %d", res.StatusCode)
	}
}
