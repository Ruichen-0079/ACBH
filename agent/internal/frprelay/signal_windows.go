//go:build windows

package frprelay

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActive
}

func processFingerprint(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID %d", pid)
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", fmt.Errorf("open process %d identity: %w", pid, err)
	}
	defer windows.CloseHandle(process)

	image := make([]uint16, 32768)
	size := uint32(len(image))
	if err := windows.QueryFullProcessImageName(process, 0, &image[0], &size); err != nil {
		return "", fmt.Errorf("read process %d executable: %w", pid, err)
	}
	executable := windows.UTF16ToString(image[:size])
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(process, &created, &exited, &kernel, &user); err != nil {
		return "", fmt.Errorf("read process %d creation time: %w", pid, err)
	}
	if strings.TrimSpace(executable) == "" || created.Nanoseconds() <= 0 {
		return "", fmt.Errorf("process %d identity is incomplete", pid)
	}
	sum := sha256.Sum256([]byte(strings.ToLower(executable) + "\x00" + strconv.FormatInt(created.Nanoseconds(), 10)))
	return hex.EncodeToString(sum[:]), nil
}
