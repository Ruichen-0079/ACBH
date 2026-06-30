package worldbackup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fileAttributeReparsePoint uint32 = 0x400

type restorePathEntry struct {
	mode         os.FileMode
	reparsePoint bool
}

func prepareRestoreDirectory(root string) error {
	cleanRoot := filepath.Clean(root)
	if !filepath.IsAbs(cleanRoot) {
		return errors.New("restore directory must be absolute")
	}
	if err := os.MkdirAll(cleanRoot, 0o755); err != nil {
		return fmt.Errorf("create restore directory: %w", err)
	}
	return checkRestoreDirectory(cleanRoot)
}

func ensureRestoreParent(root, target string) error {
	if err := checkRestoreDirectory(root); err != nil {
		return err
	}
	relativeParent, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("resolve restore parent: %w", err)
	}
	if relativeParent == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(relativeParent, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		entry, err := inspectRestorePathEntry(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create restore directory %q: %w", component, err)
			}
			entry, err = inspectRestorePathEntry(current)
		}
		if err != nil {
			return fmt.Errorf("inspect restore directory %q: %w", component, err)
		}
		if err := validateRestoreDirectory(component, entry); err != nil {
			return err
		}
	}
	return nil
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

func checkRestoreFinalTarget(target string) (bool, error) {
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
