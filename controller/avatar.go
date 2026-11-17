package controller

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"chat/model"
)

// Ảnh đại diện tải lên được lưu ở <uploads>/avatars/<key> và phục vụ qua /api/avatars/<key>.
// users.avatar lưu đường dẫn tương đối "/api/avatars/<key>"; client tự ghép với địa chỉ server
// (app desktop và web có thể truy cập server qua địa chỉ khác nhau: LAN, tunnel…).
const (
	maxAvatarSize = 5 << 20 // 5 MB (app đã thu nhỏ ảnh trước khi gửi, thường chỉ vài chục KB)
	avatarPrefix  = "/api/avatars/"
)

var avatarPathRe = regexp.MustCompile(`^/api/avatars/([0-9a-f]{32})$`)

func (a *API) avatarDir() string { return filepath.Join(a.uploadsDir, "avatars") }

// uploadedAvatarKey trả về key nếu avatar là ảnh đã tải lên server này.
func uploadedAvatarKey(avatar *string) string {
	if avatar == nil {
		return ""
	}
	if m := avatarPathRe.FindStringSubmatch(*avatar); m != nil {
		return m[1]
	}
	return ""
}

// removeOldAvatar xoá file ảnh cũ khi người dùng đổi/xoá ảnh đại diện.
func (a *API) removeOldAvatar(old, next *string) {
	key := uploadedAvatarKey(old)
	if key == "" || key == uploadedAvatarKey(next) {
		return
	}
	if err := os.Remove(filepath.Join(a.avatarDir(), key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("xoá avatar %s: %v", key, err)
	}
}

/* ---------- POST /api/me/avatar (multipart, trường "file") ---------- */

func (a *API) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarSize+64<<10)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "Ảnh đại diện tối đa 5 MB.")
			return
		}
		writeError(w, http.StatusBadRequest, "Dữ liệu tải lên không hợp lệ.")
		return
	}
	defer r.MultipartForm.RemoveAll()

	src, fh, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Chưa chọn ảnh.")
		return
	}
	defer src.Close()
	if fh.Size > maxAvatarSize {
		writeError(w, http.StatusRequestEntityTooLarge, "Ảnh đại diện tối đa 5 MB.")
		return
	}

	// Chỉ nhận ảnh thật (đọc nội dung, không tin tên file hay Content-Type client gửi).
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	if !inlineImageTypes[http.DetectContentType(head[:n])] {
		writeError(w, http.StatusBadRequest, "Chỉ nhận ảnh PNG, JPEG, GIF hoặc WebP.")
		return
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		internalError(w, r, err)
		return
	}

	if err := os.MkdirAll(a.avatarDir(), 0o755); err != nil {
		internalError(w, r, err)
		return
	}
	key, err := newStorageKey()
	if err != nil {
		internalError(w, r, err)
		return
	}
	path := filepath.Join(a.avatarDir(), key)
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(path)
		internalError(w, r, err)
		return
	}
	if err := dst.Close(); err != nil {
		os.Remove(path)
		internalError(w, r, err)
		return
	}

	me := currentUser(r)
	avatar := avatarPrefix + key
	user, err := a.store.UpdateProfile(r.Context(), me.ID, me.DisplayName, &avatar, me.StatusText)
	if err != nil {
		os.Remove(path)
		internalError(w, r, err)
		return
	}
	a.removeOldAvatar(me.Avatar, &avatar)
	a.broadcastUser(user)
	writeJSON(w, http.StatusOK, user)
}

func (a *API) broadcastUser(user model.User) {
	a.hub.ToAll(mustJSON(userEvent{Type: "user.updated", User: user.Public()}))
}

/* ---------- GET /api/avatars/{key} ---------- */

func (a *API) serveAvatar(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !storageKeyRe.MatchString(key) {
		writeError(w, http.StatusNotFound, "Không tìm thấy ảnh.")
		return
	}
	f, err := os.Open(filepath.Join(a.avatarDir(), key))
	if err != nil {
		writeError(w, http.StatusNotFound, "Không tìm thấy ảnh.")
		return
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ctype := http.DetectContentType(head[:n])
	if !inlineImageTypes[ctype] {
		writeError(w, http.StatusNotFound, "Không tìm thấy ảnh.")
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		internalError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// key đổi mỗi lần tải ảnh mới nên cache lâu được
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	var mod time.Time
	if st, err := f.Stat(); err == nil {
		mod = st.ModTime()
	}
	http.ServeContent(w, r, "", mod, f)
}

func derefOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}
