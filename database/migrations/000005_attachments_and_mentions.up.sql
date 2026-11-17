-- File/ảnh gửi kèm tin nhắn. File thật nằm trên đĩa (thư mục uploads), tên là storage_key.
CREATE TABLE IF NOT EXISTS attachments (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id   INTEGER      NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    storage_key  CHAR(32)     NOT NULL UNIQUE,
    file_name    VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    size         INTEGER      NOT NULL,
    width        INTEGER      NOT NULL DEFAULT 0, -- kích thước ảnh, 0 nếu không phải ảnh
    height       INTEGER      NOT NULL DEFAULT 0,
    created_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_attachments_message_id ON attachments (message_id);

-- Ai được nhắc tên (@username, @all) trong tin nào, để báo và đếm số lần được nhắc chưa đọc.
CREATE TABLE IF NOT EXISTS message_mentions (
    message_id INTEGER NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users (id)    ON DELETE CASCADE,
    PRIMARY KEY (message_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_message_mentions_user_id ON message_mentions (user_id, message_id);
