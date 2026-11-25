package store

import (
	"context"
	"database/sql"
	"errors"

	"chat/model"
)

// Máy (thiết bị) của tài khoản: app tự sinh một mã ngẫu nhiên, lưu trong máy và gửi kèm mọi request.
// Mã này người dùng xoá dữ liệu app là đổi được, nên chỉ để làm khó việc tạo tài khoản ảo gom xu,
// không phải bằng chứng chắc chắn.

// ErrDeviceLimit: máy này đã tạo đủ số tài khoản cho phép.
var ErrDeviceLimit = errors.New("too many accounts from this device")

// Signup: thông tin nơi tạo tài khoản. Chuỗi rỗng là không biết (app cũ, gọi từ máy chủ).
type Signup struct {
	IP     string
	Device string
	// MaxPerDevice > 0: máy Device đã tạo đủ chừng này tài khoản thì ErrDeviceLimit.
	MaxPerDevice int
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// RegisterUser tạo tài khoản như CreateUser, ghi lại IP và máy tạo. Đếm số tài khoản của máy và chèn
// trong cùng một câu lệnh nên hai request song song không cùng lọt qua giới hạn.
func (s *Store) RegisterUser(ctx context.Context, username, email, passwordHash, displayName string, at Signup) (model.User, error) {
	limit := at.MaxPerDevice
	if at.Device == "" || limit <= 0 {
		limit = -1 // không giới hạn
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback()
	u, err := scanUser(tx.QueryRowContext(ctx, `
		INSERT INTO users (username, email, password_hash, display_name, signup_ip, signup_device)
		SELECT ?, ?, ?, ?, ?, ?
		WHERE ? < 0 OR (SELECT COUNT(*) FROM users WHERE signup_device = ?) < ?
		RETURNING `+userColumns,
		username, email, passwordHash, displayName, nullStr(at.IP), nullStr(at.Device),
		limit, at.Device, limit))
	switch {
	case isUniqueViolation(err):
		return u, ErrConflict
	case errors.Is(err, sql.ErrNoRows):
		return u, ErrDeviceLimit
	case err != nil:
		return u, err
	}
	if at.Device != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO user_devices (user_id, device_id) VALUES (?, ?)`, u.ID, at.Device); err != nil {
			return u, err
		}
	}
	return u, tx.Commit()
}

// TouchDevice ghi nhận userID vừa dùng máy deviceID.
func (s *Store) TouchDevice(ctx context.Context, userID int64, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_devices (user_id, device_id) VALUES (?, ?)
		ON CONFLICT (user_id, device_id) DO UPDATE SET last_seen = CURRENT_TIMESTAMP`, userID, deviceID)
	return err
}

// SharesDevice: hai tài khoản từng dùng chung ít nhất một máy.
func (s *Store) SharesDevice(ctx context.Context, a, b int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_devices x JOIN user_devices y ON x.device_id = y.device_id
			WHERE x.user_id = ? AND y.user_id = ?)`, a, b).Scan(&n)
	return n == 1, err
}
