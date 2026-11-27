-- Sự kiện Giáng sinh (specs/FARM_SPEC.md §16).
-- farm_xmas: Kẹo Noel (tiền sự kiện) của mỗi người; earned: tổng kẹo đã kiếm (bảng xếp hạng);
-- tree: điểm đã góp vào cây thông chung toàn server.
CREATE TABLE IF NOT EXISTS farm_xmas (
    user_id INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    candy   INTEGER NOT NULL DEFAULT 0 CHECK (candy >= 0),
    earned  INTEGER NOT NULL DEFAULT 0,
    tree    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_farm_xmas_earned ON farm_xmas (earned DESC);
-- Đơn của Ông già Noel: 3 đơn mỗi người mỗi ngày (giờ VN), sinh lúc mở nông trại lần đầu trong ngày.
CREATE TABLE IF NOT EXISTS farm_xmas_orders (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day     TEXT    NOT NULL,
    slot    INTEGER NOT NULL,
    items   TEXT    NOT NULL,
    candy   INTEGER NOT NULL,
    xp      INTEGER NOT NULL,
    done    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day, slot)
);
-- Tất Noel: from_id treo ở vườn to_id ngày day (mỗi người một tất / vườn / ngày); chủ vườn mở từ hôm sau.
CREATE TABLE IF NOT EXISTS farm_xmas_socks (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    from_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    to_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day     TEXT    NOT NULL,
    item    TEXT    NOT NULL,
    qty     INTEGER NOT NULL CHECK (qty > 0),
    opened  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (from_id, to_id, day)
);
CREATE INDEX IF NOT EXISTS idx_farm_xmas_socks_to ON farm_xmas_socks (to_id, opened);

-- Cầu tuyết: from_id ném vào vườn to_id lúc at (giây unix); hit = 1 nếu trúng.
CREATE TABLE IF NOT EXISTS farm_xmas_snowballs (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    from_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    to_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    at      INTEGER NOT NULL,
    hit     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_farm_xmas_snow_to ON farm_xmas_snowballs (to_id, at);
CREATE INDEX IF NOT EXISTS idx_farm_xmas_snow_from ON farm_xmas_snowballs (from_id, at);
-- Quà Ông già Noel thả ngẫu nhiên: slot "ngày|lượt" duy nhất; ai bấm trước (claimed_by) thì được.
CREATE TABLE IF NOT EXISTS farm_xmas_drops (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slot       TEXT    NOT NULL UNIQUE,
    owner_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    at         INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    claimed_by INTEGER REFERENCES users (id) ON DELETE SET NULL
);
-- Đồ trang trí Noel hiếm (trúng từ hộp quà bí ẩn), hiện trên bản đồ vườn.
CREATE TABLE IF NOT EXISTS farm_xmas_decor (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    item    TEXT    NOT NULL,
    PRIMARY KEY (user_id, item)
);
