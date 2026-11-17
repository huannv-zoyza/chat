ALTER TABLE poll_options DROP COLUMN added_by;
ALTER TABLE polls DROP COLUMN allow_add;
ALTER TABLE polls DROP COLUMN closes_at;
DROP INDEX IF EXISTS idx_reminders_user;
DROP INDEX IF EXISTS idx_reminders_due;
DROP TABLE IF EXISTS reminders;
