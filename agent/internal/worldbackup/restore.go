package worldbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ObjectDownloader func(ctx context.Context, objectID string) (io.ReadCloser, int64, error)

type RestoreOptions struct {
	ServerDir      string
	Manifest       Manifest
	Downloader     ObjectDownloader
	ConsistentOnly bool
	TransactionID  string
}

type RestoreSummary struct {
	SnapshotID       string   `json:"snapshotId"`
	TransactionID    string   `json:"transactionId"`
	DownloadedFiles  int      `json:"downloadedFiles"`
	AppliedRoots     []string `json:"appliedRoots"`
	RollbackRoots    []string `json:"rollbackRoots"`
	FailedRoots      []string `json:"failedRoots,omitempty"`
	StagingDirectory string   `json:"stagingDirectory"`
}

func Restore(ctx context.Context, opts RestoreOptions) (RestoreSummary, error) {
	if opts.Downloader == nil {
		return RestoreSummary{}, fmt.Errorf("object downloader is required")
	}
	if err := ValidateManifest(opts.Manifest); err != nil {
		return RestoreSummary{}, err
	}
	if opts.ConsistentOnly && !opts.Manifest.Consistent {
		return RestoreSummary{}, fmt.Errorf("snapshot %s is inconsistent and cannot be automatically restored", opts.Manifest.SnapshotID)
	}
	serverDir, err := filepath.Abs(opts.ServerDir)
	if err != nil {
		return RestoreSummary{}, fmt.Errorf("resolve server directory: %w", err)
	}
	if err := prepareRestoreDirectory(serverDir); err != nil {
		return RestoreSummary{}, err
	}
	if opts.TransactionID == "" {
		opts.TransactionID = "restore-" + time.Now().UTC().Format("20060102-150405.000000000")
	}
	opts.TransactionID = safeTransactionID(opts.TransactionID)
	stagingDir := filepath.Join(serverDir, ".acbh-world-staging-"+opts.TransactionID)
	if err := os.RemoveAll(stagingDir); err != nil {
		return RestoreSummary{}, fmt.Errorf("clean stale staging directory: %w", err)
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return RestoreSummary{}, fmt.Errorf("create staging directory: %w", err)
	}
	summary := RestoreSummary{
		SnapshotID:       opts.Manifest.SnapshotID,
		TransactionID:    opts.TransactionID,
		StagingDirectory: stagingDir,
	}
	applied := false
	defer func() {
		if !applied {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	for _, file := range opts.Manifest.Files {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		default:
		}
		target, err := safeJoin(stagingDir, file.Path)
		if err != nil {
			return summary, err
		}
		if err := ensureRestoreParent(stagingDir, target); err != nil {
			return summary, fmt.Errorf("create staging parent for %s: %w", file.Path, err)
		}
		if err := downloadObject(ctx, opts.Downloader, file.ObjectID, file.SHA256, file.Size, target); err != nil {
			return summary, fmt.Errorf("stage %s: %w", file.Path, err)
		}
		summary.DownloadedFiles++
	}

	roots, err := manifestTopRoots(opts.Manifest)
	if err != nil {
		return summary, err
	}
	if len(roots) == 0 {
		applied = true
		return summary, os.RemoveAll(stagingDir)
	}
	if err := ensureSameVolume(serverDir, stagingDir); err != nil {
		return summary, err
	}
	rollback := map[string]string{}
	for _, root := range roots {
		current := filepath.Join(serverDir, filepath.FromSlash(root))
		rollbackPath := filepath.Join(serverDir, root+".acbh-rollback-"+opts.TransactionID)
		failedPath := filepath.Join(serverDir, root+".acbh-failed-"+opts.TransactionID)
		if err := ensureRestoreParent(serverDir, current); err != nil {
			return summary, fmt.Errorf("prepare restore root %s: %w", root, err)
		}
		if _, err := os.Stat(rollbackPath); err == nil {
			return summary, fmt.Errorf("rollback path already exists: %s", rollbackPath)
		}
		_ = os.RemoveAll(failedPath)
		if _, err := os.Stat(current); err == nil {
			if _, targetErr := checkRestoreFinalTarget(current); targetErr != nil {
				return summary, fmt.Errorf("check current restore root %s: %w", root, targetErr)
			}
			if err := os.Rename(current, rollbackPath); err != nil {
				return summary, fmt.Errorf("move current world %s to rollback: %w", root, err)
			}
			rollback[root] = rollbackPath
			summary.RollbackRoots = append(summary.RollbackRoots, rollbackPath)
		} else if !os.IsNotExist(err) {
			return summary, fmt.Errorf("stat current world %s: %w", root, err)
		}
		stageRoot := filepath.Join(stagingDir, filepath.FromSlash(root))
		if _, err := os.Stat(stageRoot); os.IsNotExist(err) {
			if err := os.MkdirAll(stageRoot, 0o700); err != nil {
				return summary, fmt.Errorf("create empty stage root %s: %w", root, err)
			}
		} else if err == nil {
			if _, targetErr := checkRestoreFinalTarget(stageRoot); targetErr != nil {
				return summary, fmt.Errorf("check staged restore root %s: %w", root, targetErr)
			}
		}
		if err := os.Rename(stageRoot, current); err != nil {
			for rolledRoot, rollbackPath := range rollback {
				currentRoot := filepath.Join(serverDir, filepath.FromSlash(rolledRoot))
				failedPath := filepath.Join(serverDir, rolledRoot+".acbh-failed-"+opts.TransactionID)
				if _, statErr := os.Stat(currentRoot); statErr == nil {
					_ = os.Rename(currentRoot, failedPath)
					summary.FailedRoots = append(summary.FailedRoots, failedPath)
				}
				_ = os.Rename(rollbackPath, currentRoot)
			}
			return summary, fmt.Errorf("apply staged world %s: %w", root, err)
		}
		summary.AppliedRoots = append(summary.AppliedRoots, current)
	}
	applied = true
	if err := os.RemoveAll(stagingDir); err != nil {
		return summary, fmt.Errorf("clean staging directory: %w", err)
	}
	return summary, nil
}

func manifestTopRoots(m Manifest) ([]string, error) {
	seen := map[string]struct{}{}
	for _, file := range m.Files {
		parts := strings.Split(file.Path, "/")
		if len(parts) == 0 || parts[0] == "" {
			return nil, fmt.Errorf("manifest path %q has no world root", file.Path)
		}
		seen[parts[0]] = struct{}{}
	}
	for _, deletedPath := range m.DeletedPaths {
		parts := strings.Split(deletedPath, "/")
		if len(parts) > 0 && parts[0] != "" {
			seen[parts[0]] = struct{}{}
		}
	}
	roots := make([]string, 0, len(seen))
	for root := range seen {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots, nil
}

func downloadObject(ctx context.Context, download ObjectDownloader, objectID, expectedSHA string, expectedSize int64, target string) error {
	if sha, err := objectSHA(objectID); err != nil {
		return err
	} else if sha != expectedSHA {
		return fmt.Errorf("objectId sha does not match manifest sha")
	}
	body, _, err := download(ctx, objectID)
	if err != nil {
		return err
	}
	defer body.Close()
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(out, hash), body)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("download object: %w", copyErr)
	}
	if syncErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("sync staged object: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close staged object: %w", closeErr)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedSHA {
		_ = os.Remove(tmp)
		return fmt.Errorf("downloaded object sha256 mismatch: manifest=%s actual=%s", expectedSHA, actual)
	}
	if size != expectedSize {
		_ = os.Remove(tmp)
		return fmt.Errorf("downloaded object size mismatch: manifest=%d actual=%d", expectedSize, size)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("move staged object into place: %w", err)
	}
	return nil
}

func safeJoin(root, rel string) (string, error) {
	normalized, err := NormalizeManifestPath(rel)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(normalized))
	if !underRoot(root, target) {
		return "", fmt.Errorf("path %q escapes restore root", rel)
	}
	return target, nil
}

func ensureSameVolume(serverDir, stagingDir string) error {
	serverParent := filepath.Clean(serverDir)
	stagingParent := filepath.Dir(filepath.Clean(stagingDir))
	if filepath.Clean(stagingParent) != serverParent {
		return fmt.Errorf("staging directory must be inside server directory for atomic rename")
	}
	return nil
}

func safeTransactionID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		return "txn"
	}
	return out
}
