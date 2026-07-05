//go:build !windows

package desktop

func profilePathHasReparsePoint(string) (bool, error) {
	return false, nil
}