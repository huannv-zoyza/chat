-- Điểm danh liên tục: số ngày liền nhau tính tới lần điểm danh gần nhất.
ALTER TABLE wallets ADD COLUMN streak INTEGER NOT NULL DEFAULT 0;

-- Nông trại: mỗi người một nông trại (tạo lười). Thời điểm lưu bằng giây unix (UTC).
CREATE TABLE IF NOT EXISTS farms (
    user_id    INTEGER  PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    xp         INTEGER  NOT NULL DEFAULT 0 CHECK (xp >= 0),
    plots      INTEGER  NOT NULL DEFAULT 6,
    dog_until  INTEGER  NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS farms_xp ON farms (xp DESC);

-- Mỗi dòng là một vụ đang chiếm ô đất (ô trống thì không có dòng). id đổi theo từng vụ.
CREATE TABLE IF NOT EXISTS farm_plots (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    idx        INTEGER NOT NULL,
    crop       TEXT    NOT NULL,
    planted_at INTEGER NOT NULL,
    ready_at   INTEGER NOT NULL,
    bug        INTEGER NOT NULL DEFAULT 0,
    waters     INTEGER NOT NULL DEFAULT 0,
    stolen     INTEGER NOT NULL DEFAULT 0,
    steals     INTEGER NOT NULL DEFAULT 0,
    UNIQUE (user_id, idx)
);

-- Ai đã tưới / trộm vụ nào (mỗi người mỗi vụ một lần).
CREATE TABLE IF NOT EXISTS farm_crop_acts (
    plot_id  INTEGER NOT NULL REFERENCES farm_plots (id) ON DELETE CASCADE,
    actor_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action   TEXT    NOT NULL, -- water | steal
    PRIMARY KEY (plot_id, actor_id, action)
);
CREATE INDEX IF NOT EXISTS farm_crop_acts_actor ON farm_crop_acts (actor_id);

-- Bộ đếm hành động theo ngày (giờ VN): tiến độ nhiệm vụ và lượt trộm trong ngày.
CREATE TABLE IF NOT EXISTS farm_counters (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day     TEXT    NOT NULL,
    key     TEXT    NOT NULL,
    n       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day, key)
);

-- Nông trại đã ghé thăm trong ngày (nhiệm vụ "visit" đếm nông trại khác nhau).
CREATE TABLE IF NOT EXISTS farm_visits (
    user_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day      TEXT    NOT NULL,
    owner_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, day, owner_id)
);

-- Thưởng nhiệm vụ đã nhận (quest "chest" = rương thưởng ngày).
CREATE TABLE IF NOT EXISTS farm_claims (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day     TEXT    NOT NULL,
    quest   TEXT    NOT NULL,
    PRIMARY KEY (user_id, day, quest)
);

-- Nhật ký việc người khác làm trên nông trại của mình (giữ 50 dòng mới nhất mỗi người).
CREATE TABLE IF NOT EXISTS farm_log (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    actor_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action   TEXT    NOT NULL, -- water | bug | steal | dog
    crop     TEXT    NOT NULL DEFAULT '',
    xu       INTEGER NOT NULL DEFAULT 0,
    at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS farm_log_owner ON farm_log (owner_id, id);
