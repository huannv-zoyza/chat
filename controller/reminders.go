package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

// reminder.due: tới giờ nhắc (gửi riêng cho người đặt nhắc).
type reminderEvent struct {
	Type     string         `json:"type"`
	Reminder model.Reminder `json:"reminder"`
}

type remindRequest struct {
	At   time.Time `json:"at"` // RFC 3339
	Note string    `json:"note"`
}

func validRemindAt(at time.Time) string {
	now := time.Now()
	switch {
	case at.IsZero():
		return "Chưa chọn thời gian nhắc."
	case at.Before(now.Add(-time.Minute)):
		return "Thời gian nhắc đã qua."
	case at.After(now.AddDate(1, 0, 0)):
		return "Chỉ hẹn nhắc trong vòng một năm."
	}
	return ""
}

/* ---------- POST /api/messages/{id}/remind ---------- */

func (a *API) createReminder(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	var req remindRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if m := validRemindAt(req.At); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > 200 {
		writeError(w, http.StatusBadRequest, "Ghi chú dài tối đa 200 ký tự.")
		return
	}
	rem, err := a.store.CreateReminder(r.Context(), currentUser(r).ID, msg.ID, req.At, note)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, rem)
}

/* ---------- GET /api/reminders, PATCH/DELETE /api/reminders/{id} ---------- */

func (a *API) listReminders(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListReminders(r.Context(), currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func reminderID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// Hoãn: {at} giờ mới.
func (a *API) snoozeReminder(w http.ResponseWriter, r *http.Request) {
	var req remindRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if m := validRemindAt(req.At); m != "" {
		writeError(w, http.StatusBadRequest, m)
		return
	}
	me := currentUser(r)
	err := a.store.RescheduleReminder(r.Context(), me.ID, reminderID(r), req.At)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy nhắc việc.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	rem, err := a.store.GetReminder(r.Context(), me.ID, reminderID(r))
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rem)
}

func (a *API) deleteReminder(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteReminder(r.Context(), currentUser(r).ID, reminderID(r))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy nhắc việc.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ---------- Bộ hẹn giờ nền ---------- */

// RunScheduler mỗi `every` một lần: gửi nhắc việc tới giờ, kết thúc bình chọn hết hạn.
// Chạy tới khi ctx bị huỷ (server tắt).
func (a *API) RunScheduler(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		a.tick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *API) tick(ctx context.Context, now time.Time) {
	a.farmXmasTick(ctx)
	due, err := a.store.FireDueReminders(ctx, now)
	if err != nil {
		log.Printf("scheduler reminders: %v", err)
	}
	for uid, list := range due {
		for _, rem := range list {
			a.hub.ToUser(uid, mustJSON(reminderEvent{Type: "reminder.due", Reminder: rem}))
		}
	}
	ids, err := a.store.CloseDuePolls(ctx, now)
	if err != nil {
		log.Printf("scheduler polls: %v", err)
	}
	for _, id := range ids {
		m, err := a.store.GetMessage(ctx, id)
		if err != nil || m.Poll == nil {
			continue
		}
		a.hub.ToChannel(m.ChannelID, mustJSON(pollUpdatedEvent{
			Type: "poll.updated", ChannelID: m.ChannelID, MessageID: m.ID, Poll: *m.Poll,
		}), nil)
	}
}

// Tick chạy bộ hẹn giờ một lần (dùng trong test).
func (a *API) Tick(now time.Time) { a.tick(context.Background(), now) }
