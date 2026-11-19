-- Trí nhớ dài hạn của Trợ lý AI cho từng kênh: bản tóm tắt các tin có id <= upto_id (gộp dần khi kênh dài ra).
CREATE TABLE IF NOT EXISTS ai_memory (
    channel_id INTEGER PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    summary    TEXT     NOT NULL,
    upto_id    INTEGER  NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
