package worldbackup

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersion = 1
	IndexVersion  = 1
)

var (
	snapshotIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	sha256Pattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Manifest struct {
	SchemaVersion    int         `json:"schemaVersion"`
	SnapshotID       string      `json:"snapshotId"`
	GroupID          string      `json:"groupId"`
	SourceHostID     string      `json:"sourceHostId"`
	HostGeneration   int         `json:"hostGeneration"`
	ParentSnapshotID string      `json:"parentSnapshotId,omitempty"`
	CreatedAt        time.Time   `json:"createdAt"`
	Consistent       bool        `json:"consistent"`
	LogicalSize      int64       `json:"logicalSize"`
	UploadedSize     int64       `json:"uploadedSize"`
	FileCount        int         `json:"fileCount"`
	ChangedFileCount int         `json:"changedFileCount"`
	DeletedFileCount int         `json:"deletedFileCount"`
	Files            []FileEntry `json:"files"`
	DeletedPaths     []string    `json:"deletedPaths,omitempty"`
}

type FileEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	ObjectID string `json:"objectId"`
}

type IndexedFile struct {
	Size          int64  `json:"size"`
	MTimeUnixNano int64  `json:"mtimeUnixNano"`
	SHA256        string `json:"sha256"`
	ObjectID      string `json:"objectId"`
}

type Index struct {
	Version          int                    `json:"version"`
	LatestSnapshotID string                 `json:"latestSnapshotId,omitempty"`
	Files            map[string]IndexedFile `json:"files"`
}

type Plan struct {
	SnapshotID       string          `json:"snapshotId"`
	ParentSnapshotID string          `json:"parentSnapshotId,omitempty"`
	LogicalSize      int64           `json:"logicalSize"`
	FileCount        int             `json:"fileCount"`
	ChangedFileCount int             `json:"changedFileCount"`
	DeletedFileCount int             `json:"deletedFileCount"`
	ChangedFiles     []ChangedFile   `json:"changedFiles"`
	DeletedPaths     []string        `json:"deletedPaths,omitempty"`
	Objects          []PlannedObject `json:"objects"`
}

type ChangedFile struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	ObjectID  string `json:"objectId"`
	LocalPath string `json:"localPath"`
}

type PlannedObject struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Path   string `json:"path,omitempty"`
}

type ScanOptions struct {
	ServerDir      string
	AppDataDir     string
	IgnoreRulesDir string
	WorldRoots     []string
	SnapshotID     string
	GroupID        string
	SourceHostID   string
	HostGeneration int
	Parent         *Manifest
	Consistent     bool
	Clock          func() time.Time
}

type Snapshot struct {
	Manifest Manifest
	Plan     Plan
	Index    Index
}

func ValidateManifest(m Manifest) error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", SchemaVersion)
	}
	if !snapshotIDPattern.MatchString(m.SnapshotID) {
		return errors.New("snapshotId is not a safe identifier")
	}
	if !snapshotIDPattern.MatchString(m.GroupID) {
		return errors.New("groupId is not a safe identifier")
	}
	if !snapshotIDPattern.MatchString(m.SourceHostID) {
		return errors.New("sourceHostId is not a safe identifier")
	}
	if m.HostGeneration < 0 {
		return errors.New("hostGeneration must not be negative")
	}
	if m.ParentSnapshotID != "" && !snapshotIDPattern.MatchString(m.ParentSnapshotID) {
		return errors.New("parentSnapshotId is not a safe identifier")
	}
	if m.CreatedAt.IsZero() {
		return errors.New("createdAt is required")
	}
	if m.LogicalSize < 0 || m.UploadedSize < 0 {
		return errors.New("manifest sizes must not be negative")
	}
	if m.FileCount != len(m.Files) {
		return fmt.Errorf("fileCount = %d, want %d", m.FileCount, len(m.Files))
	}
	if m.ChangedFileCount < 0 || m.DeletedFileCount < 0 {
		return errors.New("changed/deleted counts must not be negative")
	}
	if m.DeletedFileCount != len(m.DeletedPaths) {
		return fmt.Errorf("deletedFileCount = %d, want %d", m.DeletedFileCount, len(m.DeletedPaths))
	}

	var previous string
	var logical int64
	for i, file := range m.Files {
		normalized, err := NormalizeManifestPath(file.Path)
		if err != nil {
			return fmt.Errorf("files[%d].path is invalid: %w", i, err)
		}
		if normalized != file.Path {
			return fmt.Errorf("files[%d].path must be normalized", i)
		}
		if previous != "" && file.Path <= previous {
			return errors.New("files must be sorted by path with no duplicates")
		}
		previous = file.Path
		if file.Size < 0 {
			return fmt.Errorf("files[%d].size must not be negative", i)
		}
		if !sha256Pattern.MatchString(file.SHA256) {
			return fmt.Errorf("files[%d].sha256 must be lowercase SHA-256 hex", i)
		}
		if file.ObjectID != "sha256:"+file.SHA256 {
			return fmt.Errorf("files[%d].objectId must match sha256", i)
		}
		logical += file.Size
	}
	if logical != m.LogicalSize {
		return fmt.Errorf("logicalSize = %d, want %d", m.LogicalSize, logical)
	}

	var previousDeleted string
	for i, deletedPath := range m.DeletedPaths {
		normalized, err := NormalizeManifestPath(deletedPath)
		if err != nil {
			return fmt.Errorf("deletedPaths[%d] is invalid: %w", i, err)
		}
		if normalized != deletedPath {
			return fmt.Errorf("deletedPaths[%d] must be normalized", i)
		}
		if previousDeleted != "" && deletedPath <= previousDeleted {
			return errors.New("deletedPaths must be sorted by path with no duplicates")
		}
		previousDeleted = deletedPath
	}
	return nil
}

func NormalizeManifestPath(raw string) (string, error) {
	if raw == "" || strings.Contains(raw, "\x00") {
		return "", errors.New("path is empty or contains a null byte")
	}
	if strings.Contains(raw, "\\") {
		raw = strings.ReplaceAll(raw, "\\", "/")
	}
	if path.IsAbs(raw) || looksLikeWindowsAbs(raw) {
		return "", errors.New("path must be relative")
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", errors.New("path must not traverse outside the server directory")
	}
	return cleaned, nil
}

func ObjectID(sha256 string) string {
	return "sha256:" + sha256
}

func objectSHA(objectID string) (string, error) {
	sha, ok := strings.CutPrefix(objectID, "sha256:")
	if !ok || !sha256Pattern.MatchString(sha) {
		return "", errors.New("objectId must use sha256:<hex>")
	}
	return sha, nil
}

func looksLikeWindowsAbs(raw string) bool {
	return len(raw) >= 3 &&
		((raw[0] >= 'A' && raw[0] <= 'Z') || (raw[0] >= 'a' && raw[0] <= 'z')) &&
		raw[1] == ':' &&
		(raw[2] == '/' || raw[2] == '\\')
}
