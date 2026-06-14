package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func Validate(m Manifest) error {
	switch {
	case m.ManifestVersion != 0 && m.ManifestVersion != ManifestVersion:
		return fmt.Errorf("manifestVersion must be %d", ManifestVersion)
	case !IsValidArtifactKind(m.ArtifactKind):
		return fmt.Errorf("invalid artifactKind %q", m.ArtifactKind)
	case strings.TrimSpace(m.ArtifactID) == "":
		return errors.New("artifactId is required")
	case strings.TrimSpace(m.GroupID) == "":
		return errors.New("groupId is required")
	case m.CreatedAt.IsZero():
		return errors.New("createdAt is required")
	case strings.TrimSpace(m.CreatorHostID) == "":
		return errors.New("creatorHostId is required")
	case m.ArtifactKind == WorldSnapshot && stringPtrEmpty(m.ServerPackVersion):
		return errors.New("serverPackVersion is required for world-snapshot manifests")
	case m.Summary.IgnoredFiles < 0 || m.Summary.UnknownFiles < 0:
		return errors.New("summary ignoredFiles and unknownFiles must not be negative")
	}

	var includedFiles int
	var deletedFiles int
	var totalBytes int64
	seenPaths := make(map[string]struct{}, len(m.Files))
	var previousPath string

	for i, file := range m.Files {
		normalized, err := fileclass.NormalizePath(file.Path)
		if err != nil {
			return fmt.Errorf("files[%d].path is invalid: %w", i, err)
		}
		if normalized != file.Path {
			return fmt.Errorf("files[%d].path must be normalized slash-separated relative path", i)
		}
		if previousPath != "" && file.Path <= previousPath {
			return fmt.Errorf("files must be sorted by path with no duplicates")
		}
		previousPath = file.Path
		if _, ok := seenPaths[file.Path]; ok {
			return fmt.Errorf("duplicate file path %q", file.Path)
		}
		seenPaths[file.Path] = struct{}{}

		if !fileclass.IsKnownClass(file.Class) {
			return fmt.Errorf("files[%d].class is invalid: %q", i, file.Class)
		}
		if file.Class == fileclass.Ignored || file.Class == fileclass.Unknown {
			return fmt.Errorf("files[%d].class must not be ignored or unknown", i)
		}
		if !ClassAllowedForArtifact(m.ArtifactKind, file.Class) {
			return fmt.Errorf("files[%d].class %q is not allowed for %s", i, file.Class, m.ArtifactKind)
		}
		if file.Size < 0 {
			return fmt.Errorf("files[%d].size must not be negative", i)
		}

		if file.Deleted {
			if file.Size != 0 {
				return fmt.Errorf("files[%d].size must be 0 for deleted entries", i)
			}
			if file.SHA256 != "" {
				return fmt.Errorf("files[%d].sha256 must be empty for deleted entries", i)
			}
			deletedFiles++
			continue
		}

		if !sha256Pattern.MatchString(file.SHA256) {
			return fmt.Errorf("files[%d].sha256 must be lowercase SHA256 hex", i)
		}
		if file.ModifiedAt == nil || file.ModifiedAt.IsZero() {
			return fmt.Errorf("files[%d].modifiedAt is required for non-deleted entries", i)
		}
		includedFiles++
		totalBytes += file.Size
	}

	if m.Summary.IncludedFiles != includedFiles {
		return fmt.Errorf("summary.includedFiles = %d, want %d", m.Summary.IncludedFiles, includedFiles)
	}
	if m.Summary.DeletedFiles != deletedFiles {
		return fmt.Errorf("summary.deletedFiles = %d, want %d", m.Summary.DeletedFiles, deletedFiles)
	}
	if m.Summary.TotalBytes != totalBytes {
		return fmt.Errorf("summary.totalBytes = %d, want %d", m.Summary.TotalBytes, totalBytes)
	}

	return nil
}

func stringPtrEmpty(value *string) bool {
	return value == nil || strings.TrimSpace(*value) == ""
}
