-- Bình chọn: một tin nhắn (message_id) có một câu hỏi và 2–10 lựa chọn.
CREATE TABLE IF NOT EXISTS polls (
    message_id INTEGER PRIMARY KEY REFERENCES messages (id) ON DELETE CASCADE,
    question   TEXT    NOT NULL,
    multiple   INTEGER NOT NULL DEFAULT 0, -- cho chọn nhiều đáp án
    anonymous  INTEGER NOT NULL DEFAULT 0, -- không cho ai thấy ai đã chọn gì
    closed_at  DATETIME                    -- khác NULL: đã kết thúc, không nhận phiếu
);

CREATE TABLE IF NOT EXISTS poll_options (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES polls (message_id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    text       TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_poll_options_message ON poll_options (message_id, position);

CREATE TABLE IF NOT EXISTS poll_votes (
    option_id  INTEGER  NOT NULL REFERENCES poll_options (id) ON DELETE CASCADE,
    user_id    INTEGER  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (option_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_poll_votes_user ON poll_votes (user_id);
