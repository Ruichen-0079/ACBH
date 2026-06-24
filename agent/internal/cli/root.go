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
		newWorldBackupCmd(),
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
		newDesktopEnvironmentCmd(),
		newDesktopSetupCmd(),
		newDesktopServerAutoCmd(),
		newDesktopStartServerCmd(),
		newDesktopStopServerCmd(),
		newDesktopRelayCmd(),
		newDesktopRemoteCmd(),
		newDesktopDaemonCmd(),
		newDesktopScanPackCmd(),
		newDesktopSafeSyncWorldCmd(),
		newDesktopPushLatestCmd(),
		newDesktopPullLatestCmd(),
		newDesktopRCONStatusCmd(),
		newDesktopLatestManifestCmd(),
		newDesktopCanPushCmd(),
		newDesktopTakeoverStatusCmd(),
		newDesktopInspectServerCmd(),
		newDesktopImportServerCmd(),
		newDesktopResetCmd(),
	)
	return cmd
}

func newDesktopEnvironmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "environment",
		Short: "检查、修复和导入 ACBH 桌面运行环境",
	}
	cmd.AddCommand(
		newDesktopEnvironmentCheckCmd(),
		newDesktopEnvironmentRepairCmd(),
		newDesktopEnvironmentStatusCmd(),
		newDesktopEnvironmentVerifyPackageCmd(),
		newDesktopEnvironmentImportPackCmd(),
		newDesktopEnvironmentClearCacheCmd(),
	)
	return cmd
}

func newDesktopEnvironmentCheckCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "check",
		Short: "执行基础环境检查并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := desktop.CheckEnvironment(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, report)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopEnvironmentRepairCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "执行幂等环境修复并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := desktop.RepairEnvironment(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, report)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopEnvironmentStatusCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "status",
		Short: "读取环境状态并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := desktop.EnvironmentStatus(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, report)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopEnvironmentVerifyPackageCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "verify-package",
		Short: "验证离线环境包并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.VerifyEnvironmentPackage(file)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "离线环境包路径")
	addIgnoredJSONFlag(cmd)
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newDesktopEnvironmentImportPackCmd() *cobra.Command {
	var opts desktop.Options
	var file string
	cmd := &cobra.Command{
		Use:   "import-pack",
		Short: "导入离线环境包并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ImportEnvironmentPackage(opts, file)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&file, "file", "", "离线环境包路径")
	addIgnoredJSONFlag(cmd)
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newDesktopEnvironmentClearCacheCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "clear-cache",
		Short: "清理 ACBH 环境下载缓存并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ClearEnvironmentCache(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "普通桌面用户四步配置向导使用的结构化命令",
	}
	cmd.AddCommand(
		newDesktopSetupCreateGroupCmd(),
		newDesktopSetupJoinGroupCmd(),
		newDesktopSetupCreateInviteCmd(),
		newDesktopSetupListInvitesCmd(),
		newDesktopSetupRevokeInviteCmd(),
		newDesktopSetupConfigureNetworkCmd(),
		newDesktopSetupInspectServerCmd(),
		newDesktopSetupCompleteCmd(),
		newDesktopSetupConfigCmd(),
		newDesktopSetupForgetConfigCmd(),
		newDesktopSetupResetWizardCmd(),
	)
	return cmd
}

func newDesktopSetupCreateGroupCmd() *cobra.Command {
	var opts desktop.Options
	var groupName, displayName, coordinatorURL string
	cmd := &cobra.Command{
		Use:   "create-group",
		Short: "创建 Group、注册本机并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.SetupCreateGroup(cmd.Context(), opts, groupName, displayName, coordinatorURL)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&groupName, "group-name", "ACBH Server", "Group 名称")
	cmd.Flags().StringVar(&displayName, "display-name", "", "本机昵称")
	cmd.Flags().StringVar(&coordinatorURL, "coordinator-url", "", "公网 Coordinator URL")
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopSetupJoinGroupCmd() *cobra.Command {
	var opts desktop.Options
	var inviteCode, displayName, coordinatorURL string
	cmd := &cobra.Command{
		Use:   "join-group",
		Short: "使用邀请码加入 Group、注册本机并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.SetupJoinGroup(cmd.Context(), opts, inviteCode, displayName, coordinatorURL)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&inviteCode, "invite-code", "", "ACBH 邀请码")
	cmd.Flags().StringVar(&displayName, "display-name", "", "本机昵称")
	cmd.Flags().StringVar(&coordinatorURL, "coordinator-url", "", "公网 Coordinator URL")
	addIgnoredJSONFlag(cmd)
	_ = cmd.MarkFlagRequired("invite-code")
	return cmd
}

func newDesktopSetupCreateInviteCmd() *cobra.Command {
	var opts desktop.Options
	var expires int
	var oneTime bool
	cmd := &cobra.Command{
		Use:   "create-invite",
		Short: "Owner 生成一次性邀请码并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.SetupCreateInvite(cmd.Context(), opts, expires, oneTime)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().IntVar(&expires, "expires-seconds", 30*60, "邀请码有效期秒数")
	cmd.Flags().BoolVar(&oneTime, "one-time", true, "邀请码是否一次性")
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopSetupListInvitesCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "list-invites",
		Short: "Owner 查看邀请码元数据并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.SetupListInvites(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopSetupRevokeInviteCmd() *cobra.Command {
	var opts desktop.Options
	var inviteID string
	cmd := &cobra.Command{
		Use:   "revoke-invite",
		Short: "Owner 撤销邀请码并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.SetupRevokeInvite(cmd.Context(), opts, inviteID)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&inviteID, "invite-id", "", "邀请码 ID")
	addIgnoredJSONFlag(cmd)
	_ = cmd.MarkFlagRequired("invite-id")
	return cmd
}

func newDesktopSetupConfigureNetworkCmd() *cobra.Command {
	var opts desktop.Options
	var host, coordinatorPort, publicGamePort string
	cmd := &cobra.Command{
		Use:   "configure-network",
		Short: "配置公网服务器 IP/域名并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ConfigureNetwork(opts, host, coordinatorPort, publicGamePort)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&host, "host-name", "", "公网服务器 IP 或域名")
	cmd.Flags().StringVar(&coordinatorPort, "coordinator-port", "6121", "Coordinator 端口")
	cmd.Flags().StringVar(&publicGamePort, "public-game-port", "25565", "玩家公网入口端口")
	addIgnoredJSONFlag(cmd)
	_ = cmd.MarkFlagRequired("host-name")
	return cmd
}

func newDesktopSetupInspectServerCmd() *cobra.Command {
	var opts desktop.Options
	var serverDir string
	cmd := &cobra.Command{
		Use:   "inspect-server",
		Short: "检测 Minecraft 服务端目录并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.InspectServerForSetup(opts, serverDir)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&serverDir, "server-dir", "", "Minecraft 服务端目录")
	addIgnoredJSONFlag(cmd)
	_ = cmd.MarkFlagRequired("server-dir")
	return cmd
}

func newDesktopSetupCompleteCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "完成四步配置并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.CompleteSetup(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopSetupConfigCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "config",
		Short: "读取桌面向导记忆配置并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.LoadDesktopConfig(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopSetupForgetConfigCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "forget-config",
		Short: "忘记此电脑桌面向导记忆并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := desktop.ForgetDesktopConfig(opts, desktop.NewDefaultSecretStore(opts)); err != nil {
				return err
			}
			return printJSON(cmd, map[string]any{"ok": true, "message": "已忘记此电脑配置。"})
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopSetupResetWizardCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "reset-wizard",
		Short: "重置四步向导进度并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ResetDesktopWizard(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopServerAutoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "简化桌面主按钮使用的一键开服/停服命令",
	}
	cmd.AddCommand(
		newDesktopServerStartAutoCmd(),
		newDesktopServerStopAutoCmd(),
		newDesktopServerAutoStatusCmd(),
		newDesktopServerCandidatesCmd(),
		newDesktopServerSelectLaunchCmd(),
		newDesktopServerLaunchProfileCmd(),
		newDesktopServerClearLaunchProfileCmd(),
	)
	return cmd
}

func newDesktopServerStartAutoCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "start-auto",
		Short: "执行一键开服事务并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.StartAuto(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopServerStopAutoCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "stop-auto",
		Short: "执行一键停服事务并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.StopAuto(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopServerAutoStatusCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "status",
		Short: "查询简化桌面服务器状态并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ServerAutoStatus(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopServerCandidatesCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "candidates",
		Short: "列出 Minecraft 服务端启动候选并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ServerLaunchCandidates(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopServerSelectLaunchCmd() *cobra.Command {
	var opts desktop.Options
	var selectedPath string
	cmd := &cobra.Command{
		Use:   "select-launch",
		Short: "保存 Minecraft 服务端启动脚本或核心 JAR 并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.SelectServerLaunch(opts, selectedPath)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&selectedPath, "path", "", "启动脚本或服务端核心 JAR 路径")
	addIgnoredJSONFlag(cmd)
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

func newDesktopServerLaunchProfileCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "launch-profile",
		Short: "读取当前 Minecraft 启动配置并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.CurrentLaunchProfile(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func newDesktopServerClearLaunchProfileCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "clear-launch-profile",
		Short: "清除当前 Minecraft 启动文件选择并输出纯 JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ClearLaunchProfile(opts)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	addIgnoredJSONFlag(cmd)
	return cmd
}

func addIgnoredJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", true, "输出 JSON（此命令始终输出 JSON）")
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

func newDesktopStartServerCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "start-server",
		Short: "启动已导入的 Minecraft 服务端（带 preflight 详细错误）",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, startErr := desktop.StartServer(opts)
			if startErr != nil {
				return startErr
			}
			if jsonOutput {
				_ = printJSON(cmd, result)
				if !result.OK {
					if result.Message != "" {
						fmt.Fprintln(cmd.ErrOrStderr(), result.Message)
					}
					return fmt.Errorf("start-server failed: %s", result.ErrorCode)
				}
				return nil
			}
			// non json text
			if result.OK {
				fmt.Fprintln(cmd.OutOrStdout(), "MC 服务端启动成功。")
				if result.PID > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "PID: %d\n", result.PID)
				}
				if result.LogFile != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "日志: %s\n", result.LogFile)
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "启动失败: %s\n", result.Message)
			if result.Suggestion != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "建议: %s\n", result.Suggestion)
			}
			return fmt.Errorf("start-server: %s", result.ErrorCode)
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出结构化 JSON（成功/失败均返回）")
	return cmd
}

func newDesktopStopServerCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "stop-server",
		Short: "停止由 ACBH desktop 启动的 MC 服务端（仅 pid file 记录的进程）",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, stopErr := desktop.StopServer(opts)
			if stopErr != nil {
				return stopErr
			}
			if jsonOutput {
				_ = printJSON(cmd, result)
				if !result.OK {
					if result.Message != "" {
						fmt.Fprintln(cmd.ErrOrStderr(), result.Message)
					}
					return fmt.Errorf("stop-server failed")
				}
				return nil
			}
			if result.OK {
				fmt.Fprintln(cmd.OutOrStdout(), "MC 服务端已停止。")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "停止操作: %s\n", result.Message)
			return nil
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出结构化 JSON")
	return cmd
}

func newDesktopRelayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay",
		Short: "公网中转 relay host 管理（仅 current host 可启动）",
	}
	cmd.AddCommand(newDesktopRelayStartHostCmd())
	cmd.AddCommand(newDesktopRelayStopHostCmd())
	cmd.AddCommand(newDesktopRelayStatusCmd())
	return cmd
}

func newDesktopRelayStartHostCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	var target string
	cmd := &cobra.Command{
		Use:   "start-host",
		Short: "启动公网中转 relay host（仅 current host 有效，自动发现玩家 tunnel sessions）",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.StartRelayHost(opts, target)
			if jsonOutput {
				_ = printJSON(cmd, result)
			}
			if err != nil {
				return err
			}
			if !jsonOutput {
				fmt.Fprintln(cmd.OutOrStdout(), result.Message)
			}
			if !result.Running {
				return fmt.Errorf("relay host start blocked or failed")
			}
			// block to keep manager running
			select {}
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	cmd.Flags().StringVar(&target, "target-address", "", "本地目标地址 (默认从已导入 MC 配置推导 127.0.0.1:25565)")
	return cmd
}

func newDesktopRelayStopHostCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "stop-host",
		Short: "停止公网中转 relay host",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.StopRelayHost(opts)
			if jsonOutput {
				_ = printJSON(cmd, result)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), result.Message)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopRelayStatusCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "公网中转 relay host 状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.RelayHostStatus(opts)
			if jsonOutput {
				_ = printJSON(cmd, result)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "running: %t pid: %d target: %s\n%s\n", result.Running, result.PID, result.Target, result.Message)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopRemoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "远程公网模式配置与切换（VPS Coordinator + relay + artifact sync）",
	}
	cmd.AddCommand(newDesktopRemoteConfigureCmd())
	cmd.AddCommand(newDesktopRemoteStatusCmd())
	// test, login, switch 可以后续或用现有 login/heartbeat
	return cmd
}

func newDesktopRemoteConfigureCmd() *cobra.Command {
	var opts desktop.Options
	var coordURL, publicEntry, groupID string
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "配置远程公网 Coordinator URL 和玩家入口",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := agentconfig.Load(filepath.Join(opts.AppDataDir, agentconfig.FileName))
			if err != nil {
				cfg = agentconfig.Config{}
			}
			if coordURL != "" {
				cfg.CoordinatorURL = coordURL
			}
			if groupID != "" {
				cfg.GroupID = groupID
			}
			// publicEntry 可存入扩展或 Server 注释；这里简单保存到 coordinator url 后
			if err := agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "远程公网配置已保存。Coordinator: %s\n", cfg.CoordinatorURL)
			return nil
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&coordURL, "coordinator-url", "", "公网 Coordinator URL (例如 http://vps:6121)")
	cmd.Flags().StringVar(&publicEntry, "public-entry", "", "玩家公网入口 (例如 1.2.3.4:25565)")
	cmd.Flags().StringVar(&groupID, "group-id", "", "组 ID")
	return cmd
}

func newDesktopRemoteStatusCmd() *cobra.Command {
	var opts desktop.Options
	cmd := &cobra.Command{
		Use:   "status",
		Short: "显示当前远程公网模式状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := desktop.CurrentStatus(cmd.Context(), opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "mode: %s\ncoordinatorUrl: %s\ndataSyncSource: %s\npublicEntry: %s\nisCurrentHost: %v\n", st.Mode, st.CoordinatorURL, "public-vps", st.PublicEntryMessage, st.IsCurrentHost != nil && *st.IsCurrentHost)
			return nil
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	return cmd
}

func newDesktopDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "管理后台服务 daemon",
	}
	cmd.AddCommand(
		newDesktopDaemonStartCmd(),
		newDesktopDaemonStopCmd(),
		newDesktopDaemonStatusCmd(),
	)
	return cmd
}

func newDesktopDaemonStartCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "启动后台服务 daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := desktop.StartDaemon(opts)
			if jsonOutput {
				_ = printJSON(cmd, state)
			} else {
				printDesktopDaemonState(cmd, state)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopDaemonStopCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "停止后台服务 daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := desktop.StopDaemon(opts)
			if jsonOutput {
				_ = printJSON(cmd, state)
			} else {
				printDesktopDaemonState(cmd, state)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopDaemonStatusCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "查看后台服务 daemon 状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := desktop.DaemonStatus(opts)
			if jsonOutput {
				_ = printJSON(cmd, state)
			} else {
				printDesktopDaemonState(cmd, state)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopScanPackCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "scan-pack",
		Short: "扫描服务端包 scan server-pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.ScanPack(opts)
			if jsonOutput {
				_ = printJSON(cmd, result)
			} else if err == nil {
				printDesktopScanResult(cmd, result)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopSafeSyncWorldCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	var rconPassword string
	cmd := &cobra.Command{
		Use:   "safe-sync-world",
		Short: "安全同步世界快照 safe-sync",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.SafeSyncWorld(cmd.Context(), opts, rconPassword)
			if jsonOutput {
				_ = printJSON(cmd, result)
			} else if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), result.RCONMessage)
				printDesktopScanResult(cmd, result.ScanResult)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&rconPassword, "rcon-password", "", "RCON 密码；建议使用 ACBH_RCON_PASSWORD 环境变量")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopPushLatestCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "push-latest",
		Short: "上传最近 manifest 对应的同步制品 push",
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := desktop.PushLatest(cmd.Context(), opts)
			if jsonOutput {
				_ = printJSON(cmd, summary)
			} else if err == nil {
				printDesktopPushSummary(cmd, summary)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopPullLatestCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	var artifactKind string
	var artifactID string
	var applyDeletes bool
	cmd := &cobra.Command{
		Use:   "pull-latest",
		Short: "拉取同步制品 pull",
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := desktop.PullLatest(cmd.Context(), opts, manifest.ArtifactKind(artifactKind), artifactID, applyDeletes)
			if jsonOutput {
				_ = printJSON(cmd, summary)
			} else if err == nil {
				printDesktopPullSummary(cmd, summary)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&artifactKind, "artifact-kind", string(manifest.WorldSnapshot), "制品类型：server-pack 或 world-snapshot")
	cmd.Flags().StringVar(&artifactID, "artifact-id", "latest", "制品 ID，默认 latest")
	cmd.Flags().BoolVar(&applyDeletes, "apply-deletes", false, "应用删除项；默认不删除本地文件")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopRCONStatusCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	var serverDir string
	cmd := &cobra.Command{
		Use:   "rcon-status",
		Short: "检查远程控制 RCON 状态",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := desktop.RCONStatusForServer(opts, serverDir)
			if jsonOutput {
				_ = printJSON(cmd, status)
			} else if err == nil {
				printDesktopRCONStatus(cmd, status)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&serverDir, "server-dir", "", "Minecraft 服务端目录；默认读取已导入配置")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopLatestManifestCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "latest-manifest",
		Short: "查看最近生成的 manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			latest, err := desktop.LatestManifest(opts)
			if jsonOutput {
				_ = printJSON(cmd, latest)
			} else if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "最近 manifest: %s\n", latest.Path)
				fmt.Fprintf(cmd.OutOrStdout(), "同步制品 Artifact: %s / %s\n", latest.ArtifactKind, latest.ArtifactID)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopCanPushCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "can-push",
		Short: "检查当前主机 Current Host 是否允许 push",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := desktop.CanPush(cmd.Context(), opts)
			if jsonOutput {
				_ = printJSON(cmd, result)
			} else if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), result.Reason)
				fmt.Fprintf(cmd.OutOrStdout(), "下一步: %s\n", result.NextStep)
			}
			if err == nil && !result.CanPush {
				return errors.New(result.Reason)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
}

func newDesktopTakeoverStatusCmd() *cobra.Command {
	var opts desktop.Options
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "takeover-status",
		Short: "接管演练 takeover 状态检查",
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := desktop.TakeoverStatusForDesktop(cmd.Context(), opts)
			if jsonOutput {
				_ = printJSON(cmd, status)
			} else if err == nil {
				printDesktopTakeoverStatus(cmd, status)
			}
			return err
		},
	}
	addDesktopCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
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

func printDesktopDaemonState(cmd *cobra.Command, state desktop.DaemonState) {
	fmt.Fprintln(cmd.OutOrStdout(), state.Message)
	if state.PID > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "daemon PID: %d\n", state.PID)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "pid file: %s\n", state.PIDPath)
	fmt.Fprintf(cmd.OutOrStdout(), "日志文件: %s\n", state.LogPath)
}

func printDesktopScanResult(cmd *cobra.Command, result desktop.ScanResult) {
	fmt.Fprintln(cmd.OutOrStdout(), result.Message)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind / 制品类型: %s\n", result.ArtifactKind)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID / 制品 ID: %s\n", result.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "Manifest / 文件清单: %s\n", result.ManifestPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Included files / 已包含文件: %d\n", result.Report.IncludedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Ignored files / 已忽略文件: %d\n", result.Report.IgnoredFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes / 总字节数: %d\n", result.Report.TotalBytes)
}

func printDesktopPushSummary(cmd *cobra.Command, summary artifactsync.PushSummary) {
	fmt.Fprintln(cmd.OutOrStdout(), "上传同步制品 push 完成。")
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind / 制品类型: %s\n", summary.ArtifactKind)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID / 制品 ID: %s\n", summary.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "Uploaded objects / 已上传对象: %d\n", summary.UploadedObjects)
	fmt.Fprintf(cmd.OutOrStdout(), "Skipped existing objects / 已跳过已有对象: %d\n", summary.SkippedObjects)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes uploaded / 上传字节数: %d\n", summary.TotalBytesUploaded)
}

func printDesktopPullSummary(cmd *cobra.Command, summary artifactsync.PullSummary) {
	fmt.Fprintln(cmd.OutOrStdout(), "拉取同步制品 pull 完成。")
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind / 制品类型: %s\n", summary.ArtifactKind)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID / 制品 ID: %s\n", summary.ArtifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "Written files / 写入文件: %d\n", summary.WrittenFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Downloaded objects / 下载对象: %d\n", summary.DownloadedObjects)
	fmt.Fprintf(cmd.OutOrStdout(), "Pending deletes / 待确认删除: %d\n", summary.PendingDeletes)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes / 总字节数: %d\n", summary.TotalBytes)
}

func printDesktopRCONStatus(cmd *cobra.Command, status desktop.RCONStatus) {
	fmt.Fprintf(cmd.OutOrStdout(), "MC 目录: %s\n", status.ServerDir)
	fmt.Fprintf(cmd.OutOrStdout(), "server.properties: %t\n", status.PropertiesExists)
	fmt.Fprintf(cmd.OutOrStdout(), "RCON 已启用 enabled: %t\n", status.Enabled)
	fmt.Fprintf(cmd.OutOrStdout(), "RCON 端口 port: %s\n", status.Port)
	fmt.Fprintf(cmd.OutOrStdout(), "RCON 密码是否已配置 password configured: %t\n", status.PasswordConfigured)
	fmt.Fprintln(cmd.OutOrStdout(), status.Message)
	if !status.Enabled || !status.PasswordConfigured || status.Port == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "建议配置:\n%s\n", status.SuggestedConfig)
	}
}

func printDesktopTakeoverStatus(cmd *cobra.Command, status desktop.TakeoverStatus) {
	fmt.Fprintf(cmd.OutOrStdout(), "服务器组 Group ID: %s\n", status.GroupID)
	fmt.Fprintf(cmd.OutOrStdout(), "本地主机 Host ID: %s\n", status.HostID)
	fmt.Fprintf(cmd.OutOrStdout(), "当前主机 Current Host: %s\n", status.CurrentHostID)
	fmt.Fprintf(cmd.OutOrStdout(), "当前主机代数 Generation: %d\n", status.CurrentHostGeneration)
	fmt.Fprintf(cmd.OutOrStdout(), "是否本机 current host: %t\n", status.IsCurrentHost)
	if status.ActiveTakeoverAssignment != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "takeover assignment: %s / %s\n", status.ActiveTakeoverAssignment.AssignmentID, status.ActiveTakeoverAssignment.Status)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "takeover assignment: 无")
	}
	fmt.Fprintln(cmd.OutOrStdout(), status.Message)
	fmt.Fprintf(cmd.OutOrStdout(), "下一步 CLI: %s\n", status.NextCLICommand)
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
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report managed local server process status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOutput {
				runtimeDir, err := defaultServerRuntimeDir()
				if err != nil {
					return err
				}
				status, err := mcserver.GetStatus(runtimeDir)
				if err != nil {
					return err
				}
				return printJSON(cmd, map[string]any{
					"running": status.Running,
					"stale":   status.Stale,
					"unknown": status.Unknown,
					"pid":     status.State.PID,
					"reason":  status.Reason,
				})
			}
			return runServerStatus(cmd)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "输出 JSON")
	return cmd
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
