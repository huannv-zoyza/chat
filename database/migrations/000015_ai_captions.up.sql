-- Mô tả ảnh do AI viết (theo file trong uploads), để lần sau tóm tắt / hỏi lại không phải xem ảnh lần nữa.
CREATE TABLE IF NOT EXISTS ai_captions (
    storage_key TEXT PRIMARY KEY,
    caption     TEXT     NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
