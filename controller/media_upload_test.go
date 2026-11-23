package controller_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"chat/controller"
	"chat/database"
	"chat/socket"
	"chat/store"
)

// mp4Bytes: file MP4 (hộp ftyp đúng chuẩn) dài size byte.
func mp4Bytes(size int) []byte {
	b := make([]byte, size)
	copy(b, []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'})
	return b
}

func TestUploadLargeVideo(t *testing.T) {
	e, dir := newEnvWithUploads(t)
	alice := e.register("alice", "Alice")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "chung"}, &ch)

	// video > 20 MB được nhận và phát ngay trong tin
	code, msg := e.upload(ch.ID, alice.Token, "", "v1", upFile{"clip.mp4", mp4Bytes(30 << 20)})
	if code != 201 {
		t.Fatalf("30 MB video: %d %v", code, msg)
	}
	att := msg["attachments"].([]any)[0].(map[string]any)
	if att["content_type"] != "video/mp4" {
		t.Fatalf("content_type = %v", att["content_type"])
	}

	// file thường > 20 MB vẫn bị chặn
	if code, _ := e.upload(ch.ID, alice.Token, "", "b1", upFile{"big.bin", make([]byte, 30<<20)}); code != 413 {
		t.Fatalf("30 MB binary: %d", code)
	}
	// video quá 95 MB bị chặn
	if code, _ := e.upload(ch.ID, alice.Token, "", "v2", upFile{"long.mp4", mp4Bytes(95<<20 + 1)}); code != 413 {
		t.Fatalf("oversize video: %d", code)
	}
	if n, _ := os.ReadDir(dir); len(n) != 1 {
		t.Fatalf("files on disk = %d, want 1", len(n))
	}
}

func TestUploadRefusedWhenDiskLow(t *testing.T) {
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
	api := controller.NewAPI(store.New(db), hub, nil, dir)
	api.SetMinFreeDisk(1 << 62) // không đĩa nào trống được chừng này
	srv := httptest.NewServer(api.Handler(nil))
	t.Cleanup(func() { hub.Shutdown(); srv.Close() })
	e := &env{t: t, srv: srv}

	alice := e.register("alice", "Alice")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "chung"}, &ch)
	code, msg := e.upload(ch.ID, alice.Token, "", "x", upFile{"a.png", pngBytes(4, 4)})
	if code != 507 {
		t.Fatalf("low disk: %d %v", code, msg)
	}
	if n, _ := os.ReadDir(dir); len(n) != 0 {
		t.Fatalf("files on disk = %d, want 0", len(n))
	}
}
