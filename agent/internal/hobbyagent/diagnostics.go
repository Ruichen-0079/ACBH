package hobbyagent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func (r *Runtime) buildDiagnostics(ctx context.Context) map[string]any {
	config, _ := r.Config()
	status := r.Status()
	diagnosis := r.relay.Diagnose(ctx)
	r.mu.RLock()
	var coordinatorInfo any
	if r.coordinatorInfo != nil {
		copy := *r.coordinatorInfo
		coordinatorInfo = copy
	}
	r.mu.RUnlock()
	r.frpcVersionOnce.Do(func() {
		r.frpcVersion = executableVersion(context.Background(), r.frpcPath, "-v")
	})
	frpcVersion := map[string]string{
		"version": r.frpcVersion["version"],
		"error":   r.frpcVersion["error"],
	}
	return map[string]any{
		"agent_version":    r.version,
		"operating_system": map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH},
		"config":           config,
		"agent":            status.Agent,
		"local_probe":      status.LocalServer,
		"local_target":     status.LocalEndpoint,
		"public_endpoint":  status.PublicEndpoint,
		"coordinator":      map[string]any{"connectivity": status.Coordinator, "protocol": coordinatorInfo},
		"frpc_version":     frpcVersion,
		"relay": map[string]any{
			"status":        diagnosis.Status,
			"desired":       diagnosis.Desired,
			"recent_events": diagnosis.RecentEvents,
		},
		"recent_errors":            r.Logs(100),
		"recent_state_transitions": r.Events(200),
	}
}

func executableVersion(parent context.Context, path, argument string) map[string]string {
	if strings.TrimSpace(path) == "" {
		return map[string]string{"version": "", "error": "not configured"}
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, argument).CombinedOutput()
	version := strings.TrimSpace(string(output))
	if len(version) > 2048 {
		version = version[:2048]
	}
	return map[string]string{"version": version, "error": errorText(err)}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
