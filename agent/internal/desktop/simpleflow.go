package desktop

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcimport"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

const (
	setupStateFileName       = "desktop-setup.json"
	environmentReportName    = "environment-report.json"
	packageInstallRecordName = "runtime-install-records.json"
)

type DesktopState string

const (
	StateUnconfigured                   DesktopState = "Unconfigured"
	StateBootstrapReady                 DesktopState = "BootstrapReady"
	StateBootstrapWaitingForOfflinePack DesktopState = "BootstrapWaitingForOfflinePack"
	StateServerNeedsLaunchSelection     DesktopState = "ServerNeedsLaunchSelection"
	StateJavaNeedsRepair                DesktopState = "JavaNeedsRepair"
	StateReadyToStart                   DesktopState = "ReadyToStart"
	StateReady                          DesktopState = "Ready"
	StateRunning                        DesktopState = "Running"
	StateError                          DesktopState = "Error"
)

type EnvironmentCheck struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	Impact         string `json:"impact,omitempty"`
	RepairAction   string `json:"repairAction,omitempty"`
	ManualSolution string `json:"manualSolution,omitempty"`
	LogPath        string `json:"logPath,omitempty"`
	Repairable     bool   `json:"repairable"`
}

type EnvironmentReport struct {
	OK                             bool               `json:"ok"`
	State                          DesktopState       `json:"state"`
	GeneratedAt                    string             `json:"generatedAt"`
	OS                             string             `json:"os"`
	Arch                           string             `json:"arch"`
	AppDataDir                     string             `json:"appDataDir"`
	RuntimeDir                     string             `json:"runtimeDir"`
	CacheDir                       string             `json:"cacheDir"`
	LogDir                         string             `json:"logDir"`
	RunDir                         string             `json:"runDir"`
	BackupDir                      string             `json:"backupDir"`
	EnvironmentReportPath          string             `json:"environmentReportPath"`
	Checks                         []EnvironmentCheck `json:"checks"`
	RequiredPackages               []string           `json:"requiredPackages"`
	Warnings                       []string           `json:"warnings"`
	LastSuccessfulBootstrapVersion string             `json:"lastSuccessfulBootstrapVersion,omitempty"`
}

type SetupState struct {
	SetupComplete   bool                   `json:"setupComplete"`
	Mode            string                 `json:"mode"`
	CoordinatorHost string                 `json:"coordinatorHost"`
	CoordinatorPort string                 `json:"coordinatorPort"`
	CoordinatorURL  string                 `json:"coordinatorUrl"`
	PublicGamePort  string                 `json:"publicGamePort"`
	PlayerAddress   string                 `json:"playerAddress"`
	GroupName       string                 `json:"groupName"`
	DisplayName     string                 `json:"displayName"`
	ServerDir       string                 `json:"serverDir"`
	ServerType      string                 `json:"serverType"`
	ServerCore      string                 `json:"serverCore"`
	ServerPort      string                 `json:"serverPort"`
	WorldDir        string                 `json:"worldDir"`
	JavaVersion     string                 `json:"javaVersion"`
	JavaPath        string                 `json:"javaPath"`
	LaunchProfile   mcimport.LaunchProfile `json:"launchProfile"`
	EULAAccepted    bool                   `json:"eulaAccepted"`
	UpdatedAt       string                 `json:"updatedAt"`
}

type ConfigureNetworkResult struct {
	OK             bool           `json:"ok"`
	State          DesktopState   `json:"state"`
	CoordinatorURL string         `json:"coordinatorUrl"`
	PlayerAddress  string         `json:"playerAddress"`
	BootstrapURL   string         `json:"bootstrapUrl"`
	Checks         []NetworkCheck `json:"checks,omitempty"`
	Message        string         `json:"message"`
	Warnings       []string       `json:"warnings,omitempty"`
}

type NetworkCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type InspectServerSetupResult struct {
	OK                  bool                      `json:"ok"`
	InspectionOK        bool                      `json:"inspectionOk"`
	LaunchReady         bool                      `json:"launchReady"`
	State               DesktopState              `json:"state"`
	Report              mcimport.Report           `json:"report"`
	LaunchProfile       mcimport.LaunchProfile    `json:"launchProfile"`
	Candidates          mcimport.LaunchCandidates `json:"candidates"`
	RequiredJavaVersion string                    `json:"requiredJavaVersion,omitempty"`
	DetectedJavaVersion string                    `json:"detectedJavaVersion,omitempty"`
	DetectedJavaPath    string                    `json:"detectedJavaPath,omitempty"`
	JavaCompatibility   string                    `json:"javaCompatibility,omitempty"`
	JavaVersion         string                    `json:"javaVersion"`
	JavaPath            string                    `json:"javaPath"`
	Message             string                    `json:"message"`
	BlockingReasons     []string                  `json:"blockingReasons,omitempty"`
	Warnings            []string                  `json:"warnings,omitempty"`
}

type SetupCompleteResult struct {
	OK      bool         `json:"ok"`
	State   DesktopState `json:"state"`
	Setup   SetupState   `json:"setup"`
	Message string       `json:"message"`
}

type SetupGroupResult struct {
	OK         bool         `json:"ok"`
	State      DesktopState `json:"state"`
	GroupID    string       `json:"groupId"`
	HostID     string       `json:"hostId"`
	MemberID   string       `json:"memberId"`
	InviteCode string       `json:"inviteCode,omitempty"`
	ExpiresAt  string       `json:"expiresAt,omitempty"`
	Message    string       `json:"message"`
}

type InviteListResult struct {
	OK      bool                       `json:"ok"`
	Invites []coordinator.PublicInvite `json:"invites"`
	Message string                     `json:"message"`
}

type RevokeInviteResult struct {
	OK       bool   `json:"ok"`
	InviteID string `json:"inviteId"`
	Message  string `json:"message"`
}

type AutoServerResult struct {
	OK        bool         `json:"ok"`
	State     DesktopState `json:"state"`
	Message   string       `json:"message"`
	Steps     []string     `json:"steps"`
	Warnings  []string     `json:"warnings,omitempty"`
	ErrorCode string       `json:"errorCode,omitempty"`
}

type EnvironmentPackageManifest struct {
	Version      int                      `json:"version"`
	ID           string                   `json:"id"`
	PackageID    string                   `json:"packageId"`
	Kind         string                   `json:"kind"`
	OS           string                   `json:"os"`
	Architecture string                   `json:"architecture"`
	Signature    string                   `json:"signature"`
	Files        []EnvironmentPackageFile `json:"files"`
}

type EnvironmentPackageFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type PackageVerificationResult struct {
	OK       bool                       `json:"ok"`
	Package  EnvironmentPackageManifest `json:"package"`
	Message  string                     `json:"message"`
	Errors   []string                   `json:"errors,omitempty"`
	Warnings []string                   `json:"warnings,omitempty"`
}

type PackageImportResult struct {
	OK             bool                       `json:"ok"`
	Package        EnvironmentPackageManifest `json:"package"`
	InstallRoot    string                     `json:"installRoot"`
	InstalledFiles []string                   `json:"installedFiles"`
	Message        string                     `json:"message"`
}

type packageInstallRecord struct {
	PackageID   string   `json:"packageId"`
	InstalledAt string   `json:"installedAt"`
	Files       []string `json:"files"`
}

func EnvironmentStatus(opts Options) (EnvironmentReport, error) {
	return CheckEnvironment(opts)
}

func RepairEnvironment(opts Options) (EnvironmentReport, error) {
	report, err := CheckEnvironment(opts)
	if err != nil {
		return report, err
	}
	report.Warnings = append(report.Warnings, "当前版本仅执行幂等目录/报告修复；缺失 Java 时请从 ACBH VPS 或离线包补全。")
	if err := saveJSON(filepath.Join(withDefaults(opts).AppDataDir, environmentReportName), report); err != nil {
		return report, err
	}
	return report, nil
}

func ClearEnvironmentCache(opts Options) (map[string]string, error) {
	opts = withDefaults(opts)
	cacheDir := filepath.Join(opts.AppDataDir, "cache")
	if err := os.RemoveAll(cacheDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}
	return map[string]string{"message": "ACBH 环境缓存已清理。", "cacheDir": cacheDir}, nil
}

func CheckEnvironment(opts Options) (EnvironmentReport, error) {
	opts = withDefaults(opts)
	now := time.Now().Format(time.RFC3339)
	report := EnvironmentReport{
		OK:                             true,
		State:                          StateBootstrapReady,
		GeneratedAt:                    now,
		OS:                             runtime.GOOS,
		Arch:                           runtime.GOARCH,
		AppDataDir:                     opts.AppDataDir,
		RuntimeDir:                     filepath.Join(opts.AppDataDir, "runtime"),
		CacheDir:                       filepath.Join(opts.AppDataDir, "cache"),
		LogDir:                         filepath.Join(opts.AppDataDir, "logs"),
		RunDir:                         filepath.Join(opts.AppDataDir, "run"),
		BackupDir:                      filepath.Join(opts.AppDataDir, "backup"),
		EnvironmentReportPath:          filepath.Join(opts.AppDataDir, environmentReportName),
		LastSuccessfulBootstrapVersion: agentconfig.AgentVersion,
	}
	add := func(id, status, message string, repairable bool, impact, repair, manual string) {
		if status != "passed" {
			report.OK = false
		}
		report.Checks = append(report.Checks, EnvironmentCheck{
			ID: id, Status: status, Message: message, Repairable: repairable,
			Impact: impact, RepairAction: repair, ManualSolution: manual,
			LogPath: filepath.Join(report.LogDir, "desktop.log"),
		})
	}

	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		add("platform", "passed", "系统兼容：Windows amd64。", false, "", "", "")
	} else {
		add("platform", "failed", "当前版本仅支持 Windows amd64 桌面模式。", false, "桌面 EXE 不保证可用。", "", "请使用 Windows amd64 设备运行。")
	}

	for _, dir := range []string{report.AppDataDir, report.RuntimeDir, report.CacheDir, report.LogDir, report.RunDir, report.BackupDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			add("dir_"+filepath.Base(dir), "failed", "无法创建目录："+dir, true, "初始化无法继续。", "重新创建目录。", "检查磁盘权限或选择便携目录。")
		}
	}
	if err := atomicWriteProbe(report.AppDataDir); err != nil {
		add("atomic_write", "failed", "数据目录不可原子写入："+err.Error(), true, "配置保存不安全。", "重新检测目录权限。", "换到可写目录或关闭占用文件的软件。")
	} else {
		add("atomic_write", "passed", "数据目录可写，配置可原子保存。", false, "", "", "")
	}

	if opts.ExecutablePath != "" && fileExists(opts.ExecutablePath) {
		add("desktop_exe", "passed", "Desktop EXE 存在。", false, "", "", "")
	} else {
		add("desktop_exe", "failed", "Desktop EXE 缺失或无法定位。", false, "无法从 GUI 启动。", "", "重新下载完整 Windows bundle。")
	}
	agentPath := opts.ExecutablePath
	if strings.Contains(strings.ToLower(filepath.Base(agentPath)), "desktop") {
		agentPath = filepath.Join(filepath.Dir(agentPath), "acbh-agent-windows-amd64.exe")
	}
	if fileExists(agentPath) {
		add("agent_exe", "passed", "Agent EXE 存在。", false, "", "", "")
	} else {
		add("agent_exe", "failed", "Agent EXE 缺失："+agentPath, false, "所有 GUI 命令不可用。", "", "重新下载完整 Windows bundle。")
	}
	guiPath := filepath.Join(filepath.Dir(opts.ExecutablePath), "scripts", "acbh-desktop-gui.ps1")
	if opts.ExecutablePath == "" {
		guiPath = filepath.Join(opts.WorkingDir, "scripts", "acbh-desktop-gui.ps1")
	}
	if fileExists(guiPath) {
		add("gui_script", "passed", "GUI 脚本存在。", false, "", "", "")
	} else {
		add("gui_script", "failed", "GUI 脚本缺失："+guiPath, false, "桌面界面无法打开。", "", "重新下载完整 Windows bundle。")
	}
	if runtime.GOOS == "windows" {
		add("dpapi", "passed", "Windows DPAPI 可用。", false, "", "", "")
	} else {
		add("dpapi", "warning", "非 Windows 平台跳过 DPAPI 检查。", false, "仅影响 Windows 桌面安全存储验证。", "", "")
		report.Warnings = append(report.Warnings, "当前 smoke 在非 Windows 交叉环境运行，DPAPI 检查被跳过。")
	}
	if time.Now().Year() < 2024 {
		add("system_time", "failed", "系统时间明显异常。", false, "TLS 和 token 有效期可能失败。", "", "请校准系统时间。")
	} else {
		add("system_time", "passed", "系统时间看起来正常。", false, "", "", "")
	}
	if _, err := resolveNode(opts); err == nil {
		add("private_node", "passed", "私有 Node runtime 可用。", false, "", "", "")
	} else {
		report.RequiredPackages = append(report.RequiredPackages, "acbh-runtime-base-windows-amd64")
		add("private_node", "warning", "未找到私有 Node runtime；远程公网模式不需要本地 Node。", true, "私人本地 Coordinator 模式不可用。", "从 bundle runtime 或离线包补全。", "导入 acbh-runtime-base-windows-amd64.zip。")
	}
	cleanupStalePID(filepath.Join(report.RunDir, daemonPIDFileName))
	cleanupStalePID(filepath.Join(report.RunDir, mcServerPIDFileName))
	if err := saveJSON(report.EnvironmentReportPath, report); err != nil {
		return report, err
	}
	if !report.OK && len(report.RequiredPackages) > 0 {
		report.State = StateBootstrapWaitingForOfflinePack
	}
	return report, nil
}

func ConfigureNetwork(opts Options, host string, coordinatorPort string, publicGamePort string) (ConfigureNetworkResult, error) {
	opts = withDefaults(opts)
	coordURL, publicHost, err := NormalizePublicCoordinatorInput(host, coordinatorPort)
	if err != nil {
		return ConfigureNetworkResult{OK: false, State: StateError, Message: err.Error()}, nil
	}
	if publicHost == "" {
		return ConfigureNetworkResult{OK: false, State: StateError, Message: "请输入公网服务器 IP 或域名。"}, nil
	}
	if publicGamePort == "" {
		publicGamePort = "25565"
	}
	if net.ParseIP(publicHost) == nil {
		if _, err := net.LookupHost(publicHost); err != nil {
			return ConfigureNetworkResult{OK: false, State: StateError, Message: "DNS 解析失败：" + err.Error()}, nil
		}
	}
	client, err := coordinator.NewClient(coordURL)
	warnings := []string{}
	checks := []NetworkCheck{}
	addCheck := func(id string, ok bool, message string) {
		status := "passed"
		if !ok {
			status = "warning"
			warnings = append(warnings, message)
		}
		checks = append(checks, NetworkCheck{ID: id, Status: status, Message: message})
	}
	if err != nil {
		return ConfigureNetworkResult{OK: false, State: StateError, Message: "Coordinator URL 无效：" + err.Error()}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	coordHost, coordPort := hostPortFromURL(coordURL)
	addCheck("tcp_6121", tcpCheck(coordHost, coordPort) == nil, "公网控制端 TCP "+coordPort+" 可连接")
	addCheck("health", client.Health(ctx) == nil, "公网控制端 /health 正常")
	manifestOK, runtimeOK, javaOK := checkBootstrapManifest(ctx, coordURL)
	addCheck("bootstrap_manifest", manifestOK, "Bootstrap manifest 可读取")
	addCheck("runtime_package", runtimeOK, "环境包可用")
	addCheck("java_package", javaOK, "Java package 可用")
	addCheck("tcp_25565", tcpCheck(publicHost, publicGamePort) == nil, "玩家入口 TCP "+publicGamePort+" 可连接")
	setup, _ := LoadSetup(opts)
	setup.Mode = "remote-public"
	setup.CoordinatorHost = publicHost
	setup.CoordinatorPort = coordPort
	setup.PublicGamePort = publicGamePort
	setup.CoordinatorURL = coordURL
	setup.PlayerAddress = publicHost + ":" + publicGamePort
	setup.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := SaveSetup(opts, setup); err != nil {
		return ConfigureNetworkResult{}, err
	}
	_ = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.Mode = "remote-public"
		cfg.CoordinatorURL = coordURL
		cfg.PublicEntry = setup.PlayerAddress
		cfg.RelayTarget = firstNonEmpty(cfg.RelayTarget, "127.0.0.1:"+publicGamePort)
		cfg.UI.LastCompletedStep = maxInt(cfg.UI.LastCompletedStep, 1)
	})
	return ConfigureNetworkResult{
		OK: len(warnings) == 0, State: StateBootstrapReady, CoordinatorURL: coordURL,
		PlayerAddress: setup.PlayerAddress, BootstrapURL: coordURL + "/v1/bootstrap/manifest",
		Checks: checks, Message: "公网服务器配置已保存。", Warnings: warnings,
	}, nil
}

func NormalizePublicCoordinatorInput(input string, coordinatorPort string) (coordinatorURL string, publicHost string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("请输入公网服务器 IP 或域名")
	}
	if coordinatorPort == "" {
		coordinatorPort = "6121"
	}
	if !strings.Contains(input, "://") {
		input = "http://" + input
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Hostname() == "" {
		return "", "", fmt.Errorf("公网服务器地址无效")
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" && parsed.Scheme == "http" {
		port = coordinatorPort
	}
	if port == "" && parsed.Scheme == "https" {
		coordinatorURL = parsed.Scheme + "://" + host
	} else {
		coordinatorURL = parsed.Scheme + "://" + net.JoinHostPort(host, port)
	}
	return strings.TrimRight(coordinatorURL, "/"), host, nil
}

func hostPortFromURL(raw string) (string, string) {
	parsed, _ := url.Parse(raw)
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return parsed.Hostname(), port
}

func tcpCheck(host string, port string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1200*time.Millisecond)
	if err != nil {
		return err
	}
	return conn.Close()
}

func checkBootstrapManifest(ctx context.Context, coordURL string) (bool, bool, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(coordURL, "/")+"/v1/bootstrap/manifest", nil)
	if err != nil {
		return false, false, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, false, false
	}
	var body struct {
		Packages []struct {
			PackageID string `json:"packageId"`
			ID        string `json:"id"`
			Available bool   `json:"available"`
		} `json:"packages"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&body); err != nil {
		return false, false, false
	}
	runtimeOK := false
	javaOK := false
	for _, pkg := range body.Packages {
		id := strings.ToLower(firstNonEmpty(pkg.PackageID, pkg.ID))
		if strings.Contains(id, "runtime") && pkg.Available {
			runtimeOK = true
		}
		if strings.Contains(id, "java") && pkg.Available {
			javaOK = true
		}
	}
	return true, runtimeOK, javaOK
}

func SetupCreateGroup(ctx context.Context, opts Options, groupName string, displayName string, coordinatorURL string) (SetupGroupResult, error) {
	opts = withDefaults(opts)
	groupName = strings.TrimSpace(groupName)
	displayName = strings.TrimSpace(displayName)
	if groupName == "" {
		groupName = "ACBH Server"
	}
	if displayName == "" {
		displayName = opts.DisplayName
	}
	if coordinatorURL == "" {
		setup, _ := LoadSetup(opts)
		coordinatorURL = setup.CoordinatorURL
	}
	if coordinatorURL == "" {
		coordinatorURL = fmt.Sprintf("http://%s:%s", opts.Host, opts.Port)
	}
	client, err := coordinator.NewClient(coordinatorURL)
	if err != nil {
		return SetupGroupResult{}, err
	}
	created, err := client.CreateGroup(ctx, coordinator.CreateGroupRequest{Name: groupName, OwnerName: displayName})
	if err != nil {
		return SetupGroupResult{OK: false, State: StateError, Message: "创建 Group 失败：" + err.Error()}, nil
	}
	joined, err := client.JoinGroup(ctx, created.GroupID, coordinator.JoinGroupRequest{AccessKey: created.AccessKey, DisplayName: displayName})
	if err != nil {
		return SetupGroupResult{OK: false, State: StateError, GroupID: created.GroupID, Message: "注册成员失败：" + err.Error()}, nil
	}
	registered, err := client.RegisterHost(ctx, coordinator.RegisterHostRequest{
		GroupID: created.GroupID, AccessKey: created.AccessKey, MemberID: joined.MemberID,
		DeviceName: opts.DeviceName, Platform: runtime.GOOS, AgentVersion: agentconfig.AgentVersion,
	})
	if err != nil {
		return SetupGroupResult{OK: false, State: StateError, GroupID: created.GroupID, MemberID: joined.MemberID, Message: "注册本机 Host 失败：" + err.Error()}, nil
	}
	setup, _ := LoadSetup(opts)
	serverCfg := serverConfigFromSetup(opts, setup)
	if existing, loadErr := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName)); loadErr == nil && existing.Server.Dir != "" {
		serverCfg = existing.Server
	}
	cfg := agentconfig.Config{
		CoordinatorURL: coordinatorURL, GroupID: created.GroupID, MemberID: joined.MemberID,
		HostID: registered.HostID, HostToken: registered.HostToken, DisplayName: displayName,
		DeviceName: opts.DeviceName, Platform: runtime.GOOS, AgentVersion: agentconfig.AgentVersion,
		Server: serverCfg,
	}
	if err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), cfg); err != nil {
		return SetupGroupResult{}, err
	}
	secrets := NewDefaultSecretStore(opts)
	_ = secrets.Put("accessKey", created.AccessKey)
	_ = secrets.Put("hostToken", registered.HostToken)
	setup.GroupName = groupName
	setup.DisplayName = displayName
	setup.CoordinatorURL = coordinatorURL
	setup.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = SaveSetup(opts, setup)
	_ = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.Mode = firstNonEmpty(setup.Mode, "remote-public")
		cfg.CoordinatorURL = coordinatorURL
		cfg.Group = DesktopGroupConfig{GroupID: created.GroupID, MemberID: joined.MemberID, HostID: registered.HostID, Role: "owner"}
		cfg.UI.LastCompletedStep = maxInt(cfg.UI.LastCompletedStep, 2)
	})
	invite, inviteErr := client.CreateInvite(ctx, created.GroupID, coordinator.CreateInviteRequest{
		AccessKey: created.AccessKey, ExpiresInSeconds: 30 * 60, OneTime: true,
	})
	result := SetupGroupResult{
		OK: true, State: StateBootstrapReady, GroupID: created.GroupID, MemberID: joined.MemberID,
		HostID: registered.HostID, Message: "Group 已创建，本机已注册。",
	}
	if inviteErr == nil {
		result.InviteCode = invite.InviteCode
		result.ExpiresAt = invite.ExpiresAt
	} else {
		result.Message += " 当前 Coordinator 不支持邀请码 API，请升级 VPS bundle。"
	}
	return result, nil
}

func SetupJoinGroup(ctx context.Context, opts Options, inviteCode string, displayName string, coordinatorURL string) (SetupGroupResult, error) {
	opts = withDefaults(opts)
	inviteCode = strings.TrimSpace(inviteCode)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = opts.DisplayName
	}
	if inviteCode == "" {
		return SetupGroupResult{OK: false, State: StateError, Message: "请输入邀请码。"}, nil
	}
	if coordinatorURL == "" {
		setup, _ := LoadSetup(opts)
		coordinatorURL = setup.CoordinatorURL
	}
	if coordinatorURL == "" {
		coordinatorURL = fmt.Sprintf("http://%s:%s", opts.Host, opts.Port)
	}
	client, err := coordinator.NewClient(coordinatorURL)
	if err != nil {
		return SetupGroupResult{}, err
	}
	joined, err := client.JoinInvite(ctx, coordinator.JoinInviteRequest{
		InviteCode: inviteCode, DisplayName: displayName, DeviceName: opts.DeviceName,
		Platform: runtime.GOOS, AgentVersion: agentconfig.AgentVersion,
	})
	if err != nil {
		return SetupGroupResult{OK: false, State: StateError, Message: "邀请码无效或已过期：" + err.Error()}, nil
	}
	setup, _ := LoadSetup(opts)
	serverCfg := serverConfigFromSetup(opts, setup)
	if existing, loadErr := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName)); loadErr == nil && existing.Server.Dir != "" {
		serverCfg = existing.Server
	}
	cfg := agentconfig.Config{
		CoordinatorURL: coordinatorURL, GroupID: joined.GroupID, MemberID: joined.MemberID,
		HostID: joined.HostID, HostToken: joined.HostToken, DisplayName: displayName,
		DeviceName: opts.DeviceName, Platform: runtime.GOOS, AgentVersion: agentconfig.AgentVersion,
		Server: serverCfg,
	}
	if err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), cfg); err != nil {
		return SetupGroupResult{}, err
	}
	secrets := NewDefaultSecretStore(opts)
	_ = secrets.Put("hostToken", joined.HostToken)
	setup.DisplayName = displayName
	setup.CoordinatorURL = coordinatorURL
	setup.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = SaveSetup(opts, setup)
	_ = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.Mode = firstNonEmpty(setup.Mode, "remote-public")
		cfg.CoordinatorURL = coordinatorURL
		cfg.Group = DesktopGroupConfig{GroupID: joined.GroupID, MemberID: joined.MemberID, HostID: joined.HostID, Role: "member"}
		cfg.UI.LastCompletedStep = maxInt(cfg.UI.LastCompletedStep, 2)
	})
	return SetupGroupResult{
		OK: true, State: StateBootstrapReady, GroupID: joined.GroupID, MemberID: joined.MemberID,
		HostID: joined.HostID, Message: "已加入 Group，本机已注册。",
	}, nil
}

func SetupCreateInvite(ctx context.Context, opts Options, expiresSeconds int, oneTime bool) (SetupGroupResult, error) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil {
		return SetupGroupResult{OK: false, State: StateUnconfigured, Message: "请先创建或加入服务器组。"}, nil
	}
	accessKey, _ := NewDefaultSecretStore(opts).Get("accessKey")
	if accessKey == "" {
		return SetupGroupResult{OK: false, State: StateError, GroupID: cfg.GroupID, Message: "当前设备不是 owner，不能生成邀请码。"}, nil
	}
	if expiresSeconds <= 0 {
		expiresSeconds = 30 * 60
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return SetupGroupResult{}, err
	}
	invite, err := client.CreateInvite(ctx, cfg.GroupID, coordinator.CreateInviteRequest{AccessKey: accessKey, ExpiresInSeconds: expiresSeconds, OneTime: oneTime})
	if err != nil {
		return SetupGroupResult{OK: false, State: StateError, GroupID: cfg.GroupID, Message: "生成邀请码失败。"}, nil
	}
	return SetupGroupResult{OK: true, State: StateBootstrapReady, GroupID: cfg.GroupID, InviteCode: invite.InviteCode, ExpiresAt: invite.ExpiresAt, Message: "邀请码已生成，仅显示一次。"}, nil
}

func SetupListInvites(ctx context.Context, opts Options) (InviteListResult, error) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil {
		return InviteListResult{OK: false, Message: "请先创建或加入服务器组。"}, nil
	}
	accessKey, _ := NewDefaultSecretStore(opts).Get("accessKey")
	if accessKey == "" {
		return InviteListResult{OK: false, Message: "当前设备不是 owner，不能查看邀请码。"}, nil
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return InviteListResult{}, err
	}
	list, err := client.ListInvites(ctx, cfg.GroupID, coordinator.ListInvitesRequest{AccessKey: accessKey})
	if err != nil {
		return InviteListResult{OK: false, Message: "读取邀请码列表失败。"}, nil
	}
	return InviteListResult{OK: true, Invites: list.Invites, Message: "邀请码列表已读取。"}, nil
}

func SetupRevokeInvite(ctx context.Context, opts Options, inviteID string) (RevokeInviteResult, error) {
	opts = withDefaults(opts)
	cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
	if err != nil {
		return RevokeInviteResult{OK: false, Message: "请先创建或加入服务器组。"}, nil
	}
	accessKey, _ := NewDefaultSecretStore(opts).Get("accessKey")
	if accessKey == "" {
		return RevokeInviteResult{OK: false, InviteID: inviteID, Message: "当前设备不是 owner，不能撤销邀请码。"}, nil
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return RevokeInviteResult{}, err
	}
	revoked, err := client.RevokeInvite(ctx, cfg.GroupID, coordinator.RevokeInviteRequest{AccessKey: accessKey, InviteID: inviteID})
	if err != nil {
		return RevokeInviteResult{OK: false, InviteID: inviteID, Message: "撤销邀请码失败。"}, nil
	}
	return RevokeInviteResult{OK: true, InviteID: revoked.InviteID, Message: "邀请码已撤销。"}, nil
}

func InspectServerForSetup(opts Options, serverDir string) (InspectServerSetupResult, error) {
	opts = withDefaults(opts)
	report, err := mcimport.Inspect(serverDir)
	if err != nil {
		return InspectServerSetupResult{OK: false, State: StateError, Message: err.Error()}, nil
	}
	return saveInspectedServer(opts, report)
}

func inspectServerState(report mcimport.Report, java JavaResolution) DesktopState {
	if !report.LaunchReady {
		return StateServerNeedsLaunchSelection
	}
	if java.JavaCompatibility == "incompatible" {
		return StateJavaNeedsRepair
	}
	return StateReadyToStart
}

func ServerLaunchCandidates(opts Options) (mcimport.LaunchCandidates, error) {
	opts = withDefaults(opts)
	setup, _ := LoadSetup(opts)
	if strings.TrimSpace(setup.ServerDir) == "" {
		return mcimport.LaunchCandidates{}, fmt.Errorf("请先选择 Minecraft 服务端目录")
	}
	report, err := mcimport.Inspect(setup.ServerDir)
	if err != nil {
		return mcimport.LaunchCandidates{}, err
	}
	return report.Candidates, nil
}

func SelectServerLaunch(opts Options, selectedPath string) (InspectServerSetupResult, error) {
	opts = withDefaults(opts)
	setup, _ := LoadSetup(opts)
	if strings.TrimSpace(setup.ServerDir) == "" {
		return InspectServerSetupResult{OK: false, State: StateUnconfigured, Message: "请先选择 Minecraft 服务端目录。"}, nil
	}
	report, err := mcimport.SelectLaunchProfile(setup.ServerDir, selectedPath)
	if err != nil {
		return InspectServerSetupResult{OK: false, State: StateServerNeedsLaunchSelection, Message: err.Error()}, nil
	}
	return saveInspectedServer(opts, report)
}

func CurrentLaunchProfile(opts Options) (mcimport.LaunchProfile, error) {
	opts = withDefaults(opts)
	setup, _ := LoadSetup(opts)
	if setup.LaunchProfile.Kind != "" {
		profile := setup.LaunchProfile
		report := mcimport.Report{
			ServerDir:           setup.ServerDir,
			ServerType:          profile.ServerType,
			JavaRequirement:     profile.RequiredJavaVersion,
			RequiredJavaVersion: profile.RequiredJavaVersion,
			LaunchProfile:       profile,
		}
		java := ResolveJavaForServer(opts, report)
		profile.JavaPath = java.DetectedJavaPath
		profile.RequiredJavaVersion = java.RequiredJavaVersion
		profile.DetectedJavaVersion = java.DetectedJavaVersion
		profile.JavaCompatibility = java.JavaCompatibility
		return profile, nil
	}
	if strings.TrimSpace(setup.ServerDir) == "" {
		return mcimport.LaunchProfile{}, fmt.Errorf("请先选择 Minecraft 服务端目录")
	}
	report, err := mcimport.Inspect(setup.ServerDir)
	if err != nil {
		return mcimport.LaunchProfile{}, err
	}
	java := ResolveJavaForServer(opts, report)
	report.LaunchProfile.JavaPath = java.DetectedJavaPath
	report.LaunchProfile.RequiredJavaVersion = java.RequiredJavaVersion
	report.LaunchProfile.DetectedJavaVersion = java.DetectedJavaVersion
	report.LaunchProfile.JavaCompatibility = java.JavaCompatibility
	return report.LaunchProfile, nil
}

func ClearLaunchProfile(opts Options) (map[string]any, error) {
	opts = withDefaults(opts)
	setup, _ := LoadSetup(opts)
	setup.ServerCore = ""
	setup.LaunchProfile = mcimport.LaunchProfile{}
	setup.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := SaveSetup(opts, setup); err != nil {
		return nil, err
	}
	if cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName)); err == nil {
		cfg.Server.Command = ""
		if err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), cfg); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "message": "已清除启动文件选择。"}, nil
}

func saveInspectedServer(opts Options, report mcimport.Report) (InspectServerSetupResult, error) {
	java := ResolveJavaForServer(opts, report)
	report.LaunchProfile.JavaPath = java.DetectedJavaPath
	report.LaunchProfile.RequiredJavaVersion = java.RequiredJavaVersion
	report.LaunchProfile.DetectedJavaVersion = java.DetectedJavaVersion
	report.LaunchProfile.JavaCompatibility = java.JavaCompatibility
	report.JavaRequirement = java.RequiredJavaVersion
	report.RequiredJavaVersion = java.RequiredJavaVersion
	state := inspectServerState(report, java)
	blockingReasons := append([]string{}, report.BlockingReasons...)
	if !report.EULAAccepted {
		blockingReasons = append(blockingReasons, "Minecraft EULA 尚未接受。")
	}
	if java.JavaCompatibility == "incompatible" {
		blockingReasons = append(blockingReasons, "当前 Java 版本与服务端要求不兼容。")
	}
	ok := report.LaunchReady && report.EULAAccepted && java.JavaCompatibility != "incompatible"
	message := "Minecraft 服务端目录已检测。"
	if !report.LaunchReady {
		message = "服务端目录已检测，但尚未识别启动方式。"
	}
	setup, _ := LoadSetup(opts)
	setup.ServerDir = report.ServerDir
	setup.ServerType = string(report.ServerType)
	setup.ServerCore = report.LaunchEntry
	setup.ServerPort = report.ServerPort
	setup.WorldDir = report.WorldDir
	setup.JavaVersion = java.DetectedJavaVersion
	setup.JavaPath = java.DetectedJavaPath
	setup.LaunchProfile = report.LaunchProfile
	setup.EULAAccepted = report.EULAAccepted
	setup.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := SaveSetup(opts, setup); err != nil {
		return InspectServerSetupResult{}, err
	}
	if cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName)); err == nil && report.LaunchReady {
		cfg.Server = serverConfigFromReport(opts, report)
		if err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), cfg); err != nil {
			return InspectServerSetupResult{}, err
		}
	}
	_ = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.LastServerDir = report.ServerDir
		cfg.LaunchProfile = desktopLaunchProfileFromMC(report.LaunchProfile)
		cfg.JavaPath = java.DetectedJavaPath
		cfg.UI.LastCompletedStep = maxInt(cfg.UI.LastCompletedStep, 3)
	})
	return InspectServerSetupResult{
		OK: ok, InspectionOK: report.InspectionOK, LaunchReady: report.LaunchReady, State: state, Report: report,
		LaunchProfile: report.LaunchProfile, Candidates: report.Candidates,
		RequiredJavaVersion: java.RequiredJavaVersion, DetectedJavaVersion: java.DetectedJavaVersion,
		DetectedJavaPath: java.DetectedJavaPath, JavaCompatibility: java.JavaCompatibility,
		JavaVersion: java.DetectedJavaVersion, JavaPath: java.DetectedJavaPath, Message: message,
		BlockingReasons: blockingReasons, Warnings: report.Warnings,
	}, nil
}

func serverConfigFromSetup(opts Options, setup SetupState) agentconfig.ServerConfig {
	if strings.TrimSpace(setup.ServerDir) == "" {
		return agentconfig.ServerConfig{}
	}
	if setup.LaunchProfile.Kind != "" && (setup.LaunchProfile.ScriptPath != "" || setup.LaunchProfile.JarPath != "") {
		entry := setup.LaunchProfile.ScriptPath
		if entry == "" {
			entry = setup.LaunchProfile.JarPath
		}
		opts = withDefaults(opts)
		return agentconfig.ServerConfig{
			Dir:         setup.ServerDir,
			Command:     mcimport.SuggestedCommand(entry),
			LogDir:      filepath.Join(opts.AppDataDir, "logs", "minecraft"),
			StopTimeout: (30 * time.Second).String(),
		}
	}
	if report, err := mcimport.Inspect(setup.ServerDir); err == nil {
		return serverConfigFromReport(opts, report)
	}
	opts = withDefaults(opts)
	return agentconfig.ServerConfig{
		Dir:         setup.ServerDir,
		LogDir:      filepath.Join(opts.AppDataDir, "logs", "minecraft"),
		StopTimeout: (30 * time.Second).String(),
	}
}

func serverConfigFromReport(opts Options, report mcimport.Report) agentconfig.ServerConfig {
	opts = withDefaults(opts)
	return agentconfig.ServerConfig{
		Dir:         report.ServerDir,
		Command:     report.SuggestedCommand,
		LogDir:      filepath.Join(opts.AppDataDir, "logs", "minecraft"),
		StopTimeout: (30 * time.Second).String(),
	}
}

type JavaResolution struct {
	RequiredJavaVersion string `json:"requiredJavaVersion,omitempty"`
	DetectedJavaVersion string `json:"detectedJavaVersion,omitempty"`
	DetectedJavaPath    string `json:"detectedJavaPath,omitempty"`
	JavaCompatibility   string `json:"javaCompatibility"`
}

func ResolveJavaForServer(opts Options, report mcimport.Report) JavaResolution {
	opts = withDefaults(opts)
	required := report.RequiredJavaVersion
	if required == "" {
		required = report.JavaRequirement
	}
	res := JavaResolution{RequiredJavaVersion: required, JavaCompatibility: "unknown"}
	candidates := []string{
		filepath.Join(report.ServerDir, "runtime", "bin", exeName("java")),
	}
	if required != "" {
		candidates = append(candidates, filepath.Join(opts.AppDataDir, "runtime", "java", required, "bin", exeName("java")))
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			res.DetectedJavaPath = candidate
			res.DetectedJavaVersion = DetectJavaVersion(candidate)
			res.JavaCompatibility = JavaCompatibility(required, res.DetectedJavaVersion, report.ServerType)
			return res
		}
	}
	if systemJava, err := exec.LookPath("java"); err == nil {
		res.DetectedJavaPath = systemJava
		res.DetectedJavaVersion = DetectJavaVersion(systemJava)
		res.JavaCompatibility = JavaCompatibility(required, res.DetectedJavaVersion, report.ServerType)
		return res
	}
	return res
}

func DetectJavaVersion(javaPath string) string {
	if strings.TrimSpace(javaPath) == "" {
		return ""
	}
	cmd := exec.Command(javaPath, "-version")
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return DetectJavaMajorVersionFromOutput(string(out))
}

func DetectJavaMajorVersionFromOutput(output string) string {
	lower := strings.ToLower(output)
	for _, marker := range []string{"version \"", "openjdk version \""} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			start := idx + len(marker)
			rest := output[start:]
			if end := strings.Index(rest, "\""); end >= 0 {
				return javaMajor(rest[:end])
			}
		}
	}
	fields := strings.Fields(output)
	for _, field := range fields {
		if strings.Contains(field, ".") || strings.Trim(field, "\"") == field {
			if major := javaMajor(strings.Trim(field, "\"")); major != "" {
				return major
			}
		}
	}
	return ""
}

func javaMajor(version string) string {
	version = strings.TrimSpace(version)
	if strings.HasPrefix(version, "1.8") {
		return "8"
	}
	var b strings.Builder
	for _, r := range version {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func JavaCompatibility(required string, detected string, serverType mcimport.ServerType) string {
	if required == "" || detected == "" {
		return "unknown"
	}
	req, reqErr := strconv.Atoi(required)
	got, gotErr := strconv.Atoi(detected)
	if reqErr != nil || gotErr != nil {
		return "unknown"
	}
	if got < req {
		return "incompatible"
	}
	if got == req {
		return "compatible"
	}
	switch serverType {
	case mcimport.Forge, mcimport.NeoForge, mcimport.Cleanroom, mcimport.CustomScript:
		return "warning"
	default:
		return "compatible"
	}
}

func CompleteSetup(opts Options) (SetupCompleteResult, error) {
	opts = withDefaults(opts)
	setup, _ := LoadSetup(opts)
	missing := []string{}
	if setup.CoordinatorURL == "" {
		missing = append(missing, "公网服务器")
	}
	if setup.GroupName == "" && setup.DisplayName == "" {
		missing = append(missing, "Group")
	}
	if setup.ServerDir == "" {
		missing = append(missing, "Minecraft 服务端目录")
	}
	if len(missing) > 0 {
		return SetupCompleteResult{OK: false, State: StateUnconfigured, Setup: setup, Message: "配置未完成：" + strings.Join(missing, "、")}, nil
	}
	setup.SetupComplete = true
	setup.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := SaveSetup(opts, setup); err != nil {
		return SetupCompleteResult{}, err
	}
	_ = syncDesktopConfig(opts, func(cfg *DesktopConfig) {
		cfg.UI.LastCompletedStep = maxInt(cfg.UI.LastCompletedStep, 4)
	})
	return SetupCompleteResult{OK: true, State: StateReady, Setup: setup, Message: "配置已完成，可以进入 ACBH。"}, nil
}

func LoadSetup(opts Options) (SetupState, error) {
	opts = withDefaults(opts)
	var setup SetupState
	if err := loadJSON(filepath.Join(opts.AppDataDir, setupStateFileName), &setup); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SetupState{Mode: "remote-public", CoordinatorPort: "6121", PublicGamePort: "25565"}, nil
		}
		return setup, err
	}
	return setup, nil
}

func SaveSetup(opts Options, setup SetupState) error {
	opts = withDefaults(opts)
	if setup.CoordinatorPort == "" {
		setup.CoordinatorPort = "6121"
	}
	if setup.PublicGamePort == "" {
		setup.PublicGamePort = "25565"
	}
	return saveJSON(filepath.Join(opts.AppDataDir, setupStateFileName), setup)
}

func VerifyEnvironmentPackage(file string) (PackageVerificationResult, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return PackageVerificationResult{OK: false, Message: "环境包无法打开：" + err.Error()}, nil
	}
	defer zr.Close()
	manifestData, err := readPackageManifestData(&zr.Reader)
	if err != nil {
		return PackageVerificationResult{OK: false, Message: err.Error()}, nil
	}
	var manifest EnvironmentPackageManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return PackageVerificationResult{OK: false, Message: "环境包 manifest 不是有效 JSON。"}, nil
	}
	errorsOut := validatePackageManifest(manifest)
	fileMap := map[string]*zip.File{}
	for _, f := range zr.File {
		cleanName := packageZipPath(f.Name)
		if !isPackageManifestPath(cleanName) {
			fileMap[cleanName] = f
		}
		if unsafeZipPath(f.Name) {
			errorsOut = append(errorsOut, "ZIP 包含不安全路径："+f.Name)
		}
	}
	for _, expected := range manifest.Files {
		clean := path.Clean(expected.Path)
		zf := fileMap[clean]
		if zf == nil {
			errorsOut = append(errorsOut, "环境包缺少文件："+expected.Path)
			continue
		}
		sum, size, err := zipFileSHA256(zf)
		if err != nil {
			errorsOut = append(errorsOut, "读取文件失败："+expected.Path)
			continue
		}
		if expected.Size >= 0 && expected.Size != size {
			errorsOut = append(errorsOut, "文件大小不匹配："+expected.Path)
		}
		if !strings.EqualFold(expected.SHA256, sum) {
			errorsOut = append(errorsOut, "SHA256 校验失败："+expected.Path)
		}
	}
	ok := len(errorsOut) == 0
	msg := "环境包校验通过。"
	if !ok {
		msg = "环境包校验失败。"
	}
	return PackageVerificationResult{OK: ok, Package: manifest, Message: msg, Errors: errorsOut}, nil
}

func ImportEnvironmentPackage(opts Options, file string) (PackageImportResult, error) {
	opts = withDefaults(opts)
	verified, err := VerifyEnvironmentPackage(file)
	if err != nil {
		return PackageImportResult{}, err
	}
	if !verified.OK {
		return PackageImportResult{OK: false, Package: verified.Package, Message: verified.Message}, nil
	}
	zr, err := zip.OpenReader(file)
	if err != nil {
		return PackageImportResult{}, err
	}
	defer zr.Close()
	installRoot := filepath.Join(opts.AppDataDir, "runtime")
	var installed []string
	for _, zf := range zr.File {
		cleanName := packageZipPath(zf.Name)
		if isPackageManifestPath(cleanName) || zf.FileInfo().IsDir() {
			continue
		}
		if unsafeZipPath(zf.Name) {
			return PackageImportResult{}, fmt.Errorf("ZIP 包含不安全路径: %s", zf.Name)
		}
		target := filepath.Join(installRoot, filepath.FromSlash(cleanName))
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(installRoot)+string(os.PathSeparator)) {
			return PackageImportResult{}, fmt.Errorf("ZIP Slip 被拒绝: %s", zf.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return PackageImportResult{}, err
		}
		if err := extractZipFile(zf, target); err != nil {
			return PackageImportResult{}, err
		}
		installed = append(installed, target)
	}
	record := packageInstallRecord{PackageID: packageID(verified.Package), InstalledAt: time.Now().Format(time.RFC3339), Files: installed}
	if err := appendInstallRecord(opts, record); err != nil {
		return PackageImportResult{}, err
	}
	return PackageImportResult{OK: true, Package: verified.Package, InstallRoot: installRoot, InstalledFiles: installed, Message: "离线环境包已导入。"}, nil
}

func StartAuto(ctx context.Context, opts Options) (AutoServerResult, error) {
	opts = withDefaults(opts)
	res := AutoServerResult{State: "CheckingLease", Steps: []string{"正在检查运行环境"}}
	env, err := CheckEnvironment(opts)
	if err != nil {
		return res, err
	}
	if !env.OK && env.State != StateBootstrapReady {
		res.State = StateBootstrapWaitingForOfflinePack
		res.Message = "当前缺少必要运行环境，并且无法连接环境下载源。请在其他设备下载 ACBH 离线环境包后导入。"
		res.ErrorCode = "bootstrap_not_ready"
		return res, nil
	}
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		res.State = StateUnconfigured
		res.Message = "请先完成 Group 和公网服务器配置。"
		res.ErrorCode = "not_configured"
		return res, nil
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return res, err
	}
	if daemon, err := startDaemon(opts, "standby"); err != nil {
		res.State = StateError
		res.Message = "心跳后台启动失败：" + err.Error()
		res.ErrorCode = "heartbeat_start_failed"
		return res, nil
	} else if daemon.Running {
		res.Steps = append(res.Steps, "standby 心跳后台已启动")
	}
	res.Steps = append(res.Steps, "正在申请主机资格")
	status, err := client.GetElectionStatus(ctx, coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
	if err != nil {
		res.State = StateError
		res.Message = "无法查询 current-host lease：" + err.Error()
		res.ErrorCode = "lease_status_failed"
		return res, nil
	}
	claimedByUs := status.CurrentHostID != nil && *status.CurrentHostID == cfg.HostID
	if status.CurrentHostID != nil && *status.CurrentHostID != cfg.HostID {
		res.State = StateReady
		res.Message = "服务器正在由其他 Host 运行，不能在本机启动。"
		res.ErrorCode = "other_host_running"
		return res, nil
	}
	var assignmentToken string
	var assignmentID string
	if !claimedByUs {
		check, err := client.CheckElectionTimeout(ctx, coordinator.ElectionAuthRequest{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
		if err != nil {
			res.State = StateError
			res.Message = "触发 election 失败：" + err.Error()
			res.ErrorCode = "election_timeout"
			return res, nil
		}
		if check.Election != nil && !check.Election.OK {
			res.State = StateError
			res.Message = "没有可接管的候选 Host。"
			res.ErrorCode = "takeover_not_assigned"
			return res, nil
		}
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			poll, err := client.PollTakeover(ctx, coordinator.TakeoverPollRequest{ElectionAuthRequest: coordinator.ElectionAuthRequest{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken}})
			if err != nil {
				res.State = StateError
				res.Message = "轮询 takeover assignment 失败：" + err.Error()
				res.ErrorCode = "takeover_not_assigned"
				return res, nil
			}
			if poll.Assignment != nil && poll.Assignment.TakeoverToken != "" {
				assignmentID = poll.Assignment.AssignmentID
				assignmentToken = poll.Assignment.TakeoverToken
				break
			}
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		if assignmentID == "" || assignmentToken == "" {
			res.State = StateError
			res.Message = "Coordinator 未返回分配给本机的 takeover token。"
			res.ErrorCode = "takeover_not_assigned"
			return res, nil
		}
		if _, err := client.AcceptTakeover(ctx, coordinator.TakeoverActionRequest{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken, AssignmentID: assignmentID, TakeoverToken: assignmentToken}); err != nil {
			res.State = StateError
			res.Message = "接受 takeover assignment 失败：" + err.Error()
			res.ErrorCode = "takeover_accept_failed"
			return res, nil
		}
	}
	res.State = "PullingArtifacts"
	res.Steps = append(res.Steps, "正在恢复世界存档")
	if restored, err := RestoreLatestWorldSnapshot(ctx, opts); err != nil {
		if isArtifactNotAvailable(err) {
			if localWorldExists(cfg.Server.Dir) {
				res.Warnings = append(res.Warnings, "first_world_bootstrap: Group 暂无历史世界快照，使用本机现有世界首次启动。")
			} else {
				res.State = StateError
				res.Message = "Group 暂无历史世界快照，且本机没有可启动的世界目录。"
				res.ErrorCode = "no_world_snapshot"
				return res, nil
			}
		} else {
			res.State = StateError
			res.Message = "世界快照恢复失败：" + err.Error()
			res.ErrorCode = "world_restore_failed"
			return res, nil
		}
	} else {
		res.Steps = append(res.Steps, "世界快照已恢复："+restored.SnapshotID)
	}
	res.State = "StartingMinecraft"
	res.Steps = append(res.Steps, "正在检查 Minecraft 服务端", "正在启动 Minecraft")
	started, err := StartServer(opts)
	if err != nil {
		return res, err
	}
	if !started.OK {
		res.State = StateError
		res.Message = started.Message
		res.ErrorCode = firstNonEmpty(started.ErrorCode, "minecraft_start_failed")
		if assignmentID != "" && assignmentToken != "" {
			_, _ = client.FailTakeover(ctx, coordinator.TakeoverFailRequest{TakeoverActionRequest: coordinator.TakeoverActionRequest{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken, AssignmentID: assignmentID, TakeoverToken: assignmentToken}, FailureReason: started.Message})
		}
		return res, nil
	}
	if err := waitForMinecraftPort(ctx, cfg.Server.Dir, 30*time.Second); err != nil {
		res.State = StateError
		res.Message = "Minecraft 本地端口未就绪：" + err.Error()
		res.ErrorCode = "minecraft_health_failed"
		if assignmentID != "" && assignmentToken != "" {
			_, _ = client.FailTakeover(ctx, coordinator.TakeoverFailRequest{TakeoverActionRequest: coordinator.TakeoverActionRequest{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken, AssignmentID: assignmentID, TakeoverToken: assignmentToken}, FailureReason: res.Message})
		}
		return res, nil
	}
	if assignmentID != "" && assignmentToken != "" {
		if _, err := client.CompleteTakeover(ctx, coordinator.TakeoverActionRequest{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken, AssignmentID: assignmentID, TakeoverToken: assignmentToken}); err != nil {
			res.State = StateError
			res.Message = "完成 current-host lease 失败：" + err.Error()
			res.ErrorCode = "takeover_complete_failed"
			return res, nil
		}
	}
	if _, err := StopDaemon(opts); err != nil {
		res.Warnings = append(res.Warnings, "切换 hosting 心跳前停止 standby daemon 失败："+err.Error())
	}
	if daemon, err := startDaemon(opts, "hosting"); err != nil {
		res.State = StateError
		res.Message = "切换 hosting 心跳失败：" + err.Error()
		res.ErrorCode = "heartbeat_start_failed"
		return res, nil
	} else if daemon.Running {
		res.Steps = append(res.Steps, "hosting 心跳后台已启动")
	}
	finalStatus, err := client.GetElectionStatus(ctx, coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
	if err != nil || finalStatus.CurrentHostID == nil || *finalStatus.CurrentHostID != cfg.HostID {
		res.State = StateError
		res.Message = "Relay 启动前 current-host 身份校验失败。"
		if err != nil {
			res.Message += " " + err.Error()
		}
		res.ErrorCode = "relay_health_failed"
		return res, nil
	}
	res.State = "StartingRelay"
	res.Steps = append(res.Steps, "正在启动公网中转")
	if err := StartRelayHostDetached(opts); err != nil {
		res.State = StateError
		res.Message = "公网中转启动失败：" + err.Error()
		res.ErrorCode = "relay_start_failed"
		return res, nil
	}
	res.OK = true
	res.State = StateRunning
	res.Steps = append(res.Steps, "服务器已运行")
	res.Message = "服务器已在此电脑启动。"
	return res, nil
}

func isArtifactNotAvailable(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no available artifact") ||
		strings.Contains(text, "artifact does not exist") ||
		strings.Contains(text, "404")
}

type WorldSnapshotPublishResult struct {
	SnapshotID       string `json:"snapshotId"`
	MissingObjects   int    `json:"missingObjects"`
	LogicalSize      int64  `json:"logicalSize"`
	UploadedSize     int64  `json:"uploadedSize"`
	ChangedFileCount int    `json:"changedFileCount"`
	DeletedFileCount int    `json:"deletedFileCount"`
}

func CreateStoppedWorldSnapshot(ctx context.Context, opts Options) (WorldSnapshotPublishResult, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return WorldSnapshotPublishResult{}, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return WorldSnapshotPublishResult{}, err
	}
	auth := coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken}
	status, err := client.GetElectionStatus(ctx, auth)
	if err != nil {
		return WorldSnapshotPublishResult{}, fmt.Errorf("检查 current host 失败: %w", err)
	}
	if status.CurrentHostID == nil || *status.CurrentHostID != cfg.HostID {
		return WorldSnapshotPublishResult{}, errors.New("not_current_host")
	}
	generation := status.CurrentHostGeneration
	var parent *worldbackup.Manifest
	if latest, err := client.GetLatestWorldBackup(ctx, auth, false); err == nil {
		parent = &latest.Manifest
	}
	snapshotID := "ws_" + time.Now().UTC().Format("20060102_150405")
	snapshot, err := worldbackup.BuildSnapshot(worldbackup.ScanOptions{
		ServerDir:      cfg.Server.Dir,
		AppDataDir:     opts.AppDataDir,
		SnapshotID:     snapshotID,
		GroupID:        cfg.GroupID,
		SourceHostID:   cfg.HostID,
		HostGeneration: generation,
		Parent:         parent,
		Consistent:     true,
	})
	if err != nil {
		return WorldSnapshotPublishResult{}, err
	}
	planned, err := client.PlanWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupPlanRequest{
		HostID:           cfg.HostID,
		HostToken:        cfg.HostToken,
		HostGeneration:   generation,
		ParentSnapshotID: snapshot.Manifest.ParentSnapshotID,
		Objects:          snapshot.Plan.Objects,
	})
	if err != nil {
		return WorldSnapshotPublishResult{}, err
	}
	bySHA := map[string]worldbackup.ChangedFile{}
	for _, changed := range snapshot.Plan.ChangedFiles {
		if _, ok := bySHA[changed.SHA256]; !ok {
			bySHA[changed.SHA256] = changed
		}
	}
	for _, object := range planned.MissingObjects {
		changed, ok := bySHA[object.SHA256]
		if !ok {
			return WorldSnapshotPublishResult{}, fmt.Errorf("coordinator requested unknown object %s", object.SHA256)
		}
		file, err := os.Open(changed.LocalPath)
		if err != nil {
			return WorldSnapshotPublishResult{}, err
		}
		_, uploadErr := client.UploadWorldObjectStream(ctx, auth, object.SHA256, file, object.Size)
		closeErr := file.Close()
		if uploadErr != nil {
			return WorldSnapshotPublishResult{}, uploadErr
		}
		if closeErr != nil {
			return WorldSnapshotPublishResult{}, closeErr
		}
	}
	commit, err := client.CommitWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupCommitRequest{
		HostID:         cfg.HostID,
		HostToken:      cfg.HostToken,
		HostGeneration: generation,
		Manifest:       snapshot.Manifest,
	})
	if err != nil {
		return WorldSnapshotPublishResult{}, err
	}
	if err := worldbackup.SaveIndexAtomic(opts.AppDataDir, snapshot.Index); err != nil {
		return WorldSnapshotPublishResult{}, err
	}
	return WorldSnapshotPublishResult{
		SnapshotID:       commit.SnapshotID,
		MissingObjects:   len(planned.MissingObjects),
		LogicalSize:      snapshot.Manifest.LogicalSize,
		UploadedSize:     snapshot.Manifest.UploadedSize,
		ChangedFileCount: snapshot.Manifest.ChangedFileCount,
		DeletedFileCount: snapshot.Manifest.DeletedFileCount,
	}, nil
}

func RestoreLatestWorldSnapshot(ctx context.Context, opts Options) (worldbackup.RestoreSummary, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	auth := coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken}
	latest, err := client.GetLatestWorldBackup(ctx, auth, true)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	downloader := func(ctx context.Context, objectID string) (io.ReadCloser, int64, error) {
		sha, ok := strings.CutPrefix(objectID, "sha256:")
		if !ok {
			return nil, 0, fmt.Errorf("unsupported object ID %s", objectID)
		}
		return client.DownloadWorldObjectStream(ctx, auth, sha)
	}
	return worldbackup.Restore(ctx, worldbackup.RestoreOptions{
		ServerDir:      cfg.Server.Dir,
		Manifest:       latest.Manifest,
		Downloader:     downloader,
		ConsistentOnly: true,
	})
}

func localWorldExists(serverDir string) bool {
	roots, err := worldbackup.ResolveWorldRoots(serverDir, nil)
	if err != nil {
		return false
	}
	for _, root := range roots {
		if info, err := os.Stat(filepath.Join(serverDir, filepath.FromSlash(root))); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func waitForMinecraftPort(ctx context.Context, serverDir string, timeout time.Duration) error {
	port := "25565"
	if report, err := mcimport.Inspect(serverDir); err == nil {
		if p := strings.TrimSpace(report.Properties["server-port"]); p != "" {
			port = p
		}
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		dialer := net.Dialer{Timeout: 500 * time.Millisecond}
		conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:"+port)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("timeout waiting for 127.0.0.1:%s", port)
}

func StopAuto(ctx context.Context, opts Options) (AutoServerResult, error) {
	opts = withDefaults(opts)
	res := AutoServerResult{State: "StoppingMinecraft", Steps: []string{"正在保存世界", "正在停止 Minecraft"}}
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		res.State = StateUnconfigured
		res.Message = "请先完成配置。"
		res.ErrorCode = "not_configured"
		return res, nil
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err == nil {
		status, statusErr := client.GetElectionStatus(ctx, coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken})
		if statusErr == nil && status.CurrentHostID != nil && *status.CurrentHostID != cfg.HostID {
			res.State = StateReady
			res.Message = "本机不是 current host，不能执行普通停止事务。"
			res.ErrorCode = "not_current_host"
			return res, nil
		}
	}
	stopped, err := StopServer(opts)
	if err != nil {
		return res, err
	}
	if !stopped.OK {
		if strings.Contains(stopped.Message, "没有记录") || strings.Contains(stopped.Message, "未运行") {
			res.OK = true
			res.State = StateReady
			res.Steps = append(res.Steps, "服务器未运行")
			res.Message = "服务器未运行，无需停止。"
			return res, nil
		}
		res.State = StateError
		res.Message = stopped.Message
		res.ErrorCode = "stop_failed"
		return res, nil
	}
	res.State = "SyncingWorld"
	res.Steps = append(res.Steps, "正在创建世界差量快照", "正在上传变化的世界对象")
	backup, err := CreateStoppedWorldSnapshot(ctx, opts)
	if err != nil {
		res.State = StateError
		res.OK = false
		res.Message = "发布 world snapshot 失败：" + err.Error()
		res.ErrorCode = "world_backup_failed"
		return res, nil
	}
	res.Steps = append(res.Steps, "世界快照已发布："+backup.SnapshotID)
	res.State = "StoppingRelay"
	res.Steps = append(res.Steps, "正在停止公网中转")
	if _, err := StopRelayHost(opts); err != nil {
		res.Warnings = append(res.Warnings, "停止 relay 失败："+err.Error())
	}
	if _, err := StopDaemon(opts); err != nil {
		res.Warnings = append(res.Warnings, "停止心跳后台失败："+err.Error())
	} else {
		res.Steps = append(res.Steps, "心跳后台已停止")
	}
	res.State = "ReleasingLease"
	res.Steps = append(res.Steps, "正在释放主机资格")
	res.OK = true
	res.State = StateReady
	res.Steps = append(res.Steps, "服务器已停止")
	res.Message = "服务器已停止；如 Coordinator 尚未支持显式释放 lease，请等待 TTL 或使用高级诊断。"
	return res, nil
}

func StartRelayHostDetached(opts Options) error {
	opts = withDefaults(opts)
	cp, err := CanPush(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("无法检查 current host: %w", err)
	}
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return err
	}
	if !cp.CanPush || cp.CurrentHostID != cfg.HostID {
		return errors.New("blocked_not_current_host")
	}
	if status, err := RelayHostStatus(opts); err == nil && status.Running {
		return nil
	}
	cleanupStalePID(relayHostPIDPath(opts))
	exe := opts.ExecutablePath
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return err
		}
	}
	logPath := filepath.Join(opts.AppDataDir, "logs", "relay-host.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "desktop", "relay", "start-host", "--app-data-dir", opts.AppDataDir, "--target-address", "127.0.0.1:25565", "--json")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureBackgroundProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status, statusErr := RelayHostStatus(opts)
		if statusErr == nil && status.Running {
			return nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	tail, _ := os.ReadFile(logPath)
	if len(tail) > 4096 {
		tail = tail[len(tail)-4096:]
	}
	return fmt.Errorf("relay_health_failed: relay host did not stay running; log tail: %s", strings.TrimSpace(string(tail)))
}

func ServerAutoStatus(ctx context.Context, opts Options) (map[string]any, error) {
	st, err := CurrentStatus(ctx, opts)
	if err != nil {
		return nil, err
	}
	state := string(StateReady)
	if st.MCServerRunning {
		state = string(StateRunning)
	}
	return map[string]any{"ok": true, "state": state, "status": st}, nil
}

func atomicWriteProbe(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".acbh-write-probe.tmp")
	final := filepath.Join(dir, ".acbh-write-probe")
	if err := os.WriteFile(tmp, []byte("ok"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(final)
}

func cleanupStalePID(pidPath string) {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if pid <= 0 || !processRunning(pid) {
		_ = os.Remove(pidPath)
	}
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func readPackageManifestData(zr *zip.Reader) ([]byte, error) {
	for _, f := range zr.File {
		if isPackageManifestPath(f.Name) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 2*1024*1024))
		}
	}
	return nil, errors.New("环境包缺少 acbh-package.json manifest")
}

func isPackageManifestPath(p string) bool {
	clean := packageZipPath(p)
	return clean == "acbh-package.json" || clean == "manifest.json"
}

func validatePackageManifest(m EnvironmentPackageManifest) []string {
	var errs []string
	if m.Version != 1 {
		errs = append(errs, "manifest version 必须为 1")
	}
	if packageID(m) == "" {
		errs = append(errs, "packageId 缺失")
	}
	if m.OS != "" && m.OS != runtime.GOOS {
		errs = append(errs, "环境包 OS 不匹配")
	}
	if m.Architecture != "" && m.Architecture != runtime.GOARCH {
		errs = append(errs, "环境包架构不匹配")
	}
	if strings.TrimSpace(m.Signature) == "" {
		errs = append(errs, "环境包签名缺失")
	}
	if len(m.Files) == 0 {
		errs = append(errs, "环境包文件清单为空")
	}
	for _, f := range m.Files {
		if unsafeZipPath(f.Path) {
			errs = append(errs, "manifest 包含不安全路径："+f.Path)
		}
		if len(f.SHA256) != 64 {
			errs = append(errs, "manifest SHA256 无效："+f.Path)
		}
	}
	return errs
}

func packageID(m EnvironmentPackageManifest) string {
	if m.PackageID != "" {
		return m.PackageID
	}
	return m.ID
}

func unsafeZipPath(p string) bool {
	clean := packageZipPath(p)
	return clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || path.IsAbs(clean) || strings.Contains(clean, ":")
}

func packageZipPath(p string) string {
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

func zipFileSHA256(zf *zip.File) (string, int64, error) {
	rc, err := zf.Open()
	if err != nil {
		return "", 0, err
	}
	defer rc.Close()
	h := sha256.New()
	size, err := io.Copy(h, rc)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func extractZipFile(zf *zip.File, target string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zf.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

func appendInstallRecord(opts Options, record packageInstallRecord) error {
	opts = withDefaults(opts)
	path := filepath.Join(opts.AppDataDir, packageInstallRecordName)
	var records []packageInstallRecord
	_ = loadJSON(path, &records)
	records = append(records, record)
	return saveJSON(path, records)
}

func CoordinatorURLFromHost(host string, port string) (string, error) {
	if port == "" {
		port = "6121"
	}
	u := "http://" + strings.TrimSpace(host) + ":" + port
	parsed, err := url.Parse(u)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("公网服务器地址无效")
	}
	return u, nil
}
