-- Cuộc trò chuyện riêng sẽ trở thành kênh riêng tư thường tên "dm:a:b".
ALTER TABLE channels DROP COLUMN kind;

ALTER TABLE messages DROP COLUMN deleted_at;
ALTER TABLE messages DROP COLUMN edited_at;
