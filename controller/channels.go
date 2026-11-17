package controller

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

// Tên kênh: chữ thường (có dấu tiếng Việt), số, gạch ngang, gạch dưới; 1–40 ký tự.
var channelNameRe = regexp.MustCompile(`^[\p{Ll}\p{Lo}\p{Mn}\p{N}_-]{1,40}$`)

func normalizeChannelName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimLeft(s, "#")
	return strings.Join(strings.Fields(s), "-")
}

type channelRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsPrivate   *bool   `json:"is_private"`
}

// validateChannel trả về tên, mô tả đã chuẩn hoá hoặc câu báo lỗi.
func validateChannel(req channelRequest, cur model.Channel) (string, string, string) {
	name, desc := cur.Name, cur.Description
	if req.Name != nil {
		name = normalizeChannelName(*req.Name)
		if !channelNameRe.MatchString(name) {
			return "", "", "Tên kênh gồm 1–40 ký tự: chữ, số, gạch ngang hoặc gạch dưới."
		}
	}
	if req.Description != nil {
		desc = strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(desc) > 500 {
			return "", "", "Mô tả dài tối đa 500 ký tự."
		}
	}
	return name, desc, ""
}

// loadChannel lấy kênh theo {id}, trả 404 nếu không có.
// Kênh riêng tư mà mình không phải thành viên cũng trả 404, để người ngoài không dò
// được (kể cả đoán id) là kênh đó có tồn tại.
func (a *API) loadChannel(w http.ResponseWriter, r *http.Request) (model.Channel, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return model.Channel{}, false
	}
	ch, err := a.store.GetChannel(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy kênh.")
		return ch, false
	}
	if err != nil {
		internalError(w, r, err)
		return ch, false
	}
	member, err := a.store.IsMember(r.Context(), ch.ID, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return ch, false
	}
	if !member {
		if ch.IsPrivate {
			writeError(w, http.StatusNotFound, "Không tìm thấy kênh.")
			return ch, false
		}
		ch.InviteCode = "" // mã mời chỉ dành cho thành viên
	}
	return ch, true
}

// requireMember: chỉ thành viên mới xem được tin nhắn/thành viên của kênh.
func (a *API) requireMember(w http.ResponseWriter, r *http.Request, channelID int64) bool {
	ok, err := a.store.IsMember(r.Context(), channelID, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "Bạn chưa tham gia kênh này.")
		return false
	}
	return true
}

// requireOwner: chỉ người tạo kênh (đổi tên, xoá kênh, đổi link mời). Không áp dụng cho tin nhắn riêng.
func (a *API) requireOwner(w http.ResponseWriter, r *http.Request, ch model.Channel) bool {
	if ch.IsDM() {
		writeError(w, http.StatusBadRequest, "Không áp dụng cho tin nhắn riêng.")
		return false
	}
	if ch.OwnerID != currentUser(r).ID {
		writeError(w, http.StatusForbidden, "Chỉ người tạo kênh mới làm được việc này.")
		return false
	}
	return true
}

/* ---------- GET /api/channels, /api/channels/discover ---------- */

func (a *API) listMyChannels(w http.ResponseWriter, r *http.Request) {
	chans, err := a.store.ListMyChannels(r.Context(), currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chans)
}

func (a *API) listOtherChannels(w http.ResponseWriter, r *http.Request) {
	chans, err := a.store.ListOtherChannels(r.Context(), currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	for i := range chans {
		chans[i].InviteCode = ""
	}
	writeJSON(w, http.StatusOK, chans)
}

/* ---------- POST /api/channels ---------- */

func (a *API) createChannel(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil {
		writeError(w, http.StatusBadRequest, "Cần đặt tên cho kênh.")
		return
	}
	name, desc, msg := validateChannel(req, model.Channel{})
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	me := currentUser(r)
	ch, err := a.store.CreateChannel(r.Context(), me.ID, name, desc, req.IsPrivate != nil && *req.IsPrivate)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "Đã có kênh tên #"+name+".")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.Subscribe(me.ID, ch.ID)
	a.hub.ToUser(me.ID, mustJSON(channelEvent{Type: "channel.joined", Channel: ch}))
	writeJSON(w, http.StatusCreated, ch)
}

/* ---------- GET/PATCH/DELETE /api/channels/{id} ---------- */

func (a *API) getChannel(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (a *API) updateChannel(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireOwner(w, r, ch) {
		return
	}
	var req channelRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name, desc, msg := validateChannel(req, ch)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	isPrivate := ch.IsPrivate
	if req.IsPrivate != nil {
		isPrivate = *req.IsPrivate
	}
	updated, err := a.store.UpdateChannel(r.Context(), ch.ID, name, desc, isPrivate)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "Đã có kênh tên #"+name+".")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(ch.ID, mustJSON(channelEvent{Type: "channel.updated", Channel: updated}), nil)
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) deleteChannel(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireOwner(w, r, ch) {
		return
	}
	keys, err := a.store.AttachmentKeysForChannel(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.store.DeleteChannel(r.Context(), ch.ID); err != nil {
		internalError(w, r, err)
		return
	}
	for _, k := range keys {
		a.removeUpload(k)
	}
	a.hub.ToChannel(ch.ID, mustJSON(channelIDEvent{Type: "channel.deleted", ChannelID: ch.ID}), nil)
	a.hub.DropChannel(ch.ID)
	w.WriteHeader(http.StatusNoContent)
}

/* ---------- POST /api/channels/{id}/join, /leave ---------- */

// joinChannel: chỉ kênh công khai vào thẳng bằng id được (kênh riêng tư đã bị loadChannel trả 404).
func (a *API) joinChannel(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	a.addMember(w, r, ch)
}

// addMember thêm người dùng hiện tại vào kênh và báo cho mọi người liên quan.
func (a *API) addMember(w http.ResponseWriter, r *http.Request, ch model.Channel) {
	me := currentUser(r)
	added, err := a.store.AddMember(r.Context(), ch.ID, me.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	// Giờ đã là thành viên: lấy lại kênh để có đủ mã mời và số thành viên mới nhất.
	ch, err = a.store.GetChannel(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if added {
		// Báo cho thành viên cũ trước, rồi mới đăng ký nhận tin cho người mới.
		a.hub.ToChannel(ch.ID, mustJSON(memberJoinedEvent{Type: "member.joined", ChannelID: ch.ID, User: me.Public()}), nil)
		a.hub.Subscribe(me.ID, ch.ID)
		a.hub.ToUser(me.ID, mustJSON(channelEvent{Type: "channel.joined", Channel: ch}))
	}
	writeJSON(w, http.StatusOK, ch)
}

/* ---------- Link mời ---------- */

var inviteCodeRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

// loadInvite tìm kênh theo {code} trong link mời.
func (a *API) loadInvite(w http.ResponseWriter, r *http.Request) (model.Channel, bool) {
	code := strings.ToLower(r.PathValue("code"))
	if !inviteCodeRe.MatchString(code) {
		writeError(w, http.StatusNotFound, "Link mời không hợp lệ hoặc đã bị đổi.")
		return model.Channel{}, false
	}
	ch, err := a.store.ChannelByInvite(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Link mời không hợp lệ hoặc đã bị đổi.")
		return ch, false
	}
	if err != nil {
		internalError(w, r, err)
		return ch, false
	}
	return ch, true
}

type invitePreview struct {
	Channel  model.Channel `json:"channel"`
	IsMember bool          `json:"is_member"`
}

// GET /api/invites/{code}: xem trước kênh trước khi bấm tham gia.
func (a *API) previewInvite(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadInvite(w, r)
	if !ok {
		return
	}
	member, err := a.store.IsMember(r.Context(), ch.ID, currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if !member {
		ch.InviteCode = ""
	}
	writeJSON(w, http.StatusOK, invitePreview{Channel: ch, IsMember: member})
}

// POST /api/invites/{code}/join: vào kênh (công khai hoặc riêng tư) bằng link mời.
func (a *API) joinByInvite(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadInvite(w, r)
	if !ok {
		return
	}
	a.addMember(w, r, ch)
}

// POST /api/channels/{id}/invite: chủ kênh tạo link mời mới, link cũ hết hiệu lực.
func (a *API) regenerateInvite(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireOwner(w, r, ch) {
		return
	}
	updated, err := a.store.RegenerateInvite(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(ch.ID, mustJSON(channelEvent{Type: "channel.updated", Channel: updated}), nil)
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) leaveChannel(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	me := currentUser(r)
	if ch.IsDM() {
		writeError(w, http.StatusBadRequest, "Không thể rời cuộc trò chuyện riêng.")
		return
	}
	if ch.OwnerID == me.ID {
		writeError(w, http.StatusBadRequest, "Bạn là người tạo kênh nên không thể rời. Hãy xoá kênh nếu không dùng nữa.")
		return
	}
	removed, err := a.store.RemoveMember(r.Context(), ch.ID, me.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if removed {
		a.hub.Unsubscribe(me.ID, ch.ID)
		a.hub.ToChannel(ch.ID, mustJSON(memberLeftEvent{Type: "member.left", ChannelID: ch.ID, UserID: me.ID}), nil)
		a.hub.ToUser(me.ID, mustJSON(channelIDEvent{Type: "channel.left", ChannelID: ch.ID}))
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ---------- GET /api/channels/{id}/members ---------- */

func (a *API) listMembers(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	members, err := a.store.ListMembers(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

/* ---------- GET /api/channels/{id}/messages?before=&limit= ---------- */

type messagesResponse struct {
	Messages []model.Message `json:"messages"`
	HasMore  bool            `json:"has_more"`
	// Vị trí đã đọc của từng thành viên; chỉ có ở trang mới nhất (không có before).
	Reads []model.ReadState `json:"reads,omitempty"`
}

func (a *API) listMessages(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	msgs, hasMore, err := a.store.ListMessages(r.Context(), ch.ID, before, limit)
	if err != nil {
		internalError(w, r, err)
		return
	}
	res := messagesResponse{Messages: msgs, HasMore: hasMore}
	if before == 0 {
		if res.Reads, err = a.store.ReadStates(r.Context(), ch.ID); err != nil {
			internalError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, res)
}
