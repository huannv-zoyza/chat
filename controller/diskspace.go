package controller

import (
	"errors"
	"log"
)

// defaultMinFreeDisk: còn ít hơn chừng này chỗ trống thì không nhận file mới,
// để đĩa đầy không làm hỏng chat.db (SQLite cần chỗ để ghi WAL).
const defaultMinFreeDisk = 1 << 30 // 1 GB

var errDiskFreeUnsupported = errors.New("không đo được dung lượng trống trên hệ điều hành này")

// SetMinFreeDisk đổi ngưỡng chỗ trống tối thiểu (byte) trên đĩa chứa thư mục uploads.
func (a *API) SetMinFreeDisk(n uint64) { a.minFreeDisk = n }

// checkDiskSpace trả câu báo lỗi nếu lưu thêm incoming byte sẽ làm đĩa còn dưới ngưỡng.
// Không đo được (hệ điều hành lạ, lỗi) thì cho qua.
func (a *API) checkDiskSpace(incoming int64) string {
	free, err := diskFree(a.uploadsDir)
	if err != nil {
		if !errors.Is(err, errDiskFreeUnsupported) {
			log.Printf("đo dung lượng trống: %v", err)
		}
		return ""
	}
	need := a.minFreeDisk
	if incoming > 0 {
		need += uint64(incoming)
	}
	if free < need {
		return "Máy chủ sắp hết dung lượng ổ đĩa, tạm thời không gửi được file. Báo người quản trị dọn bớt."
	}
	return ""
}
