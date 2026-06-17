package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/artifactsync"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/desktop"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcimport"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcserver"
	"github.com/Ruichen-0079/ACBH/agent/internal/rcon"
	"github.com/Ruichen-0079/ACBH/agent/internal/scanner"
	"github.com/Ruichen-0079/ACBH/agent/internal/takeover"
	"github.com/spf13/cobra"
)

const (
	defaultHeartbeatInterval = 10 * time.Second
	defaultRCONTimeout       = 10 * time.Second
	defaultServerStopTimeout = 30 * time.Second
)

func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "acbh-agent",
		Short: "ACBH Agent controls Minecraft host handoff on candidate devices",
		Long:  "ACBH Agent joins ACBH groups, registers host candidates, reports heartbeat state, and generates local manifests.",
	}
	rootCmd.AddCommand(
		newDoctorCmd(),
		newLoginCmd(),
		newHeartbeatCmd(),
		newDaemonCmd(),
		newScanCmd(),
		newSafeSyncCmd(),
		newPushCmd(),
		newPullCmd(),
		newManifestCmd(),
		newServerCmd(),
		newElectionCmd(),
		newTakeoverCmd(),
		newGcCmd(),
		newRelayCmd(),
		newControlCmd(),
		newDesktopCmd(),
	)
	return rootCmd
}

func newDesktopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "desktop",
		Short: "Windows-friendly private local launcher",
		Long:  "管理私人本地模式：一键启动/关闭控制端和本地主机代理配置。默认只绑定 127.0.0.1。",
	}
	cmd.AddCommand(
		newDesktopStartCmd(),
		newDesktopStopCmd(),
		newDesktopStatusCmd(),
		newDesktopInspectServerCmd(),
		newDesktopImportServerCmd(),
		newDesktopResetCmd(),
	)
	return cmd
}

func newDesktopStartCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "start",
		Short: "一键启动控制端并自动注册本地主机",
		RunE: func(cmd *cobra.Command, args []string) error {
			var log bytes.Buffer
			status, err := desktop.Start(cmd.Context(), opts, &log)
			if log.Len() > 0 {
				fmt.Fprint(cmd.OutOrStdout(), log.String())
			}
			printDesktopStatus(cmd, status)
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.DisplayName, "name", "", "私人模式显示名称")
	cmd.Flags().StringVar(&opts.DeviceName, "device-name", "", "本机设备名称")
	return cmd
}

func newDesktopStopCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "一键关闭控制端",
		RunE: func(cmd *cobra.Command, args []string) error {
			var log bytes.Buffer
			status, err := desktop.Stop(opts, &log)
			if log.Len() > 0 {
				fmt.Fprint(cmd.OutOrStdout(), log.String())
			}
			printDesktopStatus(cmd, status)
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	return cmd
}

func newDesktopStatusCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "查看桌面私人模式状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := desktop.CurrentStatus(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(cmd, status)
			}
			printDesktopStatus(cmd, status)
			return nil
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopInspectServerCmd() *cobra.Command {
	var serverDir string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "inspect-server",
		Short: "检测 Minecraft 服务端目录",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := mcimport.Inspect(serverDir)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(cmd, report)
			}
			printMCImportReport(cmd, report)
			return nil
		},
	}
	cmd.Flags().StringVar(&serverDir, "server-dir", "", "Minecraft 服务端根目录")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	_ = cmd.MarkFlagRequired("server-dir")
	return cmd
}

func newDesktopImportServerCmd() *cobra.Command {
	var opts desktop.Options
	var serverDir string
	var stopTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "import-server",
		Short: "导入 Minecraft 服务端目录并保存到本地配置",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := mcimport.Inspect(serverDir)
			if err != nil {
				return err
			}
			printMCImportReport(cmd, report)
			if report.SuggestedCommand == "" {
				return errors.New("无法生成启动命令建议，请确认服务端 jar 文件名")
			}
			if stopTimeout <= 0 {
				stopTimeout = defaultServerStopTimeout
			}

			addDesktopConfigDefaults(&opts)
			configPath := filepath.Join(opts.AppDataDir, agentconfig.FileName)
			cfg, err := agentconfig.Load(configPath)
			if err != nil {
				return fmt.Errorf("导入前请先完成 desktop start 初始化本地主机配置: %w", err)
			}
			cfg.Server = agentconfig.ServerConfig{
				Dir:         report.ServerDir,
				Command:     report.SuggestedCommand,
				LogDir:      filepath.Join(opts.AppDataDir, "logs", "minecraft"),
				StopTimeout: stopTimeout.String(),
			}
			if err := agentconfig.Save(configPath, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "服务端目录已保存到本地配置: %s\n", configPath)
			return nil
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&serverDir, "server-dir", "", "Minecraft 服务端根目录")
	cmd.Flags().DurationVar(&stopTimeout, "stop-timeout", defaultServerStopTimeout, "停止服务端等待时间")
	_ = cmd.MarkFlagRequired("server-dir")
	return cmd
}

func newDesktopResetCmd() *cobra.Command {
	var opts desktop.Options
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "重置本地私人模式配置",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return errors.New("重置会删除本地 groupId/accessKey/hostToken；请加 --yes 确认")
			}
			if err := desktop.Reset(opts); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "本地私人模式配置已重置。")
			return nil
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&yes, "yes", false, "确认删除本地私人模式配置")
	return cmd
}

func addDesktopCommonFlags(cmd *cobra.Command, opts *desktop.Options) {
	cmd.Flags().StringVar(&opts.AppDataDir, "app-data-dir", "", "ACBH 数据目录，默认 %APPDATA%\\ACBH；便携模式使用 exe 同级 data")
	cmd.Flags().StringVar(&opts.CoordinatorPath, "coordinator", "", "coordinator dist/index.js 路径")
	cmd.Flags().StringVar(&opts.NodePath, "node", "", "Node.js 可执行文件路径")
	cmd.Flags().StringVar(&opts.Host, "host", "127.0.0.1", "控制端监听地址")
	cmd.Flags().StringVar(&opts.Port, "port", "6121", "控制端监听端口")
}

func addDesktopConfigDefaults(opts *desktop.Options) {
	if opts.ExecutablePath == "" {
		if exe, err := os.Executable(); err == nil {
			opts.ExecutablePath = exe
		}
	}
	if opts.AppDataDir == "" {
		if dir, err := agentconfig.ResolveAppDataDir(opts.ExecutablePath); err == nil {
			opts.AppDataDir = dir
		}
	}
}

func printDesktopStatus(cmd *cobra.Command, status desktop.Status) {
	if status.AppDataDir == "" {
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "桌面私人模式状态")
	fmt.Fprintf(cmd.OutOrStdout(), "数据目录: %s\n", status.AppDataDir)
	fmt.Fprintf(cmd.OutOrStdout(), "控制端地址: %s\n", status.CoordinatorURL)
	fmt.Fprintf(cmd.OutOrStdout(), "控制端状态文件: %s\n", status.CoordinatorState)
	if status.CoordinatorPID > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "控制端 PID: %d\n", status.CoordinatorPID)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "健康检查: %t\n", status.HealthOK)
	if status.GroupID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "当前服务器组: %s\n", status.GroupID)
	}
	if status.HostID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "当前主机: %s\n", status.HostID)
	}
	if status.Java != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Java: %s\n", status.Java)
	}
	if status.Node != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Node: %s\n", status.Node)
	}
	if status.LastError != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "最近错误: %s\n", status.LastError)
	}
}

func printMCImportReport(cmd *cobra.Command, report mcimport.Report) {
	fmt.Fprintln(cmd.OutOrStdout(), "Minecraft 服务端目录检测")
	fmt.Fprintf(cmd.OutOrStdout(), "目录: %s\n", report.ServerDir)
	fmt.Fprintf(cmd.OutOrStdout(), "类型: %s\n", report.ServerType)
	if report.SuggestedCommand != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "建议启动命令: %s\n", report.SuggestedCommand)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "server.properties: %t\n", report.HasProperties)
	fmt.Fprintf(cmd.OutOrStdout(), "eula.txt: %t\n", report.HasEULA)
	fmt.Fprintf(cmd.OutOrStdout(), "world: %t\n", report.HasWorld)
	fmt.Fprintf(cmd.OutOrStdout(), "mods: %t\n", report.HasMods)
	fmt.Fprintf(cmd.OutOrStdout(), "RCON: %s\n", report.RCON.ChineseMessage)
	for _, warning := range report.Warnings {
		fmt.Fprintf(cmd.OutOrStdout(), "提示: %s\n", warning)
	}
}

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start, stop, and inspect a local Minecraft server process",
	}
	cmd.AddCommand(
		newServerStartCmd(),
		newServerStopCmd(),
		newServerStatusCmd(),
		newServerRepairStateCmd(),
		newServerSuperviseCmd(),
	)
	return cmd
}

func newServerStartCmd() *cobra.Command {
	var opts serverStartOptions
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the configured local server process",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServerStart(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Minecraft server working directory")
	cmd.Flags().StringVar(&opts.command, "command", "", "User-provided server launch command")
	cmd.Flags().StringVar(&opts.logDir, "log-dir", "", "Directory for server stdout and stderr logs")
	cmd.Flags().DurationVar(&opts.stopTimeout, "stop-timeout", 0, "Graceful stop timeout before forced kill")
	return cmd
}

func newServerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Gracefully stop the managed local server process",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServerStop(cmd)
		},
	}
}

func newServerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report managed local server process status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServerStatus(cmd)
		},
	}
}

func newServerRepairStateCmd() *cobra.Command {
	var serverDir string
	cmd := &cobra.Command{
		Use:   "repair-state",
		Short: "Remove stale server state only after recorded processes are confirmed stopped",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServerRepairState(cmd, serverDir)
		},
	}
	cmd.Flags().StringVar(&serverDir, "server-dir", "", "Expected Minecraft server directory")
	return cmd
}

func newServerSuperviseCmd() *cobra.Command {
	var opts serverSupervisorOptions
	cmd := &cobra.Command{
		Use:    "supervise",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			startOpts := mcserver.StartOptions{
				ServerDir:   opts.serverDir,
				Command:     opts.command,
				CommandArgv: mcserver.DecodeCommandArgv(opts.commandArgv),
				LogDir:      opts.logDir,
				RuntimeDir:  opts.runtimeDir,
				StopTimeout: opts.stopTimeout,
			}
			return mcserver.RunSupervisor(cmd.Context(), mcserver.SupervisorOptions{
				StartOptions: startOpts,
				LockNonce:    opts.lockNonce,
			})
		},
	}
	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "")
	cmd.Flags().StringVar(&opts.command, "command", "", "")
	cmd.Flags().StringVar(&opts.commandArgv, "command-argv", "", "")
	cmd.Flags().StringVar(&opts.logDir, "log-dir", "", "")
	cmd.Flags().StringVar(&opts.runtimeDir, "runtime-dir", "", "")
	cmd.Flags().DurationVar(&opts.stopTimeout, "stop-timeout", defaultServerStopTimeout, "")
	cmd.Flags().StringVar(&opts.lockNonce, "lock-nonce", "", "")
	_ = cmd.MarkFlagRequired("server-dir")
	_ = cmd.MarkFlagRequired("command")
	_ = cmd.MarkFlagRequired("log-dir")
	_ = cmd.MarkFlagRequired("runtime-dir")
	_ = cmd.MarkFlagRequired("lock-nonce")
	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Print local host diagnostics",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := agentconfig.DefaultPath()
			if err != nil {
				return err
			}

			hostname, err := os.Hostname()
			if err != nil {
				hostname = "unknown"
			}

			javaPath, javaErr := exec.LookPath("java")
			javaAvailable := javaErr == nil
			if !javaAvailable {
				javaPath = "not found"
			}

			fmt.Fprintln(cmd.OutOrStdout(), "ACBH Agent doctor")
			fmt.Fprintf(cmd.OutOrStdout(), "OS: %s\n", runtime.GOOS)
			fmt.Fprintf(cmd.OutOrStdout(), "ARCH: %s\n", runtime.GOARCH)
			fmt.Fprintf(cmd.OutOrStdout(), "CPU cores: %d\n", runtime.NumCPU())
			fmt.Fprintf(cmd.OutOrStdout(), "Hostname: %s\n", hostname)
			fmt.Fprintf(cmd.OutOrStdout(), "Config path: %s\n", configPath)
			fmt.Fprintf(cmd.OutOrStdout(), "Config exists: %t\n", agentconfig.Exists(configPath))
			fmt.Fprintf(cmd.OutOrStdout(), "Java available: %t\n", javaAvailable)
			fmt.Fprintf(cmd.OutOrStdout(), "Java path: %s\n", javaPath)
			return nil
		},
	}
}

func newLoginCmd() *cobra.Command {
	var opts loginOptions
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Join a Coordinator group and register this device as a host candidate",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.coordinatorURL, "coordinator", "http://127.0.0.1:6121", "Coordinator base URL")
	cmd.Flags().StringVar(&opts.groupID, "group-id", "", "Coordinator group ID")
	cmd.Flags().StringVar(&opts.accessKey, "access-key", "", "One-time group access key (or set ACBH_ACCESS_KEY)")
	cmd.Flags().StringVar(&opts.displayName, "name", "", "Member display name")
	cmd.Flags().StringVar(&opts.deviceName, "device-name", "", "Local device name")
	cmd.Flags().StringVar(&opts.platform, "platform", runtime.GOOS, "Host platform label")
	_ = cmd.MarkFlagRequired("group-id")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func newHeartbeatCmd() *cobra.Command {
	var opts heartbeatOptions
	cmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Send one host heartbeat to the Coordinator",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHeartbeat(cmd.Context(), cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.status, "status", "standby", "Host status to report")
	cmd.Flags().StringVar(&opts.latestWorldSnapshot, "latest-world-snapshot", "", "Latest local world snapshot artifact ID")
	cmd.Flags().StringVar(&opts.latestServerPack, "latest-server-pack", "", "Latest local server pack artifact ID")
	cmd.Flags().StringVar(&opts.latestAdminState, "latest-admin-state", "", "Latest local admin state artifact ID")
	cmd.Flags().StringVar(&opts.javaAvailable, "java-available", "", "Override Java availability: true or false")
	cmd.Flags().StringVar(&opts.connectionHost, "connection-host", "", "Host address players can connect to")
	cmd.Flags().IntVar(&opts.connectionPort, "connection-port", 0, "Minecraft server connection port")
	cmd.Flags().StringVar(&opts.connectionNetwork, "connection-network", "", "Connection network label, such as tailscale")
	return cmd
}

func newDaemonCmd() *cobra.Command {
	var opts daemonOptions
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Send host heartbeats until interrupted",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon(cmd.Context(), cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.status, "status", "standby", "Host status to report")
	cmd.Flags().DurationVar(&opts.interval, "interval", defaultHeartbeatInterval, "Heartbeat interval")
	cmd.Flags().BoolVar(&opts.autoTakeover, "auto-takeover", false, "Enable automatic takeover assignment execution")
	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Minecraft server working directory (required with --auto-takeover)")
	cmd.Flags().StringVar(&opts.command, "command", "", "User-provided server launch command (required with --auto-takeover)")
	cmd.Flags().StringVar(&opts.logDir, "log-dir", "", "Directory for server stdout and stderr logs")
	cmd.Flags().DurationVar(&opts.stopTimeout, "stop-timeout", defaultServerStopTimeout, "Graceful stop timeout before forced kill")
	cmd.Flags().DurationVar(&opts.takeoverInterval, "takeover-interval", 0, "Takeover poll interval (defaults to heartbeat interval)")
	return cmd
}

type daemonOptions struct {
	status           string
	interval         time.Duration
	autoTakeover     bool
	serverDir        string
	command          string
	logDir           string
	stopTimeout      time.Duration
	takeoverInterval time.Duration
}

func newScanCmd() *cobra.Command {
	var opts scanOptions
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan a local server directory and generate an ACBH manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Minecraft server directory to scan")
	cmd.Flags().StringVar(&opts.artifactKind, "artifact-kind", "", "Artifact kind: world-snapshot, server-pack, or admin-state")
	cmd.Flags().StringVar(&opts.artifactID, "artifact-id", "", "Artifact ID for the generated manifest")
	cmd.Flags().StringVar(&opts.serverPackVersion, "server-pack-version", "", "Server pack version for world snapshots")
	cmd.Flags().StringVar(&opts.parentArtifactID, "parent-artifact-id", "", "Parent artifact ID")
	cmd.Flags().StringVar(&opts.groupID, "group-id", "", "Coordinator group ID")
	cmd.Flags().StringVar(&opts.creatorHostID, "creator-host-id", "", "Creator host ID")
	cmd.Flags().StringVar(&opts.previousManifest, "previous-manifest", "", "Previous manifest used to emit deleted entries")
	cmd.Flags().StringVar(&opts.output, "output", "", "Path to write manifest JSON")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("server-dir")
	_ = cmd.MarkFlagRequired("artifact-kind")
	_ = cmd.MarkFlagRequired("artifact-id")
	return cmd
}

func newSafeSyncCmd() *cobra.Command {
	var opts safeSyncOptions
	cmd := &cobra.Command{
		Use:   "safe-sync",
		Short: "Flush Minecraft through RCON, then generate a world snapshot manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSafeSync(cmd.Context(), cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Minecraft server directory to scan after RCON flush")
	cmd.Flags().StringVar(&opts.artifactKind, "artifact-kind", string(manifest.WorldSnapshot), "Artifact kind (must be world-snapshot)")
	cmd.Flags().StringVar(&opts.artifactID, "artifact-id", "", "Artifact ID for the generated world snapshot")
	cmd.Flags().StringVar(&opts.serverPackVersion, "server-pack-version", "", "Server pack version for the world snapshot")
	cmd.Flags().StringVar(&opts.parentArtifactID, "parent-artifact-id", "", "Parent artifact ID")
	cmd.Flags().StringVar(&opts.groupID, "group-id", "", "Coordinator group ID")
	cmd.Flags().StringVar(&opts.creatorHostID, "creator-host-id", "", "Creator host ID")
	cmd.Flags().StringVar(&opts.previousManifest, "previous-manifest", "", "Previous manifest used to emit deleted entries")
	cmd.Flags().StringVar(&opts.output, "output", "", "Path to write manifest JSON")
	cmd.Flags().StringVar(&opts.rconHost, "rcon-host", "127.0.0.1", "Minecraft RCON host")
	cmd.Flags().IntVar(&opts.rconPort, "rcon-port", 25575, "Minecraft RCON port")
	cmd.Flags().StringVar(&opts.rconPassword, "rcon-password", "", "Minecraft RCON password (or use ACBH_RCON_PASSWORD)")
	cmd.Flags().DurationVar(&opts.rconTimeout, "rcon-timeout", defaultRCONTimeout, "RCON connect and command timeout")
	_ = cmd.MarkFlagRequired("server-dir")
	_ = cmd.MarkFlagRequired("artifact-id")
	_ = cmd.MarkFlagRequired("server-pack-version")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Validate, diff, and inspect local ACBH manifests",
	}
	cmd.AddCommand(newManifestValidateCmd(), newManifestDiffCmd(), newManifestInspectCmd())
	return cmd
}

func newPushCmd() *cobra.Command {
	var opts pushOptions
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Upload manifest file objects and manifest metadata to the Coordinator",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.manifestPath, "manifest", "", "Manifest JSON to push")
	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Server directory containing manifest files")
	cmd.Flags().BoolVar(&opts.legacyJSONUpload, "legacy-json-upload", false, "Use compatibility JSON/base64 object upload")
	_ = cmd.MarkFlagRequired("manifest")
	_ = cmd.MarkFlagRequired("server-dir")
	return cmd
}

func newPullCmd() *cobra.Command {
	var opts pullOptions
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Download an artifact manifest and restore its file objects",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPull(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.artifactKind, "artifact-kind", "", "Artifact kind: world-snapshot, server-pack, or admin-state")
	cmd.Flags().StringVar(&opts.artifactID, "artifact-id", "latest", "Artifact ID to pull, or latest")
	cmd.Flags().StringVar(&opts.outputDir, "output-dir", "", "Directory to restore files into")
	cmd.Flags().BoolVar(&opts.applyDeletes, "apply-deletes", false, "Apply deleted manifest entries to local files")
	_ = cmd.MarkFlagRequired("artifact-kind")
	_ = cmd.MarkFlagRequired("output-dir")
	return cmd
}

func newManifestValidateCmd() *cobra.Command {
	var file string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a manifest JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := manifest.LoadFile(file)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(cmd, map[string]any{"ok": true, "file": file})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Manifest is valid: %s\n", file)
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Manifest file to validate")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newManifestDiffCmd() *cobra.Command {
	var oldPath string
	var newPath string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare two validated manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			oldManifest, err := manifest.LoadFile(oldPath)
			if err != nil {
				return fmt.Errorf("load old manifest: %w", err)
			}
			newManifest, err := manifest.LoadFile(newPath)
			if err != nil {
				return fmt.Errorf("load new manifest: %w", err)
			}
			diff, err := manifest.Diff(oldManifest, newManifest)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(cmd, diff)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Manifest diff: %s -> %s\n", diff.OldArtifactID, diff.NewArtifactID)
			fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind: %s\n", diff.ArtifactKind)
			fmt.Fprintf(cmd.OutOrStdout(), "Added: %d\n", diff.Added)
			fmt.Fprintf(cmd.OutOrStdout(), "Modified: %d\n", diff.Modified)
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %d\n", diff.Deleted)
			fmt.Fprintf(cmd.OutOrStdout(), "Unchanged: %d\n", diff.Unchanged)
			return nil
		},
	}
	cmd.Flags().StringVar(&oldPath, "old", "", "Old manifest file")
	cmd.Flags().StringVar(&newPath, "new", "", "New manifest file")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("old")
	_ = cmd.MarkFlagRequired("new")
	return cmd
}

func newManifestInspectCmd() *cobra.Command {
	var file string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Print a manifest summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := manifest.LoadFile(file)
			if err != nil {
				return err
			}
			inspection, err := manifest.Inspect(loaded)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printJSON(cmd, inspection)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Manifest: %s\n", file)
			fmt.Fprintf(cmd.OutOrStdout(), "Version: %d\n", inspection.ManifestVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind: %s\n", inspection.ArtifactKind)
			fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID: %s\n", inspection.ArtifactID)
			fmt.Fprintf(cmd.OutOrStdout(), "Group ID: %s\n", inspection.GroupID)
			fmt.Fprintf(cmd.OutOrStdout(), "Creator host ID: %s\n", inspection.CreatorHostID)
			if inspection.ServerPackVersion != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Server pack version: %s\n", *inspection.ServerPackVersion)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created at: %s\n", inspection.CreatedAt)
			fmt.Fprintf(cmd.OutOrStdout(), "Files: %d\n", inspection.FileCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted files: %d\n", inspection.DeletedCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Total bytes: %d\n", inspection.TotalBytes)
			fmt.Fprintln(cmd.OutOrStdout(), "File classes:")
			for class, count := range inspection.ClassCounts {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s: %d\n", class, count)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Manifest file to inspect")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

type loginOptions struct {
	coordinatorURL string
	groupID        string
	accessKey      string
	displayName    string
	deviceName     string
	platform       string
}

type heartbeatOptions struct {
	status              string
	latestWorldSnapshot string
	latestServerPack    string
	latestAdminState    string
	javaAvailable       string
	connectionHost      string
	connectionPort      int
	connectionNetwork   string
}

type scanOptions struct {
	serverDir         string
	artifactKind      string
	artifactID        string
	serverPackVersion string
	parentArtifactID  string
	groupID           string
	creatorHostID     string
	previousManifest  string
	output            string
	jsonOutput        bool
}

type safeSyncOptions struct {
	serverDir         string
	artifactKind      string
	artifactID        string
	serverPackVersion string
	parentArtifactID  string
	groupID           string
	creatorHostID     string
	previousManifest  string
	output            string
	rconHost          string
	rconPort          int
	rconPassword      string
	rconTimeout       time.Duration
}

type pushOptions struct {
	manifestPath     string
	serverDir        string
	legacyJSONUpload bool
}

type pullOptions struct {
	artifactKind string
	artifactID   string
	outputDir    string
	applyDeletes bool
}

type serverStartOptions struct {
	serverDir   string
	command     string
	logDir      string
	stopTimeout time.Duration
}

type serverSupervisorOptions struct {
	serverStartOptions
	runtimeDir  string
	lockNonce   string
	commandArgv string
}

func runServerStart(ctx context.Context, cmd *cobra.Command, opts serverStartOptions) error {
	resolved, err := resolveServerStartOptions(opts)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find Agent executable: %w", err)
	}
	state, err := mcserver.Start(ctx, executable, resolved)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Server started.")
	printServerState(cmd, state)
	return nil
}

func runServerStop(cmd *cobra.Command) error {
	runtimeDir, err := defaultServerRuntimeDir()
	if err != nil {
		return err
	}
	state, stopped, err := mcserver.Stop(runtimeDir)
	if err != nil {
		return err
	}
	if !stopped {
		fmt.Fprintln(cmd.OutOrStdout(), "Server is not running.")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Server stopped.")
	fmt.Fprintf(cmd.OutOrStdout(), "PID: %d\n", state.PID)
	return nil
}

func runServerStatus(cmd *cobra.Command) error {
	runtimeDir, err := defaultServerRuntimeDir()
	if err != nil {
		return err
	}
	status, err := mcserver.GetStatus(runtimeDir)
	if err != nil {
		return err
	}
	switch {
	case status.Running:
		fmt.Fprintln(cmd.OutOrStdout(), "Server status: running")
		printServerState(cmd, status.State)
	case status.Stale:
		if status.Unknown {
			fmt.Fprintln(cmd.OutOrStdout(), "Server status: unknown")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Server status: stale")
		}
		if status.State.PID > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "Recorded PID: %d\n", status.State.PID)
		} else if status.Lock.PID > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "Lock owner PID: %d\n", status.Lock.PID)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", status.Reason)
		fmt.Fprintln(cmd.OutOrStdout(), "Start is blocked. Run `acbh-agent server repair-state` only after confirming the server is stopped.")
	default:
		fmt.Fprintln(cmd.OutOrStdout(), "Server status: stopped")
	}
	return nil
}

func runServerRepairState(cmd *cobra.Command, serverDir string) error {
	runtimeDir, err := defaultServerRuntimeDir()
	if err != nil {
		return err
	}
	result, err := mcserver.RepairState(runtimeDir, serverDir)
	if err != nil {
		return err
	}
	if !result.Repaired {
		fmt.Fprintln(cmd.OutOrStdout(), "No server state or process lock needs repair.")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Server state repaired.")
	fmt.Fprintf(cmd.OutOrStdout(), "Removed state: %t\n", result.RemovedState)
	fmt.Fprintf(cmd.OutOrStdout(), "Removed lock: %t\n", result.RemovedLock)
	return nil
}

func resolveServerStartOptions(opts serverStartOptions) (mcserver.StartOptions, error) {
	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		return mcserver.StartOptions{}, err
	}
	configPath := filepath.Join(configDir, agentconfig.FileName)
	if agentconfig.Exists(configPath) {
		cfg, loadErr := agentconfig.Load(configPath)
		if loadErr != nil {
			return mcserver.StartOptions{}, fmt.Errorf("load config %s: %w", configPath, loadErr)
		}
		if opts.serverDir == "" {
			opts.serverDir = cfg.Server.Dir
		}
		if opts.command == "" {
			opts.command = cfg.Server.Command
		}
		if opts.logDir == "" {
			opts.logDir = cfg.Server.LogDir
		}
		if opts.stopTimeout == 0 && cfg.Server.StopTimeout != "" {
			opts.stopTimeout, err = time.ParseDuration(cfg.Server.StopTimeout)
			if err != nil {
				return mcserver.StartOptions{}, fmt.Errorf("parse server stopTimeout: %w", err)
			}
		}
	}
	if opts.serverDir == "" {
		return mcserver.StartOptions{}, errors.New("server directory is required; pass --server-dir or configure server.dir")
	}
	if opts.command == "" {
		return mcserver.StartOptions{}, errors.New("server command is required; pass --command or configure server.command")
	}
	if opts.logDir == "" {
		opts.logDir = filepath.Join(configDir, "logs")
	}
	if opts.stopTimeout == 0 {
		opts.stopTimeout = defaultServerStopTimeout
	}
	return mcserver.StartOptions{
		ServerDir:   opts.serverDir,
		Command:     opts.command,
		LogDir:      opts.logDir,
		RuntimeDir:  filepath.Join(configDir, "runtime"),
		StopTimeout: opts.stopTimeout,
	}, nil
}

func defaultServerRuntimeDir() (string, error) {
	configDir, err := agentconfig.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "runtime"), nil
}

func printServerState(cmd *cobra.Command, state mcserver.State) {
	fmt.Fprintf(cmd.OutOrStdout(), "PID: %d\n", state.PID)
	fmt.Fprintf(cmd.OutOrStdout(), "Server dir: %s\n", state.ServerDir)
	fmt.Fprintf(cmd.OutOrStdout(), "Command: %s\n", state.Command)
	fmt.Fprintf(cmd.OutOrStdout(), "Started at: %s\n", state.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(cmd.OutOrStdout(), "Stdout log: %s\n", state.StdoutLog)
	fmt.Fprintf(cmd.OutOrStdout(), "Stderr log: %s\n", state.StderrLog)
}

func runPush(ctx context.Context, cmd *cobra.Command, opts pushOptions) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return err
	}

	auth := coordinator.ArtifactAuth{
		GroupID:   cfg.GroupID,
		HostID:    cfg.HostID,
		HostToken: cfg.HostToken,
	}
	status, statusErr := client.GetElectionStatus(ctx, auth)
	if statusErr != nil {
		return fmt.Errorf("cannot verify current host state before publishing artifacts: %w", statusErr)
	}
	var generation *int
	if status.CurrentHostID != nil {
		if *status.CurrentHostID != cfg.HostID {
			return fmt.Errorf("this host is not the current host; only the current host may publish artifacts")
		}
		gen := status.CurrentHostGeneration
		generation = &gen
	}

	summary, err := artifactsync.Push(ctx, artifactsync.PushOptions{
		ManifestPath:     opts.manifestPath,
		ServerDir:        opts.serverDir,
		Config:           cfg,
		Client:           client,
		LegacyJSONUpload: opts.legacyJSONUpload,
		HostGeneration:   generation,
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Push complete.")
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind: %s\n", summary.ArtifactKind)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID: %s\n", summary.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "Uploaded objects: %d\n", summary.UploadedObjects)
	fmt.Fprintf(cmd.OutOrStdout(), "Skipped existing objects: %d\n", summary.SkippedObjects)
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted entries: %d\n", summary.DeletedEntries)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes uploaded: %d\n", summary.TotalBytesUploaded)
	fmt.Fprintf(cmd.OutOrStdout(), "Coordinator status: %s\n", summary.CoordinatorStatus)
	return nil
}

func runPull(ctx context.Context, cmd *cobra.Command, opts pullOptions) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return err
	}

	summary, err := artifactsync.Pull(ctx, artifactsync.PullOptions{
		ArtifactKind: manifest.ArtifactKind(opts.artifactKind),
		ArtifactID:   opts.artifactID,
		OutputDir:    opts.outputDir,
		ApplyDeletes: opts.applyDeletes,
		Config:       cfg,
		Client:       client,
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Pull complete.")
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind: %s\n", summary.ArtifactKind)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID: %s\n", summary.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "Written files: %d\n", summary.WrittenFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Downloaded objects: %d\n", summary.DownloadedObjects)
	fmt.Fprintf(cmd.OutOrStdout(), "Skipped files: %d\n", summary.SkippedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Pending deletes: %d\n", summary.PendingDeletes)
	fmt.Fprintf(cmd.OutOrStdout(), "Applied deletes: %d\n", summary.AppliedDeletes)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes restored: %d\n", summary.TotalBytes)
	return nil
}

func runScan(cmd *cobra.Command, opts scanOptions) error {
	groupID, creatorHostID, err := resolveScanIdentity(opts.groupID, opts.creatorHostID)
	if err != nil {
		return err
	}

	artifactKind := manifest.ArtifactKind(opts.artifactKind)
	manifestData, report, err := scanner.Scan(scanner.Options{
		ServerDir:            opts.serverDir,
		ArtifactKind:         artifactKind,
		ArtifactID:           opts.artifactID,
		GroupID:              groupID,
		CreatorHostID:        creatorHostID,
		ServerPackVersion:    opts.serverPackVersion,
		ParentArtifactID:     opts.parentArtifactID,
		PreviousManifestPath: opts.previousManifest,
		OutputPath:           opts.output,
	})
	if err != nil {
		return err
	}

	if opts.output != "" {
		if err := manifest.SaveFile(opts.output, manifestData); err != nil {
			return err
		}
	}

	if opts.jsonOutput {
		if opts.output == "" {
			return printJSON(cmd, manifestData)
		}
		return printJSON(cmd, report)
	}

	printScanSummary(cmd, "Scan complete.", report, opts.output)
	return nil
}

func runSafeSync(ctx context.Context, cmd *cobra.Command, opts safeSyncOptions) error {
	if manifest.ArtifactKind(opts.artifactKind) != manifest.WorldSnapshot {
		return fmt.Errorf("safe-sync only supports artifact kind %q", manifest.WorldSnapshot)
	}

	password := opts.rconPassword
	if password == "" {
		password = os.Getenv("ACBH_RCON_PASSWORD")
	}
	if password == "" {
		return errors.New("RCON password is required; pass --rcon-password or set ACBH_RCON_PASSWORD")
	}

	groupID, creatorHostID, err := resolveScanIdentity(opts.groupID, opts.creatorHostID)
	if err != nil {
		return err
	}

	response, err := rcon.Execute(ctx, rcon.Config{
		Host:     opts.rconHost,
		Port:     opts.rconPort,
		Password: password,
		Timeout:  opts.rconTimeout,
	}, "save-all flush")
	if err != nil {
		return fmt.Errorf("RCON save-all flush failed: %w", err)
	}
	if isRCONFailureResponse(response) {
		return fmt.Errorf(
			"RCON save-all flush returned failure: %s",
			redactSecret(strings.TrimSpace(response), password),
		)
	}

	manifestData, report, err := scanner.Scan(scanner.Options{
		ServerDir:            opts.serverDir,
		ArtifactKind:         manifest.WorldSnapshot,
		ArtifactID:           opts.artifactID,
		GroupID:              groupID,
		CreatorHostID:        creatorHostID,
		ServerPackVersion:    opts.serverPackVersion,
		ParentArtifactID:     opts.parentArtifactID,
		PreviousManifestPath: opts.previousManifest,
		OutputPath:           opts.output,
	})
	if err != nil {
		return err
	}
	if err := manifest.SaveFile(opts.output, manifestData); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "RCON save-all flush succeeded.")
	printScanSummary(cmd, "Safe sync complete.", report, opts.output)
	return nil
}

func printScanSummary(cmd *cobra.Command, heading string, report scanner.Report, output string) {
	fmt.Fprintln(cmd.OutOrStdout(), heading)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind: %s\n", report.ArtifactKind)
	fmt.Fprintf(cmd.OutOrStdout(), "Server dir: %s\n", report.ServerDir)
	if output != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Manifest written: %s\n", output)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Included files: %d\n", report.IncludedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Ignored files: %d\n", report.IgnoredFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Unknown files: %d\n", report.UnknownFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted files: %d\n", report.DeletedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes: %d\n", report.TotalBytes)
	printSamples(cmd, "Ignored sample", report.IgnoredSample)
	printSamples(cmd, "Unknown sample", report.UnknownSample)
}

func isRCONFailureResponse(response string) bool {
	normalized := strings.ToLower(strings.TrimSpace(response))
	if normalized == "" {
		return false
	}
	for _, marker := range []string{
		"unknown command",
		"incorrect argument",
		"not permitted",
		"permission denied",
		"failed",
		"error",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func redactSecret(value string, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

func resolveScanIdentity(groupID string, creatorHostID string) (string, string, error) {
	if groupID != "" && creatorHostID != "" {
		return groupID, creatorHostID, nil
	}

	cfg, _, err := loadConfig()
	if err != nil {
		missing := make([]string, 0, 2)
		if groupID == "" {
			missing = append(missing, "--group-id")
		}
		if creatorHostID == "" {
			missing = append(missing, "--creator-host-id")
		}
		return "", "", fmt.Errorf("local config unavailable; pass %s explicitly: %w", strings.Join(missing, " and "), err)
	}

	if groupID == "" {
		groupID = cfg.GroupID
	}
	if creatorHostID == "" {
		creatorHostID = cfg.HostID
	}

	return groupID, creatorHostID, nil
}

func runLogin(ctx context.Context, cmd *cobra.Command, opts loginOptions) error {
	opts.coordinatorURL = strings.TrimRight(strings.TrimSpace(opts.coordinatorURL), "/")
	accessKey, err := resolveLoginAccessKey(opts.accessKey)
	if err != nil {
		return err
	}
	opts.accessKey = accessKey
	if opts.deviceName == "" {
		hostname, err := os.Hostname()
		if err != nil || hostname == "" {
			opts.deviceName = opts.displayName + "-device"
		} else {
			opts.deviceName = hostname
		}
	}

	client, err := coordinator.NewClient(opts.coordinatorURL)
	if err != nil {
		return err
	}

	joined, err := client.JoinGroup(ctx, opts.groupID, coordinator.JoinGroupRequest{
		AccessKey:   opts.accessKey,
		DisplayName: opts.displayName,
	})
	if err != nil {
		return err
	}

	registered, err := client.RegisterHost(ctx, coordinator.RegisterHostRequest{
		GroupID:      opts.groupID,
		AccessKey:    opts.accessKey,
		MemberID:     joined.MemberID,
		DeviceName:   opts.deviceName,
		Platform:     opts.platform,
		AgentVersion: agentconfig.AgentVersion,
	})
	if err != nil {
		return err
	}

	configPath, err := agentconfig.DefaultPath()
	if err != nil {
		return err
	}
	if err := agentconfig.Save(configPath, agentconfig.Config{
		CoordinatorURL: opts.coordinatorURL,
		GroupID:        opts.groupID,
		MemberID:       joined.MemberID,
		HostID:         registered.HostID,
		HostToken:      registered.HostToken,
		DisplayName:    opts.displayName,
		DeviceName:     opts.deviceName,
		Platform:       opts.platform,
		AgentVersion:   agentconfig.AgentVersion,
	}); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Login successful.")
	fmt.Fprintf(cmd.OutOrStdout(), "Config saved: %s\n", configPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Group ID: %s\n", opts.groupID)
	fmt.Fprintf(cmd.OutOrStdout(), "Member ID: %s\n", joined.MemberID)
	fmt.Fprintf(cmd.OutOrStdout(), "Host ID: %s\n", registered.HostID)
	fmt.Fprintf(cmd.OutOrStdout(), "Status: host token stored locally\n")
	return nil
}

func resolveLoginAccessKey(flagValue string) (string, error) {
	accessKey := strings.TrimSpace(flagValue)
	if accessKey == "" {
		accessKey = strings.TrimSpace(os.Getenv("ACBH_ACCESS_KEY"))
	}
	if accessKey == "" {
		return "", errors.New("access key is required; pass --access-key or set ACBH_ACCESS_KEY")
	}
	return accessKey, nil
}

func runHeartbeat(ctx context.Context, cmd *cobra.Command, opts heartbeatOptions) error {
	if !coordinator.ValidStatus(opts.status) {
		return fmt.Errorf("invalid status %q", opts.status)
	}

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	req, err := buildHeartbeatRequest(cfg, opts)
	if err != nil {
		return err
	}
	resp, err := sendHeartbeat(ctx, cfg, req)
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Heartbeat sent.")
	fmt.Fprintf(cmd.OutOrStdout(), "Config: %s\n", configPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Host ID: %s\n", resp.HostID)
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", resp.Status)
	return nil
}

func runDaemon(ctx context.Context, cmd *cobra.Command, opts daemonOptions) error {
	if !coordinator.ValidStatus(opts.status) {
		return fmt.Errorf("invalid status %q", opts.status)
	}
	if opts.interval <= 0 {
		return errors.New("interval must be positive")
	}
	if opts.stopTimeout <= 0 {
		opts.stopTimeout = defaultServerStopTimeout
	}
	if opts.takeoverInterval <= 0 {
		opts.takeoverInterval = opts.interval
	}
	if opts.autoTakeover {
		if opts.serverDir == "" || opts.command == "" {
			return errors.New("--server-dir and --command are required when --auto-takeover is enabled")
		}
		if opts.logDir == "" {
			configDir, err := agentconfig.DefaultDir()
			if err != nil {
				return fmt.Errorf("resolve default log directory: %w", err)
			}
			opts.logDir = filepath.Join(configDir, "logs")
		}
	}

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return err
	}

	auth := coordinator.ElectionAuthRequest{
		GroupID:   cfg.GroupID,
		HostID:    cfg.HostID,
		HostToken: cfg.HostToken,
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Starting heartbeat daemon with config %s\n", configPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Interval: %s\n", opts.interval)
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", opts.status)
	if opts.autoTakeover {
		fmt.Fprintf(cmd.OutOrStdout(), "Auto-takeover: enabled\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Takeover interval: %s\n", opts.takeoverInterval)
		fmt.Fprintf(cmd.OutOrStdout(), "Server dir: %s\n", opts.serverDir)
		fmt.Fprintf(cmd.OutOrStdout(), "Log dir: %s\n", opts.logDir)
	}

	var (
		hosting            bool
		lastTakeoverCheck  time.Time
		takeoverInProgress atomic.Bool
	)

	if opts.autoTakeover {
		status, statusErr := client.GetElectionStatus(ctx, coordinator.ArtifactAuth{GroupID: auth.GroupID, HostID: auth.HostID, HostToken: auth.HostToken})
		if statusErr != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Warning: cannot check election status on startup: %v\n", statusErr)
		} else if status.CurrentHostID != nil && *status.CurrentHostID == cfg.HostID {
			hosting = true
			fmt.Fprintf(cmd.OutOrStdout(), "Already current host, sending hosting heartbeats and skipping takeover polling.\n")
		}
	}

	hbStatus := opts.status
	if hosting {
		hbStatus = "hosting"
	}
	if err := daemonTick(ctx, cmd, cfg, hbStatus); err != nil {
		return err
	}

	ticker := time.NewTicker(opts.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(cmd.OutOrStdout(), "Heartbeat daemon stopped.")
			return nil
		case <-ticker.C:
			hbStatus := opts.status
			if hosting {
				hbStatus = "hosting"
			}
			if err := daemonTick(ctx, cmd, cfg, hbStatus); err != nil {
				return err
			}

			if !opts.autoTakeover || hosting {
				continue
			}

			if takeoverInProgress.Load() {
				fmt.Fprintln(cmd.OutOrStdout(), "Takeover already in progress, skipping poll.")
				continue
			}

			if time.Since(lastTakeoverCheck) < opts.takeoverInterval {
				continue
			}
			lastTakeoverCheck = time.Now()

			takeoverInProgress.Store(true)
			completed, runErr := runAutoTakeover(ctx, cmd, cfg, client, auth,
				takeover.StatePath(filepath.Dir(configPath)), mcserver.StartOptions{
					ServerDir:   opts.serverDir,
					Command:     opts.command,
					LogDir:      opts.logDir,
					StopTimeout: opts.stopTimeout,
				})
			takeoverInProgress.Store(false)

			if runErr != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Auto-takeover failed: %v\n", runErr)
				continue
			}
			if completed {
				hosting = true
				fmt.Fprintln(cmd.OutOrStdout(), "Auto-takeover completed successfully. Daemon is now hosting.")
			}
		}
	}
}

func daemonTick(ctx context.Context, cmd *cobra.Command, cfg agentconfig.Config, status string) error {
	req, err := buildHeartbeatRequest(cfg, heartbeatOptions{status: status})
	if err != nil {
		return err
	}
	resp, err := sendHeartbeat(ctx, cfg, req)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s heartbeat ok host=%s status=%s\n", time.Now().Format(time.RFC3339), resp.HostID, resp.Status)
	return nil
}

func loadConfig() (agentconfig.Config, string, error) {
	configPath, err := agentconfig.DefaultPath()
	if err != nil {
		return agentconfig.Config{}, "", err
	}

	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return agentconfig.Config{}, "", fmt.Errorf("load config %s: %w", configPath, err)
	}

	return cfg, configPath, nil
}

func buildHeartbeatRequest(cfg agentconfig.Config, opts heartbeatOptions) (coordinator.HeartbeatRequest, error) {
	javaAvailable := false
	if _, err := exec.LookPath("java"); err == nil {
		javaAvailable = true
	}
	if opts.javaAvailable != "" {
		parsed, err := strconv.ParseBool(opts.javaAvailable)
		if err != nil {
			return coordinator.HeartbeatRequest{}, errors.New("--java-available must be true or false")
		}
		javaAvailable = parsed
	}

	latestArtifacts := make(map[string]string)
	if opts.latestWorldSnapshot != "" {
		latestArtifacts[string(manifest.WorldSnapshot)] = opts.latestWorldSnapshot
	}
	if opts.latestServerPack != "" {
		latestArtifacts[string(manifest.ServerPack)] = opts.latestServerPack
	}
	if opts.latestAdminState != "" {
		latestArtifacts[string(manifest.AdminState)] = opts.latestAdminState
	}

	var connection *coordinator.HostConnection
	if opts.connectionHost != "" || opts.connectionPort != 0 || opts.connectionNetwork != "" {
		if opts.connectionHost == "" || opts.connectionPort < 1 || opts.connectionPort > 65535 || opts.connectionNetwork == "" {
			return coordinator.HeartbeatRequest{}, errors.New("connection host, port, and network must be provided together")
		}
		connection = &coordinator.HostConnection{
			Host:    opts.connectionHost,
			Port:    opts.connectionPort,
			Network: opts.connectionNetwork,
		}
	}

	req := coordinator.HeartbeatRequest{
		GroupID:               cfg.GroupID,
		HostID:                cfg.HostID,
		HostToken:             cfg.HostToken,
		Status:                opts.status,
		LatestLocalSnapshotID: nil,
		HostScoreHints: &coordinator.HostScoreHints{
			CPUCores:      runtime.NumCPU(),
			JavaAvailable: &javaAvailable,
		},
		Connection: connection,
	}
	if len(latestArtifacts) > 0 {
		req.LatestLocalArtifacts = latestArtifacts
	}
	return req, nil
}

func sendHeartbeat(ctx context.Context, cfg agentconfig.Config, req coordinator.HeartbeatRequest) (coordinator.HeartbeatResponse, error) {
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return coordinator.HeartbeatResponse{}, err
	}

	if err := coordinator.ValidateHeartbeatRequest(req); err != nil {
		return coordinator.HeartbeatResponse{}, err
	}

	return client.SendHeartbeat(ctx, req)
}

func printJSON(cmd *cobra.Command, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}
	data = append(data, '\n')
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

func printSamples(cmd *cobra.Command, label string, samples []string) {
	if len(samples) == 0 {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", label)
	for _, sample := range samples {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", sample)
	}
}
