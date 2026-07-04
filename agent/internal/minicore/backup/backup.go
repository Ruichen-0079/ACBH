package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coordinatorclient"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/coreerrors"
	"github.com/Ruichen-0079/ACBH/agent/internal/minicore/identity"
	"github.com/Ruichen-0079/ACBH/agent/internal/worldbackup"
)

type Client interface {
	Probe(ctx context.Context) (coordinatorclient.ProbeResult, *coreerrors.Error)
	EnsureActiveLeaseWithGeneration(ctx context.Context, groupID string, hostID string, hostToken string, generation *int) (coordinatorclient.EnsureActiveLeaseResponse, *coreerrors.Error)
	GetLatestWorldBackup(ctx context.Context, groupID string, hostID string, hostToken string, consistentOnly bool) (coordinatorclient.WorldBackupManifestResponse, *coreerrors.Error)
	PlanWorldBackup(ctx context.Context, groupID string, req coordinatorclient.WorldBackupPlanRequest) (coordinatorclient.WorldBackupPlanResponse, *coreerrors.Error)
	UploadWorldObjectStream(ctx context.Context, groupID string, hostID string, hostToken string, sha256 string, content io.Reader, size int64) (coordinatorclient.UploadObjectResponse, *coreerrors.Error)
	CommitWorldBackup(ctx context.Context, groupID string, req coordinatorclient.WorldBackupCommitRequest) (coordinatorclient.WorldBackupCommitResponse, *coreerrors.Error)
	ListWorldBackups(ctx context.Context, groupID string, hostID string, hostToken string) (coordinatorclient.WorldBackupListResponse, *coreerrors.Error)
	GetWorldBackup(ctx context.Context, groupID string, hostID string, hostToken string, snapshotID string) (coordinatorclient.WorldBackupManifestResponse, *coreerrors.Error)
	DownloadWorldObjectStream(ctx context.Context, groupID string, hostID string, hostToken string, sha256 string) (io.ReadCloser, int64, *coreerrors.Error)
}

type AnalyzeRequest struct {
	ServerDir string `json:"serverDir,omitempty"`
	ProfileID string `json:"profileId,omitempty"`
}

type RootSummary struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	FileCount int    `json:"fileCount"`
	Size      int64  `json:"size"`
}

type Warning struct {
	Code    coreerrors.ErrorCode `json:"code"`
	Message string               `json:"message"`
	Path    string               `json:"path,omitempty"`
}

type AnalyzeResult struct {
	OK          bool          `json:"ok"`
	ProfileID   string        `json:"profileId"`
	ServerDir   string        `json:"serverDir"`
	LogicalSize int64         `json:"logicalSize"`
	FileCount   int           `json:"fileCount"`
	RootCount   int           `json:"rootCount"`
	Roots       []RootSummary `json:"roots"`
	Warnings    []Warning     `json:"warnings"`
}

type UploadResult struct {
	OK               bool                               `json:"ok"`
	Outcome          string                             `json:"outcome"`
	SnapshotID       string                             `json:"snapshotId"`
	LogicalSize      int64                              `json:"logicalSize"`
	UploadedSize     int64                              `json:"uploadedSize"`
	DeduplicatedSize int64                              `json:"deduplicatedSize"`
	FileCount        int                                `json:"fileCount"`
	RootCount        int                                `json:"rootCount"`
	ActualRequestURL string                             `json:"actualRequestUrl,omitempty"`
	CoordinatorURL   string                             `json:"coordinatorUrl"`
	NetworkRequests  []coordinatorclient.NetworkRequest `json:"networkRequests,omitempty"`
}

type SnapshotSummary struct {
	SnapshotID        string `json:"snapshotId"`
	ProfileID         string `json:"profileId"`
	Status            string `json:"status"`
	CreatedAt         string `json:"createdAt"`
	CompletedAt       string `json:"completedAt,omitempty"`
	LogicalSize       int64  `json:"logicalSize"`
	FileCount         int    `json:"fileCount"`
	RootCount         int    `json:"rootCount"`
	ServerDisplayName string `json:"serverDisplayName"`
	TraceID           string `json:"traceId,omitempty"`
}

type ListResult struct {
	OK               bool                               `json:"ok"`
	Snapshots        []SnapshotSummary                  `json:"snapshots"`
	ActualRequestURL string                             `json:"actualRequestUrl,omitempty"`
	NetworkRequests  []coordinatorclient.NetworkRequest `json:"networkRequests,omitempty"`
}

type DownloadRequest struct {
	TargetDir     string `json:"targetDir"`
	DryRun        bool   `json:"dryRun"`
	AllowNonEmpty bool   `json:"allowNonEmpty"`
}

type DownloadResult struct {
	OK               bool                               `json:"ok"`
	SnapshotID       string                             `json:"snapshotId"`
	TargetDir        string                             `json:"targetDir"`
	DownloadedFiles  int                                `json:"downloadedFiles"`
	LogicalSize      int64                              `json:"logicalSize"`
	AppliedRoots     []string                           `json:"appliedRoots"`
	ActualRequestURL string                             `json:"actualRequestUrl,omitempty"`
	NetworkRequests  []coordinatorclient.NetworkRequest `json:"networkRequests,omitempty"`
}

type Service struct {
	Client     Client
	HTTPClient *http.Client
}

type scannedFile struct {
	Path      string
	LocalPath string
	Size      int64
	SHA256    string
}

type scanResult struct {
	analyze AnalyzeResult
	files   []scannedFile
}

func (s Service) Analyze(ctx context.Context, cfg coreconfig.Config, req AnalyzeRequest) (AnalyzeResult, *coreerrors.Error) {
	_ = ctx
	scanned, err := scan(cfg, req, false)
	if err != nil {
		return AnalyzeResult{}, err
	}
	return scanned.analyze, nil
}

func (s Service) Upload(ctx context.Context, cfg coreconfig.Config, req AnalyzeRequest) (UploadResult, *coreerrors.Error) {
	coordIdentity, err := identity.Adapter(cfg)
	if err != nil {
		return UploadResult{}, err
	}
	client, err := s.client(cfg)
	if err != nil {
		return UploadResult{}, err
	}
	probe, probeErr := client.Probe(ctx)
	if probeErr != nil {
		return UploadResult{}, probeErr
	}
	requests := []coordinatorclient.NetworkRequest{{Stage: "coordinator_probe", Method: http.MethodGet, ActualRequestURL: probe.ActualRequestURL, HTTPStatus: http.StatusOK}}
	lease, leaseErr := client.EnsureActiveLeaseWithGeneration(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken, nil)
	if leaseErr != nil {
		return UploadResult{}, leaseErr
	}
	requests = append(requests, networkRequest("ensure_active", http.MethodPost, cfg, "/v1/groups/"+url.PathEscape(coordIdentity.GroupID)+"/lease/ensure-active", http.StatusOK))
	generation := lease.Lease.Generation
	scanned, scanErr := scan(cfg, req, true)
	if scanErr != nil {
		return UploadResult{}, scanErr
	}
	var parent *worldbackup.Manifest
	if latest, latestErr := client.GetLatestWorldBackup(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken, false); latestErr == nil {
		parent = &latest.Manifest
		requests = append(requests, networkRequest("latest_manifest", http.MethodGet, cfg, "/v1/groups/"+url.PathEscape(coordIdentity.GroupID)+"/world-backups/latest", http.StatusOK))
	}
	snapshotID := "ws_" + time.Now().UTC().Format("20060102_150405")
	manifest, changed, objects := buildManifest(scanned.files, parent, snapshotID, coordIdentity.GroupID, coordIdentity.HostID, generation)
	plan, planErr := client.PlanWorldBackup(ctx, coordIdentity.GroupID, coordinatorclient.WorldBackupPlanRequest{
		HostID:           coordIdentity.HostID,
		HostToken:        coordIdentity.HostToken,
		HostGeneration:   generation,
		ParentSnapshotID: manifest.ParentSnapshotID,
		Objects:          objects,
	})
	if planErr != nil {
		return UploadResult{}, planErr
	}
	requests = append(requests, networkRequest("backup_plan", http.MethodPost, cfg, "/v1/groups/"+url.PathEscape(coordIdentity.GroupID)+"/world-backups/plan", http.StatusOK))
	bySHA := map[string]scannedFile{}
	for _, file := range changed {
		if _, ok := bySHA[file.SHA256]; !ok {
			bySHA[file.SHA256] = file
		}
	}
	var uploadedSize int64
	for _, object := range plan.MissingObjects {
		file, ok := bySHA[object.SHA256]
		if !ok {
			return UploadResult{}, coreerrors.New(coreerrors.InvalidRequest, "coordinator requested unknown backup object", coreerrors.Details{CoordinatorURL: cfg.CoordinatorURL}, "Retry backup upload.")
		}
		in, openErr := os.Open(file.LocalPath)
		if openErr != nil {
			return UploadResult{}, coreerrors.New(coreerrors.InvalidRequest, "open backup object failed", coreerrors.Details{Path: file.Path}, openErr.Error())
		}
		_, uploadErr := client.UploadWorldObjectStream(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken, object.SHA256, in, object.Size)
		closeErr := in.Close()
		if uploadErr != nil {
			return UploadResult{}, uploadErr
		}
		if closeErr != nil && !strings.Contains(closeErr.Error(), "file already closed") {
			return UploadResult{}, coreerrors.New(coreerrors.InvalidRequest, "close backup object failed", coreerrors.Details{Path: file.Path}, closeErr.Error())
		}
		uploadedSize += object.Size
		requests = append(requests, networkRequest("object_upload", http.MethodPut, cfg, "/v1/groups/"+url.PathEscape(coordIdentity.GroupID)+"/world-objects/"+url.PathEscape(object.SHA256), http.StatusOK))
	}
	manifest.UploadedSize = uploadedSize
	if err := worldbackup.ValidateManifest(manifest); err != nil {
		return UploadResult{}, coreerrors.New(coreerrors.InvalidRequest, "backup manifest is invalid", coreerrors.Details{}, err.Error())
	}
	commit, commitErr := client.CommitWorldBackup(ctx, coordIdentity.GroupID, coordinatorclient.WorldBackupCommitRequest{
		HostID:         coordIdentity.HostID,
		HostToken:      coordIdentity.HostToken,
		HostGeneration: generation,
		Manifest:       manifest,
	})
	if commitErr != nil {
		return UploadResult{}, commitErr
	}
	requests = append(requests, networkRequest("snapshot_commit", http.MethodPost, cfg, "/v1/groups/"+url.PathEscape(coordIdentity.GroupID)+"/world-backups/commit", http.StatusOK))
	return UploadResult{
		OK:               true,
		Outcome:          "success",
		SnapshotID:       commit.SnapshotID,
		LogicalSize:      manifest.LogicalSize,
		UploadedSize:     uploadedSize,
		DeduplicatedSize: maxInt64(manifest.LogicalSize-uploadedSize, 0),
		FileCount:        manifest.FileCount,
		RootCount:        scanned.analyze.RootCount,
		ActualRequestURL: probe.ActualRequestURL,
		CoordinatorURL:   cfg.CoordinatorURL,
		NetworkRequests:  requests,
	}, nil
}

func (s Service) List(ctx context.Context, cfg coreconfig.Config) (ListResult, *coreerrors.Error) {
	coordIdentity, err := identity.Adapter(cfg)
	if err != nil {
		return ListResult{}, err
	}
	client, err := s.client(cfg)
	if err != nil {
		return ListResult{}, err
	}
	out, listErr := client.ListWorldBackups(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken)
	if listErr != nil {
		return ListResult{}, listErr
	}
	actualURL := requestURL(cfg, "/v1/groups/"+url.PathEscape(coordIdentity.GroupID)+"/world-backups")
	snapshots := make([]SnapshotSummary, 0, len(out.Snapshots))
	for _, item := range out.Snapshots {
		completed := ""
		if item.CompletedAt != nil {
			completed = *item.CompletedAt
		}
		traceID := ""
		if item.TraceID != nil {
			traceID = *item.TraceID
		}
		status := item.Status
		if status == "" {
			status = "success"
		}
		rootCount := item.RootCount
		if rootCount == 0 {
			rootCount = len(cfg.Backup.Include)
		}
		snapshots = append(snapshots, SnapshotSummary{
			SnapshotID:        item.SnapshotID,
			ProfileID:         firstNonEmpty(item.ProfileID, cfg.Backup.ProfileID),
			Status:            status,
			CreatedAt:         item.CreatedAt,
			CompletedAt:       completed,
			LogicalSize:       item.LogicalSize,
			FileCount:         item.FileCount,
			RootCount:         rootCount,
			ServerDisplayName: cfg.Server.DisplayName,
			TraceID:           traceID,
		})
	}
	sort.SliceStable(snapshots, func(i, j int) bool { return snapshots[i].CreatedAt > snapshots[j].CreatedAt })
	return ListResult{
		OK:               true,
		Snapshots:        snapshots,
		ActualRequestURL: actualURL,
		NetworkRequests:  []coordinatorclient.NetworkRequest{{Stage: "snapshot_list", Method: http.MethodGet, ActualRequestURL: actualURL, HTTPStatus: http.StatusOK}},
	}, nil
}

func (s Service) Download(ctx context.Context, cfg coreconfig.Config, snapshotID string, req DownloadRequest) (DownloadResult, *coreerrors.Error) {
	if strings.TrimSpace(req.TargetDir) == "" {
		return DownloadResult{}, coreerrors.New(coreerrors.TargetDirRequired, "targetDir is required", coreerrors.Details{}, "Choose a new empty restore directory.")
	}
	targetDir, absErr := filepath.Abs(req.TargetDir)
	if absErr != nil {
		return DownloadResult{}, coreerrors.New(coreerrors.InvalidRequest, "targetDir is invalid", coreerrors.Details{Path: req.TargetDir}, absErr.Error())
	}
	if err := validateTargetDir(targetDir, req.AllowNonEmpty); err != nil {
		return DownloadResult{}, err
	}
	coordIdentity, err := identity.Adapter(cfg)
	if err != nil {
		return DownloadResult{}, err
	}
	client, err := s.client(cfg)
	if err != nil {
		return DownloadResult{}, err
	}
	var remote coordinatorclient.WorldBackupManifestResponse
	var dlErr *coreerrors.Error
	manifestPath := "/v1/groups/" + url.PathEscape(coordIdentity.GroupID) + "/world-backups/" + url.PathEscape(snapshotID)
	if snapshotID == "" || snapshotID == "latest" {
		manifestPath = "/v1/groups/" + url.PathEscape(coordIdentity.GroupID) + "/world-backups/latest"
		remote, dlErr = client.GetLatestWorldBackup(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken, false)
	} else {
		remote, dlErr = client.GetWorldBackup(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken, snapshotID)
	}
	if dlErr != nil {
		if dlErr.ErrorCode == coreerrors.CoordinatorRouteMissing {
			dlErr.ErrorCode = coreerrors.SnapshotNotFound
		}
		return DownloadResult{}, dlErr
	}
	if req.DryRun {
		actualURL := requestURL(cfg, manifestPath)
		return DownloadResult{OK: true, SnapshotID: remote.Manifest.SnapshotID, TargetDir: targetDir, LogicalSize: remote.Manifest.LogicalSize, ActualRequestURL: actualURL, NetworkRequests: []coordinatorclient.NetworkRequest{{Stage: "snapshot_manifest", Method: http.MethodGet, ActualRequestURL: actualURL, HTTPStatus: http.StatusOK}}}, nil
	}
	requests := []coordinatorclient.NetworkRequest{networkRequest("snapshot_manifest", http.MethodGet, cfg, manifestPath, http.StatusOK)}
	var downloaded int
	var roots []string
	seenRoots := map[string]bool{}
	for _, file := range remote.Manifest.Files {
		target, err := safeTarget(targetDir, file.Path)
		if err != nil {
			return DownloadResult{}, err
		}
		if err := ensureSafeParent(targetDir, filepath.Dir(target)); err != nil {
			return DownloadResult{}, err
		}
		sha := strings.TrimPrefix(file.ObjectID, "sha256:")
		body, _, getErr := client.DownloadWorldObjectStream(ctx, coordIdentity.GroupID, coordIdentity.HostID, coordIdentity.HostToken, sha)
		if getErr != nil {
			return DownloadResult{}, getErr
		}
		if err := writeDownloadedObject(body, target, file.SHA256, file.Size); err != nil {
			return DownloadResult{}, err
		}
		requests = append(requests, networkRequest("object_download", http.MethodGet, cfg, "/v1/groups/"+url.PathEscape(coordIdentity.GroupID)+"/world-objects/"+url.PathEscape(sha), http.StatusOK))
		downloaded++
		root := strings.Split(file.Path, "/")[0]
		if root != "" && !seenRoots[root] {
			seenRoots[root] = true
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return DownloadResult{OK: true, SnapshotID: remote.Manifest.SnapshotID, TargetDir: targetDir, DownloadedFiles: downloaded, LogicalSize: remote.Manifest.LogicalSize, AppliedRoots: roots, ActualRequestURL: requestURL(cfg, manifestPath), NetworkRequests: requests}, nil
}

func (s Service) client(cfg coreconfig.Config) (Client, *coreerrors.Error) {
	if s.Client != nil {
		return s.Client, nil
	}
	return coordinatorclient.NewWithHTTPClient(cfg.CoordinatorURL, s.HTTPClient)
}

func scan(cfg coreconfig.Config, req AnalyzeRequest, hash bool) (scanResult, *coreerrors.Error) {
	serverDir := firstNonEmpty(req.ServerDir, cfg.Server.Dir)
	if strings.TrimSpace(serverDir) == "" {
		return scanResult{}, coreerrors.New(coreerrors.ConfigInvalid, "serverDir is required", coreerrors.Details{}, "Set server.dir or pass serverDir.")
	}
	abs, err := filepath.Abs(serverDir)
	if err != nil {
		return scanResult{}, coreerrors.New(coreerrors.ConfigInvalid, "serverDir is invalid", coreerrors.Details{Path: serverDir}, err.Error())
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		msg := "serverDir does not exist"
		if err == nil {
			msg = "serverDir is not a directory"
		}
		return scanResult{}, coreerrors.New(coreerrors.ConfigInvalid, msg, coreerrors.Details{Path: abs}, fmt.Sprint(err))
	}
	excludes := parseRules(cfg.Backup.Exclude)
	profileID := firstNonEmpty(req.ProfileID, cfg.Backup.ProfileID)
	result := AnalyzeResult{OK: true, ProfileID: profileID, ServerDir: abs}
	var files []scannedFile
	for _, rule := range parseRules(cfg.Backup.Include) {
		rootAbs := filepath.Join(abs, filepath.FromSlash(rule.path))
		if _, err := os.Stat(rootAbs); os.IsNotExist(err) {
			result.Warnings = append(result.Warnings, Warning{Code: coreerrors.ConfigInvalid, Message: "optional backup path is missing", Path: rule.kind + ":" + rule.path})
			continue
		} else if err != nil {
			return scanResult{}, coreerrors.New(coreerrors.ConfigInvalid, "inspect backup path failed", coreerrors.Details{Path: rootAbs}, err.Error())
		}
		summary := RootSummary{Kind: rule.kind, Path: rule.path}
		if err := collect(abs, rootAbs, rule, excludes, hash, &summary, &files); err != nil {
			return scanResult{}, err
		}
		if summary.FileCount > 0 || rule.kind == "file" || rule.kind == "dir" {
			result.Roots = append(result.Roots, summary)
			result.FileCount += summary.FileCount
			result.LogicalSize += summary.Size
		}
	}
	result.RootCount = len(result.Roots)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return scanResult{analyze: result, files: files}, nil
}

type rule struct {
	kind string
	path string
}

func parseRules(values []string) []rule {
	out := make([]rule, 0, len(values))
	for _, value := range values {
		kind, raw, ok := strings.Cut(value, ":")
		if !ok {
			continue
		}
		normalized, err := worldbackup.NormalizeManifestPath(raw)
		if err != nil {
			continue
		}
		out = append(out, rule{kind: kind, path: normalized})
	}
	return out
}

func collect(root string, start string, include rule, excludes []rule, hash bool, summary *RootSummary, files *[]scannedFile) *coreerrors.Error {
	if include.kind == "file" {
		return collectFile(root, start, excludes, hash, summary, files)
	}
	err := filepath.WalkDir(start, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == start {
			return nil
		}
		rel, err := relPath(root, filePath)
		if err != nil {
			return err
		}
		if excluded(rel, entry.IsDir(), excludes) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup path %s is a symlink and is not allowed", rel)
		}
		if entry.IsDir() {
			return nil
		}
		if err := addFile(root, filePath, rel, hash, summary, files); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		var coreErr *coreerrors.Error
		if errors.As(err, &coreErr) {
			return coreErr
		}
		return coreerrors.New(coreerrors.InvalidRequest, "backup analyze failed", coreerrors.Details{Path: start}, err.Error())
	}
	return nil
}

func collectFile(root string, filePath string, excludes []rule, hash bool, summary *RootSummary, files *[]scannedFile) *coreerrors.Error {
	rel, err := relPath(root, filePath)
	if err != nil {
		return coreerrors.New(coreerrors.InvalidRequest, "backup path invalid", coreerrors.Details{Path: filePath}, err.Error())
	}
	if excluded(rel, false, excludes) {
		return nil
	}
	return addFile(root, filePath, rel, hash, summary, files)
}

func addFile(root string, filePath string, rel string, hash bool, summary *RootSummary, files *[]scannedFile) *coreerrors.Error {
	info, err := os.Stat(filePath)
	if err != nil {
		return coreerrors.New(coreerrors.InvalidRequest, "stat backup file failed", coreerrors.Details{Path: rel}, err.Error())
	}
	if info.IsDir() {
		return nil
	}
	sum := ""
	if hash {
		sum, err = hashFile(filePath)
		if err != nil {
			return coreerrors.New(coreerrors.InvalidRequest, "hash backup file failed", coreerrors.Details{Path: rel}, err.Error())
		}
	}
	summary.FileCount++
	summary.Size += info.Size()
	*files = append(*files, scannedFile{Path: rel, LocalPath: filePath, Size: info.Size(), SHA256: sum})
	_ = root
	return nil
}

func relPath(root string, filePath string) (string, error) {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return "", err
	}
	return worldbackup.NormalizeManifestPath(filepath.ToSlash(rel))
}

func excluded(rel string, isDir bool, excludes []rule) bool {
	for _, rule := range excludes {
		if rule.kind == "dir" && (rel == rule.path || strings.HasPrefix(rel, rule.path+"/")) {
			return true
		}
		if !isDir && rule.kind == "file" && rel == rule.path {
			return true
		}
	}
	return false
}

func buildManifest(files []scannedFile, parent *worldbackup.Manifest, snapshotID string, groupID string, hostID string, generation int) (worldbackup.Manifest, []scannedFile, []worldbackup.PlannedObject) {
	parentFiles := map[string]worldbackup.FileEntry{}
	parentID := ""
	if parent != nil {
		parentID = parent.SnapshotID
		for _, file := range parent.Files {
			parentFiles[file.Path] = file
		}
	}
	entries := make([]worldbackup.FileEntry, 0, len(files))
	var changed []scannedFile
	objectsBySHA := map[string]worldbackup.PlannedObject{}
	var logical int64
	for _, file := range files {
		entry := worldbackup.FileEntry{Path: file.Path, Size: file.Size, SHA256: file.SHA256, ObjectID: worldbackup.ObjectID(file.SHA256)}
		entries = append(entries, entry)
		logical += file.Size
		if old, ok := parentFiles[file.Path]; !ok || old.Size != entry.Size || old.SHA256 != entry.SHA256 {
			changed = append(changed, file)
			if _, seen := objectsBySHA[file.SHA256]; !seen {
				objectsBySHA[file.SHA256] = worldbackup.PlannedObject{SHA256: file.SHA256, Size: file.Size, Path: file.Path}
			}
		}
	}
	objects := make([]worldbackup.PlannedObject, 0, len(objectsBySHA))
	for _, object := range objectsBySHA {
		objects = append(objects, object)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].SHA256 < objects[j].SHA256 })
	return worldbackup.Manifest{
		SchemaVersion:    worldbackup.SchemaVersion,
		SnapshotID:       snapshotID,
		GroupID:          groupID,
		SourceHostID:     hostID,
		HostGeneration:   generation,
		ParentSnapshotID: parentID,
		CreatedAt:        time.Now().UTC(),
		Consistent:       true,
		LogicalSize:      logical,
		FileCount:        len(entries),
		ChangedFileCount: len(changed),
		DeletedFileCount: 0,
		Files:            entries,
	}, changed, objects
}

func validateTargetDir(targetDir string, allowNonEmpty bool) *coreerrors.Error {
	if info, err := os.Lstat(targetDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || hasReparsePoint(targetDir) {
			return coreerrors.New(coreerrors.RestorePathEscapeBlocked, "targetDir is a symlink or reparse point", coreerrors.Details{Path: targetDir}, "Choose a normal directory.")
		}
		if !info.IsDir() {
			return coreerrors.New(coreerrors.InvalidRequest, "targetDir is not a directory", coreerrors.Details{Path: targetDir}, "Choose a directory.")
		}
		entries, readErr := os.ReadDir(targetDir)
		if readErr != nil {
			return coreerrors.New(coreerrors.InvalidRequest, "read targetDir failed", coreerrors.Details{Path: targetDir}, readErr.Error())
		}
		if len(entries) > 0 && !allowNonEmpty {
			return coreerrors.New(coreerrors.TargetDirNotEmpty, "targetDir is not empty", coreerrors.Details{Path: targetDir}, "Choose an empty directory or set allowNonEmpty=true.")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return coreerrors.New(coreerrors.InvalidRequest, "inspect targetDir failed", coreerrors.Details{Path: targetDir}, err.Error())
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return coreerrors.New(coreerrors.InvalidRequest, "create targetDir failed", coreerrors.Details{Path: targetDir}, err.Error())
	}
	return nil
}

func safeTarget(root string, rel string) (string, *coreerrors.Error) {
	normalized, err := worldbackup.NormalizeManifestPath(rel)
	if err != nil {
		return "", coreerrors.New(coreerrors.RestorePathEscapeBlocked, "snapshot path is unsafe", coreerrors.Details{Path: rel}, err.Error())
	}
	target := filepath.Join(root, filepath.FromSlash(normalized))
	if !underRoot(root, target) {
		return "", coreerrors.New(coreerrors.RestorePathEscapeBlocked, "restore path escape blocked", coreerrors.Details{Path: rel}, "Snapshot path must stay inside targetDir.")
	}
	return target, nil
}

func ensureSafeParent(root string, parent string) *coreerrors.Error {
	if !underRoot(root, parent) {
		return coreerrors.New(coreerrors.RestorePathEscapeBlocked, "restore parent escape blocked", coreerrors.Details{Path: parent}, "Snapshot path must stay inside targetDir.")
	}
	rel, _ := filepath.Rel(root, parent)
	current := root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			if info, err := os.Lstat(current); err == nil {
				if info.Mode()&os.ModeSymlink != 0 || hasReparsePoint(current) {
					return coreerrors.New(coreerrors.RestorePathEscapeBlocked, "restore path escape blocked", coreerrors.Details{Path: current}, "Restore parent is a symlink or reparse point.")
				}
			} else if errors.Is(err, os.ErrNotExist) {
				if mkErr := os.Mkdir(current, 0o755); mkErr != nil && !errors.Is(mkErr, os.ErrExist) {
					return coreerrors.New(coreerrors.InvalidRequest, "create restore parent failed", coreerrors.Details{Path: current}, mkErr.Error())
				}
			} else {
				return coreerrors.New(coreerrors.InvalidRequest, "inspect restore parent failed", coreerrors.Details{Path: current}, err.Error())
			}
		}
	}
	return nil
}

func writeDownloadedObject(body io.ReadCloser, target string, expectedSHA string, expectedSize int64) *coreerrors.Error {
	defer body.Close()
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || hasReparsePoint(target) {
			return coreerrors.New(coreerrors.RestorePathEscapeBlocked, "restore path escape blocked", coreerrors.Details{Path: target}, "Restore target is a symlink or reparse point.")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return coreerrors.New(coreerrors.InvalidRequest, "inspect restore target failed", coreerrors.Details{Path: target}, err.Error())
	}
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return coreerrors.New(coreerrors.SnapshotDownloadFailed, "create restore file failed", coreerrors.Details{Path: target}, err.Error())
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(out, hash), body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return coreerrors.New(coreerrors.SnapshotDownloadFailed, "download snapshot object failed", coreerrors.Details{Path: target}, copyErr.Error())
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return coreerrors.New(coreerrors.SnapshotDownloadFailed, "close restore file failed", coreerrors.Details{Path: target}, closeErr.Error())
	}
	if size != expectedSize || hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
		_ = os.Remove(tmp)
		return coreerrors.New(coreerrors.SnapshotDownloadFailed, "downloaded snapshot object verification failed", coreerrors.Details{Path: target}, "Size or sha256 mismatch.")
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return coreerrors.New(coreerrors.SnapshotDownloadFailed, "move restore file failed", coreerrors.Details{Path: target}, err.Error())
	}
	return nil
}

func hashFile(filePath string) (string, error) {
	in, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, in); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func underRoot(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func requestURL(cfg coreconfig.Config, path string) string {
	return strings.TrimRight(cfg.CoordinatorURL, "/") + path
}

func networkRequest(stage string, method string, cfg coreconfig.Config, path string, status int) coordinatorclient.NetworkRequest {
	return coordinatorclient.NetworkRequest{Stage: stage, Method: method, ActualRequestURL: requestURL(cfg, path), HTTPStatus: status}
}

func normalizeObjectID(objectID string) string {
	return strings.TrimPrefix(objectID, "sha256:")
}

var _ = path.Clean
