package env

// Đọc file .env lúc khởi động để khỏi phải export tay. Không dùng thư viện ngoài.
//
// Quy tắc:
//   - Mỗi dòng KEY=VALUE, cho phép tiền tố "export ".
//   - Dòng trống và dòng bắt đầu bằng # (sau khi bỏ khoảng trắng) được bỏ qua.
//   - KHÔNG cắt chú thích giữa dòng: giá trị như cookie có thể chứa dấu #.
//   - Giá trị bọc trong '...' hoặc "..." thì bỏ cặp nháy ngoài cùng.
//   - Biến đã có sẵn trong môi trường thì giữ nguyên, file không ghi đè —
//     nhờ vậy `MUSE_COOKIE=... ./go-chat` vẫn thắng file.

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Load nạp path vào môi trường. File không tồn tại không phải lỗi.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // cookie có thể dài vài KB
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}
		if _, có := os.LookupEnv(k); !có {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}

// LoadDefault nạp file trỏ bởi ENV_FILE, mặc định ".env" cạnh thư mục chạy.
// Trả về đường dẫn đã thử để nơi gọi ghi log.
func LoadDefault() (string, error) {
	p := os.Getenv("ENV_FILE")
	if p == "" {
		p = ".env"
	}
	return p, Load(p)
}
