package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ruichen-0079/ACBH/agent/internal/manifest"
)

func TestScanCommandWritesManifest(t *testing.T) {
	serverDir := t.TempDir()
	writeCLITestFile(t, serverDir, "world/region/r.0.0.mca", "world-data")
	writeCLITestFile(t, serverDir, "logs/latest.log", "log")
	output := filepath.Join(t.TempDir(), "manifest.json")

	out, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "world-snapshot",
		"--artifact-id", "snap_000001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", output,
	)
	if err != nil {
		t.Fatalf("scan command error = %v", err)
	}
	if !strings.Contains(out, "Scan complete.") {
		t.Fatalf("scan output = %q", out)
	}

	loaded, err := manifest.LoadFile(output)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if loaded.ArtifactID != "snap_000001" || loaded.Summary.IncludedFiles != 1 || loaded.Summary.IgnoredFiles != 1 {
		t.Fatalf("manifest = %#v", loaded)
	}
}

func TestScanCommandPrintsManifestJSONWithoutOutput(t *testing.T) {
	serverDir := t.TempDir()
	writeCLITestFile(t, serverDir, "server.properties", "motd=ACBH")

	out, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "admin-state",
		"--artifact-id", "admin_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--json",
	)
	if err != nil {
		t.Fatalf("scan command error = %v", err)
	}

	var loaded manifest.Manifest
	if err := json.Unmarshal([]byte(out), &loaded); err != nil {
		t.Fatalf("Unmarshal() error = %v output=%q", err, out)
	}
	if loaded.ArtifactKind != manifest.AdminState {
		t.Fatalf("artifactKind = %q", loaded.ArtifactKind)
	}
}

func TestManifestCommands(t *testing.T) {
	serverDir := t.TempDir()
	writeCLITestFile(t, serverDir, "world/region/r.0.0.mca", "world-data")
	oldPath := filepath.Join(t.TempDir(), "old.json")
	if _, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "world-snapshot",
		"--artifact-id", "snap_000001",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", oldPath,
	); err != nil {
		t.Fatalf("old scan error = %v", err)
	}

	writeCLITestFile(t, serverDir, "world/region/r.0.0.mca", "changed-world-data")
	newPath := filepath.Join(t.TempDir(), "new.json")
	if _, err := executeCommand(
		"scan",
		"--server-dir", serverDir,
		"--artifact-kind", "world-snapshot",
		"--artifact-id", "snap_000002",
		"--server-pack-version", "pack_000001",
		"--group-id", "group_abc",
		"--creator-host-id", "host_abc",
		"--output", newPath,
	); err != nil {
		t.Fatalf("new scan error = %v", err)
	}

	validateOut, err := executeCommand("manifest", "validate", "--file", newPath)
	if err != nil {
		t.Fatalf("validate command error = %v", err)
	}
	if !strings.Contains(validateOut, "Manifest is valid") {
		t.Fatalf("validate output = %q", validateOut)
	}

	diffOut, err := executeCommand("manifest", "diff", "--old", oldPath, "--new", newPath)
	if err != nil {
		t.Fatalf("diff command error = %v", err)
	}
	if !strings.Contains(diffOut, "Modified: 1") {
		t.Fatalf("diff output = %q", diffOut)
	}

	inspectOut, err := executeCommand("manifest", "inspect", "--file", newPath)
	if err != nil {
		t.Fatalf("inspect command error = %v", err)
	}
	if !strings.Contains(inspectOut, "Artifact ID: snap_000002") {
		t.Fatalf("inspect output = %q", inspectOut)
	}
}

func TestRootIncludesPushAndPullCommands(t *testing.T) {
	cmd := newRootCmd()
	for _, name := range []string{"push", "pull"} {
		found, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatalf("Find(%q) error = %v", name, err)
		}
		if found == nil || found.Name() != name {
			t.Fatalf("Find(%q) = %#v", name, found)
		}
	}
}

func executeCommand(args ...string) (string, error) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeCLITestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
