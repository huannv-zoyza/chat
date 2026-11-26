-- Xưởng chế biến (specs/FARM_SPEC.md §9): mỗi lò (slot) làm một món; nguyên liệu đã trừ khỏi kho lúc
-- bắt đầu, món vào kho khi chủ lấy ra. Lò trống thì không có dòng. ready_at: giây unix lúc món xong.
CREATE TABLE IF NOT EXISTS farm_jobs (
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    slot     INTEGER NOT NULL,
    recipe   TEXT    NOT NULL,
    ready_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, slot)
);
