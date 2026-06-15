package agentconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acbh", "config.yaml")
	want := Config{
		CoordinatorURL: "http://127.0.0.1:6121",
		GroupID:        "grp_123",
		MemberID:       "mem_123",
		HostID:         "host_123",
		HostToken:      "secret",
		DisplayName:    "PlayerA",
		DeviceName:     "PlayerA-PC",
		Platform:       "windows",
		AgentVersion:   AgentVersion,
		ArtifactClass:  "server-runtime",
		LastPushedID:   "runtime_001",
		ExcludeRules:   []string{"logs/", "*.tmp"},
		RCON: RCONConfig{
			Host:        "127.0.0.1",
			Port:        25575,
			PasswordEnv: "ACBH_RCON_PASSWORD",
		},
		Server: ServerConfig{
			Dir:         "C:/minecraft/server",
			Command:     "java -Xmx4G -jar server.jar nogui",
			LogDir:      ".acbh/logs",
			StopTimeout: "30s",
		},
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !Exists(path) {
		t.Fatalf("Exists(%q) = false", path)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.IsDir() {
		t.Fatal("config path is a directory")
	}
}

func TestDefaultPathUsesUserConfigDir(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}

	wantSuffix := filepath.Join(DirName, FileName)
	if !strings.HasSuffix(filepath.Clean(path), wantSuffix) {
		t.Fatalf("DefaultPath() = %q, want suffix %q", path, wantSuffix)
	}
}
