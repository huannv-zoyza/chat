package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"chat/auth"
	"chat/model"
	"chat/socket"
	"chat/store"
)

const sessionTTL = 30 * 24 * time.Hour

type API struct {
	store          *store.Store
	hub            *socket.Hub
	originPatterns []string
	uploadsDir     string // thư mục lưu file người dùng gửi
	logins         *loginLimiter
	unfurlClient   *http.Client // xem trước link: chỉ kết nối được tới internet công khai
	unfurls        unfurlCache
}

// originPatterns: các origin được phép gọi API và mở websocket từ trình duyệt/app,
// ví dụ []string{"localhost:*"}. Cú pháp giống AcceptOptions.OriginPatterns của websocket.
func NewAPI(st *store.Store, hub *socket.Hub, originPatterns []string, uploadsDir string) *API {
	return &API{
		store: st, hub: hub, originPatterns: originPatterns, uploadsDir: uploadsDir,
		logins:       newLoginLimiter(10, 15*time.Minute),
		unfurlClient: newUnfurlClient(),
	}
}

// Handler trả về toàn bộ router, đã bọc CORS.
// ui (có thể nil) phục vụ giao diện web cho mọi đường dẫn không thuộc /api và /ws.
func (a *API) Handler(ui http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.requireAuth(a.logout))

	mux.HandleFunc("GET /api/me", a.requireAuth(a.getMe))
	mux.HandleFunc("PATCH /api/me", a.requireAuth(a.updateMe))
	mux.HandleFunc("PUT /api/me/password", a.requireAuth(a.changePassword))

	mux.HandleFunc("GET /api/channels", a.requireAuth(a.listMyChannels))
	mux.HandleFunc("GET /api/channels/discover", a.requireAuth(a.listOtherChannels))
	mux.HandleFunc("POST /api/channels", a.requireAuth(a.createChannel))
	mux.HandleFunc("GET /api/channels/{id}", a.requireAuth(a.getChannel))
	mux.HandleFunc("PATCH /api/channels/{id}", a.requireAuth(a.updateChannel))
	mux.HandleFunc("DELETE /api/channels/{id}", a.requireAuth(a.deleteChannel))
	mux.HandleFunc("POST /api/channels/{id}/join", a.requireAuth(a.joinChannel))
	mux.HandleFunc("POST /api/channels/{id}/leave", a.requireAuth(a.leaveChannel))
	mux.HandleFunc("POST /api/channels/{id}/invite", a.requireAuth(a.regenerateInvite))
	mux.HandleFunc("GET /api/invites/{code}", a.requireAuth(a.previewInvite))
	mux.HandleFunc("POST /api/invites/{code}/join", a.requireAuth(a.joinByInvite))
	mux.HandleFunc("GET /api/channels/{id}/members", a.requireAuth(a.listMembers))
	mux.HandleFunc("GET /api/channels/{id}/messages", a.requireAuth(a.listMessages))
	mux.HandleFunc("POST /api/channels/{id}/messages", a.requireAuth(a.createMessageWithFiles))
	mux.HandleFunc("GET /api/files/{key}", a.serveFile)
	mux.HandleFunc("GET /api/messages/{id}/thread", a.requireAuth(a.getThread))
	mux.HandleFunc("GET /api/unfurl", a.requireAuth(a.unfurl))
	mux.HandleFunc("POST /api/messages/{id}/reactions", a.requireAuth(a.toggleReaction))

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /ws", a.serveSocket)

	// /api/... không khớp route nào: trả JSON 404 thay vì rơi xuống giao diện web.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Không có API này.")
	})
	if ui != nil {
		// Đăng ký "/" không kèm method (kèm method sẽ xung đột với "/api/" ở trên).
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				writeError(w, http.StatusMethodNotAllowed, "Method không được hỗ trợ.")
				return
			}
			ui.ServeHTTP(w, r)
		})
	}

	return a.cors(mux)
}

/* ---------- JSON helpers ---------- */

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

// writeError: msg là câu hiển thị thẳng cho người dùng.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func internalError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	writeError(w, http.StatusInternalServerError, "Server gặp lỗi, thử lại sau.")
}

// decodeJSON đọc body tối đa 64KB, từ chối trường lạ để bắt lỗi gõ sai tên trường.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "Dữ liệu gửi lên không hợp lệ.")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "Không tìm thấy kênh.")
		return 0, false
	}
	return id, true
}

/* ---------- CORS ---------- */

// originAllowed dùng cùng quy tắc với websocket.Accept: cùng host, hoặc khớp một pattern.
func (a *API) originAllowed(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(r.Host, u.Host) {
		return true
	}
	for _, p := range a.originPatterns {
		target := u.Host
		if strings.Contains(p, "://") {
			target = u.Scheme + "://" + u.Host
		}
		if ok, _ := path.Match(strings.ToLower(p), strings.ToLower(target)); ok {
			return true
		}
	}
	return false
}

func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && r.URL.Path != "/ws" {
			if !a.originAllowed(r, origin) {
				writeError(w, http.StatusForbidden, "Origin không được phép.")
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

/* ---------- Xác thực ---------- */

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxTokenHash
)

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(t)
	}
	return ""
}

func (a *API) userFromToken(ctx context.Context, token string) (model.User, error) {
	if token == "" {
		return model.User{}, store.ErrNotFound
	}
	return a.store.UserBySession(ctx, auth.HashToken(token))
}

func (a *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		user, err := a.userFromToken(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "Phiên đăng nhập đã hết hạn, hãy đăng nhập lại.")
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, user)
		ctx = context.WithValue(ctx, ctxTokenHash, auth.HashToken(token))
		next(w, r.WithContext(ctx))
	}
}

func currentUser(r *http.Request) model.User {
	return r.Context().Value(ctxUser).(model.User)
}

func currentTokenHash(r *http.Request) string {
	return r.Context().Value(ctxTokenHash).(string)
}
