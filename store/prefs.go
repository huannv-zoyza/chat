package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// UserPrefs: cấu hình giao diện của người dùng (JSON). Chưa lưu gì thì trả "{}".
func (s *Store) UserPrefs(ctx context.Context, userID int64) (json.RawMessage, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT prefs FROM user_prefs WHERE user_id = ?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// SaveUserPrefs ghi đè toàn bộ cấu hình (nơi gọi tự gộp với bản cũ).
func (s *Store) SaveUserPrefs(ctx context.Context, userID int64, prefs json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_prefs (user_id, prefs) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET prefs = excluded.prefs, updated_at = CURRENT_TIMESTAMP`, userID, string(prefs))
	return err
}
