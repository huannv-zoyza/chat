//go:build linux || darwin

package controller

import "syscall"

// diskFree: số byte còn trống (cho tiến trình thường) trên đĩa chứa path.
func diskFree(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
