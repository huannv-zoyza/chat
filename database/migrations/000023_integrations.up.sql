-- Tích hợp bot theo kênh: mỗi bot là một tài khoản riêng (tên đuôi "_bot", không đăng nhập được).
-- token_hash: băm của mã bí mật trong link webhook nhận tin (chỉ hiện một lần lúc tạo / đổi mã).
-- signing_secret: khoá ký HMAC cho tin gửi đi outgoing_url (bên nhận dùng để kiểm tra).
CREATE TABLE IF NOT EXISTS integrations (
    id             INTEGER  PRIMARY KEY AUTOINCREMENT,
    channel_id     INTEGER  NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    bot_user_id    INTEGER  NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    token_hash     CHAR(64) NOT NULL UNIQUE,
    signing_secret TEXT     NOT NULL,
    outgoing_url   TEXT     NOT NULL DEFAULT '',
    created_by     INTEGER  REFERENCES users (id) ON DELETE SET NULL,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS integrations_channel ON integrations (channel_id);
