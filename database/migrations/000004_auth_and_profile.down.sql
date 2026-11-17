ALTER TABLE channel_members DROP COLUMN last_read_message_id;

DROP INDEX IF EXISTS idx_channels_name;
ALTER TABLE channels DROP COLUMN description;

ALTER TABLE users DROP COLUMN status_text;
ALTER TABLE users DROP COLUMN display_name;

DROP INDEX IF EXISTS idx_sessions_user_id;
DROP TABLE IF EXISTS sessions;
