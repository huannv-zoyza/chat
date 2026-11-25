package model

import "time"

// WatchPartyItem đại diện cho một bài hát / video YouTube trong phòng nghe chung.
type WatchPartyItem struct {
	ID           int64      `json:"id"`
	ChannelID    int64      `json:"channel_id"`
	VideoID      string     `json:"video_id"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	Author       string     `json:"author"`
	ThumbnailURL string     `json:"thumbnail_url"`
	DurationSec  int        `json:"duration_sec"`
	Position     int        `json:"position"`
	Status       string     `json:"status"` // "playing" | "queued" | "played"
	AddedBy      PublicUser `json:"added_by"`
	CreatedAt    time.Time  `json:"created_at"`
}

// WatchPartyState là snapshot đầy đủ của phòng nghe chung trong một kênh.
type WatchPartyState struct {
	ChannelID       int64            `json:"channel_id"`
	CurrentItem     *WatchPartyItem  `json:"current_item,omitempty"`
	Queue           []WatchPartyItem `json:"queue"`
	History         []WatchPartyItem `json:"history"`
	IsPlaying       bool             `json:"is_playing"`
	AnchorPosSec    float64          `json:"anchor_pos_sec"`
	AnchorUpdatedMs int64            `json:"anchor_updated_ms"`
	ServerNowMs     int64            `json:"server_now_ms"`
	LoopMode        string           `json:"loop_mode"` // "off" | "queue" | "single"
	Listeners       []PublicUser     `json:"listeners"`
	ListenerCount   int              `json:"listener_count"`
	Joined          bool             `json:"joined"`
	Version         int              `json:"version"`
}
