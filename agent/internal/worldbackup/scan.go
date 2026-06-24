package worldbackup

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var defaultIgnoredBasenames = map[string]struct{}{
	"session.lock": {},
	".DS_Store":    {},
}

func BuildSnapshot(opts ScanOptions) (Snapshot, error) {
	if err := validateScanOptions(opts); err != nil {
		return Snapshot{}, err
	}
	serverDir, err := filepath.Abs(opts.ServerDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve server directory: %w", err)
	}
	serverDir, err = filepath.EvalSymlinks(serverDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve server symlinks: %w", err)
	}
	info, err := os.Stat(serverDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stat server directory: %w", err)
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("server directory %q is not a directory", serverDir)
	}

	roots, err := ResolveWorldRoots(serverDir, opts.WorldRoots)
	if err != nil {
		return Snapshot{}, err
	}
	ignore, err := LoadIgnoreFile(serverDir)
	if err != nil {
		return Snapshot{}, err
	}
	index, _, err := LoadIndex(opts.AppDataDir)
	if err != nil {
		return Snapshot{}, err
	}

	current := map[string]IndexedFile{}
	localPaths := map[string]string{}
	var files []FileEntry
	var logicalSize int64

	for _, root := range roots {
		rootAbs := filepath.Join(serverDir, filepath.FromSlash(root))
		if _, err := os.Stat(rootAbs); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return Snapshot{}, fmt.Errorf("stat world root %s: %w", root, err)
		}
		if err := filepath.WalkDir(rootAbs, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if filePath == rootAbs {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				rel, _ := filepath.Rel(serverDir, filePath)
				normalized, _ := NormalizeManifestPath(rel)
				return fmt.Errorf("symlink is not allowed in world backup: %s", normalized)
			}
			rel, err := filepath.Rel(serverDir, filePath)
			if err != nil {
				return fmt.Errorf("find relative path for %q: %w", filePath, err)
			}
			normalized, err := NormalizeManifestPath(rel)
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if shouldIgnorePath(normalized, true, ignore) {
					return filepath.SkipDir
				}
				return nil
			}
			if shouldIgnorePath(normalized, false, ignore) {
				return nil
			}
			fileInfo, err := entry.Info()
			if err != nil {
				return fmt.Errorf("stat %s: %w", normalized, err)
			}
			real, err := filepath.EvalSymlinks(filePath)
			if err != nil {
				return fmt.Errorf("resolve %s symlinks: %w", normalized, err)
			}
			if !underRoot(serverDir, real) {
				return fmt.Errorf("path %s escapes server directory through symlink", normalized)
			}

			mt := fileInfo.ModTime().UnixNano()
			size := fileInfo.Size()
			indexed, ok := index.Files[normalized]
			if !ok || indexed.Size != size || indexed.MTimeUnixNano != mt || indexed.SHA256 == "" {
				sum, err := hashFile(filePath)
				if err != nil {
					return err
				}
				indexed = IndexedFile{
					Size:          size,
					MTimeUnixNano: mt,
					SHA256:        sum,
					ObjectID:      ObjectID(sum),
				}
			}
			if indexed.ObjectID == "" {
				indexed.ObjectID = ObjectID(indexed.SHA256)
			}
			current[normalized] = indexed
			localPaths[normalized] = filePath
			files = append(files, FileEntry{
				Path:     normalized,
				Size:     indexed.Size,
				SHA256:   indexed.SHA256,
				ObjectID: indexed.ObjectID,
			})
			logicalSize += indexed.Size
			return nil
		}); err != nil {
			return Snapshot{}, err
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	deletedPaths := deletedFromParent(opts.Parent, files)
	changedFiles, plannedObjects := changedFromParent(opts.Parent, files, localPaths)

	now := time.Now().UTC()
	if opts.Clock != nil {
		now = opts.Clock().UTC()
	}
	parentID := ""
	if opts.Parent != nil {
		parentID = opts.Parent.SnapshotID
	} else if index.LatestSnapshotID != "" {
		parentID = index.LatestSnapshotID
	}
	uploadedSize := uniqueObjectSize(changedFiles)
	manifest := Manifest{
		SchemaVersion:    SchemaVersion,
		SnapshotID:       opts.SnapshotID,
		GroupID:          opts.GroupID,
		SourceHostID:     opts.SourceHostID,
		HostGeneration:   opts.HostGeneration,
		ParentSnapshotID: parentID,
		CreatedAt:        now,
		Consistent:       opts.Consistent,
		LogicalSize:      logicalSize,
		UploadedSize:     uploadedSize,
		FileCount:        len(files),
		ChangedFileCount: len(changedFiles),
		DeletedFileCount: len(deletedPaths),
		Files:            files,
		DeletedPaths:     deletedPaths,
	}
	if err := ValidateManifest(manifest); err != nil {
		return Snapshot{}, err
	}
	plan := Plan{
		SnapshotID:       manifest.SnapshotID,
		ParentSnapshotID: manifest.ParentSnapshotID,
		LogicalSize:      manifest.LogicalSize,
		FileCount:        manifest.FileCount,
		ChangedFileCount: manifest.ChangedFileCount,
		DeletedFileCount: manifest.DeletedFileCount,
		ChangedFiles:     changedFiles,
		DeletedPaths:     deletedPaths,
		Objects:          plannedObjects,
	}
	return Snapshot{
		Manifest: manifest,
		Plan:     plan,
		Index:    IndexFromManifest(manifest, current),
	}, nil
}

func ResolveWorldRoots(serverDir string, requested []string) ([]string, error) {
	mainWorld, err := readLevelName(serverDir)
	if err != nil {
		return nil, err
	}
	if mainWorld == "" {
		mainWorld = "world"
	}
	candidates := append([]string{mainWorld}, requested...)
	seen := map[string]struct{}{}
	var roots []string
	for _, raw := range candidates {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		normalized, err := NormalizeManifestPath(raw)
		if err != nil {
			return nil, fmt.Errorf("world root %q is invalid: %w", raw, err)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		abs := filepath.Join(serverDir, filepath.FromSlash(normalized))
		if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("world root %s is a symlink and is not allowed", normalized)
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil && !underRoot(serverDir, real) {
			return nil, fmt.Errorf("world root %s escapes server directory", normalized)
		}
		seen[normalized] = struct{}{}
		roots = append(roots, normalized)
	}
	sort.Strings(roots)
	return roots, nil
}

type IgnoreRules struct {
	Patterns []string
}

func LoadIgnoreFile(serverDir string) (IgnoreRules, error) {
	file, err := os.Open(filepath.Join(serverDir, ".acbh-worldignore"))
	if os.IsNotExist(err) {
		return IgnoreRules{}, nil
	}
	if err != nil {
		return IgnoreRules{}, fmt.Errorf("open .acbh-worldignore: %w", err)
	}
	defer file.Close()
	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		line = strings.ReplaceAll(line, "\\", "/")
		patterns = append(patterns, strings.TrimPrefix(line, "/"))
	}
	if err := scanner.Err(); err != nil {
		return IgnoreRules{}, fmt.Errorf("read .acbh-worldignore: %w", err)
	}
	return IgnoreRules{Patterns: patterns}, nil
}

func ValidateScanRoots(serverDir string, roots []string) error {
	_, err := ResolveWorldRoots(serverDir, roots)
	return err
}

func validateScanOptions(opts ScanOptions) error {
	switch {
	case opts.ServerDir == "":
		return errors.New("server directory is required")
	case opts.AppDataDir == "":
		return errors.New("app data directory is required")
	case opts.SnapshotID == "":
		return errors.New("snapshot ID is required")
	case opts.GroupID == "":
		return errors.New("group ID is required")
	case opts.SourceHostID == "":
		return errors.New("source host ID is required")
	case opts.HostGeneration < 0:
		return errors.New("host generation must not be negative")
	default:
		return nil
	}
}

func deletedFromParent(parent *Manifest, current []FileEntry) []string {
	if parent == nil {
		return nil
	}
	currentPaths := map[string]struct{}{}
	for _, file := range current {
		currentPaths[file.Path] = struct{}{}
	}
	var deleted []string
	for _, file := range parent.Files {
		if _, ok := currentPaths[file.Path]; !ok {
			deleted = append(deleted, file.Path)
		}
	}
	sort.Strings(deleted)
	return deleted
}

func changedFromParent(parent *Manifest, current []FileEntry, localPaths map[string]string) ([]ChangedFile, []PlannedObject) {
	parentFiles := map[string]FileEntry{}
	if parent != nil {
		for _, file := range parent.Files {
			parentFiles[file.Path] = file
		}
	}
	seenObjects := map[string]PlannedObject{}
	var changed []ChangedFile
	for _, file := range current {
		old, ok := parentFiles[file.Path]
		if ok && old.Size == file.Size && old.SHA256 == file.SHA256 && old.ObjectID == file.ObjectID {
			continue
		}
		change := ChangedFile{
			Path:      file.Path,
			Size:      file.Size,
			SHA256:    file.SHA256,
			ObjectID:  file.ObjectID,
			LocalPath: localPaths[file.Path],
		}
		changed = append(changed, change)
		if _, ok := seenObjects[file.SHA256]; !ok {
			seenObjects[file.SHA256] = PlannedObject{SHA256: file.SHA256, Size: file.Size, Path: file.Path}
		}
	}
	objects := make([]PlannedObject, 0, len(seenObjects))
	for _, object := range seenObjects {
		objects = append(objects, object)
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].SHA256 < objects[j].SHA256 })
	return changed, objects
}

func uniqueObjectSize(changed []ChangedFile) int64 {
	seen := map[string]int64{}
	for _, file := range changed {
		if _, ok := seen[file.SHA256]; !ok {
			seen[file.SHA256] = file.Size
		}
	}
	var total int64
	for _, size := range seen {
		total += size
	}
	return total
}

func shouldIgnorePath(rel string, isDir bool, rules IgnoreRules) bool {
	base := path.Base(rel)
	if _, ok := defaultIgnoredBasenames[base]; ok {
		return true
	}
	lowerBase := strings.ToLower(base)
	lowerRel := strings.ToLower(rel)
	if strings.HasSuffix(lowerBase, ".tmp") ||
		strings.HasSuffix(lowerBase, ".log") ||
		strings.HasPrefix(lowerBase, ".acbh-") ||
		strings.Contains(lowerRel, "/.acbh-") ||
		strings.Contains(lowerRel, "/logs/") ||
		strings.Contains(lowerRel, "/crash-reports/") {
		return true
	}
	for _, pattern := range rules.Patterns {
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "/") {
			prefix := strings.TrimSuffix(pattern, "/")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
			continue
		}
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
		if ok, _ := path.Match(pattern, base); ok {
			return true
		}
		if isDir && (rel == pattern || strings.HasPrefix(rel, pattern+"/")) {
			return true
		}
	}
	return false
}

func readLevelName(serverDir string) (string, error) {
	file, err := os.Open(filepath.Join(serverDir, "server.properties"))
	if os.IsNotExist(err) {
		return "world", nil
	}
	if err != nil {
		return "", fmt.Errorf("open server.properties: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "level-name" {
			value = strings.TrimSpace(value)
			if value == "" {
				return "world", nil
			}
			return value, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read server.properties: %w", err)
	}
	return "world", nil
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

func hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", filePath, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %q: %w", filePath, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
