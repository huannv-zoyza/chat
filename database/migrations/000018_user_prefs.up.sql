-- Cấu hình giao diện riêng của từng người (độ rộng khung luồng…), lưu dạng JSON để thêm khoá mới không cần migration.
CREATE TABLE IF NOT EXISTS user_prefs (
    user_id    INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prefs      TEXT     NOT NULL DEFAULT '{}',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
