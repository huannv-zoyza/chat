package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"unicode"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

// loadMessage lấy tin theo {id} và kiểm tra người dùng là thành viên kênh chứa tin.
// Không phải thành viên thì trả 404 (không để lộ tin của kênh riêng tư).
func (a *API) loadMessage(w http.ResponseWriter, r *http.Request) (model.Message, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "Không tìm thấy tin nhắn.")
		return model.Message{}, false
	}
	msg, err := a.store.GetMessage(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy tin nhắn.")
		return msg, false
	}
	if err != nil {
		internalError(w, r, err)
		return msg, false
	}
	member, err := a.store.IsMember(r.Context(), msg.ChannelID, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return msg, false
	}
	if !member {
		writeError(w, http.StatusNotFound, "Không tìm thấy tin nhắn.")
		return msg, false
	}
	return msg, true
}

/* ---------- Luồng trả lời ---------- */

type threadResponse struct {
	Parent  model.Message   `json:"parent"`
	Replies []model.Message `json:"replies"`
}

// GET /api/messages/{id}/thread: tin gốc và toàn bộ trả lời.
func (a *API) getThread(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	if msg.ParentID != nil {
		writeError(w, http.StatusBadRequest, "Tin này là một trả lời, không có luồng riêng.")
		return
	}
	replies, err := a.store.ListThread(r.Context(), msg.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	all := append([]model.Message{msg}, replies...)
	if err := a.fillForViewer(r.Context(), currentUser(r).ID, all); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, threadResponse{Parent: all[0], Replies: all[1:]})
}

// validateParent kiểm tra tin gốc của một trả lời: cùng kênh và chính nó không phải trả lời
// (chỉ 1 cấp luồng). Trả về câu báo lỗi, rỗng nếu hợp lệ.
func (a *API) validateParent(ctx context.Context, channelID, parentID int64) (string, error) {
	parent, err := a.store.GetMessage(ctx, parentID)
	if errors.Is(err, store.ErrNotFound) {
		return "Tin nhắn gốc không còn tồn tại.", nil
	}
	if err != nil {
		return "", err
	}
	if parent.ChannelID != channelID {
		return "Tin nhắn gốc không thuộc kênh này.", nil
	}
	if parent.ParentID != nil {
		return "Không trả lời lồng trong luồng được.", nil
	}
	return "", nil
}

// broadcastThread báo cho cả kênh tóm tắt luồng mới (số trả lời, người tham gia) của tin gốc.
func (a *API) broadcastThread(ctx context.Context, channelID, parentID int64) {
	parent, err := a.store.GetMessage(ctx, parentID)
	if err != nil || parent.Thread == nil {
		return
	}
	a.hub.ToChannel(channelID, mustJSON(threadUpdatedEvent{
		Type: "thread.updated", ChannelID: channelID, MessageID: parentID, Thread: *parent.Thread,
	}), nil)
}

/* ---------- Thả cảm xúc ---------- */

// validEmoji: chuỗi ngắn gồm ký hiệu (emoji, kể cả chuỗi ghép ZWJ/màu da), không chữ, số, khoảng trắng.
func validEmoji(s string) bool {
	if s == "" || len(s) > 32 || !utf8.ValidString(s) || utf8.RuneCountInString(s) > 10 {
		return false
	}
	hasSymbol := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsSpace(r), unicode.IsControl(r) && r != 0x200d:
			return false
		case r >= 0x2000:
			hasSymbol = true
		}
	}
	return hasSymbol
}

type reactionRequest struct {
	Emoji string `json:"emoji"`
}

type reactionsResponse struct {
	Reactions []model.Reaction `json:"reactions"`
}

// POST /api/messages/{id}/reactions {emoji}: thả hoặc bỏ cảm xúc (bấm lại để bỏ).
func (a *API) toggleReaction(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	var req reactionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validEmoji(req.Emoji) {
		writeError(w, http.StatusBadRequest, "Cảm xúc không hợp lệ.")
		return
	}
	reactions, err := a.store.ToggleReaction(r.Context(), msg.ID, currentUser(r).ID, req.Emoji)
	if errors.Is(err, store.ErrTooManyReactions) {
		writeError(w, http.StatusBadRequest, "Tin này đã có quá nhiều loại cảm xúc.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(msg.ChannelID, mustJSON(reactionUpdatedEvent{
		Type: "reaction.updated", ChannelID: msg.ChannelID, MessageID: msg.ID, ParentID: msg.ParentID, Reactions: reactions,
	}), nil)
	writeJSON(w, http.StatusOK, reactionsResponse{Reactions: reactions})
}
