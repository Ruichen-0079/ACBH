//go:build windows

package frprelay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type windowsProcessIdentity struct {
	Executable  string `json:"executable"`
	CreatedUTC  string `json:"created_utc"`
	CommandLine string `json:"command_line"`
}

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

func processFingerprint(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID %d", pid)
	}
	script := fmt.Sprintf(`$p=Get-CimInstance Win32_Process -Filter 'ProcessId=%d'; if($null -eq $p){exit 3}; [pscustomobject]@{executable=$p.ExecutablePath;created_utc=$p.CreationDate.ToUniversalTime().ToString('o');command_line=$p.CommandLine}|ConvertTo-Json -Compress`, pid)
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return "", fmt.Errorf("inspect process %d identity: %w", pid, err)
	}
	var identity windowsProcessIdentity
	if err := json.Unmarshal(output, &identity); err != nil {
		return "", fmt.Errorf("parse process %d identity: %w", pid, err)
	}
	if identity.Executable == "" || identity.CreatedUTC == "" {
		return "", fmt.Errorf("process %d identity is incomplete", pid)
	}
	sum := sha256.Sum256([]byte(strings.ToLower(identity.Executable) + "\x00" + identity.CreatedUTC + "\x00" + identity.CommandLine))
	return hex.EncodeToString(sum[:]), nil
}
