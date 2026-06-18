package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	"github.com/Ruichen-0079/ACBH/agent/internal/scanner"
)

const (
	daemonPIDFileName = "agent-daemon.pid"
	daemonLogFileName = "agent-daemon.log"
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

func StartDaemon(opts Options) (DaemonState, error) {
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
	cmd := exec.Command(exe, "daemon", "--status", "standby", "--interval", "10s")
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
