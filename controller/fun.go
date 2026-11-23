package controller

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"chat/model"
)

// Trêu nhau trong kênh.
//
//	POST /api/channels/{id}/throw               {user_id, item}  ném đồ vào một thành viên (tin "throw")
//	GET  /api/channels/{id}/nicknames                            biệt danh trong kênh {user_id: biệt danh}
//	PUT  /api/channels/{id}/nicknames/{userId}  {nickname}       đặt / xoá biệt danh (tin "nickname")
//
// Đặt biệt danh còn phát nickname.updated {channel_id, user_id, nickname} cho cả kênh.

// throwItems: đồ ném được (id -> tên hiển thị trong câu "ném … vào").
var throwItems = map[string]string{
	"dep":     "dép",
	"cachua":  "cà chua",
	"banhkem": "bánh kem",
	"trung":   "trứng",
	"hoa":     "hoa hồng",
}

const (
	throwCooldown   = 5 * time.Second
	nicknameMaxRune = 32
)

type throwLimiter struct {
	mu   sync.Mutex
	last map[int64]time.Time
}

// allow: userID được ném tiếp chưa (mỗi người một lần mỗi throwCooldown).
func (l *throwLimiter) allow(userID int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		l.last = map[int64]time.Time{}
	}
	if now.Sub(l.last[userID]) < throwCooldown {
		return false
	}
	l.last[userID] = now
	return true
}

// funTarget: người được nhắm tới phải là thành viên kênh.
func (a *API) funTarget(w http.ResponseWriter, r *http.Request, channelID, userID int64) (model.User, bool) {
	ok, err := a.store.IsMember(r.Context(), channelID, userID)
	if err != nil {
		internalError(w, r, err)
		return model.User{}, false
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "Người này không ở trong kênh.")
		return model.User{}, false
	}
	u, err := a.store.GetUserByID(r.Context(), userID)
	if err != nil {
		internalError(w, r, err)
		return model.User{}, false
	}
	return u, true
}

func userName(u model.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (a *API) throwItem(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	var req struct {
		UserID int64  `json:"user_id"`
		Item   string `json:"item"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	me := currentUser(r)
	if _, ok := throwItems[req.Item]; !ok {
		writeError(w, http.StatusBadRequest, "Không có món này để ném.")
		return
	}
	if req.UserID == me.ID {
		writeError(w, http.StatusBadRequest, "Tự ném vào mình thì kỳ lắm.")
		return
	}
	target, ok := a.funTarget(w, r, ch.ID, req.UserID)
	if !ok {
		return
	}
	if !a.throws.allow(me.ID, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "Từ từ thôi, ném chậm lại nào!")
		return
	}
	m, err := a.createAndBroadcast(r.Context(), me, ch.ID, outgoing{
		Embed: &model.Embed{Type: "throw", ID: req.Item, TargetID: target.ID, Author: userName(target)},
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (a *API) listNicknames(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	nicks, err := a.store.Nicknames(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nicks)
}

type nicknameUpdatedEvent struct {
	Type      string `json:"type"`
	ChannelID int64  `json:"channel_id"`
	UserID    int64  `json:"user_id"`
	Nickname  string `json:"nickname"`
}

func (a *API) setNickname(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "Người dùng không hợp lệ.")
		return
	}
	var req struct {
		Nickname string `json:"nickname"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	nick := strings.Join(strings.Fields(req.Nickname), " ")
	if utf8.RuneCountInString(nick) > nicknameMaxRune {
		writeError(w, http.StatusBadRequest, "Biệt danh tối đa 32 ký tự.")
		return
	}
	target, ok := a.funTarget(w, r, ch.ID, userID)
	if !ok {
		return
	}
	current, err := a.store.Nicknames(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if current[userID] == nick {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	me := currentUser(r)
	if err := a.store.SetNickname(r.Context(), ch.ID, userID, me.ID, nick); err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(ch.ID, mustJSON(nicknameUpdatedEvent{
		Type: "nickname.updated", ChannelID: ch.ID, UserID: userID, Nickname: nick,
	}), nil)
	if _, err := a.createAndBroadcast(r.Context(), me, ch.ID, outgoing{
		Embed: &model.Embed{Type: "nickname", TargetID: userID, Author: userName(target), Text: nick},
	}); err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
