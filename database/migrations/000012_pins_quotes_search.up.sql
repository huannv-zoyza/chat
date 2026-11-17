-- Ghim tin nhắn
ALTER TABLE messages ADD COLUMN pinned_at DATETIME;
ALTER TABLE messages ADD COLUMN pinned_by INTEGER;
CREATE INDEX IF NOT EXISTS idx_messages_pinned ON messages (channel_id, pinned_at) WHERE pinned_at IS NOT NULL;

-- Trả lời trích dẫn (khác luồng trả lời: tin vẫn nằm ở dòng chính, kèm trích tin được trả lời)
ALTER TABLE messages ADD COLUMN reply_to_id INTEGER;

-- Tin được chuyển tiếp từ nơi khác
ALTER TABLE messages ADD COLUMN forwarded INTEGER NOT NULL DEFAULT 0;

-- Tìm kiếm toàn văn. unicode61 + remove_diacritics: "pho" khớp "phở"; riêng "đ" không phải dấu
-- nên đổi sang "d" trước khi đưa vào chỉ mục (câu tìm kiếm cũng đổi như vậy).
-- Tin mã hoá đầu cuối có content rỗng nên không bao giờ vào chỉ mục.
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(body, tokenize = 'unicode61 remove_diacritics 2');

INSERT INTO messages_fts (rowid, body)
SELECT id, replace(replace(content, 'đ', 'd'), 'Đ', 'D') FROM messages WHERE content <> '' AND deleted_at IS NULL;

CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages WHEN new.content <> '' BEGIN
    INSERT INTO messages_fts (rowid, body) VALUES (new.id, replace(replace(new.content, 'đ', 'd'), 'Đ', 'D'));
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE OF content ON messages BEGIN
    DELETE FROM messages_fts WHERE rowid = old.id;
    INSERT INTO messages_fts (rowid, body)
    SELECT new.id, replace(replace(new.content, 'đ', 'd'), 'Đ', 'D') WHERE new.content <> '';
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
    DELETE FROM messages_fts WHERE rowid = old.id;
END;
