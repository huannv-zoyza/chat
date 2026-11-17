package controller

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"chat/store"
)

/* ---------- PATCH /api/messages/{id} {content}: sửa tin của mình ---------- */

type editMessageRequest struct {
	Content string `json:"content"`
}

func (a *API) editMessage(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	me := currentUser(r)
	if msg.UserID != me.ID {
		writeError(w, http.StatusForbidden, "Chỉ sửa được tin nhắn của chính bạn.")
		return
	}
	if msg.DeletedAt != nil {
		writeError(w, http.StatusBadRequest, "Tin nhắn đã bị xoá.")
		return
	}
	if msg.Embed != nil {
		writeError(w, http.StatusBadRequest, "Không sửa được sticker hay GIF, hãy xoá rồi gửi lại.")
		return
	}
	var req editMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if utf8.RuneCountInString(content) > maxMessageRunes {
		writeError(w, http.StatusBadRequest, "Tin nhắn dài tối đa 4000 ký tự.")
		return
	}
	if content == "" && len(msg.Attachments) == 0 {
		writeError(w, http.StatusBadRequest, "Nội dung không được để trống. Muốn bỏ tin thì hãy xoá.")
		return
	}
	if content == msg.Content {
		writeJSON(w, http.StatusOK, msg)
		return
	}
	usernames, all := parseMentions(content)
	mentionIDs, err := a.store.ResolveMentions(r.Context(), msg.ChannelID, me.ID, usernames, all)
	if err != nil {
		internalError(w, r, err)
		return
	}
	updated, err := a.store.EditMessage(r.Context(), msg.ID, content, mentionIDs)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "Tin nhắn đã bị xoá.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(msg.ChannelID, mustJSON(messageUpdatedEvent{Type: "message.updated", Message: updated}), nil)
	writeJSON(w, http.StatusOK, updated)
}

/* ---------- DELETE /api/messages/{id}: người viết, hoặc chủ kênh (không áp dụng tin nhắn riêng) ---------- */

func (a *API) deleteMessage(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	if msg.DeletedAt != nil {
		writeJSON(w, http.StatusOK, msg)
		return
	}
	me := currentUser(r)
	if msg.UserID != me.ID {
		ch, err := a.store.GetChannel(r.Context(), msg.ChannelID)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if ch.IsDM() || ch.OwnerID != me.ID {
			writeError(w, http.StatusForbidden, "Chỉ xoá được tin nhắn của chính bạn.")
			return
		}
	}
	deleted, keys, err := a.store.DeleteMessage(r.Context(), msg.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	for _, k := range keys {
		a.removeUpload(k)
	}
	a.hub.ToChannel(msg.ChannelID, mustJSON(messageUpdatedEvent{Type: "message.updated", Message: deleted}), nil)
	writeJSON(w, http.StatusOK, deleted)
}

/* ---------- GET /api/users?q=: tìm người để nhắn riêng ---------- */

func (a *API) searchUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("q")), "@"))
	if utf8.RuneCountInString(q) > 64 {
		q = string([]rune(q)[:64])
	}
	users, err := a.store.SearchUsers(r.Context(), q, 30)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

/* ---------- POST /api/dms {user_id}: mở (hoặc tạo) cuộc trò chuyện riêng ---------- */

type openDMRequest struct {
	UserID int64 `json:"user_id"`
}

func (a *API) openDM(w http.ResponseWriter, r *http.Request) {
	var req openDMRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	me := currentUser(r)
	other, err := a.store.GetUserByID(r.Context(), req.UserID)
	if errors.Is(err, store.ErrNotFound) || req.UserID <= 0 {
		writeError(w, http.StatusNotFound, "Không tìm thấy người dùng.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	ch, created, err := a.store.OpenDM(r.Context(), me.ID, other.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if created {
		for _, uid := range uniqueIDs(me.ID, other.ID) {
			a.hub.Subscribe(uid, ch.ID)
			a.hub.ToUser(uid, mustJSON(channelEvent{Type: "channel.joined", Channel: ch}))
		}
		writeJSON(w, http.StatusCreated, ch)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func uniqueIDs(a, b int64) []int64 {
	if a == b {
		return []int64{a}
	}
	return []int64{a, b}
}
