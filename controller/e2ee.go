package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"chat/store"
)

// Mã hoá đầu cuối (tin nhắn riêng). Server chỉ:
//   - lưu khoá công khai + bản sao lưu khoá bí mật đã khoá bằng PIN (không mở được),
//   - chuyển tiếp/lưu tin đã mã hoá (cipher) mà không đọc được nội dung.
// Mọi phép mã hoá/giải mã làm trên máy người dùng (desktop/src/lib/e2ee.ts).

const (
	maxCipherBytes = 64 << 10 // chữ 4000 ký tự + thông tin 10 file, sau base64
	maxBackupBytes = 2 << 10
)

// cipherEnvelope: phần vỏ client gửi lên. Server chỉ kiểm tra hình dạng, không giải mã được.
//
//	v  phiên bản (1)
//	s  khoá công khai người gửi, r  khoá công khai người nhận (base64, 32 byte)
//	n  nonce (base64, 24 byte), c  bản mã (base64)
type cipherEnvelope struct {
	V int    `json:"v"`
	S string `json:"s"`
	R string `json:"r"`
	N string `json:"n"`
	C string `json:"c"`
}

func decodeKey(s string, size int) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) == size
}

// validateCipher trả về câu báo lỗi, rỗng nếu hợp lệ.
func validateCipher(raw string) string {
	if len(raw) > maxCipherBytes {
		return "Tin nhắn quá dài."
	}
	var e cipherEnvelope
	if err := json.Unmarshal([]byte(raw), &e); err != nil || e.V != 1 ||
		!decodeKey(e.S, 32) || !decodeKey(e.R, 32) || !decodeKey(e.N, 24) {
		return "Tin nhắn mã hoá không hợp lệ."
	}
	if c, err := base64.StdEncoding.DecodeString(e.C); err != nil || len(c) < 16 {
		return "Tin nhắn mã hoá không hợp lệ."
	}
	return ""
}

// requireDMForCipher: tin mã hoá chỉ dùng trong tin nhắn riêng (kênh nhóm chưa hỗ trợ).
func (a *API) cipherAllowed(ctx context.Context, channelID int64) (string, error) {
	ch, err := a.store.GetChannel(ctx, channelID)
	if err != nil {
		return "", err
	}
	if !ch.IsDM() {
		return "Mã hoá đầu cuối hiện chỉ dùng cho tin nhắn riêng.", nil
	}
	return "", nil
}

/* ---------- GET/PUT /api/keys/me, GET /api/users/{id}/key ---------- */

func (a *API) getMyKey(w http.ResponseWriter, r *http.Request) {
	k, err := a.store.GetUserKey(r.Context(), currentUser(r).ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Bạn chưa thiết lập mã hoá đầu cuối.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

type putKeyRequest struct {
	PublicKey string `json:"public_key"`
	Backup    string `json:"backup"`
	// true: thay khoá cũ bằng khoá mới (quên PIN). Tin mã hoá bằng khoá cũ sẽ không đọc được nữa.
	Replace bool `json:"replace"`
}

type keyUpdatedEvent struct {
	Type      string `json:"type"`
	UserID    int64  `json:"user_id"`
	PublicKey string `json:"public_key"`
}

func (a *API) putMyKey(w http.ResponseWriter, r *http.Request) {
	var req putKeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var backup map[string]any
	if !decodeKey(req.PublicKey, 32) || len(req.Backup) > maxBackupBytes ||
		json.Unmarshal([]byte(req.Backup), &backup) != nil {
		writeError(w, http.StatusBadRequest, "Khoá không hợp lệ.")
		return
	}
	me := currentUser(r)
	cur, err := a.store.GetUserKey(r.Context(), me.ID)
	exists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		internalError(w, r, err)
		return
	}
	// Cùng khoá công khai: chỉ đổi PIN (bản sao lưu mới). Khác khoá: phải xác nhận thay.
	if exists && cur.PublicKey != req.PublicKey && !req.Replace {
		writeError(w, http.StatusConflict, "Tài khoản đã có khoá mã hoá. Nhập mã PIN để khôi phục, hoặc tạo khoá mới.")
		return
	}
	if err := a.store.PutUserKey(r.Context(), me.ID, req.PublicKey, req.Backup); err != nil {
		internalError(w, r, err)
		return
	}
	if !exists || cur.PublicKey != req.PublicKey {
		a.hub.ToAll(mustJSON(keyUpdatedEvent{Type: "key.updated", UserID: me.ID, PublicKey: req.PublicKey}))
	}
	writeJSON(w, http.StatusOK, store.UserKey{PublicKey: req.PublicKey, Backup: req.Backup})
}

func (a *API) getUserKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Không tìm thấy người dùng.")
		return
	}
	k, err := a.store.GetUserKey(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Người này chưa thiết lập mã hoá đầu cuối.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	// chỉ khoá công khai; bản sao lưu là của riêng chủ khoá
	writeJSON(w, http.StatusOK, store.UserKey{PublicKey: k.PublicKey})
}
