package agentconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acbh", "config.json")
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

func TestLoadFallsBackToLegacyConfigYaml(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, LegacyFileName)
	want := Config{
		CoordinatorURL: "http://121.40.101.224:6121",
		GroupID:        "grp_123",
		MemberID:       "mem_123",
		HostID:         "host_123",
		HostToken:      "secret",
		DisplayName:    "PlayerA",
		DeviceName:     "PlayerA-PC",
		Platform:       "windows",
		AgentVersion:   AgentVersion,
	}
	if err := Save(legacyPath, want); err != nil {
		t.Fatalf("Save legacy error = %v", err)
	}

	got, err := Load(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("Load fallback error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load fallback = %#v, want %#v", got, want)
	}
}

func TestSaveConfigIsAtomicTempCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	cfg := Config{
		CoordinatorURL: "http://121.40.101.224:6121",
		GroupID:        "grp_123",
		MemberID:       "mem_123",
		HostID:         "host_123",
		HostToken:      "secret",
		DisplayName:    "PlayerA",
		DeviceName:     "PlayerA-PC",
		Platform:       "windows",
		AgentVersion:   AgentVersion,
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, FileName+".*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("Save left temp files: %v", matches)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.HostToken != cfg.HostToken {
		t.Fatalf("HostToken changed: got %q want %q", got.HostToken, cfg.HostToken)
	}
}
