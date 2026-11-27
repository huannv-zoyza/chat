-- Lùi: bỏ job máy ép, giữ job xưởng; xoá máy ép đã xây.
CREATE TABLE farm_jobs_old (
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    slot     INTEGER NOT NULL,
    recipe   TEXT    NOT NULL,
    ready_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, slot)
);
INSERT INTO farm_jobs_old (user_id, slot, recipe, ready_at)
    SELECT user_id, slot, recipe, ready_at FROM farm_jobs WHERE building = 'kitchen';
DROP TABLE farm_jobs;
ALTER TABLE farm_jobs_old RENAME TO farm_jobs;
DELETE FROM farm_buildings WHERE building = 'juicer';
