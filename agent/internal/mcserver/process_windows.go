//go:build windows

package mcserver

import "syscall"

const stillActive = 259

func inspectProcess(pid int) processState {
	if pid <= 0 {
		return processDead
	}
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		if err == syscall.Errno(87) {
			return processDead
		}
		return processUnknown
	}
	defer syscall.CloseHandle(handle)

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return processUnknown
	}
	if exitCode == stillActive {
		return processAlive
	}
	return processDead
}
