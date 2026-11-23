//go:build !linux && !darwin

package controller

func diskFree(string) (uint64, error) { return 0, errDiskFreeUnsupported }
