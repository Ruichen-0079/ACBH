package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/agentconfig"
)

func TestEnsureBackupProfileForServerDirCreatesDefaultsUnderServerRoot(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	serverDir := filepath.Join(opts.AppDataDir, "中文 MC Server")
	mustMkdir(t, serverDir)
	mustMkdir(t, filepath.Join(serverDir, "config"))
	mustMkdir(t, filepath.Join(serverDir, "mods"))
	mustWrite(t, filepath.Join(serverDir, "server.properties"), "level-name=测试世界\n")
	mustWrite(t, filepath.Join(serverDir, "eula.txt"), "eula=true\n")

	result, err := EnsureBackupProfileForServerDir(opts, serverDir)
	if err != nil {
		t.Fatalf("EnsureBackupProfileForServerDir() error = %v", err)
	}
	if !result.Created {
		t.Fatalf("result = %#v, want created=true", result)
	}
	if result.Profile.ServerDir != serverDir {
		t.Fatalf("profile serverDir = %q, want %q", result.Profile.ServerDir, serverDir)
	}
	worldPath := filepath.Join(serverDir, "测试世界")
	if result.WorldPath != worldPath {
		t.Fatalf("worldPath = %q, want %q", result.WorldPath, worldPath)
	}
	if !result.WorldPending {
		t.Fatal("world should be pending when directory does not exist yet")
	}
	for _, rootID := range []string{"world", "config", "mods", "server"} {
		root, ok := findProfileRoot(result.Profile, rootID)
		if !ok {
			t.Fatalf("missing root %q", rootID)
		}
		if !strings.HasPrefix(root.SourcePath, serverDir) || !strings.HasPrefix(root.RestorePath, serverDir) {
			t.Fatalf("root %q paths must stay under serverDir: source=%q restore=%q", rootID, root.SourcePath, root.RestorePath)
		}
		if root.RestorePath != root.SourcePath {
			t.Fatalf("root %q restorePath = %q, want %q", rootID, root.RestorePath, root.SourcePath)
		}
	}
	for _, root := range result.Profile.Roots {
		if strings.EqualFold(filepath.Clean(root.RestorePath), filepath.Clean(opts.AppDataDir)) {
			t.Fatalf("restore path must not default to AppData root: %q", root.RestorePath)
		}
	}
}

func TestEnsureBackupProfileForServerDirMigratesWhenServerDirChanges(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	oldDir := filepath.Join(opts.AppDataDir, "old-server")
	newDir := filepath.Join(opts.AppDataDir, "new-server")
	mustMkdir(t, oldDir)
	mustMkdir(t, newDir)
	mustWrite(t, filepath.Join(oldDir, "server.properties"), "level-name=world\n")
	mustWrite(t, filepath.Join(newDir, "server.properties"), "level-name=world\n")

	first, err := EnsureBackupProfileForServerDir(opts, oldDir)
	if err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	if !first.Created {
		t.Fatalf("first = %#v, want created", first)
	}

	second, err := EnsureBackupProfileForServerDir(opts, newDir)
	if err != nil {
		t.Fatalf("migrate ensure: %v", err)
	}
	if !second.Migrated {
		t.Fatalf("second = %#v, want migrated", second)
	}
	if second.PreviousDir != oldDir {
		t.Fatalf("previousDir = %q, want %q", second.PreviousDir, oldDir)
	}
	if second.Profile.ServerDir != newDir {
		t.Fatalf("profile serverDir = %q, want %q", second.Profile.ServerDir, newDir)
	}
}

func TestSaveServerConfigAutoCreatesBackupProfile(t *testing.T) {
	opts := Options{AppDataDir: t.TempDir()}
	serverDir := filepath.Join(opts.AppDataDir, "Server Root")
	mustMkdir(t, serverDir)
	jar := filepath.Join(serverDir, "双击直接开服！！！.bat")
	mustWrite(t, jar, "@echo off\n")
	mustWrite(t, filepath.Join(serverDir, "server.properties"), "level-name=world\n")
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

	result, err := SaveServerConfigValidated(opts, ServerConfigPayload{
		ServerDir: serverDir, LaunchType: "script", LaunchPath: jar, WorkingDir: serverDir,
	}, "tr_backup_auto")
	if err != nil {
		t.Fatalf("SaveServerConfigValidated() error = %v", err)
	}
	if !result.OK || result.Backup == nil {
		t.Fatalf("result = %#v, want ok with backup side-effect", result)
	}
	summary, err := BackupProfileSummaryForServer(opts, "")
	if err != nil {
		t.Fatalf("BackupProfileSummaryForServer() error = %v", err)
	}
	if summary["serverDir"] != serverDir {
		t.Fatalf("summary serverDir = %#v, want %q", summary["serverDir"], serverDir)
	}
	worldPath := filepath.Join(serverDir, "world")
	if summary["worldPath"] != worldPath {
		t.Fatalf("summary worldPath = %#v, want %q", summary["worldPath"], worldPath)
	}
}

func findProfileRoot(profile BackupProfile, rootID string) (BackupProfileRoot, bool) {
	for _, root := range profile.Roots {
		if root.RootID == rootID {
			return root, true
		}
	}
	return BackupProfileRoot{}, false
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}