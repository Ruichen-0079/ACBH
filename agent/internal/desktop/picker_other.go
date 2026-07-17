//go:build !windows

package desktop

import (
	"errors"
	"os"
	"strings"
)

func PickFolder(title string) (string, error) {
	return "", errors.New("native folder picker is unavailable on this platform; enter the path manually")
}

func PickFile(title, filter string) (string, error) {
	return "", errors.New("native file picker is unavailable on this platform; enter the path manually")
}

func PickFiles(title, filter string) ([]string, error) {
	return PickFilesIn(title, filter, "")
}

func PickFolderIn(title, initialDir string) (string, error) {
	_ = initialDir
	return "", errors.New("native folder picker is unavailable on this platform; enter the path manually")
}

func PickFileIn(title, filter, initialDir string) (string, error) {
	_ = initialDir
	return "", errors.New("native file picker is unavailable on this platform; enter the path manually")
}

func PickFilesIn(title, filter, initialDir string) ([]string, error) {
	_ = initialDir
	return nil, errors.New("native file picker is unavailable on this platform; enter the paths manually")
}

func pickerAvailable() bool { return false }

func validatePickerPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}