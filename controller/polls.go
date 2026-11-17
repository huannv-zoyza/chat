package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

const (
	maxPollQuestion = 300
	maxPollOption   = 100
	minPollOptions  = 2
	maxPollOptions  = 10
)

// poll.updated: số phiếu mới của một bình chọn (không có my_votes: phát cho cả kênh).
type pollUpdatedEvent struct {
	Type      string     `json:"type"`
	ChannelID int64      `json:"channel_id"`
	MessageID int64      `json:"message_id"`
	Poll      model.Poll `json:"poll"`
}

/* ---------- POST /api/channels/{id}/polls ---------- */

type createPollRequest struct {
	Question  string   `json:"question"`
	Options   []string `json:"options"`
	Multiple  bool     `json:"multiple"`
	Anonymous bool     `json:"anonymous"`
	ClientID  string   `json:"client_id"`
	// Hạn chót (RFC 3339), không bắt buộc.
	ClosesAt *time.Time `json:"closes_at"`
	// Cho mọi người thêm lựa chọn.
	AllowAdd bool `json:"allow_add"`
}

func validatePoll(req createPollRequest) (store.NewPoll, string) {
	q := strings.TrimSpace(req.Question)
	if q == "" || utf8.RuneCountInString(q) > maxPollQuestion {
		return store.NewPoll{}, "Câu hỏi cần 1–300 ký tự."
	}
	var opts []string
	seen := map[string]bool{}
	for _, o := range req.Options {
		o = strings.Join(strings.Fields(o), " ")
		if o == "" {
			continue
		}
		if utf8.RuneCountInString(o) > maxPollOption {
			return store.NewPoll{}, "Mỗi lựa chọn dài tối đa 100 ký tự."
		}
		k := strings.ToLower(o)
		if seen[k] {
			return store.NewPoll{}, "Có hai lựa chọn giống nhau: “" + o + "”."
		}
		seen[k] = true
		opts = append(opts, o)
	}
	if len(opts) < minPollOptions || len(opts) > maxPollOptions {
		return store.NewPoll{}, "Bình chọn cần từ 2 đến 10 lựa chọn."
	}
	if req.ClosesAt != nil {
		if req.ClosesAt.Before(time.Now().Add(time.Minute)) || req.ClosesAt.After(time.Now().AddDate(0, 3, 0)) {
			return store.NewPoll{}, "Hạn chót phải sau ít nhất 1 phút và trong vòng 3 tháng."
		}
	}
	return store.NewPoll{Question: q, Options: opts, Multiple: req.Multiple, Anonymous: req.Anonymous,
		ClosesAt: req.ClosesAt, AllowAdd: req.AllowAdd}, ""
}

func (a *API) createPoll(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	if ch.IsDM() {
		// tin nhắn riêng được mã hoá đầu cuối, còn bình chọn thì server phải đếm phiếu
		writeError(w, http.StatusBadRequest, "Bình chọn chỉ dùng trong kênh.")
		return
	}
	var req createPollRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	p, msg := validatePoll(req)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if len(req.ClientID) > 64 {
		req.ClientID = req.ClientID[:64]
	}
	me := currentUser(r)
	m, err := a.store.CreatePoll(r.Context(), ch.ID, me.ID, p)
	if err != nil {
		internalError(w, r, err)
		return
	}
	_, _, _ = a.store.MarkRead(r.Context(), ch.ID, me.ID, m.ID)
	a.hub.ToChannel(ch.ID, mustJSON(messageNewEvent{Type: "message.new", Message: m, ClientID: req.ClientID}), nil)
	writeJSON(w, http.StatusCreated, m)
}

/* ---------- POST /api/messages/{id}/votes {option_ids} ---------- */

type voteRequest struct {
	OptionIDs []int64 `json:"option_ids"`
}

func (a *API) vote(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	if msg.Poll == nil || msg.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "Không tìm thấy bình chọn.")
		return
	}
	var req voteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.OptionIDs) > maxPollOptions {
		writeError(w, http.StatusBadRequest, "Lựa chọn không hợp lệ.")
		return
	}
	me := currentUser(r)
	err := a.store.Vote(r.Context(), msg.ID, me.ID, req.OptionIDs)
	switch {
	case errors.Is(err, store.ErrPollClosed):
		writeError(w, http.StatusConflict, "Bình chọn đã kết thúc.")
		return
	case errors.Is(err, store.ErrPollSingleVote):
		writeError(w, http.StatusBadRequest, "Bình chọn này chỉ được chọn một đáp án.")
		return
	case errors.Is(err, store.ErrBadPollOption):
		writeError(w, http.StatusBadRequest, "Lựa chọn không hợp lệ.")
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	a.respondPoll(w, r, msg.ID)
}

/* ---------- POST /api/messages/{id}/poll/options {text} ---------- */

type addOptionRequest struct {
	Text string `json:"text"`
}

func (a *API) addPollOption(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	if msg.Poll == nil || msg.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "Không tìm thấy bình chọn.")
		return
	}
	var req addOptionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	text := strings.Join(strings.Fields(req.Text), " ")
	if text == "" || utf8.RuneCountInString(text) > maxPollOption {
		writeError(w, http.StatusBadRequest, "Lựa chọn cần 1–100 ký tự.")
		return
	}
	err := a.store.AddPollOption(r.Context(), msg.ID, currentUser(r).ID, text, maxPollOptions)
	switch {
	case errors.Is(err, store.ErrPollClosed):
		writeError(w, http.StatusConflict, "Bình chọn đã kết thúc.")
		return
	case errors.Is(err, store.ErrPollNoAdd):
		writeError(w, http.StatusForbidden, "Bình chọn này không cho thêm lựa chọn.")
		return
	case errors.Is(err, store.ErrPollDuplicate):
		writeError(w, http.StatusBadRequest, "Đã có lựa chọn này rồi.")
		return
	case errors.Is(err, store.ErrPollFull):
		writeError(w, http.StatusBadRequest, "Đã đủ 10 lựa chọn.")
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	a.respondPoll(w, r, msg.ID)
}

/* ---------- POST /api/messages/{id}/poll/close {closed} ---------- */

type closePollRequest struct {
	Closed bool `json:"closed"`
}

func (a *API) closePoll(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	if msg.Poll == nil || msg.DeletedAt != nil {
		writeError(w, http.StatusNotFound, "Không tìm thấy bình chọn.")
		return
	}
	if msg.UserID != currentUser(r).ID {
		writeError(w, http.StatusForbidden, "Chỉ người tạo bình chọn mới kết thúc được.")
		return
	}
	var req closePollRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := a.store.SetPollClosed(r.Context(), msg.ID, req.Closed); err != nil {
		internalError(w, r, err)
		return
	}
	a.respondPoll(w, r, msg.ID)
}

// respondPoll phát số phiếu mới cho cả kênh và trả bình chọn (kèm my_votes) cho người gọi.
func (a *API) respondPoll(w http.ResponseWriter, r *http.Request, messageID int64) {
	m, err := a.store.GetMessage(r.Context(), messageID)
	if err != nil || m.Poll == nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(m.ChannelID, mustJSON(pollUpdatedEvent{
		Type: "poll.updated", ChannelID: m.ChannelID, MessageID: m.ID, Poll: *m.Poll,
	}), nil)
	msgs := []model.Message{m}
	if err := a.fillMyVotes(r.Context(), currentUser(r).ID, msgs); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs[0].Poll)
}

// fillMyVotes điền lựa chọn của người xem vào các bình chọn (cần cho bình chọn ẩn danh,
// vì danh sách người chọn không được gửi về).
func (a *API) fillMyVotes(ctx context.Context, userID int64, msgs []model.Message) error {
	var ids []int64
	for _, m := range msgs {
		if m.Poll != nil {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	mine, err := a.store.MyPollVotes(ctx, userID, ids)
	if err != nil {
		return err
	}
	for i := range msgs {
		if p := msgs[i].Poll; p != nil {
			p.MyVotes = mine[msgs[i].ID]
			if p.MyVotes == nil {
				p.MyVotes = []int64{}
			}
		}
	}
	return nil
}
