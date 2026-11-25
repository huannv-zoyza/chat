package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"chat/model"
)

var (
	ErrWatchPartyItemNotFound = errors.New("watch party item not found")
	ErrInvalidWatchPartyAct   = errors.New("invalid watch party action")
)

func ensureWatchPartyTx(ctx context.Context, tx *sql.Tx, channelID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO watch_parties (channel_id) VALUES (?)`, channelID)
	return err
}

func loadWatchPartyItemsTx(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, channelID int64, status string, orderBy string, limit int) ([]model.WatchPartyItem, error) {
	query := `
		SELECT i.id, i.channel_id, i.video_id, i.url, i.title, i.author, i.thumbnail_url,
		       i.duration_sec, i.position, i.status, i.created_at,
		       u.id, u.username, u.display_name, u.avatar, u.status_text
		FROM watch_party_items i
		JOIN users u ON u.id = i.added_by
		WHERE i.channel_id = ? AND i.status = ?
		ORDER BY ` + orderBy
	args := []any{channelID, status}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WatchPartyItem{}
	for rows.Next() {
		var it model.WatchPartyItem
		if err := rows.Scan(
			&it.ID, &it.ChannelID, &it.VideoID, &it.URL, &it.Title, &it.Author, &it.ThumbnailURL,
			&it.DurationSec, &it.Position, &it.Status, &it.CreatedAt,
			&it.AddedBy.ID, &it.AddedBy.Username, &it.AddedBy.DisplayName, &it.AddedBy.Avatar, &it.AddedBy.StatusText,
		); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func loadWatchPartyStateTx(ctx context.Context, tx *sql.Tx, channelID, nowMs int64) (model.WatchPartyState, error) {
	st := model.WatchPartyState{
		ChannelID:   channelID,
		Queue:       []model.WatchPartyItem{},
		History:     []model.WatchPartyItem{},
		ServerNowMs: nowMs,
		LoopMode:    "off",
		Listeners:   []model.PublicUser{},
		Version:     1,
	}
	var currentID sql.NullInt64
	var isPlaying int
	err := tx.QueryRowContext(ctx, `
		SELECT current_item_id, is_playing, anchor_pos_sec, anchor_updated_ms, loop_mode, version
		FROM watch_parties WHERE channel_id = ?`, channelID).
		Scan(&currentID, &isPlaying, &st.AnchorPosSec, &st.AnchorUpdatedMs, &st.LoopMode, &st.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return model.WatchPartyState{}, err
	}
	st.IsPlaying = isPlaying == 1

	playingList, err := loadWatchPartyItemsTx(ctx, tx, channelID, "playing", "i.id DESC", 1)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	if len(playingList) > 0 {
		cur := playingList[0]
		st.CurrentItem = &cur
	}
	st.Queue, err = loadWatchPartyItemsTx(ctx, tx, channelID, "queued", "i.position ASC, i.id ASC", 0)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	st.History, err = loadWatchPartyItemsTx(ctx, tx, channelID, "played", "i.updated_at DESC, i.id DESC", 10)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	return st, nil
}

// GetWatchParty đọc trạng thái phòng nghe chung của kênh.
func (s *Store) GetWatchParty(ctx context.Context, channelID, nowMs int64) (model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	defer tx.Rollback()
	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	return st, tx.Commit()
}

// AddWatchPartyItem thêm bài YouTube vào phòng (nếu chưa có bài đang phát thì phát làm bài hiện tại, ngược lại đẩy về cuối queue).
func (s *Store) AddWatchPartyItem(ctx context.Context, channelID, addedBy int64, videoID, rawURL, title, author, thumb string, durationSec int, autoPlay bool, nowMs int64) (model.WatchPartyItem, model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WatchPartyItem{}, model.WatchPartyState{}, err
	}
	defer tx.Rollback()
	if err := ensureWatchPartyTx(ctx, tx, channelID); err != nil {
		return model.WatchPartyItem{}, model.WatchPartyState{}, err
	}
	var currentID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT current_item_id FROM watch_parties WHERE channel_id = ?`, channelID).Scan(&currentID); err != nil {
		return model.WatchPartyItem{}, model.WatchPartyState{}, err
	}

	status := "queued"
	pos := 0
	if !currentID.Valid {
		status = "playing"
	} else {
		var maxPos sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(position) FROM watch_party_items WHERE channel_id = ? AND status = 'queued'`, channelID).Scan(&maxPos); err != nil {
			return model.WatchPartyItem{}, model.WatchPartyState{}, err
		}
		if maxPos.Valid {
			pos = int(maxPos.Int64) + 1
		}
	}

	var itemID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO watch_party_items (channel_id, video_id, url, title, author, thumbnail_url, duration_sec, position, status, added_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		channelID, videoID, rawURL, title, author, thumb, durationSec, pos, status, addedBy).Scan(&itemID); err != nil {
		return model.WatchPartyItem{}, model.WatchPartyState{}, err
	}

	if status == "playing" {
		playingFlag := 0
		if autoPlay {
			playingFlag = 1
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties
			SET current_item_id = ?, is_playing = ?, anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ?`, itemID, playingFlag, nowMs, channelID); err != nil {
			return model.WatchPartyItem{}, model.WatchPartyState{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties SET version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE channel_id = ?`, channelID); err != nil {
			return model.WatchPartyItem{}, model.WatchPartyState{}, err
		}
	}

	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return model.WatchPartyItem{}, model.WatchPartyState{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WatchPartyItem{}, model.WatchPartyState{}, err
	}

	var added model.WatchPartyItem
	if st.CurrentItem != nil && st.CurrentItem.ID == itemID {
		added = *st.CurrentItem
	} else {
		for _, q := range st.Queue {
			if q.ID == itemID {
				added = q
				break
			}
		}
	}
	return added, st, nil
}

// UpdateWatchPartyItem cập nhật tiêu đề, URL/video mới hoặc thời lượng của một bài trong phòng.
func (s *Store) UpdateWatchPartyItem(ctx context.Context, channelID, itemID int64, title *string, newVideo *struct {
	VideoID, URL, Title, Author, Thumb string
}, durationSec *int, nowMs int64) (model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	defer tx.Rollback()

	var curStatus, curTitle, curVideoID, curURL, curAuthor, curThumb string
	var curDur int
	err = tx.QueryRowContext(ctx, `
		SELECT status, title, video_id, url, author, thumbnail_url, duration_sec
		FROM watch_party_items WHERE id = ? AND channel_id = ?`, itemID, channelID).
		Scan(&curStatus, &curTitle, &curVideoID, &curURL, &curAuthor, &curThumb, &curDur)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WatchPartyState{}, ErrWatchPartyItemNotFound
	}
	if err != nil {
		return model.WatchPartyState{}, err
	}

	videoChanged := false
	if newVideo != nil && newVideo.VideoID != "" {
		if newVideo.VideoID != curVideoID {
			videoChanged = true
		}
		curVideoID = newVideo.VideoID
		curURL = newVideo.URL
		curAuthor = newVideo.Author
		curThumb = newVideo.Thumb
		if title == nil && newVideo.Title != "" {
			curTitle = newVideo.Title
		}
	}
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if trimmed != "" {
			curTitle = trimmed
		}
	}
	if durationSec != nil && *durationSec > 0 {
		curDur = *durationSec
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE watch_party_items
		SET title = ?, video_id = ?, url = ?, author = ?, thumbnail_url = ?, duration_sec = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND channel_id = ?`,
		curTitle, curVideoID, curURL, curAuthor, curThumb, curDur, itemID, channelID); err != nil {
		return model.WatchPartyState{}, err
	}

	if curStatus == "playing" && videoChanged {
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties SET anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ?`, nowMs, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties SET version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE channel_id = ?`, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	}

	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	return st, tx.Commit()
}

func promoteNextQueuedTx(ctx context.Context, tx *sql.Tx, channelID, nowMs int64, keepPlaying bool) error {
	var nextID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM watch_party_items
		WHERE channel_id = ? AND status = 'queued'
		ORDER BY position ASC, id ASC LIMIT 1`, channelID).Scan(&nextID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `
			UPDATE watch_parties
			SET current_item_id = NULL, is_playing = 0, anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ?`, nowMs, channelID)
		return err
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE watch_party_items SET status = 'playing', position = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, nextID); err != nil {
		return err
	}
	playingFlag := 0
	if keepPlaying {
		playingFlag = 1
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE watch_parties
		SET current_item_id = ?, is_playing = ?, anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE channel_id = ?`, nextID, playingFlag, nowMs, channelID); err != nil {
		return err
	}
	return normalizeQueuedPositionsTx(ctx, tx, channelID)
}

func normalizeQueuedPositionsTx(ctx context.Context, tx *sql.Tx, channelID int64) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM watch_party_items WHERE channel_id = ? AND status = 'queued' ORDER BY position ASC, id ASC`, channelID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE watch_party_items SET position = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteWatchPartyItem xoá một bài khỏi phòng; nếu xoá đúng bài đang phát thì tự chuyển sang bài kế tiếp.
func (s *Store) DeleteWatchPartyItem(ctx context.Context, channelID, itemID, nowMs int64, hasListeners bool) (model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	defer tx.Rollback()

	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM watch_party_items WHERE id = ? AND channel_id = ?`, itemID, channelID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WatchPartyState{}, ErrWatchPartyItemNotFound
	}
	if err != nil {
		return model.WatchPartyState{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM watch_party_items WHERE id = ? AND channel_id = ?`, itemID, channelID); err != nil {
		return model.WatchPartyState{}, err
	}

	if status == "playing" {
		if err := promoteNextQueuedTx(ctx, tx, channelID, nowMs, hasListeners); err != nil {
			return model.WatchPartyState{}, err
		}
	} else {
		if err := normalizeQueuedPositionsTx(ctx, tx, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties SET version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE channel_id = ?`, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	}

	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	return st, tx.Commit()
}

// ReorderWatchPartyQueue sắp xếp lại thứ tự các bài đang chờ trong hàng đợi.
func (s *Store) ReorderWatchPartyQueue(ctx context.Context, channelID int64, itemIDs []int64, nowMs int64) (model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM watch_party_items WHERE channel_id = ? AND status = 'queued' ORDER BY position ASC, id ASC`, channelID)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	existing := make(map[int64]bool)
	var existingOrder []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return model.WatchPartyState{}, err
		}
		existing[id] = true
		existingOrder = append(existingOrder, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return model.WatchPartyState{}, err
	}

	seen := make(map[int64]bool, len(itemIDs))
	finalOrder := make([]int64, 0, len(existingOrder))
	for _, id := range itemIDs {
		if existing[id] && !seen[id] {
			seen[id] = true
			finalOrder = append(finalOrder, id)
		}
	}
	// Giữ lại các bài mới thêm đồng thời chưa kịp có trong danh sách client gửi lên
	for _, id := range existingOrder {
		if !seen[id] {
			finalOrder = append(finalOrder, id)
		}
	}

	for i, id := range finalOrder {
		if _, err := tx.ExecContext(ctx, `UPDATE watch_party_items SET position = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, i, id); err != nil {
			return model.WatchPartyState{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE watch_parties SET version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE channel_id = ?`, channelID); err != nil {
		return model.WatchPartyState{}, err
	}

	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	return st, tx.Commit()
}

// ClearWatchPartyQueue xoá sạch danh sách bài đang chờ (và tuỳ chọn dừng/xoá bài đang phát).
func (s *Store) ClearWatchPartyQueue(ctx context.Context, channelID int64, includeCurrent bool, nowMs int64) (model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	defer tx.Rollback()

	if includeCurrent {
		if _, err := tx.ExecContext(ctx, `DELETE FROM watch_party_items WHERE channel_id = ? AND status IN ('queued', 'playing')`, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties
			SET current_item_id = NULL, is_playing = 0, anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ?`, nowMs, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM watch_party_items WHERE channel_id = ? AND status = 'queued'`, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties SET version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE channel_id = ?`, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	}

	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	return st, tx.Commit()
}

func currentAnchoredPos(isPlaying int, anchorPos float64, anchorMs, nowMs int64) float64 {
	if isPlaying != 1 || anchorMs <= 0 || nowMs <= anchorMs {
		return max(0, anchorPos)
	}
	return max(0, anchorPos+float64(nowMs-anchorMs)/1000.0)
}

func advanceCurrentLockedTx(ctx context.Context, tx *sql.Tx, channelID int64, loopMode string, currentID int64, nowMs int64, keepPlaying bool) error {
	if loopMode == "single" {
		playingFlag := 0
		if keepPlaying {
			playingFlag = 1
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE watch_parties
			SET is_playing = ?, anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ?`, playingFlag, nowMs, channelID)
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE watch_party_items SET status = 'played', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, currentID); err != nil {
		return err
	}

	var queuedCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM watch_party_items WHERE channel_id = ? AND status = 'queued'`, channelID).Scan(&queuedCount); err != nil {
		return err
	}
	if queuedCount == 0 && loopMode == "queue" {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM watch_party_items WHERE channel_id = ? AND status = 'played' ORDER BY id ASC`, channelID)
		if err != nil {
			return err
		}
		var playedIDs []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			playedIDs = append(playedIDs, id)
		}
		rows.Close()
		for i, id := range playedIDs {
			if _, err := tx.ExecContext(ctx, `UPDATE watch_party_items SET status = 'queued', position = ? WHERE id = ?`, i, id); err != nil {
				return err
			}
		}
	}

	return promoteNextQueuedTx(ctx, tx, channelID, nowMs, keepPlaying)
}

// ControlWatchParty xử lý các hành động phát nhạc: play, pause, seek, next, prev, loop.
func (s *Store) ControlWatchParty(ctx context.Context, channelID int64, action string, posSec *float64, loopMode string, nowMs int64, hasListeners bool) (model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	defer tx.Rollback()
	if err := ensureWatchPartyTx(ctx, tx, channelID); err != nil {
		return model.WatchPartyState{}, err
	}

	var currentID sql.NullInt64
	var isPlaying int
	var anchorPos float64
	var anchorMs int64
	var curLoop string
	if err := tx.QueryRowContext(ctx, `
		SELECT current_item_id, is_playing, anchor_pos_sec, anchor_updated_ms, loop_mode
		FROM watch_parties WHERE channel_id = ?`, channelID).
		Scan(&currentID, &isPlaying, &anchorPos, &anchorMs, &curLoop); err != nil {
		return model.WatchPartyState{}, err
	}

	switch action {
	case "play":
		if !currentID.Valid {
			if err := promoteNextQueuedTx(ctx, tx, channelID, nowMs, true); err != nil {
				return model.WatchPartyState{}, err
			}
		} else {
			pos := currentAnchoredPos(isPlaying, anchorPos, anchorMs, nowMs)
			if posSec != nil && *posSec >= 0 {
				pos = *posSec
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE watch_parties
				SET is_playing = 1, anchor_pos_sec = ?, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
				WHERE channel_id = ?`, pos, nowMs, channelID); err != nil {
				return model.WatchPartyState{}, err
			}
		}
	case "pause":
		pos := currentAnchoredPos(isPlaying, anchorPos, anchorMs, nowMs)
		if posSec != nil && *posSec >= 0 {
			pos = *posSec
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties
			SET is_playing = 0, anchor_pos_sec = ?, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ?`, pos, nowMs, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	case "seek":
		pos := 0.0
		if posSec != nil && *posSec >= 0 {
			pos = *posSec
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties
			SET anchor_pos_sec = ?, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
			WHERE channel_id = ?`, pos, nowMs, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	case "next":
		if currentID.Valid {
			if err := advanceCurrentLockedTx(ctx, tx, channelID, curLoop, currentID.Int64, nowMs, hasListeners); err != nil {
				return model.WatchPartyState{}, err
			}
		} else {
			if err := promoteNextQueuedTx(ctx, tx, channelID, nowMs, hasListeners); err != nil {
				return model.WatchPartyState{}, err
			}
		}
	case "prev":
		var prevID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM watch_party_items WHERE channel_id = ? AND status = 'played' ORDER BY updated_at DESC, id DESC LIMIT 1`, channelID).Scan(&prevID)
		if errors.Is(err, sql.ErrNoRows) {
			// Không có bài trước -> tua về 0
			if _, err := tx.ExecContext(ctx, `
				UPDATE watch_parties SET anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
				WHERE channel_id = ?`, nowMs, channelID); err != nil {
				return model.WatchPartyState{}, err
			}
		} else if err != nil {
			return model.WatchPartyState{}, err
		} else {
			if currentID.Valid {
				if _, err := tx.ExecContext(ctx, `
					UPDATE watch_party_items SET position = position + 1 WHERE channel_id = ? AND status = 'queued'`, channelID); err != nil {
					return model.WatchPartyState{}, err
				}
				if _, err := tx.ExecContext(ctx, `
					UPDATE watch_party_items SET status = 'queued', position = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, currentID.Int64); err != nil {
					return model.WatchPartyState{}, err
				}
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE watch_party_items SET status = 'playing', position = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, prevID); err != nil {
				return model.WatchPartyState{}, err
			}
			playingFlag := 0
			if hasListeners {
				playingFlag = 1
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE watch_parties
				SET current_item_id = ?, is_playing = ?, anchor_pos_sec = 0, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
				WHERE channel_id = ?`, prevID, playingFlag, nowMs, channelID); err != nil {
				return model.WatchPartyState{}, err
			}
		}
	case "loop":
		if loopMode != "off" && loopMode != "queue" && loopMode != "single" {
			return model.WatchPartyState{}, ErrInvalidWatchPartyAct
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE watch_parties SET loop_mode = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE channel_id = ?`,
			loopMode, channelID); err != nil {
			return model.WatchPartyState{}, err
		}
	default:
		return model.WatchPartyState{}, ErrInvalidWatchPartyAct
	}

	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return model.WatchPartyState{}, err
	}
	return st, tx.Commit()
}

// AdvanceWatchPartyIfEnded chuyển sang bài tiếp theo một cách idempotent nếu itemID đúng là bài đang phát.
func (s *Store) AdvanceWatchPartyIfEnded(ctx context.Context, channelID, itemID, nowMs int64, hasListeners bool) (bool, model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, model.WatchPartyState{}, err
	}
	defer tx.Rollback()

	var currentID sql.NullInt64
	var loopMode string
	err = tx.QueryRowContext(ctx, `SELECT current_item_id, loop_mode FROM watch_parties WHERE channel_id = ?`, channelID).
		Scan(&currentID, &loopMode)
	if errors.Is(err, sql.ErrNoRows) {
		return false, model.WatchPartyState{}, nil
	}
	if err != nil {
		return false, model.WatchPartyState{}, err
	}
	if !currentID.Valid || currentID.Int64 != itemID {
		st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
		return false, st, err
	}

	if err := advanceCurrentLockedTx(ctx, tx, channelID, loopMode, itemID, nowMs, hasListeners); err != nil {
		return false, model.WatchPartyState{}, err
	}
	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return false, model.WatchPartyState{}, err
	}
	return true, st, tx.Commit()
}

// PauseWatchPartyIfPlaying tạm dừng và đóng băng anchor_pos_sec khi người nghe cuối rời phòng.
func (s *Store) PauseWatchPartyIfPlaying(ctx context.Context, channelID, nowMs int64) (bool, model.WatchPartyState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, model.WatchPartyState{}, err
	}
	defer tx.Rollback()

	var isPlaying int
	var anchorPos float64
	var anchorMs int64
	err = tx.QueryRowContext(ctx, `
		SELECT is_playing, anchor_pos_sec, anchor_updated_ms FROM watch_parties WHERE channel_id = ?`, channelID).
		Scan(&isPlaying, &anchorPos, &anchorMs)
	if errors.Is(err, sql.ErrNoRows) || isPlaying == 0 {
		st, lErr := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
		return false, st, lErr
	}
	if err != nil {
		return false, model.WatchPartyState{}, err
	}
	pos := currentAnchoredPos(isPlaying, anchorPos, anchorMs, nowMs)
	if _, err := tx.ExecContext(ctx, `
		UPDATE watch_parties SET is_playing = 0, anchor_pos_sec = ?, anchor_updated_ms = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE channel_id = ?`, pos, nowMs, channelID); err != nil {
		return false, model.WatchPartyState{}, err
	}
	st, err := loadWatchPartyStateTx(ctx, tx, channelID, nowMs)
	if err != nil {
		return false, model.WatchPartyState{}, err
	}
	return true, st, tx.Commit()
}
