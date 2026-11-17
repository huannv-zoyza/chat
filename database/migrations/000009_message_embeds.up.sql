-- Sticker / GIF gửi kèm tin nhắn, lưu dạng JSON:
--   {"type":"sticker","id":"text/oke"}
--   {"type":"gif","id":"…","url":"https://media.giphy.com/…","width":200,"height":150,"title":"…"}
ALTER TABLE messages ADD COLUMN embed TEXT;
