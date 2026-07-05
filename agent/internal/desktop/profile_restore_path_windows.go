//go:build windows

package desktop

import "syscall"

const profileFileAttributeReparsePoint uint32 = 0x400

func profilePathHasReparsePoint(path string) (bool, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		return false, err
	}
	return attributes&profileFileAttributeReparsePoint != 0, nil
}