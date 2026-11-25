-- IP tạo tài khoản: giới hạn số tài khoản mỗi IP tạo được trong 24 giờ (chống tạo clone lấy xu).
-- NULL với tài khoản cũ, bot và tài khoản tạo từ chính máy chủ.
ALTER TABLE users ADD COLUMN signup_ip TEXT;
CREATE INDEX IF NOT EXISTS idx_users_signup_ip ON users (signup_ip, created_at);
