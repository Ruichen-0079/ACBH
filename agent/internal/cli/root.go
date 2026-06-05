package cli

import (
	"context"
	"encoding/json"
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
	"github.com/Ruichen-0079/ACBH/agent/internal/artifactsync"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
	"github.com/Ruichen-0079/ACBH/agent/internal/scanner"
	"github.com/spf13/cobra"
)

const defaultHeartbeatInterval = 10 * time.Second

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
		newPushCmd(),
		newPullCmd(),
		newManifestCmd(),
	)
	return rootCmd
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

type pushOptions struct {
	manifestPath string
	serverDir    string
}

type pullOptions struct {
	artifactKind string
	artifactID   string
	outputDir    string
	applyDeletes bool
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

	summary, err := artifactsync.Push(ctx, artifactsync.PushOptions{
		ManifestPath: opts.manifestPath,
		ServerDir:    opts.serverDir,
		Config:       cfg,
		Client:       client,
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

	fmt.Fprintln(cmd.OutOrStdout(), "Scan complete.")
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact kind: %s\n", report.ArtifactKind)
	fmt.Fprintf(cmd.OutOrStdout(), "Server dir: %s\n", report.ServerDir)
	if opts.output != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Manifest written: %s\n", opts.output)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Included files: %d\n", report.IncludedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Ignored files: %d\n", report.IgnoredFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Unknown files: %d\n", report.UnknownFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted files: %d\n", report.DeletedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes: %d\n", report.TotalBytes)
	printSamples(cmd, "Ignored sample", report.IgnoredSample)
	printSamples(cmd, "Unknown sample", report.UnknownSample)
	return nil
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
