DROP INDEX IF EXISTS idx_channels_invite_code;
ALTER TABLE channels DROP COLUMN invite_code;
ALTER TABLE channels DROP COLUMN is_private;
