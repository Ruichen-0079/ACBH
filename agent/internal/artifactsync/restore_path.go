package artifactsync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
)

const fileAttributeReparsePoint uint32 = 0x400

type restorePathEntry struct {
	mode         os.FileMode
	reparsePoint bool
}

func prepareRestoreRoot(root string) error {
	cleanRoot := filepath.Clean(root)
	if !filepath.IsAbs(cleanRoot) {
		return errors.New("output directory must be absolute")
	}
	volume := filepath.VolumeName(cleanRoot)
	remainder := strings.TrimPrefix(cleanRoot, volume)
	remainder = strings.TrimLeft(remainder, `/\`)

	current := volume + string(filepath.Separator)
	if volume == "" {
		current = string(filepath.Separator)
	}
	if err := checkOrCreateRestoreDirectory(current, false); err != nil {
		return fmt.Errorf("unsafe output directory root: %w", err)
	}
	if remainder == "" {
		return nil
	}

	for _, component := range strings.FieldsFunc(remainder, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		current = filepath.Join(current, component)
		if err := checkOrCreateRestoreDirectory(current, true); err != nil {
			return fmt.Errorf("unsafe output directory component %q: %w", component, err)
		}
	}
	return nil
}

func checkOrCreateRestoreDirectory(path string, create bool) error {
	entry, err := inspectRestorePathEntry(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		entry, err = inspectRestorePathEntry(path)
	}
	if err != nil {
		return err
	}
	return validateRestoreDirectory(path, entry)
}

func resolveRestoreTarget(root, manifestPath string) (string, error) {
	normalized, err := validateRestoreManifestPath(manifestPath)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(normalized))
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("check path %q: %w", manifestPath, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes target directory", manifestPath)
	}
	return target, nil
}

func validateRestoreManifestPath(manifestPath string) (string, error) {
	normalized, err := fileclass.NormalizePath(manifestPath)
	if err != nil {
		return "", err
	}
	for _, component := range strings.Split(normalized, "/") {
		if err := validateWindowsPathComponent(component); err != nil {
			return "", fmt.Errorf("path %q is unsafe: %w", manifestPath, err)
		}
	}
	return normalized, nil
}

func validateWindowsPathComponent(component string) error {
	if strings.Contains(component, ":") {
		return errors.New("Windows alternate data streams and drive-qualified paths are not allowed")
	}

	trimmed := strings.TrimRight(component, " .")
	base := trimmed
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("Windows reserved device name %q is not allowed", component)
	}
	return nil
}

func ensureRestoreParentDirectories(root, target string) error {
	_, err := walkRestoreParentDirectories(root, target, true)
	return err
}

func checkExistingRestoreParentDirectories(root, target string) (bool, error) {
	return walkRestoreParentDirectories(root, target, false)
}

func walkRestoreParentDirectories(root, target string, create bool) (bool, error) {
	if err := checkRestoreDirectory(root); err != nil {
		return false, err
	}
	relativeParent, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil {
		return false, fmt.Errorf("resolve restore parent: %w", err)
	}
	if relativeParent == "." {
		return true, nil
	}

	current := root
	for _, component := range strings.Split(relativeParent, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		entry, err := inspectRestorePathEntry(current)
		if errors.Is(err, os.ErrNotExist) {
			if !create {
				return false, nil
			}
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return false, fmt.Errorf("create restore directory %q: %w", component, err)
			}
			entry, err = inspectRestorePathEntry(current)
		}
		if err != nil {
			return false, fmt.Errorf("inspect restore directory %q: %w", component, err)
		}
		if err := validateRestoreDirectory(component, entry); err != nil {
			return false, err
		}
	}
	return true, nil
}

func checkRestoreDirectory(path string) error {
	entry, err := inspectRestorePathEntry(path)
	if err != nil {
		return fmt.Errorf("inspect restore directory: %w", err)
	}
	return validateRestoreDirectory(path, entry)
}

func validateRestoreDirectory(label string, entry restorePathEntry) error {
	if entry.mode&os.ModeSymlink != 0 || entry.reparsePoint {
		return fmt.Errorf("restore directory %q is a symlink or reparse point", label)
	}
	if !entry.mode.IsDir() {
		return fmt.Errorf("restore path component %q is not a directory", label)
	}
	return nil
}

func checkRestoreFinalPath(target string) (bool, error) {
	entry, err := inspectRestorePathEntry(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect restore target: %w", err)
	}
	if entry.mode&os.ModeSymlink != 0 || entry.reparsePoint {
		return false, errors.New("restore target is a symlink or reparse point")
	}
	if entry.mode.IsDir() {
		return false, errors.New("restore target is a directory")
	}
	return true, nil
}

func inspectRestorePathEntry(path string) (restorePathEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return restorePathEntry{}, err
	}
	reparsePoint, err := pathHasReparsePoint(path)
	if err != nil {
		return restorePathEntry{}, err
	}
	return restorePathEntry{mode: info.Mode(), reparsePoint: reparsePoint}, nil
}

func isReparsePointAttributes(attributes uint32) bool {
	return attributes&fileAttributeReparsePoint != 0
}
