// Package auth băm mật khẩu và sinh token phiên đăng nhập, chỉ dùng thư viện chuẩn.
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Iterations của PBKDF2-HMAC-SHA256, theo khuyến nghị OWASP.
// Là biến để test có thể hạ xuống cho nhanh.
var Iterations = 600_000

const (
	saltLen = 16
	keyLen  = 32
	prefix  = "pbkdf2_sha256"
)

// HashPassword trả về chuỗi dạng "pbkdf2_sha256$<iterations>$<salt>$<hash>".
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, Iterations, keyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", prefix, Iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword so khớp mật khẩu với chuỗi băm, thời gian so sánh không phụ thuộc nội dung.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != prefix {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken sinh token ngẫu nhiên 256-bit. Token gốc trả cho client,
// DB chỉ lưu HashToken(token) để lộ DB cũng không dùng được token.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
