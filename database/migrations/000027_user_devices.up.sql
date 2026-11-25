-- Máy (thiết bị) mỗi tài khoản đã dùng, nhận diện bằng mã ngẫu nhiên app lưu trong máy.
-- Dùng để giới hạn số tài khoản tạo trên một máy và chặn chuyển xu giữa các tài khoản dùng chung máy
-- (chống tạo tài khoản ảo gom xu). signup_device: máy tạo tài khoản (NULL nếu app cũ không gửi).
ALTER TABLE users ADD COLUMN signup_device TEXT;
CREATE INDEX IF NOT EXISTS idx_users_signup_device ON users (signup_device);

CREATE TABLE IF NOT EXISTS user_devices (
    user_id      INTEGER   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_id    TEXT      NOT NULL,
    first_seen   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, device_id)
);
CREATE INDEX IF NOT EXISTS idx_user_devices_device ON user_devices (device_id);
