package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // đăng ký decoder để đọc kích thước ảnh
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

const (
	maxFileSize     = 20 << 20 // 20 MB mỗi file
	maxFilesPerMsg  = 10
	maxFileNameRune = 200
)

var storageKeyRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Ảnh hiển thị thẳng trên trình duyệt. Mọi loại khác (kể cả SVG, HTML) chỉ cho tải về,
// để file người dùng gửi lên không chạy được script trên origin của server.
var inlineImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

/* ---------- POST /api/channels/{id}/messages (multipart) ---------- */

// createMessageWithFiles nhận tin kèm file:
//
//	content    nội dung (có thể rỗng nếu có file)
//	client_id  để client khớp tin đang gửi với sự kiện message.new
//	files      0–10 file, mỗi file ≤ 20 MB
func (a *API) createMessageWithFiles(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	me := currentUser(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxFilesPerMsg*maxFileSize+(1<<20))
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "Tổng dung lượng file quá lớn.")
			return
		}
		writeError(w, http.StatusBadRequest, "Dữ liệu gửi lên không hợp lệ.")
		return
	}
	defer r.MultipartForm.RemoveAll()

	content := strings.TrimSpace(r.FormValue("content"))
	clientID := r.FormValue("client_id")
	if len(clientID) > 64 {
		clientID = clientID[:64]
	}
	files := r.MultipartForm.File["files"]

	switch {
	case content == "" && len(files) == 0:
		writeError(w, http.StatusBadRequest, "Tin nhắn đang trống.")
		return
	case len(files) > maxFilesPerMsg:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Mỗi tin gửi tối đa %d file.", maxFilesPerMsg))
		return
	case utf8.RuneCountInString(content) > maxMessageRunes:
		writeError(w, http.StatusBadRequest, "Tin nhắn dài tối đa 4000 ký tự.")
		return
	}
	for _, fh := range files {
		if fh.Size > maxFileSize {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("File “%s” lớn hơn 20 MB.", cleanFileName(fh.Filename)))
			return
		}
	}

	// Lưu file lên đĩa trước; nếu bước sau lỗi thì xoá những file đã lưu.
	var saved []model.Attachment
	committed := false
	defer func() {
		if !committed {
			for _, att := range saved {
				a.removeUpload(att.StorageKey)
			}
		}
	}()
	for _, fh := range files {
		att, err := a.saveUpload(fh)
		if err != nil {
			internalError(w, r, err)
			return
		}
		saved = append(saved, att)
	}

	var parentID *int64
	if v := r.FormValue("parent_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "Tin nhắn gốc không hợp lệ.")
			return
		}
		msg, err := a.validateParent(r.Context(), ch.ID, id)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		parentID = &id
	}

	msg, err := a.createAndBroadcast(r.Context(), me, ch.ID, content, saved, clientID, parentID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	committed = true
	writeJSON(w, http.StatusCreated, msg)
}

// createAndBroadcast lưu tin (kèm mention) rồi phát message.new cho cả kênh.
// Dùng chung cho tin gửi qua websocket và tin kèm file gửi qua HTTP.
func (a *API) createAndBroadcast(ctx context.Context, me model.User, channelID int64, content string, atts []model.Attachment, clientID string, parentID *int64) (model.Message, error) {
	usernames, all := parseMentions(content)
	mentionIDs, err := a.store.ResolveMentions(ctx, channelID, me.ID, usernames, all)
	if err != nil {
		return model.Message{}, err
	}
	msg, err := a.store.CreateMessage(ctx, store.NewMessage{
		ChannelID:   channelID,
		UserID:      me.ID,
		Content:     content,
		Attachments: atts,
		MentionIDs:  mentionIDs,
		ParentID:    parentID,
	})
	if err != nil {
		return model.Message{}, err
	}
	// Người gửi tự đọc tin của mình rồi (client tự suy ra từ message.new, không cần gửi read.updated). Trả lời trong luồng thì không: dòng chính có thể
	// còn tin người gửi chưa xem, đánh dấu tới id trả lời sẽ làm mất số chưa đọc của các tin đó.
	if parentID == nil {
		_, _, _ = a.store.MarkRead(ctx, channelID, me.ID, msg.ID)
	}
	a.hub.ToChannel(channelID, mustJSON(messageNewEvent{Type: "message.new", Message: msg, ClientID: clientID}), nil)
	if parentID != nil {
		a.broadcastThread(ctx, channelID, *parentID)
	}
	return msg, nil
}

// saveUpload ghi file vào thư mục uploads với tên ngẫu nhiên, đọc loại file thật và kích thước ảnh.
func (a *API) saveUpload(fh *multipart.FileHeader) (model.Attachment, error) {
	src, err := fh.Open()
	if err != nil {
		return model.Attachment{}, err
	}
	defer src.Close()

	key, err := newStorageKey()
	if err != nil {
		return model.Attachment{}, err
	}
	name := cleanFileName(fh.Filename)

	// Loại file: dựa vào nội dung thật, không tin header Content-Type client gửi.
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	ctype := http.DetectContentType(head[:n])
	if ctype == "application/octet-stream" || strings.HasPrefix(ctype, "text/plain") {
		if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); byExt != "" {
			ctype = byExt
		}
	}

	att := model.Attachment{StorageKey: key, FileName: name, ContentType: ctype, Size: fh.Size}

	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return att, err
	}
	if inlineImageTypes[ctype] {
		if cfg, _, err := image.DecodeConfig(src); err == nil {
			att.Width, att.Height = cfg.Width, cfg.Height
		}
		if _, err := src.Seek(0, io.SeekStart); err != nil {
			return att, err
		}
	}

	dst, err := os.OpenFile(filepath.Join(a.uploadsDir, key), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return att, err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		a.removeUpload(key)
		return att, err
	}
	if err := dst.Close(); err != nil {
		a.removeUpload(key)
		return att, err
	}
	att.URL = store.AttachmentURL(key)
	return att, nil
}

func (a *API) removeUpload(key string) {
	if !storageKeyRe.MatchString(key) {
		return
	}
	if err := os.Remove(filepath.Join(a.uploadsDir, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("xoá file %s: %v", key, err)
	}
}

func newStorageKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// cleanFileName bỏ đường dẫn và ký tự điều khiển, giới hạn độ dài.
func cleanFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	if utf8.RuneCountInString(name) > maxFileNameRune {
		ext := filepath.Ext(name)
		runes := []rune(strings.TrimSuffix(name, ext))
		name = string(runes[:maxFileNameRune-utf8.RuneCountInString(ext)]) + ext
	}
	return name
}

/* ---------- GET /api/files/{key}[?download=1] ---------- */

// serveFile không cần đăng nhập: key là 128-bit ngẫu nhiên, chỉ thành viên kênh mới thấy
// (thẻ <img> không gửi được header Authorization).
func (a *API) serveFile(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !storageKeyRe.MatchString(key) {
		writeError(w, http.StatusNotFound, "Không tìm thấy file.")
		return
	}
	att, err := a.store.AttachmentByKey(r.Context(), key)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy file.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	f, err := os.Open(filepath.Join(a.uploadsDir, key))
	if err != nil {
		writeError(w, http.StatusNotFound, "File không còn trên máy chủ.")
		return
	}
	defer f.Close()

	disposition := "attachment"
	media := strings.HasPrefix(att.ContentType, "video/") || strings.HasPrefix(att.ContentType, "audio/")
	if (inlineImageTypes[att.ContentType] || media) && r.URL.Query().Get("download") == "" {
		disposition = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", att.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": att.FileName}))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", att.CreatedAt, f)
}
