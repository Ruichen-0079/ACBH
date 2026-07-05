package listener

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
)

type Warning struct {
	Code    coreerrors.ErrorCode `json:"code"`
	Message string               `json:"message"`
}

type ProcessInfo struct {
	PID            int    `json:"pid"`
	ProcessName    string `json:"processName,omitempty"`
	ExecutablePath string `json:"executablePath,omitempty"`
	CommandLine    string `json:"commandLine,omitempty"`
}

type Listener struct {
	LocalAddress     string `json:"localAddress"`
	LocalPort        int    `json:"localPort"`
	PID              int    `json:"pid"`
	ProcessName      string `json:"processName,omitempty"`
	ExecutablePath   string `json:"executablePath,omitempty"`
	CommandLine      string `json:"commandLine,omitempty"`
	ServerDirMatched bool   `json:"serverDirMatched"`
	Confidence       string `json:"confidence"`
}

type Status struct {
	OK         bool       `json:"ok"`
	Configured bool       `json:"configured"`
	LocalHost  string     `json:"localHost"`
	LocalPort  int        `json:"localPort"`
	Listening  bool       `json:"listening"`
	Listeners  []Listener `json:"listeners"`
	Warnings   []Warning  `json:"warnings"`
}

type Inspector interface {
	TCPListeners(ctx context.Context, host string, port int) ([]Listener, error)
	ProcessInfo(ctx context.Context, pid int) (ProcessInfo, error)
}

type Service struct {
	Inspector Inspector
}

func (s Service) Status(ctx context.Context, cfg coreconfig.Config) (Status, *coreerrors.Error) {
	listenerCfg := cfg.Listener
	if err := validate(listenerCfg); err != nil {
		return Status{}, err
	}
	status := Status{OK: true, Configured: listenerCfg.Enabled, LocalHost: listenerCfg.LocalHost, LocalPort: listenerCfg.LocalPort}
	if !listenerCfg.Enabled {
		return status, nil
	}
	inspector := s.Inspector
	if inspector == nil {
		inspector = DefaultInspector{}
	}
	listeners, err := inspector.TCPListeners(ctx, listenerCfg.LocalHost, listenerCfg.LocalPort)
	if err != nil {
		status.Warnings = append(status.Warnings, Warning{Code: coreerrors.ProcessInspectionLimited, Message: err.Error()})
	}
	for _, item := range listeners {
		if item.PID > 0 {
			info, infoErr := inspector.ProcessInfo(ctx, item.PID)
			if infoErr != nil {
				status.Warnings = append(status.Warnings, Warning{Code: coreerrors.ProcessInspectionLimited, Message: infoErr.Error()})
			} else {
				item.ProcessName = info.ProcessName
				item.ExecutablePath = info.ExecutablePath
				item.CommandLine = info.CommandLine
			}
		}
		item.ServerDirMatched = serverDirMatched(cfg.Server.Dir, item.CommandLine, item.ExecutablePath)
		item.Confidence = confidence(listenerCfg, item, cfg.Server.Dir)
		status.Listeners = append(status.Listeners, item)
	}
	status.Listening = len(status.Listeners) > 0
	if !status.Listening {
		status.Warnings = append(status.Warnings, Warning{
			Code:    coreerrors.LocalPortNotListening,
			Message: "未检测到本地 Minecraft 端口监听。请先用 MCSL 或服务端脚本启动 MC 服务端。",
		})
	}
	return status, nil
}

func validate(cfg coreconfig.ListenerConfig) *coreerrors.Error {
	if cfg.LocalPort < 1 || cfg.LocalPort > 65535 {
		return coreerrors.New(coreerrors.ConfigInvalid, "listener.localPort must be between 1 and 65535", coreerrors.Details{}, "Set listener.localPort to the Minecraft server port.")
	}
	if strings.TrimSpace(cfg.LocalHost) == "" {
		return coreerrors.New(coreerrors.ConfigInvalid, "listener.localHost is required", coreerrors.Details{}, "Set listener.localHost to 127.0.0.1.")
	}
	return nil
}

func serverDirMatched(serverDir string, values ...string) bool {
	if strings.TrimSpace(serverDir) == "" {
		return false
	}
	want, err := filepath.Abs(serverDir)
	if err != nil {
		want = serverDir
	}
	want = strings.ToLower(filepath.Clean(want))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if strings.Contains(strings.ToLower(filepath.Clean(value)), want) {
			return true
		}
	}
	return false
}

func confidence(cfg coreconfig.ListenerConfig, item Listener, serverDir string) string {
	name := strings.ToLower(item.ProcessName)
	for _, expected := range cfg.ExpectedProcessNames {
		if strings.EqualFold(strings.TrimSpace(expected), name) {
			if serverDir == "" || item.ServerDirMatched {
				return "high"
			}
			if cfg.ServerDirMatchRequired {
				return "low"
			}
			return "medium"
		}
	}
	if item.PID > 0 {
		return "medium"
	}
	return "low"
}

type DefaultInspector struct{}

func (DefaultInspector) TCPListeners(ctx context.Context, host string, port int) ([]Listener, error) {
	if runtime.GOOS == "windows" {
		return windowsTCPListeners(ctx, host, port)
	}
	return dialProbe(host, port)
}

func (DefaultInspector) ProcessInfo(ctx context.Context, pid int) (ProcessInfo, error) {
	if runtime.GOOS == "windows" {
		return windowsProcessInfo(ctx, pid)
	}
	return ProcessInfo{PID: pid}, nil
}

func dialProbe(host string, port int) ([]Listener, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return nil, nil
	}
	_ = conn.Close()
	return []Listener{{LocalAddress: host, LocalPort: port, Confidence: "low"}}, nil
}

func windowsTCPListeners(ctx context.Context, host string, port int) ([]Listener, error) {
	script := fmt.Sprintf(`$items = Get-NetTCPConnection -State Listen -LocalPort %d -ErrorAction Stop | Where-Object { $_.LocalAddress -eq '%s' -or $_.LocalAddress -eq '0.0.0.0' -or $_.LocalAddress -eq '::' }; $items | Select-Object LocalAddress,LocalPort,OwningProcess | ConvertTo-Json -Depth 3`, port, strings.ReplaceAll(host, "'", "''"))
	out, err := runPowerShell(ctx, script)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var rows []struct {
		LocalAddress  string `json:"LocalAddress"`
		LocalPort     int    `json:"LocalPort"`
		OwningProcess int    `json:"OwningProcess"`
	}
	if strings.HasPrefix(out, "[") {
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			return nil, err
		}
	} else {
		var row struct {
			LocalAddress  string `json:"LocalAddress"`
			LocalPort     int    `json:"LocalPort"`
			OwningProcess int    `json:"OwningProcess"`
		}
		if err := json.Unmarshal([]byte(out), &row); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	listeners := make([]Listener, 0, len(rows))
	for _, row := range rows {
		listeners = append(listeners, Listener{LocalAddress: row.LocalAddress, LocalPort: row.LocalPort, PID: row.OwningProcess})
	}
	return listeners, nil
}

func windowsProcessInfo(ctx context.Context, pid int) (ProcessInfo, error) {
	script := fmt.Sprintf(`Get-CimInstance Win32_Process -Filter "ProcessId = %d" | Select-Object ProcessId,Name,ExecutablePath,CommandLine | ConvertTo-Json -Depth 3`, pid)
	out, err := runPowerShell(ctx, script)
	if err != nil {
		return ProcessInfo{PID: pid}, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return ProcessInfo{PID: pid}, nil
	}
	var row struct {
		ProcessID      int    `json:"ProcessId"`
		Name           string `json:"Name"`
		ExecutablePath string `json:"ExecutablePath"`
		CommandLine    string `json:"CommandLine"`
	}
	if err := json.Unmarshal([]byte(out), &row); err != nil {
		return ProcessInfo{PID: pid}, err
	}
	return ProcessInfo{PID: row.ProcessID, ProcessName: row.Name, ExecutablePath: row.ExecutablePath, CommandLine: row.CommandLine}, nil
}

func runPowerShell(ctx context.Context, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("process inspection limited: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
