//go:build !windows

package artifactsync

func pathHasReparsePoint(string) (bool, error) {
	return false, nil
}
