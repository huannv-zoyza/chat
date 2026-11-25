-- Nông trại phần 2: kho, chăn nuôi, đơn hàng (xe tải), mở rộng đất theo bậc (specs/FARM_SPEC.md §4–8).

-- Kho: số lần đã nâng cấp (sức chứa = 60 + 30 × storage_level, tối đa 5 lần).
ALTER TABLE farms ADD COLUMN storage_level INTEGER NOT NULL DEFAULT 0;

-- farm_plots.stolen đổi nghĩa: từ số xu bị trộm sang số đơn vị nông sản bị lấy. Mỗi lần trộm cũ
-- tương ứng một đơn vị, nên quy đổi stolen = steals (tối đa 2).
UPDATE farm_plots SET stolen = MIN(steals, 2);

-- Hàng trong kho (nông sản + sản phẩm chăn nuôi). Hết hàng thì xoá dòng.
CREATE TABLE IF NOT EXISTS farm_stock (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    item    TEXT    NOT NULL,
    qty     INTEGER NOT NULL CHECK (qty >= 0),
    PRIMARY KEY (user_id, item)
);

-- Chuồng trại đã xây: level 1 = cơ bản, 2 = đã nâng cấp.
CREATE TABLE IF NOT EXISTS farm_buildings (
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    building TEXT    NOT NULL, -- coop | pasture
    level    INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, building)
);

-- Vật nuôi. ready_at = 0: đói; > 0: đã ăn, có sản phẩm từ lúc ready_at (giây unix).
CREATE TABLE IF NOT EXISTS farm_animals (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL,
    ready_at   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS farm_animals_user ON farm_animals (user_id, id);

-- Đơn hàng xe tải: mỗi người 3 ô (slot 0..2). items = '' là ô trống, có đơn mới từ next_at.
-- Mỗi đơn mới là một dòng mới (id không dùng lại) để giao / huỷ đơn cũ không trúng đơn mới.
CREATE TABLE IF NOT EXISTS farm_orders (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    slot    INTEGER NOT NULL,
    items   TEXT    NOT NULL DEFAULT '', -- JSON [{"item": "...", "qty": n}]
    xu      INTEGER NOT NULL DEFAULT 0,
    xp      INTEGER NOT NULL DEFAULT 0,
    next_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE (user_id, slot)
);
