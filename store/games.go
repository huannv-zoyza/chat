package store

import (
	"context"
	"encoding/json"

	"chat/model"
)

// GameRow: một ván đã lưu (state là JSON của package game).
type GameRow struct {
	MessageID int64
	ChannelID int64
	Kind      string
	State     []byte
}

// CreateGame tạo tin nhắn chứa ván chơi (embed type "game") cùng trạng thái ban đầu.
func (s *Store) CreateGame(ctx context.Context, channelID, userID int64, embed model.Embed, status string, state []byte) (model.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Message{}, err
	}
	defer tx.Rollback()
	e, err := json.Marshal(embed)
	if err != nil {
		return model.Message{}, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO messages (channel_id, user_id, content, embed) VALUES (?, ?, '', ?) RETURNING id`,
		channelID, userID, string(e)).Scan(&id); err != nil {
		return model.Message{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO games (message_id, kind, status, state) VALUES (?, ?, ?, ?)`,
		id, embed.ID, status, string(state)); err != nil {
		return model.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Message{}, err
	}
	return s.GetMessage(ctx, id)
}

// SaveGame ghi trạng thái mới của ván.
func (s *Store) SaveGame(ctx context.Context, messageID int64, status string, state []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE games SET status = ?, state = ?, updated_at = CURRENT_TIMESTAMP WHERE message_id = ?`,
		status, string(state), messageID)
	return err
}

// GetGame đọc ván gắn với tin messageID (ErrNotFound nếu không có hoặc tin đã xoá).
func (s *Store) GetGame(ctx context.Context, messageID int64) (GameRow, error) {
	g := GameRow{MessageID: messageID}
	var state string
	err := s.db.QueryRowContext(ctx, `
		SELECT m.channel_id, g.kind, g.state FROM games g JOIN messages m ON m.id = g.message_id
		WHERE g.message_id = ? AND m.deleted_at IS NULL`, messageID).Scan(&g.ChannelID, &g.Kind, &state)
	g.State = []byte(state)
	return g, notFound(err)
}

// ActiveGames: các ván chưa kết thúc (nạp lại khi server khởi động để chạy tiếp đồng hồ).
func (s *Store) ActiveGames(ctx context.Context) ([]GameRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.message_id, m.channel_id, g.kind, g.state FROM games g JOIN messages m ON m.id = g.message_id
		WHERE g.status <> 'ended' AND m.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GameRow
	for rows.Next() {
		var g GameRow
		var state string
		if err := rows.Scan(&g.MessageID, &g.ChannelID, &g.Kind, &state); err != nil {
			return nil, err
		}
		g.State = []byte(state)
		out = append(out, g)
	}
	return out, rows.Err()
}
