package env

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadEnvFile(t *testing.T) {
	// Cookie thật có dấu =, ;, %, và có thể có # — không được cắt nhầm.
	cookie := `datr=abc%3D; hatch_sess=x.v1%3Ayyy%3Azzz.AAA=; theme=%7B%22a%22%3A1%23b%7D`
	p := writeEnv(t, `
# chú thích
export MUSE_UA='Mozilla/5.0 (X11; Linux x86_64) Chrome/156.0.0.0'
MUSE_COOKIE=`+cookie+`
MUSE_ADDR = "127.0.0.1:9000"
BỎ_QUA
EMPTY=
`)
	for _, k := range []string{"MUSE_UA", "MUSE_COOKIE", "MUSE_ADDR", "EMPTY"} {
		os.Unsetenv(k)
	}
	if err := Load(p); err != nil {
		t.Fatal(err)
	}
	if got, want := os.Getenv("MUSE_COOKIE"), cookie; got != want {
		t.Fatalf("cookie bị biến dạng:\n got %q\nmuốn %q", got, want)
	}
	if got := os.Getenv("MUSE_UA"); got != "Mozilla/5.0 (X11; Linux x86_64) Chrome/156.0.0.0" {
		t.Fatalf("UA = %q (nháy đơn chưa bị bỏ, hoặc tiền tố export chưa xử lý)", got)
	}
	if got := os.Getenv("MUSE_ADDR"); got != "127.0.0.1:9000" {
		t.Fatalf("ADDR = %q", got)
	}
	if got, có := os.LookupEnv("EMPTY"); !có || got != "" {
		t.Fatalf("EMPTY = %q, có=%v", got, có)
	}
}

func TestLoadEnvFileKhongGhiDe(t *testing.T) {
	p := writeEnv(t, "MUSE_UA=tu-file\n")
	t.Setenv("MUSE_UA", "tu-moi-truong")
	if err := Load(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("MUSE_UA"); got != "tu-moi-truong" {
		t.Fatalf("file đã ghi đè biến môi trường: %q", got)
	}
}

func TestLoadEnvFileThieuFile(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "khong-co.env")); err != nil {
		t.Fatalf("thiếu file không được coi là lỗi: %v", err)
	}
}
