//go:build !windows

package worldbackup

func pathHasReparsePoint(string) (bool, error) {
	return false, nil
}
