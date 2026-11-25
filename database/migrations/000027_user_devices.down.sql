DROP TABLE IF EXISTS user_devices;
DROP INDEX IF EXISTS idx_users_signup_device;
ALTER TABLE users DROP COLUMN signup_device;
