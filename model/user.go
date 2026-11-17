package model

import "time"

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusInactive UserStatus = "inactive"
	UserStatusBanned   UserStatus = "banned"
)

// User ánh xạ bảng users. Chỉ trả nguyên struct này cho chính chủ (GET /api/me),
// người khác chỉ thấy PublicUser.
type User struct {
	ID           int64      `db:"id"            json:"id"`
	Username     string     `db:"username"      json:"username"`
	Email        string     `db:"email"         json:"email"`
	PasswordHash string     `db:"password_hash" json:"-"` // không bao giờ trả ra JSON
	DisplayName  string     `db:"display_name"  json:"display_name"`
	Avatar       *string    `db:"avatar"        json:"avatar"` // nullable
	StatusText   string     `db:"status_text"   json:"status_text"`
	Status       UserStatus `db:"status"        json:"status"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"    json:"updated_at"`
}

// PublicUser là thông tin người khác được phép xem (không có email).
type PublicUser struct {
	ID          int64   `json:"id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Avatar      *string `json:"avatar"`
	StatusText  string  `json:"status_text"`
}

func (u User) Public() PublicUser {
	return PublicUser{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Avatar:      u.Avatar,
		StatusText:  u.StatusText,
	}
}
