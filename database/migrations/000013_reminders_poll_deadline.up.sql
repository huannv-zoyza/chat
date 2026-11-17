-- Nhắc việc: "nhắc tôi về tin này lúc …". Thời gian lưu dạng UTC "YYYY-MM-DD HH:MM:SS"
-- (cùng dạng với CURRENT_TIMESTAMP) để so sánh chuỗi được.
CREATE TABLE IF NOT EXISTS reminders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    message_id INTEGER  NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    remind_at  DATETIME NOT NULL,
    note       TEXT     NOT NULL DEFAULT '',
    fired_at   DATETIME, -- đã nhắc (đến giờ) lúc nào
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_reminders_due ON reminders (remind_at) WHERE fired_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_reminders_user ON reminders (user_id);

-- Bình chọn: hạn chót tự kết thúc, cho người khác thêm lựa chọn.
ALTER TABLE polls ADD COLUMN closes_at DATETIME;
ALTER TABLE polls ADD COLUMN allow_add INTEGER NOT NULL DEFAULT 0;
ALTER TABLE poll_options ADD COLUMN added_by INTEGER;
