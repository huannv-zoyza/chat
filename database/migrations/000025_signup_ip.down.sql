DROP INDEX IF EXISTS idx_users_signup_ip;
ALTER TABLE users DROP COLUMN signup_ip;
