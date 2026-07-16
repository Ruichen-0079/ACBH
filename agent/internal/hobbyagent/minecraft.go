package hobbyagent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/componentstate"
	"github.com/Ruichen-0079/ACBH/agent/internal/mcserver"
)

type ManagedMinecraft struct {
	Executable string
	RuntimeDir string
	LogDir     string
	Port       int
	Timeout    time.Duration
}

func (m ManagedMinecraft) Start(ctx context.Context, imported ImportedServer) error {
	_, err := mcserver.Start(ctx, m.Executable, mcserver.StartOptions{
		ServerDir: imported.ServerDir,
		Command:   fmt.Sprintf("%q -jar %q nogui", imported.JavaPath, filepath.Base(imported.JarPath)),
		LogDir:    m.LogDir, RuntimeDir: m.RuntimeDir, StopTimeout: m.stopTimeout(),
	})
	return err
}

func (m ManagedMinecraft) Stop(_ context.Context) error {
	_, _, err := mcserver.Stop(m.RuntimeDir)
	return err
}

func (m ManagedMinecraft) Status(ctx context.Context) MinecraftStatus {
	now := time.Now().UTC()
	status, err := mcserver.GetStatus(m.RuntimeDir)
	if err != nil {
		return MinecraftStatus{Snapshot: errorSnapshot(now, "status_failed", err)}
	}
	if status.Stale {
		return MinecraftStatus{
			Snapshot: errorSnapshot(now, "process_state_stale", errors.New("server supervisor cannot be verified")),
			PID:      status.State.PID,
		}
	}
	if !status.Running {
		return MinecraftStatus{Snapshot: componentstate.NewSnapshot(componentstate.Stopped, now, "not_running", "Minecraft 未启动")}
	}
	port := m.Port
	if port == 0 {
		port = 25565
	}
	if err := probeAddress(ctx, fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond); err != nil {
		snapshot := componentstate.NewSnapshot(componentstate.Starting, now, "local_port_wait", "正在等待 Minecraft 本地端口")
		snapshot.TechnicalMessage = err.Error()
		return MinecraftStatus{Snapshot: snapshot, PID: status.State.PID}
	}
	snapshot := componentstate.NewSnapshot(componentstate.Ready, now, "local_port_ready", "Minecraft 正常")
	snapshot.LastOKAt = timePointer(now)
	return MinecraftStatus{Snapshot: snapshot, PID: status.State.PID}
}

func (m ManagedMinecraft) Diagnose(ctx context.Context) any {
	return map[string]any{
		"runtime_dir": m.RuntimeDir,
		"log_dir":     m.LogDir,
		"status":      m.Status(ctx),
	}
}

func (m ManagedMinecraft) stopTimeout() time.Duration {
	if m.Timeout <= 0 {
		return 30 * time.Second
	}
	return m.Timeout
}

func errorSnapshot(now time.Time, reason string, err error) componentstate.Snapshot {
	snapshot := componentstate.NewSnapshot(componentstate.Error, now, reason, "Minecraft 状态异常")
	snapshot.TechnicalMessage = err.Error()
	return snapshot
}

func timePointer(value time.Time) *time.Time { return &value }
