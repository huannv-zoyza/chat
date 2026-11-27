-- Máy ép nước (docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md, phần B): mỗi lò
-- thuộc về một công trình. Job xưởng đang có giữ nguyên (building = 'kitchen', cùng slot, cùng giờ).
CREATE TABLE farm_jobs_new (
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    building TEXT    NOT NULL DEFAULT 'kitchen',
    slot     INTEGER NOT NULL,
    recipe   TEXT    NOT NULL,
    ready_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, building, slot)
);
INSERT INTO farm_jobs_new (user_id, building, slot, recipe, ready_at)
    SELECT user_id, 'kitchen', slot, recipe, ready_at FROM farm_jobs;
DROP TABLE farm_jobs;
ALTER TABLE farm_jobs_new RENAME TO farm_jobs;
