-- Kết đôi trong nông trại (specs/FARM_SPEC.md §11). Mỗi cặp một dòng, user_a < user_b; mỗi người chỉ ở
-- một cặp (code kiểm tra trong transaction). love: điểm thân mật tích luỹ; since: giây unix lúc thành đôi.
CREATE TABLE IF NOT EXISTS farm_couples (
    user_a INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_b INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    love   INTEGER NOT NULL DEFAULT 0,
    since  INTEGER NOT NULL,
    PRIMARY KEY (user_a, user_b),
    CHECK (user_a < user_b)
);
CREATE UNIQUE INDEX IF NOT EXISTS farm_couples_a ON farm_couples (user_a);
CREATE UNIQUE INDEX IF NOT EXISTS farm_couples_b ON farm_couples (user_b);

-- Lời ngỏ kết đôi đang chờ trả lời. Mỗi người chỉ có một lời ngỏ gửi đi (ngỏ người mới thì thay lời cũ).
CREATE TABLE IF NOT EXISTS farm_couple_requests (
    from_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    to_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    at      INTEGER NOT NULL,
    PRIMARY KEY (from_id, to_id)
);
CREATE INDEX IF NOT EXISTS farm_couple_requests_to ON farm_couple_requests (to_id);
