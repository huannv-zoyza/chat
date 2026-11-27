-- Vườn cây ăn quả (docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md, phần A).
-- Mỗi dòng một cây đang trồng ở chỗ spot (0..11). ready_at: giây unix cây có quả (lần đầu: lớn xong).
-- cycles: số lần đã hái (0 = còn đang lớn, chưa ra quả lần nào).
CREATE TABLE IF NOT EXISTS farm_trees (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    spot       INTEGER NOT NULL,
    tree       TEXT    NOT NULL,
    planted_at INTEGER NOT NULL,
    ready_at   INTEGER NOT NULL,
    cycles     INTEGER NOT NULL DEFAULT 0,
    UNIQUE (user_id, spot)
);
