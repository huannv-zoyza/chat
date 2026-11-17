CREATE TABLE IF NOT EXISTS messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER   NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id    INTEGER   NOT NULL REFERENCES users (id)    ON DELETE CASCADE,
    content    TEXT      NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_channel_id_created_at ON messages (channel_id, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages (user_id);
