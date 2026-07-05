//go:build !windows

package backup

func hasReparsePoint(path string) bool {
	return false
}
