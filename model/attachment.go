package model

import "time"

// Attachment ánh xạ bảng attachments.
type Attachment struct {
	ID          int64     `db:"id"           json:"id"`
	MessageID   int64     `db:"message_id"   json:"message_id"`
	StorageKey  string    `db:"storage_key"  json:"-"` // tên file trên đĩa, không lộ ra ngoài ngoài URL
	FileName    string    `db:"file_name"    json:"file_name"`
	ContentType string    `db:"content_type" json:"content_type"`
	Size        int64     `db:"size"         json:"size"`
	Width       int       `db:"width"        json:"width"`
	Height      int       `db:"height"       json:"height"`
	CreatedAt   time.Time `db:"created_at"   json:"created_at"`

	// URL tải file (tương đối với server), tính từ StorageKey.
	URL string `db:"-" json:"url"`
}
