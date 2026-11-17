-- Phiên đăng nhập: chỉ lưu SHA-256 của token, không lưu token gốc.
CREATE TABLE IF NOT EXISTS sessions (
    token_hash CHAR(64)  PRIMARY KEY,
    user_id    INTEGER   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);

-- Cấu hình hồ sơ người dùng
ALTER TABLE users ADD COLUMN display_name VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN status_text  VARCHAR(140) NOT NULL DEFAULT '';

-- Mô tả kênh, tên kênh là duy nhất
ALTER TABLE channels ADD COLUMN description VARCHAR(500) NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_name ON channels (name);

-- Đánh dấu đã đọc tới tin nào, để tính số tin chưa đọc
ALTER TABLE channel_members ADD COLUMN last_read_message_id INTEGER NOT NULL DEFAULT 0;
