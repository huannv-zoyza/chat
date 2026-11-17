package model

import "time"

// Message ánh xạ bảng messages.
type Message struct {
	ID        int64     `db:"id"         json:"id"`
	ChannelID int64     `db:"channel_id" json:"channel_id"`
	UserID    int64     `db:"user_id"    json:"user_id"`
	Content   string    `db:"content"    json:"content"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	// Lần sửa gần nhất (nil nếu chưa sửa).
	EditedAt *time.Time `db:"edited_at" json:"edited_at,omitempty"`
	// Thời điểm xoá (nil nếu chưa xoá). Tin đã xoá không còn nội dung, file, cảm xúc.
	DeletedAt *time.Time `db:"deleted_at" json:"deleted_at,omitempty"`

	// Người gửi, được JOIN sẵn để client không phải tra thêm.
	User *PublicUser `db:"-" json:"user,omitempty"`
	// File/ảnh gửi kèm (bảng attachments).
	Attachments []Attachment `db:"-" json:"attachments"`
	// Id những người được nhắc tên trong tin (bảng message_mentions).
	Mentions []int64 `db:"-" json:"mentions"`
	// Cảm xúc đã thả, nhóm theo emoji, theo thứ tự emoji được thả đầu tiên.
	Reactions []Reaction `db:"-" json:"reactions"`

	// Trả lời trong luồng: id tin gốc và người viết tin gốc (để báo "X đã trả lời bạn").
	ParentID     *int64 `db:"parent_id" json:"parent_id,omitempty"`
	ParentUserID int64  `db:"-"         json:"parent_user_id,omitempty"`
	// Tóm tắt luồng của tin gốc (nil nếu chưa ai trả lời).
	Thread *ThreadSummary `db:"-" json:"thread,omitempty"`
	// Sticker hoặc GIF (cột embed, JSON). Tin có embed thì không có nội dung chữ.
	Embed *Embed `db:"embed" json:"embed,omitempty"`
}

// Embed: sticker (bộ sticker có sẵn trong app) hoặc GIF (từ GIPHY).
type Embed struct {
	Type    string `json:"type"`              // "sticker" | "gif"
	ID      string `json:"id"`                // sticker: "<bộ>/<tên>"; gif: id trên GIPHY
	URL     string `json:"url,omitempty"`     // gif: ảnh động hiển thị trong tin
	Preview string `json:"preview,omitempty"` // gif: ảnh nhỏ (không bắt buộc)
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	Title   string `json:"title,omitempty"`
}

// Reaction: một emoji và những ai đã thả nó.
type Reaction struct {
	Emoji   string  `json:"emoji"`
	UserIDs []int64 `json:"user_ids"`
}

// ThreadSummary hiển thị dưới tin gốc: "3 trả lời", avatar người tham gia, giờ trả lời gần nhất.
type ThreadSummary struct {
	ReplyCount     int       `json:"reply_count"`
	LastReplyAt    time.Time `json:"last_reply_at"`
	ParticipantIDs []int64   `json:"participant_ids"` // tối đa 3, người trả lời gần nhất trước
}
