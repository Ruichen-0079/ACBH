package artifactsync

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

type Client interface {
	UploadObject(ctx context.Context, req coordinator.UploadObjectRequest) (coordinator.UploadObjectResponse, error)
	UploadManifest(ctx context.Context, req coordinator.UploadManifestRequest) (coordinator.UploadManifestResponse, error)
	GetLatestArtifact(ctx context.Context, auth coordinator.ArtifactAuth, artifactKind manifest.ArtifactKind) (coordinator.ArtifactMetadata, error)
	DownloadManifest(ctx context.Context, auth coordinator.ArtifactAuth, artifactKind manifest.ArtifactKind, artifactID string) (coordinator.DownloadManifestResponse, error)
	DownloadObject(ctx context.Context, auth coordinator.ArtifactAuth, sha256 string) ([]byte, error)
}

type PushOptions struct {
	ManifestPath string
	ServerDir    string
	Config       agentconfig.Config
	Client       Client
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
	ArtifactKind   manifest.ArtifactKind
	ArtifactID     string
	WrittenFiles   int
	SkippedFiles   int
	PendingDeletes int
	AppliedDeletes int
	TotalBytes     int64
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

	for _, file := range loaded.Files {
		if file.Deleted {
			summary.DeletedEntries++
			continue
		}
		path, err := safeJoin(serverDir, file.Path)
		if err != nil {
			return PushSummary{}, err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return PushSummary{}, fmt.Errorf("read manifest file %s: %w", file.Path, err)
		}
		actual := sha256Hex(content)
		if actual != file.SHA256 {
			return PushSummary{}, fmt.Errorf("local file %s sha256 mismatch: manifest=%s actual=%s", file.Path, file.SHA256, actual)
		}
		resp, err := opts.Client.UploadObject(ctx, coordinator.UploadObjectRequest{
			GroupID:       opts.Config.GroupID,
			HostID:        opts.Config.HostID,
			HostToken:     opts.Config.HostToken,
			SHA256:        file.SHA256,
			ContentBase64: base64.StdEncoding.EncodeToString(content),
		})
		if err != nil {
			return PushSummary{}, err
		}
		if resp.Exists {
			summary.SkippedObjects++
		} else {
			summary.UploadedObjects++
			summary.TotalBytesUploaded += int64(len(content))
		}
	}

	resp, err := opts.Client.UploadManifest(ctx, coordinator.UploadManifestRequest{
		GroupID:      opts.Config.GroupID,
		HostID:       opts.Config.HostID,
		HostToken:    opts.Config.HostToken,
		ArtifactKind: loaded.ArtifactKind,
		ArtifactID:   loaded.ArtifactID,
		Manifest:     loaded,
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
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return PullSummary{}, fmt.Errorf("create output directory: %w", err)
	}

	summary := PullSummary{
		ArtifactKind: downloaded.Manifest.ArtifactKind,
		ArtifactID:   downloaded.Manifest.ArtifactID,
	}

	for _, file := range downloaded.Manifest.Files {
		path, err := safeJoin(outputDir, file.Path)
		if err != nil {
			return PullSummary{}, err
		}

		if file.Deleted {
			if !opts.ApplyDeletes {
				if exists(path) {
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

		content, err := opts.Client.DownloadObject(ctx, auth, file.SHA256)
		if err != nil {
			return PullSummary{}, err
		}
		actual := sha256Hex(content)
		if actual != file.SHA256 {
			return PullSummary{}, fmt.Errorf("downloaded object %s sha256 mismatch: manifest=%s actual=%s", file.Path, file.SHA256, actual)
		}
		if existing, err := os.ReadFile(path); err == nil && sha256Hex(existing) == file.SHA256 {
			summary.SkippedFiles++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return PullSummary{}, fmt.Errorf("create parent directory for %s: %w", file.Path, err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return PullSummary{}, fmt.Errorf("write %s: %w", file.Path, err)
		}
		summary.WrittenFiles++
		summary.TotalBytes += int64(len(content))
	}

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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
