package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

const defaultSampleLimit = 10

type Options struct {
	ServerDir            string
	ArtifactKind         manifest.ArtifactKind
	ArtifactID           string
	GroupID              string
	CreatorHostID        string
	ServerPackVersion    string
	ParentArtifactID     string
	PreviousManifestPath string
	OutputPath           string
	Clock                func() time.Time
}

type Report struct {
	ArtifactKind  manifest.ArtifactKind `json:"artifactKind"`
	ServerDir     string                `json:"serverDir"`
	OutputPath    string                `json:"outputPath,omitempty"`
	IncludedFiles int                   `json:"includedFiles"`
	IgnoredFiles  int                   `json:"ignoredFiles"`
	UnknownFiles  int                   `json:"unknownFiles"`
	DeletedFiles  int                   `json:"deletedFiles"`
	TotalBytes    int64                 `json:"totalBytes"`
	IgnoredSample []string              `json:"ignoredSample,omitempty"`
	UnknownSample []string              `json:"unknownSample,omitempty"`
}

func Scan(opts Options) (manifest.Manifest, Report, error) {
	if err := validateOptions(opts); err != nil {
		return manifest.Manifest{}, Report{}, err
	}

	serverDir, err := filepath.Abs(opts.ServerDir)
	if err != nil {
		return manifest.Manifest{}, Report{}, fmt.Errorf("resolve server directory: %w", err)
	}
	info, err := os.Stat(serverDir)
	if err != nil {
		return manifest.Manifest{}, Report{}, fmt.Errorf("stat server directory: %w", err)
	}
	if !info.IsDir() {
		return manifest.Manifest{}, Report{}, fmt.Errorf("server directory %q is not a directory", serverDir)
	}

	report := Report{
		ArtifactKind: opts.ArtifactKind,
		ServerDir:    serverDir,
		OutputPath:   opts.OutputPath,
	}
	var files []manifest.FileEntry

	err = filepath.WalkDir(serverDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == serverDir {
			return nil
		}

		relPath, err := filepath.Rel(serverDir, filePath)
		if err != nil {
			return fmt.Errorf("find relative path for %q: %w", filePath, err)
		}
		normalized, err := fileclass.NormalizePath(relPath)
		if err != nil {
			return err
		}

		if entry.Type()&os.ModeSymlink != 0 {
			report.IgnoredFiles++
			addSample(&report.IgnoredSample, normalized+" (symlink)")
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		class := fileclass.ClassifyNormalizedPath(normalized)
		if class == fileclass.Ignored {
			report.IgnoredFiles++
			addSample(&report.IgnoredSample, normalized)
			return nil
		}
		if class == fileclass.Unknown {
			report.UnknownFiles++
			addSample(&report.UnknownSample, normalized)
			return nil
		}
		if opts.ArtifactKind == manifest.ServerPack && normalized == "server.properties" {
			class = fileclass.ServerPack
		}
		if !manifest.ClassAllowedForArtifact(opts.ArtifactKind, class) {
			return nil
		}

		fileInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", normalized, err)
		}
		sum, err := hashFile(filePath)
		if err != nil {
			return err
		}
		modifiedAt := fileInfo.ModTime().UTC().Truncate(time.Second)
		files = append(files, manifest.FileEntry{
			Path:       normalized,
			Class:      class,
			Size:       fileInfo.Size(),
			SHA256:     sum,
			ModifiedAt: &modifiedAt,
			Deleted:    false,
		})
		return nil
	})
	if err != nil {
		return manifest.Manifest{}, Report{}, err
	}

	files, report.DeletedFiles, err = addDeletedEntries(opts, files)
	if err != nil {
		return manifest.Manifest{}, Report{}, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	report.IncludedFiles = countIncluded(files)
	report.TotalBytes = totalBytes(files)

	createdAt := time.Now().UTC().Truncate(time.Second)
	if opts.Clock != nil {
		createdAt = opts.Clock().UTC().Truncate(time.Second)
	}

	var parentArtifactID *string
	if opts.ParentArtifactID != "" {
		parentArtifactID = &opts.ParentArtifactID
	}
	var serverPackVersion *string
	if opts.ServerPackVersion != "" {
		serverPackVersion = &opts.ServerPackVersion
	} else if opts.ArtifactKind == manifest.ServerPack {
		serverPackVersion = &opts.ArtifactID
	}

	out := manifest.Manifest{
		ManifestVersion:   manifest.ManifestVersion,
		ArtifactKind:      opts.ArtifactKind,
		ArtifactID:        opts.ArtifactID,
		GroupID:           opts.GroupID,
		CreatedAt:         createdAt,
		CreatorHostID:     opts.CreatorHostID,
		ParentArtifactID:  parentArtifactID,
		ServerPackVersion: serverPackVersion,
		Files:             files,
		Summary: manifest.Summary{
			IncludedFiles: report.IncludedFiles,
			IgnoredFiles:  report.IgnoredFiles,
			UnknownFiles:  report.UnknownFiles,
			DeletedFiles:  report.DeletedFiles,
			TotalBytes:    report.TotalBytes,
		},
	}
	if err := manifest.Validate(out); err != nil {
		return manifest.Manifest{}, Report{}, err
	}

	return out, report, nil
}

func validateOptions(opts Options) error {
	switch {
	case opts.ServerDir == "":
		return fmt.Errorf("server directory is required")
	case !manifest.IsValidArtifactKind(opts.ArtifactKind):
		return fmt.Errorf("invalid artifact kind %q", opts.ArtifactKind)
	case opts.ArtifactID == "":
		return fmt.Errorf("artifact ID is required")
	case opts.GroupID == "":
		return fmt.Errorf("group ID is required")
	case opts.CreatorHostID == "":
		return fmt.Errorf("creator host ID is required")
	case opts.ArtifactKind == manifest.WorldSnapshot && opts.ServerPackVersion == "":
		return fmt.Errorf("server pack version is required for world-snapshot scans")
	default:
		return nil
	}
}

func addDeletedEntries(opts Options, files []manifest.FileEntry) ([]manifest.FileEntry, int, error) {
	if opts.PreviousManifestPath == "" {
		return files, 0, nil
	}

	previous, err := manifest.LoadFile(opts.PreviousManifestPath)
	if err != nil {
		return nil, 0, fmt.Errorf("load previous manifest: %w", err)
	}
	if previous.ArtifactKind != opts.ArtifactKind {
		return nil, 0, fmt.Errorf("previous manifest artifact kind %q does not match %q", previous.ArtifactKind, opts.ArtifactKind)
	}

	currentPaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		currentPaths[file.Path] = struct{}{}
	}

	deleted := 0
	for _, oldFile := range previous.Files {
		if oldFile.Deleted || !manifest.ClassAllowedForArtifact(opts.ArtifactKind, oldFile.Class) {
			continue
		}
		if _, ok := currentPaths[oldFile.Path]; ok {
			continue
		}
		files = append(files, manifest.FileEntry{
			Path:    oldFile.Path,
			Class:   oldFile.Class,
			Size:    0,
			SHA256:  "",
			Deleted: true,
		})
		deleted++
	}

	return files, deleted, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func countIncluded(files []manifest.FileEntry) int {
	count := 0
	for _, file := range files {
		if !file.Deleted {
			count++
		}
	}
	return count
}

func totalBytes(files []manifest.FileEntry) int64 {
	var total int64
	for _, file := range files {
		if !file.Deleted {
			total += file.Size
		}
	}
	return total
}

func addSample(samples *[]string, sample string) {
	if len(*samples) >= defaultSampleLimit {
		return
	}
	*samples = append(*samples, sample)
}
