-- Sửa/xoá tin nhắn: tin bị xoá giữ lại một dòng "đã bị xoá" (để luồng trả lời không mất gốc),
-- nội dung, file, cảm xúc, nhắc tên bị xoá sạch.
ALTER TABLE messages ADD COLUMN edited_at DATETIME;
ALTER TABLE messages ADD COLUMN deleted_at DATETIME;

-- Tin nhắn riêng (1-1): là một kênh riêng tư kind = 'dm' có đúng 2 thành viên
-- (hoặc 1 nếu tự nhắn cho mình). Tên kênh "dm:<id nhỏ>:<id lớn>" (dấu ":" không dùng được
-- trong tên kênh thường) nên chỉ mục unique trên name đảm bảo mỗi cặp chỉ có một cuộc trò chuyện.
ALTER TABLE channels ADD COLUMN kind TEXT NOT NULL DEFAULT 'channel';
