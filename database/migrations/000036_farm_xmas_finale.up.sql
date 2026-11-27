-- Giáng sinh, phần cao trào (specs/FARM_SPEC.md §16c).
-- Ông già Noel bí mật: người tham gia (đăng ký trước ngày bốc thăm); target_id: người mình tặng (sau bốc thăm);
-- item/qty: quà đã gửi (rời kho lúc gửi); opened: người nhận đã mở (từ đêm Noel).
CREATE TABLE IF NOT EXISTS farm_xmas_santa (
    user_id   INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    target_id INTEGER REFERENCES users (id) ON DELETE SET NULL,
    item      TEXT    NOT NULL DEFAULT '',
    qty       INTEGER NOT NULL DEFAULT 0,
    sent_at   INTEGER NOT NULL DEFAULT 0,
    opened    INTEGER NOT NULL DEFAULT 0
);
-- Cờ một lần toàn server (đã bốc thăm, đã báo đêm Noel...).
CREATE TABLE IF NOT EXISTS farm_xmas_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
