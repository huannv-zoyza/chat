package store

import (
	"context"
	"errors"
	"time"

	"chat/model"
)

// DBTime: dạng thời gian lưu trong SQLite (UTC, giống CURRENT_TIMESTAMP) để so sánh chuỗi được.
func DBTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") }

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return DBTime(*t)
}

var (
	ErrPollNoAdd     = errors.New("poll does not allow adding options")
	ErrPollFull      = errors.New("poll has max options")
	ErrPollDuplicate = errors.New("duplicate option")
)

/* ---------- Bình chọn: thêm lựa chọn, tự kết thúc theo hạn chót ---------- */

// AddPollOption: người tham gia thêm lựa chọn (nếu bình chọn cho phép).
func (s *Store) AddPollOption(ctx context.Context, messageID, userID int64, text string, max int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var allow, closed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT allow_add, closed_at IS NOT NULL OR (closes_at IS NOT NULL AND closes_at <= ?)
		FROM polls WHERE message_id = ?`, DBTime(time.Now()), messageID).Scan(&allow, &closed); err != nil {
		return notFound(err)
	}
	switch {
	case closed:
		return ErrPollClosed
	case !allow:
		return ErrPollNoAdd
	}
	var n, dup int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(lower(text) = lower(?)), 0) FROM poll_options WHERE message_id = ?`,
		text, messageID).Scan(&n, &dup); err != nil {
		return err
	}
	if dup > 0 {
		return ErrPollDuplicate
	}
	if n >= max {
		return ErrPollFull
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO poll_options (message_id, position, text, added_by) VALUES (?, ?, ?, ?)`,
		messageID, n, text, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// CloseDuePolls kết thúc các bình chọn đã tới hạn chót, trả về id tin của chúng.
func (s *Store) CloseDuePolls(ctx context.Context, now time.Time) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		UPDATE polls SET closed_at = closes_at
		WHERE closed_at IS NULL AND closes_at IS NOT NULL AND closes_at <= ?
		RETURNING message_id`, DBTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

/* ---------- Nhắc việc ---------- */

const reminderColumns = `id, message_id, remind_at, note, fired_at, created_at, user_id`

func scanReminder(row scanner) (model.Reminder, int64, error) {
	var r model.Reminder
	var uid int64
	err := row.Scan(&r.ID, &r.MessageID, &r.RemindAt, &r.Note, &r.FiredAt, &r.CreatedAt, &uid)
	return r, uid, err
}

func (s *Store) withMessage(ctx context.Context, r *model.Reminder) {
	if m, err := s.GetMessage(ctx, r.MessageID); err == nil {
		r.Message = &m
	}
}

func (s *Store) CreateReminder(ctx context.Context, userID, messageID int64, at time.Time, note string) (model.Reminder, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx,
		`INSERT INTO reminders (user_id, message_id, remind_at, note) VALUES (?, ?, ?, ?) RETURNING id`,
		userID, messageID, DBTime(at), note).Scan(&id); err != nil {
		return model.Reminder{}, err
	}
	return s.GetReminder(ctx, userID, id)
}

func (s *Store) GetReminder(ctx context.Context, userID, id int64) (model.Reminder, error) {
	r, _, err := scanReminder(s.db.QueryRowContext(ctx,
		`SELECT `+reminderColumns+` FROM reminders WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		return r, notFound(err)
	}
	s.withMessage(ctx, &r)
	return r, nil
}

// ListReminders: nhắc việc của userID (chỉ tin ở kênh mình còn tham gia), sắp tới trước.
func (s *Store) ListReminders(ctx context.Context, userID int64) ([]model.Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.message_id, r.remind_at, r.note, r.fired_at, r.created_at, r.user_id
		FROM reminders r JOIN messages m ON m.id = r.message_id
		JOIN channel_members cm ON cm.channel_id = m.channel_id AND cm.user_id = r.user_id
		WHERE r.user_id = ? ORDER BY r.remind_at LIMIT 200`, userID)
	if err != nil {
		return nil, err
	}
	out := []model.Reminder{}
	for rows.Next() {
		r, _, err := scanReminder(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		s.withMessage(ctx, &out[i])
	}
	return out, nil
}

// RescheduleReminder hẹn lại (hoãn): đặt giờ mới, coi như chưa nhắc.
func (s *Store) RescheduleReminder(ctx context.Context, userID, id int64, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE reminders SET remind_at = ?, fired_at = NULL WHERE id = ? AND user_id = ?`, DBTime(at), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteReminder(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM reminders WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FireDueReminders đánh dấu đã nhắc các nhắc việc tới giờ; trả về user_id -> danh sách.
func (s *Store) FireDueReminders(ctx context.Context, now time.Time) (map[int64][]model.Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `
		UPDATE reminders SET fired_at = ? WHERE fired_at IS NULL AND remind_at <= ?
		RETURNING `+reminderColumns, DBTime(now), DBTime(now))
	if err != nil {
		return nil, err
	}
	out := map[int64][]model.Reminder{}
	for rows.Next() {
		r, uid, err := scanReminder(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out[uid] = append(out[uid], r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, list := range out {
		for i := range list {
			s.withMessage(ctx, &list[i])
		}
	}
	return out, nil
}
