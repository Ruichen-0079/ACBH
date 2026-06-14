//go:build windows

package artifactsync

import "syscall"

func pathHasReparsePoint(path string) (bool, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := syscall.GetFileAttributes(pointer)
	if err != nil {
		return false, err
	}
	return isReparsePointAttributes(attributes), nil
}
