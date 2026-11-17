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
	// Tin mã hoá đầu cuối (chỉ trong tin nhắn riêng): JSON {v, s, r, n, c}, server không đọc được.
	Cipher string `db:"cipher" json:"cipher,omitempty"`
	// Bình chọn (bảng polls) nếu tin là một cuộc bình chọn.
	Poll *Poll `db:"-" json:"poll,omitempty"`

	// Ghim: thời điểm và người ghim (nil nếu không ghim).
	PinnedAt *time.Time `db:"pinned_at" json:"pinned_at,omitempty"`
	PinnedBy int64      `db:"pinned_by" json:"pinned_by,omitempty"`
	// Trả lời trích dẫn: id tin được trả lời và bản trích ngắn của tin đó.
	ReplyToID *int64         `db:"reply_to_id" json:"reply_to_id,omitempty"`
	ReplyTo   *QuotedMessage `db:"-"           json:"reply_to,omitempty"`
	// Tin được chuyển tiếp từ nơi khác.
	Forwarded bool `db:"forwarded" json:"forwarded,omitempty"`
}

// QuotedMessage: bản trích của tin được trả lời (đủ để hiện một dòng xem trước).
type QuotedMessage struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Content     string `json:"content"`
	Cipher      string `json:"cipher,omitempty"` // tin mã hoá: client tự giải mã để hiện trích
	Embed       *Embed `json:"embed,omitempty"`
	Attachments int    `json:"attachments,omitempty"`
	Poll        string `json:"poll,omitempty"` // câu hỏi nếu là bình chọn
	ParentID    *int64 `json:"parent_id,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`
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

// Poll: bình chọn gắn với một tin nhắn.
type Poll struct {
	Question  string     `json:"question"`
	Multiple  bool       `json:"multiple"`
	Anonymous bool       `json:"anonymous"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	// Hạn chót: tới giờ thì tự kết thúc.
	ClosesAt *time.Time `json:"closes_at,omitempty"`
	// Cho mọi người thêm lựa chọn.
	AllowAdd bool         `json:"allow_add,omitempty"`
	Options  []PollOption `json:"options"`
	// Số người đã bình chọn (một người chọn nhiều đáp án vẫn tính 1).
	Voters int `json:"voters"`
	// Các lựa chọn của chính người đang xem. Chỉ có trong phản hồi riêng cho người đó
	// (sự kiện phát cho cả kênh không có, vì bình chọn ẩn danh không được lộ ai chọn gì).
	MyVotes []int64 `json:"my_votes,omitempty"`
}

type PollOption struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
	// Ai đã chọn (bỏ trống nếu bình chọn ẩn danh).
	VoterIDs []int64 `json:"voter_ids,omitempty"`
}

// Reminder: "nhắc tôi về tin này" của một người.
type Reminder struct {
	ID        int64      `json:"id"`
	MessageID int64      `json:"message_id"`
	RemindAt  time.Time  `json:"remind_at"`
	Note      string     `json:"note,omitempty"`
	FiredAt   *time.Time `json:"fired_at,omitempty"` // đã đến giờ
	CreatedAt time.Time  `json:"created_at"`
	Message   *Message   `json:"message,omitempty"`
}
