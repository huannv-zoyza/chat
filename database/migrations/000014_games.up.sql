-- Trò chơi trong kênh (Ma Sói, Ai là gián điệp, cờ caro, oẳn tù tì): mỗi ván gắn với một tin nhắn.
-- state là JSON toàn bộ ván, kể cả phần bí mật (vai, từ khoá), server không gửi nguyên văn cho client.
CREATE TABLE IF NOT EXISTS games (
    message_id INTEGER PRIMARY KEY REFERENCES messages (id) ON DELETE CASCADE,
    kind       TEXT     NOT NULL,
    status     TEXT     NOT NULL, -- lobby | playing | ended
    state      TEXT     NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_games_active ON games (status) WHERE status <> 'ended';
