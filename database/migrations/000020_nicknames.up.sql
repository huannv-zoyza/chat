-- Biệt danh theo kênh (như Messenger): ai trong kênh cũng đặt được cho người khác.
CREATE TABLE IF NOT EXISTS channel_nicknames (
    channel_id INTEGER  NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id    INTEGER  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    nickname   TEXT     NOT NULL,
    set_by     INTEGER  REFERENCES users (id) ON DELETE SET NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (channel_id, user_id)
);
