package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
)

const (
	defaultCoordinatorURL = "http://127.0.0.1:6121"
	defaultHost           = "127.0.0.1"
	defaultPort           = "6121"
	stateFileName         = "desktop-state.json"
	privateStateFileName  = "private-local-state.json"
)

type Options struct {
	AppDataDir      string
	ExecutablePath  string
	WorkingDir      string
	CoordinatorPath string
	NodePath        string
	Host            string
	Port            string
	DisplayName     string
	DeviceName      string
	KeepRunning     bool
}

type Status struct {
	AppDataDir            string `json:"appDataDir"`
	ConfigPath            string `json:"configPath"`
	CoordinatorState      string `json:"coordinatorState"`
	CoordinatorPID        int    `json:"coordinatorPid,omitempty"`
	CoordinatorURL        string `json:"coordinatorUrl"`
	GroupID               string `json:"groupId,omitempty"`
	HostID                string `json:"hostId,omitempty"`
	PrivateMode           bool   `json:"privateMode"`
	HealthOK              bool   `json:"healthOk"`
	Java                  string `json:"java"`
	Node                  string `json:"node"`
	LogDir                string `json:"logDir"`
	DaemonRunning         bool   `json:"daemonRunning"`
	DaemonPID             int    `json:"daemonPid,omitempty"`
	MCServerRunning       bool   `json:"mcServerRunning"`
	MCServerDir           string `json:"mcServerDir,omitempty"`
	MCServerType          string `json:"mcServerType,omitempty"`
	RCONStatus            string `json:"rconStatus,omitempty"`
	LatestManifestPath    string `json:"latestManifestPath,omitempty"`
	LatestArtifactID      string `json:"latestArtifactId,omitempty"`
	LatestArtifactKind    string `json:"latestArtifactKind,omitempty"`
	CurrentHostID         string `json:"currentHostId,omitempty"`
	IsCurrentHost         *bool  `json:"isCurrentHost,omitempty"`
	CurrentHostGeneration int    `json:"currentHostGeneration,omitempty"`
	LastError             string `json:"lastError,omitempty"`

	// v0.3.1 hotfix3 required fields for desktop status --json
	Mode                  string   `json:"mode"`
	CoordinatorStatus     string   `json:"coordinatorStatus"`
	AgentStatus           string   `json:"agentStatus"`
	DaemonStatus          string   `json:"daemonStatus"`
	MinecraftServerStatus string   `json:"minecraftServerStatus"`
	JavaStatus            string   `json:"javaStatus"`
	ServerDir             string   `json:"serverDir"`
	ServerType            string   `json:"serverType"`
	PublicEntryStatus     string   `json:"publicEntryStatus"`
	PublicEntryMessage    string   `json:"publicEntryMessage"`
	LatestManifest        string   `json:"latestManifest"`
	DataDir               string   `json:"dataDir"`
	DataSyncSource        string   `json:"dataSyncSource"`
	Warnings              []string `json:"warnings"`
}

type processState struct {
	CoordinatorPID int       `json:"coordinatorPid"`
	StartedAt      time.Time `json:"startedAt"`
	NodePath       string    `json:"nodePath"`
}

type privateState struct {
	Mode      string `json:"mode"`
	GroupID   string `json:"groupId"`
	AccessKey string `json:"accessKey"`
	HostID    string `json:"hostId"`
	HostToken string `json:"hostToken"`
}

func Start(ctx context.Context, opts Options, out *bytes.Buffer) (Status, error) {
	opts = withDefaults(opts)
	status, err := baseStatus(opts)
	if err != nil {
		return status, err
	}

	log(out, "私人模式：已启用，仅建议本机/可信局域网使用。")
	log(out, "正在检测运行环境...")

	nodePath, err := resolveNode(opts)
	if err != nil {
		return status, ChineseError(err)
	}
	status.Node = nodePath

	coordinatorPath, err := resolveCoordinatorPath(opts)
	if err != nil {
		return status, ChineseError(err)
	}

	client, err := coordinator.NewClient(status.CoordinatorURL)
	if err != nil {
		return status, err
	}
	if err := ensurePortAvailable(opts.Host, opts.Port); err != nil {
		if healthErr := client.Health(ctx); healthErr != nil {
			return status, ChineseError(err)
		}
		status.HealthOK = true
		log(out, "检测到控制端已在运行，继续复用当前控制端。")
	} else {
		log(out, "正在启动控制端...")
		proc, err := startCoordinator(opts, nodePath, coordinatorPath)
		if err != nil {
			return status, ChineseError(err)
		}
		status.CoordinatorPID = proc.Pid
		if err := saveJSON(filepath.Join(opts.AppDataDir, stateFileName), processState{
			CoordinatorPID: proc.Pid,
			StartedAt:      time.Now(),
			NodePath:       nodePath,
		}); err != nil {
			return status, err
		}

		if err := waitForHealth(ctx, client, 20*time.Second); err != nil {
			return status, ChineseError(err)
		}
		status.HealthOK = true
		log(out, "控制端已启动，/health 正常。")
	}

	cfg, created, err := ensurePrivateIdentity(ctx, opts, client)
	if err != nil {
		return status, ChineseError(err)
	}
	status.GroupID = cfg.GroupID
	status.HostID = cfg.HostID
	status.PrivateMode = true
	if created {
		log(out, "已创建并保存私人服务器组和本地主机身份。")
	} else {
		log(out, "已复用本地保存的私人服务器组和主机身份。")
	}

	req := coordinator.HeartbeatRequest{
		GroupID:   cfg.GroupID,
		HostID:    cfg.HostID,
		HostToken: cfg.HostToken,
		Status:    "standby",
		HostScoreHints: &coordinator.HostScoreHints{
			CPUCores: runtime.NumCPU(),
		},
	}
	javaPath, javaErr := exec.LookPath("java")
	javaAvailable := javaErr == nil
	req.HostScoreHints.JavaAvailable = &javaAvailable
	if javaErr == nil {
		status.Java = javaPath
	} else {
		status.Java = "未检测到 Java。Minecraft 服务端通常需要 Java 17 或更高版本。"
	}
	if _, err := client.SendHeartbeat(ctx, req); err != nil {
		return status, ChineseError(err)
	}
	log(out, "心跳已发送，主机已在控制端可见。")
	return status, nil
}

func Stop(opts Options, out *bytes.Buffer) (Status, error) {
	opts = withDefaults(opts)
	status, err := baseStatus(opts)
	if err != nil {
		return status, err
	}

	var state processState
	statePath := filepath.Join(opts.AppDataDir, stateFileName)
	if err := loadJSON(statePath, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log(out, "没有找到正在运行的桌面启动状态。")
			return status, nil
		}
		return status, err
	}
	status.CoordinatorPID = state.CoordinatorPID
	if state.CoordinatorPID > 0 {
		proc, err := os.FindProcess(state.CoordinatorPID)
		if err == nil {
			_ = proc.Kill()
			log(out, "控制端已停止。")
		}
	}
	_ = os.Remove(statePath)
	log(out, "本地 groupId/accessKey/hostToken 已保留；只有重置配置才会删除。")
	return status, nil
}

func CurrentStatus(ctx context.Context, opts Options) (Status, error) {
	opts = withDefaults(opts)
	status, err := baseStatus(opts)
	if err != nil {
		return status, err
	}
	var state processState
	if err := loadJSON(filepath.Join(opts.AppDataDir, stateFileName), &state); err == nil {
		status.CoordinatorPID = state.CoordinatorPID
	}
	if cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName)); err == nil {
		status.GroupID = cfg.GroupID
		status.HostID = cfg.HostID
		status.PrivateMode = true
		status.MCServerDir = cfg.Server.Dir
		if cfg.Server.Dir != "" {
			if report, inspectErr := mcImportReport(cfg.Server.Dir); inspectErr == nil {
				status.MCServerType = string(report.ServerType)
				status.RCONStatus = report.RCON.ChineseMessage
			}
		}
	}
	if nodePath, err := resolveNode(opts); err == nil {
		status.Node = nodePath
	} else {
		status.Node = ChineseError(err).Error()
	}
	if javaPath, err := exec.LookPath("java"); err == nil {
		status.Java = javaPath
	} else {
		status.Java = "未检测到 Java。Minecraft 服务端通常需要 Java 17 或更高版本。"
	}
	client, err := coordinator.NewClient(status.CoordinatorURL)
	if err == nil {
		status.HealthOK = client.Health(ctx) == nil
	}
	status.LogDir = filepath.Join(opts.AppDataDir, "logs")
	if daemonStatus, daemonErr := DaemonStatus(opts); daemonErr == nil {
		status.DaemonRunning = daemonStatus.Running
		status.DaemonPID = daemonStatus.PID
	}
	if serverStatus, serverErr := ManagedServerStatus(opts); serverErr == nil {
		status.MCServerRunning = serverStatus.Running
	}
	if latest, latestErr := LatestManifest(opts); latestErr == nil {
		status.LatestManifestPath = latest.Path
		status.LatestArtifactID = latest.ArtifactID
		status.LatestArtifactKind = string(latest.ArtifactKind)
	}
	if canPush, canPushErr := CanPush(ctx, opts); canPushErr == nil {
		status.CurrentHostGeneration = canPush.CurrentHostGeneration
		status.CurrentHostID = canPush.CurrentHostID
		v := canPush.CanPush
		status.IsCurrentHost = &v
	}

	// populate hotfix3 required fields (keep old fields too for compat)
	status.Mode = "local-private"
	status.DataDir = opts.AppDataDir
	status.PublicEntryStatus = "not_configured"
	status.PublicEntryMessage = "私人模式默认仅本机/可信局域网使用；如需外网连接，请配置端口转发、VPS relay 或隧道。"
	status.ServerDir = status.MCServerDir
	status.ServerType = status.MCServerType
	status.JavaStatus = status.Java
	status.LatestManifest = status.LatestManifestPath
	status.DataSyncSource = "local"

	// remote public detection (simple: if coordinator url not localhost)
	if cfg, _ := loadDesktopConfig(opts); cfg.CoordinatorURL != "" && !strings.Contains(cfg.CoordinatorURL, "127.0.0.1") && !strings.Contains(cfg.CoordinatorURL, "localhost") {
		status.Mode = "remote-public"
		status.DataSyncSource = "public-vps"
		status.PublicEntryStatus = "configured"
		status.PublicEntryMessage = "公网中转已配置，玩家可直连 VPS 入口。"
	}

	status.CoordinatorStatus = "stopped"
	if status.CoordinatorPID > 0 {
		if status.HealthOK {
			status.CoordinatorStatus = "running"
		} else {
			status.CoordinatorStatus = "unhealthy"
		}
	}
	status.AgentStatus = "not_logged_in"
	if status.GroupID != "" && status.HostID != "" {
		status.AgentStatus = "logged_in"
	}
	status.DaemonStatus = "stopped"
	if status.DaemonRunning {
		status.DaemonStatus = "running"
	}
	status.MinecraftServerStatus = "stopped"
	if status.MCServerRunning {
		status.MinecraftServerStatus = "running"
	}
	status.Warnings = []string{}
	// collect warnings if server dir set
	if status.ServerDir != "" {
		if report, rerr := mcImportReport(status.ServerDir); rerr == nil {
			status.Warnings = append(status.Warnings, report.Warnings...)
		}
	}
	// ensure isCurrentHost not nil for json consumers
	if status.IsCurrentHost == nil {
		f := false
		status.IsCurrentHost = &f
	}
	return status, nil
}

func Reset(opts Options) error {
	opts = withDefaults(opts)
	for _, name := range []string{agentconfig.FileName, stateFileName, privateStateFileName} {
		if err := os.Remove(filepath.Join(opts.AppDataDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func ChineseError(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	lowerText := strings.ToLower(text)
	switch {
	case strings.Contains(text, "Cannot find package 'ws'"), strings.Contains(text, "ERR_MODULE_NOT_FOUND") && strings.Contains(text, "ws"):
		return fmt.Errorf("控制端缺少运行依赖 ws。请点击“修复依赖”，或重新下载完整桌面版 bundle。原始错误：%w", err)
	case strings.Contains(text, "EPERM") && strings.Contains(lowerText, "program files"):
		return fmt.Errorf("当前账户无权修改 Node.js 全局目录。ACBH 将改用本地工具目录，无需管理员权限。原始错误：%w", err)
	case strings.Contains(text, "Invalid access key"), strings.Contains(text, "401"):
		return fmt.Errorf("加入密钥与服务器组不匹配。请重新复制当前服务器组的加入密钥，或点击“重建私人服务器组”。原始错误：%w", err)
	case strings.Contains(lowerText, "group") && strings.Contains(lowerText, "not found"):
		return fmt.Errorf("本地主机配置存在，但控制端没有对应服务器组。可能是控制端状态文件被删除。请选择恢复、重建或清空本地配置。原始错误：%w", err)
	case strings.Contains(text, "RCON password is required"):
		return fmt.Errorf("需要 RCON 密码才能安全保存世界。请在 server.properties 开启 enable-rcon=true 并设置 rcon.password。原始错误：%w", err)
	default:
		return err
	}
}

func withDefaults(opts Options) Options {
	if opts.Host == "" {
		opts.Host = defaultHost
	}
	if opts.Port == "" {
		opts.Port = defaultPort
	}
	if opts.CoordinatorPath != "" {
		opts.CoordinatorPath = filepath.Clean(opts.CoordinatorPath)
	}
	if opts.ExecutablePath == "" {
		if exe, err := os.Executable(); err == nil {
			opts.ExecutablePath = exe
		}
	}
	if opts.WorkingDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			opts.WorkingDir = cwd
		}
	}
	if opts.AppDataDir == "" {
		if dir, err := agentconfig.ResolveAppDataDir(opts.ExecutablePath); err == nil {
			opts.AppDataDir = dir
		}
	}
	if opts.DisplayName == "" {
		opts.DisplayName = "私人本地主机"
	}
	if opts.DeviceName == "" {
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			opts.DeviceName = hostname
		} else {
			opts.DeviceName = "ACBH-Windows-Host"
		}
	}
	return opts
}

func baseStatus(opts Options) (Status, error) {
	if opts.AppDataDir == "" {
		return Status{}, errors.New("resolve app data directory failed")
	}
	if err := os.MkdirAll(opts.AppDataDir, 0o700); err != nil {
		return Status{}, fmt.Errorf("create app data directory: %w", err)
	}
	url := fmt.Sprintf("http://%s:%s", opts.Host, opts.Port)
	if cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName)); err == nil && cfg.CoordinatorURL != "" {
		if (opts.Host == "" || opts.Host == defaultHost) && (opts.Port == "" || opts.Port == defaultPort) {
			url = cfg.CoordinatorURL
		}
	}
	return Status{
		AppDataDir:       opts.AppDataDir,
		ConfigPath:       filepath.Join(opts.AppDataDir, agentconfig.FileName),
		CoordinatorState: filepath.Join(opts.AppDataDir, "coordinator-state.json"),
		CoordinatorURL:   url,
	}, nil
}

func ensurePrivateIdentity(ctx context.Context, opts Options, client *coordinator.Client) (agentconfig.Config, bool, error) {
	configPath := filepath.Join(opts.AppDataDir, agentconfig.FileName)
	if cfg, err := agentconfig.Load(configPath); err == nil {
		if _, hbErr := client.SendHeartbeat(ctx, coordinator.HeartbeatRequest{
			GroupID:   cfg.GroupID,
			HostID:    cfg.HostID,
			HostToken: cfg.HostToken,
			Status:    "standby",
		}); hbErr != nil {
			if !isMissingPrivateGroupError(hbErr) {
				return agentconfig.Config{}, false, hbErr
			}
		} else {
			return cfg, false, nil
		}
	}

	return createPrivateIdentity(ctx, opts, client, configPath)
}

func createPrivateIdentity(
	ctx context.Context,
	opts Options,
	client *coordinator.Client,
	configPath string,
) (agentconfig.Config, bool, error) {
	created, err := client.CreateGroup(ctx, coordinator.CreateGroupRequest{
		Name:      "私人本地服务器组",
		OwnerName: opts.DisplayName,
	})
	if err != nil {
		return agentconfig.Config{}, false, err
	}
	joined, err := client.JoinGroup(ctx, created.GroupID, coordinator.JoinGroupRequest{
		AccessKey:   created.AccessKey,
		DisplayName: opts.DisplayName,
	})
	if err != nil {
		return agentconfig.Config{}, false, err
	}
	registered, err := client.RegisterHost(ctx, coordinator.RegisterHostRequest{
		GroupID:      created.GroupID,
		AccessKey:    created.AccessKey,
		MemberID:     joined.MemberID,
		DeviceName:   opts.DeviceName,
		Platform:     runtime.GOOS,
		AgentVersion: agentconfig.AgentVersion,
	})
	if err != nil {
		return agentconfig.Config{}, false, err
	}
	cfg := agentconfig.Config{
		CoordinatorURL: fmt.Sprintf("http://%s:%s", opts.Host, opts.Port),
		GroupID:        created.GroupID,
		MemberID:       joined.MemberID,
		HostID:         registered.HostID,
		HostToken:      registered.HostToken,
		DisplayName:    opts.DisplayName,
		DeviceName:     opts.DeviceName,
		Platform:       runtime.GOOS,
		AgentVersion:   agentconfig.AgentVersion,
	}
	if err := agentconfig.Save(configPath, cfg); err != nil {
		return agentconfig.Config{}, false, err
	}
	if err := saveJSON(filepath.Join(opts.AppDataDir, privateStateFileName), privateState{
		Mode:      "private-local",
		GroupID:   created.GroupID,
		AccessKey: created.AccessKey,
		HostID:    registered.HostID,
		HostToken: registered.HostToken,
	}); err != nil {
		return agentconfig.Config{}, false, err
	}
	return cfg, true, nil
}

func isMissingPrivateGroupError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "404") && strings.Contains(text, "group") &&
		(strings.Contains(text, "not found") || strings.Contains(text, "does not exist"))
}

func startCoordinator(opts Options, nodePath string, coordinatorPath string) (*os.Process, error) {
	cmd := exec.Command(nodePath, coordinatorPath)
	cmd.Dir = filepath.Dir(coordinatorPath)
	cmd.Env = append(os.Environ(),
		"HOST="+opts.Host,
		"PORT="+opts.Port,
		"ACBH_STORAGE_ROOT="+filepath.Join(opts.AppDataDir, "storage"),
		"ACBH_COORDINATOR_STATE_PATH="+filepath.Join(opts.AppDataDir, "coordinator-state.json"),
	)
	logDir := filepath.Join(opts.AppDataDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "coordinator.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	return cmd.Process, nil
}

func waitForHealth(ctx context.Context, client *coordinator.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := client.Health(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return fmt.Errorf("控制端启动超时，/health 不可用: %w", lastErr)
	}
	return errors.New("控制端启动超时，/health 不可用")
}

func resolveCoordinatorPath(opts Options) (string, error) {
	candidates := []string{}
	if opts.CoordinatorPath != "" {
		candidates = append(candidates, opts.CoordinatorPath)
	}
	exeDir := filepath.Dir(opts.ExecutablePath)
	candidates = append(candidates,
		filepath.Join(exeDir, "coordinator", "dist", "index.js"),
		filepath.Join(exeDir, "apps", "coordinator", "dist", "index.js"),
		filepath.Join(opts.WorkingDir, "apps", "coordinator", "dist", "index.js"),
	)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("找不到控制端入口文件 dist/index.js，请确认 release bundle 完整")
}

func resolveNode(opts Options) (string, error) {
	candidates := []string{}
	if opts.NodePath != "" {
		candidates = append(candidates, opts.NodePath)
	}
	exeDir := filepath.Dir(opts.ExecutablePath)
	if runtime.GOOS == "windows" {
		candidates = append(candidates,
			filepath.Join(exeDir, "runtime", "node", "node.exe"),
			filepath.Join(exeDir, "tools", "node", "node.exe"),
			filepath.Join(exeDir, "node", "node.exe"),
		)
	} else {
		candidates = append(candidates,
			filepath.Join(exeDir, "runtime", "node", "bin", "node"),
			filepath.Join(exeDir, "tools", "node", "bin", "node"),
			filepath.Join(exeDir, "node", "bin", "node"),
		)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return "", errors.New("未检测到 Node.js。请使用完整桌面版 bundle，或安装 Node 20 LTS")
	}
	return node, nil
}

func ensurePortAvailable(host string, port string) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("端口 %s 已被占用或不可用: %w", port, err)
	}
	return ln.Close()
}

func saveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func loadJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func log(out *bytes.Buffer, message string) {
	if out != nil {
		out.WriteString(message)
		out.WriteByte('\n')
	}
}
