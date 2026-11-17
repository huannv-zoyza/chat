DROP TRIGGER IF EXISTS messages_fts_ad;
DROP TRIGGER IF EXISTS messages_fts_au;
DROP TRIGGER IF EXISTS messages_fts_ai;
DROP TABLE IF EXISTS messages_fts;

ALTER TABLE messages DROP COLUMN forwarded;
ALTER TABLE messages DROP COLUMN reply_to_id;
DROP INDEX IF EXISTS idx_messages_pinned;
ALTER TABLE messages DROP COLUMN pinned_by;
ALTER TABLE messages DROP COLUMN pinned_at;
