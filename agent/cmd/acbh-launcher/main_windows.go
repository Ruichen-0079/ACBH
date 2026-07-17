//go:build windows

package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	localUIURL        = "http://127.0.0.1:6130"
	windowsService    = "ACBHAgent"
	detachedProcess   = 0x00000008
	launcherWaitLimit = 10 * time.Second
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	appDir := filepath.Dir(executable)
	if len(os.Args) > 1 && strings.HasPrefix(strings.ToLower(os.Args[1]), "acbh://open-logs") {
		openLogDirectory(appDir)
		return
	}
	if !agentAvailable() {
		if fileExists(filepath.Join(appDir, "portable.flag")) {
			startPortableAgent(appDir)
		} else {
			startInstalledService()
		}
		waitForAgent(launcherWaitLimit)
	}
	openBrowser()
}

func openLogDirectory(appDir string) {
	logDirectory := filepath.Join(os.Getenv("ProgramData"), "ACBH", "logs")
	if fileExists(filepath.Join(appDir, "portable.flag")) {
		logDirectory = filepath.Join(appDir, "data", "logs")
	}
	_ = os.MkdirAll(logDirectory, 0o700)
	systemRoot := os.Getenv("SystemRoot")
	explorer := filepath.Join(systemRoot, "explorer.exe")
	command := exec.Command(explorer, logDirectory)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = command.Start()
}

func agentAvailable() bool {
	client := http.Client{Timeout: 400 * time.Millisecond}
	response, err := client.Get(localUIURL + "/local/v1/status")
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func waitForAgent(limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if agentAvailable() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func startPortableAgent(appDir string) {
	agent := filepath.Join(appDir, "acbh-agent.exe")
	frpc := filepath.Join(appDir, "frpc.exe")
	data := filepath.Join(appDir, "data")
	command := exec.Command(agent, "hobby", "serve", "--address", "127.0.0.1:6130", "--frpc", frpc, "--app-data-dir", data)
	command.Dir = appDir
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
		HideWindow:    true,
	}
	_ = command.Start()
}

func startInstalledService() {
	systemRoot := os.Getenv("SystemRoot")
	serviceControl := filepath.Join(systemRoot, "System32", "sc.exe")
	command := exec.Command(serviceControl, "start", windowsService)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = command.Run()
}

func openBrowser() {
	systemRoot := os.Getenv("SystemRoot")
	rundll32 := filepath.Join(systemRoot, "System32", "rundll32.exe")
	command := exec.Command(rundll32, "url.dll,FileProtocolHandler", localUIURL)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = command.Start()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
