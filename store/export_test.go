package store

import "context"

// Lối tắt chỉ dùng trong test: dựng sẵn trạng thái nông trại (cấp, kho, số ô) để kiểm tra luật phần 2.

func (s *Store) FarmSetXPForTest(ctx context.Context, userID, xp int64) error {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO farms (user_id) VALUES (?)`, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE farms SET xp = ? WHERE user_id = ?`, xp, userID)
	return err
}

func (s *Store) FarmSetPlotsForTest(ctx context.Context, userID int64, plots int) error {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO farms (user_id) VALUES (?)`, userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE farms SET plots = ? WHERE user_id = ?`, plots, userID)
	return err
}

func (s *Store) FarmSetStockForTest(ctx context.Context, userID int64, item string, qty int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO farm_stock (user_id, item, qty) VALUES (?, ?, ?)
		ON CONFLICT (user_id, item) DO UPDATE SET qty = excluded.qty`, userID, item, qty)
	return err
}

// FarmCounterForTest: bộ đếm nhiệm vụ key hôm nay (giờ VN theo đồng hồ store) của userID.
func (s *Store) FarmCounterForTest(ctx context.Context, userID int64, key string) (int64, error) {
	return counter(ctx, s.db, userID, vnDay(s.now()), key)
}

// LevelXPForTest: XP tối thiểu để đạt cấp lv.
func LevelXPForTest(lv int) int64 { return levelXP(lv) }

// FarmSetXmasForTest: đặt kẹo và tổng kẹo đã kiếm của userID (sự kiện Giáng sinh).
func (s *Store) FarmSetXmasForTest(ctx context.Context, userID, candy, earned int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO farm_xmas (user_id, candy, earned) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET candy = excluded.candy, earned = excluded.earned`, userID, candy, earned)
	return err
}

// FarmBuildForTest: xây sẵn công trình id (cấp 1) cho userID.
func (s *Store) FarmBuildForTest(ctx context.Context, userID int64, id string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO farm_buildings (user_id, building, level) VALUES (?, ?, 1)`, userID, id)
	return err
}
