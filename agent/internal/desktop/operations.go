package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/artifactsync"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcimport"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcserver"
	"github.com/Ruichen-0079/ACBH/agent/internal/rcon"
	"github.com/Ruichen-0079/ACBH/agent/internal/relay"
	"github.com/Ruichen-0079/ACBH/agent/internal/scanner"
)

const (
	daemonPIDFileName   = "agent-daemon.pid"
	daemonLogFileName   = "agent-daemon.log"
	mcServerPIDFileName = "mc-server.pid"
	mcServerLogFileName = "minecraft-server.log"
)

type DaemonState struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	PIDPath string `json:"pidPath"`
	LogPath string `json:"logPath"`
	Message string `json:"message"`
}

type RCONStatus struct {
	ServerDir          string `json:"serverDir"`
	PropertiesExists   bool   `json:"propertiesExists"`
	Enabled            bool   `json:"enabled"`
	Port               string `json:"port,omitempty"`
	PasswordConfigured bool   `json:"passwordConfigured"`
	Message            string `json:"message"`
	SuggestedConfig    string `json:"suggestedConfig"`
}

type ManifestInfo struct {
	Path         string                `json:"path"`
	ArtifactKind manifest.ArtifactKind `json:"artifactKind"`
	ArtifactID   string                `json:"artifactId"`
	TotalBytes   int64                 `json:"totalBytes"`
	FileCount    int                   `json:"fileCount"`
	ModifiedAt   time.Time             `json:"modifiedAt"`
}

type ScanResult struct {
	ManifestPath string                `json:"manifestPath"`
	ArtifactKind manifest.ArtifactKind `json:"artifactKind"`
	ArtifactID   string                `json:"artifactId"`
	Report       scanner.Report        `json:"report"`
	Message      string                `json:"message"`
}

type SafeSyncResult struct {
	ScanResult
	RCONMessage string `json:"rconMessage"`
}

type CanPushResult struct {
	CanPush               bool   `json:"canPush"`
	GroupID               string `json:"groupId"`
	HostID                string `json:"hostId"`
	CurrentHostID         string `json:"currentHostId,omitempty"`
	CurrentHostGeneration int    `json:"currentHostGeneration"`
	Reason                string `json:"reason"`
	NextStep              string `json:"nextStep"`
}

type TakeoverStatus struct {
	GroupID                  string                          `json:"groupId"`
	HostID                   string                          `json:"hostId"`
	CurrentHostID            string                          `json:"currentHostId,omitempty"`
	CurrentHostGeneration    int                             `json:"currentHostGeneration"`
	IsCurrentHost            bool                            `json:"isCurrentHost"`
	ActiveTakeoverAssignment *coordinator.TakeoverAssignment `json:"activeTakeoverAssignment,omitempty"`
	CanRunTakeover           bool                            `json:"canRunTakeover"`
	Message                  string                          `json:"message"`
	NextCLICommand           string                          `json:"nextCliCommand"`
}

type ManagedServerState struct {
	Running bool   `json:"running"`
	Stale   bool   `json:"stale"`
	Reason  string `json:"reason,omitempty"`
	PID     int    `json:"pid,omitempty"`
}

type StartServerResult struct {
	OK               bool     `json:"ok"`
	ErrorCode        string   `json:"errorCode,omitempty"`
	Message          string   `json:"message,omitempty"`
	ServerDir        string   `json:"serverDir,omitempty"`
	WorkingDirectory string   `json:"workingDirectory,omitempty"`
	LaunchCommand    string   `json:"launchCommand,omitempty"`
	ScriptPath       string   `json:"scriptPath,omitempty"`
	LauncherPath     string   `json:"launcherPath,omitempty"`
	ExitCode         int      `json:"exitCode,omitempty"`
	JavaPath         string   `json:"javaPath,omitempty"`
	JarPath          string   `json:"jarPath,omitempty"`
	LogFile          string   `json:"logFile,omitempty"`
	Suggestion       string   `json:"suggestion,omitempty"`
	PID              int      `json:"pid,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

type StopServerResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	PID     int    `json:"pid,omitempty"`
}

func StartDaemon(opts Options) (DaemonState, error) {
	return startDaemon(opts, "standby")
}

func startDaemon(opts Options, status string) (DaemonState, error) {
	opts = withDefaults(opts)
	state, err := DaemonStatus(opts)
	if err == nil && state.Running {
		state.Message = "后台服务 daemon 已在运行，未重复启动。"
		return state, nil
	}
	if err := ensureRunAndLogDirs(opts); err != nil {
		return DaemonState{}, err
	}
	logPath := daemonLogPath(opts)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return DaemonState{}, fmt.Errorf("打开 daemon 日志失败: %w", err)
	}
	exe := opts.ExecutablePath
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			_ = logFile.Close()
			return DaemonState{}, fmt.Errorf("定位 acbh-agent 可执行文件失败: %w", err)
		}
	}
	if status == "" {
		status = "standby"
	}
	cmd := exec.Command(exe, "daemon", "--status", status, "--interval", "10s")
	cmd.Env = append(os.Environ(), "ACBH_APP_DATA_DIR="+opts.AppDataDir)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureBackgroundProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return DaemonState{}, fmt.Errorf("启动后台服务 daemon 失败: %w", err)
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()
	if err := os.WriteFile(daemonPIDPath(opts), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		return DaemonState{}, fmt.Errorf("写入 daemon pid file 失败: %w", err)
	}
	return DaemonState{
		Running: true,
		PID:     cmd.Process.Pid,
		PIDPath: daemonPIDPath(opts),
		LogPath: logPath,
		Message: "后台服务 daemon 已启动，会持续发送心跳 heartbeat。",
	}, nil
}

func StopDaemon(opts Options) (DaemonState, error) {
	opts = withDefaults(opts)
	state, err := DaemonStatus(opts)
	if err != nil {
		return state, err
	}
	if !state.Running {
		_ = os.Remove(daemonPIDPath(opts))
		state.Message = "后台服务 daemon 未运行。"
		return state, nil
	}
	proc, err := os.FindProcess(state.PID)
	if err != nil {
		return state, fmt.Errorf("查找 daemon 进程失败: %w", err)
	}
	if err := proc.Kill(); err != nil {
		return state, fmt.Errorf("停止 daemon 失败: %w", err)
	}
	_ = os.Remove(daemonPIDPath(opts))
	state.Running = false
	state.Message = "后台服务 daemon 已停止。"
	return state, nil
}

func DaemonStatus(opts Options) (DaemonState, error) {
	opts = withDefaults(opts)
	pidPath := daemonPIDPath(opts)
	logPath := daemonLogPath(opts)
	raw, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return DaemonState{PIDPath: pidPath, LogPath: logPath, Message: "后台服务 daemon 未运行。"}, nil
	}
	if err != nil {
		return DaemonState{}, fmt.Errorf("读取 daemon pid file 失败: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return DaemonState{PIDPath: pidPath, LogPath: logPath, Message: "daemon pid file 无效。"}, nil
	}
	running := processRunning(pid)
	msg := "后台服务 daemon 未运行。"
	if running {
		msg = "后台服务 daemon 运行中。"
	}
	return DaemonState{Running: running, PID: pid, PIDPath: pidPath, LogPath: logPath, Message: msg}, nil
}

func RCONStatusForServer(opts Options, serverDir string) (RCONStatus, error) {
	opts = withDefaults(opts)
	if strings.TrimSpace(serverDir) == "" {
		cfg, err := loadDesktopConfig(opts)
		if err != nil {
			return RCONStatus{}, err
		}
		serverDir = cfg.Server.Dir
	}
	report, err := mcimport.Inspect(serverDir)
	if err != nil {
		return RCONStatus{}, err
	}
	msg := report.RCON.ChineseMessage
	if !report.RCON.Enabled {
		msg = "RCON 未开启，safe-sync 需要 RCON 执行 save-all flush。请按建议配置修改 server.properties。"
	} else if !report.RCON.PasswordSet {
		msg = "RCON 已开启，但 rcon.password 未配置。请设置强密码后再执行 safe-sync。"
	} else if report.RCON.Port == "" {
		msg = "RCON 已开启，但 rcon.port 缺失。请设置 rcon.port=25575 或你的实际端口。"
	}
	return RCONStatus{
		ServerDir:          report.ServerDir,
		PropertiesExists:   report.HasProperties,
		Enabled:            report.RCON.Enabled,
		Port:               report.RCON.Port,
		PasswordConfigured: report.RCON.PasswordSet,
		Message:            msg,
		SuggestedConfig:    "enable-rcon=true\nrcon.port=25575\nrcon.password=<请设置强密码>",
	}, nil
}

func ScanPack(opts Options) (ScanResult, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return ScanResult{}, err
	}
	if strings.TrimSpace(cfg.Server.Dir) == "" {
		return ScanResult{}, errors.New("请先在 GUI 中导入 MC 服务端目录。")
	}
	artifactID := "server-pack-" + time.Now().Format("20060102-150405")
	output := filepath.Join(manifestDir(opts), artifactID+".manifest.json")
	manifestData, report, err := scanner.Scan(scanner.Options{
		ServerDir:     cfg.Server.Dir,
		ArtifactKind:  manifest.ServerPack,
		ArtifactID:    artifactID,
		GroupID:       cfg.GroupID,
		CreatorHostID: cfg.HostID,
		OutputPath:    output,
	})
	if err != nil {
		return ScanResult{}, err
	}
	if err := manifest.SaveFile(output, manifestData); err != nil {
		return ScanResult{}, err
	}
	return ScanResult{
		ManifestPath: output,
		ArtifactKind: manifest.ServerPack,
		ArtifactID:   artifactID,
		Report:       report,
		Message:      "服务端包 server-pack manifest 已生成，下一步可以点击上传同步制品 push。",
	}, nil
}

func SafeSyncWorld(ctx context.Context, opts Options, rconPassword string) (SafeSyncResult, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return SafeSyncResult{}, err
	}
	if strings.TrimSpace(cfg.Server.Dir) == "" {
		return SafeSyncResult{}, errors.New("请先在 GUI 中导入 MC 服务端目录。")
	}
	rconStatus, err := RCONStatusForServer(opts, cfg.Server.Dir)
	if err != nil {
		return SafeSyncResult{}, err
	}
	if !rconStatus.PropertiesExists || !rconStatus.Enabled || rconStatus.Port == "" || !rconStatus.PasswordConfigured {
		return SafeSyncResult{}, errors.New(rconStatus.Message + "\n建议配置:\n" + rconStatus.SuggestedConfig)
	}
	password := strings.TrimSpace(rconPassword)
	if password == "" {
		password = strings.TrimSpace(os.Getenv("ACBH_RCON_PASSWORD"))
	}
	if password == "" {
		return SafeSyncResult{}, errors.New("需要 RCON 密码。safe-sync 必须通过 RCON 执行 save-all flush 后才能安全生成世界快照。")
	}
	port, err := strconv.Atoi(rconStatus.Port)
	if err != nil {
		return SafeSyncResult{}, fmt.Errorf("rcon.port 不是有效端口: %w", err)
	}
	response, err := rcon.Execute(ctx, rcon.Config{
		Host:     "127.0.0.1",
		Port:     port,
		Password: password,
		Timeout:  10 * time.Second,
	}, "save-all flush")
	if err != nil {
		return SafeSyncResult{}, fmt.Errorf("RCON save-all flush 失败: %w", err)
	}
	if strings.Contains(strings.ToLower(response), "error") || strings.Contains(strings.ToLower(response), "failed") {
		return SafeSyncResult{}, fmt.Errorf("RCON save-all flush 返回失败: %s", strings.ReplaceAll(response, password, "[REDACTED]"))
	}
	artifactID := "world-" + time.Now().Format("20060102-150405")
	output := filepath.Join(manifestDir(opts), artifactID+".manifest.json")
	serverPackVersion := latestServerPackID(opts)
	if serverPackVersion == "" {
		serverPackVersion = "server-pack-local"
	}
	manifestData, report, err := scanner.Scan(scanner.Options{
		ServerDir:         cfg.Server.Dir,
		ArtifactKind:      manifest.WorldSnapshot,
		ArtifactID:        artifactID,
		GroupID:           cfg.GroupID,
		CreatorHostID:     cfg.HostID,
		ServerPackVersion: serverPackVersion,
		OutputPath:        output,
	})
	if err != nil {
		return SafeSyncResult{}, err
	}
	if err := manifest.SaveFile(output, manifestData); err != nil {
		return SafeSyncResult{}, err
	}
	return SafeSyncResult{
		ScanResult: ScanResult{
			ManifestPath: output,
			ArtifactKind: manifest.WorldSnapshot,
			ArtifactID:   artifactID,
			Report:       report,
			Message:      "世界快照 world-snapshot manifest 已生成，下一步建议点击上传同步制品 push。",
		},
		RCONMessage: "RCON save-all flush succeeded.",
	}, nil
}

func PushLatest(ctx context.Context, opts Options) (artifactsync.PushSummary, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return artifactsync.PushSummary{}, err
	}
	latest, err := LatestManifest(opts)
	if err != nil {
		return artifactsync.PushSummary{}, errors.New("没有找到最近 manifest，请先扫描服务端包 scan server-pack 或安全同步世界快照 safe-sync。")
	}
	canPush, err := CanPush(ctx, opts)
	if err != nil {
		return artifactsync.PushSummary{}, err
	}
	if !canPush.CanPush {
		return artifactsync.PushSummary{}, errors.New(canPush.Reason + " " + canPush.NextStep)
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return artifactsync.PushSummary{}, err
	}
	generation := canPush.CurrentHostGeneration
	return artifactsync.Push(ctx, artifactsync.PushOptions{
		ManifestPath:   latest.Path,
		ServerDir:      cfg.Server.Dir,
		Config:         cfg,
		Client:         client,
		HostGeneration: &generation,
	})
}

func PullLatest(ctx context.Context, opts Options, artifactKind manifest.ArtifactKind, artifactID string, applyDeletes bool) (artifactsync.PullSummary, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return artifactsync.PullSummary{}, err
	}
	if strings.TrimSpace(cfg.Server.Dir) == "" {
		return artifactsync.PullSummary{}, errors.New("请先导入 MC 服务端目录，pull 会写入该目录。")
	}
	if artifactKind == "" {
		return artifactsync.PullSummary{}, errors.New("请选择要拉取的制品类型：server-pack 或 world-snapshot。")
	}
	if artifactID == "" {
		artifactID = "latest"
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return artifactsync.PullSummary{}, err
	}
	return artifactsync.Pull(ctx, artifactsync.PullOptions{
		ArtifactKind: artifactKind,
		ArtifactID:   artifactID,
		OutputDir:    cfg.Server.Dir,
		ApplyDeletes: applyDeletes,
		Config:       cfg,
		Client:       client,
	})
}

func CanPush(ctx context.Context, opts Options) (CanPushResult, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return CanPushResult{}, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return CanPushResult{}, err
	}
	status, err := client.GetElectionStatus(ctx, coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
	if err != nil {
		return CanPushResult{}, fmt.Errorf("检查 current host 失败: %w", err)
	}
	result := CanPushResult{
		GroupID:               cfg.GroupID,
		HostID:                cfg.HostID,
		CurrentHostGeneration: status.CurrentHostGeneration,
		NextStep:              "请先在控制端执行选举/接管，或使用 CLI 高级流程。",
	}
	if status.CurrentHostID != nil {
		result.CurrentHostID = *status.CurrentHostID
	}
	if status.CurrentHostID == nil {
		result.Reason = "控制端当前没有 current host，不能上传同步制品 push。"
		return result, nil
	}
	if *status.CurrentHostID != cfg.HostID {
		result.Reason = "当前本地主机不是 current host，不能上传同步制品 push。"
		return result, nil
	}
	result.CanPush = true
	result.Reason = "当前本地主机是 current host，可以上传同步制品 push。"
	result.NextStep = "可以点击上传同步制品 push。"
	return result, nil
}

func TakeoverStatusForDesktop(ctx context.Context, opts Options) (TakeoverStatus, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return TakeoverStatus{}, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return TakeoverStatus{}, err
	}
	status, err := client.GetElectionStatus(ctx, coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
	if err != nil {
		return TakeoverStatus{}, fmt.Errorf("读取 election status 失败: %w", err)
	}
	out := TakeoverStatus{
		GroupID:                  cfg.GroupID,
		HostID:                   cfg.HostID,
		CurrentHostGeneration:    status.CurrentHostGeneration,
		ActiveTakeoverAssignment: status.ActiveTakeoverAssignment,
		NextCLICommand:           "acbh-agent takeover poll && acbh-agent takeover run --dry-run",
	}
	if status.CurrentHostID != nil {
		out.CurrentHostID = *status.CurrentHostID
		out.IsCurrentHost = *status.CurrentHostID == cfg.HostID
	}
	if status.ActiveTakeoverAssignment == nil {
		out.Message = "当前没有 takeover assignment。GUI 仅显示演练状态，不会执行危险接管操作。"
		return out, nil
	}
	if status.ActiveTakeoverAssignment.HostID != cfg.HostID {
		out.Message = "存在 takeover assignment，但不是分配给当前本地主机。"
		return out, nil
	}
	out.CanRunTakeover = true
	out.Message = "存在分配给当前本地主机的 takeover assignment。建议先使用 CLI dry-run 检查。"
	return out, nil
}

func LatestManifest(opts Options) (ManifestInfo, error) {
	opts = withDefaults(opts)
	entries, err := os.ReadDir(manifestDir(opts))
	if err != nil {
		return ManifestInfo{}, err
	}
	var manifests []ManifestInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".manifest.json") {
			continue
		}
		path := filepath.Join(manifestDir(opts), entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		loaded, err := manifest.LoadFile(path)
		if err != nil {
			continue
		}
		manifests = append(manifests, ManifestInfo{
			Path:         path,
			ArtifactKind: loaded.ArtifactKind,
			ArtifactID:   loaded.ArtifactID,
			TotalBytes:   loaded.Summary.TotalBytes,
			FileCount:    len(loaded.Files),
			ModifiedAt:   info.ModTime(),
		})
	}
	if len(manifests) == 0 {
		return ManifestInfo{}, os.ErrNotExist
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].ModifiedAt.After(manifests[j].ModifiedAt)
	})
	return manifests[0], nil
}

func ManagedServerStatus(opts Options) (ManagedServerState, error) {
	opts = withDefaults(opts)
	status, err := mcserver.GetStatus(filepath.Join(opts.AppDataDir, "runtime"))
	if err != nil {
		return ManagedServerState{}, err
	}
	out := ManagedServerState{Running: status.Running, Stale: status.Stale, Reason: status.Reason}
	if status.State.PID > 0 {
		out.PID = status.State.PID
	}
	return out, nil
}

func loadDesktopConfig(opts Options) (agentconfig.Config, error) {
	opts = withDefaults(opts)
	return agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
}

func mcImportReport(serverDir string) (mcimport.Report, error) {
	return mcimport.Inspect(serverDir)
}

func manifestDir(opts Options) string {
	return filepath.Join(opts.AppDataDir, "manifests")
}

func latestServerPackID(opts Options) string {
	entries, err := os.ReadDir(manifestDir(opts))
	if err != nil {
		return ""
	}
	var latest ManifestInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".manifest.json") {
			continue
		}
		path := filepath.Join(manifestDir(opts), entry.Name())
		loaded, err := manifest.LoadFile(path)
		if err != nil || loaded.ArtifactKind != manifest.ServerPack {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if latest.Path == "" || info.ModTime().After(latest.ModifiedAt) {
			latest = ManifestInfo{Path: path, ArtifactID: loaded.ArtifactID, ModifiedAt: info.ModTime()}
		}
	}
	return latest.ArtifactID
}

func ensureRunAndLogDirs(opts Options) error {
	for _, dir := range []string{filepath.Join(opts.AppDataDir, "run"), filepath.Join(opts.AppDataDir, "logs")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func daemonPIDPath(opts Options) string {
	return filepath.Join(opts.AppDataDir, "run", daemonPIDFileName)
}

func daemonLogPath(opts Options) string {
	return filepath.Join(opts.AppDataDir, "logs", daemonLogFileName)
}

func mcServerPIDPath(opts Options) string {
	return filepath.Join(opts.AppDataDir, "run", mcServerPIDFileName)
}

func mcServerLogPath(opts Options) string {
	return filepath.Join(opts.AppDataDir, "logs", mcServerLogFileName)
}

func configureBackgroundProcess(cmd *exec.Cmd) {
	_ = cmd
}

func processRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
		if err != nil {
			return false
		}
		return bytes.Contains(out, []byte(strconv.Itoa(pid)))
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

type RelayHostState struct {
	Running  bool   `json:"running"`
	PID      int    `json:"pid,omitempty"`
	Target   string `json:"target,omitempty"`
	Message  string `json:"message"`
	Sessions int    `json:"activeSessions,omitempty"`
}

const relayHostPIDFileName = "relay-host.pid"

func relayHostPIDPath(opts Options) string {
	return filepath.Join(opts.AppDataDir, "run", relayHostPIDFileName)
}

// StartRelayHost starts (or ensures) the relay host manager for public entry.
// It checks current host, writes pid, and runs a discovery loop for assigned tunnel sessions.
func StartRelayHost(opts Options, targetAddress string) (RelayHostState, error) {
	opts = withDefaults(opts)
	if err := ensureRunAndLogDirs(opts); err != nil {
		return RelayHostState{}, err
	}

	// current host check
	cp, err := CanPush(context.Background(), opts)
	if err != nil {
		return RelayHostState{}, fmt.Errorf("无法检查 current host: %w", err)
	}
	cfg, _ := loadDesktopConfig(opts)
	if !cp.CanPush || cp.CurrentHostID != cfg.HostID {
		return RelayHostState{Message: "当前本地主机不是 current host，不能启动公网中转 relay。"}, nil
	}

	pidPath := relayHostPIDPath(opts)
	// if already running, return
	if raw, err := os.ReadFile(pidPath); err == nil {
		if p, _ := strconv.Atoi(strings.TrimSpace(string(raw))); p > 0 && processRunning(p) {
			return RelayHostState{Running: true, PID: p, Target: targetAddress, Message: "公网中转 relay host 已在运行。"}, nil
		}
	}

	if targetAddress == "" {
		// auto from server config or default 25565
		if cfg.Server.Dir != "" {
			if r, _ := mcimport.Inspect(cfg.Server.Dir); r.LaunchJar != "" {
				targetAddress = "127.0.0.1:25565"
			}
		}
		if targetAddress == "" {
			targetAddress = "127.0.0.1:25565"
		}
	}

	// write pid
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return RelayHostState{}, fmt.Errorf("写入 relay host pid 失败: %w", err)
	}

	// In CLI this func is called from long-running cmd, so we can block here with the manager loop.
	// For the manager, we poll list and start per-session host clients.
	go runRelayHostManager(opts, targetAddress, pidPath)

	return RelayHostState{Running: true, PID: pid, Target: targetAddress, Message: "公网中转 relay host 已启动 (manager 运行中)。"}, nil
}

func StopRelayHost(opts Options) (RelayHostState, error) {
	opts = withDefaults(opts)
	pidPath := relayHostPIDPath(opts)
	raw, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return RelayHostState{Message: "公网中转 relay host 未运行。"}, nil
	}
	if err != nil {
		return RelayHostState{}, err
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if pid <= 0 || !processRunning(pid) {
		_ = os.Remove(pidPath)
		return RelayHostState{Message: "公网中转 relay host 记录已清理。"}, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return RelayHostState{}, err
	}
	if err := proc.Kill(); err != nil {
		return RelayHostState{}, fmt.Errorf("停止 relay host 失败: %w", err)
	}
	_ = os.Remove(pidPath)
	return RelayHostState{Running: false, Message: "公网中转 relay host 已停止。"}, nil
}

func RelayHostStatus(opts Options) (RelayHostState, error) {
	opts = withDefaults(opts)
	pidPath := relayHostPIDPath(opts)
	raw, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return RelayHostState{Running: false, Message: "公网中转 relay host 未运行。"}, nil
	}
	if err != nil {
		return RelayHostState{}, err
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	running := pid > 0 && processRunning(pid)
	return RelayHostState{Running: running, PID: pid, Message: "公网中转 relay host 状态已查询。"}, nil
}

// runRelayHostManager discovers tunnel sessions assigned to us and starts HostRelayClient for each.
func runRelayHostManager(opts Options, targetAddress string, pidPath string) {
	// simple poll loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	activeSessions := map[string]context.CancelFunc{}

	for {
		select {
		case <-ticker.C:
			// list
			cfg, err := loadDesktopConfig(opts)
			if err != nil {
				continue
			}
			client, _ := coordinator.NewClient(cfg.CoordinatorURL) // note: may need full auth but for list we added open
			sessions, err := client.ListTunnelSessions(context.Background(), cfg.GroupID)
			if err != nil {
				continue
			}
			for _, s := range sessions {
				if s.HostID != cfg.HostID || s.Status == "closed" {
					continue
				}
				if _, ok := activeSessions[s.SessionID]; ok {
					continue
				}
				// start a host client for this session
				ctx, cancel := context.WithCancel(context.Background())
				activeSessions[s.SessionID] = cancel
				go func(sessID string, gen int) {
					defer func() {
						delete(activeSessions, sessID)
					}()
					hc := relay.NewHostRelayClient(relay.HostRelayOptions{
						CoordinatorURL: cfg.CoordinatorURL,
						GroupID:        cfg.GroupID,
						SessionID:      sessID,
						HostID:         cfg.HostID,
						HostToken:      cfg.HostToken,
						HostGeneration: gen,
						TargetAddress:  targetAddress,
					})
					_ = hc.Run(ctx)
				}(s.SessionID, s.CurrentHostGeneration)
			}
		}
	}
}

func isPortLikelyInUse(port string) bool {
	if port == "" {
		port = "25565"
	}
	addr := "127.0.0.1:" + port
	conn, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

// StartServer performs preflights then starts MC server using mcserver, records pid/log per spec.
func StartServer(opts Options) (StartServerResult, error) {
	opts = withDefaults(opts)
	res := StartServerResult{OK: false, Warnings: []string{}}

	cfg, err := loadDesktopConfig(opts)
	if err != nil || cfg.Server.Dir == "" {
		res.ErrorCode = "no_server_config"
		res.Message = "请先导入 Minecraft 服务端目录。"
		res.Suggestion = "请点击“导入 MC 服务端目录”或运行 desktop import-server --server-dir <dir>"
		return res, nil
	}

	serverDir := filepath.Clean(cfg.Server.Dir)
	res.ServerDir = serverDir
	res.WorkingDirectory = serverDir

	if serverDir == "" {
		res.ErrorCode = "no_server_dir"
		res.Message = "请先导入 Minecraft 服务端目录。"
		return res, nil
	}
	if _, statErr := os.Stat(serverDir); statErr != nil {
		res.ErrorCode = "server_dir_not_exist"
		res.Message = fmt.Sprintf("Minecraft 服务端目录不存在：%s", serverDir)
		res.Suggestion = "请确认目录存在并重新导入。"
		return res, nil
	}

	setup, _ := LoadSetup(opts)

	// inspect for jar, eula, props
	report, inspectErr := mcimport.Inspect(serverDir)
	if inspectErr != nil {
		res.ErrorCode = "inspect_failed"
		res.Message = "服务端目录检测失败: " + inspectErr.Error()
		return res, nil
	}

	jar := report.LaunchJar
	launchCommand := cfg.Server.Command
	if report.LaunchEntry == "" && launchCommand == "" {
		res.ErrorCode = "missing_launch_entry"
		res.Message = "找不到服务端启动入口。请重新导入 Minecraft 服务端目录。"
		res.Suggestion = "请确认根目录中存在 run.bat、start.bat 或受支持的服务端 jar。"
		return res, nil
	}
	if jar != "" {
		res.JarPath = filepath.Join(serverDir, jar)
	}

	// java
	javaPath, javaErr := exec.LookPath("java")
	if javaErr != nil {
		res.ErrorCode = "no_java"
		res.Message = "未检测到 Java，请安装 Java 17 或 21。"
		res.Suggestion = "安装 Java 17/21 (推荐 Eclipse Adoptium Temurin) 并确保 java.exe 在系统 PATH 中。"
		return res, nil
	}
	res.JavaPath = javaPath

	// eula checks (preflight 4,5)
	if !report.HasEULA {
		res.ErrorCode = "eula_missing"
		res.Message = "Minecraft EULA 未确认。请在确认 EULA 后创建 eula.txt 并设置 eula=true。"
		res.Suggestion = "在服务端目录创建 eula.txt 文件，内容为 eula=true"
		return res, nil
	}
	if !report.EULAAccepted {
		res.ErrorCode = "eula_false"
		res.Message = "Minecraft EULA 未确认。请将 eula.txt 设置为 eula=true。"
		res.Suggestion = "编辑 eula.txt 将 eula=false 改为 eula=true"
		return res, nil
	}

	// launch command from config or default
	if launchCommand == "" {
		launchCommand = report.SuggestedCommand
	}
	launchArgv, psInfo, psErr := buildPowerShellLaunchArgv(serverDir, setup, report, launchCommand)
	if psErr != nil {
		res.ErrorCode = psInfo.ErrorCode
		res.Message = psErr.Error()
		res.ScriptPath = psInfo.ScriptPath
		res.WorkingDirectory = serverDir
		res.LauncherPath = psInfo.LauncherPath
		res.Suggestion = psInfo.Suggestion
		return res, nil
	}
	if len(launchArgv) > 0 {
		launchCommand = mcserver.DisplayCommand(launchArgv)
		res.ScriptPath = psInfo.ScriptPath
		res.LauncherPath = psInfo.LauncherPath
	}
	res.LaunchCommand = launchCommand

	if len(launchArgv) == 0 {
		argv, parseErr := mcserver.ParseCommand(launchCommand)
		if parseErr != nil {
			res.ErrorCode = "bad_command"
			res.Message = "启动命令解析失败: " + parseErr.Error()
			return res, nil
		}
		_ = argv // validated; mcserver parses again inside the supervisor.
	}

	// server.properties warn but not block (7)
	if !report.HasProperties {
		res.Warnings = append(res.Warnings, "server.properties 缺失，将使用 Minecraft 默认端口 25565。")
	}

	// port may occupied (8)
	port := "25565"
	if p, ok := report.Properties["server-port"]; ok && strings.TrimSpace(p) != "" {
		port = strings.TrimSpace(p)
	}
	if isPortLikelyInUse(port) {
		res.Warnings = append(res.Warnings, fmt.Sprintf("MC 服务端端口 %s 可能被占用，请检查 server.properties 中 server-port。", port))
	}

	// ensure dirs
	if err := ensureRunAndLogDirs(opts); err != nil {
		res.ErrorCode = "mkdir_failed"
		res.Message = "无法创建 run/logs 目录: " + err.Error()
		return res, nil
	}

	logFile := mcServerLogPath(opts)
	res.LogFile = logFile

	// prepare mcserver start opts , use runtime subdir for isolation
	runtimeDir := filepath.Join(opts.AppDataDir, "runtime")
	logDirForServer := filepath.Join(opts.AppDataDir, "logs", "minecraft")
	// use config stop timeout or default
	stopTimeout := 30 * time.Second
	if cfg.Server.StopTimeout != "" {
		if d, derr := time.ParseDuration(cfg.Server.StopTimeout); derr == nil {
			stopTimeout = d
		}
	}

	startOpts := mcserver.StartOptions{
		ServerDir:   serverDir,
		Command:     launchCommand,
		CommandArgv: launchArgv,
		LogDir:      logDirForServer,
		RuntimeDir:  runtimeDir,
		StopTimeout: stopTimeout,
	}

	// find exe for supervisor
	exe := opts.ExecutablePath
	if exe == "" {
		exe, _ = os.Executable()
	}

	// call mcserver start (this will record its own state/pid in runtime, we also record simple pid)
	state, startErr := mcserver.Start(context.Background(), exe, startOpts)
	if startErr != nil {
		if len(launchArgv) > 0 {
			res.ErrorCode = "powershell_script_failed"
			res.ExitCode = -1
			res.Message = "PowerShell 启动失败或 run.ps1 执行后立即退出: " + startErr.Error()
			res.Suggestion = "请检查 minecraft-server.log 以及 logs/minecraft/server-stderr.log。"
		} else {
			res.ErrorCode = "start_failed"
			res.Message = "MC 服务端启动失败: " + startErr.Error()
			res.Suggestion = "请查看日志目录中的 minecraft 日志，或确认端口、Java 版本、EULA。"
		}
		return res, nil
	}

	res.PID = state.PID
	res.OK = true
	res.Message = "MC 服务端已启动。"

	// record pid in run/ as required
	_ = os.WriteFile(mcServerPIDPath(opts), []byte(strconv.Itoa(state.PID)+"\n"), 0o600)

	// append note to the required log name (since mcserver uses server-*.log , we touch the named one for user)
	_ = os.WriteFile(logFile, []byte(fmt.Sprintf("[%s] MC server start requested via ACBH desktop (pid supervisor ~%d, see minecraft/*.log)\n", time.Now().Format(time.RFC3339), state.PID)), 0o600)

	return res, nil
}

type powerShellLaunchInfo struct {
	ScriptPath   string
	LauncherPath string
	ErrorCode    string
	Suggestion   string
}

var lookPath = exec.LookPath

func buildPowerShellLaunchArgv(serverDir string, setup SetupState, report mcimport.Report, launchCommand string) ([]string, powerShellLaunchInfo, error) {
	scriptPath := ""
	if setup.LaunchProfile.Kind == "script" && strings.EqualFold(setup.LaunchProfile.ScriptType, "powershell") {
		scriptPath = setup.LaunchProfile.ScriptPath
	}
	if scriptPath == "" && report.LaunchProfile.Kind == "script" && strings.EqualFold(report.LaunchProfile.ScriptType, "powershell") {
		scriptPath = report.LaunchProfile.ScriptPath
	}
	if scriptPath == "" {
		scriptPath = powershellScriptFromCommand(launchCommand)
	}
	if scriptPath == "" {
		return nil, powerShellLaunchInfo{}, nil
	}
	info := powerShellLaunchInfo{ScriptPath: scriptPath, Suggestion: "请重新选择服务端目录内的 PowerShell 启动脚本。"}
	resolved, err := resolveLaunchScriptInside(serverDir, scriptPath)
	if err != nil {
		info.ErrorCode = "launch_script_outside_server_dir"
		return nil, info, err
	}
	if _, err := os.Stat(resolved); err != nil {
		info.ErrorCode = "launch_script_missing"
		if os.IsNotExist(err) {
			return nil, info, fmt.Errorf("%s 不存在或已被移动", filepath.Base(scriptPath))
		}
		return nil, info, err
	}
	launcher, err := findPowerShell()
	if err != nil {
		info.ErrorCode = "powershell_not_found"
		info.Suggestion = "请确认 Windows PowerShell 可用，或安装 PowerShell 7 并确保 pwsh.exe 在 PATH 中。"
		return nil, info, errors.New("未检测到 PowerShell，无法执行 run.ps1。")
	}
	info.ScriptPath = resolved
	info.LauncherPath = launcher
	return []string{launcher, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", resolved}, info, nil
}

func findPowerShell() (string, error) {
	if p, err := lookPath("powershell.exe"); err == nil {
		return p, nil
	}
	if p, err := lookPath("pwsh.exe"); err == nil {
		return p, nil
	}
	return "", errors.New("powershell not found")
}

func powershellScriptFromCommand(command string) string {
	args, err := mcserver.ParseCommand(command)
	if err != nil {
		return ""
	}
	for i, arg := range args {
		if strings.EqualFold(arg, "-File") && i+1 < len(args) {
			if strings.HasSuffix(strings.ToLower(args[i+1]), ".ps1") {
				return args[i+1]
			}
		}
	}
	for _, arg := range args {
		if strings.HasSuffix(strings.ToLower(arg), ".ps1") {
			return arg
		}
	}
	return ""
}

func resolveLaunchScriptInside(serverDir string, scriptPath string) (string, error) {
	scriptPath = strings.TrimSpace(scriptPath)
	if scriptPath == "" {
		return "", errors.New("请选择启动脚本。")
	}
	if strings.HasPrefix(scriptPath, `\\`) {
		return "", errors.New("暂不支持网络 UNC 启动脚本路径。")
	}
	baseAbs, err := filepath.Abs(filepath.Clean(serverDir))
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(scriptPath))
	var candidate string
	if filepath.IsAbs(clean) {
		candidate = clean
	} else {
		candidate = filepath.Join(baseAbs, clean)
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(baseAbs, candidateAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("run.ps1 不在服务端目录内，已拒绝执行。")
	}
	return candidateAbs, nil
}

func StopServer(opts Options) (StopServerResult, error) {
	opts = withDefaults(opts)
	res := StopServerResult{OK: false}

	pidPath := mcServerPIDPath(opts)
	raw, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		// fallback to mcserver stop
		runtimeDir := filepath.Join(opts.AppDataDir, "runtime")
		st, stopped, serr := mcserver.Stop(runtimeDir)
		if serr == nil && stopped {
			res.OK = true
			res.Message = "已通过 mcserver 停止。"
			res.PID = st.PID
			_ = os.Remove(pidPath)
			return res, nil
		}
		res.Message = "没有记录的由 ACBH 启动的 MC 服务端进程。"
		return res, nil
	}
	if err != nil {
		res.Message = "读取 MC pid 文件失败: " + err.Error()
		return res, nil
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if pid <= 0 {
		res.Message = "MC pid 文件内容无效。"
		return res, nil
	}

	// only kill if running and (optionally verify not other java, but per pid file trust)
	if !processRunning(pid) {
		_ = os.Remove(pidPath)
		res.Message = "记录的 MC 服务端进程已不在运行。"
		return res, nil
	}

	// use mcserver stop which is safe (via control not direct kill other)
	runtimeDir := filepath.Join(opts.AppDataDir, "runtime")
	st, stopped, serr := mcserver.Stop(runtimeDir)
	if serr == nil && stopped {
		res.OK = true
		res.PID = st.PID
		res.Message = "MC 服务端已停止。"
		_ = os.Remove(pidPath)
		return res, nil
	}

	// fallback direct kill the recorded pid (only our one)
	proc, perr := os.FindProcess(pid)
	if perr != nil {
		res.Message = "查找进程失败: " + perr.Error()
		return res, nil
	}
	if kerr := proc.Kill(); kerr != nil {
		res.Message = "停止进程失败: " + kerr.Error()
		return res, nil
	}
	_ = os.Remove(pidPath)
	res.OK = true
	res.PID = pid
	res.Message = "MC 服务端进程已通过 pid 停止（仅停止 ACBH 记录的进程）。"
	return res, nil
}
