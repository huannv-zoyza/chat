package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"chat/auth"
	"chat/model"
	"chat/store"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9_.]{3,32}$`)

// Dùng để so mật khẩu khi không tìm thấy user, giữ thời gian phản hồi như nhau
// để không dò được username nào tồn tại.
var dummyHash, _ = auth.HashPassword("dummy-password-for-timing")

type authResponse struct {
	Token string     `json:"token"`
	User  model.User `json:"user"`
}

func validatePassword(pw string) string {
	switch n := utf8.RuneCountInString(pw); {
	case n < 8:
		return "Mật khẩu cần ít nhất 8 ký tự."
	case n > 128:
		return "Mật khẩu dài tối đa 128 ký tự."
	}
	return ""
}

func (a *API) issueSession(w http.ResponseWriter, r *http.Request, status int, user model.User) {
	token, err := auth.NewToken()
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.store.CreateSession(r.Context(), user.ID, auth.HashToken(token), time.Now().Add(sessionTTL)); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, status, authResponse{Token: token, User: user})
}

/* ---------- POST /api/auth/register ---------- */

type registerRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	email := strings.TrimSpace(req.Email)
	displayName := strings.TrimSpace(req.DisplayName)

	if !usernameRe.MatchString(username) {
		writeError(w, http.StatusBadRequest, "Tên đăng nhập gồm 3–32 ký tự: chữ thường không dấu, số, dấu chấm hoặc gạch dưới.")
		return
	}
	// đuôi "_bot" và email @bot.invalid dành cho bot tích hợp: không cho người giả làm bot
	if isBotUsername(username) || strings.HasSuffix(strings.ToLower(email), botEmailDomain) {
		writeError(w, http.StatusBadRequest, "Tên đăng nhập đuôi “_bot” dành cho bot, hãy chọn tên khác.")
		return
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 255 {
		writeError(w, http.StatusBadRequest, "Email không hợp lệ.")
		return
	}
	if msg := validatePassword(req.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if displayName == "" {
		displayName = username
	}
	if utf8.RuneCountInString(displayName) > 64 {
		writeError(w, http.StatusBadRequest, "Tên hiển thị dài tối đa 64 ký tự.")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		internalError(w, r, err)
		return
	}
	user, err := a.store.RegisterUser(r.Context(), username, email, hash, displayName, clientIP(r), a.signupsPerIP)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "Tên đăng nhập hoặc email đã được dùng.")
		return
	}
	if errors.Is(err, store.ErrSignupLimit) {
		writeError(w, http.StatusTooManyRequests, "Mạng của bạn đã tạo quá nhiều tài khoản hôm nay. Thử lại sau 24 giờ.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.issueSession(w, r, http.StatusCreated, user)
}

/* ---------- POST /api/auth/login ---------- */

type loginRequest struct {
	Login    string `json:"login"` // username hoặc email
	Password string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if wait := a.logins.Blocked(req.Login); wait > 0 {
		mins := int(wait.Minutes()) + 1
		writeError(w, http.StatusTooManyRequests,
			fmt.Sprintf("Đăng nhập sai quá nhiều lần. Thử lại sau %d phút.", mins))
		return
	}
	user, err := a.store.GetUserByLogin(r.Context(), strings.TrimSpace(req.Login))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		internalError(w, r, err)
		return
	}
	hash := user.PasswordHash
	if err != nil {
		hash = dummyHash
	}
	if !auth.VerifyPassword(req.Password, hash) || err != nil {
		a.logins.Fail(req.Login)
		writeError(w, http.StatusUnauthorized, "Sai tên đăng nhập hoặc mật khẩu.")
		return
	}
	a.logins.Reset(req.Login)
	if user.Status != model.UserStatusActive {
		writeError(w, http.StatusForbidden, "Tài khoản đã bị khoá.")
		return
	}
	a.issueSession(w, r, http.StatusOK, user)
}

/* ---------- POST /api/auth/logout ---------- */

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	hash := currentTokenHash(r)
	if err := a.store.DeleteSession(r.Context(), hash); err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.CloseSessions(currentUser(r).ID, func(h string) bool { return h == hash }, closeUnauthorized)
	w.WriteHeader(http.StatusNoContent)
}

/* ---------- GET/PATCH /api/me ---------- */

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

// Trường nào không gửi lên (nil) thì giữ nguyên.
type updateMeRequest struct {
	DisplayName *string `json:"display_name"`
	Avatar      *string `json:"avatar"` // "" để xoá ảnh; link http(s) hoặc "/api/avatars/<key>" đã tải lên
	StatusText  *string `json:"status_text"`
}

func (a *API) updateMe(w http.ResponseWriter, r *http.Request) {
	var req updateMeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	me := currentUser(r)
	displayName, avatar, statusText := me.DisplayName, me.Avatar, me.StatusText

	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if displayName == "" || utf8.RuneCountInString(displayName) > 64 {
			writeError(w, http.StatusBadRequest, "Tên hiển thị cần 1–64 ký tự.")
			return
		}
	}
	if req.StatusText != nil {
		statusText = strings.TrimSpace(*req.StatusText)
		if utf8.RuneCountInString(statusText) > 140 {
			writeError(w, http.StatusBadRequest, "Trạng thái dài tối đa 140 ký tự.")
			return
		}
	}
	if req.Avatar != nil {
		v := strings.TrimSpace(*req.Avatar)
		if v == "" {
			avatar = nil
		} else if avatarPathRe.MatchString(v) {
			// ảnh đã tải lên: chỉ cho giữ ảnh của chính mình (không trỏ sang ảnh người khác)
			if v != derefOr(me.Avatar, "") {
				writeError(w, http.StatusBadRequest, "Hãy tải ảnh lên bằng nút chọn ảnh.")
				return
			}
			avatar = &v
		} else {
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(v) > 500 {
				writeError(w, http.StatusBadRequest, "Ảnh đại diện phải là đường dẫn http(s) tới một ảnh.")
				return
			}
			avatar = &v
		}
	}

	user, err := a.store.UpdateProfile(r.Context(), me.ID, displayName, avatar, statusText)
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.removeOldAvatar(me.Avatar, avatar)
	// Báo cho mọi người đang online để cập nhật tên/ảnh trong danh sách tin nhắn.
	a.broadcastUser(user)
	writeJSON(w, http.StatusOK, user)
}

/* ---------- PUT /api/me/password ---------- */

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	me := currentUser(r)
	if !auth.VerifyPassword(req.CurrentPassword, me.PasswordHash) {
		writeError(w, http.StatusBadRequest, "Mật khẩu hiện tại không đúng.")
		return
	}
	if msg := validatePassword(req.NewPassword); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.store.UpdatePassword(r.Context(), me.ID, hash); err != nil {
		internalError(w, r, err)
		return
	}
	// Đăng xuất các thiết bị khác, giữ lại phiên hiện tại.
	keep := currentTokenHash(r)
	if err := a.store.DeleteOtherSessions(r.Context(), me.ID, keep); err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.CloseSessions(me.ID, func(h string) bool { return h != keep }, closeUnauthorized)
	w.WriteHeader(http.StatusNoContent)
}
