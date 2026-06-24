package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/rcon"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

const defaultWorldBackupRCONTimeout = 10 * time.Second

type WorldBackupOptions struct {
	WorldRoots        []string
	AllowInconsistent bool
	ConsistentOnly    bool
	RCONHost          string
	RCONPort          int
	RCONTimeout       time.Duration
	SnapshotID        string
}

type WorldBackupStatusResult struct {
	OK                    bool                             `json:"ok"`
	IndexPath             string                           `json:"indexPath"`
	IndexExists           bool                             `json:"indexExists"`
	LocalLatestSnapshotID string                           `json:"localLatestSnapshotId,omitempty"`
	LocalFileCount        int                              `json:"localFileCount,omitempty"`
	RemoteLatest          *coordinator.WorldBackupMetadata `json:"remoteLatest,omitempty"`
	HistoryCount          int                              `json:"historyCount"`
	IndexError            string                           `json:"indexError,omitempty"`
	RemoteLatestError     string                           `json:"remoteLatestError,omitempty"`
	ListError             string                           `json:"listError,omitempty"`
}

type WorldBackupCreateResult struct {
	OK               bool   `json:"ok"`
	SnapshotID       string `json:"snapshotId"`
	MissingObjects   int    `json:"missingObjects"`
	LogicalSize      int64  `json:"logicalSize"`
	UploadedSize     int64  `json:"uploadedSize"`
	ChangedFileCount int    `json:"changedFileCount"`
	DeletedFileCount int    `json:"deletedFileCount"`
	IndexPath        string `json:"indexPath,omitempty"`
}

type WorldBackupActionResult struct {
	OK         bool   `json:"ok"`
	SnapshotID string `json:"snapshotId,omitempty"`
	Pinned     bool   `json:"pinned,omitempty"`
	Message    string `json:"message,omitempty"`
}

func defaultWorldBackupOptions(wb WorldBackupOptions) WorldBackupOptions {
	if wb.RCONHost == "" {
		wb.RCONHost = "127.0.0.1"
	}
	if wb.RCONPort == 0 {
		wb.RCONPort = 25575
	}
	if wb.RCONTimeout == 0 {
		wb.RCONTimeout = defaultWorldBackupRCONTimeout
	}
	return wb
}

func loadWorldBackupContext(opts Options) (agentconfig.Config, *coordinator.Client, coordinator.ArtifactAuth, error) {
	opts = withDefaults(opts)
	cfg, err := loadDesktopConfig(opts)
	if err != nil {
		return agentconfig.Config{}, nil, coordinator.ArtifactAuth{}, err
	}
	client, err := coordinator.NewClient(cfg.CoordinatorURL)
	if err != nil {
		return agentconfig.Config{}, nil, coordinator.ArtifactAuth{}, err
	}
	auth := coordinator.ArtifactAuth{GroupID: cfg.GroupID, HostID: cfg.HostID, HostToken: cfg.HostToken}
	return cfg, client, auth, nil
}

func WorldBackupStatus(ctx context.Context, opts Options) (WorldBackupStatusResult, error) {
	opts = withDefaults(opts)
	result := WorldBackupStatusResult{
		OK:        true,
		IndexPath: worldbackup.IndexPath(opts.AppDataDir),
	}
	idx, indexExists, indexErr := worldbackup.LoadIndex(opts.AppDataDir)
	result.IndexExists = indexExists
	if indexErr != nil {
		result.IndexError = indexErr.Error()
	} else {
		result.LocalLatestSnapshotID = idx.LatestSnapshotID
		result.LocalFileCount = len(idx.Files)
	}

	cfg, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		result.OK = indexErr == nil
		result.RemoteLatestError = err.Error()
		result.ListError = err.Error()
		return result, nil
	}
	_ = cfg

	if remote, err := client.GetLatestWorldBackup(ctx, auth, false); err != nil {
		result.RemoteLatestError = err.Error()
	} else {
		meta := remote.Metadata
		result.RemoteLatest = &meta
	}
	if list, err := client.ListWorldBackups(ctx, auth); err != nil {
		result.ListError = err.Error()
	} else {
		result.HistoryCount = len(list.Snapshots)
	}
	return result, nil
}

func WorldBackupCreate(ctx context.Context, opts Options, wb WorldBackupOptions, online bool, rconPassword string) (WorldBackupCreateResult, error) {
	wb = defaultWorldBackupOptions(wb)
	if online {
		return publishWorldSnapshot(ctx, opts, wb, true, rconPassword)
	}
	published, err := CreateStoppedWorldSnapshot(ctx, opts)
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	return WorldBackupCreateResult{
		OK:               true,
		SnapshotID:       published.SnapshotID,
		MissingObjects:   published.MissingObjects,
		LogicalSize:      published.LogicalSize,
		UploadedSize:     published.UploadedSize,
		ChangedFileCount: published.ChangedFileCount,
		DeletedFileCount: published.DeletedFileCount,
		IndexPath:        worldbackup.IndexPath(withDefaults(opts).AppDataDir),
	}, nil
}

func WorldBackupResume(ctx context.Context, opts Options, wb WorldBackupOptions) (WorldBackupCreateResult, error) {
	wb = defaultWorldBackupOptions(wb)
	return publishWorldSnapshot(ctx, opts, wb, false, "")
}

func publishWorldSnapshot(ctx context.Context, opts Options, wb WorldBackupOptions, online bool, rconPassword string) (WorldBackupCreateResult, error) {
	opts = withDefaults(opts)
	cfg, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	serverDir := strings.TrimSpace(cfg.Server.Dir)
	if serverDir == "" {
		return WorldBackupCreateResult{}, errors.New("server directory is required; configure server.dir first")
	}

	status, err := client.GetElectionStatus(ctx, auth)
	if err != nil {
		return WorldBackupCreateResult{}, fmt.Errorf("cannot verify current host state before publishing world snapshot: %w", err)
	}
	if status.CurrentHostID == nil || *status.CurrentHostID != cfg.HostID {
		return WorldBackupCreateResult{}, errors.New("not_current_host: only the current host may publish world snapshots")
	}
	generation := status.CurrentHostGeneration
	snapshotID := wb.SnapshotID
	if snapshotID == "" {
		snapshotID = "ws_" + time.Now().UTC().Format("20060102_150405")
	}
	consistent := true
	scanServerDir := serverDir
	ignoreRulesDir := serverDir
	if online {
		rconPassword = strings.TrimSpace(rconPassword)
		if rconPassword == "" {
			rconPassword = strings.TrimSpace(os.Getenv("ACBH_RCON_PASSWORD"))
		}
		if rconPassword == "" {
			if !wb.AllowInconsistent {
				return WorldBackupCreateResult{}, errors.New("RCON password is required for an online consistent snapshot")
			}
			consistent = false
		} else {
			rconCfg := rcon.Config{Host: wb.RCONHost, Port: wb.RCONPort, Password: rconPassword, Timeout: wb.RCONTimeout}
			session, err := worldbackup.PrepareOnlineConsistentBackup(ctx, worldbackup.OnlineStagingOptions{
				ServerDir:     serverDir,
				AppDataDir:    opts.AppDataDir,
				WorldRoots:    wb.WorldRoots,
				TransactionID: snapshotID,
				RCON:          desktopRCONRunner{cfg: rconCfg},
			})
			if err != nil {
				return WorldBackupCreateResult{}, err
			}
			defer func() { _ = worldbackup.RemoveTransactionDir(opts.AppDataDir, session.TransactionID) }()
			scanServerDir = session.StagingDir
			ignoreRulesDir = serverDir
		}
	}

	var parent *worldbackup.Manifest
	if latest, err := client.GetLatestWorldBackup(ctx, auth, false); err == nil {
		parent = &latest.Manifest
	}
	snapshot, err := worldbackup.BuildSnapshot(worldbackup.ScanOptions{
		ServerDir:      scanServerDir,
		AppDataDir:     opts.AppDataDir,
		IgnoreRulesDir: ignoreRulesDir,
		WorldRoots:     wb.WorldRoots,
		SnapshotID:     snapshotID,
		GroupID:        cfg.GroupID,
		SourceHostID:   cfg.HostID,
		HostGeneration: generation,
		Parent:         parent,
		Consistent:     consistent,
	})
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	planned, err := client.PlanWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupPlanRequest{
		HostID:           cfg.HostID,
		HostToken:        cfg.HostToken,
		HostGeneration:   generation,
		ParentSnapshotID: snapshot.Manifest.ParentSnapshotID,
		Objects:          snapshot.Plan.Objects,
	})
	if err != nil {
		return WorldBackupCreateResult{}, err
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
			return WorldBackupCreateResult{}, fmt.Errorf("coordinator requested unknown object %s", object.SHA256)
		}
		file, err := os.Open(changed.LocalPath)
		if err != nil {
			return WorldBackupCreateResult{}, fmt.Errorf("open changed file %s: %w", changed.Path, err)
		}
		_, uploadErr := client.UploadWorldObjectStream(ctx, auth, object.SHA256, file, object.Size)
		closeErr := file.Close()
		if uploadErr != nil {
			return WorldBackupCreateResult{}, uploadErr
		}
		if closeErr != nil {
			return WorldBackupCreateResult{}, closeErr
		}
	}
	commit, err := client.CommitWorldBackup(ctx, cfg.GroupID, coordinator.WorldBackupCommitRequest{
		HostID:         cfg.HostID,
		HostToken:      cfg.HostToken,
		HostGeneration: generation,
		Manifest:       snapshot.Manifest,
	})
	if err != nil {
		return WorldBackupCreateResult{}, err
	}
	if err := worldbackup.SaveIndexAtomic(opts.AppDataDir, snapshot.Index); err != nil {
		return WorldBackupCreateResult{}, err
	}
	return WorldBackupCreateResult{
		OK:               commit.OK,
		SnapshotID:       commit.SnapshotID,
		MissingObjects:   len(planned.MissingObjects),
		LogicalSize:      snapshot.Manifest.LogicalSize,
		UploadedSize:     snapshot.Manifest.UploadedSize,
		ChangedFileCount: snapshot.Manifest.ChangedFileCount,
		DeletedFileCount: snapshot.Manifest.DeletedFileCount,
		IndexPath:        worldbackup.IndexPath(opts.AppDataDir),
	}, nil
}

func WorldBackupList(ctx context.Context, opts Options) (coordinator.WorldBackupListResponse, error) {
	_, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return coordinator.WorldBackupListResponse{}, err
	}
	return client.ListWorldBackups(ctx, auth)
}

func WorldBackupShow(ctx context.Context, opts Options, snapshotID string) (coordinator.WorldBackupManifestResponse, error) {
	_, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return coordinator.WorldBackupManifestResponse{}, err
	}
	return client.GetWorldBackup(ctx, auth, snapshotID)
}

func WorldBackupRestore(ctx context.Context, opts Options, wb WorldBackupOptions, snapshotID string) (worldbackup.RestoreSummary, error) {
	wb = defaultWorldBackupOptions(wb)
	if snapshotID == "" {
		snapshotID = "latest"
	}
	if snapshotID == "latest" {
		return RestoreLatestWorldSnapshot(ctx, opts)
	}
	cfg, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	serverDir := strings.TrimSpace(cfg.Server.Dir)
	if serverDir == "" {
		return worldbackup.RestoreSummary{}, errors.New("server directory is required; configure server.dir first")
	}
	remote, err := client.GetWorldBackup(ctx, auth, snapshotID)
	if err != nil {
		return worldbackup.RestoreSummary{}, err
	}
	downloader := worldObjectDownloader(client, auth)
	return worldbackup.Restore(ctx, worldbackup.RestoreOptions{
		ServerDir:      serverDir,
		Manifest:       remote.Manifest,
		Downloader:     downloader,
		ConsistentOnly: wb.ConsistentOnly,
	})
}

func WorldBackupPin(ctx context.Context, opts Options, snapshotID string) (WorldBackupActionResult, error) {
	_, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return WorldBackupActionResult{}, err
	}
	if err := client.PinWorldBackup(ctx, auth, snapshotID, true); err != nil {
		return WorldBackupActionResult{}, err
	}
	return WorldBackupActionResult{OK: true, SnapshotID: snapshotID, Pinned: true, Message: "snapshot pinned"}, nil
}

func WorldBackupDelete(ctx context.Context, opts Options, snapshotID string) (WorldBackupActionResult, error) {
	_, client, auth, err := loadWorldBackupContext(opts)
	if err != nil {
		return WorldBackupActionResult{}, err
	}
	if err := client.DeleteWorldBackup(ctx, auth, snapshotID); err != nil {
		return WorldBackupActionResult{}, err
	}
	return WorldBackupActionResult{OK: true, SnapshotID: snapshotID, Message: "snapshot deleted"}, nil
}

func worldObjectDownloader(client *coordinator.Client, auth coordinator.ArtifactAuth) worldbackup.ObjectDownloader {
	return func(ctx context.Context, objectID string) (io.ReadCloser, int64, error) {
		sha, ok := strings.CutPrefix(objectID, "sha256:")
		if !ok {
			return nil, 0, fmt.Errorf("unsupported object ID %s", objectID)
		}
		return client.DownloadWorldObjectStream(ctx, auth, sha)
	}
}

type desktopRCONRunner struct {
	cfg rcon.Config
}

func (r desktopRCONRunner) Execute(ctx context.Context, command string) (string, error) {
	return rcon.Execute(ctx, r.cfg, command)
}