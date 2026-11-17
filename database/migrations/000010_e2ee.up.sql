-- Mã hoá đầu cuối cho tin nhắn riêng.
-- user_keys: khoá công khai X25519 của mỗi người + bản sao lưu khoá bí mật đã được khoá
-- bằng mã PIN (Argon2id + XChaCha20-Poly1305, làm trên máy người dùng). Server không có PIN
-- nên không mở được bản sao lưu.
CREATE TABLE IF NOT EXISTS user_keys (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    public_key TEXT     NOT NULL,
    backup     TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Tin đã mã hoá: nội dung (chữ, sticker/GIF, thông tin + khoá của file đính kèm) nằm trong
-- cipher (JSON {v, s, r, n, c}); content để trống, embed NULL.
ALTER TABLE messages ADD COLUMN cipher TEXT;
