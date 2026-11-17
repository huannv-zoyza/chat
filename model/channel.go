package model

import "time"

// Channel ánh xạ bảng channels.
type Channel struct {
	ID          int64  `db:"id"          json:"id"`
	Name        string `db:"name"        json:"name"`
	Description string `db:"description" json:"description"`
	OwnerID     int64  `db:"owner_id"    json:"owner_id"`
	IsPrivate   bool   `db:"is_private"  json:"is_private"`
	// "channel" (kênh thường) hoặc "dm" (tin nhắn riêng giữa hai người).
	Kind string `db:"kind" json:"kind"`
	// Mã trong link mời. Chỉ trả cho thành viên kênh (handler xoá trước khi gửi cho người ngoài).
	InviteCode string    `db:"invite_code" json:"invite_code,omitempty"`
	CreatedAt  time.Time `db:"created_at"  json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"  json:"updated_at"`

	// Các trường tính toán, không phải cột của bảng channels.
	MemberCount int `db:"-" json:"member_count"`
	UnreadCount int `db:"-" json:"unread_count"`
	// Số tin chưa đọc có nhắc tên mình.
	MentionCount int `db:"-" json:"mention_count"`
	// Với tin nhắn riêng: hai người trong cuộc trò chuyện (một người nếu tự nhắn cho mình).
	DMMembers []PublicUser `db:"-" json:"dm_members,omitempty"`
}

const (
	KindChannel = "channel"
	KindDM      = "dm"
)

func (c Channel) IsDM() bool { return c.Kind == KindDM }

// ChannelMember ánh xạ bảng channel_members (khoá chính: channel_id + user_id).
type ChannelMember struct {
	ChannelID         int64     `db:"channel_id"           json:"channel_id"`
	UserID            int64     `db:"user_id"              json:"user_id"`
	JoinedAt          time.Time `db:"joined_at"            json:"joined_at"`
	LastReadMessageID int64     `db:"last_read_message_id" json:"last_read_message_id"`
}

// ReadState: thành viên user_id đã đọc tới tin message_id của kênh.
type ReadState struct {
	UserID    int64       `json:"user_id"`
	MessageID int64       `json:"message_id"`
	User      *PublicUser `json:"user,omitempty"`
}
