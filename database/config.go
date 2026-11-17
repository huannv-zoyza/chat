package database

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// InitDB mở SQLite tại path (ví dụ "chat.db").
//   - foreign_keys(1): SQLite mặc định KHÔNG kiểm tra khoá ngoại, phải bật cho từng kết nối.
//   - journal_mode(WAL): đọc và ghi song song không chặn nhau.
//   - busy_timeout(5000): chờ tối đa 5s khi DB đang bị khoá thay vì lỗi SQLITE_BUSY ngay.
func InitDB(path string) (*sql.DB, error) {
	// _txlock=immediate: transaction xin quyền ghi ngay từ BEGIN, nên hai transaction ghi cùng lúc
	// sẽ chờ nhau (busy_timeout) thay vì một bên lỗi SQLITE_BUSY khi "nâng cấp" khoá giữa chừng.
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	// Người gọi chịu trách nhiệm đóng kết nối.
	return db, nil
}
