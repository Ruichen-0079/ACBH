package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/spf13/cobra"
)

const defaultHeartbeatInterval = 10 * time.Second

var rootCmd = &cobra.Command{
	Use:   "acbh-agent",
	Short: "ACBH Agent controls Minecraft host handoff on candidate devices",
	Long:  "ACBH Agent joins ACBH groups, registers host candidates, and reports heartbeat state to the Coordinator.",
}

func Execute() {
	rootCmd.AddCommand(newDoctorCmd(), newLoginCmd(), newHeartbeatCmd(), newDaemonCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	cmd.Flags().StringVar(&opts.accessKey, "access-key", "", "One-time group access key")
	cmd.Flags().StringVar(&opts.displayName, "name", "", "Member display name")
	cmd.Flags().StringVar(&opts.deviceName, "device-name", "", "Local device name")
	cmd.Flags().StringVar(&opts.platform, "platform", runtime.GOOS, "Host platform label")
	_ = cmd.MarkFlagRequired("group-id")
	_ = cmd.MarkFlagRequired("access-key")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

func newHeartbeatCmd() *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Send one host heartbeat to the Coordinator",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHeartbeat(cmd.Context(), cmd, status)
		},
	}

	cmd.Flags().StringVar(&status, "status", "standby", "Host status to report")
	return cmd
}

func newDaemonCmd() *cobra.Command {
	var status string
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Send host heartbeats until interrupted",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon(cmd.Context(), cmd, status, interval)
		},
	}

	cmd.Flags().StringVar(&status, "status", "standby", "Host status to report")
	cmd.Flags().DurationVar(&interval, "interval", defaultHeartbeatInterval, "Heartbeat interval")
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

func runLogin(ctx context.Context, cmd *cobra.Command, opts loginOptions) error {
	opts.coordinatorURL = strings.TrimRight(strings.TrimSpace(opts.coordinatorURL), "/")
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

func runHeartbeat(ctx context.Context, cmd *cobra.Command, status string) error {
	if !coordinator.ValidStatus(status) {
		return fmt.Errorf("invalid status %q", status)
	}

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	resp, err := sendHeartbeat(ctx, cfg, status)
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Heartbeat sent.")
	fmt.Fprintf(cmd.OutOrStdout(), "Config: %s\n", configPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Host ID: %s\n", resp.HostID)
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", resp.Status)
	return nil
}

func runDaemon(ctx context.Context, cmd *cobra.Command, status string, interval time.Duration) error {
	if !coordinator.ValidStatus(status) {
		return fmt.Errorf("invalid status %q", status)
	}
	if interval <= 0 {
		return errors.New("interval must be positive")
	}

	cfg, configPath, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(cmd.OutOrStdout(), "Starting heartbeat daemon with config %s\n", configPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Interval: %s\n", interval)
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", status)

	if err := daemonTick(ctx, cmd, cfg, status); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(cmd.OutOrStdout(), "Heartbeat daemon stopped.")
			return nil
		case <-ticker.C:
			if err := daemonTick(ctx, cmd, cfg, status); err != nil {
				return err
			}
		}
	}
}

func daemonTick(ctx context.Context, cmd *cobra.Command, cfg agentconfig.Config, status string) error {
	resp, err := sendHeartbeat(ctx, cfg, status)
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

func sendHeartbeat(ctx context.Context, cfg agentconfig.Config, status string) (coordinator.HeartbeatResponse, error) {
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return coordinator.HeartbeatResponse{}, err
	}

	req := coordinator.HeartbeatRequest{
		GroupID:               cfg.GroupID,
		HostID:                cfg.HostID,
		HostToken:             cfg.HostToken,
		Status:                status,
		LatestLocalSnapshotID: nil,
	}
	if err := coordinator.ValidateHeartbeatRequest(req); err != nil {
		return coordinator.HeartbeatResponse{}, err
	}

	return client.SendHeartbeat(ctx, req)
}
