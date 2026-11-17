-- Kênh riêng tư: không hiện ở "Khám phá kênh", chỉ vào được bằng link mời.
ALTER TABLE channels ADD COLUMN is_private BOOLEAN NOT NULL DEFAULT 0;

-- Mã mời trong link (mọi kênh đều có, kênh công khai cũng mời bằng link được).
-- Chủ kênh có thể tạo mã mới để vô hiệu hoá link cũ.
ALTER TABLE channels ADD COLUMN invite_code VARCHAR(32);
UPDATE channels SET invite_code = lower(hex(randomblob(12))) WHERE invite_code IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_invite_code ON channels (invite_code);
