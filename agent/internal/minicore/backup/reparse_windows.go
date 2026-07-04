//go:build windows

package backup

import "syscall"

const fileAttributeReparsePoint = 0x400

func hasReparsePoint(path string) bool {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := syscall.GetFileAttributes(ptr)
	return err == nil && attrs&fileAttributeReparsePoint != 0
}
