package fileclass

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type FileClass string

const (
	WorldRuntime      FileClass = "world-runtime"
	ServerPack        FileClass = "server-pack"
	AdminState        FileClass = "admin-state"
	PluginRuntimeData FileClass = "plugin-runtime-data"
	Ignored           FileClass = "ignored"
	Unknown           FileClass = "unknown"
)

var adminStateFiles = map[string]struct{}{
	"server.properties":   {},
	"whitelist.json":      {},
	"ops.json":            {},
	"banned-players.json": {},
	"banned-ips.json":     {},
	"usercache.json":      {},
}

var windowsDrivePattern = regexp.MustCompile(`^[A-Za-z]:[/\\]`)

func NormalizePath(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("path is empty")
	}
	if strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("path %q contains a null byte", raw)
	}
	if filepath.IsAbs(raw) || windowsDrivePattern.MatchString(raw) {
		return "", fmt.Errorf("path %q must be relative", raw)
	}

	normalized := strings.ReplaceAll(raw, "\\", "/")
	if path.IsAbs(normalized) {
		return "", fmt.Errorf("path %q must be relative", raw)
	}

	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("path %q is not a file path", raw)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path %q must not traverse outside the server directory", raw)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return "", fmt.Errorf("path %q must not contain traversal segments", raw)
		}
	}

	return cleaned, nil
}

func ClassifyPath(raw string) (FileClass, error) {
	normalized, err := NormalizePath(raw)
	if err != nil {
		return Unknown, err
	}

	return ClassifyNormalizedPath(normalized), nil
}

func ClassifyNormalizedPath(normalized string) FileClass {
	base := path.Base(normalized)
	if hasDirPrefix(normalized, "logs") ||
		hasDirPrefix(normalized, "crash-reports") ||
		hasDirPrefix(normalized, "cache") ||
		hasDirPrefix(normalized, "backups") ||
		hasDirPrefix(normalized, ".fabric/processedMods") ||
		hasDirPrefix(normalized, ".fabric/remappedJars") ||
		base == "session.lock" ||
		strings.HasSuffix(strings.ToLower(base), ".log") {
		return Ignored
	}

	if hasDirPrefix(normalized, "world") ||
		hasDirPrefix(normalized, "world_nether") ||
		hasDirPrefix(normalized, "world_the_end") {
		return WorldRuntime
	}

	if hasDirPrefix(normalized, "plugins/LuckPerms") ||
		hasPluginRuntimePrefix(normalized, "userdata") ||
		hasPluginRuntimePrefix(normalized, "data") {
		return PluginRuntimeData
	}

	if isModsJar(normalized) ||
		isTopLevelPluginJar(normalized) ||
		isTopLevelJar(normalized) ||
		normalized == "eula.txt" ||
		hasDirPrefix(normalized, "config") ||
		hasDirPrefix(normalized, "defaultconfigs") ||
		hasDirPrefix(normalized, "kubejs") ||
		hasDirPrefix(normalized, "scripts") ||
		hasDirPrefix(normalized, "libraries") ||
		hasDirPrefix(normalized, ".fabric/server") {
		return ServerPack
	}

	if _, ok := adminStateFiles[normalized]; ok {
		return AdminState
	}

	return Unknown
}

func IsKnownClass(class FileClass) bool {
	switch class {
	case WorldRuntime, ServerPack, AdminState, PluginRuntimeData, Ignored, Unknown:
		return true
	default:
		return false
	}
}

func hasDirPrefix(filePath, dir string) bool {
	return filePath == dir || strings.HasPrefix(filePath, dir+"/")
}

func isModsJar(filePath string) bool {
	return strings.HasPrefix(filePath, "mods/") && strings.HasSuffix(strings.ToLower(filePath), ".jar")
}

func isTopLevelPluginJar(filePath string) bool {
	rest, ok := strings.CutPrefix(filePath, "plugins/")
	return ok && !strings.Contains(rest, "/") && strings.HasSuffix(strings.ToLower(rest), ".jar")
}

func isTopLevelJar(filePath string) bool {
	return !strings.Contains(filePath, "/") && strings.HasSuffix(strings.ToLower(filePath), ".jar")
}

func hasPluginRuntimePrefix(filePath, runtimeDir string) bool {
	parts := strings.Split(filePath, "/")
	return len(parts) >= 4 && parts[0] == "plugins" && parts[2] == runtimeDir
}
