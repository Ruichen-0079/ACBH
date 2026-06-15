package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/artifactsync"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
	"github.com/Ruichen-0079/ACBH/agent/internal/scanner"
	"github.com/spf13/cobra"
)

type bootstrapOptions struct {
	coordinatorURL string
	group          string
	serverDir      string
	artifactClass  string
	displayName    string
	deviceName     string
	platform       string
	force          bool
	allowNonEmpty  bool
}

func newBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create or join a group using a complete runnable server directory",
	}
	cmd.AddCommand(newBootstrapCreateGroupCmd(), newBootstrapJoinGroupCmd())
	return cmd
}

func newBootstrapCreateGroupCmd() *cobra.Command {
	var opts bootstrapOptions
	cmd := &cobra.Command{
		Use:   "create-group",
		Short: "Create a group, register this host, and push the first server-runtime artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBootstrapCreateGroup(cmd.Context(), cmd, opts)
		},
	}
	addBootstrapFlags(cmd, &opts)
	return cmd
}

func newBootstrapJoinGroupCmd() *cobra.Command {
	var opts bootstrapOptions
	cmd := &cobra.Command{
		Use:   "join-group",
		Short: "Join a group and restore the latest server-runtime artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBootstrapJoinGroup(cmd.Context(), cmd, opts)
		},
	}
	addBootstrapFlags(cmd, &opts)
	cmd.Flags().BoolVar(&opts.allowNonEmpty, "allow-non-empty", false, "Allow restoring into a non-empty server directory")
	return cmd
}

func addBootstrapFlags(cmd *cobra.Command, opts *bootstrapOptions) {
	cmd.Flags().StringVar(&opts.coordinatorURL, "coordinator", "http://127.0.0.1:6121", "Coordinator base URL")
	cmd.Flags().StringVar(&opts.group, "group", "", "Group name for create-group, or group ID for join-group")
	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Local runnable Minecraft server directory")
	cmd.Flags().StringVar(&opts.artifactClass, "artifact-class", string(manifest.ServerRuntime), "Artifact class (must be server-runtime)")
	cmd.Flags().StringVar(&opts.displayName, "name", "", "Member display name")
	cmd.Flags().StringVar(&opts.deviceName, "device-name", "", "Local device name")
	cmd.Flags().StringVar(&opts.platform, "platform", runtime.GOOS, "Host platform label")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Explicitly replace an existing local host profile")
	_ = cmd.MarkFlagRequired("group")
	_ = cmd.MarkFlagRequired("server-dir")
}

func runBootstrapCreateGroup(ctx context.Context, cmd *cobra.Command, opts bootstrapOptions) error {
	opts = normalizeBootstrapOptions(opts)
	configPath, configDir, err := prepareBootstrap(opts)
	if err != nil {
		return err
	}
	if err := preflightBootstrapServerRuntime(opts); err != nil {
		return err
	}
	client, err := coordinator.NewClient(strings.TrimRight(strings.TrimSpace(opts.coordinatorURL), "/"))
	if err != nil {
		return err
	}

	created, err := client.CreateGroup(ctx, coordinator.CreateGroupRequest{
		Name:      opts.group,
		OwnerName: opts.displayName,
	})
	if err != nil {
		return fmt.Errorf("create group: %w", err)
	}
	registered, err := client.RegisterHost(ctx, coordinator.RegisterHostRequest{
		GroupID:      created.GroupID,
		AccessKey:    created.AccessKey,
		MemberID:     created.OwnerMemberID,
		DeviceName:   opts.deviceName,
		Platform:     opts.platform,
		AgentVersion: agentconfig.AgentVersion,
	})
	if err != nil {
		return fmt.Errorf("register first host: %w", err)
	}

	cfg := bootstrapConfig(opts, created.GroupID, created.OwnerMemberID, registered)
	recoveryDir, err := saveBootstrapRecovery(configDir, cfg, created.AccessKey)
	if err != nil {
		return fmt.Errorf("save bootstrap recovery credentials: %w", err)
	}

	auth := coordinator.ArtifactAuth{
		GroupID: created.GroupID, HostID: registered.HostID, HostToken: registered.HostToken,
	}
	status, err := client.GetElectionStatus(ctx, auth)
	if err != nil {
		return bootstrapRecoveryError(recoveryDir, "read initial group generation", err)
	}
	artifactID := newServerRuntimeArtifactID()
	manifestPath := filepath.Join(recoveryDir, artifactID+".manifest.json")
	generation := status.CurrentHostGeneration + 1
	runtimeManifest, report, err := scanner.Scan(scanner.Options{
		ServerDir:     opts.serverDir,
		ArtifactKind:  manifest.ServerRuntime,
		ArtifactID:    artifactID,
		GroupID:       created.GroupID,
		CreatorHostID: registered.HostID,
		Generation:    &generation,
		OutputPath:    manifestPath,
		ExcludeRules:  cfg.ExcludeRules,
	})
	if err != nil {
		return bootstrapRecoveryError(recoveryDir, "scan server runtime", err)
	}
	if err := manifest.SaveFile(manifestPath, runtimeManifest); err != nil {
		return bootstrapRecoveryError(recoveryDir, "save server-runtime manifest", err)
	}
	if _, err := client.SendHeartbeat(ctx, coordinator.HeartbeatRequest{
		GroupID: created.GroupID, HostID: registered.HostID, HostToken: registered.HostToken,
		Status: "standby",
	}); err != nil {
		return bootstrapRecoveryError(recoveryDir, "record first host readiness", err)
	}
	currentGeneration, err := completeInitialTakeover(ctx, client, auth)
	if err != nil {
		return bootstrapRecoveryError(recoveryDir, "establish first current host", err)
	}
	if currentGeneration != generation {
		generation = currentGeneration
		runtimeManifest.Generation = &generation
		if err := manifest.SaveFile(manifestPath, runtimeManifest); err != nil {
			return bootstrapCurrentHostError(configPath, recoveryDir, "update server-runtime generation", err)
		}
	}

	accessKeyPath := filepath.Join(configDir, "group-access-key")
	if err := agentconfig.Save(configPath, cfg); err != nil {
		return bootstrapCurrentHostError(configPath, recoveryDir, "save current host profile", err)
	}
	if err := writePrivateFile(accessKeyPath, []byte(created.AccessKey+"\n")); err != nil {
		return bootstrapCurrentHostError(configPath, recoveryDir, "save group access key", err)
	}
	if _, err := artifactsync.Push(ctx, artifactsync.PushOptions{
		ManifestPath:   manifestPath,
		ServerDir:      opts.serverDir,
		Config:         cfg,
		Client:         client,
		HostGeneration: &currentGeneration,
	}); err != nil {
		return bootstrapCurrentHostError(configPath, recoveryDir, "publish server-runtime artifact", err)
	}

	cfg.LastPushedID = artifactID
	if err := agentconfig.Save(configPath, cfg); err != nil {
		return bootstrapCurrentHostError(configPath, recoveryDir, "record published server-runtime artifact", err)
	}
	if _, err := client.SendHeartbeat(ctx, coordinator.HeartbeatRequest{
		GroupID: created.GroupID, HostID: registered.HostID, HostToken: registered.HostToken,
		Status:               "hosting",
		LatestLocalArtifacts: map[string]string{string(manifest.ServerRuntime): artifactID},
	}); err != nil {
		return bootstrapCurrentHostError(configPath, recoveryDir, "record current host artifact state", err)
	}
	removeBootstrapRecovery(cmd, recoveryDir)

	fmt.Fprintln(cmd.OutOrStdout(), "Server-runtime bootstrap complete.")
	fmt.Fprintf(cmd.OutOrStdout(), "Group ID: %s\n", created.GroupID)
	fmt.Fprintf(cmd.OutOrStdout(), "Host ID: %s\n", registered.HostID)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID: %s\n", artifactID)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact generation: %d\n", generation)
	fmt.Fprintf(cmd.OutOrStdout(), "Current host generation: %d\n", currentGeneration)
	fmt.Fprintf(cmd.OutOrStdout(), "Files: %d\n", report.IncludedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes: %d\n", report.TotalBytes)
	fmt.Fprintf(cmd.OutOrStdout(), "Config saved: %s\n", configPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Group access key saved locally: %s\n", accessKeyPath)
	return nil
}

func runBootstrapJoinGroup(ctx context.Context, cmd *cobra.Command, opts bootstrapOptions) error {
	opts = normalizeBootstrapOptions(opts)
	configPath, configDir, err := prepareBootstrap(opts)
	if err != nil {
		return err
	}
	if err := artifactsync.EnsureRestoreTarget(opts.serverDir, opts.allowNonEmpty); err != nil {
		return err
	}
	accessKey, err := resolveBootstrapAccessKey()
	if err != nil {
		return err
	}
	client, err := coordinator.NewClient(strings.TrimRight(strings.TrimSpace(opts.coordinatorURL), "/"))
	if err != nil {
		return err
	}
	joined, err := client.JoinGroup(ctx, opts.group, coordinator.JoinGroupRequest{
		AccessKey: accessKey, DisplayName: opts.displayName,
	})
	if err != nil {
		return fmt.Errorf("join group: %w", err)
	}
	registered, err := client.RegisterHost(ctx, coordinator.RegisterHostRequest{
		GroupID: opts.group, AccessKey: accessKey, MemberID: joined.MemberID,
		DeviceName: opts.deviceName, Platform: opts.platform, AgentVersion: agentconfig.AgentVersion,
	})
	if err != nil {
		return fmt.Errorf("register host: %w", err)
	}
	cfg := bootstrapConfig(opts, opts.group, joined.MemberID, registered)
	recoveryDir, err := saveBootstrapRecovery(configDir, cfg, "")
	if err != nil {
		return fmt.Errorf("save bootstrap recovery credentials: %w", err)
	}
	auth := coordinator.ArtifactAuth{
		GroupID: opts.group, HostID: registered.HostID, HostToken: registered.HostToken,
	}
	latest, err := client.GetLatestArtifact(ctx, auth, manifest.ServerRuntime)
	if err != nil {
		return bootstrapRecoveryError(recoveryDir, "latest server-runtime artifact is unavailable", err)
	}
	if _, err := artifactsync.Pull(ctx, artifactsync.PullOptions{
		ArtifactKind: manifest.ServerRuntime,
		ArtifactID:   latest.ArtifactID,
		OutputDir:    opts.serverDir,
		Config:       cfg,
		Client:       client,
	}); err != nil {
		return bootstrapRecoveryError(recoveryDir, "restore server-runtime artifact", err)
	}
	downloaded, err := client.DownloadManifest(ctx, auth, manifest.ServerRuntime, latest.ArtifactID)
	if err != nil {
		return bootstrapRecoveryError(recoveryDir, "download manifest for verification", err)
	}
	verified, err := artifactsync.VerifyRestoredFiles(opts.serverDir, downloaded.Manifest)
	if err != nil {
		return bootstrapRecoveryError(recoveryDir, "verify restored server runtime", err)
	}
	if _, err := client.SendHeartbeat(ctx, coordinator.HeartbeatRequest{
		GroupID: opts.group, HostID: registered.HostID, HostToken: registered.HostToken,
		Status:               "standby",
		LatestLocalArtifacts: map[string]string{string(manifest.ServerRuntime): latest.ArtifactID},
	}); err != nil {
		return bootstrapRecoveryError(recoveryDir, "record standby host artifact state", err)
	}
	status, err := client.GetElectionStatus(ctx, auth)
	if err != nil {
		return bootstrapRecoveryError(recoveryDir, "confirm standby host state", err)
	}
	if status.CurrentHostID != nil && *status.CurrentHostID == registered.HostID {
		return bootstrapRecoveryError(recoveryDir, "confirm standby host state", errors.New("join-group unexpectedly changed the current host"))
	}

	cfg.LastPushedID = latest.ArtifactID
	if err := agentconfig.Save(filepath.Join(recoveryDir, agentconfig.FileName), cfg); err != nil {
		return bootstrapRecoveryError(recoveryDir, "update bootstrap recovery profile", err)
	}
	if err := agentconfig.Save(configPath, cfg); err != nil {
		return bootstrapRecoveryError(recoveryDir, "save standby host profile", err)
	}
	removeBootstrapRecovery(cmd, recoveryDir)

	fmt.Fprintln(cmd.OutOrStdout(), "Server-runtime join and restore complete.")
	fmt.Fprintf(cmd.OutOrStdout(), "Host ID: %s\n", registered.HostID)
	fmt.Fprintf(cmd.OutOrStdout(), "Artifact ID: %s\n", latest.ArtifactID)
	if downloaded.Manifest.Generation != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Artifact generation: %d\n", *downloaded.Manifest.Generation)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Current host generation: %d\n", status.CurrentHostGeneration)
	fmt.Fprintf(cmd.OutOrStdout(), "Files: %d\n", verified.VerifiedFiles)
	fmt.Fprintf(cmd.OutOrStdout(), "Total bytes: %d\n", verified.TotalBytes)
	fmt.Fprintf(cmd.OutOrStdout(), "Restore path: %s\n", opts.serverDir)
	fmt.Fprintln(cmd.OutOrStdout(), "Verify result: PASS")
	return nil
}

func resolveBootstrapAccessKey() (string, error) {
	accessKey := strings.TrimSpace(os.Getenv("ACBH_ACCESS_KEY"))
	if accessKey == "" {
		return "", errors.New("access key is required in ACBH_ACCESS_KEY; bootstrap does not accept secrets in argv")
	}
	return accessKey, nil
}

func prepareBootstrap(opts bootstrapOptions) (string, string, error) {
	if manifest.ArtifactKind(opts.artifactClass) != manifest.ServerRuntime {
		return "", "", fmt.Errorf("bootstrap only supports artifact class %q", manifest.ServerRuntime)
	}
	if strings.TrimSpace(opts.serverDir) == "" {
		return "", "", errors.New("server directory is required")
	}
	configPath, err := agentconfig.DefaultPath()
	if err != nil {
		return "", "", err
	}
	if agentconfig.Exists(configPath) && !opts.force {
		return "", "", fmt.Errorf("local host profile already exists at %s; use --force only to replace it explicitly", configPath)
	}
	return configPath, filepath.Dir(configPath), nil
}

func preflightBootstrapServerRuntime(opts bootstrapOptions) error {
	generation := 0
	_, _, err := scanner.Scan(scanner.Options{
		ServerDir:     opts.serverDir,
		ArtifactKind:  manifest.ServerRuntime,
		ArtifactID:    "runtime-bootstrap-preflight",
		GroupID:       "group-bootstrap-preflight",
		CreatorHostID: "host-bootstrap-preflight",
		Generation:    &generation,
		ExcludeRules:  scanner.DefaultServerRuntimeExcludeRules(),
	})
	if err != nil {
		return fmt.Errorf("preflight server runtime: %w", err)
	}
	return nil
}

func saveBootstrapRecovery(configDir string, cfg agentconfig.Config, accessKey string) (string, error) {
	recoveryDir := filepath.Join(
		configDir,
		"bootstrap-recovery",
		time.Now().UTC().Format("20060102T150405.000000000Z"),
	)
	if err := agentconfig.Save(filepath.Join(recoveryDir, agentconfig.FileName), cfg); err != nil {
		return "", err
	}
	if accessKey != "" {
		if err := writePrivateFile(filepath.Join(recoveryDir, "group-access-key"), []byte(accessKey+"\n")); err != nil {
			_ = os.RemoveAll(recoveryDir)
			return "", err
		}
	}
	return recoveryDir, nil
}

func bootstrapRecoveryError(recoveryDir, stage string, err error) error {
	return fmt.Errorf(
		"%s; bootstrap credentials were preserved at %s: %w",
		stage,
		recoveryDir,
		err,
	)
}

func bootstrapCurrentHostError(configPath, recoveryDir, stage string, err error) error {
	return fmt.Errorf(
		"current host was established but bootstrap could not %s; use the profile at %s and recovery data at %s to retry the artifact operation: %w",
		stage,
		configPath,
		recoveryDir,
		err,
	)
}

func removeBootstrapRecovery(cmd *cobra.Command, recoveryDir string) {
	if err := os.RemoveAll(recoveryDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: bootstrap succeeded but recovery data could not be removed from %s: %v\n", recoveryDir, err)
		return
	}
	_ = os.Remove(filepath.Dir(recoveryDir))
}

func normalizeBootstrapOptions(opts bootstrapOptions) bootstrapOptions {
	if opts.deviceName == "" {
		opts.deviceName = localHostname()
	}
	if opts.displayName == "" {
		opts.displayName = opts.deviceName
	}
	return opts
}

func bootstrapConfig(
	opts bootstrapOptions,
	groupID string,
	memberID string,
	registered coordinator.RegisterHostResponse,
) agentconfig.Config {
	return agentconfig.Config{
		CoordinatorURL: strings.TrimRight(strings.TrimSpace(opts.coordinatorURL), "/"),
		GroupID:        groupID,
		MemberID:       memberID,
		HostID:         registered.HostID,
		HostToken:      registered.HostToken,
		DisplayName:    opts.displayName,
		DeviceName:     opts.deviceName,
		Platform:       opts.platform,
		AgentVersion:   agentconfig.AgentVersion,
		Server:         agentconfig.ServerConfig{Dir: opts.serverDir},
		ArtifactClass:  string(manifest.ServerRuntime),
		ExcludeRules:   scanner.DefaultServerRuntimeExcludeRules(),
		RCON: agentconfig.RCONConfig{
			Host: "127.0.0.1", Port: 25575, PasswordEnv: "ACBH_RCON_PASSWORD",
		},
	}
}

func completeInitialTakeover(
	ctx context.Context,
	client *coordinator.Client,
	auth coordinator.ArtifactAuth,
) (int, error) {
	req := coordinator.ElectionAuthRequest{
		GroupID: auth.GroupID, HostID: auth.HostID, HostToken: auth.HostToken,
	}
	election, err := client.RunElection(ctx, req, "no-current-host")
	if err != nil {
		return 0, fmt.Errorf("elect first current host: %w", err)
	}
	if election.Assignment == nil {
		return 0, errors.New("elect first current host: no takeover assignment was created")
	}
	polled, err := client.PollTakeover(ctx, coordinator.TakeoverPollRequest{
		ElectionAuthRequest: req,
	})
	if err != nil {
		return 0, fmt.Errorf("poll first-host assignment: %w", err)
	}
	if polled.Assignment == nil || polled.Assignment.TakeoverToken == "" {
		return 0, errors.New("poll first-host assignment: takeover token was not issued")
	}
	action := coordinator.TakeoverActionRequest{
		GroupID: auth.GroupID, HostID: auth.HostID, HostToken: auth.HostToken,
		AssignmentID:  polled.Assignment.AssignmentID,
		TakeoverToken: polled.Assignment.TakeoverToken,
	}
	if _, err := client.AcceptTakeover(ctx, action); err != nil {
		return 0, fmt.Errorf("accept first-host assignment: %w", err)
	}
	if _, err := client.CompleteTakeover(ctx, action); err != nil {
		return 0, fmt.Errorf("complete first-host assignment: %w", err)
	}
	status, err := client.GetElectionStatus(ctx, auth)
	if err != nil {
		return 0, fmt.Errorf("confirm first current host: %w", err)
	}
	if status.CurrentHostID == nil || *status.CurrentHostID != auth.HostID {
		return 0, errors.New("first host was not recorded as current host")
	}
	return status.CurrentHostGeneration, nil
}

func newServerRuntimeArtifactID() string {
	return "runtime-" + time.Now().UTC().Format("20060102T150405.000000000Z")
}

func writePrivateFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func localHostname() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "acbh-host"
	}
	return hostname
}
