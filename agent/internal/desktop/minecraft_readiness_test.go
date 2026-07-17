package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestWaitForMinecraftPortEventuallySucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("slow readiness integration test")
	}
	port := 29123 + int(time.Now().UnixNano()%1000)
	appData := t.TempDir()
	serverDir := filepath.Join(appData, "server")
	if err := os.MkdirAll(serverDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(serverDir, "server.properties"),
		[]byte(fmt.Sprintf("server-port=%d\n", port)),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	go func() {
		time.Sleep(35 * time.Second)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return
		}
		defer ln.Close()
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	cfg := agentconfig.Config{
		Server: agentconfig.ServerConfig{
			Dir:     serverDir,
			Command: "java -jar server.jar nogui",
			LogDir:  filepath.Join(appData, "logs"),
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	started := time.Now()
	if err := waitForMinecraftPort(ctx, Options{AppDataDir: appData}, cfg, 60*time.Second); err != nil {
		t.Fatalf("waitForMinecraftPort() error = %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < 31*time.Second {
		t.Fatalf("waitForMinecraftPort() elapsed = %v, want at least 31s delayed success", elapsed)
	}
}

func TestWaitForMinecraftPortDetectsProcessExit(t *testing.T) {
	appData := t.TempDir()
	runtimeDir := filepath.Join(appData, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	state := `{"pid":0,"supervisorPid":4242,"serverDir":"C:/server","command":"java -jar server.jar","startedAt":"2026-01-01T00:00:00Z","stdoutLog":"stdout.log","stderrLog":"stderr.log","status":"stopped","stopTimeout":"30s","controlAddr":"127.0.0.1:0","controlToken":"t"}`
	if err := os.WriteFile(filepath.Join(runtimeDir, "server-state.json"), []byte(state), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	logDir := filepath.Join(appData, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "server-stdout.log"), []byte("Process exited with code 1"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	serverDir := filepath.Join(appData, "server")
	if err := os.MkdirAll(serverDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "server.properties"), []byte("server-port=25565\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := agentconfig.Config{
		Server: agentconfig.ServerConfig{
			Dir:     serverDir,
			Command: "java -jar server.jar nogui",
			LogDir:  logDir,
		},
	}
	err := waitForMinecraftPort(context.Background(), Options{AppDataDir: appData}, cfg, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "进程已退出") || !strings.Contains(err.Error(), "4242") {
		t.Fatalf("waitForMinecraftPort() error = %v, want process exit", err)
	}
}

func TestServerAutoStatusIncludesStartTimeout(t *testing.T) {
	appData := t.TempDir()
	if err := agentconfig.Save(filepath.Join(appData, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "grp",
		MemberID:       "mem",
		HostID:         "host",
		HostToken:      "token",
		DisplayName:    "owner",
		DeviceName:     "pc",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
		Server: agentconfig.ServerConfig{
			StartTimeout: "90s",
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	out, err := ServerAutoStatus(context.Background(), Options{AppDataDir: appData})
	if err != nil {
		t.Fatalf("ServerAutoStatus() error = %v", err)
	}
	got := fmt.Sprint(out["startTimeout"])
	if got != "1m30s" && got != "90s" {
		t.Fatalf("startTimeout = %q, want 90s", got)
	}
}

func TestReadRecentLogSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server-stdout.log")
	long := strings.Repeat("x", 1000)
	if err := os.WriteFile(path, []byte(long), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got := readRecentLogSummary(path, 200)
	if len(got) > 512 {
		t.Fatalf("summary too long: %d", len(got))
	}
}

func TestPartialFailureDetailJSONShape(t *testing.T) {
	res := AutoServerResult{
		OK:        false,
		ErrorCode: "world_backup_failed",
		PartialFailure: &PartialFailureDetail{
			MinecraftStopped:       true,
			WorldSnapshotPublished: false,
			RelayStopped:           true,
			HeartbeatStopped:       false,
			Message:                "upload failed",
		},
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), "partialFailure") {
		t.Fatalf("JSON missing partialFailure: %s", data)
	}
}