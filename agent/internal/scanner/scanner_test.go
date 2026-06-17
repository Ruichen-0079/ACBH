package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/fileclass"
	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

func TestScanWorldSnapshot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "world/region/r.0.0.mca", "world-data")
	writeFile(t, root, "plugins/LuckPerms/config.yml", "permissions")
	writeFile(t, root, "mods/example.jar", "mod")
	writeFile(t, root, "logs/latest.log", "log")
	writeFile(t, root, "notes.txt", "unknown")

	got, report, err := Scan(testOptions(root, manifest.WorldSnapshot, "snap_000001"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if err := manifest.Validate(got); err != nil {
		t.Fatalf("manifest.Validate() error = %v", err)
	}
	if len(got.Files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(got.Files))
	}
	if got.Files[0].Path != "plugins/LuckPerms/config.yml" || got.Files[1].Path != "world/region/r.0.0.mca" {
		t.Fatalf("files not sorted or filtered correctly: %#v", got.Files)
	}
	if got.Files[1].SHA256 != sha("world-data") {
		t.Fatalf("sha = %s, want %s", got.Files[1].SHA256, sha("world-data"))
	}
	if report.IncludedFiles != 2 || report.IgnoredFiles != 1 || report.UnknownFiles != 1 || report.TotalBytes == 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestScanServerPack(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mods/example.jar", "mod")
	writeFile(t, root, "plugins/Vault.jar", "plugin")
	writeFile(t, root, "fabric-server-mc.1.20.1-loader.0.16.7-launcher.1.0.1.jar", "launcher")
	writeFile(t, root, "server.jar", "server")
	writeFile(t, root, "server.properties", "motd=ACBH")
	writeFile(t, root, "libraries/com/google/guava/guava.jar", "library")
	writeFile(t, root, ".fabric/server/fabric-loader-server.jar", "fabric-server")
	writeFile(t, root, "eula.txt", "eula=true")
	writeFile(t, root, "config/server.toml", "config")
	writeFile(t, root, "world/level.dat", "world")
	writeFile(t, root, "logs/latest.log", "log")
	writeFile(t, root, ".fabric/processedMods/fabric-api.jar", "processed")
	writeFile(t, root, ".fabric/remappedJars/minecraft.jar", "remapped")

	got, report, err := Scan(testOptions(root, manifest.ServerPack, "pack_000001"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(got.Files) != 9 {
		t.Fatalf("len(files) = %d, want 9: %#v", len(got.Files), got.Files)
	}
	for _, file := range got.Files {
		if file.Class != fileclass.ServerPack {
			t.Fatalf("file class = %q, want server-pack", file.Class)
		}
	}
	if report.IgnoredFiles != 3 || report.UnknownFiles != 0 {
		t.Fatalf("report = %#v", report)
	}
	if got.ServerPackVersion == nil || *got.ServerPackVersion != "pack_000001" {
		t.Fatalf("serverPackVersion = %#v", got.ServerPackVersion)
	}
}

func TestScanAdminState(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server.properties", "motd=ACBH")
	writeFile(t, root, "ops.json", "[]")
	writeFile(t, root, "world/level.dat", "world")

	got, _, err := Scan(testOptions(root, manifest.AdminState, "admin_000001"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(got.Files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(got.Files))
	}
	for _, file := range got.Files {
		if file.Class != fileclass.AdminState {
			t.Fatalf("file class = %q, want admin-state", file.Class)
		}
	}
}

func TestScanServerPackIncludesServerProperties(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server.properties", "motd=ACBH")
	writeFile(t, root, "eula.txt", "eula=true")
	writeFile(t, root, "server.jar", "server")
	writeFile(t, root, "world/level.dat", "world")

	got, _, err := Scan(testOptions(root, manifest.ServerPack, "pack_000001"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	found := false
	for _, f := range got.Files {
		if f.Path == "server.properties" {
			found = true
			if f.Class != fileclass.ServerPack {
				t.Fatalf("server.properties class = %q, want server-pack", f.Class)
			}
		}
	}
	if !found {
		t.Fatalf("server.properties not included in server-pack scan")
	}
}

func TestScanWorldSnapshotExcludesServerProperties(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "server.properties", "motd=ACBH")
	writeFile(t, root, "world/region/r.0.0.mca", "world-data")

	got, _, err := Scan(testOptions(root, manifest.WorldSnapshot, "snap_000001"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	for _, f := range got.Files {
		if f.Path == "server.properties" {
			t.Fatalf("server.properties should not be included in world-snapshot scan")
		}
	}
}

func TestScanDeletedFilesFromPreviousManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "world/region/r.0.0.mca", "world-data")

	previousRoot := t.TempDir()
	writeFile(t, previousRoot, "world/region/r.0.0.mca", "world-data")
	writeFile(t, previousRoot, "world/region/r.1.0.mca", "old")
	oldManifest, _, err := Scan(testOptions(previousRoot, manifest.WorldSnapshot, "snap_000001"))
	if err != nil {
		t.Fatalf("previous Scan() error = %v", err)
	}
	previousPath := filepath.Join(t.TempDir(), "previous.json")
	if err := manifest.SaveFile(previousPath, oldManifest); err != nil {
		t.Fatalf("SaveFile() error = %v", err)
	}

	opts := testOptions(root, manifest.WorldSnapshot, "snap_000002")
	opts.PreviousManifestPath = previousPath
	got, report, err := Scan(opts)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if report.DeletedFiles != 1 || got.Summary.DeletedFiles != 1 {
		t.Fatalf("deleted counts = report %#v summary %#v", report, got.Summary)
	}
	deleted := got.Files[1]
	if !deleted.Deleted || deleted.Path != "world/region/r.1.0.mca" || deleted.Size != 0 || deleted.SHA256 != "" {
		t.Fatalf("deleted entry = %#v", deleted)
	}
}

func TestScanSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "world"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "world", "linked.dat")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	got, report, err := Scan(testOptions(root, manifest.WorldSnapshot, "snap_000001"))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(got.Files) != 0 {
		t.Fatalf("len(files) = %d, want 0", len(got.Files))
	}
	if report.IgnoredFiles != 1 {
		t.Fatalf("ignored = %d, want 1", report.IgnoredFiles)
	}
}

func testOptions(root string, kind manifest.ArtifactKind, artifactID string) Options {
	return Options{
		ServerDir:         root,
		ArtifactKind:      kind,
		ArtifactID:        artifactID,
		GroupID:           "group_abc",
		CreatorHostID:     "host_abc",
		ServerPackVersion: "pack_000001",
		Clock: func() time.Time {
			return time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
		},
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
