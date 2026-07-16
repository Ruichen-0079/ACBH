package hobbyagent

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func (r *Runtime) buildDiagnostics(ctx context.Context) map[string]any {
	config, _ := r.Config()
	imported, importErr := r.store.LoadImportedServer()
	eula := false
	if importErr == nil {
		eula, _ = eulaAccepted(imported.ServerDir + string(os.PathSeparator) + "eula.txt")
	}
	r.mu.RLock()
	var coordinatorInfo any
	if r.coordinatorInfo != nil {
		copy := *r.coordinatorInfo
		coordinatorInfo = copy
	}
	r.mu.RUnlock()
	diskPath := imported.ServerDir
	if diskPath == "" {
		diskPath = r.runtimeDir
	}
	diskBytes, diskErr := diskFreeBytes(diskPath)
	status := r.Status()
	return map[string]any{
		"agent_version":            r.version,
		"operating_system":         map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH},
		"config":                   config,
		"coordinator":              map[string]any{"connectivity": status.Coordinator, "protocol": coordinatorInfo},
		"java":                     executableVersion(ctx, imported.JavaPath, "-version"),
		"server_dir":               imported.ServerDir,
		"server_jar":               imported.JarPath,
		"eula_accepted":            eula,
		"minecraft":                r.minecraft.Diagnose(ctx),
		"local_25565_probe":        status.Minecraft.State,
		"frpc":                     executableVersion(ctx, r.frpcPath, "-v"),
		"relay":                    r.relay.Diagnose(ctx),
		"status":                   status,
		"disk":                     map[string]any{"path": diskPath, "free_bytes": diskBytes, "error": errorText(diskErr)},
		"recent_errors":            r.Logs(100),
		"recent_state_transitions": r.Events(500),
	}
}

func executableVersion(parent context.Context, path string, argument string) map[string]string {
	if strings.TrimSpace(path) == "" {
		return map[string]string{"path": "", "version": "", "error": "not configured"}
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, argument).CombinedOutput()
	version := strings.TrimSpace(string(output))
	if len(version) > 2048 {
		version = version[:2048]
	}
	return map[string]string{"path": path, "version": version, "error": errorText(err)}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
