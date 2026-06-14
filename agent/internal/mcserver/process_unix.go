//go:build !windows

package mcserver

import (
	"errors"
	"os"
	"syscall"
)

func inspectProcess(pid int) processState {
	if pid <= 0 {
		return processDead
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return processUnknown
	}
	err = process.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return processAlive
	case errors.Is(err, os.ErrProcessDone), errors.Is(err, syscall.ESRCH):
		return processDead
	case errors.Is(err, syscall.EPERM):
		return processAlive
	default:
		return processUnknown
	}
}
