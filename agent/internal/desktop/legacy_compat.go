package desktop

import "fmt"

func CurrentBackupProfile(opts Options) (map[string]any, error) {
	return nil, minimalCoreUnsupported("backup profile")
}

func UseBackupPreset(opts Options, presetID string, includeFiles []string, includeDirs []string) (map[string]any, error) {
	return nil, minimalCoreUnsupported("backup preset selection")
}

func SelectServerJava(opts Options, selectedPath string) (map[string]any, error) {
	return nil, minimalCoreUnsupported("server Java selection")
}

func RepairServerState(opts Options) (map[string]any, error) {
	return nil, minimalCoreUnsupported("server lock repair")
}

func minimalCoreUnsupported(feature string) error {
	return fmt.Errorf("%s is disabled in minimal-core; use the body API workflow instead", feature)
}
