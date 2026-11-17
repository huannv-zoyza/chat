DROP INDEX IF EXISTS idx_messages_parent_id;
ALTER TABLE messages DROP COLUMN parent_id;

DROP INDEX IF EXISTS idx_message_reactions_message_id;
DROP TABLE IF EXISTS message_reactions;
