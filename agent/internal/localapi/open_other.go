//go:build !windows

package localapi

import "errors"

func openDirectory(string) (bool, error) {
	return false, errors.New("opening the log directory is only supported on Windows")
}
