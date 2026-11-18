package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

// validateReplyTo: tin được trả lời trích dẫn phải cùng kênh và còn tồn tại.
// raw rỗng = không trả lời. Trả về id, câu báo lỗi (rỗng nếu hợp lệ).
func (a *API) validateReplyTo(ctx context.Context, channelID int64, raw string) (*int64, string, error) {
	if raw == "" || raw == "0" {
		return nil, "", nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil, "Tin được trả lời không hợp lệ.", nil
	}
	m, err := a.store.GetMessage(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "Tin được trả lời không còn tồn tại.", nil
	}
	if err != nil {
		return nil, "", err
	}
	if m.ChannelID != channelID {
		return nil, "Tin được trả lời không thuộc kênh này.", nil
	}
	return &id, "", nil
}

/* ---------- POST /api/messages/{id}/pin {pinned}, GET /api/channels/{id}/pins ---------- */

type pinRequest struct {
	Pinned bool `json:"pinned"`
}

// Thành viên nào cũng ghim/bỏ ghim được (như Slack); mọi người thấy ai đã ghim.
func (a *API) pinMessage(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	if msg.DeletedAt != nil {
		writeError(w, http.StatusBadRequest, "Tin nhắn đã bị xoá.")
		return
	}
	var req pinRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	by := int64(0)
	if req.Pinned {
		by = currentUser(r).ID
	}
	if err := a.store.SetPinned(r.Context(), msg.ID, by); err != nil {
		internalError(w, r, err)
		return
	}
	updated, err := a.store.GetMessage(r.Context(), msg.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(msg.ChannelID, mustJSON(messageUpdatedEvent{Type: "message.updated", Message: updated}), nil)
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) listPins(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	msgs, err := a.store.ListPinned(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.fillForViewer(r.Context(), currentUser(r).ID, msgs); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

/* ---------- GET /api/search?q=&channel_id=&before= ---------- */

type searchResponse struct {
	Messages []model.Message `json:"messages"`
	HasMore  bool            `json:"has_more"`
}

const searchPage = 30

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 100 {
		q = string([]rune(q)[:100])
	}
	channelID, _ := strconv.ParseInt(r.URL.Query().Get("channel_id"), 10, 64)
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	me := currentUser(r)
	msgs, err := a.store.SearchMessages(r.Context(), me.ID, q, channelID, before, searchPage+1)
	if err != nil {
		internalError(w, r, err)
		return
	}
	res := searchResponse{Messages: msgs}
	if len(msgs) > searchPage {
		res.Messages, res.HasMore = msgs[:searchPage], true
	}
	if err := a.fillForViewer(r.Context(), me.ID, res.Messages); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
