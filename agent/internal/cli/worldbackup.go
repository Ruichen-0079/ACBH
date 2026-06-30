package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/rcon"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
	"github.com/spf13/cobra"
)

type worldBackupOptions struct {
	appDataDir        string
	serverDir         string
	worldRoots        []string
	jsonOutput        bool
	dryRun            bool
	online            bool
	rconHost          string
	rconPort          int
	rconPassword      string
	rconTimeout       time.Duration
	allowInconsistent bool
	consistentOnly    bool
	snapshotID        string
}

func newWorldBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "world-backup",
		Short: "Create, restore, and manage differential Minecraft world snapshots",
	}
	cmd.AddCommand(
		newWorldBackupCreateCmd(),
		newWorldBackupStatusCmd(),
		newWorldBackupListCmd(),
		newWorldBackupShowCmd(),
		newWorldBackupRestoreCmd(),
		newWorldBackupPinCmd(),
		newWorldBackupDeleteCmd(),
		newWorldBackupResumeCmd(),
		newWorldBackupGcCmd(),
	)
	return cmd
}

func newWorldBackupCreateCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and publish a differential world snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorldBackupCreate(cmd.Context(), cmd, opts)
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&opts.online, "online", false, "Use RCON save-off/save-all flush/save-on around online backup")
	cmd.Flags().StringVar(&opts.rconHost, "rcon-host", "127.0.0.1", "Minecraft RCON host")
	cmd.Flags().IntVar(&opts.rconPort, "rcon-port", 25575, "Minecraft RCON port")
	cmd.Flags().StringVar(&opts.rconPassword, "rcon-password", "", "Minecraft RCON password (or use ACBH_RCON_PASSWORD)")
	cmd.Flags().DurationVar(&opts.rconTimeout, "rcon-timeout", defaultRCONTimeout, "RCON timeout")
	cmd.Flags().BoolVar(&opts.allowInconsistent, "allow-inconsistent", false, "Create a manual inconsistent snapshot if online consistency cannot be guaranteed")
	cmd.Flags().StringVar(&opts.snapshotID, "snapshot-id", "", "Snapshot ID (default generated)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Scan and plan without uploading or committing")
	return cmd
}

func newWorldBackupStatusCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local world backup index and latest remote snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorldBackupStatus(cmd.Context(), cmd, opts)
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	return cmd
}

func newWorldBackupListCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List remote world snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, auth, _, err := loadWorldBackupContext(opts)
			if err != nil {
				return err
			}
			_ = cfg
			out, err := client.ListWorldBackups(cmd.Context(), auth)
			if err != nil {
				return err
			}
			return printJSON(cmd, out)
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	return cmd
}

func newWorldBackupShowCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "show <snapshot-id>",
		Short: "Show a world snapshot manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, auth, _, err := loadWorldBackupContext(opts)
			if err != nil {
				return err
			}
			out, err := client.GetWorldBackup(cmd.Context(), auth, args[0])
			if err != nil {
				return err
			}
			return printJSON(cmd, out)
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	return cmd
}

func newWorldBackupRestoreCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "restore <latest|snapshot-id>",
		Short: "Restore a world snapshot transactionally",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorldBackupRestore(cmd.Context(), cmd, opts, args[0])
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&opts.consistentOnly, "consistent-only", true, "Refuse inconsistent snapshots")
	return cmd
}

func newWorldBackupPinCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "pin <snapshot-id>",
		Short: "Pin a world snapshot so retention keeps it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, auth, _, err := loadWorldBackupContext(opts)
			if err != nil {
				return err
			}
			if err := client.PinWorldBackup(cmd.Context(), auth, args[0], true); err != nil {
				return err
			}
			return printJSON(cmd, map[string]any{"ok": true, "snapshotId": args[0], "pinned": true})
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	return cmd
}

func newWorldBackupDeleteCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "delete <snapshot-id>",
		Short: "Delete an unpinned non-latest world snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, auth, _, err := loadWorldBackupContext(opts)
			if err != nil {
				return err
			}
			if err := client.DeleteWorldBackup(cmd.Context(), auth, args[0]); err != nil {
				return err
			}
			return printJSON(cmd, map[string]any{"ok": true, "snapshotId": args[0]})
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	return cmd
}

func newWorldBackupResumeCmd() *cobra.Command {
	var opts worldBackupOptions
	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume by re-planning and uploading missing objects for the current world",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorldBackupCreate(cmd.Context(), cmd, opts)
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	return cmd
}

func newWorldBackupGcCmd() *cobra.Command {
	var opts worldBackupOptions
	var execute bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Run world snapshot retention/GC dry-run through the Coordinator",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGc(cmd, gcOptions{execute: execute, explicitDryRun: !execute})
		},
	}
	addWorldBackupCommonFlags(cmd, &opts)
	cmd.Flags().BoolVar(&execute, "execute", false, "Actually delete eligible unretained artifacts and objects")
	return cmd
}

func addWorldBackupCommonFlags(cmd *cobra.Command, opts *worldBackupOptions) {
	cmd.Flags().StringVar(&opts.appDataDir, "app-data-dir", "", "ACBH app data directory")
	cmd.Flags().StringVar(&opts.serverDir, "server-dir", "", "Minecraft server directory")
	cmd.Flags().StringArrayVar(&opts.worldRoots, "world-root", nil, "Extra world root relative to server-dir; may be repeated")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", true, "Output JSON")
}

func runWorldBackupCreate(ctx context.Context, cmd *cobra.Command, opts worldBackupOptions) error {
	cfg, client, auth, appDataDir, err := loadWorldBackupContext(opts)
	if err != nil {
		return err
	}
	serverDir := firstNonEmpty(opts.serverDir, cfg.Server.Dir)
	if strings.TrimSpace(serverDir) == "" {
		return errors.New("server directory is required; pass --server-dir or configure server.dir")
	}

	status, err := client.GetElectionStatus(ctx, auth)
	if err != nil {
		return fmt.Errorf("cannot verify current host state before publishing world snapshot: %w", err)
	}
	if status.CurrentHostID == nil || *status.CurrentHostID != cfg.HostID {
		return errors.New("not_current_host: only the current host may publish world snapshots")
	}
	generation := status.CurrentHostGeneration
	if _, err := client.EnsureActiveLease(ctx, auth, &generation); err != nil {
		return fmt.Errorf("renew host lease before world backup: %w", err)
	}
	ctx, stopLease := startWorldBackupLeaseKeeper(ctx, client, auth, &generation)
	defer func() {
		_ = stopLease()
	}()
	consistent := true
	if opts.online {
		rconPassword := strings.TrimSpace(opts.rconPassword)
		if rconPassword == "" {
			rconPassword = strings.TrimSpace(os.Getenv("ACBH_RCON_PASSWORD"))
		}
		if rconPassword == "" {
			if !opts.allowInconsistent {
				return errors.New("RCON password is required for an online consistent snapshot")
			}
			consistent = false
		} else {
			rconCfg := rcon.Config{Host: opts.rconHost, Port: opts.rconPort, Password: rconPassword, Timeout: opts.rconTimeout}
			saveOn := false
			defer func() {
				if saveOn {
					_, _ = rcon.Execute(context.Background(), rconCfg, "save-on")
				}
			}()
			if _, err := rcon.Execute(ctx, rconCfg, "save-off"); err != nil {
				return fmt.Errorf("RCON save-off failed: %w", err)
			}
			saveOn = true
			if _, err := rcon.Execute(ctx, rconCfg, "save-all flush"); err != nil {
				return fmt.Errorf("RCON save-all flush failed: %w", err)
			}
		}
	}

	var parent *worldbackup.Manifest
	if latest, err := client.GetLatestWorldBackup(ctx, auth, false); err == nil {
		parent = &latest.Manifest
	}
	snapshotID := opts.snapshotID
	if snapshotID == "" {
		snapshotID = "ws_" + time.Now().UTC().Format("20060102_150405")
	}
	snapshot, err := worldbackup.BuildSnapshot(worldbackup.ScanOptions{
		ServerDir:      serverDir,
		AppDataDir:     appDataDir,
		WorldRoots:     opts.worldRoots,
		SnapshotID:     snapshotID,
		GroupID:        cfg.GroupID,
		SourceHostID:   cfg.HostID,
		HostGeneration: generation,
		Parent:         parent,
		Consistent:     consistent,
	})
	if err != nil {
		return err
	}
	if opts.dryRun {
		return printJSON(cmd, snapshot.Plan)
	}

	planned, err := client.PlanWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupPlanRequest{
		HostID:           cfg.HostID,
		HostToken:        cfg.HostToken,
		HostGeneration:   generation,
		ParentSnapshotID: snapshot.Manifest.ParentSnapshotID,
		Objects:          snapshot.Plan.Objects,
	})
	if err != nil {
		if leaseErr := stopLease(); leaseErr != nil && errors.Is(err, context.Canceled) {
			return leaseErr
		}
		return err
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
			return fmt.Errorf("coordinator requested unknown object %s", object.SHA256)
		}
		file, err := os.Open(changed.LocalPath)
		if err != nil {
			return fmt.Errorf("open changed file %s: %w", changed.Path, err)
		}
		_, uploadErr := client.UploadWorldObjectStream(ctx, auth, object.SHA256, file, object.Size)
		closeErr := file.Close()
		if uploadErr != nil {
			if leaseErr := stopLease(); leaseErr != nil && errors.Is(uploadErr, context.Canceled) {
				return leaseErr
			}
			return uploadErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	commit, err := client.CommitWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupCommitRequest{
		HostID:         cfg.HostID,
		HostToken:      cfg.HostToken,
		HostGeneration: generation,
		Manifest:       snapshot.Manifest,
	})
	if err != nil {
		if leaseErr := stopLease(); leaseErr != nil && errors.Is(err, context.Canceled) {
			return leaseErr
		}
		return err
	}
	if _, err := client.EnsureActiveLease(ctx, auth, &generation); err != nil {
		return fmt.Errorf("renew host lease after world backup: %w", err)
	}
	if leaseErr := stopLease(); leaseErr != nil {
		return leaseErr
	}
	if err := worldbackup.SaveIndexAtomic(appDataDir, snapshot.Index); err != nil {
		return err
	}
	return printJSON(cmd, map[string]any{
		"ok":             commit.OK,
		"snapshotId":     commit.SnapshotID,
		"status":         commit.Status,
		"plan":           snapshot.Plan,
		"missingObjects": len(planned.MissingObjects),
		"indexPath":      worldbackup.IndexPath(appDataDir),
	})
}

func runWorldBackupStatus(ctx context.Context, cmd *cobra.Command, opts worldBackupOptions) error {
	_, client, auth, appDataDir, err := loadWorldBackupContext(opts)
	if err != nil {
		return err
	}
	idx, indexExists, indexErr := worldbackup.LoadIndex(appDataDir)
	var latest any
	if remote, err := client.GetLatestWorldBackup(ctx, auth, false); err == nil {
		latest = remote.Metadata
	}
	out := map[string]any{
		"indexPath":    worldbackup.IndexPath(appDataDir),
		"indexExists":  indexExists,
		"remoteLatest": latest,
	}
	if indexErr == nil {
		out["localLatestSnapshotId"] = idx.LatestSnapshotID
		out["localFileCount"] = len(idx.Files)
	} else {
		out["indexError"] = indexErr.Error()
	}
	return printJSON(cmd, out)
}

func runWorldBackupRestore(ctx context.Context, cmd *cobra.Command, opts worldBackupOptions, snapshotID string) error {
	cfg, client, auth, _, err := loadWorldBackupContext(opts)
	if err != nil {
		return err
	}
	serverDir := firstNonEmpty(opts.serverDir, cfg.Server.Dir)
	if strings.TrimSpace(serverDir) == "" {
		return errors.New("server directory is required; pass --server-dir or configure server.dir")
	}
	ctx, stopLease, leaseStartErr := startWorldBackupRestoreLeaseKeeper(ctx, client, auth)
	if leaseStartErr != nil {
		return leaseStartErr
	}
	defer func() {
		_ = stopLease()
	}()
	var remote coordinator.WorldBackupManifestResponse
	if snapshotID == "latest" {
		remote, err = client.GetLatestWorldBackup(ctx, auth, opts.consistentOnly)
	} else {
		remote, err = client.GetWorldBackup(ctx, auth, snapshotID)
	}
	if err != nil {
		return err
	}
	downloader := func(ctx context.Context, objectID string) (io.ReadCloser, int64, error) {
		sha, ok := strings.CutPrefix(objectID, "sha256:")
		if !ok {
			return nil, 0, fmt.Errorf("unsupported object ID %s", objectID)
		}
		return client.DownloadWorldObjectStream(ctx, auth, sha)
	}
	summary, err := worldbackup.Restore(ctx, worldbackup.RestoreOptions{
		ServerDir:      serverDir,
		Manifest:       remote.Manifest,
		Downloader:     downloader,
		ConsistentOnly: opts.consistentOnly,
	})
	if err != nil {
		if leaseErr := stopLease(); leaseErr != nil && errors.Is(err, context.Canceled) {
			return leaseErr
		}
		return err
	}
	if leaseErr := stopLease(); leaseErr != nil {
		return leaseErr
	}
	return printJSON(cmd, summary)
}

func startWorldBackupRestoreLeaseKeeper(ctx context.Context, client *coordinator.Client, auth coordinator.ArtifactAuth) (context.Context, func() error, error) {
	status, err := client.GetLeaseStatus(ctx, auth)
	if err != nil || !status.CurrentHostIDMatches {
		return ctx, func() error { return nil }, nil
	}
	generation := status.Generation
	if _, err := client.EnsureActiveLease(ctx, auth, &generation); err != nil {
		return ctx, func() error { return nil }, fmt.Errorf("renew host lease before world backup restore: %w", err)
	}
	leaseCtx, stop := startWorldBackupLeaseKeeper(ctx, client, auth, &generation)
	return leaseCtx, stop, nil
}

func startWorldBackupLeaseKeeper(ctx context.Context, client *coordinator.Client, auth coordinator.ArtifactAuth, generation *int) (context.Context, func() error) {
	leaseCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				if _, err := client.EnsureActiveLease(leaseCtx, auth, generation); err != nil {
					select {
					case errCh <- fmt.Errorf("renew host lease during world backup: %w", err):
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	stop := func() error {
		cancel()
		<-done
		select {
		case err := <-errCh:
			return err
		default:
			return nil
		}
	}
	return leaseCtx, stop
}

func loadWorldBackupContext(opts worldBackupOptions) (
	cfg agentconfig.Config,
	client *coordinator.Client,
	auth coordinator.ArtifactAuth,
	appDataDir string,
	err error,
) {
	loaded, configPath, err := loadConfig()
	if err != nil {
		return cfg, nil, coordinator.ArtifactAuth{}, "", err
	}
	client, err = coordinator.NewClient(loaded.CoordinatorURL)
	if err != nil {
		return cfg, nil, coordinator.ArtifactAuth{}, "", err
	}
	appDataDir = opts.appDataDir
	if appDataDir == "" {
		appDataDir = filepath.Dir(configPath)
	}
	auth = coordinator.ArtifactAuth{GroupID: loaded.GroupID, HostID: loaded.HostID, HostToken: loaded.HostToken}
	return loaded, client, auth, appDataDir, nil
}

func sortWorldBackupObjects(objects []worldbackup.PlannedObject) {
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].SHA256 < objects[j].SHA256
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
