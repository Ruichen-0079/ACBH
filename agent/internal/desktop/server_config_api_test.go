package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestSaveServerConfigRoundTripWithUnicodeAndSpaces(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	serverDir := filepath.Join(opts.AppDataDir, "中文 Server Dir")
	if err := os.MkdirAll(serverDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	jarPath := filepath.Join(serverDir, "paper 服务端.jar")
	if err := os.WriteFile(jarPath, []byte("jar"), 0o600); err != nil {
		t.Fatalf("write jar: %v", err)
	}
	_ = agentconfig.Save(filepath.Join(opts.AppDataDir, agentconfig.FileName), agentconfig.Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "grp_test",
		MemberID:       "mem_test",
		HostID:         "host_test",
		HostToken:      "token",
		DisplayName:    "Tester",
		DeviceName:     "PC",
		Platform:       "windows",
		AgentVersion:   agentconfig.AgentVersion,
	})

	payload := ServerConfigPayload{
		ServerDir: serverDir, LaunchType: "jar", LaunchPath: jarPath,
		WorkingDir: serverDir, StartTimeoutSeconds: 120,
	}
	result, err := SaveServerConfigValidated(opts, payload, "tr_test")
	if err != nil {
		t.Fatalf("SaveServerConfigValidated() error = %v", err)
	}
	if !result.OK || result.Outcome != OutcomeSuccess {
		t.Fatalf("result = %#v, want success", result)
	}
	if result.Saved.ServerDir != serverDir || !strings.Contains(result.Saved.LaunchPath, "paper") {
		t.Fatalf("saved = %#v", result.Saved)
	}
	loaded, err := LoadServerConfigPayload(opts)
	if err != nil {
		t.Fatalf("LoadServerConfigPayload() error = %v", err)
	}
	if loaded.ServerDir != serverDir {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestSaveServerConfigRejectsMissingDir(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	result, err := SaveServerConfigValidated(opts, ServerConfigPayload{ServerDir: filepath.Join(opts.AppDataDir, "missing")}, "tr_test")
	if err != nil {
		t.Fatalf("SaveServerConfigValidated() error = %v", err)
	}
	if result.OK || result.Field != "serverDir" || result.ErrorCode != "validation_failed" {
		t.Fatalf("result = %#v, want field validation failure", result)
	}
}

