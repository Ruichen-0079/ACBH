package artifactsync

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

type Client interface {
	UploadObject(ctx context.Context, req coordinator.UploadObjectRequest) (coordinator.UploadObjectResponse, error)
	UploadObjectStream(ctx context.Context, auth coordinator.ArtifactAuth, sha256 string, content io.Reader, size int64) (coordinator.UploadObjectResponse, error)
	UploadManifest(ctx context.Context, req coordinator.UploadManifestRequest) (coordinator.UploadManifestResponse, error)
	GetLatestArtifact(ctx context.Context, auth coordinator.ArtifactAuth, artifactKind manifest.ArtifactKind) (coordinator.ArtifactMetadata, error)
	DownloadManifest(ctx context.Context, auth coordinator.ArtifactAuth, artifactKind manifest.ArtifactKind, artifactID string) (coordinator.DownloadManifestResponse, error)
	DownloadObjectStream(ctx context.Context, auth coordinator.ArtifactAuth, sha256 string) (io.ReadCloser, int64, error)
}

type PushOptions struct {
	ManifestPath     string
	ServerDir        string
	Config           agentconfig.Config
	Client           Client
	LegacyJSONUpload bool
	HostGeneration   *int
}

type PushSummary struct {
	ArtifactKind       manifest.ArtifactKind
	ArtifactID         string
	UploadedObjects    int
	SkippedObjects     int
	DeletedEntries     int
	TotalBytesUploaded int64
	CoordinatorStatus  string
}

type PullOptions struct {
	ArtifactKind manifest.ArtifactKind
	ArtifactID   string
	OutputDir    string
	ApplyDeletes bool
	Config       agentconfig.Config
	Client       Client
}

type PullSummary struct {
	ArtifactKind      manifest.ArtifactKind
	ArtifactID        string
	WrittenFiles      int
	DownloadedObjects int
	SkippedFiles      int
	PendingDeletes    int
	AppliedDeletes    int
	TotalBytes        int64
	VerifiedFiles     int
	VerifyResult      string
}

func Push(ctx context.Context, opts PushOptions) (PushSummary, error) {
	if opts.Client == nil {
		return PushSummary{}, fmt.Errorf("coordinator client is required")
	}
	if opts.ServerDir == "" {
		return PushSummary{}, fmt.Errorf("server directory is required")
	}

	loaded, err := manifest.LoadFile(opts.ManifestPath)
	if err != nil {
		return PushSummary{}, err
	}
	if loaded.GroupID != opts.Config.GroupID {
		return PushSummary{}, fmt.Errorf("manifest groupId %q does not match local config groupId", loaded.GroupID)
	}
	if loaded.CreatorHostID != opts.Config.HostID {
		return PushSummary{}, fmt.Errorf("manifest creatorHostId %q does not match local config hostId", loaded.CreatorHostID)
	}

	serverDir, err := filepath.Abs(opts.ServerDir)
	if err != nil {
		return PushSummary{}, fmt.Errorf("resolve server directory: %w", err)
	}

	summary := PushSummary{
		ArtifactKind: loaded.ArtifactKind,
		ArtifactID:   loaded.ArtifactID,
	}
	auth := coordinator.ArtifactAuth{
		GroupID:   opts.Config.GroupID,
		HostID:    opts.Config.HostID,
		HostToken: opts.Config.HostToken,
	}

	for _, file := range loaded.Files {
		if file.Deleted {
			summary.DeletedEntries++
			continue
		}
		path, err := safeJoin(serverDir, file.Path)
		if err != nil {
			return PushSummary{}, err
		}
		actual, size, err := hashFile(path)
		if err != nil {
			return PushSummary{}, fmt.Errorf("read manifest file %s: %w", file.Path, err)
		}
		if actual != file.SHA256 {
			return PushSummary{}, fmt.Errorf("local file %s sha256 mismatch: manifest=%s actual=%s", file.Path, file.SHA256, actual)
		}
		if size != file.Size {
			return PushSummary{}, fmt.Errorf("local file %s size mismatch: manifest=%d actual=%d", file.Path, file.Size, size)
		}

		var resp coordinator.UploadObjectResponse
		if opts.LegacyJSONUpload {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return PushSummary{}, fmt.Errorf("read manifest file %s: %w", file.Path, readErr)
			}
			resp, err = opts.Client.UploadObject(ctx, coordinator.UploadObjectRequest{
				GroupID:       opts.Config.GroupID,
				HostID:        opts.Config.HostID,
				HostToken:     opts.Config.HostToken,
				SHA256:        file.SHA256,
				ContentBase64: base64.StdEncoding.EncodeToString(content),
			})
		} else {
			content, openErr := os.Open(path)
			if openErr != nil {
				return PushSummary{}, fmt.Errorf("open manifest file %s: %w", file.Path, openErr)
			}
			resp, err = opts.Client.UploadObjectStream(ctx, auth, file.SHA256, content, size)
			_ = content.Close()
		}
		if err != nil {
			return PushSummary{}, err
		}
		if resp.Exists {
			summary.SkippedObjects++
		} else {
			summary.UploadedObjects++
			summary.TotalBytesUploaded += size
		}
	}

	resp, err := opts.Client.UploadManifest(ctx, coordinator.UploadManifestRequest{
		GroupID:        opts.Config.GroupID,
		HostID:         opts.Config.HostID,
		HostToken:      opts.Config.HostToken,
		ArtifactKind:   loaded.ArtifactKind,
		ArtifactID:     loaded.ArtifactID,
		Manifest:       loaded,
		HostGeneration: opts.HostGeneration,
	})
	if err != nil {
		return PushSummary{}, err
	}
	summary.CoordinatorStatus = resp.Status

	return summary, nil
}

func Pull(ctx context.Context, opts PullOptions) (PullSummary, error) {
	if opts.Client == nil {
		return PullSummary{}, fmt.Errorf("coordinator client is required")
	}
	if opts.OutputDir == "" {
		return PullSummary{}, fmt.Errorf("output directory is required")
	}
	if !manifest.IsValidArtifactKind(opts.ArtifactKind) {
		return PullSummary{}, fmt.Errorf("invalid artifact kind %q", opts.ArtifactKind)
	}

	artifactID := opts.ArtifactID
	auth := coordinator.ArtifactAuth{
		GroupID:   opts.Config.GroupID,
		HostID:    opts.Config.HostID,
		HostToken: opts.Config.HostToken,
	}
	if artifactID == "" || artifactID == "latest" {
		latest, err := opts.Client.GetLatestArtifact(ctx, auth, opts.ArtifactKind)
		if err != nil {
			return PullSummary{}, err
		}
		artifactID = latest.ArtifactID
	}

	downloaded, err := opts.Client.DownloadManifest(ctx, auth, opts.ArtifactKind, artifactID)
	if err != nil {
		return PullSummary{}, err
	}
	if err := manifest.Validate(downloaded.Manifest); err != nil {
		return PullSummary{}, err
	}

	outputDir, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return PullSummary{}, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := prepareRestoreRoot(outputDir); err != nil {
		return PullSummary{}, err
	}

	summary := PullSummary{
		ArtifactKind: downloaded.Manifest.ArtifactKind,
		ArtifactID:   downloaded.Manifest.ArtifactID,
	}

	for _, file := range downloaded.Manifest.Files {
		path, err := resolveRestoreTarget(outputDir, file.Path)
		if err != nil {
			return PullSummary{}, err
		}

		if file.Deleted {
			parentsExist, err := checkExistingRestoreParentDirectories(outputDir, path)
			if err != nil {
				return PullSummary{}, fmt.Errorf("check delete path %s: %w", file.Path, err)
			}
			targetExists := false
			if parentsExist {
				targetExists, err = checkRestoreFinalPath(path)
				if err != nil {
					return PullSummary{}, fmt.Errorf("check delete target %s: %w", file.Path, err)
				}
			}
			if !opts.ApplyDeletes {
				if targetExists {
					summary.PendingDeletes++
				}
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return PullSummary{}, fmt.Errorf("delete %s: %w", file.Path, err)
			}
			summary.AppliedDeletes++
			continue
		}

		if err := ensureRestoreParentDirectories(outputDir, path); err != nil {
			return PullSummary{}, fmt.Errorf("prepare restore path %s: %w", file.Path, err)
		}
		targetExists, err := checkRestoreFinalPath(path)
		if err != nil {
			return PullSummary{}, fmt.Errorf("check restore target %s: %w", file.Path, err)
		}
		if targetExists {
			actual, _, err := hashFile(path)
			if err != nil {
				return PullSummary{}, fmt.Errorf("read restore target %s: %w", file.Path, err)
			}
			if actual == file.SHA256 {
				summary.SkippedFiles++
				continue
			}
		}
		if err := ensureRestoreParentDirectories(outputDir, path); err != nil {
			return PullSummary{}, fmt.Errorf("recheck restore path %s: %w", file.Path, err)
		}
		if _, err := checkRestoreFinalPath(path); err != nil {
			return PullSummary{}, fmt.Errorf("recheck restore target %s: %w", file.Path, err)
		}

		size, err := downloadVerifiedObject(ctx, opts.Client, auth, file.SHA256, file.Size, outputDir, path)
		if err != nil {
			return PullSummary{}, fmt.Errorf("restore %s: %w", file.Path, err)
		}
		summary.WrittenFiles++
		summary.DownloadedObjects++
		summary.TotalBytes += size
	}

	verified, err := VerifyRestoredFiles(outputDir, downloaded.Manifest)
	if err != nil {
		return PullSummary{}, err
	}
	summary.VerifiedFiles = verified.VerifiedFiles
	summary.VerifyResult = "PASS"
	return summary, nil
}

func safeJoin(root string, manifestPath string) (string, error) {
	normalized, err := fileclass.NormalizePath(manifestPath)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(normalized))
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("check path %q: %w", manifestPath, err)
	}
	if relative == ".." || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("path %q escapes target directory", manifestPath)
	}
	return target, nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func downloadVerifiedObject(
	ctx context.Context,
	client Client,
	auth coordinator.ArtifactAuth,
	expectedSHA string,
	expectedSize int64,
	restoreRoot string,
	targetPath string,
) (int64, error) {
	body, _, err := client.DownloadObjectStream(ctx, auth, expectedSHA)
	if err != nil {
		return 0, err
	}

	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".acbh-object-*.tmp")
	if err != nil {
		body.Close()
		return 0, fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, hash), body)
	bodyCloseErr := body.Close()
	syncErr := temporary.Sync()
	fileCloseErr := temporary.Close()
	if copyErr != nil {
		return 0, fmt.Errorf("download object: %w", copyErr)
	}
	if bodyCloseErr != nil {
		return 0, fmt.Errorf("close download: %w", bodyCloseErr)
	}
	if syncErr != nil {
		return 0, fmt.Errorf("sync temporary file: %w", syncErr)
	}
	if fileCloseErr != nil {
		return 0, fmt.Errorf("close temporary file: %w", fileCloseErr)
	}

	actualSHA := hex.EncodeToString(hash.Sum(nil))
	if actualSHA != expectedSHA {
		return 0, fmt.Errorf("downloaded object sha256 mismatch: manifest=%s actual=%s", expectedSHA, actualSHA)
	}
	if size != expectedSize {
		return 0, fmt.Errorf("downloaded object size mismatch: manifest=%d actual=%d", expectedSize, size)
	}
	if err := os.Chmod(temporaryPath, 0o644); err != nil {
		return 0, fmt.Errorf("set file mode: %w", err)
	}
	if err := ensureRestoreParentDirectories(restoreRoot, targetPath); err != nil {
		return 0, fmt.Errorf("recheck restore directory: %w", err)
	}
	if _, err := checkRestoreFinalPath(targetPath); err != nil {
		return 0, fmt.Errorf("recheck restore target: %w", err)
	}
	if err := replaceFile(temporaryPath, targetPath); err != nil {
		return 0, err
	}
	keepTemporary = true
	return size, nil
}

func replaceFile(temporaryPath string, targetPath string) error {
	if _, err := checkRestoreFinalPath(targetPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, targetPath); err == nil {
		return nil
	}
	if _, err := checkRestoreFinalPath(targetPath); err != nil {
		return err
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace target file: %w", err)
	}
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		return fmt.Errorf("move verified object into place: %w", err)
	}
	return nil
}
