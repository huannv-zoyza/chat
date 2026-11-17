CREATE TABLE IF NOT EXISTS channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       VARCHAR(255) NOT NULL,
    owner_id   INTEGER      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_channels_owner_id ON channels (owner_id);

-- Bảng nối thay cho cột members INT[] (mảng không thể có FOREIGN KEY)
CREATE TABLE IF NOT EXISTS channel_members (
    channel_id INTEGER   NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id    INTEGER   NOT NULL REFERENCES users (id)    ON DELETE CASCADE,
    joined_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_members_user_id ON channel_members (user_id);
