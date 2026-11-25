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
	maxMediaSize    = 95 << 20 // video/âm thanh: 95 MB, vừa giới hạn 100 MB mỗi request của Cloudflare Tunnel
	maxFilesPerMsg  = 10
	maxFileNameRune = 200
)

// isMediaType: video/âm thanh, được gửi file lớn hơn và phát ngay trong tin.
func isMediaType(ctype string) bool {
	return strings.HasPrefix(ctype, "video/") || strings.HasPrefix(ctype, "audio/")
}

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
//	files      0–10 file, mỗi file ≤ 20 MB (video/âm thanh ≤ 95 MB)
func (a *API) createMessageWithFiles(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	me := currentUser(r)

	// File được ghi tạm ra đĩa ngay khi đọc form, nên kiểm tra chỗ trống trước.
	if msg := a.checkDiskSpace(r.ContentLength); msg != "" {
		writeError(w, http.StatusInsufficientStorage, msg)
		return
	}
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
	// Tin mã hoá đầu cuối kèm file: file đã được mã hoá trên máy người gửi, tên/loại/khoá
	// của file nằm trong cipher. Server lưu như file nhị phân, không đọc được.
	cipher := r.FormValue("cipher")
	if cipher != "" {
		if msg := validateCipher(cipher); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		msg, err := a.cipherAllowed(r.Context(), ch.ID)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		content = ""
	}
	if len(clientID) > 64 {
		clientID = clientID[:64]
	}
	files := r.MultipartForm.File["files"]

	switch {
	case content == "" && cipher == "" && len(files) == 0:
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
		var limit int64
		switch {
		case cipher != "":
			// File đã mã hoá: server không biết loại thật nên cho tới mức của video
			// (client tự giữ file thường ≤ 20 MB). Bản mã dài hơn bản gốc 16 byte (tag).
			limit = maxMediaSize + 1024
		case fh.Size <= maxFileSize:
			limit = maxFileSize
		default:
			ctype, err := detectType(fh)
			if err != nil {
				internalError(w, r, err)
				return
			}
			limit = maxFileSize
			if isMediaType(ctype) {
				limit = maxMediaSize
			}
		}
		if fh.Size > limit {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
				"File “%s” quá lớn: tối đa %d MB (video/âm thanh %d MB).",
				cleanFileName(fh.Filename), maxFileSize>>20, maxMediaSize>>20))
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

	replyTo, msg2, err := a.validateReplyTo(r.Context(), ch.ID, r.FormValue("reply_to_id"))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if msg2 != "" {
		writeError(w, http.StatusBadRequest, msg2)
		return
	}
	msg, err := a.createAndBroadcast(r.Context(), me, ch.ID, outgoing{
		Content: content, Atts: saved, ClientID: clientID, ParentID: parentID, Cipher: cipher,
		ReplyToID: replyTo, Forwarded: r.FormValue("forwarded") == "1",
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	committed = true
	writeJSON(w, http.StatusCreated, msg)
}

// createAndBroadcast lưu tin (kèm mention) rồi phát message.new cho cả kênh.
// Dùng chung cho tin gửi qua websocket và tin kèm file gửi qua HTTP.
// outgoing: một tin sắp tạo (từ websocket hoặc HTTP).
type outgoing struct {
	Content   string
	Atts      []model.Attachment
	ClientID  string
	ParentID  *int64       // trả lời trong luồng
	Embed     *model.Embed // sticker / GIF
	Cipher    string       // tin mã hoá đầu cuối
	ReplyToID *int64       // trả lời trích dẫn
	Forwarded bool
}

func (a *API) createAndBroadcast(ctx context.Context, me model.User, channelID int64, o outgoing) (model.Message, error) {
	content, atts, clientID, parentID := o.Content, o.Atts, o.ClientID, o.ParentID
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
		Embed:       o.Embed,
		Cipher:      o.Cipher,
		ReplyToID:   o.ReplyToID,
		Forwarded:   o.Forwarded,
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
	a.maybeAI(msg, me)
	return msg, nil
}

// sniffType: loại file dựa vào nội dung thật, không tin header Content-Type client gửi.
// Không nhận ra thì đoán theo đuôi file.
func sniffType(src io.Reader, name string) string {
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	ctype := http.DetectContentType(head[:n])
	if ctype == "application/octet-stream" || strings.HasPrefix(ctype, "text/plain") {
		if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); byExt != "" {
			ctype = byExt
		}
	}
	return ctype
}

// detectType: sniffType cho file trong form, dùng trước khi lưu.
func detectType(fh *multipart.FileHeader) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()
	return sniffType(src, cleanFileName(fh.Filename)), nil
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
	ctype := sniffType(src, name)

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
	if (inlineImageTypes[att.ContentType] || isMediaType(att.ContentType)) && r.URL.Query().Get("download") == "" {
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
