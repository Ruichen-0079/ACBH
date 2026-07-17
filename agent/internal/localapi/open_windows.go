//go:build windows

package localapi

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/svc"
)

func openDirectory(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, errors.New("log directory is not configured")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return false, err
	}
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, err
	}
	if isService {
		// Session-0 services cannot launch Explorer in the signed-in user's
		// desktop. The installed URI handler delegates that action to the
		// launcher running in the interactive session.
		return false, nil
	}
	explorer := filepath.Join(os.Getenv("WINDIR"), "explorer.exe")
	if !filepath.IsAbs(explorer) {
		return false, errors.New("Windows Explorer path is unavailable")
	}
	return true, exec.Command(explorer, path).Start()
}
