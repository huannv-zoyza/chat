package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"chat/model"
)

// MessagesSince: tin trong kênh (cả dòng chính lẫn trả lời trong luồng) tạo từ since, có id < beforeID,
// cũ -> mới. Quá limit tin thì lấy limit tin MỚI nhất và trả về số tin cũ hơn bị bỏ.
// (created_at chỉ lưu tới giây nên mốc cuối dùng id, không dùng thời gian.)
func (s *Store) MessagesSince(ctx context.Context, channelID int64, since time.Time, beforeID int64, limit int) ([]model.Message, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM messages WHERE channel_id = ? AND created_at >= ? AND id < ? AND deleted_at IS NULL`,
		channelID, DBTime(since), beforeID).Scan(&total); err != nil {
		return nil, 0, err
	}
	msgs, err := s.queryMessages(ctx, `SELECT `+messageColumns+messageFrom+`
		WHERE msg.channel_id = ? AND msg.created_at >= ? AND msg.id < ? AND msg.deleted_at IS NULL
		ORDER BY msg.id DESC LIMIT ?`, channelID, DBTime(since), beforeID, limit)
	if err != nil {
		return nil, 0, err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, max(0, total-len(msgs)), nil
}

// Caption: mô tả ảnh đã lưu ("" nếu chưa có).
func (s *Store) Caption(ctx context.Context, storageKey string) (string, error) {
	var c string
	err := s.db.QueryRowContext(ctx, `SELECT caption FROM ai_captions WHERE storage_key = ?`, storageKey).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return c, err
}

func (s *Store) SaveCaption(ctx context.Context, storageKey, caption string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_captions (storage_key, caption) VALUES (?, ?)
		ON CONFLICT(storage_key) DO UPDATE SET caption = excluded.caption, created_at = CURRENT_TIMESTAMP`, storageKey, caption)
	return err
}
