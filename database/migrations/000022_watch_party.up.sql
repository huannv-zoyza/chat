-- Phòng Nghe nhạc / Xem video YouTube chung theo kênh và hàng đợi cộng tác.
CREATE TABLE IF NOT EXISTS watch_parties (
    channel_id        INTEGER PRIMARY KEY REFERENCES channels (id) ON DELETE CASCADE,
    current_item_id   INTEGER,
    is_playing        INTEGER  NOT NULL DEFAULT 0,
    anchor_pos_sec    REAL     NOT NULL DEFAULT 0,
    anchor_updated_ms INTEGER  NOT NULL DEFAULT 0,
    loop_mode         TEXT     NOT NULL DEFAULT 'off',
    version           INTEGER  NOT NULL DEFAULT 1,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS watch_party_items (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id    INTEGER  NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    video_id      TEXT     NOT NULL,
    url           TEXT     NOT NULL,
    title         TEXT     NOT NULL,
    author        TEXT     NOT NULL DEFAULT '',
    thumbnail_url TEXT     NOT NULL DEFAULT '',
    duration_sec  INTEGER  NOT NULL DEFAULT 0,
    position      INTEGER  NOT NULL DEFAULT 0,
    status        TEXT     NOT NULL DEFAULT 'queued', -- playing | queued | played
    added_by      INTEGER  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_wp_items_channel_status_pos
    ON watch_party_items (channel_id, status, position, id);
