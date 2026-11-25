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
	"sync"
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
	throws         throwLimiter // giới hạn tốc độ ném đồ
	originPatterns []string
	uploadsDir     string // thư mục lưu file người dùng gửi
	minFreeDisk    uint64 // còn ít hơn chừng này byte trống thì không nhận file mới
	logins         *loginLimiter
	newAccountHold time.Duration // tài khoản mới phải chờ chừng này mới đưa xu cho người khác được
	devicesSeen    sync.Map      // "userID:deviceID" đã ghi vào user_devices trong lần chạy này
	unfurlClient   *http.Client  // xem trước link: chỉ kết nối được tới internet công khai
	unfurls        unfurlCache
	giphyKey       string // rỗng: tắt tìm GIF
	giphyBase      string
	giphyClient    *http.Client
	gifs           gifCache
	calls          callRegistry
	groups         groupRegistry
	games          gameRegistry
	voices         voiceRegistry
	watchParties   watchPartyRegistry
	ai             *aiService   // nil: tắt trợ lý AI
	hookRate       *rateWindow  // giới hạn tin mỗi bot tích hợp
	botClient      *http.Client // gọi URL trả lời của bot (cho phép địa chỉ LAN)
	iceServers     []ICEServer
	uiBuild        string
	turnMu         sync.RWMutex
	turn           TURNProvider
	tlsPort        string
}

// SetUIBuild: mã bản build giao diện web đang nhúng (gửi cho client trong sự kiện "ready").
func (a *API) SetUIBuild(id string) { a.uiBuild = id }

// originPatterns: các origin được phép gọi API và mở websocket từ trình duyệt/app,
// ví dụ []string{"localhost:*"}. Cú pháp giống AcceptOptions.OriginPatterns của websocket.
func NewAPI(st *store.Store, hub *socket.Hub, originPatterns []string, uploadsDir string) *API {
	return &API{
		store: st, hub: hub, originPatterns: originPatterns, uploadsDir: uploadsDir,
		minFreeDisk:    defaultMinFreeDisk,
		logins:         newLoginLimiter(10, 15*time.Minute),
		newAccountHold: defaultNewAccountHold,
		unfurlClient:   newUnfurlClient(),
		hookRate:       newRateWindow(hookRatePerMin, time.Minute),
		botClient:      &http.Client{Timeout: outgoingTimeout},
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
	mux.HandleFunc("GET /api/me/prefs", a.requireAuth(a.getPrefs))
	mux.HandleFunc("PATCH /api/me/prefs", a.requireAuth(a.updatePrefs))
	mux.HandleFunc("PUT /api/me/password", a.requireAuth(a.changePassword))
	mux.HandleFunc("POST /api/me/avatar", a.requireAuth(a.uploadAvatar))
	mux.HandleFunc("GET /api/avatars/{key}", a.serveAvatar)

	mux.HandleFunc("GET /api/channels", a.requireAuth(a.listMyChannels))
	mux.HandleFunc("GET /api/channels/discover", a.requireAuth(a.listOtherChannels))
	mux.HandleFunc("POST /api/channels", a.requireAuth(a.createChannel))
	mux.HandleFunc("GET /api/channels/{id}", a.requireAuth(a.getChannel))
	mux.HandleFunc("PATCH /api/channels/{id}", a.requireAuth(a.updateChannel))
	mux.HandleFunc("DELETE /api/channels/{id}", a.requireAuth(a.deleteChannel))
	mux.HandleFunc("POST /api/channels/{id}/join", a.requireAuth(a.joinChannel))
	mux.HandleFunc("POST /api/channels/{id}/leave", a.requireAuth(a.leaveChannel))
	mux.HandleFunc("DELETE /api/channels/{id}/members/{userId}", a.requireAuth(a.kickMember))
	mux.HandleFunc("GET /api/channels/{id}/wallet-board", a.requireAuth(a.walletBoard))
	mux.HandleFunc("POST /api/channels/{id}/throw", a.requireAuth(a.throwItem))
	mux.HandleFunc("GET /api/channels/{id}/nicknames", a.requireAuth(a.listNicknames))
	mux.HandleFunc("GET /api/channels/{id}/integrations", a.requireAuth(a.listIntegrations))
	mux.HandleFunc("POST /api/channels/{id}/integrations", a.requireAuth(a.createIntegration))
	mux.HandleFunc("PATCH /api/channels/{id}/integrations/{iid}", a.requireAuth(a.updateIntegration))
	mux.HandleFunc("POST /api/channels/{id}/integrations/{iid}/token", a.requireAuth(a.rotateIntegrationToken))
	mux.HandleFunc("DELETE /api/channels/{id}/integrations/{iid}", a.requireAuth(a.deleteIntegration))
	mux.HandleFunc("POST /api/hooks/{token}", a.incomingHook)
	mux.HandleFunc("PUT /api/channels/{id}/nicknames/{userId}", a.requireAuth(a.setNickname))
	mux.HandleFunc("GET /api/wallet", a.requireAuth(a.getWallet))
	mux.HandleFunc("POST /api/wallet/claim", a.requireAuth(a.walletAction(func(r *http.Request, id int64) (store.Wallet, error) {
		return a.store.ClaimDaily(r.Context(), id, vnToday(a.store.Now()))
	})))
	mux.HandleFunc("POST /api/wallet/loan", a.requireAuth(a.walletAction(func(r *http.Request, id int64) (store.Wallet, error) {
		return a.store.TakeLoan(r.Context(), id)
	})))
	mux.HandleFunc("POST /api/wallet/repay", a.requireAuth(a.walletAction(func(r *http.Request, id int64) (store.Wallet, error) {
		return a.store.RepayDebt(r.Context(), id)
	})))
	mux.HandleFunc("GET /api/wallet/transactions", a.requireAuth(a.listWalletTransactions))
	mux.HandleFunc("GET /api/farm", a.requireAuth(a.getFarm))
	mux.HandleFunc("GET /api/farm/users/{userId}", a.requireAuth(a.getUserFarm))
	mux.HandleFunc("GET /api/farm/neighbors", a.requireAuth(a.farmNeighbors))
	mux.HandleFunc("GET /api/farm/leaderboard", a.requireAuth(a.farmLeaderboard))
	mux.HandleFunc("POST /api/farm/plant", a.requireAuth(a.farmPlant))
	mux.HandleFunc("POST /api/farm/harvest", a.requireAuth(a.farmHarvest))
	mux.HandleFunc("POST /api/farm/clear", a.requireAuth(a.farmClear))
	mux.HandleFunc("POST /api/farm/water", a.requireAuth(a.farmWater))
	mux.HandleFunc("POST /api/farm/bug", a.requireAuth(a.farmBug))
	mux.HandleFunc("POST /api/farm/steal", a.requireAuth(a.farmSteal))
	mux.HandleFunc("POST /api/farm/expand", a.requireAuth(a.farmExpand))
	mux.HandleFunc("POST /api/farm/buy-plot", a.requireAuth(a.farmExpand)) // bí danh cũ của /expand
	mux.HandleFunc("POST /api/farm/sell", a.requireAuth(a.farmSell))
	mux.HandleFunc("POST /api/farm/buy", a.requireAuth(a.farmBuyItem))
	mux.HandleFunc("POST /api/farm/storage/upgrade", a.requireAuth(a.farmUpgradeStorage))
	mux.HandleFunc("POST /api/farm/buildings/{id}", a.requireAuth(a.farmBuild))
	mux.HandleFunc("POST /api/farm/buildings/{id}/upgrade", a.requireAuth(a.farmUpgradeBuilding))
	mux.HandleFunc("POST /api/farm/animals", a.requireAuth(a.farmBuyAnimal))
	mux.HandleFunc("POST /api/farm/animals/feed", a.requireAuth(a.farmFeed))
	mux.HandleFunc("POST /api/farm/animals/collect", a.requireAuth(a.farmCollect))
	mux.HandleFunc("POST /api/farm/orders/{id}/deliver", a.requireAuth(a.farmDeliverOrder))
	mux.HandleFunc("DELETE /api/farm/orders/{id}", a.requireAuth(a.farmDiscardOrder))
	mux.HandleFunc("POST /api/farm/buy-dog", a.requireAuth(a.farmBuyDog))
	mux.HandleFunc("POST /api/farm/quests/{questId}/claim", a.requireAuth(a.farmClaimQuest))
	mux.HandleFunc("POST /api/wallet/transfer", a.requireAuth(a.transferCoins))
	mux.HandleFunc("POST /api/channels/{id}/invite", a.requireAuth(a.regenerateInvite))
	mux.HandleFunc("GET /api/invites/{code}", a.requireAuth(a.previewInvite))
	mux.HandleFunc("POST /api/invites/{code}/join", a.requireAuth(a.joinByInvite))
	mux.HandleFunc("GET /api/channels/{id}/members", a.requireAuth(a.listMembers))
	mux.HandleFunc("GET /api/channels/{id}/messages", a.requireAuth(a.listMessages))
	mux.HandleFunc("POST /api/channels/{id}/messages", a.requireAuth(a.createMessageWithFiles))
	mux.HandleFunc("GET /api/files/{key}", a.serveFile)
	mux.HandleFunc("GET /api/messages/{id}/thread", a.requireAuth(a.getThread))
	mux.HandleFunc("GET /api/messages/{id}/link", a.requireAuth(a.messageLink))
	mux.HandleFunc("GET /api/unfurl", a.requireAuth(a.unfurl))
	mux.HandleFunc("POST /api/messages/{id}/reactions", a.requireAuth(a.toggleReaction))
	mux.HandleFunc("PATCH /api/messages/{id}", a.requireAuth(a.editMessage))
	mux.HandleFunc("DELETE /api/messages/{id}", a.requireAuth(a.deleteMessage))
	mux.HandleFunc("POST /api/channels/{id}/polls", a.requireAuth(a.createPoll))
	mux.HandleFunc("POST /api/messages/{id}/pin", a.requireAuth(a.pinMessage))
	mux.HandleFunc("GET /api/channels/{id}/pins", a.requireAuth(a.listPins))
	mux.HandleFunc("GET /api/search", a.requireAuth(a.search))
	mux.HandleFunc("POST /api/messages/{id}/votes", a.requireAuth(a.vote))
	mux.HandleFunc("GET /api/channels/{id}/live-games", a.requireAuth(a.listLiveGames))
	mux.HandleFunc("POST /api/channels/{id}/games", a.requireAuth(a.createGame))
	mux.HandleFunc("GET /api/channels/{id}/watch-party", a.requireAuth(a.getWatchParty))
	mux.HandleFunc("POST /api/channels/{id}/watch-party/items", a.requireAuth(a.addWatchPartyItem))
	mux.HandleFunc("PATCH /api/channels/{id}/watch-party/items/{itemId}", a.requireAuth(a.updateWatchPartyItem))
	mux.HandleFunc("DELETE /api/channels/{id}/watch-party/items/{itemId}", a.requireAuth(a.deleteWatchPartyItem))
	mux.HandleFunc("PUT /api/channels/{id}/watch-party/queue/reorder", a.requireAuth(a.reorderWatchPartyQueue))
	mux.HandleFunc("DELETE /api/channels/{id}/watch-party/queue", a.requireAuth(a.clearWatchPartyQueue))
	mux.HandleFunc("POST /api/channels/{id}/watch-party/control", a.requireAuth(a.controlWatchParty))
	mux.HandleFunc("GET /api/games/leaderboard", a.requireAuth(a.gameLeaderboard))
	mux.HandleFunc("POST /api/messages/{id}/game", a.requireAuth(a.gameAction))
	mux.HandleFunc("POST /api/messages/{id}/poll/close", a.requireAuth(a.closePoll))
	mux.HandleFunc("POST /api/messages/{id}/poll/options", a.requireAuth(a.addPollOption))
	mux.HandleFunc("POST /api/messages/{id}/remind", a.requireAuth(a.createReminder))
	mux.HandleFunc("GET /api/reminders", a.requireAuth(a.listReminders))
	mux.HandleFunc("PATCH /api/reminders/{id}", a.requireAuth(a.snoozeReminder))
	mux.HandleFunc("DELETE /api/reminders/{id}", a.requireAuth(a.deleteReminder))

	mux.HandleFunc("GET /api/users", a.requireAuth(a.searchUsers))
	mux.HandleFunc("GET /api/gifs", a.requireAuth(a.searchGifs))
	mux.HandleFunc("GET /api/rtc-config", a.requireAuth(a.rtcConfig))

	mux.HandleFunc("GET /api/keys/me", a.requireAuth(a.getMyKey))
	mux.HandleFunc("PUT /api/keys/me", a.requireAuth(a.putMyKey))
	mux.HandleFunc("GET /api/users/{id}/key", a.requireAuth(a.getUserKey))
	mux.HandleFunc("POST /api/dms", a.requireAuth(a.openDM))

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
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-ID")
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
		a.touchDevice(r, user.ID)
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
