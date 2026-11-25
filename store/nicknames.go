package store

import "context"

// Nicknames: biệt danh trong kênh, userID -> biệt danh.
func (s *Store) Nicknames(ctx context.Context, channelID int64) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, nickname FROM channel_nicknames WHERE channel_id = ?`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var nick string
		if err := rows.Scan(&id, &nick); err != nil {
			return nil, err
		}
		out[id] = nick
	}
	return out, rows.Err()
}

// SetNickname đặt (nickname rỗng: xoá) biệt danh của userID trong kênh.
func (s *Store) SetNickname(ctx context.Context, channelID, userID, by int64, nickname string) error {
	if nickname == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM channel_nicknames WHERE channel_id = ? AND user_id = ?`, channelID, userID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_nicknames (channel_id, user_id, nickname, set_by) VALUES (?, ?, ?, ?)
		ON CONFLICT (channel_id, user_id) DO UPDATE SET nickname = excluded.nickname, set_by = excluded.set_by,
			updated_at = CURRENT_TIMESTAMP`, channelID, userID, nickname, by)
	return err
}
