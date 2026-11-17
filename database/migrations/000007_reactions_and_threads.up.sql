-- Thả cảm xúc vào tin nhắn: mỗi người mỗi emoji tối đa 1 lần trên 1 tin.
CREATE TABLE IF NOT EXISTS message_reactions (
    message_id INTEGER     NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    user_id    INTEGER     NOT NULL REFERENCES users (id)    ON DELETE CASCADE,
    emoji      VARCHAR(32) NOT NULL,
    created_at TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (message_id, user_id, emoji)
);

CREATE INDEX IF NOT EXISTS idx_message_reactions_message_id ON message_reactions (message_id);

-- Trả lời theo luồng: tin trả lời trỏ về tin gốc (chỉ 1 cấp, không có luồng lồng nhau).
-- Không khai báo REFERENCES để rollback (DROP COLUMN) được; trả lời luôn cùng kênh với tin gốc
-- nên vẫn bị xoá theo kênh (ON DELETE CASCADE của channel_id).
ALTER TABLE messages ADD COLUMN parent_id INTEGER;
CREATE INDEX IF NOT EXISTS idx_messages_parent_id ON messages (parent_id, id);
