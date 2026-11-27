-- Giáng sinh, phần bổ sung (specs/FARM_SPEC.md §16d).
-- Thống kê theo ngày: số lần mỗi hoạt động (key) và người chơi có hoạt động sự kiện trong ngày.
CREATE TABLE IF NOT EXISTS farm_xmas_stats (
    day TEXT    NOT NULL,
    key TEXT    NOT NULL,
    n   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, key)
);
CREATE TABLE IF NOT EXISTS farm_xmas_active (
    day     TEXT    NOT NULL,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (day, user_id)
);
-- Tin Noel đã báo (để người offline xem lại): user_id NULL = tin chung (bỏ qua người trong skip ",id,id,"),
-- có user_id = tin riêng người đó.
CREATE TABLE IF NOT EXISTS farm_xmas_news (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    at      INTEGER NOT NULL,
    user_id INTEGER REFERENCES users (id) ON DELETE CASCADE,
    kind    TEXT    NOT NULL DEFAULT 'news',
    text    TEXT    NOT NULL,
    from_id INTEGER NOT NULL DEFAULT 0,
    skip    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_farm_xmas_news_user ON farm_xmas_news (user_id, id);
