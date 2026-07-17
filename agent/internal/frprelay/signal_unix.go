//go:build !windows

package frprelay

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	return err == nil && process.Signal(syscall.Signal(0)) == nil
}

func processFingerprint(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID %d", pid)
	}
	root := filepath.Join("/proc", strconv.Itoa(pid))
	executable, err := os.Readlink(filepath.Join(root, "exe"))
	if err != nil {
		return "", fmt.Errorf("read process %d executable: %w", pid, err)
	}
	stat, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return "", fmt.Errorf("read process %d start time: %w", pid, err)
	}
	command, err := os.ReadFile(filepath.Join(root, "cmdline"))
	if err != nil {
		return "", fmt.Errorf("read process %d command line: %w", pid, err)
	}
	fields := strings.Fields(string(stat))
	if len(fields) < 22 {
		return "", fmt.Errorf("process %d stat is incomplete", pid)
	}
	sum := sha256.Sum256([]byte(executable + "\x00" + fields[21] + "\x00" + string(command)))
	return hex.EncodeToString(sum[:]), nil
}
