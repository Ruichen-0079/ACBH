package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopConfigRoundTripAndSecretsExcluded(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	cfg := defaultDesktopConfig()
	cfg.CoordinatorURL = "http://121.40.101.224:6121"
	cfg.PublicEntry = "121.40.101.224:25565"
	cfg.LastServerDir = `C:\Servers\test`
	cfg.LaunchProfile = DesktopLaunchProfile{Kind: "script", Path: "run.ps1", ScriptType: "powershell"}
	cfg.JavaPath = `C:\Java\bin\java.exe`
	cfg.Group = DesktopGroupConfig{GroupID: "grp_1", MemberID: "mem_1", HostID: "host_1"}
	cfg.UI.LastCompletedStep = 3

	if err := SaveDesktopConfig(opts, cfg); err != nil {
		t.Fatalf("SaveDesktopConfig() error = %v", err)
	}
	loaded, err := LoadDesktopConfig(opts)
	if err != nil {
		t.Fatalf("LoadDesktopConfig() error = %v", err)
	}
	if loaded.CoordinatorURL != cfg.CoordinatorURL || loaded.LaunchProfile.Path != "run.ps1" || loaded.LaunchProfile.ScriptType != "powershell" || loaded.UI.LastCompletedStep != 3 {
		t.Fatalf("loaded config = %+v", loaded)
	}
	raw, err := os.ReadFile(desktopConfigPath(opts))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	for _, forbidden := range []string{"accessKey", "hostToken", "inviteCode", "rcon.password", "takeoverToken"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("desktop-config.json contains forbidden secret marker %q: %s", forbidden, string(raw))
		}
	}
}

func TestDesktopConfigPortablePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "portable.flag"), []byte(""), 0o600); err != nil {
		t.Fatalf("write portable.flag: %v", err)
	}
	opts := Options{AppDataDir: filepath.Join(root, "data")}
	if got := desktopConfigPath(opts); got != filepath.Join(root, "data", "config", desktopConfigFileName) {
		t.Fatalf("desktopConfigPath() = %q", got)
	}
}

func TestDesktopConfigMigrationAndCorruptRecovery(t *testing.T) {
	appData := t.TempDir()
	opts := Options{AppDataDir: appData}
	if err := SaveSetup(opts, SetupState{
		Mode: "remote-public", CoordinatorURL: "http://host:6121", PlayerAddress: "host:25565",
		ServerDir: `C:\Servers\test`, JavaPath: `C:\Java\bin\java.exe`,
	}); err != nil {
		t.Fatalf("SaveSetup() error = %v", err)
	}
	migrated, err := LoadDesktopConfig(opts)
	if err != nil {
		t.Fatalf("LoadDesktopConfig() migration error = %v", err)
	}
	if migrated.CoordinatorURL != "http://host:6121" || migrated.LastServerDir == "" {
		t.Fatalf("migrated = %+v", migrated)
	}

	if err := os.WriteFile(desktopConfigPath(opts), []byte("{bad json"), 0o600); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}
	recovered, err := LoadDesktopConfig(opts)
	if err != nil {
		t.Fatalf("LoadDesktopConfig() corrupt error = %v", err)
	}
	if recovered.SchemaVersion != 1 {
		t.Fatalf("recovered schema = %d", recovered.SchemaVersion)
	}
	if _, err := os.Stat(desktopConfigPath(opts) + ".corrupt"); err != nil {
		t.Fatalf("corrupt file was not preserved: %v", err)
	}
}

func TestNormalizePublicCoordinatorInput(t *testing.T) {
	cases := []struct {
		in       string
		wantURL  string
		wantHost string
	}{
		{"121.40.101.224", "http://121.40.101.224:6121", "121.40.101.224"},
		{"domain.example.com", "http://domain.example.com:6121", "domain.example.com"},
		{"http://121.40.101.224:6121", "http://121.40.101.224:6121", "121.40.101.224"},
		{"https://domain.example.com", "https://domain.example.com", "domain.example.com"},
	}
	for _, tc := range cases {
		gotURL, gotHost, err := NormalizePublicCoordinatorInput(tc.in, "6121")
		if err != nil {
			t.Fatalf("NormalizePublicCoordinatorInput(%q) error = %v", tc.in, err)
		}
		if gotURL != tc.wantURL || gotHost != tc.wantHost {
			t.Fatalf("NormalizePublicCoordinatorInput(%q) = %q, %q", tc.in, gotURL, gotHost)
		}
	}
}
