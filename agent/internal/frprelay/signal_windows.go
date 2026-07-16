//go:build windows

package frprelay

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	output, err := exec.Command(
		"tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH",
	).Output()
	if err != nil {
		return false
	}
	line := strings.TrimSpace(string(output))
	if line == "" || strings.HasPrefix(strings.ToUpper(line), "INFO:") {
		return false
	}
	return strings.Contains(line, ",\""+strconv.Itoa(pid)+"\",")
}
