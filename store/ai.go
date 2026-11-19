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

// AIMemory: trí nhớ của AI cho kênh (tóm tắt các tin có id <= UptoID). Chưa có thì trả giá trị rỗng.
type AIMemory struct {
	Summary   string
	UptoID    int64
	UpdatedAt time.Time
}

func (s *Store) AIMemory(ctx context.Context, channelID int64) (AIMemory, error) {
	var m AIMemory
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT summary, upto_id, updated_at FROM ai_memory WHERE channel_id = ?`, channelID).
		Scan(&m.Summary, &m.UptoID, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return AIMemory{}, nil
	}
	if err != nil {
		return AIMemory{}, err
	}
	m.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updated)
	return m, nil
}

func (s *Store) SaveAIMemory(ctx context.Context, channelID int64, summary string, uptoID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ai_memory (channel_id, summary, upto_id, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET summary = excluded.summary, upto_id = excluded.upto_id, updated_at = excluded.updated_at`,
		channelID, summary, uptoID, DBTime(time.Now()))
	return err
}

// MessagesRange: tin của kênh (cả trả lời trong luồng) có afterID < id < beforeID và gửi từ since,
// cũ -> mới, lấy tối đa limit tin cũ nhất.
func (s *Store) MessagesRange(ctx context.Context, channelID, afterID, beforeID int64, since time.Time, limit int) ([]model.Message, error) {
	return s.queryMessages(ctx, `SELECT `+messageColumns+messageFrom+`
		WHERE msg.channel_id = ? AND msg.id > ? AND msg.id < ? AND msg.created_at >= ? AND msg.deleted_at IS NULL
		ORDER BY msg.id ASC LIMIT ?`, channelID, afterID, beforeID, DBTime(since), limit)
}
